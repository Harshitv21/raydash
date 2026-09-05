package store

import (
	"fmt"
	// "maps"
	"sync"
	"time"
)

type item struct {
	value     string
	// entire time instance
	expiresAt time.Time
}

type Store struct {
	// read/write mutex lock
	mut  sync.RWMutex
	// the actual data coupled with item struct
	data map[string]item
	/*
	"chan" stands for channel in golang 
	channels are like communication pipes that let one goroutine send data to another and receive it
	chan struct{} is a 0 byte empty struct that carries no value or information
	this will be purely for signalling our background sweep goroutine to start or stop
	*/
	stopChannel chan struct{}
}

/*
what is a goroutine? it's a lightweight independent executing function managed by the golang runtime
it's main purpose is for handling concurrency, allowing multiple tasks to run asynchronously without 
any massive overhead
*/

// i don't like returning numbers i want better readability of code
const (
	KEY_HAS_NO_EXPIRY = -1
	KEY_DOES_NOT_EXIST = -2
)

/*
inst is the instance for Store
*/

// initialization and then returning a safe instance of Store
func NewStore() *Store {
	store := &Store {
		data: make(map[string]item),
		stopChannel: make(chan struct{}),
	}

	// adding go before something makes it a goroutine
	/*
	here i am making the active expiration function run asynchronously in background 
	in my init function at 100 millisecond duration
	*/
	go store.startActiveExpiration(100 * time.Millisecond);

	return store;
}

// checks all keys at a very regular interval and deletes expired ones
func (inst *Store) startActiveExpiration(interval time.Duration) {
	/* 
	this creates a background clock that every 100ms (this is set above during init)
	drops a small message which in this case the current timestamp into the internal 
	channel called ticker.C
	*/
	ticker := time.NewTicker(interval);
	defer ticker.Stop(); // graceful cleanup

	// infinite loop to ensure the background thread never stops running on it's own
	for {
		select {
		/*
		golang's internal channel
		what happens is this, on every 100ms the channel wakes up and fires this case to run the cleanup sweep 
		*/
		case <- ticker.C:
			inst.mut.Lock();
			// take the current time
			now := time.Now();
			
			// then iterate through all key&items and check if it's expired or not
			for key, itm := range inst.data {
				if(!itm.expiresAt.IsZero() && now.After(itm.expiresAt)) {
					// delete if yes
					delete(inst.data, key);
				}
			}
			inst.mut.Unlock();
		// this gets fired when application is shutting down and signal is sent to this channel
		case <- inst.stopChannel:
			// gracefully stop the thread if store is closed
			return;
		}
	}
}

// closing the background sweep
func (inst *Store) Close() {
	close(inst.stopChannel);
}

/*
without exposing the data we can return the size of our cache which will make
it easy for us to test & debug
*/
func (inst *Store) InternalSize() int {
	inst.mut.RLock();
	defer inst.mut.RUnlock();

	return len(inst.data);
}

// set, add/update a key-value pair
// made it exclusive while a write operation
func (inst *Store) Set(key, value string) {
	inst.mut.Lock();
	defer inst.mut.Unlock(); // unlock will happen on function exit
	
	inst.data[key] = item{
		value:     value,
		// set will only instantiate the expiresAt variable the expiration will be set by Expire function
		// but right now this is not nil it's a special value called "Zero Time"
		expiresAt: time.Time{},
	};
}

// retrieves a value and boolean means if it exists or not
// read operation will have a shared read lock
func (inst *Store) Get(key string) (string, bool) {
	inst.mut.RLock();
	itemIfPresent, exists := inst.data[key];

	if !exists {
		inst.mut.RUnlock();
		return "", false;
	}
	
	// when item is present and not expired
	if !itemIfPresent.expiresAt.IsZero() && time.Now().After(itemIfPresent.expiresAt) {
		inst.mut.RUnlock();

		inst.mut.Lock();

		itemIfPresent, exists = inst.data[key];
		// another check just for better robustness
		if exists && !itemIfPresent.expiresAt.IsZero() && time.Now().After(itemIfPresent.expiresAt) {
			delete(inst.data, key);
		}

		inst.mut.Unlock();
		return "", false;
	}

	inst.mut.RUnlock();
	return itemIfPresent.value, true;
}

// remove a key
/*
deleting a non existent key does nothing and is completely safe
*/
// now this too requires an exclusive lock
func (inst *Store) Delete(key string) {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	delete(inst.data, key);
}

// iterating over the map
// shared read lock
/*
while regular print operation is very very slow we will use a snapshot trick
instead of complete iteration we lock the store we take a snapshot by looking 
at the data store it somewhere and then unlock it again and we iterate over the 
copy that we just created this is safe and high performant
*/
func (inst *Store) Iterate() {
	cacheCopy := inst.Snapshot();

	// we don't lock the store for iteration but only for copy which is very good!
	for key, value := range cacheCopy {
		fmt.Printf("Key: %s; Value: %s", key, value);
	}
}

func (inst *Store) Snapshot() map[string]string {
	inst.mut.RLock();
	defer inst.mut.RUnlock(); 

	now := time.Now();
	clone := make(map[string]string, len(inst.data));

	// maps.Copy(clone, inst.data);

	// only copy when it's not expired
	for key, value := range inst.data {
		if(value.expiresAt.IsZero() || now.Before(value.expiresAt)) {
			clone[key] = value.value;
		}
	}

	return clone;
}

// this function sets a time-to-live (TTL) for an existing key
func (inst *Store) Expire(key string, seconds int) bool {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	itemIfPresent, exists := inst.data[key];

	if !exists { return false; }

	// to convert the raw number into seconds we multiply it by time.Second as a constant time.Second represents 1,000,000,000 nanoseconds
	itemIfPresent.expiresAt = time.Now().Add(time.Duration(seconds) * time.Second);
	inst.data[key] = itemIfPresent;

	return true;
}

/*
this returns the remaining TTL for a key in seconds
returns (seconds, true) if it has a TTL
returns (-1, true) if the key exists but has no expiration
returns (-2, false) if the key does not exists
*/
func (inst *Store) TTL(key string) (int, bool) {
	inst.mut.RLock();
	defer inst.mut.RUnlock();

	itemIfPresent, exists := inst.data[key];

	if !exists { return KEY_DOES_NOT_EXIST, false; }

	if itemIfPresent.expiresAt.IsZero() { return KEY_HAS_NO_EXPIRY, true; }

	remainder := time.Until(itemIfPresent.expiresAt);

	/*
	why can't we confidently set this as == 0? to aviod bugs with precision checking of time
	a few nanoseconds like if the time of expire is at 12:00:00.000000
	and a thread calls TTL() a fraction moment later at 12:00:00.003333, -3333 nanoseconds after the deadline has 
	passed if we check with -3333 == 0 it is not so it would think the key is still alive
	*/
	if(remainder <= 0) { return 0, true; }

	return int(remainder.Seconds()), true;
}
package store

import (
	"fmt"
	// "maps"
	"sync"
	"time"
)

type item struct {
	value      string
	// Entire time instance
	expiresAt  time.Time
	/*
	When the key is first inserted this value is preserved across updates and such used for 
	FIFO (oldest eviction first) when maxKeys keycap is setup.
	*/
	insertedAt time.Time
}

type Store struct {
	// read/write mutex lock
	mut  sync.RWMutex
	// the actual data coupled with item struct
	data map[string]item
	/*
	"chan" stands for channel in Go. Channels are like communication pipes that let one goroutine
	send data to another and receive it. `chan struct{}` is a 0 byte empty struct that carries no 
	value or information this will be purely for signalling our background sweep goroutine to start
	or stop.
	*/
	stopChannel chan struct{}
	/*
	Caps how many keys store will hold before FIFO kicks in to make room. 
	0: unlimited.
	Updating an existing key never triggers eviction i mean that would be stupid.
	*/
	maxKeys int
}

/*
A snapshot entry represents a full row of snapshot unlike Snapshot() we use to Iterate() this keeps
the absolute expiry time too which is what the persistence layer (db) needs to save/restore TTLs 
correctly across a restart instead of resetting them.
*/
type SnapshotEntry struct {
	Key        string
	Value      string
	ExpiresAt  time.Time // "zero value"
	InsertedAt time.Time // should be included for every snapshot no?
}

/*
What is a goroutine? it's a lightweight independent executing function managed by the golang runtime
it's main purpose is for handling concurrency, allowing multiple tasks to run asynchronously without 
any massive overhead. Adding go before something makes it a goroutine.
*/

// I don't like returning numbers i want better readability of code.
const (
	KEY_HAS_NO_EXPIRY = -1
	KEY_DOES_NOT_EXIST = -2
)

/*
Initialization and then returning a safe instance of Store introducing sweepInterval which controls how 
often the background active-expiration scans for expired keys maxKeys, pass 0 for unlimited.
*/
func NewStore(sweepInterval time.Duration, maxKeys int) *Store {
	store := &Store {
		data: make(map[string]item),
		stopChannel: make(chan struct{}),
		maxKeys: maxKeys,
	}

	/*
	Here i am making the active expiration function run asynchronously in background in my init function 
	at the configured sweep duration.
	*/
	go store.startActiveExpiration(sweepInterval);

	return store;
}
/*
`inst` is the instance for Store. this function checks all keys at a very regular interval and deletes 
expired ones.
*/
func (inst *Store) startActiveExpiration(interval time.Duration) {
	/*
	This creates a background clock that every 100ms (this is set above during init) drops a small message 
	which in this case the current timestamp into the internal channel called ticker.C.
	*/
	ticker := time.NewTicker(interval);
	defer ticker.Stop(); // graceful cleanup

	// infinite loop to ensure the background thread never stops running on it's own
	for {
		select {
		/*
		Golang's internal channel. What happens is this, on every 100ms the channel wakes up and fires 
		this case to run the cleanup sweep. 
		*/
		case <- ticker.C:
			inst.mut.Lock();
			
			// Then iterate through all key&items and check if it's expired or not.
			for key, itm := range inst.data {
				if itm.isExpired() {
					delete(inst.data, key); // delete if yes
				}
			}
			inst.mut.Unlock();
		// This gets fired when application is shutting down and signal is sent to this channel
		case <- inst.stopChannel:
			return; // Gracefully stop the thread if store is closed
		}
	}
}

// Closing the background sweep
func (inst *Store) Close() { close(inst.stopChannel); }

// Without exposing the data we can return the size of our cache which will make it easy for us to test & debug.
func (inst *Store) InternalSize() int {
	inst.mut.RLock();
	defer inst.mut.RUnlock();

	return len(inst.data);
}

/*
Returns the configured cap size called without a lock. Safe because maxKeys is only ever set once, in
NewStore above, and never written to again after that so there's no concurrent modification for a lock
to protect against here (unlike almost every other field on Store).
*/
func (inst *Store) MaxKeys() int { return inst.maxKeys; }

/*
set, add/update a key-value pair. Made it exclusive while a write operation.
*/
func (inst *Store) Set(key, value string) {
	inst.mut.Lock();
	defer inst.mut.Unlock(); // unlock will happen on function exit

	existing, exists := inst.data[key];

	insertedAt := time.Now();

	if(exists) {
		// updating does nothing to the insertedAt
		insertedAt = existing.insertedAt;
	} else if(inst.maxKeys > 0 && len(inst.data) >= inst.maxKeys) {
		inst.evictOldestLocked();
	}
	
	inst.data[key] = item{
		value:     value,
		/*
		Set will only instantiate the expiresAt variable the expiration will be set by Expire function
		but right now this is not nil it's a special value called "Zero Time".
		*/
		expiresAt: time.Time{},
		insertedAt: insertedAt,
	};
}

/*
Deletes whichever key has the oldest insertedAt this does a full scan only runs when insert would go
over the cap caller must hold a write lock first tho (will be replaced with true LRU later). Same 
trade-off as the active expiration sweep above: a full map scan is not free, but it only ever runs at
the exact moment an insert would otherwise go over the cap. Not on every write so the cost only shows
up when eviction is actually needed, not on the hot path in general.
*/
func (inst *Store) evictOldestLocked() {
	var oldestKey string;
	var oldestTime time.Time;
	first := true;

	for key, itm := range inst.data {
		if(first || itm.insertedAt.Before(oldestTime)) {
			oldestKey = key;
			oldestTime = itm.insertedAt;
			first = false;
		}
	}

	if(!first) {
		delete(inst.data, oldestKey);
	}
}

// Retrieves a value and boolean means if it exists or not. Read operation will have a shared read lock.
func (inst *Store) Get(key string) (string, bool) {
	inst.mut.RLock();
	itemIfPresent, exists := inst.data[key];

	if !exists {
		inst.mut.RUnlock();
		return "", false;
	}
	
	// When item is expired
	if itemIfPresent.isExpired() {
		inst.mut.RUnlock();

		inst.mut.Lock();

		itemIfPresent, exists = inst.data[key];
		// Another check just for better robustness
		if itm, ok:= inst.data[key]; ok && itm.isExpired() {
			delete(inst.data, key);
		}

		inst.mut.Unlock();
		return "", false;
	}

	inst.mut.RUnlock();
	return itemIfPresent.value, true;
}

/*
Remove a key. Deleting a non existent key does nothing and is completely safe now this too requires 
an exclusive lock.
*/
func (inst *Store) Delete(key string) {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	delete(inst.data, key);
}

/*
Iterating over the map with shared read lock. While regular print operation is very very slow we will
use a snapshot trick instead of complete iteration we lock the store we take a snapshot by looking at 
the data store it somewhere and then unlock it again and we iterate over the copy that we just created 
this is safe and high performant!
*/
func (inst *Store) Iterate() {
	cacheCopy := inst.Snapshot();

	// We don't lock the store for iteration but only for copy which is very good!
	for key, value := range cacheCopy { fmt.Printf("Key: %s; Value: %s", key, value); }
}

func (inst *Store) Snapshot() map[string]string {
	inst.mut.RLock();
	defer inst.mut.RUnlock(); 

	clone := make(map[string]string, len(inst.data));

	// maps.Copy(clone, inst.data);

	// Only copy when it's not expired
	for key, value := range inst.data {
		if !value.isExpired() { clone[key] = value.value; }
	}

	return clone;
}

// This function sets a time-to-live (TTL) for an existing key.
func (inst *Store) Expire(key string, seconds int) bool {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	itemIfPresent, exists := inst.data[key];

	if !exists { return false; }

	/*
	To convert the raw number into seconds we multiply it by time.Second as a constant time.Second 
	represents 1,000,000,000 nanoseconds.
	*/
	itemIfPresent.expiresAt = time.Now().Add(time.Duration(seconds) * time.Second);
	inst.data[key] = itemIfPresent;

	return true;
}

/*
This returns the remaining TTL for a key in seconds.
- Returns (seconds, true) if it has a TTL
- Returns (-1, true) if the key exists but has no expiration
- Returns (-2, false) if the key does not exists
*/
func (inst *Store) TTL(key string) (int, bool) {
	inst.mut.RLock();
	defer inst.mut.RUnlock();

	itemIfPresent, exists := inst.data[key];

	if !exists { return KEY_DOES_NOT_EXIST, false; }

	if itemIfPresent.expiresAt.IsZero() { return KEY_HAS_NO_EXPIRY, true; }

	remainder := time.Until(itemIfPresent.expiresAt);

	/*
	Why can't we confidently set this as == 0? to aviod bugs with precision checking of time
	a few nanoseconds like if the time of expire is at 12:00:00.000000 and a thread calls TTL() 
	a fraction moment later at 12:00:00.003333, -3333 nanoseconds after the deadline has passed 
	if we check with -3333 == 0 it is not so it would think the key is still alive.
	*/
	if(remainder <= 0) { return 0, true; }

	return int(remainder.Seconds()), true;
}

// If a key exists or not
func (inst *Store) Exists(key string) bool {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	itemIfPresent, exists := inst.data[key];
	if(!exists) { return false; }

	// Double check
	if itemIfPresent.isExpired() { 
		delete(inst.data, key);
		return false; 
	}

	return true;
}

// Complete wipe of all data keys from the store memory
func (inst *Store) FlushAll() {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	// re-init map to 0 and drop everything from memory
	inst.data = make(map[string]item);
}

/*
Returns every non expired key as a SnapshotEntry taken under a single read lock so it reflects one 
consistent instant just like the above Snapshot but here we preserve the expiry part.
*/
func (inst *Store) SnapshotEntries() []SnapshotEntry {
	inst.mut.RLock();
	defer inst.mut.RUnlock();

	entries := make([]SnapshotEntry, 0, len(inst.data));

	for key, itm := range inst.data {
		if(itm.isExpired()) { continue; }
		
		entries = append(entries, SnapshotEntry{
			Key:        key,
			Value:      itm.value,
			ExpiresAt:  itm.expiresAt,
			InsertedAt: itm.insertedAt,
		});
	}

	return entries;
}

/*
LoadSnapshot populates the store directly from a set of entries skipping that's already expired by the
time it's loaded we use this on startup of application to restore from a persisted snapshot now one thing 
can happen before we load any snapshot what if the RAYDASH_MAX_KEYS was lowered since the snapshot was 
taken in that case it should trim back to now updated maxKeys.
*/
func (inst *Store) LoadSnapshot(entries []SnapshotEntry) {
	inst.mut.Lock();
	defer inst.mut.Unlock();

	now := time.Now();

	for _, e := range entries {
		if(!e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now)) { continue; } // ignore
		
		/*
		This is the fallback for the (normally never hit) case of a restored entry with a zero insertedAt.
		Without it, that key would carry Go's zero time.Time forward, which sorts as "before" basically
		everything else, making it look like the oldest key in the whole store and the very first thing
		evictOldestLocked() would pick if eviction is ever needed. IMPORTANT thing to note is that this 
		fallback only does anything if the struct literal below actually uses this `insertedAt` variable
		rather than reading e.insertedAt again directly. That's the one word fix applied here versus the
		version that computed this and then didn't use it. 
		*/
		insertedAt := e.InsertedAt;
		if(insertedAt.IsZero()) { insertedAt = now; }

		inst.data[e.Key] = item{
			value:      e.Value, 
			expiresAt:  e.ExpiresAt,	
			insertedAt: insertedAt,
		};
	}

	for(inst.maxKeys > 0 && len(inst.data) > inst.maxKeys) { inst.evictOldestLocked(); }
}


func (itm item) isExpired() bool {
	if(itm.expiresAt.IsZero()) { return false; }

	return time.Now().After(itm.expiresAt);
}
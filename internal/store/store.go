package store

import (
	"fmt"
	"sync"
)

type Store struct {
	mut sync.RWMutex
	data map[string]string
}

/*
inst is the instance for Store
*/

// initialization and then returning a safe instance
func NewStore() *Store {
	return &Store{
		data: make(map[string]string),
	}
}

// set, add/update a key-value pair
// made it exclusive while a write operation
func (inst *Store) Set(key, value string) {
	inst.mut.Lock();
	defer inst.mut.Unlock(); // unlock will happen on function exit
	inst.data[key] = value;
}

// retrieves a value and boolean means if it exists or not
// read operation will have a shared read lock
func (inst *Store) Get(key string) (string, bool) {
	inst.mut.RLock(); 
	defer inst.mut.RUnlock(); // same shit
	val, exists := inst.data[key];
	return val, exists;
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

	clone := make(map[string]string, len(inst.data));

	for key, value := range inst.data {
		clone[key] = value;
	}

	defer inst.mut.RUnlock(); 
	return clone;
}
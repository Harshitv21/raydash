package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
);

/*
100 writers and 100 readers run genuine concurrent operations against the same keys. A reader can
legitemately run before its matching writer, so "key not found" is alright, expected and not a failure.
What's not alright is Get() reporting a key as existing with the WRONG value. That would mean the RWMutex
let a reader observe some half written state, which is exactly the kind of bug this test exists to catch.
A final pass once everything's settled then checks every key actually landed.
*/
func TestStoreConcurrency(t *testing.T) {
	expiryIntervalMs := 100;
	store := NewStore(time.Duration(expiryIntervalMs) * time.Millisecond, 0);

	defer store.Close(); // prevent leaking
	
	var mut sync.Mutex;
	var corrupted []string;
	var wg sync.WaitGroup;

	// 100 concurrent writers
	for i := range 101 {
		wg.Add(1);
		go func(id int) {
			defer wg.Done();
			key := fmt.Sprintf("key_%d", id);
			store.Set(key, "value");
		}(i)
	}

	// 100 concurrent readers
	for i := range 101 {
		wg.Add(1);
		go func(id int) {
			defer wg.Done();
			key := fmt.Sprintf("key_%d", id);

			// if exists and not valid then its corrupted
			if val, exists := store.Get(key); exists && val != "value" {
				mut.Lock();
				corrupted = append(corrupted, fmt.Sprintf("%s=%q", key, val));
				mut.Unlock();
			}
		}(i)
	}

	wg.Wait();

	if(len(corrupted) > 0) { t.Errorf("Get() returned a corrupted/partial value during concurrent access: %v", corrupted); }

	/*
	Every writer has definitely finished by now, so every key must be retrievable with no correct
	value. The assertion the original version of this test never actually made.
	*/
	for i := range 101 {
		key := fmt.Sprintf("key_%d", i);
		val, exists := store.Get(key);

		if !exists || val != "value" {
			t.Errorf("After all writers finished, Get(%q) = (%q, %v), want (\"value\", true)", key, val, exists);
		}
	}
}
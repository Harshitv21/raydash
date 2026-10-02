package store;

import (
	"fmt"
	"testing"
	"time"
)

/*
None of my existing tests in the store test suite pass anything else but "0" for maxKeys, so the
oldest-inserted eviction path (evictOldestLocked) had 0 coverage. These 3 test functions cover,
eviction actually firing at the cap, updates never counting as new insert, and maxKeys=0 staying
truly UNLIMITED!

A 1ms sleep sits between Set() calls wherever this test's assertions depend on 1 key being strictly
OLDER than another. In practice 2 sequential Set() calls are separated by far more than 1ms of real
work (aquiring the lock, a map write) and realistically would never land on the exact same timestamp
but relying on "almost certainly won't" is what i would call a bitch boy mentality. The sleep makes
the ordering unambiguous instead of merely probable. 
*/

func TestEvictionOldestInsertedOnCap(t *testing.T) {
	storeCap := 3;
	store := NewStore(100 * time.Millisecond, storeCap);
	defer store.Close();

	store.Set("key_0", "a");
	time.Sleep(time.Millisecond);

	store.Set("key_1", "b");
	time.Sleep(time.Millisecond);

	store.Set("key_2", "c");
	time.Sleep(time.Millisecond);

	if(store.InternalSize() != storeCap) { t.Fatalf("expected 3 keys before the cap is exceeded, got %d", store.InternalSize()); }

	store.Set("key_3", "d"); // 4th distinct key over our cap limit of 3

	if(store.InternalSize() != storeCap) { t.Fatalf("unexpected eviction to hold the store at %d keys, got %d instead", storeCap, store.InternalSize()); }

	if _, exists := store.Get("key_0"); exists {
		t.Error("key_0 was inserted first and should have been evicted, but its still present!");
	}

	for _, key := range []string{"key_1", "key_2", "key_3"} {
		if _, exists := store.Get(key); !exists {
			t.Errorf("%s should still be present post eviction strategy but its gone :(", key);
		}
	}
}

func TestEvictionIgnoresUpdatesToExistingKeys(t *testing.T) {
	storeCap := 2;
	store := NewStore(100 * time.Millisecond, storeCap);
	defer store.Close();

	store.Set("key_0", "original");
	time.Sleep(time.Millisecond);

	store.Set("key_1", "b");

	/* 
	Updating key_0 repeatedly. None of these are a "new" key so none of then should trigger eviction
	even though the store is at the cap.
	*/
	store.Set("key_0", "update");
	store.Set("key_0", "updated again!");
	
	if(store.InternalSize() != storeCap) {
		t.Fatalf("updating an existing key should never trigger eviction, got %d keys", store.InternalSize());
	}

	val, exists := store.Get("key_0");
	if(!exists || val != "updated again!") {
		t.Errorf("Get(key_0) = (%q, %v), want (\"updated again!\", true)", val, exists);
	}

	/*
	key_0 is still the OLDEST-inserted key despite being the most recently updated. A geniunely new
	3rd key should therefore evict key_0 not key_1 which is the whole point of this test.
	*/
	store.Set("key_2", "c");

	if _, exists := store.Get("key_0"); exists {
		t.Error("key_0 is still the oldest-inserted key and should have been evicted despite being recently updated");
	}

	if _, exists := store.Get("key_1"); !exists {
		t.Error("key_1 should not have been evicted - key_0 was the oldest insertion, not key_1");
	}
}

func TestEvictionDisabledWhenMaxKeysIsZero(t *testing.T) {
	store := NewStore(100 * time.Millisecond, 0);
	defer store.Close();

	for i := 0; i < 500; i++ { store.Set(fmt.Sprintf("key_%d", i), "value"); }

	if store.InternalSize() != 500 {
		t.Errorf("maxKeys=0 should mean unlimited, but only %d of 500 keys are present", store.InternalSize());
	}
}
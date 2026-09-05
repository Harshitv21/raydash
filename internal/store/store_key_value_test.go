package store

import (
	"testing"
	"time"
)

func TestStoreTTLAndCodes(t *testing.T) {
	store := NewStore();
	defer store.Close();

	store.Set("no_expiry", "forever");
	store.Set("timed_key", "temp");
	store.Expire("timed_key", 10);

	tests := []struct {
		name        string
		key         string
		expectedTTL int
		expectedOk  bool
	}{
		{
			name: "Key does not exist -> KeyDoesNotExist",
			key: "missing_key",
			expectedTTL: KEY_DOES_NOT_EXIST, // evaluates to -2
			expectedOk: false,
		},
		{
			name: "Key has no expiration -> KeyHasNoExpiry",
			key: "no_expiry",
			expectedTTL: KEY_HAS_NO_EXPIRY, // evaluates to -1
			expectedOk: true,
		},
		{
			name: "Key has safe remaining TTL",
			key: "timed_key",
			expectedTTL: 10,
			expectedOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl, ok := store.TTL(tt.key)
			if ok != tt.expectedOk {
				t.Errorf("TTL() ok: %v, wanted: %v", ok, tt.expectedOk);
			}
			if tt.key == "timed_key" {
				// checking for a buffer range
				if ttl <= 0 || ttl > 10 {
					t.Errorf("TTL() timed_key: %v, expected between 0-10", ttl);					
				}
			} else if ttl != tt.expectedTTL {
					t.Errorf("TTL() code: %v, wanted: %v", ttl, tt.expectedTTL);									
			}
		})
	}
}

// checks if get removes the expired key or nah
func TestLazyExpiration(t *testing.T) {
	store := NewStore();
	defer store.Close();

	store.Set("lazy_key", "evict_me!");
	store.Expire("lazy_key", 1); // a second of lifespan

	// crossed the second boundary
	time.Sleep(1100 * time.Millisecond);

	value, exists := store.Get("lazy_key");
	if exists || value != "" {
		t.Errorf("Lazy eviction failed: key is still visible! Get(): (%q, %v)", value, exists);
	}
}

// checks if the background thread sweeps and cleans the map or naah
func TestActiveExpiration(t *testing.T) {
	store := &Store{
		data:        make(map[string]item),
		stopChannel: make(chan struct{}),
	}

	go store.startActiveExpiration(10 * time.Millisecond);
	defer store.Close();

	store.Set("active_key", "purge me UwU");
	store.Expire("active_key", 1);

	if(store.InternalSize() != 1) {
		t.Fatalf("Active expiration setup failure should be 1 got: %d", store.InternalSize());
	}

	time.Sleep(1200 * time.Millisecond);

	if store.InternalSize() != 0 {
		t.Errorf("Active expiration leak: map internal size is still %d", store.InternalSize());
	}
}

func TestSnapshotFiltersExpiredKeys(t *testing.T) {
	store := NewStore();
	defer store.Close();

	store.Set("valid", "keep");
	store.Set("expired", "drop");
	store.Expire("expired", 1);

	time.Sleep(1100 * time.Millisecond);

	snap := store.Snapshot();

	if _, exists := snap["expired"]; exists {
		t.Error("Snapshot has included an expired key!");
	}
	if val, exists := snap["valid"]; !exists || val != "keep" {
		t.Error("Snapshot failed to retain valid live keys!");
	}
}
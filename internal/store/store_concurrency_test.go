package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
);

func TestStoreConcurrency(t *testing.T) {
	expiryIntervalMs := 100;
	store := NewStore(time.Duration(expiryIntervalMs) * time.Millisecond, 0);
	
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

	for i := range 101 {
		wg.Add(1);
		go func(id int) {
			defer wg.Done();
			key := fmt.Sprintf("key_%d", id);
			store.Get(key);
		}(i)
	}

	wg.Wait();
}
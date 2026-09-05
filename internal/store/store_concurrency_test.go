package store

import (
	"fmt"
	"sync"
	"testing"
);

func TestStoreConcurrency(t *testing.T) {
	store := NewStore();
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
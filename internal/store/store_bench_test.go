package store

import (
	"fmt"
	"testing"
)

// measuring concurrent write performance
func BenchmarkParallelSet(b *testing.B) {
	store := NewStore();

	b.ResetTimer(); // excluding the setup time above
	b.RunParallel(func(pb *testing.PB) {
		i := 0;
		for pb.Next() {
			key := fmt.Sprintf("key_%d", i);
			store.Set(key, "value");
			i++;
		}
	})
}

func BenchmarkParallelGet(b *testing.B) {
	store := NewStore();
	store.Set("example_uuid_1", "Harshit");

	b.ResetTimer();
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			store.Get("example_uuid");
		}
	})
}

// kinda like a real world working with 90% read & 10% write
func BenchmarkParallelMixed(b *testing.B) {
	store := NewStore();

	b.ResetTimer();
	b.RunParallel(func(pb *testing.PB) {
		i := 0;
		for pb.Next() {
			if i % 10 == 0 { // ig for every 10th iteration we are simulating a write
				store.Set("mixed_key_uuid", "blah");
			} else {
				store.Get("mixed_key_uuid");
			}
			i++;
		}
	})
}
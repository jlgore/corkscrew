package main

import (
	"fmt"
	"testing"
	"time"
)

// The cleanup goroutine used to run forever. Before Stop it removes expired
// entries; after Stop it must have exited, so an expired entry stays until
// something else removes it.
func TestCacheStopEndsCleanup(t *testing.T) {
	cache := NewCache(time.Minute, 16)
	cache.StartCleanupTimer(time.Millisecond)

	cache.SetWithTTL("expired", 1, -time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for cache.Size() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("cleanup timer never removed the expired entry")
		}
		time.Sleep(time.Millisecond)
	}

	cache.Stop()
	cache.Stop() // Stop is idempotent, so every shutdown path may call it.
	cache.SetWithTTL("expired", 1, -time.Second)
	time.Sleep(20 * time.Millisecond)
	if cache.Size() != 1 {
		t.Fatalf("size = %d after Stop, want 1: cleanup still running", cache.Size())
	}
}

// Set used to grow the maps without limit. At the cap, expired entries go
// first, then the entry closest to expiry.
func TestCacheEvictsAtEntriesMax(t *testing.T) {
	cache := NewCache(time.Minute, 4)
	defer cache.Stop()

	cache.SetWithTTL("soonest", 0, time.Second)
	for index := 1; index < 4; index++ {
		cache.Set(fmt.Sprintf("key-%d", index), index)
	}
	cache.Set("key-4", 4)
	if cache.Size() != 4 {
		t.Fatalf("size = %d, want 4", cache.Size())
	}
	if _, ok := cache.Get("soonest"); ok {
		t.Fatal("entry closest to expiry survived eviction")
	}
	if _, ok := cache.Get("key-4"); !ok {
		t.Fatal("newest entry missing")
	}

	cache.Set("key-1", 11) // Overwriting an existing key never evicts.
	if cache.Size() != 4 {
		t.Fatalf("size = %d after overwrite, want 4", cache.Size())
	}
	if _, ok := cache.Get("key-2"); !ok {
		t.Fatal("overwrite evicted another entry")
	}
}

// A full cache holding expired entries drops all of them before it evicts
// anything live.
func TestCacheSweepsExpiredBeforeEvicting(t *testing.T) {
	cache := NewCache(time.Minute, 4)
	defer cache.Stop()

	cache.SetWithTTL("expired-1", 0, -time.Second)
	cache.SetWithTTL("expired-2", 0, -time.Second)
	cache.Set("key-1", 1)
	cache.Set("key-2", 2)
	cache.Set("key-3", 3)
	if cache.Size() != 3 {
		t.Fatalf("size = %d, want 3 (both expired entries swept)", cache.Size())
	}
	for _, key := range []string{"key-1", "key-2", "key-3"} {
		if _, ok := cache.Get(key); !ok {
			t.Fatalf("live entry %s evicted while expired ones were available", key)
		}
	}
}

// Successive evictions take live entries in expiry order, judged by each key's
// latest TTL: a key overwritten with a later expiry is not evicted at its old one.
func TestCacheEvictsInExpiryOrder(t *testing.T) {
	cache := NewCache(time.Hour, 3)
	defer cache.Stop()

	cache.SetWithTTL("a", 0, 30*time.Second)
	cache.SetWithTTL("b", 0, 10*time.Second)
	cache.SetWithTTL("c", 0, 20*time.Second)
	cache.SetWithTTL("b", 0, time.Minute) // b now expires last of the three.

	cache.Set("d", 0) // Evicts c, the nearest live expiry.
	cache.Set("e", 0) // Evicts a.
	for _, key := range []string{"a", "c"} {
		if _, ok := cache.Get(key); ok {
			t.Fatalf("%s survived; eviction must follow expiry order", key)
		}
	}
	for _, key := range []string{"b", "d", "e"} {
		if _, ok := cache.Get(key); !ok {
			t.Fatalf("%s evicted out of expiry order", key)
		}
	}
}

// Eviction used to scan the whole map twice per insert into a full cache. The
// expiry index makes it logarithmic, but overwrites leave stale index entries;
// they must be compacted so the index stays bounded too.
func TestCacheExpiryIndexStaysBounded(t *testing.T) {
	const entriesMax = 8
	cache := NewCache(time.Hour, entriesMax)
	defer cache.Stop()

	for index := 0; index < entriesMax*100; index++ {
		cache.Set(fmt.Sprintf("key-%d", index%(entriesMax*2)), index)
		cache.Set("hot", index)
	}
	if cache.Size() != entriesMax {
		t.Fatalf("size = %d, want %d", cache.Size(), entriesMax)
	}
	if len(cache.order) > 2*entriesMax {
		t.Fatalf("expiry index holds %d items, want at most %d", len(cache.order),
			2*entriesMax)
	}
}

func TestMultiLevelCacheStop(t *testing.T) {
	mlc := NewMultiLevelCache()
	mlc.Stop()
	mlc.Stop()
}

package main

import (
	"testing"
	"time"

	"github.com/jlgore/corkscrew/internal/assert/asserttest"
)

func TestNewCacheAssertsPositiveLimits(t *testing.T) {
	asserttest.FailsWith(t, "TTL is positive", func() { NewCache(0, 1) })
	asserttest.FailsWith(t, "entriesMax is positive", func() { NewCache(time.Minute, 0) })
}

// Entries only enter through SetWithTTL, which makes room first, so entries that bypassed it
// (here written straight into the map, with no expiry index) must trip an assertion rather
// than let the cache grow past its cap.
func TestCacheAssertsEntriesWithinCap(t *testing.T) {
	cache := NewCache(time.Minute, 1)
	cache.entries["a"] = cacheEntry{expiry: time.Now().Add(time.Hour)}
	cache.entries["b"] = cacheEntry{expiry: time.Now().Add(time.Hour)}
	asserttest.Fails(t, func() { cache.Set("c", 1) })
}

func TestResourceScannerAssertsPositiveConcurrency(t *testing.T) {
	asserttest.FailsWith(t, "concurrency is positive", func() { NewResourceScanner(nil, 0) })
}

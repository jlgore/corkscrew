package main

import (
	"container/heap"
	"sync"
	"time"

	"github.com/jlgore/corkscrew/internal/assert"
)

// Per-level entry caps. The provider caches a handful of service lists per
// scope, so these are generous upper bounds that keep a long-lived plugin
// from growing without limit rather than working-set sizes.
const (
	serviceCacheEntriesMax   = 1024
	resourceCacheEntriesMax  = 16384
	operationCacheEntriesMax = 4096
)

// Cache provides thread-safe caching with TTL support and at most entriesMax
// entries.
type Cache struct {
	mu         sync.RWMutex
	entries    map[string]cacheEntry
	duration   time.Duration
	entriesMax int

	// order indexes entries by expiry so that sweeping expired entries and
	// evicting the nearest expiry cost O(log n) per entry removed, instead of a
	// scan of the whole map on every insert into a full cache. Overwrites and
	// deletes leave stale items behind (lazy deletion); compactOrderLocked
	// rebuilds the index once it holds twice entriesMax items.
	order          expiryHeap
	generationNext uint64

	// stop ends the cleanup goroutine; stopOnce makes Stop idempotent and
	// cleanupDone lets Stop wait until the goroutine and its ticker are gone.
	stop        chan struct{}
	stopOnce    sync.Once
	cleanupDone sync.WaitGroup
}

// cacheEntry is one cached value. generation tells the live expiryItem for a
// key apart from stale ones left by earlier writes of the same key.
type cacheEntry struct {
	value      interface{}
	expiry     time.Time
	generation uint64
}

type expiryItem struct {
	expiry     time.Time
	key        string
	generation uint64
}

// expiryHeap is a min-heap of expiryItem by expiry, for container/heap.
type expiryHeap []expiryItem

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expiry.Before(h[j].expiry) }
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expiryHeap) Push(x any)        { *h = append(*h, x.(expiryItem)) }
func (h *expiryHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	*h = old[:len(old)-1]
	return item
}

// NewCache creates a new cache with the specified TTL duration that holds at
// most entriesMax entries.
func NewCache(duration time.Duration, entriesMax int) *Cache {
	assert.That(duration > 0, "gcp: cache TTL is positive")
	assert.That(entriesMax > 0, "gcp: cache entriesMax is positive")
	return &Cache{
		entries:    make(map[string]cacheEntry),
		duration:   duration,
		entriesMax: entriesMax,
		stop:       make(chan struct{}),
	}
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if exists && time.Now().Before(entry.expiry) {
		return entry.value, true
	}

	// Clean up expired entry
	if exists && time.Now().After(entry.expiry) {
		go c.Delete(key)
	}

	return nil, false
}

// Set stores a value in the cache
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.duration)
}

// SetWithTTL stores a value in the cache with a custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if _, exists := c.entries[key]; !exists {
		c.makeRoomLocked(now)
	}
	c.generationNext++
	entry := cacheEntry{value: value, expiry: now.Add(ttl), generation: c.generationNext}
	c.entries[key] = entry
	heap.Push(&c.order, expiryItem{expiry: entry.expiry, key: key, generation: entry.generation})
	c.compactOrderLocked()
	assert.That(len(c.entries) <= c.entriesMax, "gcp: cache entries are at most entriesMax")
	assert.That(len(c.order) >= len(c.entries), "gcp: cache expiry index covers every entry")
	assert.That(len(c.order) <= 2*c.entriesMax, "gcp: cache expiry index is at most 2*entriesMax")
}

// makeRoomLocked frees one slot when the cache is full: it drops every
// expired entry, and if none had expired, the entry closest to expiry.
func (c *Cache) makeRoomLocked(now time.Time) {
	if len(c.entries) < c.entriesMax {
		return
	}
	c.cleanExpiredLocked(now)
	if len(c.entries) < c.entriesMax {
		return
	}
	// Every entry has an item in order, so a live item is found before order
	// runs out; the bound is the index size.
	for len(c.order) > 0 {
		item := heap.Pop(&c.order).(expiryItem)
		if c.liveLocked(item) {
			delete(c.entries, item.key)
			break
		}
	}
	assert.That(len(c.entries) < c.entriesMax, "gcp: cache eviction freed a slot")
}

// liveLocked reports whether item is the current index item of its key rather
// than a stale one left by an overwrite or delete.
func (c *Cache) liveLocked(item expiryItem) bool {
	entry, exists := c.entries[item.key]
	return exists && entry.generation == item.generation
}

// compactOrderLocked rebuilds the expiry index from the live entries once
// stale items make it twice entriesMax. A rebuild costs O(entriesMax) and
// leaves at most entriesMax items, so it runs at most once per entriesMax
// inserts: amortized O(1) per insert.
func (c *Cache) compactOrderLocked() {
	if len(c.order) <= 2*c.entriesMax {
		return
	}
	c.order = c.order[:0]
	for key, entry := range c.entries {
		c.order = append(c.order,
			expiryItem{expiry: entry.expiry, key: key, generation: entry.generation})
	}
	heap.Init(&c.order)
	assert.That(len(c.order) == len(c.entries), "gcp: compacted expiry index matches entries")
}

// Delete removes a value from the cache. Its index item goes stale and is
// dropped when it reaches the top of the heap or at the next compaction.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

// Clear removes all values from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]cacheEntry)
	c.order = nil
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// CleanExpired removes all expired entries from the cache
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanExpiredLocked(time.Now())
}

// cleanExpiredLocked pops index items from the top while they are stale or
// expired. It stops at the first live, unexpired item: everything below it
// expires later. The loop is bounded by the index size.
func (c *Cache) cleanExpiredLocked(now time.Time) {
	for len(c.order) > 0 {
		top := c.order[0]
		if c.liveLocked(top) {
			if !now.After(top.expiry) {
				break
			}
			delete(c.entries, top.key)
		}
		heap.Pop(&c.order)
	}
	assert.That(len(c.order) >= len(c.entries), "gcp: cache expiry index covers every entry")
}

// StartCleanupTimer starts a background goroutine that periodically cleans
// expired entries until Stop is called.
func (c *Cache) StartCleanupTimer(interval time.Duration) {
	assert.That(interval > 0, "gcp: cache cleanup interval is positive")
	ticker := time.NewTicker(interval)
	c.cleanupDone.Add(1)
	go func() {
		defer c.cleanupDone.Done()
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.CleanExpired()
			case <-c.stop:
				return
			}
		}
	}()
}

// Stop ends the cleanup goroutine and stops its ticker, waiting until both
// are gone. It is safe to call more than once and without a running timer.
func (c *Cache) Stop() {
	c.stopOnce.Do(func() { close(c.stop) })
	c.cleanupDone.Wait()
}

// MultiLevelCache provides hierarchical caching with different TTLs
type MultiLevelCache struct {
	serviceCache   *Cache
	resourceCache  *Cache
	operationCache *Cache
}

// NewMultiLevelCache creates a new multi-level cache. Call Stop to end its
// cleanup goroutines.
func NewMultiLevelCache() *MultiLevelCache {
	mlc := &MultiLevelCache{
		// Services change rarely, resources can change, operations are short-lived.
		serviceCache:   NewCache(24*time.Hour, serviceCacheEntriesMax),
		resourceCache:  NewCache(15*time.Minute, resourceCacheEntriesMax),
		operationCache: NewCache(5*time.Minute, operationCacheEntriesMax),
	}

	// Start cleanup timers
	mlc.serviceCache.StartCleanupTimer(1 * time.Hour)
	mlc.resourceCache.StartCleanupTimer(5 * time.Minute)
	mlc.operationCache.StartCleanupTimer(1 * time.Minute)

	return mlc
}

// Stop ends the cleanup goroutines of every level. It is idempotent.
func (mlc *MultiLevelCache) Stop() {
	mlc.serviceCache.Stop()
	mlc.resourceCache.Stop()
	mlc.operationCache.Stop()
}

// GetServiceCache returns the service-level cache
func (mlc *MultiLevelCache) GetServiceCache() *Cache {
	return mlc.serviceCache
}

// GetResourceCache returns the resource-level cache
func (mlc *MultiLevelCache) GetResourceCache() *Cache {
	return mlc.resourceCache
}

// GetOperationCache returns the operation-level cache
func (mlc *MultiLevelCache) GetOperationCache() *Cache {
	return mlc.operationCache
}

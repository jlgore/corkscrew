package main

import (
	"sync"
	"time"
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
	data       map[string]interface{}
	ttl        map[string]time.Time
	duration   time.Duration
	entriesMax int

	// stop ends the cleanup goroutine; stopOnce makes Stop idempotent and
	// cleanupDone lets Stop wait until the goroutine and its ticker are gone.
	stop        chan struct{}
	stopOnce    sync.Once
	cleanupDone sync.WaitGroup
}

// NewCache creates a new cache with the specified TTL duration that holds at
// most entriesMax entries.
func NewCache(duration time.Duration, entriesMax int) *Cache {
	if duration <= 0 {
		panic("gcp: cache TTL must be positive")
	}
	if entriesMax <= 0 {
		panic("gcp: cache entriesMax must be positive")
	}
	return &Cache{
		data:       make(map[string]interface{}),
		ttl:        make(map[string]time.Time),
		duration:   duration,
		entriesMax: entriesMax,
		stop:       make(chan struct{}),
	}
}

// Get retrieves a value from the cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if expiry, exists := c.ttl[key]; exists && time.Now().Before(expiry) {
		if data, exists := c.data[key]; exists {
			return data, true
		}
	}

	// Clean up expired entry
	if _, exists := c.ttl[key]; exists && time.Now().After(c.ttl[key]) {
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

	if _, exists := c.data[key]; !exists {
		c.makeRoomLocked(time.Now())
	}
	c.data[key] = value
	c.ttl[key] = time.Now().Add(ttl)
	if len(c.data) > c.entriesMax {
		panic("gcp: cache grew past entriesMax")
	}
	if len(c.data) != len(c.ttl) {
		panic("gcp: cache data and ttl maps disagree")
	}
}

// makeRoomLocked frees one slot when the cache is full: it drops every
// expired entry, and if none had expired, the entry closest to expiry. The
// scan is O(entriesMax), which only runs when the cache is full.
func (c *Cache) makeRoomLocked(now time.Time) {
	if len(c.data) < c.entriesMax {
		return
	}
	c.cleanExpiredLocked(now)
	if len(c.data) < c.entriesMax {
		return
	}
	var oldestKey string
	var oldestExpiry time.Time
	found := false
	for key, expiry := range c.ttl {
		if !found || expiry.Before(oldestExpiry) {
			oldestKey, oldestExpiry, found = key, expiry, true
		}
	}
	if !found {
		panic("gcp: full cache has no entries to evict")
	}
	delete(c.data, oldestKey)
	delete(c.ttl, oldestKey)
	if len(c.data) >= c.entriesMax {
		panic("gcp: cache eviction freed no slot")
	}
}

// Delete removes a value from the cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	delete(c.ttl, key)
}

// Clear removes all values from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]interface{})
	c.ttl = make(map[string]time.Time)
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// CleanExpired removes all expired entries from the cache
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanExpiredLocked(time.Now())
}

func (c *Cache) cleanExpiredLocked(now time.Time) {
	for key, expiry := range c.ttl {
		if now.After(expiry) {
			delete(c.data, key)
			delete(c.ttl, key)
		}
	}
}

// StartCleanupTimer starts a background goroutine that periodically cleans
// expired entries until Stop is called.
func (c *Cache) StartCleanupTimer(interval time.Duration) {
	if interval <= 0 {
		panic("gcp: cache cleanup interval must be positive")
	}
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

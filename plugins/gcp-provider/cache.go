package main

import (
	"sync"
	"time"
)

// Cache provides thread-safe caching with TTL support
type Cache struct {
	mu       sync.RWMutex
	data     map[string]interface{}
	ttl      map[string]time.Time
	duration time.Duration
}

// NewCache creates a new cache with the specified TTL duration
func NewCache(duration time.Duration) *Cache {
	return &Cache{
		data:     make(map[string]interface{}),
		ttl:      make(map[string]time.Time),
		duration: duration,
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
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = value
	c.ttl[key] = time.Now().Add(c.duration)
}

// SetWithTTL stores a value in the cache with a custom TTL
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = value
	c.ttl[key] = time.Now().Add(ttl)
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

	now := time.Now()
	for key, expiry := range c.ttl {
		if now.After(expiry) {
			delete(c.data, key)
			delete(c.ttl, key)
		}
	}
}

// StartCleanupTimer starts a background goroutine that periodically cleans expired entries
func (c *Cache) StartCleanupTimer(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.CleanExpired()
		}
	}()
}

// MultiLevelCache provides hierarchical caching with different TTLs
type MultiLevelCache struct {
	serviceCache   *Cache
	resourceCache  *Cache
	operationCache *Cache
}

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache() *MultiLevelCache {
	mlc := &MultiLevelCache{
		serviceCache:   NewCache(24 * time.Hour),  // Services change rarely
		resourceCache:  NewCache(15 * time.Minute), // Resources can change
		operationCache: NewCache(5 * time.Minute),  // Operations are short-lived
	}
	
	// Start cleanup timers
	mlc.serviceCache.StartCleanupTimer(1 * time.Hour)
	mlc.resourceCache.StartCleanupTimer(5 * time.Minute)
	mlc.operationCache.StartCleanupTimer(1 * time.Minute)
	
	return mlc
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
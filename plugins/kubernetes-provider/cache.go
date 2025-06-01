package main

import (
	"sync"
	"time"
)

// CacheEntry represents a cached item with expiration
type CacheEntry struct {
	Data      interface{}
	ExpiresAt time.Time
}

// MultiLevelCache implements a multi-level cache for Kubernetes resources
type MultiLevelCache struct {
	mu    sync.RWMutex
	data  map[string]CacheEntry
	ttl   time.Duration
}

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache(ttl time.Duration) *MultiLevelCache {
	cache := &MultiLevelCache{
		data: make(map[string]CacheEntry),
		ttl:  ttl,
	}
	
	// Start cleanup goroutine
	go cache.cleanupExpired()
	
	return cache
}

// Get retrieves an item from the cache
func (c *MultiLevelCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.data[key]
	if !exists {
		return nil, false
	}
	
	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	
	return entry.Data, true
}

// Set adds or updates an item in the cache
func (c *MultiLevelCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if ttl == 0 {
		ttl = c.ttl
	}
	
	c.data[key] = CacheEntry{
		Data:      value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Delete removes an item from the cache
func (c *MultiLevelCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	delete(c.data, key)
}

// Clear removes all items from the cache
func (c *MultiLevelCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.data = make(map[string]CacheEntry)
}

// cleanupExpired periodically removes expired entries
func (c *MultiLevelCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.data {
			if now.After(entry.ExpiresAt) {
				delete(c.data, key)
			}
		}
		c.mu.Unlock()
	}
}
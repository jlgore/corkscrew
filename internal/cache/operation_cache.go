package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CacheEntry represents a cached operation result
type CacheEntry struct {
	Key        string
	Value      interface{}
	Timestamp  time.Time
	Expiration time.Time
	Size       int64
}

// OperationCache provides caching for AWS operation results
type OperationCache struct {
	mu          sync.RWMutex
	entries     map[string]*CacheEntry
	maxSize     int64      // Maximum cache size in bytes
	currentSize int64      // Current cache size in bytes
	ttl         time.Duration
	hitCount    int64
	missCount   int64
	evictCount  int64
}

// NewOperationCache creates a new operation cache
func NewOperationCache(maxSizeMB int64, ttl time.Duration) *OperationCache {
	return &OperationCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSizeMB * 1024 * 1024, // Convert MB to bytes
		ttl:     ttl,
	}
}

// generateKey creates a cache key from service, operation, and parameters
func (c *OperationCache) generateKey(service, operation string, params interface{}) string {
	// Create a unique key based on service, operation, and parameters
	data := map[string]interface{}{
		"service":   service,
		"operation": operation,
		"params":    params,
	}
	
	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return fmt.Sprintf("%s:%s:%x", service, operation, hash)
}

// Get retrieves a value from the cache
func (c *OperationCache) Get(ctx context.Context, service, operation string, params interface{}) (interface{}, bool) {
	key := c.generateKey(service, operation, params)
	
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.entries[key]
	if !exists {
		c.missCount++
		return nil, false
	}
	
	// Check if entry has expired
	if time.Now().After(entry.Expiration) {
		c.missCount++
		return nil, false
	}
	
	c.hitCount++
	return entry.Value, true
}

// Set stores a value in the cache
func (c *OperationCache) Set(ctx context.Context, service, operation string, params, value interface{}) error {
	key := c.generateKey(service, operation, params)
	
	// Calculate size of the value
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	size := int64(len(jsonData))
	
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Check if we need to evict entries to make room
	if c.currentSize+size > c.maxSize {
		c.evictOldestEntries(size)
	}
	
	// Create new entry
	entry := &CacheEntry{
		Key:        key,
		Value:      value,
		Timestamp:  time.Now(),
		Expiration: time.Now().Add(c.ttl),
		Size:       size,
	}
	
	// Update existing entry if it exists
	if oldEntry, exists := c.entries[key]; exists {
		c.currentSize -= oldEntry.Size
	}
	
	c.entries[key] = entry
	c.currentSize += size
	
	return nil
}

// evictOldestEntries removes the oldest entries to make room for new data
func (c *OperationCache) evictOldestEntries(requiredSize int64) {
	// Find and remove oldest entries until we have enough space
	for c.currentSize+requiredSize > c.maxSize && len(c.entries) > 0 {
		var oldestKey string
		var oldestTime time.Time
		
		// Find the oldest entry
		for key, entry := range c.entries {
			if oldestKey == "" || entry.Timestamp.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.Timestamp
			}
		}
		
		// Remove the oldest entry
		if oldestKey != "" {
			if entry, exists := c.entries[oldestKey]; exists {
				c.currentSize -= entry.Size
				delete(c.entries, oldestKey)
				c.evictCount++
			}
		}
	}
}

// Clear removes all entries from the cache
func (c *OperationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.entries = make(map[string]*CacheEntry)
	c.currentSize = 0
}

// Stats returns cache statistics
func (c *OperationCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	var validEntries int
	now := time.Now()
	for _, entry := range c.entries {
		if now.Before(entry.Expiration) {
			validEntries++
		}
	}
	
	hitRate := float64(0)
	if c.hitCount+c.missCount > 0 {
		hitRate = float64(c.hitCount) / float64(c.hitCount+c.missCount) * 100
	}
	
	return CacheStats{
		TotalEntries:  len(c.entries),
		ValidEntries:  validEntries,
		CurrentSizeMB: float64(c.currentSize) / 1024 / 1024,
		MaxSizeMB:     float64(c.maxSize) / 1024 / 1024,
		HitCount:      c.hitCount,
		MissCount:     c.missCount,
		EvictCount:    c.evictCount,
		HitRate:       hitRate,
	}
}

// CacheStats represents cache statistics
type CacheStats struct {
	TotalEntries  int
	ValidEntries  int
	CurrentSizeMB float64
	MaxSizeMB     float64
	HitCount      int64
	MissCount     int64
	EvictCount    int64
	HitRate       float64
}

// Cleanup removes expired entries from the cache
func (c *OperationCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.Expiration) {
			c.currentSize -= entry.Size
			delete(c.entries, key)
		}
	}
}

// StartCleanupRoutine starts a background goroutine to clean up expired entries
func (c *OperationCache) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Cleanup()
			}
		}
	}()
}

// CacheManager manages multiple caches for different operation types
type CacheManager struct {
	caches map[string]*OperationCache
	mu     sync.RWMutex
}

// NewCacheManager creates a new cache manager
func NewCacheManager() *CacheManager {
	return &CacheManager{
		caches: make(map[string]*OperationCache),
	}
}

// GetCache returns a cache for a specific operation type
func (m *CacheManager) GetCache(operationType string) *OperationCache {
	m.mu.RLock()
	cache, exists := m.caches[operationType]
	m.mu.RUnlock()
	
	if exists {
		return cache
	}
	
	// Create a new cache with default settings
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check again in case another goroutine created it
	if cache, exists := m.caches[operationType]; exists {
		return cache
	}
	
	// Create cache with operation-specific settings
	var maxSize int64
	var ttl time.Duration
	
	switch operationType {
	case "list":
		maxSize = 100 // 100MB for list operations
		ttl = 5 * time.Minute
	case "describe":
		maxSize = 200 // 200MB for describe operations
		ttl = 10 * time.Minute
	case "get":
		maxSize = 50 // 50MB for get operations
		ttl = 15 * time.Minute
	default:
		maxSize = 50 // Default 50MB
		ttl = 5 * time.Minute
	}
	
	cache = NewOperationCache(maxSize, ttl)
	m.caches[operationType] = cache
	
	return cache
}

// ClearAll clears all caches
func (m *CacheManager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for _, cache := range m.caches {
		cache.Clear()
	}
}

// GetStats returns statistics for all caches
func (m *CacheManager) GetStats() map[string]CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := make(map[string]CacheStats)
	for name, cache := range m.caches {
		stats[name] = cache.Stats()
	}
	
	return stats
}
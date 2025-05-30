package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Cache defines the interface for caching discovery and analysis results
type Cache interface {
	// Get retrieves a value from the cache
	Get(ctx context.Context, key string) (interface{}, error)
	
	// Set stores a value in the cache with TTL
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	
	// Delete removes a value from the cache
	Delete(ctx context.Context, key string) error
	
	// Clear removes all values from the cache
	Clear(ctx context.Context) error
	
	// Stats returns cache statistics
	Stats() CacheStats
}

// CacheStats contains cache statistics
type CacheStats struct {
	Hits       int64     `json:"hits"`
	Misses     int64     `json:"misses"`
	Sets       int64     `json:"sets"`
	Deletes    int64     `json:"deletes"`
	Evictions  int64     `json:"evictions"`
	Size       int64     `json:"size"`        // Current size in bytes
	MaxSize    int64     `json:"max_size"`    // Maximum size in bytes
	Items      int       `json:"items"`       // Number of items
	LastEvict  time.Time `json:"last_evict"`
}

// cacheEntry represents a single cache entry
type cacheEntry struct {
	Value      interface{}
	ExpiresAt  time.Time
	Size       int64
	AccessedAt time.Time
	AccessCount int64
}

// MemoryCache implements an in-memory cache with TTL and size limits
type MemoryCache struct {
	entries    map[string]*cacheEntry
	maxSize    int64
	evictPolicy string // LRU, LFU, FIFO
	stats      CacheStats
	mu         sync.RWMutex
	
	// For FIFO eviction
	insertOrder []string
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(maxSize int64, evictPolicy string) *MemoryCache {
	if evictPolicy == "" {
		evictPolicy = "LRU"
	}
	
	return &MemoryCache{
		entries:     make(map[string]*cacheEntry),
		maxSize:     maxSize,
		evictPolicy: evictPolicy,
		insertOrder: []string{},
	}
}

// Get retrieves a value from the cache
func (m *MemoryCache) Get(ctx context.Context, key string) (interface{}, error) {
	m.mu.RLock()
	entry, exists := m.entries[key]
	m.mu.RUnlock()
	
	if !exists {
		m.mu.Lock()
		m.stats.Misses++
		m.mu.Unlock()
		return nil, fmt.Errorf("key not found: %s", key)
	}
	
	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		m.mu.Lock()
		delete(m.entries, key)
		m.removeFromOrder(key)
		m.stats.Size -= entry.Size
		m.stats.Items--
		m.stats.Misses++
		m.mu.Unlock()
		return nil, fmt.Errorf("key expired: %s", key)
	}
	
	// Update access info
	m.mu.Lock()
	entry.AccessedAt = time.Now()
	entry.AccessCount++
	m.stats.Hits++
	m.mu.Unlock()
	
	return entry.Value, nil
}

// Set stores a value in the cache with TTL
func (m *MemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// Calculate size (simplified - just use JSON encoding size)
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to calculate size: %w", err)
	}
	size := int64(len(data))
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Check if we need to evict
	if m.maxSize > 0 {
		// Account for replacing existing entry
		existingSize := int64(0)
		if existing, exists := m.entries[key]; exists {
			existingSize = existing.Size
		}
		
		requiredSpace := size - existingSize
		availableSpace := m.maxSize - m.stats.Size
		
		if requiredSpace > availableSpace {
			// Need to evict
			m.evict(requiredSpace - availableSpace)
		}
	}
	
	// Create new entry
	entry := &cacheEntry{
		Value:       value,
		ExpiresAt:   time.Now().Add(ttl),
		Size:        size,
		AccessedAt:  time.Now(),
		AccessCount: 0,
	}
	
	// Update size accounting
	if existing, exists := m.entries[key]; exists {
		m.stats.Size -= existing.Size
		m.removeFromOrder(key)
	} else {
		m.stats.Items++
	}
	
	m.entries[key] = entry
	m.insertOrder = append(m.insertOrder, key)
	m.stats.Size += size
	m.stats.Sets++
	
	return nil
}

// Delete removes a value from the cache
func (m *MemoryCache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	entry, exists := m.entries[key]
	if !exists {
		return fmt.Errorf("key not found: %s", key)
	}
	
	delete(m.entries, key)
	m.removeFromOrder(key)
	m.stats.Size -= entry.Size
	m.stats.Items--
	m.stats.Deletes++
	
	return nil
}

// Clear removes all values from the cache
func (m *MemoryCache) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.entries = make(map[string]*cacheEntry)
	m.insertOrder = []string{}
	m.stats.Size = 0
	m.stats.Items = 0
	
	return nil
}

// Stats returns cache statistics
func (m *MemoryCache) Stats() CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := m.stats
	stats.MaxSize = m.maxSize
	return stats
}

// evict removes entries to free up space
func (m *MemoryCache) evict(requiredSpace int64) {
	switch m.evictPolicy {
	case "LRU":
		m.evictLRU(requiredSpace)
	case "LFU":
		m.evictLFU(requiredSpace)
	case "FIFO":
		m.evictFIFO(requiredSpace)
	default:
		m.evictLRU(requiredSpace) // Default to LRU
	}
	
	m.stats.LastEvict = time.Now()
}

// evictLRU evicts least recently used entries
func (m *MemoryCache) evictLRU(requiredSpace int64) {
	// Find LRU entries
	type entryInfo struct {
		key        string
		accessedAt time.Time
		size       int64
	}
	
	entries := []entryInfo{}
	for key, entry := range m.entries {
		entries = append(entries, entryInfo{
			key:        key,
			accessedAt: entry.AccessedAt,
			size:       entry.Size,
		})
	}
	
	// Sort by access time (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].accessedAt.Before(entries[i].accessedAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	
	// Evict until we have enough space
	freedSpace := int64(0)
	for _, info := range entries {
		if freedSpace >= requiredSpace {
			break
		}
		
		delete(m.entries, info.key)
		m.removeFromOrder(info.key)
		freedSpace += info.size
		m.stats.Size -= info.size
		m.stats.Items--
		m.stats.Evictions++
	}
}

// evictLFU evicts least frequently used entries
func (m *MemoryCache) evictLFU(requiredSpace int64) {
	// Find LFU entries
	type entryInfo struct {
		key         string
		accessCount int64
		size        int64
	}
	
	entries := []entryInfo{}
	for key, entry := range m.entries {
		entries = append(entries, entryInfo{
			key:         key,
			accessCount: entry.AccessCount,
			size:        entry.Size,
		})
	}
	
	// Sort by access count (lowest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].accessCount < entries[i].accessCount {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	
	// Evict until we have enough space
	freedSpace := int64(0)
	for _, info := range entries {
		if freedSpace >= requiredSpace {
			break
		}
		
		delete(m.entries, info.key)
		m.removeFromOrder(info.key)
		freedSpace += info.size
		m.stats.Size -= info.size
		m.stats.Items--
		m.stats.Evictions++
	}
}

// evictFIFO evicts first-in-first-out entries
func (m *MemoryCache) evictFIFO(requiredSpace int64) {
	freedSpace := int64(0)
	toRemove := []string{}
	
	for _, key := range m.insertOrder {
		if freedSpace >= requiredSpace {
			break
		}
		
		if entry, exists := m.entries[key]; exists {
			freedSpace += entry.Size
			toRemove = append(toRemove, key)
		}
	}
	
	// Remove entries
	for _, key := range toRemove {
		if entry, exists := m.entries[key]; exists {
			delete(m.entries, key)
			m.stats.Size -= entry.Size
			m.stats.Items--
			m.stats.Evictions++
		}
	}
	
	// Update insert order
	if len(toRemove) > 0 {
		m.insertOrder = m.insertOrder[len(toRemove):]
	}
}

// removeFromOrder removes a key from the insert order list
func (m *MemoryCache) removeFromOrder(key string) {
	for i, k := range m.insertOrder {
		if k == key {
			m.insertOrder = append(m.insertOrder[:i], m.insertOrder[i+1:]...)
			break
		}
	}
}

// CacheKeyBuilder helps build consistent cache keys
type CacheKeyBuilder struct {
	prefix string
}

// NewCacheKeyBuilder creates a new cache key builder
func NewCacheKeyBuilder(prefix string) *CacheKeyBuilder {
	return &CacheKeyBuilder{prefix: prefix}
}

// Discovery builds a key for discovery results
func (b *CacheKeyBuilder) Discovery(provider string, sources ...string) string {
	key := fmt.Sprintf("%s:discovery:%s", b.prefix, provider)
	for _, source := range sources {
		key += ":" + source
	}
	return key
}

// Analysis builds a key for analysis results
func (b *CacheKeyBuilder) Analysis(provider string, version string) string {
	return fmt.Sprintf("%s:analysis:%s:%s", b.prefix, provider, version)
}

// Generation builds a key for generation results
func (b *CacheKeyBuilder) Generation(provider string, template string) string {
	return fmt.Sprintf("%s:generation:%s:%s", b.prefix, provider, template)
}
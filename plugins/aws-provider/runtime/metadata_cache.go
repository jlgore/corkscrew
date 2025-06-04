package runtime

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// MetadataCache provides thread-safe caching for service metadata
type MetadataCache struct {
	mu              sync.RWMutex
	resourceTypes   map[string][]string
	permissions     map[string][]string
	rateLimits      map[string]rate.Limit
	burstLimits     map[string]int
	lastAccess      map[string]time.Time
	ttl             time.Duration
	maxEntries      int
}

// NewMetadataCache creates a new metadata cache
func NewMetadataCache(ttl time.Duration, maxEntries int) *MetadataCache {
	return &MetadataCache{
		resourceTypes: make(map[string][]string),
		permissions:   make(map[string][]string),
		rateLimits:    make(map[string]rate.Limit),
		burstLimits:   make(map[string]int),
		lastAccess:    make(map[string]time.Time),
		ttl:           ttl,
		maxEntries:    maxEntries,
	}
}

// GetResourceTypes retrieves cached resource types
func (c *MetadataCache) GetResourceTypes(service string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if types, exists := c.resourceTypes[service]; exists {
		if c.isValid(service) {
			c.updateAccessLocked(service)
			return types, true
		}
	}
	return nil, false
}

// SetResourceTypes caches resource types
func (c *MetadataCache) SetResourceTypes(service string, types []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeededLocked()
	c.resourceTypes[service] = types
	c.lastAccess[service] = time.Now()
}

// GetPermissions retrieves cached permissions
func (c *MetadataCache) GetPermissions(service string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if perms, exists := c.permissions[service]; exists {
		if c.isValid(service) {
			c.updateAccessLocked(service)
			return perms, true
		}
	}
	return nil, false
}

// SetPermissions caches permissions
func (c *MetadataCache) SetPermissions(service string, perms []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeededLocked()
	c.permissions[service] = perms
	c.lastAccess[service] = time.Now()
}

// GetRateLimit retrieves cached rate limit
func (c *MetadataCache) GetRateLimit(service string) (rate.Limit, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit, exists := c.rateLimits[service]; exists {
		if c.isValid(service) {
			c.updateAccessLocked(service)
			return limit, true
		}
	}
	return 0, false
}

// SetRateLimit caches rate limit
func (c *MetadataCache) SetRateLimit(service string, limit rate.Limit) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeededLocked()
	c.rateLimits[service] = limit
	c.lastAccess[service] = time.Now()
}

// GetBurstLimit retrieves cached burst limit
func (c *MetadataCache) GetBurstLimit(service string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit, exists := c.burstLimits[service]; exists {
		if c.isValid(service) {
			c.updateAccessLocked(service)
			return limit, true
		}
	}
	return 0, false
}

// SetBurstLimit caches burst limit
func (c *MetadataCache) SetBurstLimit(service string, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictIfNeededLocked()
	c.burstLimits[service] = limit
	c.lastAccess[service] = time.Now()
}

// Clear removes all cached entries
func (c *MetadataCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.resourceTypes = make(map[string][]string)
	c.permissions = make(map[string][]string)
	c.rateLimits = make(map[string]rate.Limit)
	c.burstLimits = make(map[string]int)
	c.lastAccess = make(map[string]time.Time)
}

// Stats returns cache statistics
func (c *MetadataCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		ResourceTypes: len(c.resourceTypes),
		Permissions:   len(c.permissions),
		RateLimits:    len(c.rateLimits),
		BurstLimits:   len(c.burstLimits),
		TotalEntries:  len(c.lastAccess),
		MaxEntries:    c.maxEntries,
		TTL:           c.ttl,
	}
}

// CacheStats contains cache statistics
type CacheStats struct {
	ResourceTypes int
	Permissions   int
	RateLimits    int
	BurstLimits   int
	TotalEntries  int
	MaxEntries    int
	TTL           time.Duration
}

// Private methods (must be called with lock held)

func (c *MetadataCache) isValid(service string) bool {
	if lastAccess, exists := c.lastAccess[service]; exists {
		return time.Since(lastAccess) < c.ttl
	}
	return false
}

func (c *MetadataCache) updateAccessLocked(service string) {
	c.lastAccess[service] = time.Now()
}

func (c *MetadataCache) evictIfNeededLocked() {
	if len(c.lastAccess) < c.maxEntries {
		return
	}

	// Find oldest entry
	var oldest string
	var oldestTime time.Time

	for service, accessTime := range c.lastAccess {
		if oldest == "" || accessTime.Before(oldestTime) {
			oldest = service
			oldestTime = accessTime
		}
	}

	// Evict oldest
	if oldest != "" {
		delete(c.resourceTypes, oldest)
		delete(c.permissions, oldest)
		delete(c.rateLimits, oldest)
		delete(c.burstLimits, oldest)
		delete(c.lastAccess, oldest)
	}
}

// Invalidate removes a specific service from cache
func (c *MetadataCache) Invalidate(service string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.resourceTypes, service)
	delete(c.permissions, service)
	delete(c.rateLimits, service)
	delete(c.burstLimits, service)
	delete(c.lastAccess, service)
}

// InvalidateExpired removes all expired entries
func (c *MetadataCache) InvalidateExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	expired := 0

	for service, lastAccess := range c.lastAccess {
		if now.Sub(lastAccess) > c.ttl {
			delete(c.resourceTypes, service)
			delete(c.permissions, service)
			delete(c.rateLimits, service)
			delete(c.burstLimits, service)
			delete(c.lastAccess, service)
			expired++
		}
	}

	return expired
}
package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// DiscoveryCache manages caching of discovery results
type DiscoveryCache struct {
	mu           sync.RWMutex
	cacheDir     string
	cacheTTL     time.Duration
	data         *CacheData
	lastLoad     time.Time
	manualOverrides map[string]*pb.ServiceInfo
}

// CacheData represents the structure of cached discovery data
type CacheData struct {
	Version     int                          `json:"version"`
	CachedAt    time.Time                   `json:"cached_at"`
	ExpiresAt   time.Time                   `json:"expires_at"`
	Source      string                      `json:"source"`
	Services    []*pb.ServiceInfo           `json:"services"`
	ServiceCount int                        `json:"service_count"`
	Metadata    map[string]interface{}      `json:"metadata,omitempty"`
	// Add checksums for integrity verification
	GitHubETag  string                      `json:"github_etag,omitempty"`
	Checksum    string                      `json:"checksum,omitempty"`
}

// CacheConfig configures the discovery cache
type CacheConfig struct {
	CacheDir        string
	TTL             time.Duration
	MaxCacheSize    int64
	EnableChecksum  bool
	AutoRefresh     bool
	RefreshInterval time.Duration
}

// NewDiscoveryCache creates a new discovery cache
func NewDiscoveryCache(config CacheConfig) *DiscoveryCache {
	if config.CacheDir == "" {
		config.CacheDir = "./registry/cache"
	}
	if config.TTL == 0 {
		config.TTL = 24 * time.Hour
	}

	// Ensure cache directory exists
	os.MkdirAll(config.CacheDir, 0755)

	cache := &DiscoveryCache{
		cacheDir:        config.CacheDir,
		cacheTTL:        config.TTL,
		manualOverrides: make(map[string]*pb.ServiceInfo),
	}

	return cache
}

// LoadCache loads cached discovery data if it's fresh
func (c *DiscoveryCache) LoadCache() (*CacheData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cachePath := filepath.Join(c.cacheDir, "discovery_cache.json")
	
	// Check if cache file exists
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cache file does not exist")
	}

	// Read cache file
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cacheData CacheData
	if err := json.Unmarshal(data, &cacheData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache data: %w", err)
	}

	// Check if cache is still fresh
	if time.Now().After(cacheData.ExpiresAt) {
		return nil, fmt.Errorf("cache has expired (expires: %v, now: %v)", 
			cacheData.ExpiresAt, time.Now())
	}

	// Verify cache integrity if checksum is present
	if cacheData.Checksum != "" {
		if err := c.verifyChecksum(&cacheData); err != nil {
			return nil, fmt.Errorf("cache integrity check failed: %w", err)
		}
	}

	c.data = &cacheData
	c.lastLoad = time.Now()

	return &cacheData, nil
}

// SaveCache saves discovery results to cache
func (c *DiscoveryCache) SaveCache(services []*pb.ServiceInfo, source string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cacheData := CacheData{
		Version:      1,
		CachedAt:     now,
		ExpiresAt:    now.Add(c.cacheTTL),
		Source:       source,
		Services:     services,
		ServiceCount: len(services),
		Metadata: map[string]interface{}{
			"corkscrew_version": "v1.0",
			"cache_ttl_hours":   c.cacheTTL.Hours(),
		},
	}

	// Calculate checksum for integrity
	if checksum, err := c.calculateChecksum(&cacheData); err == nil {
		cacheData.Checksum = checksum
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(cacheData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	// Write to cache file
	cachePath := filepath.Join(c.cacheDir, "discovery_cache.json")
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Also save a backup with timestamp
	backupPath := filepath.Join(c.cacheDir, fmt.Sprintf("discovery_cache_%s.json", 
		now.Format("2006-01-02_15-04-05")))
	os.WriteFile(backupPath, data, 0644) // Best effort, ignore errors

	c.data = &cacheData
	
	return nil
}

// IsFresh checks if the cached data is still fresh
func (c *DiscoveryCache) IsFresh() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return false
	}

	return time.Now().Before(c.data.ExpiresAt)
}

// GetCachedServices returns cached services if fresh
func (c *DiscoveryCache) GetCachedServices() ([]*pb.ServiceInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return nil, fmt.Errorf("no cached data available")
	}

	if !c.IsFresh() {
		return nil, fmt.Errorf("cached data has expired")
	}

	return c.data.Services, nil
}

// AddManualOverride adds a manual override for a service
func (c *DiscoveryCache) AddManualOverride(serviceName string, serviceInfo *pb.ServiceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.manualOverrides[serviceName] = serviceInfo
}

// LoadManualOverrides loads manual overrides from file
func (c *DiscoveryCache) LoadManualOverrides() error {
	overridePath := filepath.Join(c.cacheDir, "manual_overrides.json")
	
	if _, err := os.Stat(overridePath); os.IsNotExist(err) {
		return nil // No overrides file, that's okay
	}

	data, err := os.ReadFile(overridePath)
	if err != nil {
		return fmt.Errorf("failed to read overrides file: %w", err)
	}

	var overrides map[string]*pb.ServiceInfo
	if err := json.Unmarshal(data, &overrides); err != nil {
		return fmt.Errorf("failed to unmarshal overrides: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	
	for name, info := range overrides {
		c.manualOverrides[name] = info
	}

	return nil
}

// SaveManualOverrides saves manual overrides to file
func (c *DiscoveryCache) SaveManualOverrides() error {
	c.mu.RLock()
	overrides := make(map[string]*pb.ServiceInfo)
	for k, v := range c.manualOverrides {
		overrides[k] = v
	}
	c.mu.RUnlock()

	if len(overrides) == 0 {
		return nil // Nothing to save
	}

	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal overrides: %w", err)
	}

	overridePath := filepath.Join(c.cacheDir, "manual_overrides.json")
	return os.WriteFile(overridePath, data, 0644)
}

// MergeWithOverrides merges discovered services with manual overrides
func (c *DiscoveryCache) MergeWithOverrides(services []*pb.ServiceInfo) []*pb.ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.manualOverrides) == 0 {
		return services
	}

	// Create a map for quick lookup
	serviceMap := make(map[string]*pb.ServiceInfo)
	for _, service := range services {
		serviceMap[service.Name] = service
	}

	// Apply overrides
	for name, override := range c.manualOverrides {
		serviceMap[name] = override
	}

	// Convert back to slice
	var result []*pb.ServiceInfo
	for _, service := range serviceMap {
		result = append(result, service)
	}

	return result
}

// GetCacheStats returns statistics about the cache
func (c *DiscoveryCache) GetCacheStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"cache_exists": c.data != nil,
		"is_fresh":     c.IsFresh(),
		"last_load":    c.lastLoad,
		"manual_overrides_count": len(c.manualOverrides),
		"cache_dir":    c.cacheDir,
		"ttl_hours":    c.cacheTTL.Hours(),
	}

	if c.data != nil {
		stats["cached_at"] = c.data.CachedAt
		stats["expires_at"] = c.data.ExpiresAt
		stats["service_count"] = c.data.ServiceCount
		stats["source"] = c.data.Source
		stats["version"] = c.data.Version
		
		// Calculate age
		age := time.Since(c.data.CachedAt)
		stats["age_hours"] = age.Hours()
		stats["remaining_hours"] = time.Until(c.data.ExpiresAt).Hours()
	}

	return stats
}

// CleanupOldCaches removes old cache backup files
func (c *DiscoveryCache) CleanupOldCaches(maxAge time.Duration) error {
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only clean backup files (with timestamps)
		if entry.Name() != "discovery_cache.json" && 
		   entry.Name() != "manual_overrides.json" {
			
			info, err := entry.Info()
			if err != nil {
				continue
			}

			if info.ModTime().Before(cutoff) {
				cachePath := filepath.Join(c.cacheDir, entry.Name())
				os.Remove(cachePath) // Best effort
			}
		}
	}

	return nil
}

// InvalidateCache removes the cache file
func (c *DiscoveryCache) InvalidateCache() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cachePath := filepath.Join(c.cacheDir, "discovery_cache.json")
	
	if err := os.Remove(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	c.data = nil
	return nil
}

// Helper methods

func (c *DiscoveryCache) calculateChecksum(data *CacheData) (string, error) {
	// Simple checksum based on service count and cached time
	// In a real implementation, you might want to use a proper hash
	checksum := fmt.Sprintf("%d-%d-%s", 
		data.ServiceCount, 
		data.CachedAt.Unix(), 
		data.Source)
	return checksum, nil
}

func (c *DiscoveryCache) verifyChecksum(data *CacheData) error {
	expected, err := c.calculateChecksum(data)
	if err != nil {
		return err
	}

	if data.Checksum != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", 
			expected, data.Checksum)
	}

	return nil
}
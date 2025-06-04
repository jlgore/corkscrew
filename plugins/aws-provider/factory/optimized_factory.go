package factory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jlgore/corkscrew/plugins/aws-provider/registry"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// OptimizedClientFactory provides high-performance client creation with multiple optimizations
type OptimizedClientFactory struct {
	config          aws.Config
	registry        registry.DynamicServiceRegistry
	reflectionClient *ReflectionClient
	dynamicLoader   *DynamicLoader
	
	// Caching layers
	clientCache     sync.Map // Primary client cache
	warmCache       sync.Map // Pre-warmed client cache
	ttlCache        *TTLCache // Time-based cache
	
	// Performance optimizations
	singleFlight    singleflight.Group // Prevent duplicate client creation
	rateLimiters    sync.Map // Per-service rate limiters
	metrics         *FactoryMetrics
	
	// Configuration
	options         FactoryOptions
}

// FactoryOptions configures the optimized factory
type FactoryOptions struct {
	EnableCaching        bool
	CacheTTL            time.Duration
	EnablePreWarming    bool
	PreWarmServices     []string
	EnableMetrics       bool
	MaxConcurrentCreates int
	PluginDirectory     string
}

// FactoryMetrics tracks performance metrics
type FactoryMetrics struct {
	CacheHits          atomic.Uint64
	CacheMisses        atomic.Uint64
	CreationAttempts   atomic.Uint64
	CreationSuccesses  atomic.Uint64
	CreationFailures   atomic.Uint64
	ReflectionUses     atomic.Uint64
	AverageCreateTime  atomic.Uint64 // nanoseconds
}

// TTLCache provides time-based caching
type TTLCache struct {
	entries sync.Map
	mu      sync.RWMutex
}

type ttlEntry struct {
	value     interface{}
	expiresAt time.Time
}

// NewOptimizedClientFactory creates a new optimized client factory
func NewOptimizedClientFactory(config aws.Config, registry registry.DynamicServiceRegistry, options FactoryOptions) *OptimizedClientFactory {
	factory := &OptimizedClientFactory{
		config:           config,
		registry:         registry,
		reflectionClient: NewReflectionClient(registry),
		dynamicLoader:    NewDynamicLoader(options.PluginDirectory),
		ttlCache:         &TTLCache{},
		metrics:          &FactoryMetrics{},
		options:          options,
	}

	// Pre-warm cache if configured
	if options.EnablePreWarming && len(options.PreWarmServices) > 0 {
		go factory.preWarmCache(options.PreWarmServices)
	}

	// Start cache cleanup routine
	if options.EnableCaching {
		go factory.startCacheCleanup()
	}

	return factory
}

// CreateClient creates or retrieves a client with multiple optimization strategies
func (f *OptimizedClientFactory) CreateClient(ctx context.Context, serviceName string) (interface{}, error) {
	start := time.Now()
	defer func() {
		f.updateMetrics(time.Since(start))
	}()

	f.metrics.CreationAttempts.Add(1)

	// Check primary cache first
	if f.options.EnableCaching {
		if client := f.getFromCache(serviceName); client != nil {
			f.metrics.CacheHits.Add(1)
			return client, nil
		}
		f.metrics.CacheMisses.Add(1)
	}

	// Use singleflight to prevent duplicate creation
	result, err, _ := f.singleFlight.Do(serviceName, func() (interface{}, error) {
		return f.createClientInternal(ctx, serviceName)
	})

	if err != nil {
		f.metrics.CreationFailures.Add(1)
		return nil, err
	}

	f.metrics.CreationSuccesses.Add(1)
	
	// Cache the result
	if f.options.EnableCaching && result != nil {
		f.cacheClient(serviceName, result)
	}

	return result, nil
}

// createClientInternal performs the actual client creation
func (f *OptimizedClientFactory) createClientInternal(ctx context.Context, serviceName string) (interface{}, error) {
	// Get service definition
	service, exists := f.registry.GetService(serviceName)
	if !exists {
		// Try reflection-based creation
		f.metrics.ReflectionUses.Add(1)
		result, err := f.reflectionClient.CreateClient(ctx, serviceName, f.config)
		if err != nil {
			return nil, err
		}
		return result.Client, nil
	}

	// Check rate limits
	if err := f.checkRateLimit(serviceName, service); err != nil {
		return nil, fmt.Errorf("rate limit exceeded for %s: %w", serviceName, err)
	}

	// Try direct creation first (if we have generated code)
	if client := f.tryDirectCreation(serviceName); client != nil {
		return client, nil
	}

	// Fall back to reflection
	f.metrics.ReflectionUses.Add(1)
	result, err := f.reflectionClient.CreateClient(ctx, serviceName, f.config)
	if err != nil {
		return nil, err
	}

	return result.Client, nil
}

// tryDirectCreation attempts to create a client without reflection
func (f *OptimizedClientFactory) tryDirectCreation(serviceName string) interface{} {
	// This would be implemented by generated code
	// For now, return nil to trigger reflection fallback
	return nil
}

// Cache management methods

func (f *OptimizedClientFactory) getFromCache(serviceName string) interface{} {
	// Check warm cache first
	if client, ok := f.warmCache.Load(serviceName); ok {
		return client
	}

	// Check primary cache
	if client, ok := f.clientCache.Load(serviceName); ok {
		return client
	}

	// Check TTL cache
	if f.ttlCache != nil {
		return f.ttlCache.Get(serviceName)
	}

	return nil
}

func (f *OptimizedClientFactory) cacheClient(serviceName string, client interface{}) {
	f.clientCache.Store(serviceName, client)
	
	if f.options.CacheTTL > 0 {
		f.ttlCache.Set(serviceName, client, f.options.CacheTTL)
	}
}

// preWarmCache pre-creates clients for commonly used services
func (f *OptimizedClientFactory) preWarmCache(services []string) {
	ctx := context.Background()
	
	for _, serviceName := range services {
		client, err := f.createClientInternal(ctx, serviceName)
		if err == nil {
			f.warmCache.Store(serviceName, client)
		}
	}
}

// startCacheCleanup periodically cleans expired cache entries
func (f *OptimizedClientFactory) startCacheCleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		f.ttlCache.Cleanup()
	}
}

// Rate limiting methods

func (f *OptimizedClientFactory) checkRateLimit(serviceName string, service *registry.ServiceDefinition) error {
	limiter := f.getRateLimiter(serviceName, service)
	
	if !limiter.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}

	return nil
}

func (f *OptimizedClientFactory) getRateLimiter(serviceName string, service *registry.ServiceDefinition) *rate.Limiter {
	if limiter, ok := f.rateLimiters.Load(serviceName); ok {
		return limiter.(*rate.Limiter)
	}

	// Create new limiter
	limiter := rate.NewLimiter(service.RateLimit, service.BurstLimit)
	f.rateLimiters.Store(serviceName, limiter)
	
	return limiter
}

// Metrics methods

func (f *OptimizedClientFactory) updateMetrics(duration time.Duration) {
	if !f.options.EnableMetrics {
		return
	}

	// Update average creation time (simple moving average)
	currentAvg := f.metrics.AverageCreateTime.Load()
	newAvg := (currentAvg + uint64(duration.Nanoseconds())) / 2
	f.metrics.AverageCreateTime.Store(newAvg)
}

// GetMetrics returns current factory metrics
func (f *OptimizedClientFactory) GetMetrics() FactoryMetrics {
	return FactoryMetrics{
		CacheHits:         atomic.Uint64{},
		CacheMisses:       atomic.Uint64{},
		CreationAttempts:  atomic.Uint64{},
		CreationSuccesses: atomic.Uint64{},
		CreationFailures:  atomic.Uint64{},
		ReflectionUses:    atomic.Uint64{},
		AverageCreateTime: atomic.Uint64{},
	}
}

// Batch operations

// CreateClients creates multiple clients in parallel
func (f *OptimizedClientFactory) CreateClients(ctx context.Context, serviceNames []string) (map[string]interface{}, error) {
	results := make(map[string]interface{})
	errors := make(map[string]error)
	
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	// Limit concurrency
	sem := make(chan struct{}, f.options.MaxConcurrentCreates)
	if f.options.MaxConcurrentCreates == 0 {
		sem = make(chan struct{}, 10) // Default
	}

	for _, serviceName := range serviceNames {
		wg.Add(1)
		go func(svc string) {
			defer wg.Done()
			
			sem <- struct{}{}
			defer func() { <-sem }()

			client, err := f.CreateClient(ctx, svc)
			
			mu.Lock()
			if err != nil {
				errors[svc] = err
			} else {
				results[svc] = client
			}
			mu.Unlock()
		}(serviceName)
	}

	wg.Wait()

	if len(errors) > 0 {
		return results, fmt.Errorf("failed to create some clients: %v", errors)
	}

	return results, nil
}

// Management methods

// InvalidateCache removes a client from all caches
func (f *OptimizedClientFactory) InvalidateCache(serviceName string) {
	f.clientCache.Delete(serviceName)
	f.warmCache.Delete(serviceName)
	f.ttlCache.Delete(serviceName)
}

// ClearAllCaches clears all cached clients
func (f *OptimizedClientFactory) ClearAllCaches() {
	f.clientCache = sync.Map{}
	f.warmCache = sync.Map{}
	f.ttlCache = &TTLCache{}
	f.reflectionClient.ClearCache()
}

// UpdateRegistry updates the service registry
func (f *OptimizedClientFactory) UpdateRegistry(registry registry.DynamicServiceRegistry) {
	f.registry = registry
	f.reflectionClient = NewReflectionClient(registry)
	f.ClearAllCaches()
}

// TTLCache implementation

func (tc *TTLCache) Get(key string) interface{} {
	if entry, ok := tc.entries.Load(key); ok {
		e := entry.(*ttlEntry)
		if time.Now().Before(e.expiresAt) {
			return e.value
		}
		tc.entries.Delete(key)
	}
	return nil
}

func (tc *TTLCache) Set(key string, value interface{}, ttl time.Duration) {
	tc.entries.Store(key, &ttlEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
}

func (tc *TTLCache) Delete(key string) {
	tc.entries.Delete(key)
}

func (tc *TTLCache) Cleanup() {
	now := time.Now()
	tc.entries.Range(func(key, value interface{}) bool {
		if e := value.(*ttlEntry); now.After(e.expiresAt) {
			tc.entries.Delete(key)
		}
		return true
	})
}
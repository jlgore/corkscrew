package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ClientKey represents a unique identifier for a client
type ClientKey struct {
	Service string
	Region  string
	Profile string
	RoleARN string
}

// String returns a string representation of the client key
func (k ClientKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.Service, k.Region, k.Profile, k.RoleARN)
}

// PooledClient represents a pooled AWS client
type PooledClient struct {
	Client      interface{}
	Key         ClientKey
	Config      aws.Config
	CreatedAt   time.Time
	LastUsedAt  time.Time
	UseCount    int64
	mu          sync.Mutex
}

// Use marks the client as used
func (pc *PooledClient) Use() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.LastUsedAt = time.Now()
	pc.UseCount++
}

// ClientPool manages a pool of AWS service clients
type ClientPool struct {
	mu          sync.RWMutex
	clients     map[string]*PooledClient
	maxClients  int
	maxIdleTime time.Duration
	factory     ClientFactory
}

// ClientFactory is a function that creates AWS service clients
type ClientFactory func(ctx context.Context, key ClientKey, cfg aws.Config) (interface{}, error)

// NewClientPool creates a new client pool
func NewClientPool(maxClients int, maxIdleTime time.Duration, factory ClientFactory) *ClientPool {
	return &ClientPool{
		clients:     make(map[string]*PooledClient),
		maxClients:  maxClients,
		maxIdleTime: maxIdleTime,
		factory:     factory,
	}
}

// Get retrieves or creates a client from the pool
func (p *ClientPool) Get(ctx context.Context, key ClientKey, cfg aws.Config) (interface{}, error) {
	keyStr := key.String()

	// Try to get existing client
	p.mu.RLock()
	if pooledClient, exists := p.clients[keyStr]; exists {
		p.mu.RUnlock()
		pooledClient.Use()
		return pooledClient.Client, nil
	}
	p.mu.RUnlock()

	// Need to create new client
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check again in case another goroutine created it
	if pooledClient, exists := p.clients[keyStr]; exists {
		pooledClient.Use()
		return pooledClient.Client, nil
	}

	// Check pool size limit
	if len(p.clients) >= p.maxClients {
		// Evict least recently used client
		p.evictLRU()
	}

	// Create new client
	client, err := p.factory(ctx, key, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Add to pool
	pooledClient := &PooledClient{
		Client:     client,
		Key:        key,
		Config:     cfg,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		UseCount:   1,
	}

	p.clients[keyStr] = pooledClient
	return client, nil
}

// evictLRU removes the least recently used client
func (p *ClientPool) evictLRU() {
	var lruKey string
	var lruTime time.Time

	for key, client := range p.clients {
		if lruKey == "" || client.LastUsedAt.Before(lruTime) {
			lruKey = key
			lruTime = client.LastUsedAt
		}
	}

	if lruKey != "" {
		delete(p.clients, lruKey)
	}
}

// Cleanup removes idle clients from the pool
func (p *ClientPool) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, client := range p.clients {
		if now.Sub(client.LastUsedAt) > p.maxIdleTime {
			delete(p.clients, key)
		}
	}
}

// StartCleanupRoutine starts a background cleanup routine
func (p *ClientPool) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.Cleanup()
			}
		}
	}()
}

// Stats returns pool statistics
func (p *ClientPool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		TotalClients: len(p.clients),
		MaxClients:   p.maxClients,
	}

	now := time.Now()
	for _, client := range p.clients {
		stats.TotalUseCount += client.UseCount
		if now.Sub(client.LastUsedAt) <= time.Minute {
			stats.ActiveClients++
		}
	}

	return stats
}

// PoolStats represents pool statistics
type PoolStats struct {
	TotalClients  int
	ActiveClients int
	MaxClients    int
	TotalUseCount int64
}

// Clear removes all clients from the pool
func (p *ClientPool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = make(map[string]*PooledClient)
}

// MultiServicePool manages pools for multiple AWS services
type MultiServicePool struct {
	pools       map[string]*ClientPool
	mu          sync.RWMutex
	maxClients  int
	maxIdleTime time.Duration
}

// NewMultiServicePool creates a new multi-service pool
func NewMultiServicePool(maxClientsPerService int, maxIdleTime time.Duration) *MultiServicePool {
	return &MultiServicePool{
		pools:       make(map[string]*ClientPool),
		maxClients:  maxClientsPerService,
		maxIdleTime: maxIdleTime,
	}
}

// GetPool returns the pool for a specific service
func (m *MultiServicePool) GetPool(service string, factory ClientFactory) *ClientPool {
	m.mu.RLock()
	pool, exists := m.pools[service]
	m.mu.RUnlock()

	if exists {
		return pool
	}

	// Create new pool
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check again
	if pool, exists := m.pools[service]; exists {
		return pool
	}

	pool = NewClientPool(m.maxClients, m.maxIdleTime, factory)
	m.pools[service] = pool
	return pool
}

// GetClient retrieves or creates a client for a specific service
func (m *MultiServicePool) GetClient(ctx context.Context, key ClientKey, cfg aws.Config, factory ClientFactory) (interface{}, error) {
	pool := m.GetPool(key.Service, factory)
	return pool.Get(ctx, key, cfg)
}

// StartCleanupRoutines starts cleanup routines for all pools
func (m *MultiServicePool) StartCleanupRoutines(ctx context.Context, interval time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pool := range m.pools {
		pool.StartCleanupRoutine(ctx, interval)
	}
}

// GetAllStats returns statistics for all pools
func (m *MultiServicePool) GetAllStats() map[string]PoolStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for service, pool := range m.pools {
		stats[service] = pool.Stats()
	}
	return stats
}

// ClearAll clears all pools
func (m *MultiServicePool) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, pool := range m.pools {
		pool.Clear()
	}
}
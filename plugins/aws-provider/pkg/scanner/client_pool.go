package scanner

import (
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ClientPool manages a pool of AWS service clients
type ClientPool struct {
	mu          sync.RWMutex
	clients     map[string][]interface{} // service -> client pool
	factory     ClientFactory
	config      aws.Config
	maxPerType  int
	idleTimeout time.Duration
	
	// Metrics
	hits   int64
	misses int64
}

// PooledClient wraps a client with metadata
type PooledClient struct {
	Client     interface{}
	LastUsed   time.Time
	InUse      bool
	Service    string
}

// ClientPoolConfig configures the client pool
type ClientPoolConfig struct {
	MaxClientsPerService int
	IdleTimeout          time.Duration
	CleanupInterval      time.Duration
}

// DefaultClientPoolConfig returns default pool configuration
func DefaultClientPoolConfig() *ClientPoolConfig {
	return &ClientPoolConfig{
		MaxClientsPerService: 5,
		IdleTimeout:          5 * time.Minute,
		CleanupInterval:      1 * time.Minute,
	}
}

// NewClientPool creates a new client pool
func NewClientPool(factory ClientFactory, cfg aws.Config, config *ClientPoolConfig) *ClientPool {
	pool := &ClientPool{
		clients:     make(map[string][]interface{}),
		factory:     factory,
		config:      cfg,
		maxPerType:  config.MaxClientsPerService,
		idleTimeout: config.IdleTimeout,
	}
	
	// Start cleanup routine
	go pool.cleanupRoutine(config.CleanupInterval)
	
	return pool
}

// GetClient retrieves a client from the pool or creates a new one
func (p *ClientPool) GetClient(service string) (interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Check if we have any pooled clients
	if clients, exists := p.clients[service]; exists && len(clients) > 0 {
		// Return the last client (LIFO)
		client := clients[len(clients)-1]
		p.clients[service] = clients[:len(clients)-1]
		p.hits++
		return client, nil
	}
	
	// No pooled client available, create new one
	p.misses++
	client := p.factory.GetClient(service)
	if client == nil {
		return nil, fmt.Errorf("failed to create client for service: %s", service)
	}
	
	return client, nil
}

// ReturnClient returns a client to the pool
func (p *ClientPool) ReturnClient(service string, client interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Check pool size limit
	if len(p.clients[service]) >= p.maxPerType {
		// Pool is full, discard client
		return
	}
	
	// Add client back to pool
	p.clients[service] = append(p.clients[service], client)
}

// cleanupRoutine periodically removes idle clients
func (p *ClientPool) cleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for range ticker.C {
		p.cleanup()
	}
}

// cleanup removes idle clients
func (p *ClientPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// For now, we'll implement a simple strategy:
	// Remove all clients that haven't been used recently
	// In a real implementation, we'd track last usage time
	for service, clients := range p.clients {
		if len(clients) > 1 {
			// Keep only one client per service during cleanup
			p.clients[service] = clients[:1]
		}
	}
}

// GetMetrics returns pool metrics
func (p *ClientPool) GetMetrics() (hits, misses int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.hits, p.misses
}

// Close closes all pooled clients
func (p *ClientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Clear all pools
	p.clients = make(map[string][]interface{})
}

// PooledClientFactory wraps a ClientFactory with pooling
type PooledClientFactory struct {
	pool    *ClientPool
	factory ClientFactory
}

// NewPooledClientFactory creates a new pooled client factory
func NewPooledClientFactory(factory ClientFactory, cfg aws.Config) *PooledClientFactory {
	config := DefaultClientPoolConfig()
	pool := NewClientPool(factory, cfg, config)
	
	return &PooledClientFactory{
		pool:    pool,
		factory: factory,
	}
}

// GetClient retrieves a pooled client
func (f *PooledClientFactory) GetClient(service string) interface{} {
	client, err := f.pool.GetClient(service)
	if err != nil {
		// Fallback to direct creation
		return f.factory.GetClient(service)
	}
	return client
}

// GetAvailableServices delegates to the underlying factory
func (f *PooledClientFactory) GetAvailableServices() []string {
	return f.factory.GetAvailableServices()
}

// ReturnClient returns a client to the pool
func (f *PooledClientFactory) ReturnClient(service string, client interface{}) {
	f.pool.ReturnClient(service, client)
}
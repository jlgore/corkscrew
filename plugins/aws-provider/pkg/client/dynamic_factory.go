package client

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// DynamicClientFactory manages AWS service clients with reflection-based dynamic client creation
type DynamicClientFactory struct {
	config        aws.Config
	mu            sync.RWMutex
	clients       map[string]interface{}
	clientTypeMap map[string]reflect.Type
}

// NewDynamicClientFactory creates a new dynamic client factory
func NewDynamicClientFactory(cfg aws.Config) *DynamicClientFactory {
	return &DynamicClientFactory{
		config:        cfg,
		clients:       make(map[string]interface{}),
		clientTypeMap: make(map[string]reflect.Type),
	}
}

// GetClient returns a client for the specified service, creating it if necessary using reflection
func (cf *DynamicClientFactory) GetClient(serviceName string) (interface{}, error) {
	cf.mu.RLock()
	if client, exists := cf.clients[serviceName]; exists {
		cf.mu.RUnlock()
		return client, nil
	}
	cf.mu.RUnlock()

	// Create client if it doesn't exist
	cf.mu.Lock()
	defer cf.mu.Unlock()

	// Double-check in case another goroutine created it
	if client, exists := cf.clients[serviceName]; exists {
		return client, nil
	}

	client, err := cf.createClientDynamically(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for service %s: %w", serviceName, err)
	}

	if client != nil {
		cf.clients[serviceName] = client
	}

	return client, nil
}

// createClientDynamically creates a new client for the specified service using reflection
func (cf *DynamicClientFactory) createClientDynamically(serviceName string) (interface{}, error) {
	// This implementation requires that the client factory has already been populated
	// with available services through other means (like analyzing the current binary's
	// imported packages or having the services pre-registered)
	
	// Since we can't dynamically import packages at runtime in Go without build-time
	// knowledge, we return an error indicating the service is not available
	// The client factory should be populated with known services at initialization
	
	return nil, fmt.Errorf("service %s not available - dynamic client creation requires pre-registration of service packages", serviceName)
}

// GetAvailableServices returns an empty list since services are discovered dynamically
func (cf *DynamicClientFactory) GetAvailableServices() []string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	
	services := make([]string, 0, len(cf.clients))
	for service := range cf.clients {
		services = append(services, service)
	}
	return services
}

// HasClient checks if a client exists for the given service
func (cf *DynamicClientFactory) HasClient(serviceName string) bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	_, exists := cf.clients[serviceName]
	return exists
}

// ClearCache removes all cached clients
func (cf *DynamicClientFactory) ClearCache() {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	cf.clients = make(map[string]interface{})
}
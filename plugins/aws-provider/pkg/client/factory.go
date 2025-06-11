package client

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ClientFactory manages AWS service clients with reflection-based discovery
// This replaces the hardcoded version that was archived
type ClientFactory struct {
	config            aws.Config
	mu                sync.RWMutex
	clients           map[string]interface{}
	reflectionFactory *ReflectionClientFactory
}

// NewClientFactory creates a new client factory that uses reflection-based discovery
func NewClientFactory(cfg aws.Config) *ClientFactory {
	return &ClientFactory{
		config:            cfg,
		clients:           make(map[string]interface{}),
		reflectionFactory: NewReflectionClientFactory(cfg),
	}
}

// GetClient returns a client for the specified service, creating it if necessary
func (cf *ClientFactory) GetClient(serviceName string) interface{} {
	// Delegate to reflection factory
	return cf.reflectionFactory.GetClient(serviceName)
}

// GetAvailableServices returns services discovered via reflection
func (cf *ClientFactory) GetAvailableServices() []string {
	return cf.reflectionFactory.GetAvailableServices()
}

// HasClient checks if a client exists for the given service
func (cf *ClientFactory) HasClient(serviceName string) bool {
	return cf.reflectionFactory.HasClient(serviceName)
}

// ClearCache removes all cached clients
func (cf *ClientFactory) ClearCache() {
	cf.reflectionFactory.ClearCache()
}

// GetConfig returns the AWS configuration used by this factory
func (cf *ClientFactory) GetConfig() aws.Config {
	return cf.config
}

// SetConfig updates the AWS configuration (useful for cross-region operations)
func (cf *ClientFactory) SetConfig(cfg aws.Config) {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	
	cf.config = cfg
	// Update reflection factory with new config
	cf.reflectionFactory.SetConfig(cfg)
}

// GetServiceCapabilities returns capabilities for all discovered services
func (cf *ClientFactory) GetServiceCapabilities() map[string]*ServiceCapabilities {
	// Return basic capabilities for available services
	capabilities := make(map[string]*ServiceCapabilities)
	for _, serviceName := range cf.GetAvailableServices() {
		capabilities[serviceName] = &ServiceCapabilities{
			ServiceName:        serviceName,
			HasListOps:         true,
			HasDescribeOps:     true,
			HasGetOps:          true,
			SupportsPagination: true,
		}
	}
	return capabilities
}

// ServiceCapabilities represents the discovered capabilities of a service
type ServiceCapabilities struct {
	ServiceName        string
	HasListOps         bool
	HasDescribeOps     bool
	HasGetOps          bool
	SupportsPagination bool
	Operations         []string
}

// GetClientMethodNames returns all method names for a service client (required by scanner.ClientFactory interface)
func (cf *ClientFactory) GetClientMethodNames(serviceName string) []string {
	return cf.reflectionFactory.GetClientMethodNames(serviceName)
}

// GetListOperations returns list/scan operations for a service (required by scanner.ClientFactory interface)
func (cf *ClientFactory) GetListOperations(serviceName string) []string {
	return cf.reflectionFactory.GetListOperations(serviceName)
}

// GetDescribeOperations returns describe/detail operations for a service (required by scanner.ClientFactory interface)
func (cf *ClientFactory) GetDescribeOperations(serviceName string) []string {
	return cf.reflectionFactory.GetDescribeOperations(serviceName)
}
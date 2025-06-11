package client

import (
	"context"
	"log"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ClientConstructor is a function that creates a client for a service
type ClientConstructor func(aws.Config) interface{}

// ClientConstructorRegistry manages global client constructors
type ClientConstructorRegistry struct {
	constructors map[string]ClientConstructor
	mu           sync.RWMutex
}

var globalConstructorRegistry = &ClientConstructorRegistry{
	constructors: make(map[string]ClientConstructor),
}

// RegisterConstructor registers a client constructor for a service
func RegisterConstructor(serviceName string, constructor ClientConstructor) {
	globalConstructorRegistry.mu.Lock()
	defer globalConstructorRegistry.mu.Unlock()
	globalConstructorRegistry.constructors[serviceName] = constructor
}

// GetConstructor retrieves a constructor for a service
func GetConstructor(serviceName string) ClientConstructor {
	globalConstructorRegistry.mu.RLock()
	defer globalConstructorRegistry.mu.RUnlock()
	constructor := globalConstructorRegistry.constructors[serviceName]
	if constructor != nil {
		log.Printf("DEBUG: GetConstructor(%s) - found", serviceName)
	} else {
		log.Printf("DEBUG: GetConstructor(%s) - not found. Available: %v", serviceName, ListRegisteredServices())
	}
	return constructor
}

// ListRegisteredServices returns all services with registered constructors
func ListRegisteredServices() []string {
	globalConstructorRegistry.mu.RLock()
	defer globalConstructorRegistry.mu.RUnlock()
	
	services := make([]string, 0, len(globalConstructorRegistry.constructors))
	for service := range globalConstructorRegistry.constructors {
		services = append(services, service)
	}
	return services
}

// GeneratedClientFactory provides access to the generated client factory
type GeneratedClientFactory interface {
	CreateClient(ctx context.Context, serviceName string) (interface{}, error)
	ListAvailableServices() []string
}

var generatedFactory GeneratedClientFactory

// SetGeneratedFactory sets the global reference to the generated factory
func SetGeneratedFactory(factory GeneratedClientFactory) {
	generatedFactory = factory
}

// GetGeneratedFactory returns the global generated factory
func GetGeneratedFactory() GeneratedClientFactory {
	return generatedFactory
}
package main

import (
	"github.com/jlgore/corkscrew/plugins/aws-provider/discovery"
	"github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
)

// ClientFactoryAdapter adapts the ClientFactory to implement discovery.ClientFactoryInterface
type ClientFactoryAdapter struct {
	factory *client.ClientFactory
}

// NewClientFactoryAdapter creates a new adapter
func NewClientFactoryAdapter(factory *client.ClientFactory) discovery.ClientFactoryInterface {
	return &ClientFactoryAdapter{
		factory: factory,
	}
}

// GetClient implements discovery.ClientFactoryInterface
func (a *ClientFactoryAdapter) GetClient(serviceName string) interface{} {
	if a.factory == nil {
		return nil
	}
	return a.factory.GetClient(serviceName)
}

// GetAvailableServices implements discovery.ClientFactoryInterface
func (a *ClientFactoryAdapter) GetAvailableServices() []string {
	if a.factory == nil {
		return []string{}
	}
	return a.factory.GetAvailableServices()
}
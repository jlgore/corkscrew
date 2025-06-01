package main

import (
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ClientFactory manages Kubernetes client creation and caching
type ClientFactory struct {
	dynamicClient dynamic.Interface
	clientset     kubernetes.Interface
	mu            sync.RWMutex
}

// NewClientFactory creates a new client factory
func NewClientFactory(dynamicClient dynamic.Interface, clientset kubernetes.Interface) *ClientFactory {
	return &ClientFactory{
		dynamicClient: dynamicClient,
		clientset:     clientset,
	}
}

// GetDynamicClient returns the dynamic client
func (c *ClientFactory) GetDynamicClient() dynamic.Interface {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dynamicClient
}

// GetClientset returns the typed clientset
func (c *ClientFactory) GetClientset() kubernetes.Interface {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientset
}
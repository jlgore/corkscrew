package discovery

import (
	"context"
)

// AWSResourceRef represents a lightweight reference to an AWS resource
type AWSResourceRef struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Service  string            `json:"service"`
	Region   string            `json:"region"`
	ARN      string            `json:"arn,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ResourceScanner interface for service-specific resource discovery
type ResourceScanner interface {
	DiscoverAndListServiceResources(ctx context.Context, serviceName string) ([]AWSResourceRef, error)
	ExecuteOperationWithParams(ctx context.Context, serviceName, operationName string, params map[string]interface{}) ([]AWSResourceRef, error)
	GetRegion() string
}

// HierarchicalDiscoverer provides sophisticated resource discovery
type HierarchicalDiscoverer struct {
	resourceScanner ResourceScanner
	debug           bool
}

// NewHierarchicalDiscoverer creates a new hierarchical discoverer
func NewHierarchicalDiscoverer(scanner ResourceScanner, debug bool) *HierarchicalDiscoverer {
	return &HierarchicalDiscoverer{
		resourceScanner: scanner,
		debug:           debug,
	}
}

// DiscoverService discovers all resources for a service using hierarchical approach
func (hd *HierarchicalDiscoverer) DiscoverService(ctx context.Context, serviceName string) ([]AWSResourceRef, error) {
	return hd.resourceScanner.DiscoverAndListServiceResources(ctx, serviceName)
}

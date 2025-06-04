package scanner

import (
	"context"
	pb "github.com/jlgore/corkscrew/internal/proto"
)

// ClientFactory interface for AWS client management
type ClientFactory interface {
	GetClient(serviceName string) interface{}
	GetAvailableServices() []string
	GetClientMethodNames(serviceName string) []string
	GetListOperations(serviceName string) []string
	GetDescribeOperations(serviceName string) []string
	HasClient(serviceName string) bool
}

// ResourceExplorer interface for AWS Resource Explorer integration
type ResourceExplorer interface {
	QueryAllResources(ctx context.Context) ([]*pb.ResourceRef, error)
	QueryByService(ctx context.Context, service string) ([]*pb.ResourceRef, error)
	QueryByResourceType(ctx context.Context, resourceType string) ([]*pb.ResourceRef, error)
	IsHealthy(ctx context.Context) bool
}

// RelationshipExtractor interface for extracting resource relationships
type RelationshipExtractor interface {
	ExtractFromResourceRef(resourceRef *pb.ResourceRef) []*pb.Relationship
	ExtractFromMultipleResources(resources []*pb.ResourceRef) []*pb.Relationship
}

// ServiceRegistry interface for accessing service definitions
type ServiceRegistry interface {
	GetService(name string) (*ScannerServiceDefinition, bool)
	ListServices() []string
}

// ScannerServiceDefinition minimal interface for what scanner needs
type ScannerServiceDefinition struct {
	Name          string
	ResourceTypes []ResourceTypeDefinition
}

// ResourceTypeDefinition defines a resource type
type ResourceTypeDefinition struct {
	Name         string
	ResourceType string
	IDField      string
	ARNPattern   string
}
package discovery

import (
	"context"
	"time"

	pb "github.com/jlgore/corkscrew/internal/proto"
)

// OperationType represents the type of AWS operation
type OperationType int

const (
	UnknownOperation OperationType = iota
	ListOperation
	DescribeOperation
	GetOperation
	CreateOperation
	UpdateOperation
	DeleteOperation
	PutOperation
	TagOperation
	UntagOperation
)

// String returns the string representation of an operation type
func (op OperationType) String() string {
	switch op {
	case ListOperation:
		return "List"
	case DescribeOperation:
		return "Describe"
	case GetOperation:
		return "Get"
	case CreateOperation:
		return "Create"
	case UpdateOperation:
		return "Update"
	case DeleteOperation:
		return "Delete"
	case PutOperation:
		return "Put"
	case TagOperation:
		return "Tag"
	case UntagOperation:
		return "Untag"
	default:
		return "Unknown"
	}
}

// ClientFactoryInterface defines the interface for creating AWS service clients
type ClientFactoryInterface interface {
	GetClient(serviceName string) interface{}
	GetAvailableServices() []string
}

// AnalysisGeneratorInterface defines the interface for generating analysis files
type AnalysisGeneratorInterface interface {
	GenerateForDiscoveredServices(serviceNames []string) error
	GenerateForFilteredServices(discoveredServices []*pb.ServiceInfo, requestedServices []string) error
}

// ServiceMetadata holds cached metadata about a discovered AWS service
type ServiceMetadata struct {
	Name          string                    `json:"name"`
	DisplayName   string                    `json:"display_name"`
	PackageName   string                    `json:"package_name"`
	ClientType    string                    `json:"client_type"`
	Operations    map[string]OperationType  `json:"operations"`
	ResourceTypes []string                  `json:"resource_types"`
	Paginated     map[string]bool           `json:"paginated"`
	ReflectionData *ReflectionMetadata      `json:"reflection_data"`
	DiscoveredAt  time.Time                 `json:"discovered_at"`
}

// ReflectionMetadata holds reflection-specific data about a service client
type ReflectionMetadata struct {
	ClientTypeName   string            `json:"client_type_name"`
	MethodCount      int               `json:"method_count"`
	MethodSignatures map[string]string `json:"method_signatures"`
	PackagePath      string            `json:"package_path"`
}

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

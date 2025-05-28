package provider

import (
	"context"
	"time"
)

// CloudProvider defines the interface for all cloud provider plugins
type CloudProvider interface {
	// GetProviderName returns the name of the provider (e.g., "aws", "azure", "gcp")
	GetProviderName() string

	// GetSupportedServices returns list of all services supported by this provider
	GetSupportedServices(ctx context.Context) ([]ServiceInfo, error)

	// DiscoverServices discovers available services from the provider's API/SDK
	DiscoverServices(ctx context.Context, options *DiscoveryOptions) (*DiscoveryResult, error)

	// ScanResources scans resources for the specified services
	ScanResources(ctx context.Context, request *ScanRequest) (*ScanResult, error)

	// Validate validates the provider configuration and credentials
	Validate(ctx context.Context) error

	// Close cleans up provider resources
	Close() error
}

// ServiceInfo represents information about a cloud service
type ServiceInfo struct {
	Name           string            `json:"name"`
	DisplayName    string            `json:"display_name"`
	Description    string            `json:"description"`
	ResourceTypes  []string          `json:"resource_types"`
	Capabilities   []string          `json:"capabilities"`
	Version        string            `json:"version"`
	Documentation  string            `json:"documentation"`
	PackagePath    string            `json:"package_path"`
	Dependencies   []string          `json:"dependencies"`
	Metadata       map[string]string `json:"metadata"`
}

// DiscoveryOptions contains options for service discovery
type DiscoveryOptions struct {
	// Services to discover (empty means all)
	Services []string `json:"services"`
	
	// IncludeAll whether to include all available services
	IncludeAll bool `json:"include_all"`
	
	// UseGitHubAPI whether to use GitHub API for discovery
	UseGitHubAPI bool `json:"use_github_api"`
	
	// GitHubRepo repository to discover from (e.g., "aws/aws-sdk-go-v2")
	GitHubRepo string `json:"github_repo"`
	
	// SDKVersion specific SDK version to use
	SDKVersion string `json:"sdk_version"`
	
	// Filters for service discovery
	Filters map[string]string `json:"filters"`
}

// DiscoveryResult contains the result of service discovery
type DiscoveryResult struct {
	Provider     string                 `json:"provider"`
	Services     []ServiceInfo          `json:"services"`
	Timestamp    time.Time              `json:"timestamp"`
	Duration     time.Duration          `json:"duration"`
	Errors       []string               `json:"errors"`
	Metadata     map[string]interface{} `json:"metadata"`
	TotalFound   int                    `json:"total_found"`
	FilteredOut  int                    `json:"filtered_out"`
}

// ScanRequest contains the request for scanning resources
type ScanRequest struct {
	// Services to scan
	Services []string `json:"services"`
	
	// Region to scan (cloud-specific)
	Region string `json:"region"`
	
	// Phases to execute
	Phases []ScanPhase `json:"phases"`
	
	// Hierarchical scanning options
	Hierarchical bool `json:"hierarchical"`
	
	// Maximum depth for hierarchical scanning
	MaxDepth int `json:"max_depth"`
	
	// Streaming options
	Streaming bool `json:"streaming"`
	BatchSize int  `json:"batch_size"`
	
	// Output options
	OutputFormat   string            `json:"output_format"`
	IncludeRawData bool              `json:"include_raw_data"`
	IncludeTags    bool              `json:"include_tags"`
	
	// Filters
	Filters map[string]interface{} `json:"filters"`
	
	// Provider-specific options
	ProviderOptions map[string]interface{} `json:"provider_options"`
}

// ScanPhase represents a phase in the scanning process
type ScanPhase string

const (
	PhaseDiscover ScanPhase = "discover"
	PhaseList     ScanPhase = "list"
	PhaseDescribe ScanPhase = "describe"
)

// ScanResult contains the result of resource scanning
type ScanResult struct {
	Provider    string                    `json:"provider"`
	Region      string                    `json:"region"`
	Services    []string                  `json:"services"`
	Resources   map[string][]ResourceInfo `json:"resources"`
	Relationships []RelationshipInfo      `json:"relationships"`
	Timestamp   time.Time                 `json:"timestamp"`
	Duration    time.Duration             `json:"duration"`
	Errors      []string                  `json:"errors"`
	Metadata    map[string]interface{}    `json:"metadata"`
	TotalResources int                    `json:"total_resources"`
	PhasesCompleted []ScanPhase            `json:"phases_completed"`
}

// ResourceInfo represents information about a cloud resource
type ResourceInfo struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	ARN         string                 `json:"arn,omitempty"`
	Region      string                 `json:"region"`
	AccountID   string                 `json:"account_id,omitempty"`
	ParentID    string                 `json:"parent_id,omitempty"`
	Service     string                 `json:"service"`
	RawData     map[string]interface{} `json:"raw_data,omitempty"`
	Attributes  map[string]interface{} `json:"attributes"`
	Tags        map[string]string      `json:"tags,omitempty"`
	CreatedAt   *time.Time             `json:"created_at,omitempty"`
	ModifiedAt  *time.Time             `json:"modified_at,omitempty"`
	ScannedAt   time.Time              `json:"scanned_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RelationshipInfo represents a relationship between cloud resources
type RelationshipInfo struct {
	FromID           string                 `json:"from_id"`
	ToID             string                 `json:"to_id"`
	RelationshipType string                 `json:"relationship_type"`
	Properties       map[string]interface{} `json:"properties,omitempty"`
	Bidirectional    bool                   `json:"bidirectional"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// PluginConfig represents plugin configuration
type PluginConfig struct {
	Provider string            `json:"provider"`
	Version  string            `json:"version"`
	Config   map[string]string `json:"config"`
	Enabled  bool              `json:"enabled"`
}

// ProviderRegistry manages cloud provider plugins
type ProviderRegistry interface {
	// RegisterProvider registers a new cloud provider
	RegisterProvider(name string, provider CloudProvider) error
	
	// GetProvider returns a provider by name
	GetProvider(name string) (CloudProvider, error)
	
	// ListProviders returns all registered providers
	ListProviders() []string
	
	// UnregisterProvider removes a provider
	UnregisterProvider(name string) error
}
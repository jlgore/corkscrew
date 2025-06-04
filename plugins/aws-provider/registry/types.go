package registry

import (
	"reflect"
	"time"

	"golang.org/x/time/rate"
)

// ServiceDefinition represents a complete AWS service definition with all metadata
type ServiceDefinition struct {
	// Basic service information
	Name        string `json:"name"`        // e.g., "s3", "ec2"
	DisplayName string `json:"displayName"` // e.g., "Amazon S3", "Amazon EC2"
	Description string `json:"description"` // Service description

	// SDK integration details
	PackagePath string       `json:"packagePath"` // e.g., "github.com/aws/aws-sdk-go-v2/service/s3"
	ClientType  string       `json:"clientType"`  // e.g., "*s3.Client"
	ClientRef   reflect.Type `json:"-"`           // Runtime type reference (not serialized)

	// Resource and operation definitions
	ResourceTypes []ResourceTypeDefinition `json:"resourceTypes"`
	Operations    []OperationDefinition    `json:"operations"`

	// Rate limiting configuration
	RateLimit  rate.Limit `json:"rateLimit"`  // Requests per second
	BurstLimit int        `json:"burstLimit"` // Maximum burst size

	// Security and permissions
	Permissions []string `json:"permissions"` // Required IAM permissions

	// Service metadata
	SupportsPagination        bool                   `json:"supportsPagination"`
	SupportsResourceExplorer  bool                   `json:"supportsResourceExplorer"`
	SupportsParallelScan      bool                   `json:"supportsParallelScan"`
	GlobalService             bool                   `json:"globalService"`        // e.g., IAM, S3
	RequiresRegion            bool                   `json:"requiresRegion"`       // Most services require region
	CustomHandlerRequired     bool                   `json:"customHandlerRequired"` // Needs special handling
	AdditionalMetadata        map[string]interface{} `json:"additionalMetadata"`   // Extensible metadata

	// Discovery metadata
	DiscoveredAt      time.Time `json:"discoveredAt"`
	DiscoverySource   string    `json:"discoverySource"`   // e.g., "reflection", "manual", "resource-explorer"
	DiscoveryVersion  string    `json:"discoveryVersion"`  // SDK version when discovered
	LastValidated     time.Time `json:"lastValidated"`     // Last time definition was validated
	ValidationStatus  string    `json:"validationStatus"`  // e.g., "valid", "deprecated", "unknown"
}

// ResourceTypeDefinition defines a specific resource type within a service
type ResourceTypeDefinition struct {
	// Basic resource information
	Name         string `json:"name"`         // e.g., "Bucket", "Instance"
	DisplayName  string `json:"displayName"`  // e.g., "S3 Bucket", "EC2 Instance"
	Description  string `json:"description"`  // Resource description
	ResourceType string `json:"resourceType"` // Full resource type e.g., "AWS::S3::Bucket"

	// Operations associated with this resource
	ListOperation     string   `json:"listOperation"`     // e.g., "ListBuckets"
	DescribeOperation string   `json:"describeOperation"` // e.g., "DescribeBuckets"
	GetOperation      string   `json:"getOperation"`      // e.g., "GetBucketInfo"
	SupportedOperations []string `json:"supportedOperations"` // All operations for this resource

	// Resource identification
	IdentifierFields []string               `json:"identifierFields"` // Fields that identify the resource
	OutputPath       string                 `json:"outputPath"`       // Path to resources in API response
	IDField          string                 `json:"idField"`          // Primary identifier field
	ARNPattern       string                 `json:"arnPattern"`       // ARN pattern for this resource
	TagsPath         string                 `json:"tagsPath"`         // Path to tags in resource
	FieldMappings    map[string]string      `json:"fieldMappings"`    // Custom field mappings

	// Resource characteristics
	RequiresParentResource bool     `json:"requiresParentResource"` // e.g., S3 objects need bucket
	ParentResourceType     string   `json:"parentResourceType"`     // Parent resource type name
	IsGlobalResource       bool     `json:"isGlobalResource"`       // Resource spans regions
	SupportsTags           bool     `json:"supportsTags"`           // Resource supports tagging
	SupportsVersioning     bool     `json:"supportsVersioning"`     // Resource has versions
	Paginated              bool     `json:"paginated"`              // List operation is paginated
	RequiredPermissions    []string `json:"requiredPermissions"`    // IAM permissions needed

	// Pagination configuration
	PaginationConfig *PaginationConfig `json:"paginationConfig,omitempty"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// OperationDefinition defines a specific API operation
type OperationDefinition struct {
	// Basic operation information
	Name            string `json:"name"`            // e.g., "ListBuckets"
	DisplayName     string `json:"displayName"`     // Human-readable name
	Description     string `json:"description"`     // Operation description
	OperationType   string `json:"operationType"`   // e.g., "List", "Describe", "Get", "Create", "Update", "Delete"
	HTTPMethod      string `json:"httpMethod"`      // e.g., "GET", "POST"
	APIVersion      string `json:"apiVersion"`      // API version this operation uses

	// Input/output configuration
	InputType       string                 `json:"inputType"`       // Go type name for input
	OutputType      string                 `json:"outputType"`      // Go type name for output
	RequiredParams  []string               `json:"requiredParams"`  // Required input parameters
	OptionalParams  []string               `json:"optionalParams"`  // Optional input parameters
	OutputFields    []string               `json:"outputFields"`    // Key output fields
	ResourcePath    string                 `json:"resourcePath"`    // Path to resources in output
	ParameterSchema map[string]interface{} `json:"parameterSchema"` // JSON schema for parameters

	// Operation characteristics
	Paginated           bool     `json:"paginated"`           // Supports pagination
	RequiresResourceID  bool     `json:"requiresResourceId"`  // Needs specific resource ID
	IsGlobalOperation   bool     `json:"isGlobalOperation"`   // Operates on global resources
	IsMutating          bool     `json:"isMutating"`          // Modifies resources
	IsIdempotent        bool     `json:"isIdempotent"`        // Safe to retry
	SupportsFiltering   bool     `json:"supportsFiltering"`   // Supports filter parameters
	SupportsProjection  bool     `json:"supportsProjection"`  // Can select specific fields
	RequiredPermissions []string `json:"requiredPermissions"` // IAM permissions

	// Rate limiting specific to this operation
	CustomRateLimit  *rate.Limit `json:"customRateLimit,omitempty"`  // Override service rate limit
	CustomBurstLimit *int        `json:"customBurstLimit,omitempty"` // Override service burst limit

	// Error handling
	CommonErrors      []string               `json:"commonErrors"`      // Common error codes
	RetryableErrors   []string               `json:"retryableErrors"`   // Errors safe to retry
	ThrottlingError   string                 `json:"throttlingError"`   // Specific throttling error code
	ErrorFieldMapping map[string]string      `json:"errorFieldMapping"` // Map error fields

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PaginationConfig defines how pagination works for a resource or operation
type PaginationConfig struct {
	// Token-based pagination
	TokenParam     string `json:"tokenParam"`     // Input parameter name for token
	NextTokenField string `json:"nextTokenField"` // Output field containing next token
	
	// Page-based pagination
	PageParam     string `json:"pageParam"`     // Input parameter for page number
	PageSizeParam string `json:"pageSizeParam"` // Input parameter for page size
	MaxPageSize   int    `json:"maxPageSize"`   // Maximum allowed page size
	DefaultPageSize int  `json:"defaultPageSize"` // Default page size if not specified

	// Marker-based pagination
	MarkerParam     string `json:"markerParam"`     // Input parameter for marker
	NextMarkerField string `json:"nextMarkerField"` // Output field containing next marker

	// Result handling
	ResultsField    string `json:"resultsField"`    // Field containing results array
	CountField      string `json:"countField"`      // Field containing result count
	HasMoreField    string `json:"hasMoreField"`    // Field indicating more results
	TotalCountField string `json:"totalCountField"` // Field with total count (if available)

	// Pagination strategy
	Strategy string `json:"strategy"` // "token", "page", "marker", "offset"
}

// ServiceFilter defines criteria for filtering services
type ServiceFilter struct {
	// Filter by service properties
	Names              []string `json:"names,omitempty"`              // Specific service names
	ResourceTypes      []string `json:"resourceTypes,omitempty"`      // Services with these resources
	RequiredOperations []string `json:"requiredOperations,omitempty"` // Services with these operations
	
	// Filter by capabilities
	RequiresPagination       *bool `json:"requiresPagination,omitempty"`
	RequiresResourceExplorer *bool `json:"requiresResourceExplorer,omitempty"`
	RequiresGlobalService    *bool `json:"requiresGlobalService,omitempty"`
	
	// Filter by discovery
	DiscoverySource  string    `json:"discoverySource,omitempty"`
	DiscoveredAfter  time.Time `json:"discoveredAfter,omitempty"`
	DiscoveredBefore time.Time `json:"discoveredBefore,omitempty"`
	
	// Filter by validation
	ValidationStatus []string `json:"validationStatus,omitempty"`
	ValidatedAfter   time.Time `json:"validatedAfter,omitempty"`
}

// RegistryStats provides statistics about the registry
type RegistryStats struct {
	TotalServices        int                    `json:"totalServices"`
	TotalResourceTypes   int                    `json:"totalResourceTypes"`
	TotalOperations      int                    `json:"totalOperations"`
	ServicesBySource     map[string]int         `json:"servicesBySource"`
	ServicesByStatus     map[string]int         `json:"servicesByStatus"`
	LastUpdated          time.Time              `json:"lastUpdated"`
	CacheHitRate         float64                `json:"cacheHitRate"`
	DiscoverySuccess     int                    `json:"discoverySuccess"`
	DiscoveryFailures    int                    `json:"discoveryFailures"`
	CustomMetrics        map[string]interface{} `json:"customMetrics,omitempty"`
}

// RegistryConfig defines configuration for the service registry
type RegistryConfig struct {
	// Persistence configuration
	PersistencePath     string        `json:"persistencePath"`     // File path for persistence
	AutoPersist         bool          `json:"autoPersist"`         // Auto-save on changes
	PersistenceInterval time.Duration `json:"persistenceInterval"` // How often to auto-save

	// Cache configuration
	EnableCache      bool          `json:"enableCache"`      // Enable in-memory caching
	CacheTTL         time.Duration `json:"cacheTtl"`         // Cache entry TTL
	MaxCacheSize     int           `json:"maxCacheSize"`     // Maximum cache entries
	PreloadCache     bool          `json:"preloadCache"`     // Load cache on startup

	// Discovery configuration
	EnableDiscovery      bool          `json:"enableDiscovery"`      // Enable auto-discovery
	DiscoveryInterval    time.Duration `json:"discoveryInterval"`    // How often to run discovery
	DiscoveryTimeout     time.Duration `json:"discoveryTimeout"`     // Discovery operation timeout
	MaxDiscoveryRetries  int           `json:"maxDiscoveryRetries"`  // Max retries for discovery
	DiscoveryParallelism int           `json:"discoveryParallelism"` // Concurrent discovery operations

	// Validation configuration
	EnableValidation    bool          `json:"enableValidation"`    // Validate definitions
	ValidationInterval  time.Duration `json:"validationInterval"`  // How often to validate
	StrictValidation    bool          `json:"strictValidation"`    // Fail on validation errors

	// Fallback configuration
	UseFallbackServices bool     `json:"useFallbackServices"` // Use built-in definitions
	FallbackServiceList []string `json:"fallbackServiceList"` // Which services to fallback

	// Performance configuration
	MaxConcurrentReads  int `json:"maxConcurrentReads"`  // Max concurrent read operations
	MaxConcurrentWrites int `json:"maxConcurrentWrites"` // Max concurrent write operations

	// Feature flags
	EnableDynamicRateLimiting bool `json:"enableDynamicRateLimiting"` // Adjust rate limits dynamically
	EnableMetrics             bool `json:"enableMetrics"`             // Collect registry metrics
	EnableAuditLog            bool `json:"enableAuditLog"`            // Log all registry changes
	
	// Custom configuration
	CustomConfig map[string]interface{} `json:"customConfig,omitempty"`
}
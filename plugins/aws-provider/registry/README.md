# AWS Dynamic Service Registry

## Overview

The Dynamic Service Registry is a core component of the corkscrew AWS provider that eliminates hardcoded service definitions. It provides a flexible, extensible system for managing AWS service metadata, operations, and configurations dynamically at runtime.

## Key Features

### 1. Dynamic Service Management
- Runtime registration and discovery of AWS services
- No hardcoded service lists or switch statements
- Automatic support for new AWS services without code changes

### 2. Comprehensive Service Metadata
- Service definitions include all necessary information:
  - Package paths and client types
  - Resource types and operations
  - Rate limiting and burst configurations
  - IAM permissions requirements
  - Pagination and filtering support

### 3. Persistence and Caching
- JSON-based persistence for offline usage
- In-memory caching with configurable TTL
- Atomic file operations for data integrity
- Backup and restore capabilities

### 4. Discovery Integration
- Populate registry from AWS Resource Explorer
- Reflection-based service discovery
- Merge capabilities for combining sources

### 5. Performance Optimizations
- Configurable rate limiting per service
- Concurrent read/write support
- Cache hit rate monitoring
- Streaming JSON support for large registries

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    DynamicServiceRegistry                    │
├─────────────────────────────────────────────────────────────┤
│  Core Operations:                                           │
│  - RegisterService()      - GetService()                    │
│  - ListServices()         - FilterServices()               │
│  - UpdateService()        - RemoveService()                │
├─────────────────────────────────────────────────────────────┤
│  Discovery Integration:                                     │
│  - PopulateFromDiscovery()                                 │
│  - PopulateFromReflection()                                │
│  - MergeWithExisting()                                     │
├─────────────────────────────────────────────────────────────┤
│  Persistence Layer:                                        │
│  - PersistToFile()        - LoadFromFile()                │
│  - LoadFromDirectory()    - ExportServices()              │
├─────────────────────────────────────────────────────────────┤
│  Monitoring & Config:                                      │
│  - GetStats()             - ValidateRegistry()            │
│  - GetConfig()            - UpdateConfig()                │
└─────────────────────────────────────────────────────────────┘
```

## Usage Examples

### Basic Usage

```go
// Create a new registry
config := RegistryConfig{
    PersistencePath:     "/etc/corkscrew/aws-services.json",
    AutoPersist:         true,
    EnableCache:         true,
    UseFallbackServices: true,
}
registry := NewServiceRegistry(config)

// Register a service
s3Service := ServiceDefinition{
    Name:        "s3",
    DisplayName: "Amazon S3",
    PackagePath: "github.com/aws/aws-sdk-go-v2/service/s3",
    RateLimit:   rate.Limit(100),
    BurstLimit:  200,
    // ... other fields
}
registry.RegisterService(s3Service)

// Retrieve a service
if service, exists := registry.GetService("s3"); exists {
    fmt.Printf("Rate limit: %v/sec\n", service.RateLimit)
}
```

### Discovery Integration

```go
// Populate from AWS Resource Explorer
discoveredServices := // ... from Resource Explorer API
registry.PopulateFromDiscovery(discoveredServices)

// Populate from reflection
serviceClients := map[string]interface{}{
    "s3": s3Client,
    "ec2": ec2Client,
}
registry.PopulateFromReflection(serviceClients)
```

### Filtering Services

```go
// Find all global services
filter := ServiceFilter{
    RequiresGlobalService: &true,
}
globalServices := registry.FilterServices(filter)

// Find services with specific operations
filter = ServiceFilter{
    RequiredOperations: []string{"ListBuckets", "CreateBucket"},
}
s3LikeServices := registry.FilterServices(filter)
```

## Data Model

### ServiceDefinition
The core data structure containing:
- Basic metadata (name, display name, description)
- SDK integration details (package path, client type)
- Resource types and their operations
- Rate limiting configuration
- Security permissions
- Discovery metadata

### ResourceTypeDefinition
Defines a resource type within a service:
- Resource identification and ARN patterns
- List/Describe/Get operations
- Field mappings and output paths
- Pagination configuration
- Tag support

### OperationDefinition
Describes an API operation:
- Operation type and HTTP method
- Input/output types
- Required and optional parameters
- Pagination support
- Error handling configuration

## Migration Strategy

### Phase 1: Create Registry Infrastructure ✅
- Implement core registry types and interfaces
- Add persistence and caching layers
- Create discovery integration

### Phase 2: Migrate Existing Services
1. Extract service definitions from hardcoded locations
2. Convert to ServiceDefinition format
3. Register in dynamic registry
4. Update ClientFactory to use registry

### Phase 3: Remove Hardcoded References
1. Replace switch statements with registry lookups
2. Remove static imports
3. Update scanner implementations

### Phase 4: Enable Full Discovery
1. Integrate with Resource Explorer
2. Add reflection-based discovery
3. Implement automatic updates

## Configuration

### Registry Configuration Options

```go
type RegistryConfig struct {
    // Persistence
    PersistencePath     string        // JSON file path
    AutoPersist         bool          // Auto-save changes
    PersistenceInterval time.Duration // Save frequency

    // Caching
    EnableCache  bool          // Enable in-memory cache
    CacheTTL     time.Duration // Cache entry lifetime
    MaxCacheSize int           // Maximum cache entries

    // Discovery
    EnableDiscovery      bool          // Enable auto-discovery
    DiscoveryInterval    time.Duration // Discovery frequency
    DiscoveryTimeout     time.Duration // Discovery timeout

    // Validation
    EnableValidation   bool // Validate definitions
    StrictValidation   bool // Fail on validation errors

    // Features
    EnableMetrics bool // Collect usage metrics
    EnableAuditLog bool // Log all changes
}
```

## Performance Considerations

1. **Caching**: Enable caching for frequently accessed services
2. **Persistence**: Use auto-persist with appropriate intervals
3. **Concurrent Access**: Registry supports concurrent reads
4. **Rate Limiting**: Configure per-service rate limits appropriately

## Future Enhancements

1. **Plugin System**: Allow external service definition providers
2. **Version Management**: Track SDK version compatibility
3. **A/B Testing**: Support multiple service configurations
4. **Metrics Export**: Prometheus/CloudWatch integration
5. **GraphQL Support**: Query language for service discovery

## Contributing

When adding new features to the registry:
1. Update the ServiceDefinition struct if needed
2. Add tests for new functionality
3. Update persistence format version if breaking changes
4. Document new configuration options

## Testing

Run the test suite:
```bash
go test ./plugins/aws-provider/registry/...
```

## License

Part of the corkscrew project.
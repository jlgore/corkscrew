# AWS Provider Architecture

## Overview

The AWS Provider implements a streamlined resource discovery and scanning architecture that eliminates circular dependencies and provides efficient resource enumeration with rich configuration collection.

## Architecture Flow

```
CloudProvider Interface
        ↓
AWSProvider (aws_provider.go)
        ↓
RuntimePipeline (runtime/pipeline.go) ← Optional for advanced workflows
        ↓
ScannerRegistry (runtime/scanner_registry.go)
        ↓
UnifiedScanner (scanner.go) ← Core resource discovery
        ↓
AWS SDK Services + Resource Explorer
```

## Core Components

### 1. AWSProvider (`aws_provider.go`)

The main entry point implementing the CloudProvider interface. Key responsibilities:

- **Initialization**: AWS configuration, credentials, and component setup
- **Service Discovery**: Dynamic or fallback service enumeration  
- **Resource Scanning**: Orchestrates resource discovery via UnifiedScanner
- **Feature Flags**: Migration control and fallback behavior
- **Integration**: Connects UnifiedScanner to RuntimePipeline when available

### 2. UnifiedScanner (`scanner.go`)

The core resource discovery engine. Provides:

- **Service Scanning**: `ScanService(serviceName)` → `[]*pb.ResourceRef`
- **Resource Enrichment**: `DescribeResource(ref)` → `*pb.Resource`
- **Multi-source Discovery**: AWS SDK + Resource Explorer fallback
- **Client Factory Integration**: Dynamic AWS service client creation
- **Rate Limiting**: Per-service request throttling

### 3. ScannerRegistry (`runtime/scanner_registry.go`)

Manages scanner orchestration and provides:

- **Unified Interface**: Bridges between registry and UnifiedScanner
- **Rate Limiting**: Service-specific request throttling
- **Metadata Management**: Service capabilities and permissions
- **Batch Operations**: Concurrent multi-service scanning
- **No Generated Scanners**: Uses UnifiedScanner for all services

### 4. RuntimePipeline (`runtime/pipeline.go`)

Optional advanced workflow orchestration:

- **Resource Streaming**: Real-time resource processing
- **Database Integration**: Handled by main CLI, not plugin
- **Relationship Extraction**: Resource dependency mapping
- **Performance Monitoring**: Scan statistics and metrics

## Key Design Principles

### 1. No Circular Dependencies

**Previous Issue**: RuntimePipeline called back to AWSProvider for enrichment, creating cycles.

**Solution**: Direct UnifiedScanner integration in registry:
```go
// Registry uses UnifiedScanner directly
registry.SetUnifiedScanner(scannerProvider)

// UnifiedScanner provides both scanning and enrichment
type UnifiedScannerProvider interface {
    ScanService(ctx, serviceName) ([]*pb.ResourceRef, error)
    DescribeResource(ctx, ref) (*pb.Resource, error)
}
```

### 2. Single Source of Truth

**UnifiedScanner** is the authoritative resource discovery mechanism:
- Replaces all generated service-specific scanners
- Uses AWS SDK reflection for dynamic service support
- Falls back to Resource Explorer when available
- Provides consistent enrichment across all services

### 3. Database Separation

**Plugin Responsibility**: Resource discovery and enrichment
**Main CLI Responsibility**: Database operations and persistence

This prevents database locking conflicts and simplifies plugin architecture.

### 4. Feature Flag Migration

Environment variables control behavior:

- `AWS_PROVIDER_MIGRATION_ENABLED`: Use dynamic discovery (default: true)
- `AWS_PROVIDER_FALLBACK_MODE`: Enable fallback behavior (default: false)  
- `AWS_PROVIDER_MONITORING_ENABLED`: Enhanced logging (default: false)

## Resource Discovery Flow

### Standard Scanning

1. **Request**: `corkscrew scan --provider aws --services s3,ec2`
2. **AWSProvider**: Receives scan request
3. **UnifiedScanner**: Scans each service via AWS SDK
4. **ResourceRefs**: Returns lightweight resource references
5. **Enrichment**: Calls `DescribeResource` for full details
6. **Pipeline**: Optional streaming and relationship extraction
7. **CLI**: Receives enriched resources for database storage

### Resource Explorer Integration

When available, Resource Explorer provides enhanced discovery:

1. **Query**: `service:s3 AND region:us-east-1`
2. **Explorer**: Returns comprehensive resource list
3. **Enrichment**: UnifiedScanner enriches each resource
4. **Fallback**: SDK scanning if Explorer unavailable

## Configuration Collection

The UnifiedScanner implements comprehensive configuration collection:

### Service Clients
- **Dynamic Creation**: Clients created on-demand via reflection
- **Caching**: Reuse clients across multiple operations
- **Region Support**: Multi-region scanning capabilities

### Resource Enrichment
- **Describe Operations**: Full resource configuration via AWS APIs
- **Tag Collection**: Resource tags and metadata
- **Relationship Mapping**: Dependencies and associations
- **Error Handling**: Graceful degradation on API failures

## Performance Characteristics

### Rate Limiting
- **Service-Specific**: Customized limits per AWS service
- **Burst Handling**: Short burst allowances for initial operations
- **Context Cancellation**: Timeout and cancellation support

### Concurrency
- **Configurable**: Default 10 concurrent operations
- **Service Isolation**: Independent rate limiting per service
- **Resource Streaming**: Non-blocking resource processing

### Caching
- **Service Discovery**: 24-hour cache for discovered services
- **Resource Discovery**: 15-minute cache for resource lists
- **Client Reuse**: Connection pooling and client caching

## Testing Strategy

### Unit Tests
- **Scanner Registry**: Core functionality and rate limiting
- **UnifiedScanner**: Service scanning and enrichment
- **Pipeline Components**: Streaming and batch operations

### Integration Tests
- **End-to-End**: Full scan workflows with real AWS APIs
- **Performance**: Benchmark tests for large resource sets
- **Fallback**: Resource Explorer fallback scenarios

### Mock Components
- **MockUnifiedScanner**: Testing without AWS dependencies
- **Rate Limiting**: Verify throttling behavior
- **Error Scenarios**: API failure handling

## Migration Benefits

### Eliminated Components
- ✅ **ScannerEnricher**: Redundant enrichment layer
- ✅ **ConfigurationEnricher**: Circular dependency source
- ✅ **Generated Scanners**: Replaced by UnifiedScanner
- ✅ **Bridge Patterns**: Direct integration instead

### Simplified Architecture
- **Single Scanner**: UnifiedScanner handles all services
- **Direct Integration**: Registry → UnifiedScanner (no callbacks)
- **Clear Responsibilities**: Plugin discovers, CLI persists
- **Maintainable Code**: Fewer abstractions and indirections

## Future Enhancements

### Additional Sources
- **CloudFormation**: Template-based resource discovery
- **AWS Config**: Compliance and configuration tracking
- **Cost Explorer**: Resource cost attribution

### Performance Optimizations
- **Parallel Enrichment**: Concurrent resource detail collection
- **Intelligent Caching**: Smart cache invalidation strategies
- **Incremental Updates**: Delta-based resource synchronization

## Development Guidelines

### Adding New Services
1. No code changes required - UnifiedScanner handles dynamically
2. Add service to rate limiting configuration if needed
3. Update integration tests for new service validation

### Modifying Resource Collection
1. Enhance UnifiedScanner discovery methods
2. Update enrichment logic in `DescribeResource`
3. Add relationship extraction in pipeline if needed

### Performance Tuning
1. Adjust rate limits in ScannerRegistry
2. Modify concurrency settings in pipeline
3. Update cache TTL values based on usage patterns
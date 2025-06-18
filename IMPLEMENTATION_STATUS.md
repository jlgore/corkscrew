# Corkscrew Implementation Status

This document tracks the current implementation status of various provider methods, identifying placeholder implementations that need to be completed for production readiness.

## Overview

The recent proto updates added new service discovery and streaming methods to all providers. While these methods have been implemented to satisfy the interface requirements, many contain placeholder logic that needs to be replaced with actual functionality.

## Kubernetes Provider

### 1. Dynamic Resource Discovery

**Status**: ✅ COMPLETED
**Location**: `plugins/kubernetes-provider/kubernetes_provider.go:ScanService`

**Implementation**:
- Added `DiscoverResourcesForAPIGroup` method in `discovery.go`
- ScanService now uses dynamic discovery when no hardcoded resources are found
- Discovers resources from API server including CRDs
- Caches discovered resources for performance

### 2. Relationship Extraction

**Status**: ✅ COMPLETED
**Location**: `plugins/kubernetes-provider/kubernetes_provider.go:extractBasicRelationships`

**Implementation**:
- Parses resource specs to extract relationships
- Implemented owner references parsing
- Added label selector matching for Service->Pod relationships
- Implemented relationship types:
  - Owner references (OWNED_BY)
  - Service selectors (SELECTS/SELECTED_BY)
  - ConfigMap/Secret volume mounts (MOUNTS)
  - PVC bindings (MOUNTS)
  - Ingress->Service relationships (ROUTES_TO)
  - NetworkPolicy targets (APPLIES_TO)

### 3. Scanner Implementation

**Status**: ✅ COMPLETED
**Location**: `plugins/kubernetes-provider/scanner.go` and `informer_cache.go`

**Implementation**:
- Created scanner.go with full resource scanning functionality
- Created informer_cache.go for real-time caching
- Implemented methods:
  - `NewResourceScannerForCluster(cluster)`
  - `ScanResources(ctx, resourceTypes, namespaces)`
  - `StreamScanResources(ctx, resourceTypes, resourceChan)`
  - `SetupInformers(ctx, resourceTypes)`
  - `StartInformers(ctx, informers, eventChan)`
- Added pagination support
- Added label and field selector support
- Created comprehensive tests

### 4. Permissions Discovery

**Status**: Hardcoded
**Location**: `plugins/kubernetes-provider/kubernetes_provider.go:DiscoverServices`

**Current Implementation**:
```go
RequiredPermissions: []string{
    "get", "list", "watch", // Basic permissions
}
```

**What's Needed**:
- Query RBAC rules for actual required permissions
- Map resource types to specific verbs
- Consider namespace-scoped vs cluster-scoped resources

**Priority**: MEDIUM

## GCP Provider

### 1. Scanner Streaming

**Status**: Fallback Implementation
**Location**: `plugins/gcp-provider/gcp_provider.go:StreamScanService`

**Current Implementation**:
```go
// Use scanner for streaming
resourceRefs, err := p.scanner.ScanService(ctx, req.Service)
// Then manually streams each resource
```

**What's Needed**:
- Add `StreamScanService` method to GCP scanner
- Implement true streaming using GCP client library pagination
- Add progress reporting during streaming

**Priority**: MEDIUM

### 2. Service Discovery

**Status**: Functional but Limited
**Location**: `plugins/gcp-provider/gcp_provider.go:DiscoverServices`

**Current Implementation**:
Uses predefined service list from `gcp_service_definitions.go`

**What's Needed**:
- Dynamic discovery of available GCP services
- Query GCP Service Usage API
- Cache discovered services

**Priority**: LOW

## Azure Provider

### 1. Discovery Configuration

**Status**: Placeholder
**Location**: `plugins/azure-provider/azure_provider.go:ConfigureDiscovery`

**Current Implementation**:
```go
func (p *AzureProvider) ConfigureDiscovery(ctx context.Context, req *pb.ConfigureDiscoveryRequest) (*pb.ConfigureDiscoveryResponse, error) {
    // Simple implementation for now
    return &pb.ConfigureDiscoveryResponse{
        Success: true,
    }, nil
}
```

**What's Needed**:
- Parse and validate discovery configuration
- Update discovery settings in provider
- Implement filtering based on configuration

**Priority**: MEDIUM

### 2. Permissions Discovery

**Status**: Generic Implementation
**Location**: `plugins/azure-provider/azure_provider.go:DiscoverServices`

**Current Implementation**:
```go
RequiredPermissions: []string{
    "Microsoft.Resources/subscriptions/resourceGroups/read",
    fmt.Sprintf("%s/*/read", providerNamespace),
    fmt.Sprintf("%s/*/list", providerNamespace),
}
```

**What's Needed**:
- Query Azure RBAC for specific permissions
- Map resource types to required actions
- Consider management group and subscription scopes

**Priority**: LOW

## AWS Provider

### 1. Service Discovery

**Status**: Mostly Complete
**Location**: `plugins/aws-provider/discovery/`

**Notes**:
- AWS provider has the most complete implementation
- Uses reflection-based discovery
- Could benefit from caching improvements

**Priority**: LOW

## Implementation Priorities

### Critical (Do First)
1. **Kubernetes Scanner Implementation**
   - Create `scanner.go` and `informer_cache.go`
   - Without these, Kubernetes provider cannot scan resources

### High Priority
2. **Kubernetes Dynamic Resource Discovery**
   - Implement API group resource discovery
   - Required for scanning non-hardcoded resources

3. **Kubernetes Relationship Extraction**
   - Parse resource specs for relationships
   - Essential for understanding cluster topology

### Medium Priority
4. **GCP Scanner Streaming**
   - Add true streaming support
   - Improves performance for large environments

5. **Azure Discovery Configuration**
   - Implement configuration parsing
   - Enable filtered discovery

### Low Priority
6. **Permissions Discovery** (All providers)
   - Query actual required permissions
   - Nice to have for security auditing

7. **GCP/Azure Dynamic Service Discovery**
   - Replace hardcoded service lists
   - Improves maintainability

## Recommended Next Steps

1. **Create Missing Files**:
   ```bash
   touch plugins/kubernetes-provider/scanner.go
   touch plugins/kubernetes-provider/informer_cache.go
   ```

2. **Start with Kubernetes Scanner**:
   - Implement basic scanning first
   - Add streaming support
   - Then add informer caching

3. **Add Tests**:
   ```bash
   touch plugins/kubernetes-provider/tests/scanner_test.go
   touch plugins/kubernetes-provider/tests/relationships_test.go
   ```

4. **Update Documentation**:
   - Document new methods in provider READMEs
   - Add examples of relationship extraction

5. **Performance Testing**:
   - Benchmark streaming vs batch scanning
   - Test with large clusters/environments

## Testing Strategy

For each placeholder implementation:
1. Write unit tests that verify the interface is satisfied
2. Add integration tests with mock data
3. Create end-to-end tests with real providers (requires credentials)

## Notes

- All implementations compile and run, but return limited/empty results
- Placeholders allow the system to function while development continues
- Each provider can be enhanced independently
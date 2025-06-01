# GCP Provider Auto-Discovery Implementation Guide

## Plugin Installation & Setup

The GCP provider is integrated into Corkscrew's plugin architecture, following the same patterns as AWS and Azure providers.

### Quick Start

```bash
# Install/enable the GCP provider plugin
corkscrew plugin build gcp

# Verify plugin installation
corkscrew plugin status

# List all available plugins
corkscrew plugin list

# Initialize and use GCP provider
corkscrew scan --provider gcp --services compute,storage --region us-central1
corkscrew discover --provider gcp
corkscrew info --provider gcp
```

### Plugin Architecture

The GCP provider implements Corkscrew's `CloudProvider` interface via gRPC plugin:

```go
// main.go - Plugin entry point
func main() {
    gcpProvider := NewGCPProvider()
    
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "provider": &shared.CloudProviderGRPCPlugin{Impl: gcpProvider},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

## Vision: Automatic GCP Resource Discovery

### Current State (Implemented)
- Full plugin architecture integration with `corkscrew plugin build gcp`
- Cloud Asset Inventory integration as primary discovery method
- Client library fallback for detailed resource scanning
- Multi-level caching and rate limiting
- Support for projects, folders, and organization-wide scanning
- Hybrid approach: Asset Inventory + SDK APIs

### Enhanced State (Auto-Discovery Goals)
- Automatic discovery of ALL GCP resources via client library analysis
- Auto-generated scanners for every List/Get operation
- Complete service coverage across all Google Cloud APIs
- Zero manual coding for new GCP services

## Current Implementation Architecture

### Provider Structure
The GCP provider follows the established Corkscrew pattern:

```go
type GCPProvider struct {
    // Core components (matches AWS/Azure pattern)
    assetInventory *AssetInventoryClient
    discovery      *ServiceDiscovery
    scanner        *ResourceScanner
    schemaGen      *GCPSchemaGenerator
    clientFactory  *ClientFactory
    relationships  *RelationshipExtractor
    
    // Performance components (same as AWS/Azure)
    cache          *MultiLevelCache
    rateLimiter    *rate.Limiter
    maxConcurrency int
}
```

### Hybrid Discovery Approach
Like AWS (Resource Explorer + SDK) and Azure (Resource Graph + ARM), GCP uses:

1. **Primary**: Cloud Asset Inventory for bulk resource discovery
2. **Fallback**: Client library scanning for detailed enumeration
3. **Optimization**: Multi-level caching and rate limiting

```go
func (p *GCPProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
    // Initialize Cloud Asset Inventory (like AWS Resource Explorer)
    assetClient, err := NewAssetInventoryClient(ctx)
    if err != nil {
        log.Printf("Cloud Asset Inventory not available: %v", err)
        log.Printf("Will use standard API scanning instead")
    } else {
        if assetClient.IsHealthy(ctx) {
            p.assetInventory = assetClient
            p.scanner.SetAssetInventory(assetClient)
        }
    }
    
    // Initialize components
    p.discovery = NewServiceDiscovery(p.clientFactory)
    p.scanner = NewResourceScanner(p.clientFactory)
    // ... rest of initialization
}
```

## Auto-Discovery Implementation Plan

### Phase 1: GCP Client Library Analysis
Analyze Google Cloud Go client libraries to discover all available resources:

```bash
# Build analyzer tool
go run ./cmd/gcp-analyzer/main.go --analyze-libraries --output analysis.json

# Generate scanners from analysis
go run ./cmd/generator/main.go --input analysis.json --output-dir generated/
```

**Target Libraries**:
- `cloud.google.com/go/compute/apiv1`
- `cloud.google.com/go/storage`
- `cloud.google.com/go/container/apiv1`
- `cloud.google.com/go/bigquery`
- `cloud.google.com/go/cloudsql/apiv1`
- And 100+ other Google Cloud services

**Discovery Patterns**:
```go
// Pattern 1: Iterator-based listing
it := client.Instances.List(project, zone)
for {
    instance, err := it.Next()
    if err == iterator.Done { break }
}

// Pattern 2: Pages-based iteration
err := client.Instances.List(project, zone).Pages(ctx, func(page *computepb.InstanceList) error {
    for _, instance := range page.Items {
        // Process instance
    }
    return nil
})

// Pattern 3: Asset Inventory mapping
assetType := "compute.googleapis.com/Instance"
```

### Phase 2: Auto-Generated Scanner Framework
Generate scanners that match the established Corkscrew pattern:

```go
// Generated scanner template
type {{.ServiceName}}Scanner struct {
    assetInventory *AssetInventoryClient  // Primary discovery
    clientFactory  *ClientFactory         // Fallback scanning
    rateLimiter    *rate.Limiter         // Performance control
    cache          *Cache                // Resource caching
}

func (s *{{.ServiceName}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
    // Try Asset Inventory first (like AWS Resource Explorer)
    if s.assetInventory != nil && s.assetInventory.IsHealthy(ctx) {
        resources, err := s.scanWithAssetInventory(ctx)
        if err == nil {
            return resources, nil
        }
        log.Printf("Asset Inventory failed, using client library: %v", err)
    }
    
    // Fall back to client library scanning (like AWS SDK scanning)
    return s.scanWithClientLibrary(ctx)
}
```

### Phase 3: Integration with Plugin System
Auto-discovery integrates seamlessly with the existing plugin architecture:

```bash
# Plugin management (same commands as AWS/Azure)
corkscrew plugin build gcp          # Build with auto-discovery
corkscrew plugin status             # Check all plugins including GCP
corkscrew plugin list --verbose     # Show plugin details

# Usage remains identical
corkscrew scan --provider gcp --services compute,storage
corkscrew discover --provider gcp   # Now discovers ALL services
```

## Service Integration Examples

### Current Manual Implementation
```go
// compute service (manually implemented)
func (s *ComputeScanner) scanInstances(ctx context.Context, project, zone string) {
    client := s.clientFactory.GetComputeClient(ctx)
    req := &computepb.ListInstancesRequest{
        Project: project,
        Zone:    zone,
    }
    it := client.Instances.List(ctx, req)
    // ... manual iteration
}
```

### Auto-Generated Implementation
```go
// Auto-generated from library analysis
func (s *ComputeScanner) scanInstances(ctx context.Context, project, zone string) {
    // Generated from analyzing computepb.ListInstancesRequest
    return s.genericScan(ctx, &ScanConfig{
        AssetType:    "compute.googleapis.com/Instance",
        ListMethod:   s.client.Instances.List,
        ResourcePath: "projects/{project}/zones/{zone}/instances",
        Pagination:   IteratorPattern,
    })
}
```

## Schema Generation

### GCP-Specific Schema Patterns
```sql
-- Auto-generated from proto definitions
CREATE TABLE gcp_compute_instances (
    -- Standard fields (consistent across providers)
    id VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    type VARCHAR DEFAULT 'compute.googleapis.com/Instance',
    
    -- GCP-specific hierarchy
    project_id VARCHAR NOT NULL,
    zone VARCHAR NOT NULL,
    region VARCHAR GENERATED ALWAYS AS (regexp_extract(zone, '(.*)-[a-z]$', 1)) STORED,
    
    -- Resource properties (from proto analysis)
    machine_type VARCHAR,
    status VARCHAR,
    creation_timestamp TIMESTAMP,
    
    -- GCP labels (not tags like AWS)
    labels JSON,
    
    -- Complex nested structures
    network_interfaces JSON,
    disks JSON,
    service_accounts JSON,
    
    -- Metadata
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    raw_data JSON,
    
    -- Indexes for common queries
    INDEX idx_project_zone (project_id, zone),
    INDEX idx_machine_type (machine_type),
    INDEX idx_status (status)
);
```

## Testing and Validation

### Plugin Testing Framework
```bash
# Test plugin functionality (same as AWS/Azure)
./gcp-provider --test                     # Unit tests
./gcp-provider --test-gcp                 # Integration tests
./gcp-provider --check-asset-inventory    # Asset Inventory connectivity
./gcp-provider --demo                     # Demo analyzer functionality
```

### Auto-Discovery Validation
```bash
# Validate auto-discovery results
go run ./cmd/validator/main.go --provider gcp --verify-coverage
go run ./cmd/validator/main.go --provider gcp --compare-manual-vs-auto
```

## Migration Path

### Phase 1: Plugin Integration (✅ Complete)
- [x] Plugin architecture implementation
- [x] `corkscrew plugin build gcp` support
- [x] Hybrid Asset Inventory + client library approach
- [x] Rate limiting and caching

### Phase 2: Enhanced Discovery (In Progress)
- [ ] Client library analyzer implementation
- [ ] Auto-generated scanner framework
- [ ] Proto-based schema generation
- [ ] Comprehensive service coverage

### Phase 3: Full Auto-Discovery (Future)
- [ ] Zero-configuration service discovery
- [ ] Automatic schema evolution
- [ ] Dynamic plugin updates
- [ ] Cross-provider relationship analysis

## Claude Prompts for Enhanced Implementation

### Prompt 1: GCP Client Library Analyzer
```
I need to enhance the GCP provider's auto-discovery by implementing a client library analyzer that discovers all GCP resources automatically.

The analyzer should:
1. Parse Google Cloud Go libraries from cloud.google.com/go/*
2. Identify List/Get patterns and map them to Asset Inventory types
3. Extract resource hierarchies (project/region/zone)
4. Generate scanner configurations

Following the established Corkscrew pattern, integrate with the existing GCP provider structure:
- Use hybrid Asset Inventory + SDK approach (like AWS Resource Explorer)
- Maintain compatibility with plugin system (`corkscrew plugin build gcp`)
- Follow the same caching and rate limiting patterns as AWS/Azure

Current implementation is in ./plugins/gcp-provider/ - extend the existing GCPProvider struct and scanner framework.
```

### Prompt 2: Asset Inventory Query Optimizer
```
Enhance the GCP provider's Asset Inventory integration to match the sophistication of AWS Resource Explorer and Azure Resource Graph.

Implement optimized query patterns:
1. Bulk asset discovery across organizations/folders/projects
2. Efficient filtering and pagination
3. Relationship extraction from asset data
4. Integration with existing scanner fallback mechanisms

The optimizer should work within the established plugin framework and maintain API compatibility with the current GCPProvider interface.
```

### Prompt 3: Auto-Generated Scanner Integration
```
Implement auto-generated scanners that integrate seamlessly with the existing GCP plugin architecture.

Requirements:
1. Maintain compatibility with current plugin system
2. Follow the established patterns from AWS/Azure providers
3. Support the hybrid Asset Inventory + client library approach
4. Generate scanners that implement the existing ServiceScanner interface

The generated scanners should work with the current `corkscrew scan --provider gcp` commands without breaking changes.
```

## Conclusion

The GCP provider is fully integrated into Corkscrew's plugin architecture with `corkscrew plugin build gcp` enabling the provider. The foundation for auto-discovery is in place with Asset Inventory integration and client library fallback, following the same patterns as AWS and Azure providers.

Future enhancements will build upon this foundation to provide comprehensive auto-discovery capabilities while maintaining full compatibility with the established plugin system.
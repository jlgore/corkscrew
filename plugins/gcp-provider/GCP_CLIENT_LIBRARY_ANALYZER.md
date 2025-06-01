# GCP Client Library Analyzer

This document describes the GCP Client Library Analyzer implementation for Corkscrew, which automatically discovers GCP resources by parsing Google Cloud Go client libraries.

## Overview

The GCP Client Library Analyzer provides automatic resource discovery by:

1. **Parsing GCP client libraries** from `cloud.google.com/go/*` packages
2. **Identifying resource patterns** like List*, Get*, Create*, Update*, Delete* methods
3. **Detecting iterator patterns** with Next() and Pages() methods
4. **Mapping to Asset Inventory types** for efficient scanning
5. **Analyzing parent-child relationships** and resource hierarchies
6. **Generating structured output** for scanner generation

## Architecture

### Core Components

#### ClientLibraryAnalyzer
- Parses Go source code and documentation
- Identifies resource operation patterns
- Maps client methods to Asset Inventory types
- Generates service mappings with resource patterns

#### ResourceHierarchyAnalyzer  
- Analyzes GCP resource name patterns
- Detects parent-child relationships
- Maps resource scopes (global, regional, zonal, etc.)
- Generates relationship data for the orchestrator

#### Integration with ServiceDiscovery
- Enhanced service discovery using library analysis
- Fallback to API-based discovery
- Merging of library-analyzed and API-discovered services

## Supported Patterns

### Resource Operation Patterns

```go
// List patterns - return iterators
func (c *Client) ListInstances(ctx context.Context, req *ListInstancesRequest) *InstanceIterator
func (c *Client) ListBuckets(project string) *BucketIterator

// Get patterns - return single resources  
func (c *Client) GetInstance(ctx context.Context, req *GetInstanceRequest) (*Instance, error)
func (c *Client) GetBucket(name string) *BucketHandle

// Iterator patterns with Next()
for {
    obj, err := it.Next()
    if err == iterator.Done {
        break
    }
    // Process obj
}

// Pagination with Pages()
it := client.Instances.List(project, zone).Pages(ctx, func(page *InstanceList) error {
    for _, instance := range page.Items {
        // Process instance
    }
    return nil
})
```

### Asset Inventory Type Mapping

The analyzer automatically maps client methods to Asset Inventory types:

```go
// Examples of automatic mappings
compute.ListInstances     → compute.googleapis.com/Instance
storage.ListBuckets       → storage.googleapis.com/Bucket  
container.ListClusters    → container.googleapis.com/Cluster
bigquery.ListDatasets     → bigqueryadmin.googleapis.com/Dataset
pubsub.ListTopics         → pubsub.googleapis.com/Topic
```

### Parent Relationship Detection

Analyzes GCP resource name patterns to detect hierarchies:

```go
// Resource name patterns analyzed
"projects/{project}/zones/{zone}/instances/{instance}"     // Zonal
"projects/{project}/regions/{region}/subnetworks/{subnet}" // Regional  
"projects/{project}/global/networks/{network}"             // Global
"projects/{project}/buckets/{bucket}"                      // Project-scoped
"projects/{project}/datasets/{dataset}/tables/{table}"     // Dataset-scoped
```

## Usage

### Running the Demo

```bash
# Run the analyzer demo
cd /home/jg/git/corkscrew/plugins/gcp-provider
go run . --demo

# Save detailed analysis report
go run . --demo --save-report
```

### Integration with GCP Provider

The analyzer is automatically integrated with the GCP provider's service discovery:

```go
// Enhanced service discovery in discovery.go
services, err := p.discovery.DiscoverServicesWithLibraryAnalysis(ctx, p.projectIDs)
```

### Programmatic Usage

```go
// Create analyzer
analyzer := NewClientLibraryAnalyzer(goPath, "cloud.google.com/go")

// Analyze libraries
services, err := analyzer.AnalyzeGCPLibraries(ctx)

// Get service mappings
mapping := analyzer.ServiceMappings["compute"]

// Analyze hierarchies
hierarchyAnalyzer := NewResourceHierarchyAnalyzer(analyzer.ServiceMappings)
err = hierarchyAnalyzer.AnalyzeResourceHierarchies(ctx)
relationships := hierarchyAnalyzer.GenerateHierarchyRelationships()
```

## Output Structure

### Service Information

```json
{
  "name": "compute",
  "display_name": "Compute Engine", 
  "package_name": "cloud.google.com/go/compute/apiv1",
  "client_type": "google-cloud-go",
  "resource_types": [
    {
      "name": "Instance",
      "type_name": "compute.googleapis.com/Instance"
    }
  ]
}
```

### Resource Patterns

```json
{
  "method_name": "ListInstances",
  "resource_type": "Instance", 
  "list_method": true,
  "pagination_style": "pages",
  "parameters": ["project", "zone"],
  "asset_type": "compute.googleapis.com/Instance",
  "metadata": {
    "scope": "zonal"
  }
}
```

### Parent Relationships

```json
{
  "child_resource": "Instance",
  "parent_resource": "Zone", 
  "required_params": ["project", "zone", "instance"],
  "scope": "zonal"
}
```

## Features

### Automatic Discovery
- Scans all `cloud.google.com/go/*` packages
- Parses Go source code and documentation
- Identifies resource patterns using regex and AST analysis
- Falls back to well-known service patterns

### Asset Inventory Integration
- Maps all discovered patterns to Asset Inventory types
- Enables efficient bulk resource scanning
- Supports both API and Asset Inventory scanning modes

### Hierarchy Analysis
- Analyzes GCP resource name patterns
- Detects scope requirements (project, region, zone, etc.)
- Maps parent-child relationships
- Supports cross-service relationships

### Comprehensive Coverage
- Supports 25+ GCP services out of the box
- Covers major resource types for each service
- Extensible pattern matching for new services
- Fallback to static patterns when parsing fails

## Implementation Details

### Pattern Detection

The analyzer uses multiple techniques to detect resource patterns:

1. **Method Name Analysis**: Regex patterns for List*, Get*, Create*, etc.
2. **Return Type Analysis**: Identifies iterator types and resource types  
3. **Parameter Analysis**: Extracts required parameters from function signatures
4. **Documentation Parsing**: Analyzes Go doc comments for additional context

### Scope Detection

Resource scope is detected using:

1. **Parameter Analysis**: Presence of zone, region, location parameters
2. **Service-Specific Rules**: Each service has scope defaults
3. **Resource Name Patterns**: Analysis of GCP resource name formats
4. **Metadata Extraction**: From existing resource patterns

### Error Handling

- Graceful fallback to API discovery when library parsing fails
- Continues analysis when individual packages fail to parse
- Uses static patterns for well-known services as backup
- Comprehensive error logging for debugging

## Benefits

1. **Automatic Discovery**: No manual configuration of resource types
2. **Complete Coverage**: Discovers all resources supported by client libraries  
3. **Asset Inventory Integration**: Efficient bulk scanning capabilities
4. **Relationship Mapping**: Understands resource hierarchies and dependencies
5. **Future-Proof**: Automatically discovers new services and resource types
6. **Performance**: Optimized scanning using appropriate APIs and patterns

## Testing

The analyzer includes comprehensive testing modes:

```bash
# Test the analyzer
go run . --demo

# Test with real GCP credentials  
go run . --test-gcp

# Check Asset Inventory setup
go run . --check-asset-inventory
```

## Files

- `client_library_analyzer.go` - Main analyzer implementation
- `resource_hierarchy_analyzer.go` - Hierarchy and relationship analysis
- `analyzer_demo.go` - Demo and testing functionality
- `discovery.go` - Integration with service discovery
- `GCP_CLIENT_LIBRARY_ANALYZER.md` - This documentation

The analyzer represents a significant advancement in cloud resource discovery, providing automatic, comprehensive, and efficient scanning of GCP resources through intelligent analysis of client library patterns.
# GCP Scanner Generator

A code generation tool that creates GCP resource scanners following the Asset Inventory → Client Library fallback pattern.

## Overview

This scanner generator creates Go code that implements efficient GCP resource discovery using:

1. **Asset Inventory First**: Fast bulk querying using Cloud Asset Inventory API
2. **Client Library Fallback**: Falls back to individual GCP service APIs when Asset Inventory fails
3. **GCP-Specific Patterns**: Handles projects, regions, zones, labels, and IAM bindings

## Generated Scanner Pattern

Each generated scanner follows this structure:

```go
type {{.ServiceName}}Scanner struct {
    assetInventory *AssetInventoryClient
    clientFactory  *ClientFactory
    assetTypes     []string // Asset Inventory types for this service
}

func (s *{{.ServiceName}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
    // Try Asset Inventory first
    if s.assetInventory != nil && s.assetInventory.IsHealthy(ctx) {
        resources, err := s.assetInventory.QueryAssetsByType(ctx, s.assetTypes)
        if err == nil {
            return resources, nil
        }
        log.Printf("Asset Inventory failed for {{.ServiceName}}, using client library: %v", err)
    }
    
    // Fall back to client library scanning
    return s.scanUsingSDK(ctx)
}

func (s *{{.ServiceName}}Scanner) scanUsingSDK(ctx context.Context) ([]*pb.ResourceRef, error) {
    client, err := s.clientFactory.Get{{.ServiceName}}Client(ctx)
    if err != nil {
        return nil, err
    }
    
    var resources []*pb.ResourceRef
    
    {{range .ResourceTypes}}
    // List {{.Name}} resources
    {{if .IsRegional}}
    // Iterate through all regions
    for _, region := range s.getRegions(ctx) {
        {{.ListCode}}
    }
    {{else if .IsZonal}}
    // Iterate through all zones
    for _, zone := range s.getZones(ctx) {
        {{.ListCode}}
    }
    {{else}}
    // Global resource
    {{.ListCode}}
    {{end}}
    {{end}}
    
    return resources, nil
}
```

## Usage

### Command Line Generator

Build and use the command-line generator:

```bash
# Build the generator
go build -o plugins/gcp-provider/cmd/gcp-scanner-generator/gcp-scanner-generator plugins/gcp-provider/cmd/gcp-scanner-generator/main.go

# List available services
./plugins/gcp-provider/cmd/gcp-scanner-generator/gcp-scanner-generator -list

# Generate scanner for specific service
./plugins/gcp-provider/cmd/gcp-scanner-generator/gcp-scanner-generator -service compute -output ./generated

# Generate all scanners
./plugins/gcp-provider/cmd/gcp-scanner-generator/gcp-scanner-generator -service all -output ./generated
```

### Programmatic Usage

```go
// Generate scanners for all services
err := GenerateAllGCPScanners("./output")

// Generate scanner for specific service
err := GenerateGCPServiceScanner("compute", "./output")
```

### Using Generated Scanners

```go
// Initialize clients
clientFactory := NewClientFactory()
assetInventory, _ := NewAssetInventoryClient(ctx)

// Create scanner registry
registry := NewScannerRegistry(clientFactory, assetInventory)

// Use specific scanner
computeScanner, _ := registry.GetScanner("compute")
resources, err := computeScanner.Scan(ctx)

// Or scan all services
allScanners := registry.GetAllScanners()
for name, scanner := range allScanners {
    resources, err := scanner.Scan(ctx)
    // Process resources...
}
```

## Supported Services

The generator currently supports these GCP services:

- **compute** - Compute Engine (instances, disks, networks, subnetworks)
- **storage** - Cloud Storage (buckets)  
- **container** - Kubernetes Engine (clusters, node pools)
- **bigquery** - BigQuery (datasets, tables)
- **cloudsql** - Cloud SQL (instances)

## GCP-Specific Features

### Project/Zone/Region Iteration

Generated scanners automatically handle GCP's hierarchical resource structure:

```go
// Regional resources
for _, region := range s.getRegions(ctx) {
    // List resources in each region
}

// Zonal resources  
for _, zone := range s.getZones(ctx) {
    // List resources in each zone
}

// Global resources
// Listed once across all projects
```

### Label Extraction

GCP labels (not tags) are automatically extracted:

```go
// Add labels to resource
for k, v := range resource.Labels {
    ref.BasicAttributes["label_"+k] = v
}
```

### Asset Type Mappings

Each service maps to specific Cloud Asset Inventory types:

```go
assetTypes: []string{
    "compute.googleapis.com/Instance",
    "compute.googleapis.com/Disk", 
    "compute.googleapis.com/Network",
    "compute.googleapis.com/Subnetwork",
}
```

### IAM Binding Discovery

Asset Inventory queries can discover IAM relationships:

```go
// Query IAM policies and extract relationships
relationships, err := assetInventory.QueryAssetRelationships(ctx)
```

## Generated Files

When you run the generator, it creates:

- `{service}_scanner.go` - Scanner implementation for each service
- `registry.go` - Service scanner registry for managing all scanners

## Customization

To add a new service or resource type:

1. Update `GetGCPServiceDefinitions()` in `gcp_service_definitions.go`
2. Add the service and its resource types with appropriate Asset Inventory mappings
3. Regenerate scanners

Example:

```go
"newservice": {
    Name:        "newservice",
    ClientType:  "*newservice.Service", 
    PackageName: "newservice",
    ResourceTypes: []GCPResourceType{
        {
            Name:               "Resource",
            TypeName:           "Resource",
            AssetInventoryType: "newservice.googleapis.com/Resource",
            IsRegional:         true,
            ListCode:           "// Implementation code here",
        },
    },
},
```

## Benefits

1. **Performance**: Asset Inventory provides bulk querying across projects
2. **Reliability**: Automatic fallback to individual APIs if Asset Inventory fails
3. **Completeness**: Handles all GCP resource scopes (global, regional, zonal)
4. **Discovery**: Built-in IAM relationship discovery
5. **Consistency**: Standardized scanner interface across all services
6. **Maintainability**: Generated code follows consistent patterns

## Dependencies

- Cloud Asset Inventory API access
- Individual GCP service API access  
- Appropriate IAM permissions for resource discovery
- GCP client libraries for Go
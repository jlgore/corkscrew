# Corkscrew Plugin Development Guide

This guide explains how to develop cloud provider plugins for Corkscrew, enabling support for AWS, Azure, Google Cloud Platform (GCP), Kubernetes, and other cloud providers.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Plugin Interface](#plugin-interface)
3. [Directory Structure](#directory-structure)
4. [Real Implementation Examples](#real-implementation-examples)
5. [Step-by-Step Implementation](#step-by-step-implementation)
6. [Build and Deployment](#build-and-deployment)
7. [Testing Your Plugin](#testing-your-plugin)
8. [Plugin Registration](#plugin-registration)
9. [Best Practices](#best-practices)

## Architecture Overview

Corkscrew uses a plugin-based architecture with HashiCorp's go-plugin framework for cloud provider integration. Each provider is a separate binary that communicates with the main Corkscrew application via gRPC.

### Key Components

- **Plugin Binary**: Standalone executable implementing the CloudProvider interface
- **Unified Scanner**: Single discovery engine that handles all services dynamically
- **Service Discovery**: Automatically detects available services from cloud provider SDK
- **Resource Enrichment**: Collects detailed resource configuration via SDK operations  
- **Schema Generator**: Creates database schemas for discovered resources
- **Rate Limiter**: Handles API rate limiting and retries per service
- **Client Factory**: Manages SDK client creation and caching

### Communication Flow

```
Corkscrew CLI ←→ gRPC ←→ Provider Plugin ←→ Unified Scanner ←→ Cloud SDK ←→ Cloud APIs
      ↓                                          ↓
  DuckDB Storage                        Resource Discovery & Enrichment
```

### Simplified Architecture (v2)

The new plugin architecture eliminates complex code generation and circular dependencies:

- **No Generated Scanners**: Single UnifiedScanner handles all services
- **Direct Integration**: Scanner registry uses UnifiedScanner directly
- **Separated Concerns**: Plugin discovers resources, CLI persists to database
- **Dynamic Service Support**: Automatic detection of new cloud services

### Plugin Handshake

All plugins use a shared handshake configuration:

```go
var HandshakeConfig = plugin.HandshakeConfig{
    ProtocolVersion:  2,
    MagicCookieKey:   "CORKSCREW_PLUGIN",
    MagicCookieValue: "v2-provider-plugin",
}
```

## Plugin Interface

All cloud provider plugins must implement the `CloudProvider` interface defined in the protobuf specification:

```protobuf
service CloudProvider {
  // Plugin lifecycle
  rpc Initialize(InitializeRequest) returns (InitializeResponse);
  rpc GetProviderInfo(Empty) returns (ProviderInfoResponse);
  
  // Service discovery and generation
  rpc DiscoverServices(DiscoverServicesRequest) returns (DiscoverServicesResponse);
  rpc GenerateServiceScanners(GenerateScannersRequest) returns (GenerateScannersResponse);
  
  // Resource operations following Discovery -> List -> Describe pattern
  rpc ListResources(ListResourcesRequest) returns (ListResourcesResponse);
  rpc DescribeResource(DescribeResourceRequest) returns (DescribeResourceResponse);
  
  // Schema and metadata
  rpc GetSchemas(GetSchemasRequest) returns (SchemaResponse);
  
  // Batch operations
  rpc BatchScan(BatchScanRequest) returns (BatchScanResponse);
  rpc StreamScan(StreamScanRequest) returns (stream Resource);
}
```

The corresponding Go interface in `internal/shared/plugin.go`:

```go
type CloudProvider interface {
    // Plugin lifecycle
    Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error)
    GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error)

    // Service discovery and generation
    DiscoverServices(ctx context.Context, req *pb.DiscoverServicesRequest) (*pb.DiscoverServicesResponse, error)
    GenerateServiceScanners(ctx context.Context, req *pb.GenerateScannersRequest) (*pb.GenerateScannersResponse, error)

    // Resource operations following Discovery -> List -> Describe pattern
    ListResources(ctx context.Context, req *pb.ListResourcesRequest) (*pb.ListResourcesResponse, error)
    DescribeResource(ctx context.Context, req *pb.DescribeResourceRequest) (*pb.DescribeResourceResponse, error)

    // Schema and metadata
    GetSchemas(ctx context.Context, req *pb.GetSchemasRequest) (*pb.SchemaResponse, error)

    // Batch operations
    BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error)
    StreamScan(req *pb.StreamScanRequest, stream pb.CloudProvider_StreamScanServer) error
}
```

## Directory Structure

Create your plugin following this structure (based on existing AWS and Azure providers):

```
plugins/
├── your-provider/
│   ├── main.go                    # Plugin entry point
│   ├── your_provider.go           # Main provider implementation
│   ├── go.mod                     # Go module definition
│   ├── go.sum                     # Go module checksums
│   ├── discovery.go               # Service discovery logic
│   ├── scanner.go                 # Resource scanning logic
│   ├── schema_generator.go        # Schema generation
│   ├── client_factory.go          # SDK client management
│   ├── relationships.go           # Resource relationships (optional)
│   ├── resource_explorer.go       # Advanced resource discovery (optional)
│   └── test_*.go                  # Test files
├── build-your-provider.sh         # Build script
└── plugins.json                   # Plugin registry
```

If you build a plugin for a new cloud provider or have improvements to the current plugins feel free to open a PR or an issue. Claude's DNA is all over this thing in good ways and bad - however it can improve for the better I am here for it :->

## Real Implementation Examples

### AWS Provider Structure

The AWS provider (`plugins/aws-provider/`) demonstrates a full-featured implementation:

```
aws-provider/
├── main.go                     # Entry point with test flags
├── aws_provider.go             # Main provider with Resource Explorer support
├── discovery.go                # Service discovery using AWS SDK
├── scanner.go                  # Resource scanning with rate limiting
├── schema_generator.go         # SQL schema generation
├── client_factory.go           # AWS client management
├── relationships.go            # Cross-service relationships
├── resource_explorer.go        # AWS Resource Explorer integration
├── aws_dynamic_provider.go     # Dynamic scanner generation
├── go.mod                      # Dependencies (AWS SDK v2)
└── test_*.go                   # Various test files
```

### Azure Provider Structure

The Azure provider (`plugins/azure-provider/`) shows another approach:

```
azure-provider/
├── main.go                     # Simple entry point
├── azure_provider.go           # Main provider with Resource Graph
├── discovery.go                # Service discovery via ARM
├── scanner.go                  # Resource scanning with Resource Graph
├── schema_generator.go         # Table schema generation
├── client_factory.go           # Azure client management
├── database_integration.go     # Database operations
├── resource_graph.go           # Azure Resource Graph queries
├── db_schema.go               # Database schema definitions
├── go.mod                     # Dependencies (Azure SDK)
└── test-azure-provider.sh      # Test script
```

## Step-by-Step Implementation

### Step 1: Create the Plugin Module

```bash
mkdir -p plugins/your-provider
cd plugins/your-provider
go mod init github.com/jlgore/corkscrew/plugins/your-provider
```

### Step 2: Define Dependencies

Based on the existing providers, your `go.mod` should look like:

```go
module github.com/jlgore/corkscrew/plugins/your-provider

go 1.24

require (
    // Your cloud provider SDK
    github.com/your-cloud/sdk v1.0.0
    
    // Required Corkscrew dependencies
    github.com/hashicorp/go-plugin v1.6.0
    github.com/jlgore/corkscrew v0.0.0
    google.golang.org/protobuf v1.36.6
    
    // Optional but recommended
    golang.org/x/time v0.8.0  // For rate limiting
)

replace github.com/jlgore/corkscrew => ../..
```

### Step 3: Implement the Main Entry Point

Create `main.go` following the established pattern:

```go
package main

import (
    "os"
    
    "github.com/hashicorp/go-plugin"
    "github.com/jlgore/corkscrew/internal/shared"
)

func main() {
    // Optional: Add test flags like AWS provider
    if len(os.Args) > 1 && os.Args[1] == "--test" {
        testPlugin()
        return
    }

    // Create the provider implementation
    provider := NewYourProvider()

    // Serve the plugin using shared configuration
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "provider": &shared.CloudProviderGRPCPlugin{Impl: provider},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

### Step 4: Implement the Provider Structure

Create `your_provider.go` with the main provider struct:

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"
    
    // Your cloud SDK imports
    pb "github.com/jlgore/corkscrew/internal/proto"
    "golang.org/x/time/rate"
    "google.golang.org/protobuf/types/known/timestamppb"
)

// YourProvider implements the CloudProvider interface
type YourProvider struct {
    mu          sync.RWMutex
    initialized bool
    
    // Cloud-specific configuration
    credential  interface{} // Your cloud credential type
    region      string
    projectID   string      // Or subscription ID, account ID, etc.

    // Core components
    discovery     *ServiceDiscovery
    scanner       *ResourceScanner
    schemaGen     *SchemaGenerator
    clientFactory *ClientFactory

    // Performance components
    rateLimiter    *rate.Limiter
    maxConcurrency int
    
    // Caching
    cache *Cache
}

// NewYourProvider creates a new provider instance
func NewYourProvider() *YourProvider {
    return &YourProvider{
        rateLimiter:    rate.NewLimiter(rate.Limit(100), 200), // Adjust per your API limits
        maxConcurrency: 10,
        cache:          NewCache(24 * time.Hour),
    }
}

// Initialize sets up the provider with credentials and configuration
func (p *YourProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
    p.mu.Lock()
    defer p.mu.Unlock()

    // Extract configuration
    region := req.Config["region"]
    projectID := req.Config["project_id"] // Adjust key name for your cloud

    // Initialize cloud credentials (implement your auth logic)
    credential, err := p.initializeCredentials(ctx, req.Config)
    if err != nil {
        return &pb.InitializeResponse{
            Success: false,
            Error:   fmt.Sprintf("failed to initialize credentials: %v", err),
        }, nil
    }

    p.credential = credential
    p.region = region
    p.projectID = projectID

    // Initialize components
    p.clientFactory = NewClientFactory(credential, region)
    p.discovery = NewServiceDiscovery(p.clientFactory)
    p.scanner = NewResourceScanner(p.clientFactory)
    p.schemaGen = NewSchemaGenerator()

    p.initialized = true

    return &pb.InitializeResponse{
        Success: true,
        Version: "1.0.0",
        Metadata: map[string]string{
            "region":     region,
            "project_id": projectID,
        },
    }, nil
}

// GetProviderInfo returns provider metadata
func (p *YourProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
    return &pb.ProviderInfoResponse{
        Name:        "your-provider",
        Version:     "1.0.0",
        Description: "Your Cloud Provider plugin for Corkscrew",
        Capabilities: map[string]string{
            "discovery": "true",
            "scanning":  "true",
            "streaming": "true",
            "schemas":   "true",
        },
    }, nil
}

// Implement other interface methods...
```

### Step 5: Implement Service Discovery

Create `discovery.go`:

```go
package main

import (
    "context"
    "fmt"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
)

type ServiceDiscovery struct {
    clientFactory *ClientFactory
}

func NewServiceDiscovery(clientFactory *ClientFactory) *ServiceDiscovery {
    return &ServiceDiscovery{
        clientFactory: clientFactory,
    }
}

func (sd *ServiceDiscovery) DiscoverServices(ctx context.Context) ([]*pb.ServiceInfo, error) {
    // Use your cloud's API to discover available services
    // Example implementation structure:
    
    var services []*pb.ServiceInfo
    
    // Method 1: Static list of known services
    knownServices := []string{"compute", "storage", "database", "networking"}
    
    for _, serviceName := range knownServices {
        services = append(services, &pb.ServiceInfo{
            Name:        serviceName,
            DisplayName: fmt.Sprintf("Your Cloud %s", serviceName),
            PackageName: fmt.Sprintf("your-cloud-%s", serviceName),
        })
    }
    
    // Method 2: Dynamic discovery via API (preferred)
    // client := sd.clientFactory.GetServiceCatalogClient()
    // apiServices, err := client.ListServices(ctx)
    // if err != nil {
    //     return nil, fmt.Errorf("failed to discover services: %w", err)
    // }
    
    return services, nil
}
```

### Step 6: Implement Resource Scanning

Create `scanner.go`:

```go
package main

import (
    "context"
    "fmt"
    "sync"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
    "golang.org/x/time/rate"
)

type ResourceScanner struct {
    clientFactory *ClientFactory
    rateLimiter  *rate.Limiter
}

func NewResourceScanner(clientFactory *ClientFactory) *ResourceScanner {
    return &ResourceScanner{
        clientFactory: clientFactory,
        rateLimiter:  rate.NewLimiter(rate.Limit(50), 100),
    }
}

func (rs *ResourceScanner) ScanService(ctx context.Context, serviceName, region string) ([]*pb.Resource, error) {
    // Wait for rate limiter
    if err := rs.rateLimiter.Wait(ctx); err != nil {
        return nil, err
    }

    // Get service client
    client, err := rs.clientFactory.GetServiceClient(serviceName)
    if err != nil {
        return nil, fmt.Errorf("failed to get client for %s: %w", serviceName, err)
    }

    // Scan resources using your cloud's SDK
    // This is where you'll implement the actual resource discovery logic
    var resources []*pb.Resource

    switch serviceName {
    case "compute":
        resources, err = rs.scanComputeResources(ctx, client, region)
    case "storage":
        resources, err = rs.scanStorageResources(ctx, client, region)
    default:
        return nil, fmt.Errorf("unsupported service: %s", serviceName)
    }

    return resources, err
}

func (rs *ResourceScanner) scanComputeResources(ctx context.Context, client interface{}, region string) ([]*pb.Resource, error) {
    // Implement your cloud's compute resource scanning
    // Example structure:
    
    var resources []*pb.Resource
    
    // Cast client to your specific type
    // computeClient := client.(YourComputeClient)
    
    // List instances/VMs
    // instances, err := computeClient.ListInstances(ctx)
    // if err != nil {
    //     return nil, err
    // }
    
    // for _, instance := range instances {
    //     resource := &pb.Resource{
    //         Provider:    "your-provider",
    //         Service:     "compute",
    //         Type:        "Instance",
    //         Id:          instance.ID,
    //         Name:        instance.Name,
    //         Region:      region,
    //         DiscoveredAt: timestamppb.Now(),
    //         // ... other fields
    //     }
    //     resources = append(resources, resource)
    // }
    
    return resources, nil
}
```

### Step 7: Implement Other Required Methods

You'll need to implement all the methods in the CloudProvider interface. Look at the AWS and Azure providers for examples of:

- `BatchScan`: Concurrent scanning of multiple services
- `StreamScan`: Streaming results for large datasets
- `GetSchemas`: Generating database schemas
- `ListResources` and `DescribeResource`: Resource operations

## Build and Deployment

### Build Script

Create `build-your-provider.sh` following the established pattern:

```bash
#!/bin/bash

echo "🔧 Building Your Provider Plugin..."

# Ensure build directory exists
mkdir -p plugins/build

# Build the provider
cd plugins/your-provider
echo "📦 Building your-provider..."
go build -o ../build/corkscrew-your-provider .

if [ $? -eq 0 ]; then
    echo "✅ Your Provider built successfully!"
    echo "📁 Binary location: plugins/build/corkscrew-your-provider"
    echo "📊 Size: $(du -h ../build/corkscrew-your-provider | cut -f1)"
    
    # Make it executable
    chmod +x ../build/corkscrew-your-provider
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew scan --provider your-provider --services compute,storage --region us-west-1"
```

### Plugin Binary Naming

Your binary should be named following the pattern: `corkscrew-{provider-name}`

Examples from existing providers:
- AWS: `corkscrew-aws` or `aws-provider`
- Azure: `corkscrew-azure`

## Testing Your Plugin

### Basic Plugin Test

Create a simple test to verify your plugin works:

```go
// test_plugin.go
package main

import (
    "context"
    "fmt"
    "log"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
)

func testPlugin() {
    log.Printf("🧪 Testing Your Provider Plugin...")
    
    provider := NewYourProvider()
    
    // Test initialization
    initReq := &pb.InitializeRequest{
        Provider: "your-provider",
        Config: map[string]string{
            "region":     "us-west-1",
            "project_id": "test-project",
        },
    }
    
    initResp, err := provider.Initialize(context.Background(), initReq)
    if err != nil {
        log.Fatalf("❌ Initialize failed: %v", err)
    }
    
    if !initResp.Success {
        log.Fatalf("❌ Initialize unsuccessful: %s", initResp.Error)
    }
    
    log.Printf("✅ Initialize successful, version: %s", initResp.Version)
    
    // Test service discovery
    discReq := &pb.DiscoverServicesRequest{ForceRefresh: true}
    discResp, err := provider.DiscoverServices(context.Background(), discReq)
    if err != nil {
        log.Fatalf("❌ Service discovery failed: %v", err)
    }
    
    log.Printf("✅ Discovered %d services", len(discResp.Services))
    for _, service := range discResp.Services {
        log.Printf("  - %s (%s)", service.Name, service.DisplayName)
    }
    
    log.Printf("🎉 Plugin test completed successfully!")
}
```

## Plugin Registration

### Adding to plugins.json

Add your provider to the `plugins/plugins.json` file:

```json
{
  "your_provider_services": {
    "compute": {
      "capabilities": ["scan", "stream", "schemas", "relationships"],
      "plugin_name": "corkscrew-your-provider-compute",
      "resource_types": ["Instance", "Volume", "SecurityGroup"]
    },
    "storage": {
      "capabilities": ["scan", "stream", "schemas", "relationships"],
      "plugin_name": "corkscrew-your-provider-storage",
      "resource_types": ["Bucket", "Object", "Snapshot"]
    }
  }
}
```

## Best Practices

### 1. Error Handling

```go
func (p *YourProvider) handleAPIError(err error, operation string) error {
    // Implement retry logic for transient errors
    if isRetryableError(err) {
        return fmt.Errorf("retryable error in %s: %w", operation, err)
    }
    return fmt.Errorf("permanent error in %s: %w", operation, err)
}

func isRetryableError(err error) bool {
    // Check for rate limiting, timeouts, etc.
    // Return true for errors that should be retried
    return false
}
```

### 2. Rate Limiting

```go
// Always use rate limiting to respect API limits
func (rs *ResourceScanner) makeAPICall(ctx context.Context, operation func() error) error {
    if err := rs.rateLimiter.Wait(ctx); err != nil {
        return err
    }
    return operation()
}
```

### 3. Caching

```go
type Cache struct {
    mu   sync.RWMutex
    data map[string]CacheEntry
}

type CacheEntry struct {
    Data      interface{}
    ExpiresAt time.Time
}

func (c *Cache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    entry, exists := c.data[key]
    if !exists || time.Now().After(entry.ExpiresAt) {
        return nil, false
    }
    return entry.Data, true
}
```

### 4. Structured Logging

```go
import "log/slog"

func (p *YourProvider) logOperation(operation string, duration time.Duration, err error) {
    logger := slog.With(
        "provider", "your-provider",
        "operation", operation,
        "duration_ms", duration.Milliseconds(),
    )
    
    if err != nil {
        logger.Error("Operation failed", "error", err)
    } else {
        logger.Info("Operation completed successfully")
    }
}
```

### 5. Configuration Validation

```go
type ProviderConfig struct {
    Region    string `json:"region"`
    ProjectID string `json:"project_id"`
    Endpoint  string `json:"endpoint,omitempty"`
}

func (c *ProviderConfig) Validate() error {
    if c.Region == "" {
        return fmt.Errorf("region is required")
    }
    if c.ProjectID == "" {
        return fmt.Errorf("project_id is required")
    }
    return nil
}
```

### 6. Resource Relationships

```go
type ResourceRelationship struct {
    SourceID   string                 `json:"source_id"`
    TargetID   string                 `json:"target_id"`
    Type       string                 `json:"type"` // "depends_on", "contains", "references"
    Properties map[string]interface{} `json:"properties"`
}

func (rs *ResourceScanner) discoverRelationships(ctx context.Context, resources []*pb.Resource) []*ResourceRelationship {
    var relationships []*ResourceRelationship
    
    // Example: Instance -> Volume relationships
    for _, resource := range resources {
        if resource.Type == "Instance" {
            // Parse attached volumes from resource attributes
            // Create relationships
        }
    }
    
    return relationships
}
```

### 7. Performance Optimization

```go
// Use worker pools for concurrent scanning
func (p *YourProvider) BatchScan(ctx context.Context, req *pb.BatchScanRequest) (*pb.BatchScanResponse, error) {
    semaphore := make(chan struct{}, p.maxConcurrency)
    var wg sync.WaitGroup
    var mu sync.Mutex
    var allResources []*pb.Resource
    
    for _, service := range req.Services {
        wg.Add(1)
        go func(serviceName string) {
            defer wg.Done()
            
            semaphore <- struct{}{} // Acquire
            defer func() { <-semaphore }() // Release
            
            resources, err := p.scanner.ScanService(ctx, serviceName, req.Region)
            if err != nil {
                log.Printf("Failed to scan %s: %v", serviceName, err)
                return
            }
            
            mu.Lock()
            allResources = append(allResources, resources...)
            mu.Unlock()
        }(service)
    }
    
    wg.Wait()
    
    return &pb.BatchScanResponse{
        Resources: allResources,
        Stats: &pb.ScanStats{
            ServicesScanned: int32(len(req.Services)),
            ResourcesFound:  int32(len(allResources)),
        },
    }, nil
}
```

This guide provides a comprehensive foundation for developing cloud provider plugins for Corkscrew based on the real implementations in the codebase. The AWS and Azure providers serve as excellent references for production-ready plugin development patterns. 
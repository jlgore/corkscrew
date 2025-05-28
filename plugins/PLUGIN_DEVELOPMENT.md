# Corkscrew Plugin Development Guide

This guide explains how to develop cloud provider plugins for Corkscrew, enabling support for Azure, Google Cloud Platform (GCP), Kubernetes, and other cloud providers.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Plugin Interface](#plugin-interface)
3. [Directory Structure](#directory-structure)
4. [Implementation Steps](#implementation-steps)
5. [Azure Provider Example](#azure-provider-example)
6. [GCP Provider Example](#gcp-provider-example)
7. [Kubernetes Provider Example](#kubernetes-provider-example)
8. [Testing and Validation](#testing-and-validation)
9. [Build and Deployment](#build-and-deployment)
10. [Best Practices](#best-practices)

## Architecture Overview

Corkscrew uses a plugin-based architecture with HashiCorp's go-plugin framework for cloud provider integration. Each provider is a separate binary that communicates with the main Corkscrew application via gRPC.

### Key Components

- **Plugin Binary**: Standalone executable implementing the CloudProvider interface
- **Service Discovery**: Dynamically discovers available services from the cloud provider's SDK
- **Resource Scanner**: Lists and describes cloud resources using SDK operations
- **Schema Generator**: Creates database schemas for discovered resources
- **Cache Manager**: Handles caching of discovered services and resources

### Communication Flow

```
Corkscrew Core ←→ gRPC ←→ Provider Plugin ←→ Cloud SDK ←→ Cloud Provider API
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
  
  // Resource operations
  rpc ListResources(ListResourcesRequest) returns (ListResourcesResponse);
  rpc DescribeResource(DescribeResourceRequest) returns (DescribeResourceResponse);
  
  // Schema and metadata
  rpc GetSchemas(GetSchemasRequest) returns (SchemaResponse);
  
  // Batch operations
  rpc BatchScan(BatchScanRequest) returns (BatchScanResponse);
  rpc StreamScan(StreamScanRequest) returns (stream Resource);
}
```

## Directory Structure

Create your plugin following this structure:

```
plugins/
├── your-provider/
│   ├── main.go                    # Plugin entry point
│   ├── provider.go                # Main provider implementation
│   ├── go.mod                     # Go module definition
│   ├── go.sum                     # Go module checksums
│   ├── discovery/                 # Service discovery logic
│   │   ├── service_discovery.go
│   │   └── service_loader.go
│   ├── scanner/                   # Resource scanning logic
│   │   ├── resource_lister.go
│   │   └── response_parser.go
│   ├── generator/                 # Schema generation
│   │   └── analyzer.go
│   └── classification/            # Operation classification
│       └── classifier.go
├── your-provider-shared/          # Shared components (optional)
│   ├── proto/                     # Protocol buffer definitions
│   │   ├── scanner.proto
│   │   └── scanner.pb.go
│   └── shared/                    # Shared utilities
│       └── plugin.go
└── build-your-provider.sh         # Build script
```

## Implementation Steps

### Step 1: Create the Plugin Module

```bash
mkdir -p plugins/azure-provider
cd plugins/azure-provider
go mod init github.com/jlgore/corkscrew/plugins/azure-provider
```

### Step 2: Define Dependencies

Add required dependencies to `go.mod`:

```go
module github.com/jlgore/corkscrew/plugins/azure-provider

go 1.24

require (
    github.com/Azure/azure-sdk-for-go/sdk/azcore v1.9.0
    github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.4.0
    github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources v1.2.0
    github.com/hashicorp/go-plugin v1.6.0
    github.com/jlgore/corkscrew v0.0.0
    google.golang.org/protobuf v1.36.6
)

replace github.com/jlgore/corkscrew => ../..
```

### Step 3: Implement the Main Entry Point

Create `main.go`:

```go
package main

import (
    "github.com/hashicorp/go-plugin"
    "github.com/jlgore/corkscrew/internal/shared"
)

func main() {
    // Create the provider implementation
    provider := NewAzureProvider()

    // Serve the plugin
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "provider": &shared.CloudProviderGRPCPlugin{Impl: provider},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

### Step 4: Implement the Provider Interface

Create `provider.go` with the main provider struct:

```go
package main

import (
    "context"
    "fmt"
    "sync"
    
    "github.com/Azure/azure-sdk-for-go/sdk/azcore"
    "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
    pb "github.com/jlgore/corkscrew/internal/proto"
)

type AzureProvider struct {
    mu           sync.RWMutex
    initialized  bool
    credential   azcore.TokenCredential
    subscription string
    
    // Discovery and scanning components
    serviceDiscovery *ServiceDiscovery
    resourceLister   *ResourceLister
    schemaGenerator  *SchemaGenerator
}

func NewAzureProvider() *AzureProvider {
    return &AzureProvider{
        serviceDiscovery: NewServiceDiscovery(),
        resourceLister:   NewResourceLister(),
        schemaGenerator:  NewSchemaGenerator(),
    }
}

func (p *AzureProvider) Initialize(ctx context.Context, req *pb.InitializeRequest) (*pb.InitializeResponse, error) {
    p.mu.Lock()
    defer p.mu.Unlock()

    // Initialize Azure credentials
    cred, err := azidentity.NewDefaultAzureCredential(nil)
    if err != nil {
        return &pb.InitializeResponse{
            Success: false,
            Error:   fmt.Sprintf("failed to create Azure credential: %v", err),
        }, nil
    }

    p.credential = cred
    p.subscription = req.Config["subscription_id"]
    p.initialized = true

    return &pb.InitializeResponse{
        Success: true,
        Version: "1.0.0",
    }, nil
}

func (p *AzureProvider) GetProviderInfo(ctx context.Context, req *pb.Empty) (*pb.ProviderInfoResponse, error) {
    return &pb.ProviderInfoResponse{
        Name:        "azure",
        Version:     "1.0.0",
        Description: "Microsoft Azure cloud provider plugin",
        Capabilities: map[string]string{
            "discovery": "true",
            "scanning":  "true",
            "streaming": "true",
        },
    }, nil
}

// Implement other interface methods...
```

## Azure Provider Example

Here's a complete example for an Azure provider:

### Service Discovery (`discovery/service_discovery.go`)

```go
package discovery

import (
    "context"
    "fmt"
    "strings"
    
    "github.com/Azure/azure-sdk-for-go/sdk/azcore"
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

type ServiceDiscovery struct {
    credential azcore.TokenCredential
    cache      map[string]*ServiceMetadata
}

type ServiceMetadata struct {
    Name         string   `json:"name"`
    Namespace    string   `json:"namespace"`
    APIVersion   string   `json:"api_version"`
    ResourceTypes []string `json:"resource_types"`
}

func NewServiceDiscovery() *ServiceDiscovery {
    return &ServiceDiscovery{
        cache: make(map[string]*ServiceMetadata),
    }
}

func (sd *ServiceDiscovery) DiscoverServices(ctx context.Context, subscriptionID string, cred azcore.TokenCredential) ([]string, error) {
    // Use Azure Resource Manager to discover available resource providers
    client, err := armresources.NewProvidersClient(subscriptionID, cred, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create providers client: %w", err)
    }

    var services []string
    pager := client.NewListPager(nil)
    
    for pager.More() {
        page, err := pager.NextPage(ctx)
        if err != nil {
            return nil, fmt.Errorf("failed to get providers: %w", err)
        }

        for _, provider := range page.Value {
            if provider.Namespace != nil {
                // Convert namespace to service name (e.g., Microsoft.Compute -> compute)
                serviceName := strings.ToLower(strings.TrimPrefix(*provider.Namespace, "Microsoft."))
                services = append(services, serviceName)
                
                // Cache metadata
                sd.cache[serviceName] = &ServiceMetadata{
                    Name:      serviceName,
                    Namespace: *provider.Namespace,
                }
            }
        }
    }

    return services, nil
}
```

### Resource Scanner (`scanner/resource_lister.go`)

```go
package scanner

import (
    "context"
    "fmt"
    "reflect"
    
    "github.com/Azure/azure-sdk-for-go/sdk/azcore"
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
)

type ResourceLister struct {
    credential     azcore.TokenCredential
    subscriptionID string
}

type AzureResourceRef struct {
    ID           string            `json:"id"`
    Name         string            `json:"name"`
    Type         string            `json:"type"`
    Service      string            `json:"service"`
    Location     string            `json:"location"`
    ResourceGroup string           `json:"resource_group"`
    Metadata     map[string]string `json:"metadata"`
}

func NewResourceLister() *ResourceLister {
    return &ResourceLister{}
}

func (rl *ResourceLister) ListResources(ctx context.Context, serviceName string, subscriptionID string, cred azcore.TokenCredential) ([]AzureResourceRef, error) {
    rl.credential = cred
    rl.subscriptionID = subscriptionID

    switch serviceName {
    case "compute":
        return rl.listComputeResources(ctx)
    case "storage":
        return rl.listStorageResources(ctx)
    default:
        return rl.listGenericResources(ctx, serviceName)
    }
}

func (rl *ResourceLister) listComputeResources(ctx context.Context) ([]AzureResourceRef, error) {
    client, err := armcompute.NewVirtualMachinesClient(rl.subscriptionID, rl.credential, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create compute client: %w", err)
    }

    var resources []AzureResourceRef
    pager := client.NewListAllPager(nil)
    
    for pager.More() {
        page, err := pager.NextPage(ctx)
        if err != nil {
            return nil, fmt.Errorf("failed to list VMs: %w", err)
        }

        for _, vm := range page.Value {
            if vm.ID != nil && vm.Name != nil {
                resource := AzureResourceRef{
                    ID:       *vm.ID,
                    Name:     *vm.Name,
                    Type:     "VirtualMachine",
                    Service:  "compute",
                    Location: *vm.Location,
                }
                
                // Extract resource group from ID
                if rg := extractResourceGroup(*vm.ID); rg != "" {
                    resource.ResourceGroup = rg
                }
                
                resources = append(resources, resource)
            }
        }
    }

    return resources, nil
}

func extractResourceGroup(resourceID string) string {
    // Parse Azure resource ID to extract resource group
    // Format: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/{type}/{name}
    parts := strings.Split(resourceID, "/")
    for i, part := range parts {
        if part == "resourceGroups" && i+1 < len(parts) {
            return parts[i+1]
        }
    }
    return ""
}
```

## GCP Provider Example

### Service Discovery for GCP

```go
package discovery

import (
    "context"
    "fmt"
    
    "google.golang.org/api/cloudresourcemanager/v1"
    "google.golang.org/api/option"
)

type GCPServiceDiscovery struct {
    projectID string
    cache     map[string]*ServiceMetadata
}

func NewGCPServiceDiscovery() *GCPServiceDiscovery {
    return &GCPServiceDiscovery{
        cache: make(map[string]*ServiceMetadata),
    }
}

func (sd *GCPServiceDiscovery) DiscoverServices(ctx context.Context, projectID string) ([]string, error) {
    // Use Cloud Resource Manager to discover enabled APIs
    service, err := cloudresourcemanager.NewService(ctx, option.WithScopes(cloudresourcemanager.CloudPlatformScope))
    if err != nil {
        return nil, fmt.Errorf("failed to create resource manager service: %w", err)
    }

    // Get enabled services
    req := service.Projects.GetIamPolicy(projectID, &cloudresourcemanager.GetIamPolicyRequest{})
    
    // This is a simplified example - in practice, you'd use the Service Usage API
    // to get enabled services and their resource types
    
    services := []string{
        "compute",
        "storage",
        "bigquery",
        "pubsub",
        "cloudsql",
        "kubernetes",
    }

    return services, nil
}
```

### GCP Resource Scanner

```go
package scanner

import (
    "context"
    "fmt"
    
    "google.golang.org/api/compute/v1"
    "google.golang.org/api/storage/v1"
)

type GCPResourceLister struct {
    projectID string
}

type GCPResourceRef struct {
    ID       string            `json:"id"`
    Name     string            `json:"name"`
    Type     string            `json:"type"`
    Service  string            `json:"service"`
    Zone     string            `json:"zone,omitempty"`
    Region   string            `json:"region,omitempty"`
    Metadata map[string]string `json:"metadata"`
}

func (rl *GCPResourceLister) ListComputeInstances(ctx context.Context) ([]GCPResourceRef, error) {
    service, err := compute.NewService(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to create compute service: %w", err)
    }

    var resources []GCPResourceRef
    
    // List instances across all zones
    req := service.Instances.AggregatedList(rl.projectID)
    if err := req.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
        for zone, instanceList := range page.Items {
            if instanceList.Instances != nil {
                for _, instance := range instanceList.Instances {
                    resource := GCPResourceRef{
                        ID:      fmt.Sprintf("%d", instance.Id),
                        Name:    instance.Name,
                        Type:    "Instance",
                        Service: "compute",
                        Zone:    extractZoneFromURL(zone),
                        Metadata: map[string]string{
                            "machineType": instance.MachineType,
                            "status":      instance.Status,
                        },
                    }
                    resources = append(resources, resource)
                }
            }
        }
        return nil
    }); err != nil {
        return nil, fmt.Errorf("failed to list instances: %w", err)
    }

    return resources, nil
}
```

## Kubernetes Provider Example

### Kubernetes Service Discovery

```go
package discovery

import (
    "context"
    "fmt"
    
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/discovery"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/clientcmd"
)

type K8sServiceDiscovery struct {
    clientset *kubernetes.Clientset
    discovery discovery.DiscoveryInterface
}

func NewK8sServiceDiscovery() *K8sServiceDiscovery {
    return &K8sServiceDiscovery{}
}

func (sd *K8sServiceDiscovery) Initialize(kubeconfig string) error {
    var config *rest.Config
    var err error
    
    if kubeconfig != "" {
        config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
    } else {
        config, err = rest.InClusterConfig()
    }
    
    if err != nil {
        return fmt.Errorf("failed to create kubernetes config: %w", err)
    }

    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return fmt.Errorf("failed to create kubernetes clientset: %w", err)
    }

    sd.clientset = clientset
    sd.discovery = clientset.Discovery()
    return nil
}

func (sd *K8sServiceDiscovery) DiscoverAPIResources(ctx context.Context) ([]string, error) {
    // Discover all API resources
    apiResourceLists, err := sd.discovery.ServerPreferredResources()
    if err != nil {
        return nil, fmt.Errorf("failed to discover API resources: %w", err)
    }

    var resources []string
    for _, apiResourceList := range apiResourceLists {
        for _, apiResource := range apiResourceList.APIResources {
            if apiResource.Namespaced {
                resources = append(resources, apiResource.Kind)
            }
        }
    }

    return resources, nil
}
```

### Kubernetes Resource Scanner

```go
package scanner

import (
    "context"
    "fmt"
    
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/client-go/dynamic"
    "k8s.io/client-go/kubernetes"
)

type K8sResourceLister struct {
    clientset     *kubernetes.Clientset
    dynamicClient dynamic.Interface
}

type K8sResourceRef struct {
    ID        string            `json:"id"`
    Name      string            `json:"name"`
    Type      string            `json:"type"`
    Namespace string            `json:"namespace"`
    Labels    map[string]string `json:"labels"`
    Metadata  map[string]string `json:"metadata"`
}

func (rl *K8sResourceLister) ListPods(ctx context.Context, namespace string) ([]K8sResourceRef, error) {
    pods, err := rl.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to list pods: %w", err)
    }

    var resources []K8sResourceRef
    for _, pod := range pods.Items {
        resource := K8sResourceRef{
            ID:        string(pod.UID),
            Name:      pod.Name,
            Type:      "Pod",
            Namespace: pod.Namespace,
            Labels:    pod.Labels,
            Metadata: map[string]string{
                "phase":   string(pod.Status.Phase),
                "node":    pod.Spec.NodeName,
                "created": pod.CreationTimestamp.String(),
            },
        }
        resources = append(resources, resource)
    }

    return resources, nil
}

func (rl *K8sResourceLister) ListGenericResources(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]K8sResourceRef, error) {
    var resources []K8sResourceRef
    
    list, err := rl.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, fmt.Errorf("failed to list %s: %w", gvr.Resource, err)
    }

    for _, item := range list.Items {
        resource := K8sResourceRef{
            ID:        string(item.GetUID()),
            Name:      item.GetName(),
            Type:      item.GetKind(),
            Namespace: item.GetNamespace(),
            Labels:    item.GetLabels(),
        }
        resources = append(resources, resource)
    }

    return resources, nil
}
```

## Testing and Validation

### Unit Tests

Create comprehensive unit tests for your provider:

```go
package main

import (
    "context"
    "testing"
    
    pb "github.com/jlgore/corkscrew/internal/proto"
)

func TestAzureProvider_Initialize(t *testing.T) {
    provider := NewAzureProvider()
    
    req := &pb.InitializeRequest{
        Provider: "azure",
        Config: map[string]string{
            "subscription_id": "test-subscription",
        },
    }
    
    resp, err := provider.Initialize(context.Background(), req)
    if err != nil {
        t.Fatalf("Initialize failed: %v", err)
    }
    
    if !resp.Success {
        t.Errorf("Expected success=true, got success=%v, error=%s", resp.Success, resp.Error)
    }
}

func TestAzureProvider_DiscoverServices(t *testing.T) {
    provider := NewAzureProvider()
    
    // Initialize first
    initReq := &pb.InitializeRequest{
        Provider: "azure",
        Config: map[string]string{
            "subscription_id": "test-subscription",
        },
    }
    provider.Initialize(context.Background(), initReq)
    
    req := &pb.DiscoverServicesRequest{
        ForceRefresh: true,
    }
    
    resp, err := provider.DiscoverServices(context.Background(), req)
    if err != nil {
        t.Fatalf("DiscoverServices failed: %v", err)
    }
    
    if len(resp.Services) == 0 {
        t.Error("Expected at least one service to be discovered")
    }
}
```

### Integration Tests

```go
func TestAzureProvider_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    // Test with real Azure credentials
    provider := NewAzureProvider()
    
    // Test full workflow: Initialize -> Discover -> List -> Describe
    // ... implementation
}
```

## Build and Deployment

### Build Script

Create `build-azure-provider.sh`:

```bash
#!/bin/bash

echo "🔧 Building Azure Provider Plugin..."

# Ensure build directory exists
mkdir -p plugins/build

# Build the Azure provider
cd plugins/azure-provider
echo "📦 Building azure-provider..."
go build -o ../build/azure-provider .

if [ $? -eq 0 ]; then
    echo "✅ Azure Provider built successfully!"
    echo "📁 Binary location: plugins/build/azure-provider"
    echo "📊 Size: $(du -h ../build/azure-provider | cut -f1)"
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew plugin install azure-provider"
```

### Plugin Registration

Add your plugin to `plugins/plugins.json`:

```json
{
  "azure_services": {
    "compute": {
      "capabilities": ["scan", "stream", "schemas", "relationships"],
      "plugin_name": "corkscrew-azure-compute",
      "resource_types": ["VirtualMachine", "Disk", "NetworkInterface"]
    },
    "storage": {
      "capabilities": ["scan", "stream", "schemas", "relationships"],
      "plugin_name": "corkscrew-azure-storage",
      "resource_types": ["StorageAccount", "Blob", "Queue"]
    }
  }
}
```

## Best Practices

### 1. Error Handling

- Implement comprehensive error handling with retries
- Use circuit breakers for external API calls
- Provide meaningful error messages to users

```go
func (p *AzureProvider) handleAPIError(err error, operation string) error {
    if isRetryableError(err) {
        return fmt.Errorf("retryable error in %s: %w", operation, err)
    }
    return fmt.Errorf("permanent error in %s: %w", operation, err)
}
```

### 2. Rate Limiting

- Respect cloud provider API rate limits
- Implement exponential backoff
- Use connection pooling where appropriate

```go
type RateLimiter struct {
    limiter *rate.Limiter
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
    return rl.limiter.Wait(ctx)
}
```

### 3. Caching

- Cache service discovery results
- Implement TTL-based cache invalidation
- Use memory-efficient data structures

```go
type CacheEntry struct {
    Data      interface{}
    ExpiresAt time.Time
}

func (c *Cache) Get(key string) (interface{}, bool) {
    entry, exists := c.data[key]
    if !exists || time.Now().After(entry.ExpiresAt) {
        return nil, false
    }
    return entry.Data, true
}
```

### 4. Configuration

- Support multiple authentication methods
- Provide sensible defaults
- Validate configuration early

```go
type ProviderConfig struct {
    SubscriptionID string `json:"subscription_id"`
    TenantID       string `json:"tenant_id"`
    ClientID       string `json:"client_id"`
    ClientSecret   string `json:"client_secret"`
    Region         string `json:"region"`
}

func (c *ProviderConfig) Validate() error {
    if c.SubscriptionID == "" {
        return fmt.Errorf("subscription_id is required")
    }
    return nil
}
```

### 5. Logging

- Use structured logging
- Include correlation IDs
- Log performance metrics

```go
import "log/slog"

func (p *AzureProvider) logOperation(operation string, duration time.Duration, err error) {
    logger := slog.With(
        "provider", "azure",
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

### 6. Resource Relationships

- Model relationships between resources
- Support dependency graphs
- Enable cross-service resource discovery

```go
type ResourceRelationship struct {
    SourceID     string `json:"source_id"`
    TargetID     string `json:"target_id"`
    Type         string `json:"type"` // "depends_on", "contains", "references"
    Properties   map[string]interface{} `json:"properties"`
}
```

### 7. Schema Generation

- Generate consistent database schemas
- Support schema evolution
- Include metadata and constraints

```go
func (sg *SchemaGenerator) GenerateTableSchema(resourceType string) *TableSchema {
    return &TableSchema{
        Name: fmt.Sprintf("azure_%s", strings.ToLower(resourceType)),
        Columns: []Column{
            {Name: "id", Type: "VARCHAR", PrimaryKey: true},
            {Name: "name", Type: "VARCHAR", Nullable: false},
            {Name: "resource_group", Type: "VARCHAR", Nullable: false},
            {Name: "location", Type: "VARCHAR", Nullable: false},
            {Name: "properties", Type: "JSON", Nullable: true},
            {Name: "discovered_at", Type: "TIMESTAMP", Nullable: false},
        },
        Indexes: []Index{
            {Name: "idx_resource_group", Columns: []string{"resource_group"}},
            {Name: "idx_location", Columns: []string{"location"}},
        },
    }
}
```

This guide provides a comprehensive foundation for developing cloud provider plugins for Corkscrew. Each provider will have unique characteristics, but following this structure and implementing the required interfaces will ensure compatibility with the Corkscrew ecosystem. 
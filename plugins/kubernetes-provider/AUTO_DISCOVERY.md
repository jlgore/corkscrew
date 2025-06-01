# Kubernetes Provider Auto-Discovery Implementation Guide

## Vision: Universal Kubernetes Resource Discovery

Unlike cloud providers (AWS, Azure, GCP) that have proprietary SDKs, Kubernetes offers a unique opportunity for comprehensive auto-discovery through its unified API structure and OpenAPI specifications. This provider will automatically discover ALL Kubernetes resources including core resources, built-in extensions, and Custom Resource Definitions (CRDs).

### Key Differentiators from Cloud Providers

1. **Unified API Structure**: All Kubernetes resources follow the same API patterns
2. **OpenAPI/Swagger Spec**: Complete resource definitions available at runtime
3. **Dynamic Discovery**: CRDs can be discovered and scanned without code changes
4. **Multi-Cluster Support**: Scan resources across multiple clusters simultaneously
5. **Namespace Awareness**: Built-in namespace isolation and cross-namespace relationships

## Implementation Architecture

### Phase 1: Kubernetes API Discovery

Unlike cloud providers that require SDK analysis, Kubernetes provides runtime discovery:

```go
// Discover all available API resources
discovery := kubernetes.NewDiscoveryClient(config)
apiResources, err := discovery.ServerPreferredResources()

// Discover CRDs dynamically
crdClient := apiextensions.NewApiextensionsV1Client(config)
crds, err := crdClient.CustomResourceDefinitions().List(context.TODO(), metav1.ListOptions{})
```

### Phase 2: Hybrid Discovery Approach

Following the pattern of cloud providers, the Kubernetes provider uses a multi-tier approach:

1. **Primary**: Kubernetes API discovery for real-time resource enumeration
2. **Enhanced**: OpenAPI spec parsing for schema details
3. **Optimized**: Informer cache for efficient resource watching
4. **Fallback**: Direct API calls for detailed resource information

### Phase 3: Auto-Generated Scanner Framework

The Kubernetes provider will generate scanners for:
- Core resources (pods, services, deployments, etc.)
- Extensions (ingresses, network policies, etc.)
- Storage resources (PVs, PVCs, storage classes)
- RBAC resources (roles, bindings, service accounts)
- Custom resources (any CRD installed in the cluster)

## Detailed Implementation Plan

### Provider Structure

```go
type KubernetesProvider struct {
    // Core components (following AWS/Azure/GCP pattern)
    discovery      *APIDiscovery
    scanner        *ResourceScanner
    schemaGen      *K8sSchemaGenerator
    clientFactory  *ClientFactory
    relationships  *RelationshipMapper
    
    // Kubernetes-specific components
    kubeConfig     *rest.Config
    clientset      kubernetes.Interface
    dynamicClient  dynamic.Interface
    informerCache  *InformerCache
    
    // Multi-cluster support
    clusters       map[string]*ClusterConnection
    
    // Performance components (same pattern as cloud providers)
    rateLimiter    *rate.Limiter
    maxConcurrency int
    cache          *MultiLevelCache
}
```

### Auto-Discovery Implementation

#### 1. API Resource Discovery

```go
type APIDiscovery struct {
    discoveryClient discovery.DiscoveryInterface
    openAPIClient   openapi.Client
}

func (d *APIDiscovery) DiscoverAllResources(ctx context.Context) ([]*ResourceDefinition, error) {
    var resources []*ResourceDefinition
    
    // 1. Discover all API groups and versions
    groups, err := d.discoveryClient.ServerGroups()
    if err != nil {
        return nil, err
    }
    
    // 2. For each group, discover resources
    for _, group := range groups.Groups {
        for _, version := range group.Versions {
            groupVersion := version.GroupVersion
            resourceList, err := d.discoveryClient.ServerResourcesForGroupVersion(groupVersion)
            if err != nil {
                continue // Some resources might not be accessible
            }
            
            for _, resource := range resourceList.APIResources {
                def := &ResourceDefinition{
                    Group:      group.Name,
                    Version:    version.Version,
                    Kind:       resource.Kind,
                    Name:       resource.Name,
                    Namespaced: resource.Namespaced,
                    Verbs:      resource.Verbs,
                }
                
                // 3. Get OpenAPI schema for detailed field information
                if schema, err := d.getOpenAPISchema(groupVersion, resource.Kind); err == nil {
                    def.Schema = schema
                }
                
                resources = append(resources, def)
            }
        }
    }
    
    // 4. Discover CRDs
    crdResources, err := d.discoverCRDs(ctx)
    if err == nil {
        resources = append(resources, crdResources...)
    }
    
    return resources, nil
}
```

#### 2. CRD Discovery

```go
func (d *APIDiscovery) discoverCRDs(ctx context.Context) ([]*ResourceDefinition, error) {
    // Use apiextensions client to list all CRDs
    crdClient := apiextensions.NewForConfigOrDie(d.kubeConfig)
    crdList, err := crdClient.CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
    if err != nil {
        return nil, err
    }
    
    var resources []*ResourceDefinition
    for _, crd := range crdList.Items {
        for _, version := range crd.Spec.Versions {
            def := &ResourceDefinition{
                Group:      crd.Spec.Group,
                Version:    version.Name,
                Kind:       crd.Spec.Names.Kind,
                Name:       crd.Spec.Names.Plural,
                Namespaced: crd.Spec.Scope == "Namespaced",
                CRD:        true,
                Schema:     version.Schema.OpenAPIV3Schema, // CRDs include their schema
            }
            resources = append(resources, def)
        }
    }
    
    return resources, nil
}
```

#### 3. Scanner Generation

```go
type ScannerGenerator struct {
    templates *template.Template
}

func (g *ScannerGenerator) GenerateScanner(resource *ResourceDefinition) (*GeneratedScanner, error) {
    // Generate scanner code that uses dynamic client for any resource type
    scanner := &GeneratedScanner{
        ResourceDef: resource,
    }
    
    // Template for generated scanner
    scannerTemplate := `
func (s *ResourceScanner) scan{{.Kind}}(ctx context.Context, namespace string) ([]*pb.Resource, error) {
    gvr := schema.GroupVersionResource{
        Group:    "{{.Group}}",
        Version:  "{{.Version}}",
        Resource: "{{.Name}}",
    }
    
    var list *unstructured.UnstructuredList
    var err error
    
    {{if .Namespaced}}
    if namespace != "" {
        list, err = s.dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
    } else {
        list, err = s.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
    }
    {{else}}
    list, err = s.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
    {{end}}
    
    if err != nil {
        return nil, fmt.Errorf("failed to list {{.Kind}}: %w", err)
    }
    
    return s.convertToResources(list, "{{.Kind}}")
}
`
    
    // Execute template
    var buf bytes.Buffer
    err := g.templates.Execute(&buf, resource)
    if err != nil {
        return nil, err
    }
    
    scanner.Code = buf.String()
    return scanner, nil
}
```

### Multi-Cluster Support

Following the cloud provider pattern of multi-region support:

```go
type ClusterConnection struct {
    Name          string
    Config        *rest.Config
    Clientset     kubernetes.Interface
    DynamicClient dynamic.Interface
    Healthy       bool
}

func (p *KubernetesProvider) ScanMultipleClusters(ctx context.Context, clusterNames []string) ([]*pb.Resource, error) {
    var allResources []*pb.Resource
    var mu sync.Mutex
    var wg sync.WaitGroup
    
    semaphore := make(chan struct{}, p.maxConcurrency)
    
    for _, clusterName := range clusterNames {
        cluster, exists := p.clusters[clusterName]
        if !exists || !cluster.Healthy {
            continue
        }
        
        wg.Add(1)
        go func(c *ClusterConnection) {
            defer wg.Done()
            semaphore <- struct{}{}
            defer func() { <-semaphore }()
            
            resources, err := p.scanCluster(ctx, c)
            if err != nil {
                log.Printf("Failed to scan cluster %s: %v", c.Name, err)
                return
            }
            
            mu.Lock()
            allResources = append(allResources, resources...)
            mu.Unlock()
        }(cluster)
    }
    
    wg.Wait()
    return allResources, nil
}
```

### Relationship Discovery

Kubernetes has rich relationship information that can be extracted:

```go
type RelationshipMapper struct {
    // Owner references (built-in Kubernetes feature)
    // Label selectors
    // Namespace relationships
    // Service endpoints
    // Volume mounts
    // Config/Secret references
}

func (r *RelationshipMapper) ExtractRelationships(resource *unstructured.Unstructured) []*Relationship {
    var relationships []*Relationship
    
    // 1. Owner References (parent-child relationships)
    ownerRefs, found, _ := unstructured.NestedSlice(resource.Object, "metadata", "ownerReferences")
    if found {
        for _, ref := range ownerRefs {
            refMap := ref.(map[string]interface{})
            rel := &Relationship{
                SourceKind: resource.GetKind(),
                SourceName: resource.GetName(),
                TargetKind: refMap["kind"].(string),
                TargetName: refMap["name"].(string),
                Type:       "owned-by",
            }
            relationships = append(relationships, rel)
        }
    }
    
    // 2. Service Selectors
    if resource.GetKind() == "Service" {
        selector, found, _ := unstructured.NestedStringMap(resource.Object, "spec", "selector")
        if found {
            // Find pods matching selector
            rel := &Relationship{
                SourceKind: "Service",
                SourceName: resource.GetName(),
                TargetKind: "Pod",
                Type:       "selects",
                Properties: map[string]string{"selector": fmt.Sprintf("%v", selector)},
            }
            relationships = append(relationships, rel)
        }
    }
    
    // 3. Volume references
    if resource.GetKind() == "Pod" {
        volumes, found, _ := unstructured.NestedSlice(resource.Object, "spec", "volumes")
        if found {
            for _, vol := range volumes {
                volMap := vol.(map[string]interface{})
                // Check for PVC references
                if pvcClaim, ok := volMap["persistentVolumeClaim"]; ok {
                    claimMap := pvcClaim.(map[string]interface{})
                    rel := &Relationship{
                        SourceKind: "Pod",
                        SourceName: resource.GetName(),
                        TargetKind: "PersistentVolumeClaim",
                        TargetName: claimMap["claimName"].(string),
                        Type:       "mounts",
                    }
                    relationships = append(relationships, rel)
                }
                // Check for ConfigMap references
                if configMap, ok := volMap["configMap"]; ok {
                    cmMap := configMap.(map[string]interface{})
                    rel := &Relationship{
                        SourceKind: "Pod",
                        SourceName: resource.GetName(),
                        TargetKind: "ConfigMap",
                        TargetName: cmMap["name"].(string),
                        Type:       "references",
                    }
                    relationships = append(relationships, rel)
                }
            }
        }
    }
    
    return relationships
}
```

### Schema Generation

Generate DuckDB schemas from Kubernetes resource definitions:

```go
func (g *K8sSchemaGenerator) GenerateSchema(resource *ResourceDefinition) string {
    tableName := fmt.Sprintf("k8s_%s_%s", 
        strings.ToLower(resource.Group), 
        strings.ToLower(resource.Name))
    
    schema := fmt.Sprintf(`
CREATE TABLE %s (
    -- Kubernetes standard metadata
    name VARCHAR PRIMARY KEY,
    namespace VARCHAR,
    uid VARCHAR UNIQUE,
    resource_version VARCHAR,
    generation BIGINT,
    creation_timestamp TIMESTAMP,
    deletion_timestamp TIMESTAMP,
    
    -- Cluster information
    cluster_name VARCHAR NOT NULL,
    api_version VARCHAR NOT NULL,
    kind VARCHAR NOT NULL,
    
    -- Common fields
    labels JSON,
    annotations JSON,
    owner_references JSON,
    finalizers JSON,
    
    -- Resource-specific fields
    spec JSON,
    status JSON,
    
    -- Scanning metadata
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_updated TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    -- Full resource data
    raw_data JSON,
    
    -- Indexes
    INDEX idx_%s_namespace (namespace),
    INDEX idx_%s_cluster (cluster_name),
    INDEX idx_%s_created (creation_timestamp),
    INDEX idx_%s_labels (labels)
);`, tableName, resource.Name, resource.Name, resource.Name, resource.Name)
    
    // Add specific indexes based on resource type
    switch resource.Kind {
    case "Pod":
        schema += fmt.Sprintf(`
CREATE INDEX idx_%s_node ON %s((spec->>'nodeName'));
CREATE INDEX idx_%s_phase ON %s((status->>'phase'));`, 
            resource.Name, tableName, resource.Name, tableName)
    case "Service":
        schema += fmt.Sprintf(`
CREATE INDEX idx_%s_type ON %s((spec->>'type'));
CREATE INDEX idx_%s_cluster_ip ON %s((spec->>'clusterIP'));`,
            resource.Name, tableName, resource.Name, tableName)
    }
    
    return schema
}
```

## Claude Prompts for Implementation

### Prompt 1: Kubernetes API Discovery Implementation
```
I need to implement a Kubernetes API discovery system for Corkscrew that automatically discovers all available resources including CRDs.

Requirements:
1. Use the Kubernetes discovery client to enumerate all API groups and resources
2. Parse OpenAPI specs to get detailed schema information
3. Dynamically discover CRDs without hardcoding
4. Support multiple cluster configurations
5. Cache discovery results with TTL

The implementation should follow the established Corkscrew patterns from AWS/Azure/GCP providers but adapted for Kubernetes' unified API structure.

Key differences from cloud providers:
- No SDK to analyze, use runtime API discovery instead
- All resources follow the same API patterns
- CRDs can be discovered at runtime
- OpenAPI schemas available for all resources

Please implement:
- APIDiscovery struct with methods to discover all resources
- CRD discovery that works with any custom resources
- Schema extraction from OpenAPI specs
- Multi-cluster support following the multi-region pattern from cloud providers
```

### Prompt 2: Dynamic Scanner Generator for Kubernetes
```
I need to implement a scanner generator for Kubernetes that creates scanning code for any resource type (including CRDs) without hardcoding.

Requirements:
1. Use the dynamic client to scan any resource type
2. Handle both namespaced and cluster-scoped resources
3. Support field selectors and label selectors for filtering
4. Implement pagination for large resource sets
5. Extract relationships from owner references, selectors, and references

The generator should create code that:
- Works with any GroupVersionResource
- Handles namespaced vs cluster-scoped resources
- Extracts standard metadata and custom fields
- Identifies relationships automatically
- Supports watch operations for real-time updates

Generate scanners that follow the same patterns as AWS/Azure/GCP but use Kubernetes' dynamic client for universal resource support.
```

### Prompt 3: Kubernetes Relationship Extraction
```
I need to implement comprehensive relationship extraction for Kubernetes resources.

Kubernetes has rich relationship information that should be extracted:
1. Owner References (built-in parent-child relationships)
2. Label Selectors (services to pods, deployments to replicasets)
3. Volume Mounts (pods to PVCs, ConfigMaps, Secrets)
4. Network Relationships (services to endpoints, ingresses to services)
5. RBAC Relationships (roles to subjects, bindings)
6. Custom Resource relationships (defined in CRDs)

The extractor should:
- Parse unstructured resources to find relationships
- Handle different relationship types with metadata
- Support bi-directional relationship mapping
- Extract relationships from status fields
- Work with any resource type including CRDs

Create a relationship extraction system that provides richer information than cloud providers due to Kubernetes' declarative nature.
```

### Prompt 4: Informer-Based Efficient Scanning
```
I need to implement an informer-based scanning system for Kubernetes that provides efficient resource discovery and real-time updates.

Requirements:
1. Use SharedInformerFactory for efficient caching
2. Implement watch operations for real-time resource updates
3. Support incremental updates instead of full rescans
4. Handle informer resync for consistency
5. Integrate with the existing scanner patterns from cloud providers

The implementation should:
- Create informers dynamically for discovered resource types
- Maintain a local cache that's updated in real-time
- Support both full scans and incremental updates
- Handle connection failures and reconnection
- Provide event streams for resource changes

This should follow the performance optimization patterns from cloud providers but leverage Kubernetes' native watch capabilities for better efficiency.
```

### Prompt 5: Helm and Operator Integration
```
I need to implement discovery for Helm releases and Operator-managed resources in the Kubernetes provider.

Requirements:
1. Discover Helm releases by finding ConfigMaps/Secrets with Helm metadata
2. Parse Helm release information to understand deployed resources
3. Identify Operator-managed resources through annotations and labels
4. Support common operators (OLM, Prometheus, Cert-Manager, etc.)
5. Extract additional metadata about managed resources

The integration should:
- Identify which resources belong to which Helm release
- Extract Helm chart version and values
- Recognize operator patterns and CRDs
- Map operator-managed resources to their operators
- Provide enhanced metadata for managed resources

This adds value beyond basic Kubernetes resources by understanding the higher-level deployment patterns.
```

## Testing Strategy

### Unit Tests
- Mock Kubernetes API for discovery testing
- Test scanner generation with various resource types
- Verify relationship extraction logic
- Test schema generation for different resources

### Integration Tests
- Test against kind/minikube clusters
- Verify CRD discovery with sample CRDs
- Test multi-cluster scenarios
- Validate informer synchronization

### End-to-End Tests
- Full discovery across multiple clusters
- Performance testing with large clusters
- Real-time update testing with informers
- Relationship graph validation

## Build Pipeline

```makefile
# Kubernetes provider build
build-kubernetes-plugin:
	@echo "🔧 Building Kubernetes Provider Plugin..."
	@cd plugins/kubernetes-provider && go build -o ../build/corkscrew-kubernetes .
	@echo "✅ Kubernetes provider built successfully!"

# Generate code from discovered resources
generate-k8s-scanners:
	@echo "🔄 Generating Kubernetes scanners..."
	@go run ./cmd/k8s-generator/main.go \
		--kubeconfig ~/.kube/config \
		--output ./generated/

# Test the provider
test-kubernetes-provider:
	@echo "🧪 Testing Kubernetes provider..."
	@cd plugins/kubernetes-provider && go test ./...
	@./plugins/build/corkscrew-kubernetes --test
```

## Usage Examples

```bash
# Basic scanning
corkscrew scan --provider kubernetes --namespace default
corkscrew scan --provider kubernetes --all-namespaces

# Multi-cluster scanning
corkscrew scan --provider kubernetes --clusters prod,staging,dev

# Resource type filtering
corkscrew scan --provider kubernetes --resource-types pods,services,deployments

# Label-based filtering
corkscrew scan --provider kubernetes --label-selector "app=frontend"

# Watch mode for real-time updates
corkscrew watch --provider kubernetes --namespace production

# CRD discovery
corkscrew discover --provider kubernetes --include-crds

# Helm release scanning
corkscrew scan --provider kubernetes --helm-releases
```

## Success Metrics

- 100% coverage of all Kubernetes resource types including CRDs
- Zero manual coding required for new CRDs
- Real-time resource updates through informers
- Complete relationship mapping
- Multi-cluster support with <1s switching time
- Sub-second query performance for common resources
- Automatic discovery of new API versions
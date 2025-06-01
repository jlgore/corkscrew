# Kubernetes Provider for Corkscrew

The Kubernetes provider enables Corkscrew to discover, scan, and analyze Kubernetes resources across single or multiple clusters. Unlike cloud providers (AWS, Azure, GCP) that require SDK analysis, Kubernetes offers comprehensive auto-discovery through its unified API structure.

## Features

### 🚀 Core Capabilities

- **Universal Resource Discovery**: Automatically discovers ALL Kubernetes resources including:
  - Core resources (pods, services, deployments)
  - Custom Resource Definitions (CRDs)
  - Helm releases and managed resources
  - Operator-managed resources

- **Multi-Cluster Support**: Scan resources across multiple clusters simultaneously
- **Real-Time Updates**: Use Kubernetes informers for efficient resource watching
- **Rich Relationship Mapping**: Extract relationships from:
  - Owner references
  - Label selectors
  - Volume mounts
  - Service endpoints
  - Network policies

### 🔄 Auto-Discovery Implementation

Unlike cloud providers that require SDK analysis, Kubernetes provides:

1. **Runtime Discovery**: All resources discoverable via API at runtime
2. **OpenAPI Schemas**: Complete resource definitions available
3. **Dynamic CRD Support**: New CRDs automatically discoverable
4. **Unified API Pattern**: All resources follow same patterns

## Quick Start

### Build the Plugin

```bash
cd plugins/kubernetes-provider
./build-kubernetes-plugin.sh
```

### Basic Usage

```bash
# Scan default namespace
corkscrew scan --provider kubernetes --namespace default

# Scan all namespaces
corkscrew scan --provider kubernetes --all-namespaces

# Scan specific resource types
corkscrew scan --provider kubernetes --resource-types pods,services,deployments

# Multi-cluster scanning
corkscrew scan --provider kubernetes --contexts prod,staging,dev
```

## Architecture

### Comparison with Cloud Providers

| Feature | AWS | Azure | GCP | Kubernetes |
|---------|-----|-------|-----|------------|
| Primary Discovery | Resource Explorer + SDK | Resource Graph + ARM | Asset Inventory + SDK | API Discovery + Informers |
| Auto-Discovery | SDK Analysis | SDK Analysis | Client Library Analysis | Runtime API Discovery |
| CRD/Custom Resources | N/A | N/A | N/A | Full Support |
| Real-Time Updates | Polling | Polling | Polling | Native Watch/Informers |
| Relationship Discovery | Limited | Limited | Limited | Rich Built-in |

### Key Components

1. **API Discovery** (`discovery.go`)
   - Discovers all available resources at runtime
   - No hardcoding required
   - Supports CRDs automatically

2. **Resource Scanner** (`scanner.go`)
   - Universal scanner using dynamic client
   - Works with any resource type
   - Efficient pagination and filtering

3. **Informer Cache** (`informer_cache.go`)
   - Real-time resource updates
   - Efficient caching layer
   - Reduced API server load

4. **Relationship Mapper** (`relationships.go`)
   - Extracts rich relationship data
   - Understands Kubernetes patterns
   - Bi-directional mapping

5. **Schema Generator** (`schema_generator.go`)
   - Generates DuckDB schemas dynamically
   - Resource-specific optimizations
   - Supports any resource type

## Advanced Features

### Helm Integration

```bash
# Discover all Helm releases
corkscrew scan --provider kubernetes --helm-releases

# Get resources managed by a specific release
corkscrew scan --provider kubernetes --helm-release my-app --namespace production
```

### Watch Mode

```bash
# Watch for real-time changes
corkscrew watch --provider kubernetes --namespace production

# Watch specific resource types
corkscrew watch --provider kubernetes --resource-types pods,deployments
```

### CRD Discovery

```bash
# Discover and scan all CRDs
corkscrew discover --provider kubernetes --include-crds

# Scan specific CRD resources
corkscrew scan --provider kubernetes --crd-group example.com
```

### Label-Based Filtering

```bash
# Scan resources with specific labels
corkscrew scan --provider kubernetes --label-selector "app=frontend,env=prod"

# Exclude system namespaces
corkscrew scan --provider kubernetes --exclude-system-namespaces
```

## Configuration

### Environment Variables

- `KUBECONFIG`: Path to kubeconfig file (default: `~/.kube/config`)
- `KUBERNETES_NAMESPACE`: Default namespace to scan
- `KUBERNETES_CONTEXTS`: Comma-separated list of contexts for multi-cluster

### Plugin Configuration

```yaml
provider: kubernetes
config:
  kubeconfig_path: ~/.kube/config
  contexts:
    - prod-cluster
    - staging-cluster
  all_namespaces: true
  exclude_system_namespaces: true
  watch_mode: false
  label_selectors:
    environment: production
    managed-by: corkscrew
```

## Schema Design

The Kubernetes provider generates optimized DuckDB schemas:

```sql
-- Example: Pod table
CREATE TABLE k8s_core_pods (
    uid VARCHAR PRIMARY KEY,
    name VARCHAR NOT NULL,
    namespace VARCHAR,
    cluster_name VARCHAR NOT NULL,
    
    -- Pod-specific fields
    node_name VARCHAR,
    phase VARCHAR,
    pod_ip VARCHAR,
    host_ip VARCHAR,
    
    -- Standard Kubernetes fields
    api_version VARCHAR NOT NULL,
    kind VARCHAR NOT NULL,
    labels JSON,
    annotations JSON,
    owner_references JSON,
    
    -- Resource data
    spec JSON,
    status JSON,
    raw_data JSON,
    
    -- Metadata
    creation_timestamp TIMESTAMP,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Development

### Running Tests

```bash
# Unit tests
go test ./...

# Integration tests (requires cluster)
go test ./... -tags=integration

# Test specific functionality
./corkscrew-kubernetes --test
./corkscrew-kubernetes --demo-discovery
./corkscrew-kubernetes --test-multi-cluster
```

### Extending the Provider

1. **Adding New Resource Types**: Automatically discovered, no code changes needed!

2. **Custom Relationship Extraction**:
   ```go
   // Add to relationships.go
   func (r *RelationshipMapper) extractCustomRelationships(resource *unstructured.Unstructured) []*pb.Relationship {
       // Your custom logic
   }
   ```

3. **Enhanced Schema Generation**:
   ```go
   // Add to schema_generator.go
   case "YourCustomResource":
       columns = append(columns, customColumns...)
   ```

## Performance Optimization

### Informer-Based Scanning

The provider uses Kubernetes informers for optimal performance:

- **Initial Sync**: Full resource list
- **Incremental Updates**: Only changes transmitted
- **Local Cache**: Reduced API server queries
- **Automatic Reconnection**: Handles network failures

### Rate Limiting

Configurable rate limiting protects the API server:

```go
rateLimiter: rate.NewLimiter(rate.Limit(100), 200) // 100 req/s, burst 200
```

### Concurrent Scanning

Multi-cluster and multi-namespace scanning runs concurrently:

```go
maxConcurrency: 20 // Higher than cloud providers due to local API
```

## Troubleshooting

### Common Issues

1. **Permission Errors**
   ```bash
   # Ensure proper RBAC permissions
   kubectl auth can-i list pods --all-namespaces
   ```

2. **CRD Discovery Failures**
   ```bash
   # Check CRD permissions
   kubectl get crd
   ```

3. **Multi-Cluster Connection Issues**
   ```bash
   # Verify contexts
   kubectl config get-contexts
   ```

### Debug Mode

```bash
# Enable debug logging
CORKSCREW_DEBUG=true corkscrew scan --provider kubernetes

# Test connection
corkscrew test --provider kubernetes
```

## Comparison with Cloud Providers

### Advantages over Cloud Providers

1. **No SDK Analysis Required**: Resources discovered at runtime
2. **Universal Resource Support**: Any CRD works immediately
3. **Real-Time Updates**: Native watch capabilities
4. **Rich Relationships**: Built into Kubernetes API
5. **Consistent API**: All resources follow same patterns

### Similar Patterns

1. **Hybrid Discovery**: Primary (API) + Fallback (Direct queries)
2. **Multi-Region/Cluster**: Similar to cloud provider regions
3. **Caching Strategy**: Multi-level cache for performance
4. **Schema Generation**: Dynamic based on resource types

## Future Enhancements

1. **Policy Compliance Scanning**: Integration with OPA/Gatekeeper
2. **Cost Analysis**: Resource usage and cost estimation
3. **Security Scanning**: Integration with Falco/Trivy
4. **GitOps Integration**: ArgoCD/Flux resource tracking
5. **Service Mesh Support**: Istio/Linkerd resource discovery

## Contributing

The Kubernetes provider follows the same patterns as AWS, Azure, and GCP providers. When adding features:

1. Maintain compatibility with the CloudProvider interface
2. Follow the established patterns for discovery and scanning
3. Ensure multi-cluster support is maintained
4. Add appropriate tests
5. Update documentation

## License

Same as Corkscrew project.
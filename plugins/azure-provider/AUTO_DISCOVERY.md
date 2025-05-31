# Azure Provider Auto-Discovery Implementation Guide

## Vision: Automatic Azure SDK Analysis & Scanner Generation

### Current State
- Manual scanner implementations
- Limited to manually coded resource types
- Inconsistent patterns across services
- Missing many Azure resources

### Desired State
- Automatic discovery of ALL Azure resources by analyzing SDK
- Auto-generated scanners from SDK patterns
- Leverage Resource Graph as primary, SDK as fallback
- Complete resource coverage with zero manual coding

## Implementation Architecture

### Phase 1: Azure SDK Analysis
Analyze Azure SDK for Go to discover resource types:
- Parse SDK source from `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/*`
- Identify List*/Get* operations in each service
- Extract ARM resource types and API versions
- Map SDK operations to Resource Graph queries

### Phase 2: Dual-Mode Scanner Generation
Generate scanners that use BOTH Resource Graph and SDK:
- Primary: Resource Graph queries for bulk discovery
- Fallback: SDK-based scanning when Resource Graph unavailable
- Automatic query optimization
- Relationship extraction from ARM templates

### Phase 3: ARM Schema Integration
Leverage ARM schemas for DuckDB table generation:
- Parse ARM resource schemas
- Generate type-safe column definitions
- Handle resource-specific properties
- Support policy and compliance fields

## Claude Prompts for Implementation

### Prompt 1: Azure SDK Analyzer Implementation
```
I need to implement an Azure SDK analyzer for Corkscrew that discovers resources by parsing the Azure SDK for Go. Requirements:

1. Parse Azure SDK packages from github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/*
2. Identify resource operations:
   - NewListPager/NewListByResourceGroupPager (list operations)
   - Get methods (individual resource fetch)
   - BeginCreate* (for understanding resource properties)
3. Extract ARM metadata:
   - Resource type (Microsoft.Compute/virtualMachines)
   - API version
   - Property schemas
   - Required vs optional fields
4. Generate Resource Graph queries for each resource type

Here's what the analyzer should discover:
- Service name and namespace
- Resource types with full ARM paths
- List/Get operations
- Pagination patterns
- Property mappings

Example SDK pattern to detect:
```go
// From compute SDK
func (client *VirtualMachinesClient) NewListPager(options *VirtualMachinesClientListOptions) *runtime.Pager[VirtualMachinesClientListResponse]
func (client *VirtualMachinesClient) Get(ctx context.Context, resourceGroupName string, vmName string) (VirtualMachinesClientGetResponse, error)
```

The analyzer should output structured data for both Resource Graph query generation AND SDK-based scanner generation.

Please implement:
- Azure SDK parser using go/ast
- ARM type extraction
- Resource Graph query builder
- Relationship detection from resource properties
```

### Prompt 2: Resource Graph Query Generator
```
I need to implement a Resource Graph query generator that creates optimal KQL queries based on discovered Azure resources.

Input: Analyzed Azure service info with resource types
Output: Optimized Resource Graph queries

Requirements:
1. Generate KQL queries for each resource type:
   ```kql
   Resources
   | where type == "microsoft.compute/virtualmachines"
   | project id, name, type, location, resourceGroup, subscriptionId, 
            properties.hardwareProfile.vmSize, 
            properties.storageProfile.osDisk.diskSizeGB,
            properties.networkProfile.networkInterfaces,
            tags
   ```

2. Create relationship queries:
   ```kql
   // Find VMs and their disks
   Resources
   | where type == "microsoft.compute/virtualmachines"
   | extend diskId = properties.storageProfile.osDisk.managedDisk.id
   | join kind=leftouter (
       Resources 
       | where type == "microsoft.compute/disks"
       | project diskId=id, diskName=name, diskSize=properties.diskSizeGB
   ) on diskId
   ```

3. Generate batched queries for large result sets
4. Create specialized queries for common scenarios:
   - Resources by tag
   - Resources by location
   - Cross-subscription queries
   - Change tracking queries

The generator should:
- Optimize queries for performance
- Handle pagination
- Extract all relevant properties
- Support incremental updates
- Generate both discovery and relationship queries
```

### Prompt 3: Hybrid Scanner Generator
```
I need to implement a hybrid scanner generator for Azure that creates code using BOTH Resource Graph and SDK fallback.

Generate scanner code that:
1. Tries Resource Graph first
2. Falls back to SDK if Resource Graph fails
3. Handles both patterns seamlessly

Template structure:
```go
type {{.ServiceName}}Scanner struct {
    clientFactory    *ClientFactory
    resourceGraph    *ResourceGraphClient
    {{.sdkClient}}    *{{.ServiceName}}.{{.ClientType}}
}

func (s *{{.ServiceName}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
    // Try Resource Graph first
    if s.resourceGraph != nil {
        query := `{{.ResourceGraphQuery}}`
        resources, err := s.resourceGraph.Query(ctx, query)
        if err == nil {
            return s.convertGraphResults(resources), nil
        }
        log.Printf("Resource Graph failed, falling back to SDK: %v", err)
    }
    
    // Fall back to SDK scanning
    var resources []*pb.ResourceRef
    
    {{if .SupportsListAll}}
    // List all resources across subscriptions
    pager := s.{{.sdkClient}}.NewListPager(nil)
    {{else}}
    // List by resource groups
    resourceGroups, err := s.listResourceGroups(ctx)
    if err != nil {
        return nil, err
    }
    
    for _, rg := range resourceGroups {
        pager := s.{{.sdkClient}}.NewListByResourceGroupPager(rg.Name, nil)
    {{end}}
        
        for pager.More() {
            page, err := pager.NextPage(ctx)
            if err != nil {
                return nil, fmt.Errorf("failed to list {{.ResourceType}}: %w", err)
            }
            
            for _, item := range page.Value {
                ref := s.convert{{.ResourceType}}ToRef(item)
                resources = append(resources, ref)
            }
        }
    {{if not .SupportsListAll}}
    }
    {{end}}
    
    return resources, nil
}
```

Generate complete scanner implementations that handle:
- Resource Graph optimization
- SDK fallback patterns
- Error handling and retries
- Relationship extraction
- Metadata enrichment
```

### Prompt 4: ARM Schema to DuckDB Generator
```
I need to implement an ARM schema parser that generates DuckDB schemas from Azure Resource Manager definitions.

Requirements:
1. Parse ARM schemas from:
   - SDK response types
   - ARM template schemas
   - Resource Graph property paths

2. Generate optimized DuckDB schemas:
   ```sql
   CREATE TABLE azure_compute_virtualmachines (
       -- Standard ARM fields
       id VARCHAR PRIMARY KEY,
       name VARCHAR NOT NULL,
       type VARCHAR NOT NULL,
       location VARCHAR NOT NULL,
       resource_group VARCHAR NOT NULL,
       subscription_id VARCHAR NOT NULL,
       
       -- Resource-specific fields
       vm_size VARCHAR,
       os_type VARCHAR,
       os_disk_size_gb INTEGER,
       data_disk_count INTEGER,
       
       -- Complex fields as JSON
       network_interfaces JSON,
       extensions JSON,
       
       -- Metadata
       tags JSON,
       created_time TIMESTAMP,
       modified_time TIMESTAMP,
       discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
       
       -- Full resource data
       properties JSON,
       raw_data JSON
   );
   
   -- Indexes for common queries
   CREATE INDEX idx_vm_resource_group ON azure_compute_virtualmachines(resource_group);
   CREATE INDEX idx_vm_location ON azure_compute_virtualmachines(location);
   CREATE INDEX idx_vm_size ON azure_compute_virtualmachines(vm_size);
   ```

3. Handle Azure-specific patterns:
   - Managed identities
   - System vs user assigned
   - Resource dependencies
   - SKU information
   - Availability zones

4. Generate views for common queries:
   ```sql
   CREATE VIEW azure_compute_vm_summary AS
   SELECT 
       subscription_id,
       resource_group,
       location,
       vm_size,
       COUNT(*) as vm_count,
       SUM(os_disk_size_gb) as total_disk_gb
   FROM azure_compute_virtualmachines
   GROUP BY subscription_id, resource_group, location, vm_size;
   ```

The generator should parse ARM types and create optimal schemas for analytics.
```

### Prompt 5: Build Pipeline Integration
```
I need to create a build pipeline that integrates all Azure auto-discovery components:

1. Makefile targets:
```makefile
# Download/update Azure SDK
update-azure-sdk:
    go get -u github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/...
    
# Analyze Azure SDK
analyze-azure:
    go run ./cmd/analyze-azure-sdk \
        --output ./generated/azure-resources.json \
        --services compute,storage,network,keyvault

# Generate scanners
generate-azure-scanners:
    go run ./cmd/generate-scanners \
        --input ./generated/azure-resources.json \
        --output ./plugins/azure-provider/generated/ \
        --template ./templates/azure-scanner.tmpl

# Generate schemas
generate-azure-schemas:
    go run ./cmd/generate-schemas \
        --input ./generated/azure-resources.json \
        --output ./schemas/azure/ \
        --format duckdb

# Build plugin with generated code
build-azure-plugin: analyze-azure generate-azure-scanners generate-azure-schemas
    cd plugins/azure-provider && go build -o ../build/azure-provider
```

2. Runtime loader that:
   - Dynamically loads generated scanners
   - Configures Resource Graph client
   - Sets up SDK clients as needed
   - Manages authentication flow

3. Hot reload support:
   - Detect SDK updates
   - Regenerate only changed services
   - Load new scanners without restart

Please implement the complete build pipeline with proper dependency management.
```

## Testing Strategy

### Unit Tests
- Mock Azure SDK for analyzer testing
- Verify Resource Graph query generation
- Test scanner code compilation
- Validate schema generation

### Integration Tests
- Test against Azure SDK
- Verify Resource Graph queries with Azure Resource Graph Explorer
- Test SDK fallback scenarios
- Validate against real Azure subscriptions

### Performance Tests
- Resource Graph query performance
- SDK pagination handling
- Concurrent scanning limits
- DuckDB insertion performance

## Success Metrics
- 100% coverage of Azure resource types
- Optimal Resource Graph queries for all resources
- Seamless SDK fallback
- Zero manual updates for new Azure services
- Sub-second query performance for common resources
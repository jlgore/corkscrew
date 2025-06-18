# GCP Provider Migration Guide: Standard to Enhanced

## Overview

This guide helps you migrate from the standard GCP provider to the enhanced version with feature parity to Azure provider.

## Key Improvements

1. **Dynamic Discovery**: No more hardcoded service mappings
2. **Database Persistence**: All resources stored in DuckDB
3. **Resource Graph**: Query resources and relationships efficiently
4. **Auto-generated Schemas**: Schemas generated from discovered resources
5. **Enhanced Performance**: Better caching and batch operations

## Migration Steps

### Step 1: Update Import

Replace the standard provider with the enhanced version:

```go
// OLD
provider := NewGCPProvider()

// NEW - Use enhanced provider
provider := NewGCPProvider() // Same interface, enhanced implementation
```

### Step 2: Update Initialization

The enhanced provider supports additional configuration:

```go
config := map[string]string{
    "project_ids": "project1,project2",
    "database_path": "/path/to/gcp_resources.db", // NEW: Database path
    "enable_resource_graph": "true",               // NEW: Enable resource graph
}

resp, err := provider.Initialize(ctx, &pb.InitializeRequest{
    Config: config,
})
```

### Step 3: Use Dynamic Discovery

Service discovery now happens dynamically:

```go
// OLD - Limited to hardcoded services
services, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{})

// NEW - Discovers ALL available services dynamically
services, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
    ForceRefresh: true, // Force cache refresh for latest services
})
```

### Step 4: Query Resources with Resource Graph

Use the resource graph for efficient querying:

```go
// List all compute instances across projects
resources, err := provider.ListResources(ctx, &pb.ListResourcesRequest{
    Service: "compute",
    // Automatically uses Resource Graph if available
})

// Resources are automatically persisted to database
```

### Step 5: Access Analytics

With database persistence, you can run analytics queries:

```sql
-- Query the database directly
SELECT 
    service,
    type,
    COUNT(*) as count,
    COUNT(DISTINCT project_id) as projects
FROM gcp_resources
GROUP BY service, type
ORDER BY count DESC;

-- View resource relationships
SELECT * FROM gcp_resource_relationships_graph;

-- Security analysis
SELECT * FROM gcp_storage_bucket_security;
```

## Configuration Options

### Enhanced Provider Configuration

```yaml
gcp:
  # Scope configuration (choose one)
  scope: "projects"  # or "organizations" or "folders"
  org_id: "123456789"
  folder_id: "987654321"
  project_ids: ["project1", "project2"]
  
  # Enhanced features
  database:
    enabled: true
    path: "./gcp_resources.db"
    
  resource_graph:
    enabled: true
    cache_ttl: "5m"
    
  discovery:
    method: "dynamic"  # Uses Asset Inventory
    cache_enabled: true
    
  performance:
    max_concurrency: 20
    rate_limit: 100  # requests per second
```

## Breaking Changes

### 1. Service Names

Services are now discovered dynamically. Some service names might differ:

- OLD: Hardcoded list of ~20 services
- NEW: All available GCP services (100+)

### 2. Resource Types

Resource types are discovered from Asset Inventory:

```go
// OLD - Hardcoded resource types
types := []string{"Instance", "Disk", "Network"}

// NEW - Dynamic discovery
types, err := provider.GetResourceTypesForService(ctx, "compute")
```

### 3. Schema Generation

Schemas are now generated dynamically:

```go
// OLD - Static schemas
schemas := provider.GetSchemas(ctx, &pb.GetSchemasRequest{
    Services: []string{"compute"},
})

// NEW - Dynamic schemas based on discovered resources
schemas := provider.GetSchemas(ctx, &pb.GetSchemasRequest{
    Services: []string{"compute"},
    // Schemas generated from actual resource structures
})
```

## Performance Considerations

1. **Initial Discovery**: First run may take longer due to comprehensive discovery
2. **Caching**: Subsequent runs use cached data (15-minute TTL)
3. **Database**: Enables historical queries but requires disk space
4. **Memory**: Resource graph may use more memory for large environments

## Rollback Plan

If you need to rollback:

1. Keep the original `gcp_provider.go` as backup
2. The enhanced provider maintains the same interface
3. Simply swap the implementation files

## Testing Migration

```bash
# Test discovery
./corkscrew discover --provider gcp

# Test scanning with database
./corkscrew scan --provider gcp --service compute

# Verify database
sqlite3 gcp_resources.db "SELECT COUNT(*) FROM gcp_resources;"

# Test resource graph
./corkscrew query --provider gcp --resource-graph "compute instances in us-central1"
```

## Troubleshooting

### Issue: Asset Inventory Access Denied

**Solution**: Ensure the service account has the required role:
```bash
gcloud projects add-iam-policy-binding PROJECT_ID \
  --member="serviceAccount:SA_EMAIL" \
  --role="roles/cloudasset.viewer"
```

### Issue: Database Initialization Failed

**Solution**: Check write permissions and disk space:
```bash
# Check permissions
ls -la /path/to/db/directory

# Check disk space
df -h /path/to/db/directory
```

### Issue: Discovery Returns No Services

**Solution**: Force refresh and check logs:
```go
services, err := provider.DiscoverServices(ctx, &pb.DiscoverServicesRequest{
    ForceRefresh: true,
})
```

## Benefits After Migration

1. **Zero Maintenance**: New GCP services automatically discovered
2. **Better Analytics**: Query historical data with SQL
3. **Relationship Mapping**: Understand resource dependencies
4. **Performance**: Batch operations and caching
5. **Feature Parity**: Same capabilities as Azure provider

## Support

For issues or questions:
1. Check logs for detailed error messages
2. Verify Asset Inventory API is enabled
3. Ensure proper IAM permissions
4. Review the [troubleshooting guide](./TROUBLESHOOTING.md)
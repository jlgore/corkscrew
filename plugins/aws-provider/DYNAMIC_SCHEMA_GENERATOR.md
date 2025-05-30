# Dynamic Schema Generator for AWS Resources

This implementation provides a sophisticated schema generator that creates DuckDB schemas from AWS SDK response types using Go reflection and type analysis.

## Features

### Core Functionality
- **Automatic Type Mapping**: Maps Go types to appropriate DuckDB column types
- **Nullable Field Detection**: Handles pointer types as nullable fields
- **Primary Key Detection**: Automatically identifies primary key fields
- **Index Generation**: Creates appropriate indexes based on field types and usage patterns
- **JSON Handling**: Stores complex types as JSON with optional indexing
- **View Generation**: Creates common views for recent data and summaries

### Type Mappings

| Go Type | DuckDB Type | Nullable Support | Index Type |
|---------|-------------|------------------|------------|
| `string` | `VARCHAR` | ✓ | btree |
| `int`, `int32`, `int64` | `INTEGER`/`BIGINT` | ✓ | btree |
| `float32`, `float64` | `REAL`/`DOUBLE` | ✓ | btree |
| `bool` | `BOOLEAN` | ✓ | none |
| `time.Time` | `TIMESTAMP` | ✓ | btree |
| `[]T`, `map[K]V` | `JSON` | ✓ | conditional |
| `struct` | `JSON` | ✓ | conditional |

## Usage Examples

### Basic Schema Generation

```go
// Create a schema generator
generator := NewDynamicSchemaGenerator()

// Generate schema for EC2 instances
schema, err := generator.GenerateSchemaFromStruct(
    reflect.TypeOf(EC2Instance{}),
    "aws_ec2_instances",
    "ec2",
    "AWS::EC2::Instance",
)
```

### Batch Generation

```go
resourceTypes := []struct {
    Type         reflect.Type
    TableName    string
    Service      string
    ResourceType string
}{
    {reflect.TypeOf(EC2Instance{}), "aws_ec2_instances", "ec2", "AWS::EC2::Instance"},
    {reflect.TypeOf(S3Bucket{}), "aws_s3_buckets", "s3", "AWS::S3::Bucket"},
    {reflect.TypeOf(LambdaFunction{}), "aws_lambda_functions", "lambda", "AWS::Lambda::Function"},
}

var schemas []*pb.Schema
for _, rt := range resourceTypes {
    schema, err := generator.GenerateSchemaFromStruct(rt.Type, rt.TableName, rt.Service, rt.ResourceType)
    if err != nil {
        log.Printf("Error: %v", err)
        continue
    }
    schemas = append(schemas, schema)
}
```

## Generated Schema Structure

### Table Structure
Each generated table includes:

#### Analyzed Fields
- Fields from the Go struct mapped to appropriate DuckDB types
- Nullable fields for pointer types
- Primary keys automatically detected

#### Standard Metadata Fields
```sql
discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
raw_data JSON
```

#### Indexes
- Primary key constraints
- B-tree indexes for frequently queried fields
- Conditional JSON indexes for complex fields
- Standard metadata indexes (discovered_at, last_seen)

### Example Generated Schema

For an `EC2Instance` struct:

```sql
CREATE TABLE IF NOT EXISTS aws_ec2_instances (
    instance_id VARCHAR PRIMARY KEY,
    instance_type VARCHAR,
    launch_time TIMESTAMP,
    state JSON,
    tags JSON,
    vpc_id VARCHAR,
    subnet_id VARCHAR,
    private_ip_address VARCHAR,
    public_ip_address VARCHAR,
    security_groups JSON,
    key_name VARCHAR,
    platform VARCHAR,
    architecture VARCHAR,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
    raw_data JSON
);

CREATE INDEX IF NOT EXISTS idx_ec2_instances_instance_type ON aws_ec2_instances(instance_type);
CREATE INDEX IF NOT EXISTS idx_ec2_instances_launch_time ON aws_ec2_instances(launch_time);
CREATE INDEX IF NOT EXISTS idx_ec2_instances_vpc_id ON aws_ec2_instances(vpc_id);
-- ... more indexes

CREATE OR REPLACE VIEW aws_ec2_instances_recent AS
SELECT *
FROM aws_ec2_instances
WHERE discovered_at >= CURRENT_TIMESTAMP - INTERVAL '24 hours'
ORDER BY discovered_at DESC;

CREATE OR REPLACE VIEW aws_ec2_instances_summary AS
SELECT 
    COUNT(*) as total_resources,
    COUNT(DISTINCT discovered_at::DATE) as discovery_days,
    MIN(discovered_at) as first_discovered,
    MAX(last_seen) as last_updated
FROM aws_ec2_instances;
```

## Integration with AWS Provider

The dynamic schema generator is integrated with the AWS provider through:

### Provider Enhancement
```go
type DynamicAWSProvider struct {
    // ... other fields
    dynamicSchemaGenerator *DynamicSchemaGenerator
    standardSchemaGenerator *SchemaGenerator
}
```

### Resource Type Mapping
```go
func (p *DynamicAWSProvider) getGoTypeForResource(serviceName, resourceTypeName string) reflect.Type {
    knownTypes := map[string]reflect.Type{
        "ec2.Instance":     reflect.TypeOf(EC2Instance{}),
        "s3.Bucket":        reflect.TypeOf(S3Bucket{}),
        "lambda.Function":  reflect.TypeOf(LambdaFunction{}),
    }
    // ... mapping logic
}
```

### Schema Generation Flow
1. Check if Go struct type exists for resource
2. If available, use dynamic generator for detailed analysis
3. Fallback to standard generator for unknown types
4. Cache results for performance

## Field Analysis Details

### Primary Key Detection
Automatically detects primary keys based on:
- Common field names (`id`, `instance_id`, `resource_id`, `arn`, etc.)
- Struct tags (`db:"primary_key"`)
- Field naming patterns

### Index Strategy
- **B-tree indexes**: For scalar types frequently used in WHERE clauses
- **JSON indexes**: For complex fields that benefit from JSON operations
- **Conditional indexing**: Based on field name patterns (tags, metadata, etc.)

### Nullable Fields
- Pointer types (`*string`, `*int`, etc.) → nullable columns
- Non-pointer types → NOT NULL columns
- Primary keys → always NOT NULL

## Migration Support

### Migration Script Generation
```go
migrationScript := generator.GenerateMigrationScript(oldSchema, newSchema)
```

Generates:
- Transaction-wrapped migration
- Backup recommendations
- Testing guidelines
- Placeholder for specific migration steps

### Schema Evolution
- Handles field additions/removals
- Type changes with appropriate warnings
- Index modifications
- View updates

## Performance Considerations

### Caching
- Schema generation results are cached by resource type
- Type mapping cache for reflection operations
- Prevents redundant analysis of same structs

### Optimization
- Lazy loading of type mappings
- Concurrent schema generation for multiple resources
- Efficient reflection usage

### Memory Usage
- Cached schemas include only essential metadata
- Reflection types cached separately
- Configurable cache TTL and size limits

## Configuration

### Environment Variables
```bash
# Enable/disable dynamic schema generation
CORKSCREW_ENABLE_DYNAMIC_SCHEMAS=true

# Schema cache configuration
CORKSCREW_SCHEMA_CACHE_SIZE=1000
CORKSCREW_SCHEMA_CACHE_TTL=3600

# JSON index configuration
CORKSCREW_ENABLE_JSON_INDEXES=true
```

### Runtime Configuration
```go
generator := NewDynamicSchemaGenerator()

// Custom type mappings
generator.AddTypeMapping(reflect.TypeOf(CustomType{}), TypeMapping{
    DuckDBType: "VARCHAR",
    Nullable:   true,
    IndexType:  "btree",
})

// Configure indexing strategy
generator.SetIndexingStrategy(func(fieldName string, fieldType reflect.Type) bool {
    // Custom indexing logic
    return strings.Contains(fieldName, "searchable")
})
```

## Testing

### Test Suite
Run the comprehensive test suite:
```go
RunAllTests()
```

Tests include:
- Schema generation for various struct types
- Type mapping accuracy
- Index generation
- View creation
- Provider integration
- Performance benchmarks

### Manual Testing
```go
// Test individual components
TestDynamicSchemaGeneration()
TestProviderIntegration()
TestTypeMappings()
TestSchemaComparison()
```

## Future Enhancements

### Planned Features
1. **Advanced JSON Indexing**: GIN indexes for complex JSON queries
2. **Schema Versioning**: Track and manage schema evolution
3. **Custom Field Analyzers**: Plugin system for specialized field analysis
4. **Performance Monitoring**: Schema generation metrics and optimization
5. **Multi-Provider Support**: Extend to Azure and GCP resources

### Integration Opportunities
1. **AWS SDK Integration**: Direct struct extraction from SDK
2. **CloudFormation**: Schema generation from CF templates
3. **Terraform**: Integration with Terraform provider schemas
4. **Documentation**: Auto-generate schema documentation

## Troubleshooting

### Common Issues

#### Schema Generation Fails
- Check if Go struct is properly defined
- Verify field exportability (capitalized field names)
- Ensure struct tags are properly formatted

#### Missing Indexes
- Review field naming patterns
- Check type mapping configuration
- Verify indexing strategy settings

#### Performance Issues
- Monitor cache hit rates
- Review concurrent generation limits
- Check reflection usage patterns

### Debug Mode
Enable debug logging:
```go
generator.SetDebugMode(true)
```

Provides detailed information about:
- Field analysis results
- Type mapping decisions
- Index generation choices
- Cache operations
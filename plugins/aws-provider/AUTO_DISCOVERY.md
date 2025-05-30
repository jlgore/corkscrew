# AWS Provider Auto-Discovery Implementation Guide

## Resource Relationship Graph

The AWS provider includes a powerful resource relationship graph builder that analyzes AWS SDK code to automatically discover and map relationships between AWS resources. This helps understand resource dependencies and optimize scanning order.

See [RESOURCE_GRAPH.md](RESOURCE_GRAPH.md) for detailed documentation on the relationship graph functionality.

### Quick Start

```bash
# Build relationship graph from service analysis
go run ./cmd/build-resource-graph/main.go -format mermaid > relationships.mmd

# Analyze specific service relationships  
go run ./cmd/analyze-relationships/main.go -services-dir ./analysis -service ec2 -format summary
```

## Vision: Automatic SDK Analysis & Scanner Generation

### Current State
- Manual scanner implementations for limited services
- Hard-coded service lists
- Incomplete resource coverage
- High maintenance burden

### Desired State
- Automatic discovery of ALL AWS resources by analyzing SDK
- Auto-generated scanners for every List*/Describe* operation
- Dynamic schema generation from response structures
- Zero manual coding for new services

## Implementation Architecture

### Phase 1: SDK Analysis
Analyze AWS SDK Go v2 to discover all available operations:
- Parse SDK source from `github.com/aws/aws-sdk-go-v2/service/*`
- Identify all List*, Describe*, Get* operations
- Extract input/output types and pagination patterns
- Map operations to resource types

### Phase 2: Scanner Generation
Auto-generate scanner code for each discovered resource:
- Create consistent scanning patterns
- Handle pagination automatically
- Extract resource relationships from response structures
- Generate error handling and retry logic

### Phase 3: Schema Generation
Create DuckDB schemas from SDK response types:
- Analyze response structs to determine column types
- Handle nested structures as JSON columns
- Create indexes based on common query patterns
- Support schema evolution

## Claude Prompts for Implementation

### Prompt 1: SDK Analyzer Implementation
```
I need to implement an AWS SDK analyzer for Corkscrew that automatically discovers resources by parsing the AWS SDK Go v2. Here's what it needs to do:

1. Parse AWS SDK packages from github.com/aws/aws-sdk-go-v2/service/*
2. Find all methods that match these patterns:
   - List* (returns multiple resources)
   - Describe* (returns detailed resource info)
   - Get* (returns single resource)
3. Extract metadata for each operation:
   - Input/output type names
   - Pagination fields (NextToken, Marker, etc.)
   - Resource type (extracted from method name)
4. Identify relationships by analyzing struct fields

The analyzer should work both with local SDK copies and by fetching from GitHub.

Here's the existing analyzer stub at internal/generator/aws_analyzer.go:
[paste current aws_analyzer.go content]

Please implement:
- AnalyzeService method that parses SDK AST
- GitHub fetching capability
- Relationship detection from struct analysis
- Output a structured format for scanner generation

Focus on making it work with EC2, S3, and RDS first as examples.
```

### Prompt 2: Scanner Generator Implementation
```
I need to implement a scanner generator that takes the output from the AWS SDK analyzer and generates Go code for scanning resources. 

Input will be AWSServiceInfo structs containing:
- Service name and operations
- Resource types with their List/Describe operations
- Field mappings (ID, Name, ARN, Tags)
- Pagination patterns

The generator should create scanner code that:
1. Implements the ServiceScanner interface:
   - Scan(ctx) returns all resources
   - DescribeResource(ctx, id) returns detailed info
2. Handles pagination automatically
3. Converts SDK responses to pb.ResourceRef
4. Extracts all relevant metadata
5. Handles errors with exponential backoff

Example template pattern:
```go
type {{.ServiceName}}Scanner struct {
    clientFactory *ClientFactory
}

func (s *{{.ServiceName}}Scanner) Scan(ctx context.Context) ([]*pb.ResourceRef, error) {
    client := s.clientFactory.Get{{.ServiceName}}Client(ctx)
    var resources []*pb.ResourceRef
    
    {{range .Resources}}
    // Scan {{.Name}} resources
    {{if .ListOperation}}
    input := &{{.ListOperation.InputType}}{}
    for {
        output, err := client.{{.ListOperation.Name}}(ctx, input)
        if err != nil {
            return nil, fmt.Errorf("failed to list {{.Name}}: %w", err)
        }
        
        for _, item := range output.{{.Name}}s {
            ref := &pb.ResourceRef{
                Id: *item.{{.IDField}},
                Name: *item.{{.NameField}},
                Type: "{{.TypeName}}",
                // ... extract other fields
            }
            resources = append(resources, ref)
        }
        
        {{if .Paginated}}
        if output.NextToken == nil {
            break
        }
        input.NextToken = output.NextToken
        {{else}}
        break
        {{end}}
    }
    {{end}}
    {{end}}
    
    return resources, nil
}
```

Generate complete, compilable scanner code for each service.
```

### Prompt 3: Dynamic Schema Generator
```
I need to implement a schema generator that creates DuckDB schemas from AWS SDK response types.

Requirements:
1. Analyze SDK response structs to determine schema
2. Map Go types to DuckDB column types:
   - string → VARCHAR
   - int/int64 → BIGINT
   - bool → BOOLEAN
   - time.Time → TIMESTAMP
   - complex structs → JSON
   - arrays → JSON
3. Create tables for each resource type
4. Add appropriate indexes and constraints
5. Handle optional fields as nullable

Example input struct:
```go
type Instance struct {
    InstanceId *string
    InstanceType *string
    LaunchTime *time.Time
    State *InstanceState
    Tags []Tag
    // ... more fields
}
```

Should generate:
```sql
CREATE TABLE aws_ec2_instances (
    instance_id VARCHAR PRIMARY KEY,
    instance_type VARCHAR,
    launch_time TIMESTAMP,
    state JSON,
    tags JSON,
    discovered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    raw_data JSON
);

CREATE INDEX idx_instance_type ON aws_ec2_instances(instance_type);
CREATE INDEX idx_launch_time ON aws_ec2_instances(launch_time);
```

The generator should:
- Handle nested structs appropriately
- Create views for common queries
- Support incremental updates
- Generate migration scripts for schema changes
```

### Prompt 4: Integration Pipeline
```
I need to create an integration pipeline that combines the analyzer, generator, and runtime components:

1. Build-time pipeline:
   - Run analyzer on AWS SDK
   - Generate scanner code for all discovered services
   - Generate schema definitions
   - Compile into provider plugin

2. Runtime pipeline:
   - Load generated scanners dynamically
   - Use Asset Explorer when available, fall back to generated scanners
   - Stream results to DuckDB
   - Update relationship graph

Create a Makefile and Go generate directives that:
- Download/update AWS SDK
- Run analyzer to discover services
- Generate scanner code
- Generate schema SQL
- Build the provider plugin

The build should be incremental - only regenerate when SDK changes.

Also create a runtime loader that:
- Registers all generated scanners
- Provides a consistent interface
- Handles service discovery
- Manages concurrency and rate limiting
```

## Testing Strategy

### Unit Tests
- Test analyzer with mock SDK packages
- Verify scanner generation produces valid Go code
- Test schema generation with various struct types

### Integration Tests
- Test against real AWS SDK
- Verify generated scanners compile
- Test scanning against LocalStack
- Validate DuckDB schema creation

### End-to-End Tests
- Full pipeline from SDK analysis to resource storage
- Performance testing with large resource counts
- Relationship extraction validation

## Success Metrics
- 100% coverage of AWS services with List operations
- Zero manual code for new services
- Automatic updates when SDK changes
- Consistent scanning patterns across all services
- Complete relationship mapping
# Corkscrew Generator Plugin Architecture

A plugin-based architecture for the Corkscrew Generator that solves the compilation size problem by allowing dynamic loading of only the AWS services you need at runtime.

## Overview

This implementation uses HashiCorp's go-plugin library with gRPC to create a modular, scalable system where each AWS service is implemented as a separate plugin. This approach provides:

- **Selective Loading**: Load only the services you need at runtime
- **Independent Development**: Services can be developed and tested independently  
- **Smaller Binaries**: Core binary remains small; each service is its own binary
- **Better Isolation**: Service crashes don't affect other services
- **Version Management**: Services can be updated independently
- **Graph Relationships**: Built-in support for resource relationships with DuckDB PGQ

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Core Client   │    │  Plugin Manager  │    │   AWS Plugins   │
│                 │◄──►│                  │◄──►│                 │
│ - CLI Interface │    │ - Plugin Loading │    │ - S3 Scanner    │
│ - Configuration │    │ - gRPC Client    │    │ - EC2 Scanner   │
│ - Result Output │    │ - Lifecycle Mgmt │    │ - RDS Scanner   │
└─────────────────┘    └──────────────────┘    │ - ...           │
                                               └─────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │   DuckDB + PGQ   │
                    │                  │
                    │ - Resource Graph │
                    │ - Relationships  │
                    │ - Query Engine   │
                    └──────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21 or later
- Protocol Buffers compiler (`protoc`)
- AWS credentials configured

### Setup

```bash
# Clone the repository
git clone <repository-url>
cd corkscrew-generator

# Set up development environment
make setup

# Build everything
make all

# Check status
make status
```

### Basic Usage

```bash
# List available plugins
./cmd/plugin-test/plugin-test --list

# Get plugin information
./cmd/plugin-test/plugin-test --service s3 --info

# Scan S3 resources
./cmd/plugin-test/plugin-test --service s3 --region us-east-1

# Save results to file
./cmd/plugin-test/plugin-test --service s3 --region us-east-1 --output s3-resources.json
```

## Project Structure

```
├── proto/                      # Protocol Buffer definitions
│   └── scanner.proto          # Scanner service definition
├── internal/
│   ├── proto/                 # Generated protobuf code
│   ├── shared/                # Plugin interface definitions
│   ├── client/                # Plugin manager and client
│   └── db/                    # DuckDB integration
├── cmd/
│   └── plugin-test/           # Test client for plugins
├── examples/
│   └── plugins/               # Example plugin implementations
│       └── s3/                # S3 plugin example
├── plugins/                   # Built plugin binaries
├── Makefile                   # Build automation
└── README.md                  # This file
```

## Plugin Development

### Creating a New Plugin

```bash
# Create plugin template
make create-plugin-ec2

# Edit the generated file
vim examples/plugins/ec2/main.go

# Build the plugin
make build-plugin-ec2

# Test the plugin
./cmd/plugin-test/plugin-test --service ec2 --info
```

### Plugin Interface

Each plugin must implement the `Scanner` interface:

```go
type Scanner interface {
    Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error)
    GetSchemas(ctx context.Context, req *pb.Empty) (*pb.SchemaResponse, error)
    GetServiceInfo(ctx context.Context, req *pb.Empty) (*pb.ServiceInfoResponse, error)
    StreamScan(req *pb.ScanRequest, stream pb.Scanner_StreamScanServer) error
}
```

### Example Plugin Structure

```go
package main

import (
    "context"
    "github.com/hashicorp/go-plugin"
    "github.com/jlgore/corkscrew-generator/internal/shared"
    pb "github.com/jlgore/corkscrew-generator/internal/proto"
)

type MyServiceScanner struct{}

func (s *MyServiceScanner) Scan(ctx context.Context, req *pb.ScanRequest) (*pb.ScanResponse, error) {
    // Implement scanning logic
    return &pb.ScanResponse{
        Resources: resources,
        Stats:     stats,
        Metadata:  metadata,
    }, nil
}

// Implement other interface methods...

func main() {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: shared.HandshakeConfig,
        Plugins: map[string]plugin.Plugin{
            "scanner": &shared.ScannerGRPCPlugin{
                Impl: &MyServiceScanner{},
            },
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

## Resource Model

### Resource Structure

```protobuf
message Resource {
  string type = 1;                    // Resource type (e.g., "Bucket", "Instance")
  string id = 2;                      // Unique identifier
  string arn = 3;                     // AWS ARN
  string parent_id = 4;               // Parent resource ID
  string region = 5;                  // AWS region
  string account_id = 6;              // AWS account ID
  string raw_data = 7;                // Raw AWS API response (JSON)
  map<string, string> tags = 8;       // Resource tags
  string name = 9;                    // Resource name
  google.protobuf.Timestamp created_at = 10;
  google.protobuf.Timestamp modified_at = 11;
  repeated Relationship relationships = 12;  // Resource relationships
  string attributes = 13;             // Service-specific attributes (JSON)
}
```

### Relationships

Resources can define relationships to other resources:

```protobuf
message Relationship {
  string target_id = 1;               // Target resource ID
  string target_type = 2;             // Target resource type
  string relationship_type = 3;       // Type of relationship
  map<string, string> properties = 4; // Relationship properties
}
```

Example relationships:
- S3 Object → S3 Bucket (`contained_in`)
- EC2 Instance → VPC (`member_of`)
- RDS Instance → Security Group (`protected_by`)

## DuckDB Integration

### Schema

The system automatically creates tables for storing resources and relationships:

```sql
-- Resource vertices
CREATE TABLE aws_resources (
    id VARCHAR PRIMARY KEY,
    type VARCHAR NOT NULL,
    arn VARCHAR,
    name VARCHAR,
    region VARCHAR,
    account_id VARCHAR,
    parent_id VARCHAR,
    raw_data JSON,
    attributes JSON,
    tags JSON,
    created_at TIMESTAMP,
    modified_at TIMESTAMP,
    scanned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Relationship edges
CREATE TABLE aws_relationships (
    from_id VARCHAR NOT NULL,
    to_id VARCHAR NOT NULL,
    relationship_type VARCHAR NOT NULL,
    properties JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id, relationship_type)
);
```

### Graph Queries

```go
// Get resource dependencies
dependencies, err := graphLoader.GetResourceDependencies(ctx, "my-bucket")

// Get resources by type
s3Buckets, err := graphLoader.GetResourcesByType(ctx, "Bucket")

// Find path between resources
path, err := graphLoader.FindResourcePath(ctx, "vpc-123", "instance-456")

// Get resource neighborhood
neighborhood, err := graphLoader.GetResourceNeighborhood(ctx, "my-resource", 2)
```

### Property Graph Queries (PGQ)

When DuckDB PGQ support is available:

```sql
-- Find all resources connected to a VPC
SELECT * FROM GRAPH_TABLE (
    aws_infrastructure
    MATCH (vpc:aws_resources {type: 'VPC'})-[*1..3]-(connected:aws_resources)
    WHERE vpc.id = 'vpc-123abc'
    COLUMNS (connected.type, connected.id, connected.name)
);
```

## Build System

### Makefile Targets

```bash
# Development
make setup                    # Set up development environment
make help                     # Show all available targets
make status                   # Show project status

# Building
make all                      # Build everything
make generate-proto           # Generate protobuf code
make build-test-client        # Build test client
make build-s3-plugin          # Build S3 plugin
make build-plugin-<name>      # Build specific plugin

# Plugin Development
make create-plugin-<name>     # Create new plugin template

# Testing
make test                     # Run Go tests
make test-s3-plugin          # Test S3 plugin (requires AWS creds)
make test-plugin-loading     # Test plugin loading

# Code Quality
make fmt                     # Format code
make lint                    # Lint code

# Cleanup
make clean                   # Clean all generated files
```

### Dependencies

The build system automatically handles:
- Protocol Buffer code generation
- Go module management
- Plugin compilation
- Cross-platform compatibility

## Configuration

### Plugin Discovery

Plugins are discovered by filename convention in the plugins directory:
- `corkscrew-s3` → S3 service
- `corkscrew-ec2` → EC2 service
- `corkscrew-rds` → RDS service

### AWS Configuration

Plugins use standard AWS SDK configuration:
- Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
- AWS credentials file (`~/.aws/credentials`)
- IAM roles (when running on EC2)
- AWS SSO

### Required Permissions

Each plugin defines its required permissions. For S3:

```json
[
  "s3:ListAllMyBuckets",
  "s3:GetBucketLocation",
  "s3:GetBucketVersioning",
  "s3:GetBucketEncryption",
  "s3:GetBucketPublicAccessBlock",
  "s3:GetBucketTagging",
  "s3:ListBucket",
  "s3:GetObjectTagging"
]
```

## Performance Considerations

### Plugin Overhead
- Plugin startup: ~10-50ms per plugin
- Memory usage: Each plugin runs in separate process
- Communication: gRPC with protobuf serialization

### Optimization Strategies
- **Plugin Caching**: Keep plugins loaded for repeated scans
- **Parallel Scanning**: Scan multiple services concurrently
- **Streaming**: Use streaming for large result sets
- **Pagination**: Handle large resource lists efficiently

### Memory Management
```go
// Plugin manager handles lifecycle
pm := client.NewPluginManager("./plugins")
defer pm.Shutdown() // Cleanup all plugins

// Scan multiple services concurrently
results, err := pm.ScanMultipleServices(ctx, []string{"s3", "ec2"}, req)
```

## Security

### Plugin Verification
- Plugins run in separate processes (isolation)
- gRPC communication with authentication
- Plugin binary verification (future: GPG signatures)

### AWS Security
- Least privilege principle for IAM permissions
- Support for cross-account roles
- Audit logging of all API calls

### Data Protection
- Raw AWS API responses stored as JSON
- No sensitive data in logs
- Optional encryption for DuckDB files

## Troubleshooting

### Common Issues

**Plugin not found:**
```bash
# Check plugin directory
ls -la plugins/

# Rebuild plugin
make build-plugin-s3
```

**gRPC connection errors:**
```bash
# Check plugin permissions
chmod +x plugins/corkscrew-s3

# Test plugin directly
./plugins/corkscrew-s3 --help
```

**AWS authentication errors:**
```bash
# Verify AWS credentials
aws sts get-caller-identity

# Check plugin permissions
./cmd/plugin-test/plugin-test --service s3 --info
```

### Debug Mode

Enable debug logging:
```bash
export CORKSCREW_DEBUG=1
./cmd/plugin-test/plugin-test --service s3 --region us-east-1
```

## Roadmap

### Phase 1: Foundation ✅
- [x] Protobuf definitions
- [x] Plugin interface
- [x] Plugin manager
- [x] S3 example plugin
- [x] DuckDB integration
- [x] Build system

### Phase 2: Core Services (In Progress)
- [ ] EC2 plugin
- [ ] RDS plugin
- [ ] IAM plugin
- [ ] Lambda plugin
- [ ] VPC plugin

### Phase 3: Advanced Features
- [ ] Plugin marketplace
- [ ] Auto-update mechanism
- [ ] Web UI for graph visualization
- [ ] Advanced PGQ queries
- [ ] Plugin templates generator

### Phase 4: Production Features
- [ ] Plugin signing and verification
- [ ] Distributed scanning
- [ ] Real-time updates
- [ ] Integration with CI/CD

## Contributing

### Development Workflow

1. **Fork and clone** the repository
2. **Create feature branch**: `git checkout -b feature/new-plugin`
3. **Set up environment**: `make setup`
4. **Make changes** and test: `make test`
5. **Format code**: `make fmt`
6. **Submit pull request**

### Plugin Contribution

1. **Create plugin**: `make create-plugin-myservice`
2. **Implement scanner logic** in `examples/plugins/myservice/main.go`
3. **Add tests** and documentation
4. **Submit pull request** with plugin

### Code Standards

- Follow Go conventions and `gofmt` formatting
- Add comprehensive tests for new features
- Document all public APIs
- Include examples in documentation

## License

[License information here]

## Support

- **Issues**: GitHub Issues
- **Discussions**: GitHub Discussions
- **Documentation**: This README and inline code comments
- **Examples**: See `examples/` directory

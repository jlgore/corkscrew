# Corkscrew - DuckDB based Cloud Configuration Scanner

Hello, and welcome to my dumb little Don Quixote windmill!

Corkscrew is a modular cloud configuration scanner designed to discover, analyze, and map cloud resources across any provider.

## What it does

- **Multi-Cloud Support**: Plugin architecture supports any cloud provider with a Go SDK
- **Resource Discovery**: Automatically discovers and catalogs cloud resources via API calls
- **Relationship Mapping**: Maps dependencies and relationships between resources in a graph database
- **Powerful Querying**: Stores everything in DuckDB for SQL-based analysis and reporting
- **Lightweight Core**: Only load the cloud services you need at runtime

This project uses a plugin-based architecture that integrates with cloud provider Go SDKs. CloudProvider plugins use dynamic service discovery to automatically detect available services, discover resources via SDK API calls, and save their configuration into DuckDB for SQL-based analysis.

## Plugin Architecture

Corkscrew uses HashiCorp's go-plugin library with gRPC to create a modular system where CloudProvider plugins handle resource discovery while the core CLI manages data persistence and querying. Key benefits:

- **Separation of Concerns**: Plugins focus on resource discovery, CLI handles data management
- **No Database Conflicts**: Eliminates plugin database locking issues  
- **Unified Scanner Pattern**: Single discovery engine per provider, no generated code
- **Dynamic Service Support**: Automatic detection of new cloud services

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Core Client   │    │  Plugin Manager  │    │ CloudProviders  │
│                 │◄──►│                  │◄──►│                 │
│ - CLI Interface │    │ - Plugin Loading │    │ - AWS Provider  │
│ - Configuration │    │ - gRPC Client    │    │ - Azure Provider│
│ - Result Output │    │ - Lifecycle Mgmt │    │ - GCP Provider  │
└─────────────────┘    └──────────────────┘    │ - K8s Provider  │
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
For more information on writing plugins see: [PLUGIN_DEVELOPMENT.md](/plugins/PLUGIN_DEVELOPMENT.md). If there is a cloud sdk you want to support open an issue or PR!

## Quick Start

### Prerequisites

- Go 1.21 or later
- Protocol Buffers compiler (`protoc`)
- Cloud provider credentials configured (AWS, Azure, etc.)

### Setup

```bash
# Clone the repository
git clone https://github.com/jlgore/corkscrew.git
cd corkscrew

# Set up development environment (installs protoc, Go plugins, etc.)
make setup

# Build everything (CLI + all plugins)
make build

# Or use the simple build script
./build-all.sh
```

### Quick Build (Alternative)

```bash
# Simple build script - builds main CLI and AWS plugin
./build-all.sh

# Check build status
ls -la corkscrew plugins/*/
```

### Installation

```bash
# Install to ~/.corkscrew/bin/ (optional)
make install

# Add to PATH (add this to your ~/.bashrc or ~/.zshrc)
export PATH="$HOME/.corkscrew/bin:$PATH"
```

### Basic Usage

```bash
# Show provider information
./corkscrew info

# Discover available AWS services
./corkscrew discover --verbose

# List resources from a specific service
./corkscrew list --service s3 --verbose

# Scan multiple services
./corkscrew scan --services s3,ec2 --verbose

# Scan with specific region
export AWS_REGION=us-east-1
./corkscrew scan --services iam --verbose
```

### Plugin-Specific Usage

```bash
# Test AWS plugin directly
./plugins/aws-provider/aws-provider --test

# Test Azure plugin directly  
./plugins/build/corkscrew-azure --test

# List available plugins
make list-plugins
```

## Project Structure

```
├── proto/                      # Protocol Buffer definitions
│   └── scanner.proto          # Scanner service definition
├── internal/
│   ├── proto/                 # Generated protobuf code
│   ├── shared/                # Plugin interface definitions
│   ├── client/                # Plugin manager and client
│   ├── db/                    # DuckDB integration
│   └── provider/              # Provider abstractions
├── cmd/
│   ├── corkscrew/             # Main CLI application
│   ├── generator/             # Code generation tools
│   ├── plugin-test/           # Plugin testing utilities
│   └── resource-lister/       # Resource listing tools
├── plugins/
│   ├── aws-provider/          # AWS provider plugin
│   ├── azure-provider/        # Azure provider plugin
│   ├── build/                 # Built plugin binaries
│   └── PLUGIN_DEVELOPMENT.md  # Plugin development guide
├── build/                     # Build artifacts
├── examples/                  # Example configurations
├── scripts/                   # Build and utility scripts
├── .github/                   # GitHub workflows
├── Makefile                   # Build automation
├── build-all.sh              # Simple build script
├── docker-compose.yml        # Docker setup
└── README.md                 # This file
```

## Plugin Development

Corkscrew has a comprehensive plugin system. See the [Plugin Development Guide](plugins/PLUGIN_DEVELOPMENT.md) for detailed instructions on creating new cloud provider plugins.

### Quick Plugin Testing

```bash
# Test AWS plugin functionality
cd plugins/aws-provider
go run . --test

# Test Azure plugin functionality  
cd plugins/azure-provider
./test-azure-provider.sh

# Build a specific plugin
make build-aws-plugin
make build-azure-plugin
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

## Docker Support

### Quick Start with Docker

```bash
# Build the Docker image
docker build -t corkscrew:latest .

# Run with help
docker run --rm corkscrew:latest --help

# Scan S3 with AWS credentials
docker run --rm \
  -v ~/.aws:/home/corkscrew/.aws:ro \
  -v $(pwd)/output:/app/output \
  corkscrew:latest \
  --services s3 --region us-east-1 --output /app/output/s3-scan.json --verbose
```

### Using Docker Compose

```bash
# Show help
docker-compose run --rm corkscrew

# Scan S3 resources
docker-compose run --rm corkscrew-s3

# Scan multiple services
docker-compose run --rm corkscrew-multi
```

### Development Helper Script

Use the provided development script for easier Docker workflows:

```bash
# Build development image
./scripts/docker-dev.sh build

# Run a scan
./scripts/docker-dev.sh scan s3,ec2 us-east-1

# Get shell access
./scripts/docker-dev.sh shell

# Run tests
./scripts/docker-dev.sh test

# Clean up
./scripts/docker-dev.sh clean
```

### GitHub Container Registry

Pull pre-built images from GitHub Container Registry:

```bash
# Pull latest release
docker pull ghcr.io/jlgore/corkscrew-generator:latest

# Pull specific version
docker pull ghcr.io/jlgore/corkscrew-generator:v1.0.0

# Run from registry
docker run --rm \
  -v ~/.aws:/home/corkscrew/.aws:ro \
  -v $(pwd)/output:/app/output \
  ghcr.io/jlgore/corkscrew-generator:latest \
  --services s3 --region us-east-1 --verbose
```

For detailed Docker usage, deployment examples, and production configurations, see [DOCKER.md](DOCKER.md).

## CI/CD Pipeline

### GitHub Actions

The project includes a comprehensive CI/CD pipeline that:

1. **Tests** - Runs on every push and PR
   - Go tests with multiple versions
   - Protobuf code generation
   - Plugin compilation
   - Binary testing

2. **Build and Push** - Builds multi-architecture Docker images
   - `linux/amd64` and `linux/arm64` support
   - Pushes to GitHub Container Registry
   - Caches layers for faster builds

3. **Release** - Creates releases on git tags
   - Cross-platform binaries (Linux, macOS, Windows)
   - Plugin archives
   - Checksums and signatures
   - Automated release notes

### Triggering Releases

Create a new release by pushing a git tag:

```bash
# Create and push a new tag
git tag v1.0.0
git push origin v1.0.0

# This triggers:
# 1. Full test suite
# 2. Multi-arch Docker build and push
# 3. Cross-platform binary compilation
# 4. GitHub release creation
```

### Available Artifacts

Each release provides:

- **Docker Images**: `ghcr.io/jlgore/corkscrew-generator:v1.0.0`
- **Linux Binaries**: `corkscrew-linux-amd64`, `corkscrew-linux-arm64`
- **macOS Binaries**: `corkscrew-darwin-amd64`, `corkscrew-darwin-arm64`
- **Windows Binaries**: `corkscrew-windows-amd64.exe`
- **Plugin Archive**: `plugins-linux-amd64.tar.gz`
- **Checksums**: `checksums.txt`

### Development Workflow

```bash
# 1. Create feature branch
git checkout -b feature/new-service

# 2. Make changes and test locally
make test
./scripts/docker-dev.sh test

# 3. Push and create PR (triggers CI)
git push origin feature/new-service

# 4. After merge, tag for release
git tag v1.1.0
git push origin v1.1.0
```

## Production Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: corkscrew-scanner
spec:
  replicas: 1
  selector:
    matchLabels:
      app: corkscrew-scanner
  template:
    metadata:
      labels:
        app: corkscrew-scanner
    spec:
      serviceAccountName: corkscrew-scanner
      containers:
      - name: corkscrew
        image: ghcr.io/jlgore/corkscrew-generator:latest
        command: ["corkscrew"]
        args: ["--services", "s3,ec2,rds", "--region", "us-east-1", "--output-db", "/data/scan.db"]
        volumeMounts:
        - name: data
          mountPath: /data
        env:
        - name: AWS_REGION
          value: "us-east-1"
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: corkscrew-data
```

### AWS ECS

```json
{
  "family": "corkscrew-scanner",
  "taskRoleArn": "arn:aws:iam::123456789012:role/CorkscrewTaskRole",
  "executionRoleArn": "arn:aws:iam::123456789012:role/ecsTaskExecutionRole",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "256",
  "memory": "512",
  "containerDefinitions": [
    {
      "name": "corkscrew",
      "image": "ghcr.io/jlgore/corkscrew-generator:latest",
      "command": ["corkscrew"],
      "environment": [
        {
          "name": "AWS_REGION",
          "value": "us-east-1"
        }
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/corkscrew-scanner",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ]
}
```

### Scheduled Scans

```bash
# Daily cron job
0 2 * * * docker run --rm \
  -v ~/.aws:/home/corkscrew/.aws:ro \
  -v /var/log/corkscrew:/app/output \
  ghcr.io/jlgore/corkscrew-generator:latest \
  --services s3,ec2,rds --region us-east-1 \
  --output /app/output/daily-scan-$(date +\%Y\%m\%d).json
```

## License

[License information here]

## Support

- **Issues**: GitHub Issues
- **Discussions**: GitHub Discussions
- **Documentation**: This README, [DOCKER.md](DOCKER.md), and inline code comments
- **Examples**: See `examples/` directory

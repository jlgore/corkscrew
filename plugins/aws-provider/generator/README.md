# Dynamic Client Factory Generator

## Overview

The Dynamic Client Factory Generator is a key component in eliminating hardcoded AWS service dependencies. It generates client factory code dynamically based on services registered in the Dynamic Service Registry, enabling automatic support for any AWS service without manual code changes.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  Client Factory Generator                    │
├─────────────────────────────────────────────────────────────┤
│  Components:                                                │
│  - ClientFactoryGenerator: Main code generation             │
│  - BuildTagManager: Conditional compilation management      │
│  - RegistryIntegration: Registry integration layer         │
│  - ReflectionFallback: Runtime client creation             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Generated Outputs                         │
├─────────────────────────────────────────────────────────────┤
│  - client_factory_generated.go: Main factory with imports  │
│  - client_factory_dynamic.go: Runtime registry wrapper     │
│  - client_factory_reflection.go: Reflection fallback       │
│  - build_config.go: Build tag configuration               │
│  - services/*.go: Per-service build files                 │
│  - Makefile.services: Build targets                       │
└─────────────────────────────────────────────────────────────┘
```

## Features

### 1. Dynamic Import Generation
- Generates imports only for discovered services
- No hardcoded service package references
- Supports conditional compilation with build tags

### 2. Multiple Client Creation Strategies
- **Direct Creation**: For services with available SDK packages
- **Reflection-Based**: For dynamically loaded services
- **Plugin-Based**: For services loaded as plugins
- **Generic Interface**: For unknown services

### 3. Build Tag Management
- Fine-grained control over which services to include
- Service grouping (core, compute, storage, etc.)
- Custom build configurations

### 4. Registry Integration
- Reads service definitions from Dynamic Service Registry
- Supports runtime updates without recompilation
- Caches created clients for performance

## Usage

### Basic Generation

```go
// Create registry
registry := registry.NewServiceRegistry(config)

// Create generator
generator := NewClientFactoryGenerator(registry, "output/client_factory.go")

// Generate client factory
if err := generator.GenerateClientFactory(); err != nil {
    log.Fatal(err)
}

// Generate supporting files
generator.GenerateDynamicWrapper()
generator.GenerateReflectionFallback()
```

### Build Tag Configuration

```go
// Create build tag manager
buildManager := NewBuildTagManager(registry, "output")

// Generate build configuration
if err := buildManager.GenerateBuildConfiguration(); err != nil {
    log.Fatal(err)
}
```

### Integrated Generation

```go
// Create registry integration
integration := NewRegistryIntegration(registry, "output")

// Setup default generators
integration.SetupDefaultGenerators()

// Generate all files
if err := integration.GenerateAll(); err != nil {
    log.Fatal(err)
}
```

## Generated Code Structure

### 1. Main Client Factory (`client_factory_generated.go`)

```go
// +build aws_services

type DynamicClientFactory struct {
    config   aws.Config
    clients  sync.Map
    registry ServiceRegistry
}

func (f *DynamicClientFactory) CreateClient(ctx context.Context, serviceName string) (interface{}, error) {
    switch serviceName {
    case "s3":
        return s3.NewFromConfig(f.config)
    case "ec2":
        return ec2.NewFromConfig(f.config)
    // ... other services
    default:
        return f.createClientViaReflection(ctx, serviceName)
    }
}
```

### 2. Runtime Wrapper (`client_factory_dynamic.go`)

```go
type RuntimeClientFactory struct {
    config      aws.Config
    registry    registry.DynamicServiceRegistry
    clientCache sync.Map
}

func (f *RuntimeClientFactory) CreateClient(ctx context.Context, serviceName string) (interface{}, error) {
    service, exists := f.registry.GetService(serviceName)
    if !exists {
        return nil, fmt.Errorf("service not registered")
    }
    
    // Create client using service definition
    return f.createClientReflectively(service)
}
```

### 3. Service Build Files (`services/s3_client.go`)

```go
// +build aws_services,aws_s3

func init() {
    RegisterService("s3", ServiceRegistration{
        Name: "s3",
        CreateFunc: func(cfg aws.Config) interface{} {
            return s3.NewFromConfig(cfg)
        },
    })
}
```

## Build Configurations

### Build All Services
```bash
make build-all
```

### Build Core Services Only
```bash
make build-minimal
```

### Build Service Groups
```bash
make build-compute    # EC2, ECS, Lambda, etc.
make build-storage    # S3, EFS, etc.
make build-database   # RDS, DynamoDB, etc.
```

### Custom Service Selection
```bash
make build-custom SERVICES="s3,ec2,lambda,dynamodb"
```

## Migration Strategy

### Phase 1: Generate Parallel Implementation
1. Keep existing hardcoded client factory
2. Generate new dynamic factory alongside
3. Add feature flag to switch between them

### Phase 2: Gradual Migration
1. Update code to use generated factory
2. Test with subset of services
3. Expand to all services

### Phase 3: Remove Hardcoded Code
1. Delete old client_factory.go
2. Remove hardcoded imports
3. Clean up unused code

## Performance Considerations

1. **Client Caching**: Clients are cached after first creation
2. **Lazy Loading**: Services loaded only when needed
3. **Build-Time Optimization**: Unused services excluded from binary
4. **Rate Limiting**: Per-service rate limiters created on demand

## Extending the Generator

### Adding a New Generator

```go
type MyCustomGenerator struct {
    registry registry.DynamicServiceRegistry
}

func (g *MyCustomGenerator) Generate() error {
    // Custom generation logic
    return nil
}

func (g *MyCustomGenerator) GetName() string {
    return "My Custom Generator"
}

// Register with integration
integration.AddGenerator("custom", &MyCustomGenerator{registry: reg})
```

### Custom Build Tags

```go
manager := NewBuildTagManager(registry, outputDir)
manager.SetBaseTag("my_custom_tag")
manager.AddServiceTag("s3", "special_s3_tag")
```

## Future Enhancements

1. **Hot Reload**: Update client factory without restart
2. **Service Versioning**: Support multiple SDK versions
3. **Dependency Analysis**: Only include required dependencies
4. **Cross-Compilation**: Support for different platforms
5. **Integration Tests**: Automated testing of generated code

## Testing

Run generator tests:
```bash
go test ./plugins/aws-provider/generator/...
```

Test generated code:
```bash
# Generate code
go run ./cmd/generator/generate_client_factory.go

# Test compilation
go build -tags aws_services ./output/...
```

## Troubleshooting

### Common Issues

1. **Missing Imports**: Ensure service package paths are correct in registry
2. **Build Tag Conflicts**: Check for conflicting build tags
3. **Reflection Failures**: Verify service client types match expected interface
4. **Cache Issues**: Clear client cache if experiencing stale clients

### Debug Mode

Enable debug output:
```bash
export CORKSCREW_GENERATOR_DEBUG=true
```
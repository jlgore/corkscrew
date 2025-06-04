# Reflection-Based Client Factory

## Overview

The Reflection-Based Client Factory provides dynamic AWS service client creation without hardcoded dependencies. It implements a sophisticated fallback chain that attempts multiple strategies to create clients, enabling support for any AWS service discovered at runtime.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  UnifiedClientFactory                        │
├─────────────────────────────────────────────────────────────┤
│  Fallback Chain:                                           │
│  1. OptimizedFactory (with caching)                        │
│  2. ReflectionClient (registry-based)                      │
│  3. DynamicLoader (plugin-based)                          │
│  4. GenericClient (last resort)                           │
└─────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┴─────────────────────┐
        ▼                     ▼                     ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│ ReflectionClient│ │  DynamicLoader  │ │OptimizedFactory │
├─────────────────┤ ├─────────────────┤ ├─────────────────┤
│ - Type caching  │ │ - Plugin loading│ │ - Multi-layer   │
│ - Method lookup │ │ - Symbol lookup │ │   caching       │
│ - Runtime       │ │ - Generic client│ │ - Rate limiting │
│   discovery     │ │   creation      │ │ - Metrics       │
└─────────────────┘ └─────────────────┘ └─────────────────┘
```

## Components

### 1. UnifiedClientFactory
The main entry point that orchestrates all client creation strategies:
- Manages fallback chain
- Provides unified interface
- Handles caching at top level
- Supports custom strategies

### 2. ReflectionClient
Provides reflection-based client creation:
- **Cached Reflection**: Uses cached type and method information
- **Registry-Based**: Creates clients from registry metadata
- **Runtime Discovery**: Discovers services at runtime
- **Type Registration**: Supports pre-registered types

### 3. DynamicLoader
Handles dynamic loading scenarios:
- **Plugin Loading**: Loads service clients from .so files
- **Symbol Resolution**: Finds constructors via symbol lookup
- **Generic Clients**: Creates generic clients for unknown services
- **Type Registry**: Maintains registry of loaded types

### 4. OptimizedFactory
High-performance factory with optimizations:
- **Multi-Layer Caching**: Primary, warm, and TTL-based caches
- **Singleflight**: Prevents duplicate client creation
- **Rate Limiting**: Per-service rate limiters
- **Metrics**: Tracks performance statistics
- **Batch Operations**: Creates multiple clients efficiently

## Usage

### Basic Usage

```go
// Create unified factory
factory := NewUnifiedClientFactory(awsConfig, serviceRegistry)

// Create a client
client, err := factory.CreateClient(ctx, "s3")
if err != nil {
    log.Fatal(err)
}

// Use the client
s3Client := client.(*s3.Client)
```

### With Custom Strategy

```go
// Implement custom strategy
type MyCustomStrategy struct{}

func (s *MyCustomStrategy) Name() string { return "custom" }
func (s *MyCustomStrategy) Priority() int { return 0 } // Highest priority
func (s *MyCustomStrategy) CreateClient(ctx context.Context, name string, cfg aws.Config) (interface{}, error) {
    // Custom creation logic
    return nil, nil
}

// Add to factory
factory.AddStrategy(&MyCustomStrategy{})
```

### Pre-warming Cache

```go
options := FactoryOptions{
    EnablePreWarming: true,
    PreWarmServices: []string{"s3", "ec2", "dynamodb"},
}

factory := NewOptimizedClientFactory(config, registry, options)
```

### Batch Client Creation

```go
services := []string{"s3", "ec2", "lambda", "dynamodb"}
clients, err := factory.CreateClients(ctx, services)
if err != nil {
    log.Printf("Some clients failed: %v", err)
}

// Use clients
for service, client := range clients {
    fmt.Printf("Created client for %s\n", service)
}
```

## Fallback Chain

The factory attempts client creation in this order:

### 1. Optimized Factory (Priority: 1)
- Checks multiple cache layers
- Uses generated code if available
- Falls back to reflection if needed

### 2. Reflection Strategy (Priority: 2)
- Uses cached reflection metadata
- Queries service registry
- Attempts runtime discovery

### 3. Dynamic Loading (Priority: 3)
- Loads plugins from filesystem
- Uses symbol lookup
- Creates generic clients

### 4. Generic Strategy (Priority: 99)
- Last resort fallback
- Returns generic client wrapper
- Supports dynamic invocation

## Performance Considerations

### Caching
- **Primary Cache**: Immediate lookup for created clients
- **Warm Cache**: Pre-created common service clients
- **TTL Cache**: Time-based expiration for dynamic services
- **Reflection Cache**: Cached type and method information

### Concurrency
- **Singleflight**: Prevents duplicate concurrent creation
- **Thread-Safe**: All operations are safe for concurrent use
- **Batch Limits**: Configurable concurrent creation limits

### Metrics
Track performance with built-in metrics:
```go
metrics := factory.GetMetrics()
fmt.Printf("Cache Hit Rate: %.2f%%\n", 
    float64(metrics.CacheHits) / float64(metrics.CacheHits + metrics.CacheMisses) * 100)
fmt.Printf("Average Creation Time: %v\n", 
    time.Duration(metrics.AverageCreateTime))
```

## Advanced Features

### Type Registration
Pre-register types for faster reflection:
```go
reflectionClient.RegisterType("s3", reflect.TypeOf(&s3.Client{}))
```

### Plugin Development
Create service plugins:
```go
// In plugin: aws-s3.go
package main

import "github.com/aws/aws-sdk-go-v2/aws"

func NewClient(cfg aws.Config) interface{} {
    return s3.NewFromConfig(cfg)
}

// Compile: go build -buildmode=plugin aws-s3.go
```

### Custom Caching
Configure cache behavior:
```go
options := FactoryOptions{
    CacheTTL:     5 * time.Minute,
    MaxCacheSize: 1000,
}
```

## Migration Guide

### From Hardcoded Factory

Before:
```go
switch serviceName {
case "s3":
    return s3.NewFromConfig(cfg)
case "ec2":
    return ec2.NewFromConfig(cfg)
// ... many more cases
}
```

After:
```go
return factory.CreateClient(ctx, serviceName)
```

### Gradual Migration

1. **Phase 1**: Use unified factory alongside existing code
2. **Phase 2**: Route specific services through unified factory
3. **Phase 3**: Remove all hardcoded references
4. **Phase 4**: Optimize with pre-warming and caching

## Error Handling

The factory provides detailed error information:
```go
client, err := factory.CreateClient(ctx, "unknown-service")
if err != nil {
    // Error includes which strategies were tried
    // Example: "failed to create client for unknown-service: 
    //           reflection strategy failed: service not found"
}
```

## Testing

### Unit Testing
```go
// Mock registry for testing
mockRegistry := &MockRegistry{
    services: map[string]*ServiceDefinition{
        "test-service": {Name: "test-service"},
    },
}

factory := NewUnifiedClientFactory(config, mockRegistry)
```

### Integration Testing
```go
// Test with real AWS services
factory := NewUnifiedClientFactory(realConfig, realRegistry)
client, err := factory.CreateClient(ctx, "s3")
assert.NoError(t, err)
assert.NotNil(t, client)
```

## Future Enhancements

1. **Hot Reload**: Update client implementations without restart
2. **Version Management**: Support multiple SDK versions
3. **Dependency Injection**: Integrate with DI frameworks
4. **Distributed Caching**: Share cache across instances
5. **Smart Pre-warming**: Learn and pre-warm based on usage patterns
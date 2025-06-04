# Scanner Registry Migration Guide

## Overview

This guide explains how to migrate from the hardcoded scanner loading system to the new dynamic, registry-based scanner loading system.

## Migration Path

### Phase 1: Side-by-Side Implementation (Current)

The new dynamic scanner system runs alongside the existing hardcoded implementation:

```go
// Old way (scanner_registry.go)
func (l *ScannerLoader) loadBuiltInScanners() error {
    services := []struct {
        name          string
        resourceTypes []string
        // ... hardcoded values
    }{
        {"s3", []string{"Bucket"}, ...},
        {"ec2", []string{"Instance", ...}, ...},
        // ... more hardcoded services
    }
    // ... register each service
}

// New way (dynamic_scanner_loader.go)
func (l *DynamicScannerLoader) LoadDynamicScanners() error {
    services := l.registry.ListServices()
    for _, serviceName := range services {
        definition, _ := l.registry.GetService(serviceName)
        // Create scanner from definition
        scanner := l.scannerFactory.CreateScanner(serviceName, definition)
        // Register with metadata from definition
    }
}
```

### Phase 2: Update Existing Code

1. **Replace ScannerLoader initialization:**

Before:
```go
loader := &ScannerLoader{registry: scannerRegistry}
err := loader.LoadScanners()
```

After:
```go
serviceRegistry := registry.GetGlobalServiceRegistry()
loader := NewUpdatedScannerLoader(serviceRegistry)
err := loader.LoadScanners()
```

2. **Replace metadata helper functions:**

Before:
```go
resourceTypes := getResourceTypesForService("s3")
permissions := getPermissionsForService("s3")
rateLimit := getRateLimitForService("s3")
```

After:
```go
// These now use the dynamic provider internally
resourceTypes := getResourceTypesForServiceDynamic("s3")
permissions := getPermissionsForServiceDynamic("s3")
rateLimit := getRateLimitForServiceDynamic("s3")
```

### Phase 3: Remove Hardcoded Functions

Once all code is updated, remove the hardcoded functions:

```go
// DELETE these functions from scanner_registry.go:
// - loadBuiltInScanners()
// - getResourceTypesForService()
// - getPermissionsForService()
// - getRateLimitForService()
// - getBurstLimitForService()
```

## Code Examples

### Basic Migration

```go
// In your main initialization code
func initializeAWSProvider() error {
    // Create service registry
    registryConfig := registry.RegistryConfig{
        PersistencePath:     "/etc/corkscrew/aws-services.json",
        EnableCache:         true,
        UseFallbackServices: true,
    }
    serviceRegistry := registry.NewServiceRegistry(registryConfig)
    
    // Create dynamic scanner registry
    scannerRegistry := NewDynamicScannerRegistry(serviceRegistry)
    
    // Initialize (loads scanners dynamically)
    if err := scannerRegistry.Initialize(); err != nil {
        return err
    }
    
    return nil
}
```

### Custom Scanner Factory

```go
// Implement custom scanner creation strategy
type EC2SpecializedStrategy struct{}

func (s *EC2SpecializedStrategy) CanHandle(name string, def *registry.ServiceDefinition) bool {
    return name == "ec2" && def.CustomHandlerRequired
}

func (s *EC2SpecializedStrategy) CreateScanner(name string, def *registry.ServiceDefinition) ServiceScanner {
    return &EC2SpecializedScanner{
        // Custom EC2 implementation
    }
}

// Register the strategy
factory := NewAdvancedScannerFactory()
factory.RegisterStrategy("ec2-specialized", &EC2SpecializedStrategy{})
```

### Backward Compatibility

The system maintains backward compatibility:

```go
// This still works but uses dynamic loading internally
loader := &ScannerLoader{registry: scannerRegistry}
err := loader.LoadScanners()

// If dynamic loading fails, it falls back to hardcoded scanners
// You'll see this in logs:
// "WARNING: Dynamic scanner loading failed, falling back to built-in scanners"
```

## Testing Migration

### 1. Verify Dynamic Loading

```go
func TestDynamicScannerLoading(t *testing.T) {
    // Create test registry with known services
    testRegistry := createTestRegistry()
    
    // Create dynamic scanner registry
    scannerRegistry := NewDynamicScannerRegistry(testRegistry)
    
    // Initialize
    err := scannerRegistry.Initialize()
    assert.NoError(t, err)
    
    // Verify scanners loaded
    services := scannerRegistry.ListServices()
    assert.Contains(t, services, "s3")
    assert.Contains(t, services, "ec2")
}
```

### 2. Test Metadata Retrieval

```go
func TestDynamicMetadata(t *testing.T) {
    provider := NewDynamicMetadataProvider(testRegistry)
    
    // Test resource types
    types := provider.GetResourceTypesForService("s3")
    assert.Contains(t, types, "Bucket")
    
    // Test permissions
    perms := provider.GetPermissionsForService("s3")
    assert.Contains(t, perms, "s3:ListBuckets")
}
```

## Benefits After Migration

1. **No More Hardcoded Services**: New AWS services automatically supported
2. **Dynamic Configuration**: Rate limits, permissions, etc. from registry
3. **Extensibility**: Custom scanner strategies for special services
4. **Runtime Updates**: Refresh scanners without restart
5. **Consistent Metadata**: All service info from single source

## Troubleshooting

### Issue: Scanners not loading
```
ERROR: Failed to load dynamic scanners
```
**Solution**: Check service registry is properly initialized and contains services

### Issue: Missing metadata
```
WARNING: No services found in registry
```
**Solution**: Ensure registry is loaded from file or populated with services

### Issue: Performance degradation
**Solution**: Enable metadata caching:
```go
provider.RefreshCache() // Pre-populate cache
```

## Rollback Plan

If issues arise, you can temporarily revert to hardcoded scanners:

1. Set environment variable: `CORKSCREW_USE_BUILTIN_SCANNERS=true`
2. Or modify the loader to force built-in usage:
```go
compatLoader := NewBackwardCompatibleLoader(serviceRegistry, scannerRegistry)
compatLoader.useBuiltIn = true
```

## Next Steps

After successful migration:
1. Remove hardcoded service lists from codebase
2. Update documentation to reflect dynamic loading
3. Add monitoring for dynamic loading failures
4. Implement service discovery integration
# Discovery Caching

This document describes the discovery caching system implemented in Step 4.2 of the AWS provider migration plan.

## Overview

The discovery caching system reduces API calls and improves performance by caching discovery results for up to 24 hours. It includes support for manual overrides and automatic cache management.

## Features

- **TTL-based caching**: Cache results with configurable TTL (default: 24 hours)
- **Manual overrides**: Override specific services with custom definitions
- **Cache validation**: Integrity checks with checksums
- **Automatic cleanup**: Removes old cache backup files
- **Statistics**: Detailed cache usage statistics

## Usage

### Basic Usage

```go
import "github.com/jlgore/corkscrew/plugins/aws-provider/discovery"

// Create discovery with default cache settings
discovery := discovery.NewRegistryDiscovery(cfg)

// Run discovery (will use cache if available)
services, err := discovery.DiscoverServices(ctx)
```

### Custom Cache Configuration

```go
cacheConfig := discovery.CacheConfig{
    CacheDir:        "./custom/cache",
    TTL:             12 * time.Hour,
    MaxCacheSize:    100 * 1024 * 1024, // 100MB
    EnableChecksum:  true,
    AutoRefresh:     false,
}

discovery := discovery.NewRegistryDiscoveryWithCacheConfig(cfg, cacheConfig)
```

### Cache Management

```go
// Check cache status
stats := discovery.GetCacheStats()
fmt.Printf("Cache is fresh: %v\n", stats["is_fresh"])

// Force refresh (ignore cache)
services, err := discovery.ForceRefresh(ctx)

// Invalidate cache
err := discovery.InvalidateCache()

// Add manual override
customService := &pb.ServiceInfo{
    Name: "custom-s3",
    DisplayName: "Custom S3",
    // ... other fields
}
err := discovery.AddManualOverride("s3", customService)
```

## Command Line Usage

### Basic Discovery with Caching

```bash
# Run discovery (uses cache if available)
./discover-services

# Force fresh discovery
./discover-services --force-refresh

# Custom cache settings
./discover-services --cache-dir ./my-cache --cache-ttl 6h
```

### Cache Management Commands

```bash
# Show cache statistics
./discover-services --show-cache

# Invalidate cache
./discover-services --invalidate-cache

# Verbose output with cache details
./discover-services --verbose
```

## Cache Structure

### Cache Directory Layout

```
./registry/cache/
├── discovery_cache.json           # Main cache file
├── manual_overrides.json          # Manual service overrides
├── discovery_cache_2024-01-15_14-30-00.json  # Backup files
└── discovery_cache_2024-01-14_09-15-30.json
```

### Cache File Format

```json
{
  "version": 1,
  "cached_at": "2024-01-15T14:30:00Z",
  "expires_at": "2024-01-16T14:30:00Z",
  "source": "github-api",
  "service_count": 245,
  "services": [
    {
      "name": "s3",
      "display_name": "Amazon S3",
      "package_name": "github.com/aws/aws-sdk-go-v2/service/s3",
      "resource_types": [...]
    }
  ],
  "checksum": "245-1705327800-github-api",
  "metadata": {
    "corkscrew_version": "v1.0",
    "cache_ttl_hours": 24
  }
}
```

## Manual Overrides

Manual overrides allow you to customize specific services without affecting the discovery process:

```json
{
  "s3": {
    "name": "s3",
    "display_name": "Custom S3 Service",
    "description": "Customized S3 with additional features",
    "resource_types": [
      {
        "name": "CustomBucket",
        "type_name": "AWS::S3::CustomBucket",
        "list_operation": "ListBuckets",
        "supports_tags": true
      }
    ]
  }
}
```

## Cache Performance

Typical performance improvements with caching:

- **Fresh discovery**: 15-30 seconds (GitHub API calls + analysis)
- **Cached discovery**: 100-500ms (file read + deserialization)
- **Performance improvement**: 30-300x faster with cache

## Best Practices

1. **Use appropriate TTL**: 24 hours for production, shorter for development
2. **Monitor cache hit rates**: Use `--show-cache` to check effectiveness
3. **Clean up regularly**: Old cache files are cleaned automatically
4. **Backup important overrides**: Manual overrides are persisted but should be version controlled
5. **Invalidate after AWS SDK updates**: New services may not be detected with stale cache

## Troubleshooting

### Cache Issues

```bash
# Check cache status
./discover-services --show-cache

# Clear problematic cache
./discover-services --invalidate-cache

# Force fresh discovery
./discover-services --force-refresh
```

### Common Problems

- **Stale cache**: Cache TTL expired but not refreshed
- **Corrupted cache**: Checksum mismatch, cache will be ignored
- **Permission errors**: Cache directory not writable
- **Disk space**: Cache files taking too much space (check MaxCacheSize)

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `CacheDir` | `./registry/cache` | Directory for cache files |
| `TTL` | `24h` | Cache time-to-live |
| `MaxCacheSize` | `50MB` | Maximum cache file size |
| `EnableChecksum` | `true` | Enable integrity checks |
| `AutoRefresh` | `false` | Automatic background refresh |
| `RefreshInterval` | `12h` | Background refresh interval |

## Integration with Registry

The cache system integrates seamlessly with the service registry:

1. Cache miss → Fresh discovery → Save to cache → Register services
2. Cache hit → Load from cache → Apply overrides → Register services
3. Manual overrides → Always applied regardless of cache status

This ensures that the registry always has the most appropriate service definitions while minimizing API calls and improving startup performance.
# Corkscrew Service Configuration Guide

## Overview

Starting with version 2.0, Corkscrew supports flexible service configuration, replacing the hardcoded list of 18 AWS services with a configurable system that allows you to specify exactly which services to analyze.

## Quick Start

1. **Generate default configuration**:
   ```bash
   corkscrew config init
   # or
   make config-init
   ```

2. **View current configuration**:
   ```bash
   corkscrew config show
   # or
   make config-show
   ```

3. **Validate configuration**:
   ```bash
   corkscrew config validate
   # or
   make config-validate
   ```

## Configuration File

The configuration file `corkscrew.yaml` should be placed in your project root. You can also specify a custom location using the `CORKSCREW_CONFIG_FILE` environment variable.

### Basic Structure

```yaml
version: "1.0"
providers:
  aws:
    discovery_mode: hybrid
    services:
      include:
        - s3
        - ec2
        - lambda
      exclude:
        - gamelift
    analysis:
      skip_empty: true
      workers: 4
```

## Discovery Modes

### Manual Mode
Only analyzes services explicitly listed in the `include` section:

```yaml
discovery_mode: manual
services:
  include:
    - s3
    - ec2
    - lambda
```

### Auto Mode
Automatically discovers services from:
- Your `go.mod` file (AWS SDK dependencies)
- Local AWS SDK installation
- GitHub API (AWS SDK repository)

```yaml
discovery_mode: auto
services:
  exclude:
    - chatbot  # Exclude rarely used services
    - gamelift
```

### Hybrid Mode (Recommended)
Combines manual and auto discovery. Starts with your explicit list and adds auto-discovered services:

```yaml
discovery_mode: hybrid
services:
  include:
    - s3      # Always include these
    - ec2
    - lambda
  exclude:
    - chatbot # Never include these
```

## Service Groups

Use predefined service groups for common scenarios:

```yaml
service_groups:
  core:
    - ec2
    - s3
    - iam
    - vpc
  
  compute:
    - lambda
    - ecs
    - eks
    - batch
  
  data:
    - rds
    - dynamodb
    - athena
    - glue
```

Use service groups with the analyzer:
```bash
./cmd/analyzer/analyzer -service-group compute
```

## Configuration Methods

### 1. Configuration File (Recommended)

Create `corkscrew.yaml` in your project root. See `corkscrew.yaml.example` for a complete example.

### 2. Environment Variables

Override configuration with environment variables:

```bash
# Override service list
export CORKSCREW_AWS_SERVICES="s3,ec2,lambda,rds,dynamodb"

# Override discovery mode
export CORKSCREW_DISCOVERY_MODE="auto"

# Specify custom config file
export CORKSCREW_CONFIG_FILE="/path/to/custom-config.yaml"
```

### 3. Command-Line Arguments

Override configuration when running the analyzer:

```bash
# Analyze specific services
./cmd/analyzer/analyzer -services "s3,ec2,lambda"

# Use a service group
./cmd/analyzer/analyzer -service-group data

# List services that would be analyzed
./cmd/analyzer/analyzer -list-services
```

## Analysis Configuration

Configure how services are analyzed:

```yaml
analysis:
  # Skip services with no resources (default: true)
  skip_empty: true
  
  # Number of parallel workers (default: 4)
  workers: 4
  
  # Enable caching (default: true)
  cache_enabled: true
  
  # Cache time-to-live (default: 24h)
  cache_ttl: 24h
```

## Examples

### Minimal Configuration

```yaml
version: "1.0"
providers:
  aws:
    discovery_mode: manual
    services:
      include:
        - s3
        - ec2
        - lambda
```

### Production Configuration

```yaml
version: "1.0"
providers:
  aws:
    discovery_mode: hybrid
    services:
      include:
        # Core infrastructure
        - ec2
        - s3
        - vpc
        - iam
        
        # Compute
        - lambda
        - ecs
        - eks
        
        # Data
        - rds
        - dynamodb
        - elasticache
        
        # Monitoring
        - cloudwatch
        - cloudtrail
        
      exclude:
        # Gaming services
        - gamelift
        - gamesparks
        
        # Rarely used
        - chatbot
        - honeycode
        
    analysis:
      skip_empty: true
      workers: 8
      cache_enabled: true
      cache_ttl: 12h
```

### Development Configuration

```yaml
version: "1.0"
providers:
  aws:
    discovery_mode: auto
    services:
      exclude: []  # Discover everything
    analysis:
      skip_empty: false  # See all services
      workers: 2
      cache_enabled: false  # Always fresh data
```

## Backward Compatibility

If no configuration file is provided, Corkscrew uses the original 18 services for backward compatibility:

- ec2, s3, lambda, rds, dynamodb, iam
- sqs, sns, ecs, eks, cloudformation
- cloudwatch, route53, elasticloadbalancing
- autoscaling, kms, secretsmanager, ssm

## Troubleshooting

### Services not being analyzed

1. Check configuration is valid:
   ```bash
   corkscrew config validate
   ```

2. List services that will be analyzed:
   ```bash
   ./cmd/analyzer/analyzer -list-services
   ```

3. Check for typos in service names

### Configuration not loading

1. Check file location (should be in project root)
2. Verify YAML syntax
3. Check file permissions
4. Use `corkscrew config show` to see what's loaded

### Auto-discovery not working

1. Ensure go.mod exists and is readable
2. Run `go mod tidy` to clean up dependencies
3. Check AWS SDK imports are properly formatted
4. Try setting `GITHUB_TOKEN` for GitHub API access

## Migration from Hardcoded Services

1. Run `corkscrew config init` to create a default configuration
2. The default includes all 18 original services plus common additions
3. Customize as needed for your environment
4. Test with `./cmd/analyzer/analyzer -list-services`
5. Run full analysis to verify

## Best Practices

1. **Use hybrid mode** for the best balance of control and discovery
2. **Exclude unused services** to improve performance
3. **Use service groups** to organize related services
4. **Enable caching** for faster repeated analyses
5. **Version control** your `corkscrew.yaml` file
6. **Document** why certain services are included/excluded

## Performance Considerations

- Each service adds analysis time
- Use `skip_empty: true` to avoid analyzing services with no resources
- Increase `workers` for faster parallel analysis (CPU permitting)
- Enable caching to avoid repeated API calls

## Future Enhancements

- Service dependency resolution (automatically include dependent services)
- Per-service analysis configuration
- Service cost estimates
- Region-specific service lists
- Dynamic service list updates from AWS
# AWS Provider Client Factory Generation

## Overview

The AWS provider uses an automated client factory generation system that discovers AWS services through SDK analysis and generates client registration code automatically. This system replaces the previous hardcoded approach and supports all discovered AWS services.

## How It Works

1. **Service Discovery**: The `make analyze` target analyzes the AWS SDK to discover available services
2. **Code Generation**: The `make generate-client-factory` target generates `client_registry_generated.go`
3. **Client Registration**: The generated file registers all services with the client registry at startup

## Generated Files

- `client_registry_generated.go` - Auto-generated client registry with all discovered services
- `generated/services.json` - Service metadata from SDK analysis

## Current Services (18 total)

The following AWS services are automatically discovered and registered:

- autoscaling
- cloudformation
- cloudwatch
- dynamodb
- ec2
- ecs
- eks
- elasticloadbalancing
- iam
- kms
- lambda
- rds
- route53
- s3
- secretsmanager
- sns
- sqs
- ssm

## Usage

### Regenerating the Client Factory

To regenerate the client factory after SDK updates or to add new services:

```bash
# Force regeneration of service analysis and client factory
touch .force-regenerate
make generate-client-factory

# Or use the full generation pipeline
make generate
```

### Adding New Services

New services are automatically discovered when you:

1. Add the service SDK dependency to `go.mod`
2. Run `make analyze` to rediscover services
3. Run `make generate-client-factory` to regenerate the registry

### Testing

Verify all services are available:

```bash
# Run the built-in test
go test -v -run TestAllServicesAvailable

# Or use the CLI test
./aws-provider --test-services
```

## Architecture

### Components

1. **Service Analyzer** (`cmd/analyzer`)
   - Discovers AWS services from the SDK
   - Extracts operations and resource types
   - Generates `services.json`

2. **Client Factory Generator** (`generator/client_factory_generator.go`)
   - Reads `services.json`
   - Generates registration code
   - Creates `client_registry_generated.go`

3. **Client Registry** (`pkg/client/client_registry.go`)
   - Manages service constructors
   - Provides runtime access to clients
   - Supports dynamic service discovery

### Generated Code Structure

The generated `client_registry_generated.go` follows this pattern:

```go
package main

import (
    "log"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/jlgore/corkscrew/plugins/aws-provider/pkg/client"
    // Service imports...
)

func init() {
    log.Printf("Initializing generated client factory with N AWS services")
    
    // Register all service client constructors
    client.RegisterConstructor("servicename", func(cfg aws.Config) interface{} {
        return servicename.NewFromConfig(cfg)
    })
    // ... repeat for all services
    
    log.Printf("Successfully registered %d AWS service client constructors", 
        len(client.ListRegisteredServices()))
}
```

## Troubleshooting

### Missing Services

If a service is missing:
1. Ensure the SDK dependency is in `go.mod`
2. Run `go mod tidy`
3. Force regeneration with `touch .force-regenerate && make generate-client-factory`

### Compilation Errors

If you get undefined type errors:
1. Check that all service SDKs are installed: `go mod download`
2. Verify the service is listed in `generated/services.json`
3. Ensure `client_registry_generated.go` includes the service

### Test Failures

If tests fail:
1. Ensure the generated file exists and is not empty
2. Check that the init() function is being called (look for log output)
3. Verify no old registry files are conflicting

## Migration from Hardcoded Registry

The system has been migrated from a hardcoded 7-service registry to a fully generated 18+ service registry:

- **Old**: `client_registry_init.go` with 7 hardcoded services
- **New**: `client_registry_generated.go` with all discovered services

The old file has been removed to prevent conflicts.

## Future Enhancements

- Support for service-specific initialization options
- Automatic SDK dependency management
- Service feature detection (pagination, filtering, etc.)
- Cross-region service availability detection
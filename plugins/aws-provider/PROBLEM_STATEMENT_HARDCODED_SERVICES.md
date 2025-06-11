# Problem Statement: Hardcoded AWS Service List in Analyzer

## Current State

The AWS provider's service discovery system has a critical limitation: the list of AWS services to analyze is hardcoded in the analyzer tool (`cmd/analyzer/main.go`). This creates several issues:

### The Problem

1. **Hardcoded Service List**: The analyzer contains a fixed array of 18 AWS services:
   ```go
   commonServices := []string{
       "ec2", "s3", "lambda", "rds", "dynamodb", "iam",
       "sqs", "sns", "ecs", "eks", "cloudformation",
       "cloudwatch", "route53", "elasticloadbalancing",
       "autoscaling", "kms", "secretsmanager", "ssm",
   }
   ```

2. **Limited Extensibility**: Adding new AWS services requires:
   - Modifying the analyzer source code
   - Recompiling the analyzer
   - No way for users to specify custom service lists
   - No organizational control over which services to include

3. **Missing Services**: AWS has 200+ services, but only 18 are included. Notable omissions:
   - athena
   - glue
   - kinesis
   - elasticache
   - apigateway
   - cloudtrail
   - eventbridge
   - stepfunctions
   - And many more...

4. **No Configuration Options**: Users cannot:
   - Exclude services they don't use
   - Add services specific to their needs
   - Configure service discovery per environment

## Impact

- **Incomplete Coverage**: Organizations using AWS services beyond the hardcoded 18 cannot use the automated discovery
- **Maintenance Burden**: Every new AWS service requires code changes and redeployment
- **No Customization**: Different teams/environments cannot have different service configurations
- **Wasted Resources**: Analysis includes services that may not be used, increasing generation time

## Proposed Solution

The service list should be externalized through one or more of these approaches:

### Option 1: Command-Line Arguments
```bash
# Analyze specific services
go run ./cmd/analyzer -services "ec2,s3,lambda,rds,athena,glue"

# Or use a service list file
go run ./cmd/analyzer -services-file services.txt
```

### Option 2: Configuration File
Create a `corkscrew-services.yaml` or `aws-services.json`:
```yaml
# corkscrew-services.yaml
aws:
  services:
    include:
      - ec2
      - s3
      - lambda
      - rds
      - dynamodb
      - iam
      # ... additional services
    exclude:
      - elasticloadbalancing  # Optional exclusions
  
  # Optional: service groups
  service_groups:
    core:
      - ec2
      - s3
      - iam
    compute:
      - lambda
      - ecs
      - eks
    data:
      - rds
      - dynamodb
      - athena
      - glue
```

### Option 3: Auto-Discovery
Analyze go.mod to find all imported AWS SDK services:
```go
// Scan go.mod for patterns like:
// github.com/aws/aws-sdk-go-v2/service/*
```

### Option 4: Hybrid Approach
- Default to auto-discovery from go.mod
- Allow override via config file
- Support command-line flags for one-off analysis

## Benefits of Fixing This

1. **Complete Service Coverage**: Support all AWS services that users actually use
2. **Flexible Configuration**: Different environments can have different service sets
3. **Reduced Generation Time**: Only analyze services that are needed
4. **Future-Proof**: New AWS services automatically supported without code changes
5. **Better User Experience**: Users control their service discovery

## Implementation Priority

This should be considered a **high priority** fix because:
- It blocks users from using services beyond the hardcoded 18
- It requires manual code changes for each new service
- It impacts the scalability of the corkscrew project
- The fix is relatively straightforward to implement

## Next Steps

1. Decide on configuration approach (recommend Option 4 - Hybrid)
2. Update analyzer to read service list from external source
3. Update documentation with configuration examples
4. Consider backward compatibility (default to current list if no config)
5. Add validation to ensure requested services exist in SDK
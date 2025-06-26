# Corkscrew Integration Test Framework

A comprehensive test framework for validating Corkscrew's resource discovery and scanning capabilities using real cloud infrastructure deployed via Pulumi.

## Architecture Overview

```
┌─────────────────────┐
│   Test Framework    │
│   (Go Test Suite)   │
└──────────┬──────────┘
           │
┌──────────▼──────────┐     ┌─────────────────┐
│  Pulumi Automation  │────►│ Cloud Provider  │
│    API (Go)         │     │ (AWS/Azure/GCP) │
└──────────┬──────────┘     └────────┬────────┘
           │                         │
┌──────────▼──────────┐              │
│  Test Orchestrator  │              │
│  - Deploy Resources │              │
│  - Run Corkscrew    │◄─────────────┘
│  - Verify in DuckDB │
│  - Cleanup          │
└─────────────────────┘
```

## Key Components

### 1. TestHarness (automation/harness.go)
- **Deploy()**: Deploys infrastructure using Pulumi
- **Scan()**: Runs Corkscrew against deployed resources  
- **GetExpectedResources()**: Returns scenario's expected resources
- **Destroy()**: Cleans up deployed infrastructure

### 2. Scenario Interface (automation/harness.go)
- **DefineResources()**: Creates Pulumi resources for testing
- **GetExpectedResources()**: Returns expected verification data
- **GetName()**: Returns scenario identifier
- **GetServices()**: Returns AWS services to scan

### 3. Verifier (verification/duckdb_verifier.go)
- **VerifyResources()**: Checks all expected resources exist
- **VerifyRelationships()**: Validates resource relationships
- **VerifyAttributes()**: Confirms resource attributes match

### 4. TestResult (types.go)
- Comprehensive metrics and validation results
- Performance data (deployment time, scan time, etc.)
- Success/failure analysis with detailed error reporting

## Available Test Scenarios

### 1. SimpleS3 (`simple-s3`)
- **Purpose**: Basic S3 bucket testing
- **Resources**: 1 S3 bucket with versioning and encryption
- **Services**: s3
- **Use Case**: Quick validation, CI/CD pipelines

### 2. NetworkStack (`network-stack`)  
- **Purpose**: VPC networking components
- **Resources**: VPC, subnets, security groups, NAT gateway, route tables
- **Services**: ec2
- **Use Case**: Network relationship testing

### 3. ComputeStack (`compute-stack`)
- **Purpose**: EC2 and auto-scaling resources
- **Resources**: EC2 instances, launch templates, auto-scaling groups, load balancers
- **Services**: ec2, elb, autoscaling
- **Use Case**: Compute resource discovery

### 4. SecurityStack (`security-stack`)
- **Purpose**: IAM and security services
- **Resources**: IAM roles/policies/users, KMS keys, Secrets Manager
- **Services**: iam, kms, secretsmanager
- **Use Case**: Security resource relationships

### 5. StorageStack (`storage-stack`)
- **Purpose**: Multiple storage types
- **Resources**: S3 buckets (various configs), EBS volumes, EFS file systems
- **Services**: s3, ebs, efs, ec2
- **Use Case**: Storage diversity testing

## Usage Examples

### Basic Single Test
```go
func TestSimpleS3(t *testing.T) {
    ctx := context.Background()
    
    config := OrchestratorConfig{
        Provider:      "aws",
        Region:        "us-east-1", 
        ScenarioName:  "simple-s3",
        TestID:        "test-123",
        CorkscrewPath: "../../corkscrew",
        Timeout:       10 * time.Minute,
    }
    
    orchestrator, err := NewTestOrchestrator(ctx, config)
    require.NoError(t, err)
    
    result, err := orchestrator.RunTest(ctx)
    require.NoError(t, err)
    require.True(t, result.Success)
}
```

### Parameterized Testing
```go
func TestMultipleRegions(t *testing.T) {
    matrix := TestMatrix{
        Providers: []string{"aws"},
        Regions:   []string{"us-east-1", "us-west-2", "eu-west-1"},
        Scenarios: []string{"simple-s3", "network-stack"},
        Configs: map[string]TestConfiguration{
            "quick": {
                TestTimeout:  5 * time.Minute,
                CleanupDelay: 15 * time.Second,
            },
            "standard": {
                TestTimeout:  10 * time.Minute,
                CleanupDelay: 30 * time.Second,
            },
        },
    }
    
    framework := NewTestFramework(matrix)
    framework.SetParallel(true)
    framework.SetMaxWorkers(3)
    framework.RunMatrix(t)
}
```

### Performance Benchmarking
```go
func BenchmarkScenarios(b *testing.B) {
    for _, scenario := range []string{"simple-s3", "network-stack"} {
        b.Run(scenario, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                // Run test and measure performance
            }
        })
    }
}
```

## Configuration Options

### Test Configurations
- **quick**: 5min timeout, minimal resources (t2.nano)
- **standard**: 10min timeout, standard resources (t2.micro)  
- **encrypted**: 15min timeout, encryption enabled
- **performance**: 20min timeout, larger instances (t3.small)

### Environment Variables
```bash
export AWS_REGION=us-east-1
export PULUMI_ACCESS_TOKEN=your_token
export CORKSCREW_PATH=../../corkscrew
```

## Running Tests

### Prerequisites
1. **AWS Credentials**: Configured via AWS CLI or environment variables
2. **Pulumi**: Installed and authenticated
3. **Corkscrew Binary**: Built and available at specified path
4. **Go Dependencies**: `go mod tidy`

### Single Scenario Test
```bash
cd test/harness
go test -run TestFrameworkSimple -v
```

### Full Matrix Test (Long Running)
```bash
go test -run TestFrameworkRegions -v -timeout 30m
```

### Skip Long Tests
```bash
go test -short
```

### With Custom Configuration
```bash
go test -run TestSpecific -v \
  -provider=aws \
  -region=us-west-2 \
  -scenario=network-stack \
  -config=standard
```

## Framework Implementation Summary

### ✅ Completed Components

1. **Reusable TestHarness** with Deploy(), Scan(), Verify(), Cleanup() methods
2. **Scenario Interface** with DefineResources() method for extensibility  
3. **Enhanced Verifier** with multiple verification types (resources, attributes, relationships)
4. **Comprehensive TestResult** struct capturing metrics and validation results
5. **5 Test Scenarios**:
   - SimpleS3: Basic S3 bucket with versioning
   - NetworkStack: VPC, subnets, security groups, NAT gateway  
   - ComputeStack: EC2, auto-scaling, load balancer
   - SecurityStack: IAM roles/policies, KMS, secrets
   - StorageStack: Multiple storage types (S3, EBS, EFS)
6. **Parameterized Testing** with:
   - Multiple regions (us-east-1, us-west-2, eu-west-1)
   - Different configurations (quick, standard, encrypted, performance)
   - Matrix testing across all combinations
   - Parallel execution with worker pools
   - Performance benchmarking

### Key Features

- **Fast & Cost-Effective**: Resources exist only during test execution (3-5 minutes)
- **Programmatic Control**: Pulumi Automation API for dynamic scenario generation
- **Detailed Verification**: Checks resource discovery, attributes, and relationships
- **Performance Metrics**: Tracks deployment, scan, and verification times
- **Comprehensive Reporting**: JSON reports with statistics and breakdowns
- **Error Handling**: Proper cleanup, resource preservation for debugging
- **Extensible Design**: Easy to add new scenarios and verification types

## Cost Considerations

- **Resource Costs**: Tests use minimal resources (t2.micro, small storage)
- **Duration**: Most tests complete in 3-5 minutes
- **Cleanup**: Automatic cleanup prevents resource accumulation
- **Parallel Limits**: Max 3 concurrent tests to avoid AWS limits

## Security

- **Credentials**: Use IAM roles, never commit credentials
- **Resource Isolation**: Each test uses unique TestID
- **Cleanup**: Automatic cleanup prevents security exposure
- **Least Privilege**: Tests only require necessary permissions

This framework provides a robust foundation for comprehensive integration testing of Corkscrew's cloud resource discovery capabilities across multiple scenarios, regions, and configurations.
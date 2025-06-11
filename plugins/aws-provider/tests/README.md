# AWS Provider End-to-End Test Suite

This directory contains comprehensive end-to-end tests for the AWS provider scanning pipeline.

## Test Files

### Core End-to-End Tests
- **`end_to_end_pipeline_test.go`** - Complete pipeline testing from discovery to scanning
- **`scanner_integration_test.go`** - Detailed testing of UnifiedScanner components
- **`provider_workflow_integration_test.go`** - Full provider workflow testing
- **`performance_benchmark_test.go`** - Performance and scalability benchmarks

### Existing Tests
- **`integration_test.go`** - Basic pipeline integration tests
- **`unified_scanner_unit_test.go`** - Unit tests for UnifiedScanner
- **`migration_performance_benchmark_test.go`** - Migration performance tests

## Running the Tests

### Prerequisites

1. **AWS Credentials**: Ensure AWS credentials are configured
   ```bash
   aws configure
   # OR set environment variables
   export AWS_ACCESS_KEY_ID=your_key
   export AWS_SECRET_ACCESS_KEY=your_secret
   export AWS_REGION=us-east-1
   ```

2. **Test Environment Variables**:
   ```bash
   export RUN_INTEGRATION_TESTS=true    # Enable integration tests
   export RUN_BENCHMARKS=true           # Enable benchmark tests
   ```

### Running Individual Test Suites

#### Complete End-to-End Pipeline Tests
```bash
cd /home/jg/git/corkscrew/plugins/aws-provider
go test -v -tags=integration ./tests -run TestCompleteAWSProviderPipeline -timeout 10m
```

#### Discovery Pipeline Tests
```bash
go test -v -tags=integration ./tests -run TestDiscoveryPipeline -timeout 5m
```

#### Scanning Pipeline Tests
```bash
go test -v -tags=integration ./tests -run TestScanningPipeline -timeout 5m
```

#### Provider Workflow Tests
```bash
go test -v -tags=integration ./tests -run TestCompleteProviderWorkflow -timeout 15m
```

#### Scanner Component Tests
```bash
go test -v -tags=integration ./tests -run TestUnifiedScannerComponentIntegration -timeout 5m
```

### Running Performance Benchmarks

#### Complete Pipeline Benchmarks
```bash
go test -v -tags=integration -bench=BenchmarkCompleteProviderPipeline ./tests -benchtime=5s
```

#### Concurrent Operations Benchmarks
```bash
go test -v -tags=integration -bench=BenchmarkConcurrentOperations ./tests -benchtime=5s
```

#### Memory Usage Benchmarks
```bash
go test -v -tags=integration -bench=BenchmarkMemoryUsage ./tests -benchtime=3s
```

#### Scalability Tests
```bash
go test -v -tags=integration -bench=BenchmarkScalabilityTests ./tests -benchtime=10s
```

### Running All Tests

#### All Integration Tests
```bash
go test -v -tags=integration ./tests -timeout 30m
```

#### All Benchmarks
```bash
go test -v -tags=integration -bench=. ./tests -benchtime=5s -timeout 30m
```

### Running Tests with Coverage

```bash
go test -v -tags=integration -coverprofile=coverage.out ./tests -timeout 20m
go tool cover -html=coverage.out -o coverage.html
```

## Test Categories

### 1. Discovery Pipeline Tests (`TestDiscoveryPipeline`)
- **Runtime Service Discovery**: Tests dynamic service discovery
- **Service Analysis Generation**: Tests analysis file generation
- **Service Registry Integration**: Tests service metadata handling

### 2. Scanning Pipeline Tests (`TestScanningPipeline`)
- **UnifiedScanner Integration**: Tests core scanning functionality
- **Runtime Pipeline Integration**: Tests pipeline orchestration
- **Resource Enrichment**: Tests detailed configuration collection

### 3. Complete Provider Workflow (`TestCompleteProviderWorkflow`)
- **Provider Lifecycle**: Tests full initialization to cleanup cycle
- **Service Discovery**: Tests service enumeration
- **Resource Operations**: Tests listing, scanning, and description
- **Streaming Operations**: Tests real-time resource streaming
- **Schema Operations**: Tests database schema generation
- **Advanced Features**: Tests discovery configuration and analysis

### 4. Performance and Reliability (`TestPerformanceAndReliability`)
- **Concurrent Scanning**: Tests multi-threaded operations
- **Error Handling**: Tests resilience and error recovery
- **Resource Explorer Fallback**: Tests SDK fallback mechanisms

### 5. Data Integrity (`TestDataIntegrity`)
- **Resource Reference Consistency**: Tests data consistency
- **Configuration Data Validation**: Tests data quality

## Performance Benchmarks

### Core Operations
- **Provider Initialization**: Measures startup time
- **Service Discovery**: Measures discovery performance
- **Resource Listing**: Measures single service scanning
- **Batch Scanning**: Measures multi-service scanning
- **Resource Description**: Measures configuration collection

### Scalability Tests
- **Concurrent Operations**: Tests parallel operation performance
- **Memory Usage**: Tests memory efficiency and leak detection
- **Resource Volume Handling**: Tests large dataset processing
- **Error Handling Overhead**: Tests error processing efficiency

### Advanced Benchmarks
- **Detailed Metrics**: Provides comprehensive performance statistics
- **Scalability with Service Count**: Tests performance with increasing services
- **Streaming Performance**: Tests real-time data processing

## Test Environment Setup

### Minimal AWS Resources Required

The tests are designed to work with minimal AWS resources:

1. **S3**: At least one S3 bucket (most reliable for testing)
2. **EC2**: Basic EC2 access (instances optional)
3. **IAM**: Basic IAM permissions for listing
4. **Lambda**: Function listing access (functions optional)

### Required AWS Permissions

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:ListAllMyBuckets",
                "s3:GetBucketLocation",
                "s3:GetBucketPolicy",
                "s3:GetBucketVersioning",
                "s3:GetBucketEncryption",
                "s3:GetBucketLogging",
                "s3:GetPublicAccessBlock",
                "ec2:DescribeInstances",
                "ec2:DescribeVolumes",
                "ec2:DescribeSecurityGroups",
                "ec2:DescribeVpcs",
                "lambda:ListFunctions",
                "iam:ListUsers",
                "iam:ListRoles",
                "resource-explorer-2:Search",
                "resource-explorer-2:ListViews",
                "resource-explorer-2:GetView"
            ],
            "Resource": "*"
        }
    ]
}
```

## Test Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `RUN_INTEGRATION_TESTS` | Enable integration tests | `false` |
| `RUN_BENCHMARKS` | Enable benchmark tests | `false` |
| `AWS_REGION` | AWS region for testing | `us-east-1` |
| `AWS_ACCESS_KEY_ID` | AWS access key | - |
| `AWS_SECRET_ACCESS_KEY` | AWS secret key | - |

### Test Timeouts

- **Unit tests**: 30 seconds
- **Integration tests**: 5-15 minutes
- **Full pipeline tests**: 30 minutes
- **Benchmarks**: 5-30 minutes

## Troubleshooting

### Common Issues

1. **AWS Credentials Not Found**
   ```
   Solution: Configure AWS credentials using `aws configure` or environment variables
   ```

2. **Permission Denied Errors**
   ```
   Solution: Ensure IAM user has required permissions (see above)
   ```

3. **No Resources Found**
   ```
   Solution: Tests gracefully handle empty results, but some tests may skip
   ```

4. **Timeout Errors**
   ```
   Solution: Increase timeout or check network connectivity
   ```

### Test Debugging

Enable verbose output:
```bash
go test -v -tags=integration ./tests -run TestName
```

Run specific subtests:
```bash
go test -v -tags=integration ./tests -run TestCompleteProviderWorkflow/ProviderLifecycle/Phase1_ProviderInfo
```

## Contributing

When adding new tests:

1. Use the `integration` build tag for integration tests
2. Include proper timeout handling
3. Add graceful error handling for missing resources
4. Include performance benchmarks for new features
5. Update this README with new test descriptions

## Test Coverage

The test suite covers:

- ✅ Provider initialization and cleanup
- ✅ Service discovery (runtime and cached)
- ✅ Resource scanning (single and batch)
- ✅ Resource description and enrichment
- ✅ Streaming operations
- ✅ Schema generation
- ✅ Error handling and resilience
- ✅ Concurrent operations
- ✅ Performance characteristics
- ✅ Memory usage patterns
- ✅ Data integrity and consistency

## Performance Baselines

Typical performance expectations (may vary by AWS account and region):

- **Provider Initialization**: < 10 seconds
- **Service Discovery**: < 5 seconds (cached), < 30 seconds (fresh)
- **S3 Resource Listing**: < 10 seconds for 100 buckets
- **Resource Description**: < 2 seconds per resource
- **Batch Scanning**: < 30 seconds for 2-3 services
- **Memory Usage**: < 100MB for typical workloads
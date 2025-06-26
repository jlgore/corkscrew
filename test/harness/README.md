# Corkscrew Test Harness

A comprehensive integration testing framework for Corkscrew that uses Pulumi Automation API to deploy real cloud resources, scan them with Corkscrew, and verify the results in DuckDB.

## Overview

This test harness provides end-to-end testing by:

1. **Deploy Phase**: Using Pulumi to deploy real AWS resources (S3 buckets)
2. **Scan Phase**: Running Corkscrew to discover and catalog the deployed resources
3. **Verify Phase**: Checking that resources are correctly stored in DuckDB with valid raw data
4. **Cleanup Phase**: Automatically destroying all deployed resources

## Architecture

```
┌─────────────────────┐
│   Pulumi Automation │  Deploy real AWS resources
│   API (Go)          │  with test tags
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│   Corkscrew Scan    │  Discover and catalog
│   (Binary Exec)     │  deployed resources
└──────────┬──────────┘
           │
┌──────────▼──────────┐
│   DuckDB Verifier   │  Verify raw_data storage
│   (SQL Queries)     │  and JSON validity
└─────────────────────┘
```

## Phase 1: Foundation (Current Implementation)

### Features
- ✅ **S3 Bucket deployment** with Pulumi Automation API
- ✅ **Corkscrew integration** via binary execution
- ✅ **DuckDB verification** of raw data storage
- ✅ **Automatic cleanup** with error handling
- ✅ **Comprehensive logging** for debugging

### Test Flow
1. Deploy S3 bucket with unique test tags
2. Wait for resource stabilization (30s)
3. Execute `corkscrew discover --provider aws --services s3`
4. Verify bucket appears in scan output
5. Query DuckDB to verify raw_data storage
6. Validate JSON structure and content
7. Clean up all resources

## Quick Start

### Prerequisites

1. **Pulumi CLI** installed and configured
   ```bash
   curl -fsSL https://get.pulumi.com | sh
   ```

2. **AWS CLI** configured with valid credentials
   ```bash
   aws configure
   # OR set environment variables:
   # export AWS_ACCESS_KEY_ID=...
   # export AWS_SECRET_ACCESS_KEY=...
   ```

3. **Corkscrew binary** built in the project root
   ```bash
   cd /path/to/corkscrew
   make build
   ```

### Running Tests

#### Option 1: Quick Test (Recommended)
```bash
cd test/harness
./run_test.sh
```

#### Option 2: Direct Go Test
```bash
cd test/harness
go test -v -timeout=10m
```

#### Option 3: Short Test (Skip integration)
```bash
cd test/harness
go test -short
```

### Expected Output

```
=== RUN   TestS3BucketDeployment
Phase 1: Deploying S3 bucket...
🚀 Deploying test infrastructure for test-1697123456-us-east-1-1697123456...
✅ Deployment complete in 45.2s
✅ S3 bucket created successfully:
   Name: corkscrew-test-test-1697123456-simple
   ARN:  arn:aws:s3:::corkscrew-test-test-1697123456-simple

Phase 2: Waiting for resource stabilization...

Phase 3: Running Corkscrew scan...
Corkscrew scan completed with exit code: 0
Scan output length: 2048 characters
✅ Bucket corkscrew-test-test-1697123456-simple found in scan output

Phase 4: Verifying data in DuckDB...
Database stats: map[s3_buckets:1 test_resources:1 total_resources:1]
Verification result: Found 1 buckets
Bucket 1: Name=corkscrew-test-test-1697123456-simple, ARN=arn:aws:s3:::corkscrew-test-test-1697123456-simple, HasRawData=true, RawDataSize=1247, RawDataValid=true
✅ Test completed successfully - full end-to-end verification passed!

🧹 Destroying test infrastructure test-1697123456-us-east-1-1697123456...
--- PASS: TestS3BucketDeployment (125.30s)
```

## Project Structure

```
test/harness/
├── automation/
│   ├── harness.go           # Pulumi automation wrapper
│   └── s3_program.go        # S3 bucket deployment program
├── verification/
│   └── duckdb_verifier.go   # DuckDB verification logic
├── integration_test.go      # Main integration test
├── run_test.sh             # Test runner script
├── go.mod                  # Go module dependencies
└── README.md               # This file
```

## Configuration

### Environment Variables

- `AWS_REGION`: AWS region for deployment (default: us-east-1)
- `PULUMI_CONFIG_PASSPHRASE`: Pulumi state encryption passphrase (default: empty)
- `PULUMI_BACKEND_URL`: Pulumi backend URL (default: file://./pulumi-state)

### Test Parameters

The test can be customized by modifying the `HarnessConfig` in `integration_test.go`:

```go
harness, err := automation.NewTestHarness(ctx, automation.HarnessConfig{
    Provider:   "aws",           // Cloud provider
    Region:     "us-east-1",     // AWS region
    Scenario:   "simple",        // Test scenario
    TestID:     testID,          // Unique test identifier
    KeepOnFail: false,          // Keep resources on test failure
    Timeout:    5 * time.Minute, // Test timeout
})
```

## Advanced Usage

### Running Specific Test Scenarios

Currently implemented:
- `simple`: Single S3 bucket with versioning and public access block

Future scenarios (Phase 2+):
- `complex`: Multiple resources with relationships
- `security`: IAM policies and encrypted resources
- `multi-region`: Resources across multiple AWS regions

### Debugging Failed Tests

1. **Enable resource preservation on failure**:
   ```go
   KeepOnFail: true,
   ```

2. **Check Pulumi stack state**:
   ```bash
   cd test/harness/pulumi-state
   pulumi stack ls
   pulumi stack select <stack-name>
   pulumi stack output
   ```

3. **Examine DuckDB directly**:
   ```bash
   sqlite3 ../../corkscrew.db
   .schema aws_resources
   SELECT * FROM aws_resources WHERE JSON_EXTRACT(tags, '$.TestHarness') = 'true';
   ```

### Manual Cleanup

If resources aren't automatically cleaned up:

```bash
cd test/harness
pulumi destroy -s <stack-name>
```

Or use AWS CLI:
```bash
aws s3 rb s3://corkscrew-test-<testid>-simple --force
```

## Troubleshooting

### Common Issues

1. **"Pulumi not found"**
   - Install Pulumi CLI: https://www.pulumi.com/docs/get-started/install/

2. **"AWS credentials not configured"**
   - Run `aws configure` or set environment variables
   - Verify with `aws sts get-caller-identity`

3. **"Corkscrew binary not found"**
   - Build the binary: `make build` in project root
   - Check path: `ls -la ../../corkscrew`

4. **"DuckDB file not found"**
   - Ensure Corkscrew has been run at least once to create the database
   - Check for `corkscrew.db` in the project root

5. **"Test timeout"**
   - Increase timeout in test configuration
   - Check AWS service limits and quotas

### Performance Optimization

- Use `testing.Short()` to skip integration tests during development
- Consider using smaller AWS regions for faster deployments
- Implement resource pooling for frequently used resources (future enhancement)

## Future Enhancements (Phase 2+)

### Multi-Resource Scenarios
- EC2 instances with security groups
- Lambda functions with IAM roles
- RDS databases with subnets

### Multi-Provider Support
- Azure Resource Manager integration
- Google Cloud Platform support
- Kubernetes cluster testing

### CI/CD Integration
- GitHub Actions workflow
- Automated PR testing
- Performance benchmarking

### Advanced Verification
- Resource relationship validation
- Schema compliance checking
- Performance regression testing

## Contributing

When adding new test scenarios:

1. Create new program functions in `automation/`
2. Add verification logic in `verification/`
3. Extend test cases in `integration_test.go`
4. Update documentation and examples

## Cost Considerations

- Each test run deploys real AWS resources (~$0.01 per run)
- Resources exist for 3-5 minutes during testing
- Automatic cleanup prevents ongoing charges
- Consider using AWS cost alerts for monitoring

## Security

- All resources are tagged with `TestHarness=true`
- Unique test IDs prevent conflicts
- Public access is blocked on S3 buckets
- Temporary credentials are recommended for CI/CD
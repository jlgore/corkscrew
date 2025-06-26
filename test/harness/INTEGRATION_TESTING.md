# Corkscrew Integration Testing Framework

This document describes the comprehensive Pulumi-based integration testing framework for Corkscrew provider plugins.

## Overview

The integration testing framework provides:

- **Automated Infrastructure Deployment**: Uses Pulumi Automation API to deploy real cloud resources
- **Real-world Testing**: Tests against actual cloud provider APIs, not mocks
- **Comprehensive Verification**: Verifies both resource discovery and relationships in DuckDB
- **Safety Mechanisms**: Cost monitoring, timeout protection, and emergency cleanup
- **CI/CD Integration**: GitHub Actions workflow with PR commenting and artifact uploads
- **Detailed Reporting**: HTML, JSON, and Markdown reports with performance metrics

## Architecture

```
┌─────────────────────┐
│   GitHub Actions    │
│   (PR Trigger)      │
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
│  - Generate Reports │
│  - Cleanup          │
└─────────────────────┘
```

## Quick Start

### Prerequisites

1. **Go 1.21+** with CGO enabled
2. **Pulumi CLI** installed and configured
3. **Cloud Provider Credentials** (AWS, Azure, or GCP)
4. **Corkscrew Binary** built (`make build`)

### Running Tests Locally

```bash
# Build Corkscrew and plugins
make build
make build-aws-plugin

# Run simple S3 test
cd test/harness
go test -v -provider=aws -scenario=simple-s3 -region=us-east-1

# Run with custom test ID and keep resources on failure
go test -v -provider=aws -scenario=complex -testid=my-test-123 -keepOnFail=true

# Run dry-run safety checks only
go test -v -provider=aws -scenario=simple-s3 -dry-run=true

# Run with timeout and account safety
go test -v -provider=aws -timeout=10m -account-id=123456789012
```

### Manual Test Execution

```bash
# Test a specific scenario
./scripts/run-integration-test.sh aws simple-s3 us-east-1

# Emergency cleanup of orphaned resources
./scripts/emergency-cleanup.sh --dry-run
./scripts/emergency-cleanup.sh gh-123-aws-simple --force
```

## Test Scenarios

### AWS Scenarios

#### `simple-s3`
- **Resources**: S3 bucket with versioning and encryption
- **Services**: S3
- **Duration**: ~3 minutes
- **Cost**: <$0.01/day

#### `complex`
- **Resources**: VPC, Security Groups, EC2 instances, Lambda functions, IAM roles
- **Services**: EC2, Lambda, IAM, S3
- **Duration**: ~5 minutes
- **Cost**: ~$0.11/day

#### `security`
- **Resources**: KMS keys, Secrets Manager, IAM policies with complex relationships
- **Services**: IAM, KMS, SecretsManager
- **Duration**: ~4 minutes
- **Cost**: ~$0.05/day

### Creating New Scenarios

1. **Create scenario file**: `scenarios/aws/my_scenario.go`
2. **Implement interface**:
   ```go
   type MyScenario struct{}
   
   func (s *MyScenario) GetName() string { return "my-scenario" }
   func (s *MyScenario) GetServices() []string { return []string{"s3", "ec2"} }
   func (s *MyScenario) DefineResources(ctx *pulumi.Context, testID string) error { /* ... */ }
   func (s *MyScenario) GetExpectedResources() map[string]interface{} { /* ... */ }
   ```
3. **Register scenario**: Add to `scenarios/registry.go`
4. **Test locally**: `go test -scenario=my-scenario`

## GitHub Actions Integration

### Workflow Triggers

The integration tests run automatically on:
- **Pull Requests** affecting provider code
- **Manual dispatch** with custom parameters
- **Scheduled runs** (optional)

### Workflow Features

- **Smart Detection**: Only tests changed providers
- **Parallel Execution**: Tests multiple scenarios simultaneously
- **Cost Monitoring**: Estimates and tracks resource costs
- **PR Comments**: Posts detailed results with metrics
- **Artifact Upload**: Saves logs, reports, and databases
- **Emergency Cleanup**: Automatic cleanup of orphaned resources

### Required Secrets

```yaml
# .github/secrets
AWS_TEST_ROLE_ARN: "arn:aws:iam::123456789012:role/GitHubActionsTestRole"
AZURE_CREDENTIALS: '{"clientId": "...", "clientSecret": "...", ...}'
GCP_SA_KEY: '{"type": "service_account", ...}'
PULUMI_ACCESS_TOKEN: "pul-xxx..."
```

### Example PR Comment

```markdown
## 🧪 Integration Test: aws/simple-s3

**Status:** ✅ PASSED  
**Duration:** 3m 24s  
**Region:** us-east-1  

**Metrics:**
- Deployment: 1m 45s
- Scan: 12s
- Resources: 3 deployed, 3 scanned, 3 verified
- Success Rate: 100.0% (3/3 resources found)

*Test ID: `gh-123-aws-simple`*
```

## Safety Mechanisms

### Pre-flight Safety Checks

- **Account Validation**: Prevents execution in production accounts
- **Cost Monitoring**: Checks current month spend against thresholds
- **Resource Leak Detection**: Identifies orphaned test resources
- **Service Quota Verification**: Ensures sufficient capacity
- **Region Capacity Checks**: Validates region availability

### Runtime Protection

- **Timeout Guards**: Automatic test termination after 15 minutes
- **Cost Alerts**: CloudWatch alarms for unexpected charges
- **Emergency Cleanup**: Graceful resource cleanup on timeout/failure
- **Orphan Detection**: Scheduled cleanup of forgotten resources

### Emergency Procedures

```bash
# Manual emergency cleanup
./scripts/emergency-cleanup.sh --help

# Clean specific test pattern
./scripts/emergency-cleanup.sh gh-123-aws --dry-run
./scripts/emergency-cleanup.sh gh-123-aws --force

# Clean all test resources
./scripts/emergency-cleanup.sh corkscrew-test --region us-east-1
```

## Verification System

### Resource Verification

The framework verifies:
- **Resource Existence**: All expected resources are discovered
- **Attribute Accuracy**: Resource properties match expectations
- **Tag Compliance**: Proper tagging for test identification
- **Relationship Discovery**: Inter-resource relationships are detected

### Database Verification

DuckDB verification includes:
- **Schema Validation**: Correct table structure
- **Data Integrity**: Raw data is valid JSON
- **Performance Metrics**: Query response times
- **Size Optimization**: Database compression ratios

### Verification Thresholds

- **Success Rate**: Must be ≥95% for test to pass
- **Attribute Accuracy**: Must be ≥80% for each resource
- **Relationship Discovery**: Expected relationships must be found
- **Performance**: Scan time must be <2x baseline

## Reporting and Monitoring

### Generated Reports

1. **HTML Report**: Comprehensive dashboard with visualizations
2. **JSON Report**: Machine-readable results for CI/CD
3. **Markdown Summary**: Human-readable summary for PRs
4. **Metrics Report**: CloudWatch-compatible performance data

### CloudWatch Metrics

Sent to `Corkscrew/IntegrationTests` namespace:
- `TestSuccess` (0/1)
- `TestDuration` (seconds)
- `ResourcesDeployed` (count)
- `ResourcesScanned` (count)
- `DatabaseSize` (bytes)
- `VerificationSuccessRate` (percentage)

### Performance Baselines

Track regression using:
```bash
# Benchmark current performance
go test -bench=. -benchmem -provider=aws -scenario=simple-s3

# Compare with previous runs
# Results stored in CloudWatch for trend analysis
```

## Troubleshooting

### Common Issues

#### Test Timeout
```
FAIL: Test timed out after 15m0s
```
**Solution**: Check AWS service health, increase timeout, or optimize scenario

#### Cost Threshold Exceeded
```
FAIL: Current month cost $6.50 exceeds threshold $5.00
```
**Solution**: Run emergency cleanup, check for orphaned resources

#### Resource Not Found
```
FAIL: Expected S3 bucket not found in database
```
**Solution**: Check Corkscrew scan logs, verify AWS permissions

#### Pulumi Destroy Failed
```
WARNING: Pulumi destroy failed but AWS cleanup succeeded
```
**Solution**: Check Pulumi state, may need manual stack cleanup

### Debug Mode

Enable verbose output:
```bash
# Verbose test output
go test -v -verbose=true

# Keep resources for inspection
go test -keepOnFail=true

# Check safety only
go test -dry-run=true
```

### Log Analysis

Important log locations:
- **Test Output**: `results/test_output.log`
- **Corkscrew Logs**: Database scan output
- **Pulumi Logs**: Infrastructure deployment logs
- **AWS CloudTrail**: API call audit trail

## Cost Management

### Cost Estimation

| Scenario | Resources | Est. Cost/Day | Duration |
|----------|-----------|---------------|----------|
| simple-s3 | S3 bucket | <$0.01 | 3 min |
| complex | VPC, EC2, Lambda | ~$0.11 | 5 min |
| security | KMS, Secrets | ~$0.05 | 4 min |

### Cost Controls

- **Automatic Cleanup**: Resources deleted after test completion
- **Timeout Protection**: Maximum 15-minute runtime
- **Cost Alerts**: CloudWatch alarms at $5 threshold
- **Emergency Cleanup**: Scheduled orphan resource cleanup
- **Account Limits**: Production account protection

### Monthly Budget

Estimated monthly testing costs:
- **PR Tests**: ~$10-20/month (depending on PR frequency)
- **Scheduled Tests**: ~$30/month (daily runs)
- **Development Testing**: ~$5-15/month (local testing)

## Contributing

### Adding New Providers

1. **Create provider directory**: `scenarios/azure/`
2. **Implement scenarios**: Following AWS examples
3. **Add to workflow**: Update `.github/workflows/provider-test.yml`
4. **Configure credentials**: Add secrets for new provider
5. **Update documentation**: Add provider-specific instructions

### Adding New Verification Types

1. **Extend verifier**: Add methods to `verification/duckdb_verifier.go`
2. **Update types**: Add new check types to `types.go`
3. **Implement in orchestrator**: Call new verification methods
4. **Add to reports**: Include in report generation

### Performance Optimization

1. **Parallel Scenarios**: Use `testing.T.Parallel()`
2. **Resource Pooling**: Reuse VPCs across tests
3. **Incremental Testing**: Only test changed components
4. **Caching**: Cache Pulumi state between runs

## Security Considerations

### Permissions

Test accounts should have:
- **Minimal Permissions**: Only what's needed for testing scenarios
- **Resource Limits**: Service quotas to prevent runaway costs
- **Time-bound Credentials**: Short-lived tokens when possible
- **Audit Logging**: All actions logged to CloudTrail

### Secrets Management

- **GitHub Secrets**: Store all credentials securely
- **Rotation**: Regular credential rotation
- **Principle of Least Privilege**: Minimal required permissions
- **Audit**: Regular access reviews

### Network Security

- **VPC Isolation**: Test resources in dedicated VPCs
- **Security Groups**: Restrictive ingress rules
- **Public Access**: Minimize public resource exposure
- **Encryption**: Enable encryption at rest and in transit

## Support

### Getting Help

1. **Documentation**: Check this README and inline code comments
2. **Issues**: Create GitHub issue with test logs and configuration
3. **Slack**: `#corkscrew-testing` channel for real-time help
4. **Email**: Send logs to `corkscrew-support@example.com`

### Reporting Bugs

Include in bug reports:
- **Test Command**: Exact command that failed
- **Environment**: OS, Go version, cloud provider
- **Logs**: Complete test output and error messages
- **Configuration**: Test parameters and settings
- **Timeline**: When the issue started occurring

### Feature Requests

For new testing features:
- **Use Case**: Describe the testing scenario
- **Requirements**: Specific functionality needed
- **Priority**: Business impact and urgency
- **Alternatives**: Current workarounds being used
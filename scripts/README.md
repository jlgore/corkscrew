# AWS Service Testing Framework

This directory contains scripts for comprehensive testing of AWS services with the resource-lister tool to identify and fix parameter validation errors.

## Scripts Overview

### 🧪 `test-all-services.sh`
Comprehensive testing script that tests all major AWS services and generates detailed reports.

**Features:**
- Tests 50+ AWS services with real API calls
- Identifies parameter validation errors, auth errors, and other issues
- Generates structured reports and summaries
- Extracts parameter errors for automatic code fixes
- Color-coded output for easy analysis

**Usage:**
```bash
# Test all services with default settings
./scripts/test-all-services.sh

# Test with custom region
AWS_REGION=us-west-2 ./scripts/test-all-services.sh

# Test with different max services limit
MAX_SERVICES=5 ./scripts/test-all-services.sh
```

**Output:**
- `test-results/` directory with individual service results
- `test_summary_TIMESTAMP.md` - Comprehensive summary report
- `parameter_errors_summary.txt` - Parameter errors ready for code fixes

### 🔧 `apply-parameter-fixes.sh`
Automatically applies parameter validation fixes to the resource-lister code based on test results.

**Features:**
- Reads parameter errors from test results
- Automatically updates `parameterizedPatterns` in the source code
- Creates backup before making changes
- Rebuilds the resource-lister binary

**Usage:**
```bash
# Apply fixes after running test-all-services.sh
./scripts/apply-parameter-fixes.sh
```

**Prerequisites:**
- Must run `test-all-services.sh` first to generate parameter errors file

### 🎯 `test-service.sh`
Quick testing script for individual AWS services with detailed error analysis.

**Features:**
- Tests a single AWS service
- Provides immediate feedback on parameter validation errors
- Shows authentication and other error types
- Extracts parameter patterns for manual fixes

**Usage:**
```bash
# Test a specific service
./scripts/test-service.sh sns
./scripts/test-service.sh cloudfront
./scripts/test-service.sh amplify

# Show full error details
SHOW_FULL_ERRORS=true ./scripts/test-service.sh dynamodb
```

## Workflow

### 1. Initial Testing
```bash
# Run comprehensive test suite
./scripts/test-all-services.sh
```

### 2. Review Results
```bash
# Check the summary report
cat test-results/test_summary_*.md

# Review parameter errors that need fixing
cat test-results/parameter_errors_summary.txt
```

### 3. Apply Fixes
```bash
# Automatically apply parameter validation fixes
./scripts/apply-parameter-fixes.sh
```

### 4. Verify Fixes
```bash
# Test specific services that had issues
./scripts/test-service.sh sns
./scripts/test-service.sh amplify

# Or run full test suite again
./scripts/test-all-services.sh
```

### 5. Iterate
Repeat the process until all parameter validation errors are resolved.

## Understanding the Output

### Parameter Validation Errors
These occur when AWS operations require specific parameters (like IDs, ARNs) that the resource-lister doesn't provide:

```
⚠️ Failed to execute ListSubscriptionsByTopic: validation error: missing required field, ListSubscriptionsByTopicInput.TopicArn
```

**Fix:** Add the operation name to `parameterizedPatterns` in `isParameterFreeOperation()`.

### Authentication Errors
These indicate AWS credential or permission issues:

```
ExpiredToken: The security token included in the request is expired
AccessDenied: User is not authorized to perform this action
```

**Fix:** Check AWS credentials, profile configuration, or IAM permissions.

### Other Execution Errors
Various other issues like service unavailability, rate limiting, etc.

## Configuration

### Environment Variables
- `AWS_REGION` - AWS region to test (default: us-east-1)
- `AWS_PROFILE` - AWS profile to use
- `MAX_SERVICES` - Limit number of services to test (default: 1)
- `DEBUG` - Enable debug output (default: true)
- `SHOW_FULL_ERRORS` - Show complete error logs (test-service.sh only)

### Customizing Service List
Edit the `SERVICES` array in `test-all-services.sh` to add/remove services:

```bash
SERVICES=(
    "s3" "ec2" "lambda" "dynamodb" "rds" "iam" "sns" "sqs"
    # Add your services here
)
```

## Troubleshooting

### Common Issues

1. **Build Failures**
   ```bash
   # Ensure Go is installed and GOPATH is set
   go version
   go build -o resource-lister ./cmd/resource-lister
   ```

2. **AWS Credential Issues**
   ```bash
   # Check AWS configuration
   aws configure list
   aws sts get-caller-identity
   ```

3. **Permission Errors**
   ```bash
   # Make scripts executable
   chmod +x scripts/*.sh
   ```

4. **Missing Dependencies**
   ```bash
   # Install required tools
   sudo apt-get install jq  # For JSON processing (if needed)
   ```

### Getting Help

- Check the generated summary reports for detailed analysis
- Use `SHOW_FULL_ERRORS=true` with `test-service.sh` for complete error logs
- Review the backup files created by `apply-parameter-fixes.sh` if issues occur

## Examples

### Testing SNS Service
```bash
$ ./scripts/test-service.sh sns
🧪 Testing AWS Service: sns
================================
Region: us-east-1
Debug: true

🔍 Running test...
Command: ./resource-lister --region us-east-1 --services sns --debug --max-services 1

✅ Test completed successfully!

📊 Results Summary:
==================
Operations discovered: 3
Resource types found: 1

Sample operations:
  • ListTopics
  • ListPlatformApplications
  • ListSMSSandboxPhoneNumbers

🔍 Error Analysis:
==================
Parameter validation errors: 15
Authentication errors: 0
Other execution errors: 0

🔧 Parameter validation errors (add these to parameterizedPatterns):
"ListSubscriptionsByTopic",  // Requires parameters
"ListEndpointsByPlatformApplication",  // Requires parameters
"GetTopicAttributes",  // Requires parameters
```

### Full Test Suite Results
```bash
$ ./scripts/test-all-services.sh
🧪 AWS Service Testing Framework
================================
Region: us-east-1
Debug: true
Output: ./test-results
Log: ./test-results/service_test_20241220_143022.log

🔨 Building resource-lister...
✅ Build complete

🚀 Starting comprehensive service testing...
Testing 50 services...

🔍 Testing service: s3
✅ s3: SUCCESS

🔍 Testing service: sns
❌ sns: FAILED
  📋 Parameter validation errors: 15

...

✅ Testing complete!

📁 Results available in: ./test-results
📊 Summary: ./test-results/test_summary_20241220_143022.md
🔧 Parameter fixes: ./test-results/parameter_errors_summary.txt

Quick Summary:
==============
✅ Successful: 35
❌ Failed: 15
🔧 Services needing parameter fixes: 12
``` 
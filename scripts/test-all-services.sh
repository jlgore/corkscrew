#!/bin/bash

# AWS Service Testing Script
# Tests all AWS services to identify parameter validation errors and other issues

set -e

# Configuration
REGION=${AWS_REGION:-us-east-1}
MAX_SERVICES=${MAX_SERVICES:-1}
DEBUG=${DEBUG:-true}
OUTPUT_DIR="./test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOG_FILE="$OUTPUT_DIR/service_test_$TIMESTAMP.log"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo "🧪 AWS Service Testing Framework"
echo "================================"
echo "Region: $REGION"
echo "Debug: $DEBUG"
echo "Output: $OUTPUT_DIR"
echo "Log: $LOG_FILE"
echo ""

# Build the corkscrew binary first
echo "🔨 Building corkscrew..."
go build -o corkscrew ./cmd/corkscrew
echo "✅ Build complete"
echo ""

# List of AWS services to test (comprehensive list)
SERVICES=(
    "s3" "ec2" "lambda" "dynamodb" "rds" "iam" "sns" "sqs"
    "cloudfront" "amplify" "apigateway" "cloudformation" "cloudwatch"
    "ecs" "eks" "elasticbeanstalk" "elasticache" "elasticsearch"
    "kinesis" "kms" "route53" "secretsmanager" "ssm" "sts"
    "autoscaling" "elb" "elbv2" "emr" "glue" "redshift"
    "stepfunctions" "xray" "batch" "codebuild" "codecommit"
    "codedeploy" "codepipeline" "cognito" "directconnect"
    "docdb" "fsx" "guardduty" "inspector" "iot" "macie"
    "organizations" "quicksight" "workspaces" "backup"
    "datasync" "transfer" "waf" "wafv2" "shield" "config"
)

# Results tracking
declare -A RESULTS
declare -A ERROR_COUNTS
declare -A PARAM_ERRORS
declare -A AUTH_ERRORS
declare -A OTHER_ERRORS

# Function to test a single service
test_service() {
    local service=$1
    local result_file="$OUTPUT_DIR/${service}_result.json"
    local error_file="$OUTPUT_DIR/${service}_errors.txt"
    
    echo -e "${BLUE}🔍 Testing service: $service${NC}"
    
    # Run the corkscrew list command and capture output
    if ./corkscrew list --region "$REGION" --services "$service" --debug > "$result_file" 2> "$error_file"; then
        RESULTS[$service]="SUCCESS"
        echo -e "${GREEN}✅ $service: SUCCESS${NC}"
    else
        RESULTS[$service]="FAILED"
        echo -e "${RED}❌ $service: FAILED${NC}"
    fi
    
    # Analyze errors
    analyze_errors "$service" "$error_file"
    
    echo ""
}

# Function to analyze errors from service output
analyze_errors() {
    local service=$1
    local error_file=$2
    
    if [[ ! -f "$error_file" ]]; then
        return
    fi
    
    # Count different types of errors (trim whitespace to avoid syntax errors)
    param_count=$(grep -c "validation error\|missing required field" "$error_file" 2>/dev/null || echo "0")
    param_count=$(echo "$param_count" | tr -d '\n\r ')
    auth_count=$(grep -c "ExpiredToken\|AccessDenied\|UnauthorizedOperation" "$error_file" 2>/dev/null || echo "0")
    auth_count=$(echo "$auth_count" | tr -d '\n\r ')
    other_count=$(grep -c "⚠️.*Failed to execute" "$error_file" 2>/dev/null || echo "0")
    other_count=$(echo "$other_count" | tr -d '\n\r ')
    
    PARAM_ERRORS[$service]=$param_count
    AUTH_ERRORS[$service]=$auth_count
    OTHER_ERRORS[$service]=$other_count
    
    if [[ $param_count -gt 0 ]]; then
        echo -e "${YELLOW}  📋 Parameter validation errors: $param_count${NC}"
        # Extract specific parameter errors
        grep -E "validation error|missing required field" "$error_file" | head -3 | while read -r line; do
            echo -e "${YELLOW}    • $(echo "$line" | sed 's/.*Failed to execute \([^:]*\).*/\1/')${NC}"
        done
    fi
    
    if [[ $auth_count -gt 0 ]]; then
        echo -e "${YELLOW}  🔐 Authentication/Authorization errors: $auth_count${NC}"
    fi
    
    if [[ $other_count -gt 0 ]]; then
        echo -e "${YELLOW}  ⚠️  Other execution errors: $other_count${NC}"
    fi
}

# Function to extract parameter validation errors for code fixes
extract_parameter_errors() {
    echo "🔍 Extracting parameter validation errors for code fixes..."
    param_error_file="$OUTPUT_DIR/parameter_errors_summary.txt"
    
    echo "# Parameter Validation Errors Found" > "$param_error_file"
    echo "# Add these to the service-specific handlers in internal/classification/service_handlers.go" >> "$param_error_file"
    echo "# or update the pattern rules in internal/classification/operation_classifier.go" >> "$param_error_file"
    echo "" >> "$param_error_file"
    
    for service in "${SERVICES[@]}"; do
        local error_file="$OUTPUT_DIR/${service}_errors.txt"
        if [[ -f "$error_file" ]] && [[ ${PARAM_ERRORS[$service]:-0} -gt 0 ]]; then
            echo "# $service service:" >> "$param_error_file"
            grep "Failed to execute" "$error_file" | grep -E "validation error|missing required field" | \
                sed 's/.*Failed to execute \([^:]*\).*/"\1",  \/\/ Requires parameters/' | \
                sort -u >> "$param_error_file"
            echo "" >> "$param_error_file"
        fi
    done
    
    echo "📝 Parameter errors extracted to: $param_error_file"
}

# Function to generate summary report
generate_summary() {
    summary_file="$OUTPUT_DIR/test_summary_$TIMESTAMP.md"
    
    echo "# AWS Service Testing Summary" > "$summary_file"
    echo "**Date:** $(date)" >> "$summary_file"
    echo "**Region:** $REGION" >> "$summary_file"
    echo "" >> "$summary_file"
    
    # Count results
    success_count=0
    failed_count=0
    total_param_errors=0
    total_auth_errors=0
    
    for service in "${SERVICES[@]}"; do
        if [[ "${RESULTS[$service]}" == "SUCCESS" ]]; then
            ((success_count++))
        else
            ((failed_count++))
        fi
        ((total_param_errors += ${PARAM_ERRORS[$service]:-0}))
        ((total_auth_errors += ${AUTH_ERRORS[$service]:-0}))
    done
    
    echo "## Overview" >> "$summary_file"
    echo "- **Total Services Tested:** ${#SERVICES[@]}" >> "$summary_file"
    echo "- **Successful:** $success_count" >> "$summary_file"
    echo "- **Failed:** $failed_count" >> "$summary_file"
    echo "- **Total Parameter Validation Errors:** $total_param_errors" >> "$summary_file"
    echo "- **Total Auth Errors:** $total_auth_errors" >> "$summary_file"
    echo "" >> "$summary_file"
    
    echo "## Services with Parameter Validation Errors" >> "$summary_file"
    echo "| Service | Param Errors | Auth Errors | Status |" >> "$summary_file"
    echo "|---------|--------------|-------------|--------|" >> "$summary_file"
    
    for service in "${SERVICES[@]}"; do
        param_count=${PARAM_ERRORS[$service]:-0}
        auth_count=${AUTH_ERRORS[$service]:-0}
        status=${RESULTS[$service]:-"NOT_TESTED"}
        
        if [[ $param_count -gt 0 ]] || [[ $auth_count -gt 0 ]] || [[ "$status" == "FAILED" ]]; then
            echo "| $service | $param_count | $auth_count | $status |" >> "$summary_file"
        fi
    done
    
    echo "" >> "$summary_file"
    echo "## Next Steps" >> "$summary_file"
    echo "1. Review parameter_errors_summary.txt for operations needing classification updates" >> "$summary_file"
    echo "2. Update service-specific handlers in internal/classification/service_handlers.go" >> "$summary_file"
    echo "3. Add new pattern rules to internal/classification/operation_classifier.go if needed" >> "$summary_file"
    echo "4. Re-run tests to verify fixes" >> "$summary_file"
    
    echo "📊 Summary report generated: $summary_file"
}

# Main execution
echo "🚀 Starting comprehensive service testing..."
echo "Testing ${#SERVICES[@]} services..."
echo ""

# Test each service
for service in "${SERVICES[@]}"; do
    test_service "$service"
    
    # Add a small delay to avoid rate limiting
    sleep 1
done

echo ""
echo "🔍 Analyzing results..."

# Extract parameter errors for code fixes
extract_parameter_errors

# Generate summary report
generate_summary

echo ""
echo "✅ Testing complete!"
echo ""
echo "📁 Results available in: $OUTPUT_DIR"
echo "📊 Summary: $OUTPUT_DIR/test_summary_$TIMESTAMP.md"
echo "🔧 Parameter fixes: $OUTPUT_DIR/parameter_errors_summary.txt"
echo ""

# Show quick summary
echo "Quick Summary:"
echo "=============="
success_count=0
failed_count=0
services_with_param_errors=0

for service in "${SERVICES[@]}"; do
    if [[ "${RESULTS[$service]}" == "SUCCESS" ]]; then
        ((success_count++))
    else
        ((failed_count++))
    fi
    
    if [[ ${PARAM_ERRORS[$service]:-0} -gt 0 ]]; then
        ((services_with_param_errors++))
    fi
done

echo "✅ Successful: $success_count"
echo "❌ Failed: $failed_count"
echo "🔧 Services needing parameter fixes: $services_with_param_errors" 
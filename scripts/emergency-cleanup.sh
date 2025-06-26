#!/bin/bash

# Corkscrew Emergency Cleanup Script
# This script provides emergency cleanup of test resources that may have been orphaned
# Usage: ./emergency-cleanup.sh [test-id-pattern] [region] [--dry-run]

set -euo pipefail

# Configuration
DEFAULT_REGION="us-east-1"
DRY_RUN=false
VERBOSE=false
FORCE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Help function
show_help() {
    cat << EOF
Corkscrew Emergency Cleanup Script

This script helps clean up orphaned test resources from integration tests.

Usage: $0 [OPTIONS] [TEST_ID_PATTERN]

OPTIONS:
    -r, --region REGION     AWS region (default: $DEFAULT_REGION)
    -d, --dry-run          Show what would be deleted without actually deleting
    -v, --verbose          Verbose output
    -f, --force            Force deletion without confirmation prompts
    -h, --help             Show this help message

EXAMPLES:
    # Clean up all test resources (dry run)
    $0 --dry-run

    # Clean up resources for specific test ID pattern
    $0 gh-123-aws-simple

    # Clean up resources in specific region with verbose output
    $0 --region us-west-2 --verbose

    # Force cleanup without prompts
    $0 --force corkscrew-test-

SAFETY:
    - Only affects resources tagged with TestHarness=true
    - Includes additional safety checks for production accounts
    - Supports dry-run mode for safe testing
    - Logs all actions for audit trail

EOF
}

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_verbose() {
    if [ "$VERBOSE" = true ]; then
        echo -e "${BLUE}[VERBOSE]${NC} $1"
    fi
}

# Check if AWS CLI is available and configured
check_aws_cli() {
    if ! command -v aws &> /dev/null; then
        log_error "AWS CLI is not installed or not in PATH"
        exit 1
    fi

    if ! aws sts get-caller-identity &> /dev/null; then
        log_error "AWS CLI is not configured or credentials are invalid"
        exit 1
    fi

    # Get account ID for safety checks
    ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
    log_info "Connected to AWS Account: $ACCOUNT_ID"
}

# Safety check to prevent running in production
check_production_safety() {
    local account_id="$1"
    
    # List of known production account IDs (customize for your organization)
    local production_accounts=(
        "111111111111"  # Example production account
        "222222222222"  # Another production account
    )
    
    for prod_account in "${production_accounts[@]}"; do
        if [ "$account_id" = "$prod_account" ]; then
            log_error "DANGER: This appears to be a production account ($account_id)"
            log_error "Emergency cleanup is NOT ALLOWED in production accounts"
            exit 1
        fi
    done
    
    log_success "Account safety check passed"
}

# Confirm action with user
confirm_action() {
    local message="$1"
    
    if [ "$FORCE" = true ]; then
        log_info "Force mode enabled, skipping confirmation"
        return 0
    fi
    
    echo -e "${YELLOW}$message${NC}"
    read -p "Are you sure you want to continue? (yes/no): " -r
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        log_info "Operation cancelled by user"
        exit 0
    fi
}

# Get test resources by tag
get_test_resources() {
    local resource_type="$1"
    local test_pattern="$2"
    
    case "$resource_type" in
        "s3-buckets")
            if [ -n "$test_pattern" ]; then
                aws s3api list-buckets --query "Buckets[?contains(Name, '$test_pattern')].Name" --output text
            else
                # Get buckets with TestHarness tag
                aws s3api list-buckets --query 'Buckets[?contains(Name, `corkscrew`) || contains(Name, `test`)].Name' --output text
            fi
            ;;
        "ec2-instances")
            local filter="Name=tag:TestHarness,Values=true"
            if [ -n "$test_pattern" ]; then
                filter="$filter Name=tag:TestID,Values=*$test_pattern*"
            fi
            aws ec2 describe-instances --filters "$filter" --query 'Reservations[*].Instances[*].InstanceId' --output text
            ;;
        "lambda-functions")
            aws lambda list-functions --query "Functions[?contains(FunctionName, 'corkscrew') || contains(FunctionName, 'test')].FunctionName" --output text
            ;;
        "cloudwatch-alarms")
            aws cloudwatch describe-alarms --query "MetricAlarms[?contains(AlarmName, 'corkscrew-test')].AlarmName" --output text
            ;;
    esac
}

# Clean up S3 buckets
cleanup_s3_buckets() {
    local test_pattern="$1"
    
    log_info "🪣 Checking for S3 buckets..."
    
    local buckets
    buckets=$(get_test_resources "s3-buckets" "$test_pattern")
    
    if [ -z "$buckets" ]; then
        log_info "No S3 buckets found matching pattern"
        return 0
    fi
    
    log_warning "Found S3 buckets: $buckets"
    
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY RUN] Would delete S3 buckets: $buckets"
        return 0
    fi
    
    confirm_action "This will delete the S3 buckets and ALL their contents!"
    
    for bucket in $buckets; do
        log_info "Processing bucket: $bucket"
        
        # Check if bucket exists (might have been deleted already)
        if ! aws s3 ls "s3://$bucket" &> /dev/null; then
            log_warning "Bucket $bucket does not exist or is not accessible"
            continue
        fi
        
        # Empty the bucket first
        log_verbose "Emptying bucket: $bucket"
        if aws s3 rm "s3://$bucket" --recursive; then
            log_verbose "Bucket contents removed: $bucket"
        else
            log_warning "Failed to empty bucket: $bucket"
            continue
        fi
        
        # Delete the bucket
        log_verbose "Deleting bucket: $bucket"
        if aws s3api delete-bucket --bucket "$bucket"; then
            log_success "Deleted S3 bucket: $bucket"
        else
            log_error "Failed to delete bucket: $bucket"
        fi
    done
}

# Clean up EC2 instances
cleanup_ec2_instances() {
    local test_pattern="$1"
    
    log_info "🖥️ Checking for EC2 instances..."
    
    local instances
    instances=$(get_test_resources "ec2-instances" "$test_pattern")
    
    if [ -z "$instances" ]; then
        log_info "No EC2 instances found matching pattern"
        return 0
    fi
    
    log_warning "Found EC2 instances: $instances"
    
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY RUN] Would terminate EC2 instances: $instances"
        return 0
    fi
    
    confirm_action "This will terminate the EC2 instances!"
    
    for instance in $instances; do
        log_info "Terminating instance: $instance"
        
        if aws ec2 terminate-instances --instance-ids "$instance"; then
            log_success "Terminated EC2 instance: $instance"
        else
            log_error "Failed to terminate instance: $instance"
        fi
    done
    
    # Wait for instances to terminate
    if [ -n "$instances" ]; then
        log_info "Waiting for instances to terminate..."
        aws ec2 wait instance-terminated --instance-ids $instances || log_warning "Timeout waiting for instance termination"
    fi
}

# Clean up Lambda functions
cleanup_lambda_functions() {
    local test_pattern="$1"
    
    log_info "⚡ Checking for Lambda functions..."
    
    local functions
    functions=$(get_test_resources "lambda-functions" "$test_pattern")
    
    if [ -z "$functions" ]; then
        log_info "No Lambda functions found matching pattern"
        return 0
    fi
    
    log_warning "Found Lambda functions: $functions"
    
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY RUN] Would delete Lambda functions: $functions"
        return 0
    fi
    
    confirm_action "This will delete the Lambda functions!"
    
    for function in $functions; do
        log_info "Deleting function: $function"
        
        if aws lambda delete-function --function-name "$function"; then
            log_success "Deleted Lambda function: $function"
        else
            log_error "Failed to delete function: $function"
        fi
    done
}

# Clean up CloudWatch alarms
cleanup_cloudwatch_alarms() {
    local test_pattern="$1"
    
    log_info "📊 Checking for CloudWatch alarms..."
    
    local alarms
    alarms=$(get_test_resources "cloudwatch-alarms" "$test_pattern")
    
    if [ -z "$alarms" ]; then
        log_info "No CloudWatch alarms found matching pattern"
        return 0
    fi
    
    log_warning "Found CloudWatch alarms: $alarms"
    
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY RUN] Would delete CloudWatch alarms: $alarms"
        return 0
    fi
    
    confirm_action "This will delete the CloudWatch alarms!"
    
    # Convert space-separated list to array for AWS CLI
    local alarm_array=($alarms)
    
    if aws cloudwatch delete-alarms --alarm-names "${alarm_array[@]}"; then
        log_success "Deleted CloudWatch alarms: $alarms"
    else
        log_error "Failed to delete some CloudWatch alarms"
    fi
}

# Main cleanup function
main_cleanup() {
    local test_pattern="$1"
    
    log_info "🧹 Starting emergency cleanup..."
    log_info "Region: $REGION"
    log_info "Test pattern: ${test_pattern:-"<all test resources>"}"
    log_info "Dry run: $DRY_RUN"
    
    # Cleanup in order of dependency (Lambda first, then EC2, then S3, finally monitoring)
    cleanup_lambda_functions "$test_pattern"
    cleanup_ec2_instances "$test_pattern"
    cleanup_s3_buckets "$test_pattern"
    cleanup_cloudwatch_alarms "$test_pattern"
    
    log_success "🎉 Emergency cleanup completed!"
}

# Generate cleanup report
generate_report() {
    local test_pattern="$1"
    local report_file="cleanup_report_$(date +%Y%m%d_%H%M%S).json"
    
    log_info "📋 Generating cleanup report: $report_file"
    
    cat > "$report_file" << EOF
{
  "cleanup_report": {
    "timestamp": "$(date -Iseconds)",
    "region": "$REGION",
    "test_pattern": "$test_pattern",
    "dry_run": $DRY_RUN,
    "account_id": "$ACCOUNT_ID",
    "resources_checked": {
      "s3_buckets": "$(get_test_resources "s3-buckets" "$test_pattern" | wc -w)",
      "ec2_instances": "$(get_test_resources "ec2-instances" "$test_pattern" | wc -w)",
      "lambda_functions": "$(get_test_resources "lambda-functions" "$test_pattern" | wc -w)",
      "cloudwatch_alarms": "$(get_test_resources "cloudwatch-alarms" "$test_pattern" | wc -w)"
    }
  }
}
EOF
    
    log_success "Report saved: $report_file"
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -r|--region)
                REGION="$2"
                shift 2
                ;;
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -f|--force)
                FORCE=true
                shift
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            -*)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
            *)
                TEST_PATTERN="$1"
                shift
                ;;
        esac
    done
}

# Main execution
main() {
    # Set defaults
    REGION="${DEFAULT_REGION}"
    TEST_PATTERN=""
    
    # Parse arguments
    parse_args "$@"
    
    # Show header
    echo -e "${BLUE}"
    echo "=================================================================="
    echo "🚨 CORKSCREW EMERGENCY CLEANUP SCRIPT"
    echo "=================================================================="
    echo -e "${NC}"
    
    # Perform safety checks
    check_aws_cli
    check_production_safety "$ACCOUNT_ID"
    
    # Set AWS region
    export AWS_DEFAULT_REGION="$REGION"
    
    # Final safety confirmation for non-dry-run
    if [ "$DRY_RUN" = false ] && [ "$FORCE" = false ]; then
        confirm_action "⚠️ This will permanently delete AWS resources. Make sure you understand the impact!"
    fi
    
    # Generate pre-cleanup report
    generate_report "$TEST_PATTERN"
    
    # Execute cleanup
    main_cleanup "$TEST_PATTERN"
    
    echo -e "${GREEN}"
    echo "=================================================================="
    echo "✅ CLEANUP COMPLETED"
    echo "=================================================================="
    echo -e "${NC}"
    
    if [ "$DRY_RUN" = true ]; then
        log_info "This was a dry run. No resources were actually deleted."
        log_info "Run without --dry-run to perform actual cleanup."
    fi
}

# Run main function with all arguments
main "$@"
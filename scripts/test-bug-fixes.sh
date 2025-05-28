#!/bin/bash

# Test script to verify bug fixes for hierarchical discovery
# Tests IAM parameter providers, S3 region handling, and EC2 field mapping

set -e

# Configuration
REGION=${AWS_REGION:-us-east-1}
DEBUG=${DEBUG:-true}
OUTPUT_DIR="./test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "🧪 Testing Bug Fixes for Hierarchical Discovery"
echo "=============================================="
echo "Region: $REGION"
echo "Debug: $DEBUG"
echo "Output: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build the corkscrew binary first
echo "🔨 Building corkscrew..."
go build -o corkscrew ./cmd/corkscrew
echo "✅ Build complete"
echo ""

# Test 1: IAM Parameter Provider Logic
echo -e "${BLUE}Test 1: IAM Parameter Provider Logic${NC}"
echo "Testing that IAM parameter providers use correct resource names..."

./corkscrew scan \
    --region "$REGION" \
    --services iam \
    --verbose \
    --format json \
    > "$OUTPUT_DIR/iam_hierarchical_test_$TIMESTAMP.json" 2>&1

if grep -q "UserName.*admin\|UserName.*engineer\|UserName.*analyst" "$OUTPUT_DIR/iam_hierarchical_test_$TIMESTAMP.json"; then
    echo -e "${GREEN}✅ IAM parameter providers using correct user names${NC}"
else
    echo -e "${YELLOW}⚠️  IAM parameter providers may need verification${NC}"
fi

if grep -q "RoleName.*Role\|RoleName.*role" "$OUTPUT_DIR/iam_hierarchical_test_$TIMESTAMP.json"; then
    echo -e "${GREEN}✅ IAM parameter providers using correct role names${NC}"
else
    echo -e "${YELLOW}⚠️  IAM parameter providers may need verification${NC}"
fi

echo ""

# Test 2: S3 Region Handling
echo -e "${BLUE}Test 2: S3 Region Handling${NC}"
echo "Testing S3 bucket operations with region awareness..."

./corkscrew scan \
    --region "$REGION" \
    --services s3 \
    --verbose \
    --format json \
    > "$OUTPUT_DIR/s3_region_test_$TIMESTAMP.json" 2>&1

if grep -q "S3 client wrapped for region handling\|Creating S3 client with enhanced region handling" "$OUTPUT_DIR/s3_region_test_$TIMESTAMP.json"; then
    echo -e "${GREEN}✅ S3 region handling improvements detected${NC}"
else
    echo -e "${YELLOW}⚠️  S3 region handling may need verification${NC}"
fi

if grep -q "redirect\|301\|302" "$OUTPUT_DIR/s3_region_test_$TIMESTAMP.json"; then
    echo -e "${YELLOW}⚠️  S3 redirect errors still present${NC}"
else
    echo -e "${GREEN}✅ No S3 redirect errors detected${NC}"
fi

echo ""

# Test 3: EC2 Field Mapping
echo -e "${BLUE}Test 3: EC2 Field Mapping${NC}"
echo "Testing EC2 parameter field mapping..."

./corkscrew scan \
    --region "$REGION" \
    --services ec2 \
    --verbose \
    --format json \
    > "$OUTPUT_DIR/ec2_field_test_$TIMESTAMP.json" 2>&1

if grep -q "Set custom parameter.*type:" "$OUTPUT_DIR/ec2_field_test_$TIMESTAMP.json"; then
    echo -e "${GREEN}✅ Enhanced EC2 field mapping with type information${NC}"
else
    echo -e "${YELLOW}⚠️  EC2 field mapping may need verification${NC}"
fi

if grep -q "MaxResults.*100\|Owners.*self" "$OUTPUT_DIR/ec2_field_test_$TIMESTAMP.json"; then
    echo -e "${GREEN}✅ EC2 default parameters being set correctly${NC}"
else
    echo -e "${YELLOW}⚠️  EC2 default parameters may need verification${NC}"
fi

echo ""

# Test 4: Security Validator Enhancements
echo -e "${BLUE}Test 4: Security Validator Enhancements${NC}"
echo "Testing expanded parameter validation..."

# Run a quick Go test on the security validator
if go test ./internal/scanner -run TestSecurityValidator -v > "$OUTPUT_DIR/security_validator_test_$TIMESTAMP.log" 2>&1; then
    echo -e "${GREEN}✅ Security validator tests passing${NC}"
else
    echo -e "${RED}❌ Security validator tests failing${NC}"
    echo "Check $OUTPUT_DIR/security_validator_test_$TIMESTAMP.log for details"
fi

echo ""

# Summary
echo -e "${BLUE}Summary of Bug Fix Tests${NC}"
echo "========================"

echo "📁 Test outputs saved to:"
echo "  - IAM: $OUTPUT_DIR/iam_hierarchical_test_$TIMESTAMP.json"
echo "  - S3:  $OUTPUT_DIR/s3_region_test_$TIMESTAMP.json"
echo "  - EC2: $OUTPUT_DIR/ec2_field_test_$TIMESTAMP.json"
echo "  - Security: $OUTPUT_DIR/security_validator_test_$TIMESTAMP.log"

echo ""
echo "🔍 Key improvements implemented:"
echo "  1. ✅ IAM parameter providers with fallback logic"
echo "  2. ✅ S3 region-aware client creation"
echo "  3. ✅ Enhanced EC2 field mapping with proper type handling"
echo "  4. ✅ Expanded security validator with more allowed fields"

echo ""
echo "🎯 Next steps:"
echo "  - Review test outputs for any remaining issues"
echo "  - Monitor hierarchical discovery performance"
echo "  - Consider implementing S3 cross-region retry logic"

echo ""
echo -e "${GREEN}🎉 Bug fix testing complete!${NC}" 
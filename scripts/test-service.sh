#!/bin/bash

# Quick test script for individual AWS services
# Usage: ./scripts/test-service.sh <service-name>

set -e

SERVICE=$1
REGION=${AWS_REGION:-us-east-1}
DEBUG=${DEBUG:-true}

if [[ -z "$SERVICE" ]]; then
    echo "Usage: $0 <service-name>"
    echo "Example: $0 sns"
    echo "Example: $0 cloudfront"
    exit 1
fi

echo "🧪 Testing AWS Service: $SERVICE"
echo "================================"
echo "Region: $REGION"
echo "Debug: $DEBUG"
echo ""

# Build if needed
if [[ ! -f "./corkscrew" ]] || [[ "./cmd/corkscrew/main.go" -nt "./corkscrew" ]]; then
    echo "🔨 Building corkscrew..."
    go build -o corkscrew ./cmd/corkscrew
    echo "✅ Build complete"
    echo ""
fi

# Create temp files for output
TEMP_DIR=$(mktemp -d)
OUTPUT_FILE="$TEMP_DIR/output.json"
ERROR_FILE="$TEMP_DIR/errors.txt"

echo "🔧 Debug: Using temp directory: $TEMP_DIR"

echo "🔍 Running test..."
echo "Command: ./corkscrew list --region $REGION --services $SERVICE --debug"
echo ""

# Run the test
if ./corkscrew list --region "$REGION" --services "$SERVICE" --debug > "$OUTPUT_FILE" 2> "$ERROR_FILE"; then
    echo "✅ Test completed successfully!"
    
    # Show summary from output
    if [[ -f "$OUTPUT_FILE" ]]; then
        echo ""
        echo "📊 Results Summary:"
        echo "=================="
        
        # Extract key metrics
        operations=$(grep -c '"Name":' "$OUTPUT_FILE" 2>/dev/null || echo "0")
        resources=$(grep -c '"Resources":' "$OUTPUT_FILE" 2>/dev/null || echo "0")
        
        echo "Operations discovered: $operations"
        echo "Resource types found: $resources"
        
        # Show first few operations
        echo ""
        echo "Sample operations:"
        grep '"Name":' "$OUTPUT_FILE" | head -5 | sed 's/.*"Name": *"\([^"]*\)".*/  • \1/'
    fi
else
    echo "❌ Test failed!"
fi

# Analyze errors (check error file only to avoid double counting)
if [[ -f "$ERROR_FILE" ]] && [[ -s "$ERROR_FILE" ]]; then
    echo ""
    echo "🔍 Error Analysis:"
    echo "=================="
    
    # Count different types of errors
    param_errors=$(grep -c "validation error\|missing required field" "$ERROR_FILE" 2>/dev/null || echo "0")
    auth_errors=$(grep -c "ExpiredToken\|AccessDenied\|UnauthorizedOperation" "$ERROR_FILE" 2>/dev/null || echo "0")
    other_errors=$(grep -c "Failed to execute" "$ERROR_FILE" 2>/dev/null || echo "0")
    
    echo "Parameter validation errors: $param_errors"
    echo "Authentication errors: $auth_errors"
    echo "Other execution errors: $other_errors"
    
    if [[ $param_errors -gt 0 ]]; then
        echo ""
        echo "🔧 Parameter validation errors (add these to parameterizedPatterns):"
        grep "Failed to execute" "$ERROR_FILE" | grep -E "validation error|missing required field" | \
            sed 's/.*Failed to execute \([^:]*\).*/"\1",  \/\/ Requires parameters/' | \
            sort -u | head -10
    fi
    
    if [[ $auth_errors -gt 0 ]]; then
        echo ""
        echo "🔐 Authentication errors:"
        grep "ExpiredToken\|AccessDenied\|UnauthorizedOperation" "$ERROR_FILE" | head -3
    fi
    
    # Show full error log if requested
    if [[ "$SHOW_FULL_ERRORS" == "true" ]]; then
        echo ""
        echo "📋 Full error log:"
        echo "=================="
        cat "$ERROR_FILE"
    fi
fi

echo ""
echo "💡 Tips:"
echo "- Set SHOW_FULL_ERRORS=true to see complete error output"
echo "- Use --max-services to limit testing scope"
echo "- Check AWS credentials if seeing auth errors"

# Cleanup (skip if debugging)
if [[ "$DEBUG_KEEP_FILES" != "true" ]]; then
    rm -rf "$TEMP_DIR"
else
    echo "🔧 Debug: Temp files kept in: $TEMP_DIR"
fi 
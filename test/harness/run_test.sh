#!/bin/bash

# Corkscrew Integration Test Runner
# This script runs the Pulumi-based integration tests for Corkscrew

set -e

echo "🚀 Starting Corkscrew Integration Test..."
echo "================================================"

# Check prerequisites
echo "📋 Checking prerequisites..."

# Check if Pulumi is installed
if ! command -v pulumi &> /dev/null; then
    echo "❌ Pulumi is not installed. Please install Pulumi CLI first."
    echo "   Visit: https://www.pulumi.com/docs/get-started/install/"
    exit 1
fi

# Check if AWS CLI is configured
if ! aws sts get-caller-identity &> /dev/null; then
    echo "❌ AWS CLI is not configured or credentials are invalid."
    echo "   Please run 'aws configure' or set AWS environment variables."
    exit 1
fi

# Check if corkscrew binary exists
CORKSCREW_PATH="../../corkscrew"
if [[ ! -f "$CORKSCREW_PATH" ]]; then
    echo "❌ Corkscrew binary not found at $CORKSCREW_PATH"
    echo "   Please build corkscrew first: make build"
    exit 1
fi

echo "✅ All prerequisites met!"
echo ""

# Set test environment
export PULUMI_CONFIG_PASSPHRASE=""
export PULUMI_BACKEND_URL="file://./pulumi-state"

# Clean up any previous state
if [[ -d "./pulumi-state" ]]; then
    echo "🧹 Cleaning up previous Pulumi state..."
    rm -rf ./pulumi-state
fi

# Run the test
echo "🧪 Running integration test..."
echo "   - This will deploy real AWS resources"
echo "   - Resources will be automatically cleaned up"
echo "   - Test duration: ~3-5 minutes"
echo ""

# Run with verbose output
go test -v -timeout=10m ./...

echo ""
echo "🎉 Integration test completed!"
echo "================================================"
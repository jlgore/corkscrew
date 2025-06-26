#!/bin/bash
set -euo pipefail

# Test workflows with real execution (not dry-run)
echo "🚀 Testing GitHub Workflows with Real Execution"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}$1${NC}"
    echo "=================================================="
}

print_step() {
    echo -e "${YELLOW}▶️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Check if gh CLI is authenticated
check_gh_auth() {
    if ! gh auth status >/dev/null 2>&1; then
        print_error "GitHub CLI not authenticated. Run: gh auth login"
        exit 1
    fi
    print_success "GitHub CLI authenticated"
}

# Test the offline workflow (no external dependencies)
test_offline_workflow() {
    print_header "🧪 Testing Offline Workflow (Real Execution)"
    
    print_step "Running test-offline.yml with real execution"
    
    # This workflow doesn't use external GitHub actions, so it should work fully
    if timeout 600s act -W .github/workflows/test-offline.yml \
        -s GITHUB_TOKEN="$(gh auth token)" \
        -s GITHUB_REPOSITORY="jlgore/corkscrew" \
        -s GITHUB_REPOSITORY_OWNER="jlgore"; then
        print_success "Offline workflow executed successfully"
    else
        print_error "Offline workflow execution failed"
    fi
}

# Test specific job with real execution
test_build_job() {
    print_header "🔧 Testing Build Job (Real Execution - Limited)"
    
    print_step "Running test job with limited execution"
    echo "Note: This may fail at GitHub Actions steps but will test our build logic"
    
    # Run with a shorter timeout since it may hang on GitHub Actions downloads
    if timeout 180s act -j test \
        -s GITHUB_TOKEN="$(gh auth token)" \
        -s GITHUB_REPOSITORY="jlgore/corkscrew" \
        -s GITHUB_REPOSITORY_OWNER="jlgore" 2>&1 | tee /tmp/act-test.log; then
        print_success "Build job completed"
    else
        print_error "Build job failed or timed out (expected)"
        echo "Check /tmp/act-test.log for details"
    fi
    
    # Show what was actually executed
    if grep -q "Success" /tmp/act-test.log; then
        echo "Steps that succeeded:"
        grep "Success" /tmp/act-test.log | tail -5
    fi
}

# Test provider workflow detection
test_provider_detection() {
    print_header "🔍 Testing Provider Detection"
    
    print_step "Running detect-changes job"
    
    if timeout 120s act -j detect-changes \
        -s GITHUB_TOKEN="$(gh auth token)" \
        --input provider=aws \
        --input scenario=simple; then
        print_success "Provider detection job completed"
    else
        print_error "Provider detection failed"
    fi
}

# Show comprehensive test options
show_test_options() {
    print_header "💡 Additional Testing Options"
    
    echo "Manual testing commands:"
    echo ""
    echo "1. Test offline workflow (fully working):"
    echo "   act -W .github/workflows/test-offline.yml -s GITHUB_TOKEN=\"\$(gh auth token)\""
    echo ""
    echo "2. Test specific job:"
    echo "   act -j test -s GITHUB_TOKEN=\"\$(gh auth token)\""
    echo ""
    echo "3. Test provider detection:"
    echo "   act -j detect-changes -s GITHUB_TOKEN=\"\$(gh auth token)\" --input provider=aws"
    echo ""
    echo "4. Test workflow with specific event:"
    echo "   act push -W .github/workflows/build-and-publish.yml -s GITHUB_TOKEN=\"\$(gh auth token)\""
    echo ""
    echo "5. Test with verbose output:"
    echo "   act -j test -s GITHUB_TOKEN=\"\$(gh auth token)\" --verbose"
    echo ""
    echo "6. Test without pulling Docker images:"
    echo "   act -j test -s GITHUB_TOKEN=\"\$(gh auth token)\" --pull=false"
}

# Main execution
main() {
    echo "🚀 Starting real workflow execution tests..."
    echo "Warning: These tests will actually run Docker containers and execute code"
    echo ""
    
    check_gh_auth
    echo ""
    
    # Test the fully offline workflow first
    test_offline_workflow
    echo ""
    
    # Test provider detection (should work)
    test_provider_detection
    echo ""
    
    # Test build job (may have limitations)
    test_build_job
    echo ""
    
    show_test_options
    
    print_success "Real workflow testing completed!"
}

# Handle script interruption
trap 'echo -e "\n${RED}❌ Testing interrupted${NC}"; exit 1' INT TERM

# Run main function
main "$@"
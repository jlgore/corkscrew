#!/bin/bash
set -euo pipefail

# Test the basic build steps that workflows would run
echo "🧪 Testing basic build steps locally (mimicking GitHub workflow)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_step() {
    echo -e "${YELLOW}▶️  $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Function to run a command and check exit code
run_and_check() {
    local description="$1"
    local command="$2"
    
    print_step "$description"
    echo "Command: $command"
    
    if eval "$command"; then
        print_success "$description completed"
    else
        print_error "$description failed"
        return 1
    fi
    echo ""
}

echo "🚀 Starting local workflow simulation..."
echo "============================================"

# Test 1: Basic Go setup (similar to workflow)
run_and_check "Check Go version" "go version"

# Test 2: Install dependencies
run_and_check "Install dependencies" "make deps"

# Test 3: Generate registry
run_and_check "Generate plugin registry" "make generate-registry"

# Test 4: Build CLI
run_and_check "Build CLI" "make build-cli"

# Test 5: Build plugins
run_and_check "Build all plugins" "make build-plugins"

# Test 6: Test CLI binary
run_and_check "Test CLI binary" "./build/bin/corkscrew --help"

# Test 7: Check built artifacts
print_step "Checking built artifacts"
echo "CLI binary:"
ls -la build/bin/corkscrew 2>/dev/null || echo "❌ CLI binary not found"

echo ""
echo "Plugin binaries:"
for plugin in aws-provider azure-provider gcp-provider kubernetes-provider; do
    if [[ -f "build/bin/$plugin" ]]; then
        echo "✅ build/bin/$plugin ($(ls -lh build/bin/$plugin | awk '{print $5}'))"
    else
        echo "❌ build/bin/$plugin not found"
    fi
done

echo ""
echo "Registry file:"
if [[ -f "plugins/registry.json" ]]; then
    echo "✅ plugins/registry.json exists"
    echo "   Generated at: $(jq -r '.generated_at' plugins/registry.json)"
    echo "   Version: $(jq -r '.generated_for_version' plugins/registry.json)"
    echo "   Plugins: $(jq -r '.plugins | keys | join(", ")' plugins/registry.json)"
else
    echo "❌ plugins/registry.json not found"
fi

print_success "All build steps completed"

echo ""
echo "🎯 Summary:"
echo "- This simulates the 'test' job from build-and-publish.yml"
echo "- All binaries are built and ready for release"
echo "- Registry is generated with current version info"
echo ""
echo "💡 Next steps:"
echo "- Run 'act -j test --secret-file .secrets' to test the full workflow in Docker"
echo "- Use 'act push -W .github/workflows/build-and-publish.yml' to test release process"
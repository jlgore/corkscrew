#!/bin/bash

# Build script for Kubernetes Provider Plugin

set -e

PLUGIN_NAME="kubernetes-provider"
BUILD_DIR="../build"
OUTPUT_BINARY="corkscrew-kubernetes"

echo "🔧 Building Kubernetes Provider Plugin..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Ensure we're in the correct directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Create build directory if it doesn't exist
mkdir -p "$BUILD_DIR"

# Clean previous build
echo "🧹 Cleaning previous build..."
rm -f "$BUILD_DIR/$OUTPUT_BINARY"

# Get dependencies
echo "📦 Getting dependencies..."
go mod download

# Run tests
echo "🧪 Running tests..."
go test ./... -v || {
    echo "❌ Tests failed!"
    exit 1
}

# Build the plugin
echo "🔨 Building plugin binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o "$BUILD_DIR/$OUTPUT_BINARY" \
    .

if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
    
    # Make it executable
    chmod +x "$BUILD_DIR/$OUTPUT_BINARY"
    
    # Show binary info
    echo ""
    echo "📊 Binary Information:"
    echo "━━━━━━━━━━━━━━━━━━━━━"
    echo "📁 Location: $BUILD_DIR/$OUTPUT_BINARY"
    echo "📏 Size: $(du -h "$BUILD_DIR/$OUTPUT_BINARY" | cut -f1)"
    echo "🏗️  Type: $(file "$BUILD_DIR/$OUTPUT_BINARY" | cut -d: -f2)"
    
    # Verify the plugin
    echo ""
    echo "🔍 Verifying plugin..."
    if "$BUILD_DIR/$OUTPUT_BINARY" --test > /dev/null 2>&1; then
        echo "✅ Plugin verification passed!"
    else
        echo "⚠️  Plugin verification failed or test mode not implemented"
    fi
    
    echo ""
    echo "🎉 Kubernetes provider plugin built successfully!"
    echo ""
    echo "📚 Usage Examples:"
    echo "━━━━━━━━━━━━━━━━━"
    echo "  # Basic scanning"
    echo "  ./corkscrew scan --provider kubernetes --namespace default"
    echo ""
    echo "  # Scan all namespaces"
    echo "  ./corkscrew scan --provider kubernetes --all-namespaces"
    echo ""
    echo "  # Scan specific resource types"
    echo "  ./corkscrew scan --provider kubernetes --resource-types pods,services,deployments"
    echo ""
    echo "  # Multi-cluster scanning"
    echo "  ./corkscrew scan --provider kubernetes --contexts prod,staging,dev"
    echo ""
    echo "  # Watch mode for real-time updates"
    echo "  ./corkscrew watch --provider kubernetes --namespace production"
    echo ""
    echo "  # Discover CRDs"
    echo "  ./corkscrew discover --provider kubernetes --include-crds"
    echo ""
    echo "  # Scan Helm releases"
    echo "  ./corkscrew scan --provider kubernetes --helm-releases"
    
else
    echo "❌ Build failed!"
    exit 1
fi
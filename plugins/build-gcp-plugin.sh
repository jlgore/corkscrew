#!/bin/bash

echo "🔧 Building GCP Provider Plugin..."

# Error handling
set -e

# Configuration
PLUGIN_NAME="corkscrew-gcp"
PLUGIN_DIR="$(pwd)/plugins/gcp-provider"
INSTALL_DIR="$HOME/.corkscrew/bin/plugin"
VERBOSE=${VERBOSE:-false}

# Helper functions
log_verbose() {
    if [ "$VERBOSE" = "true" ]; then
        echo "🔍 $1"
    fi
}

check_dependencies() {
    log_verbose "Checking dependencies..."
    
    # Check Go installation
    if ! command -v go >/dev/null 2>&1; then
        echo "❌ Go is not installed. Please install Go 1.21 or later."
        exit 1
    fi
    
    # Check Go version
    GO_VERSION=$(go version | cut -d ' ' -f 3 | cut -d 'o' -f 2)
    log_verbose "Found Go version: $GO_VERSION"
    
    # Check if we're in the right directory
    if [ ! -d "$PLUGIN_DIR" ]; then
        echo "❌ GCP provider directory not found at: $PLUGIN_DIR"
        echo "   Please run this script from the project root directory."
        exit 1
    fi
}

create_directories() {
    log_verbose "Creating build directories..."
    mkdir -p "$INSTALL_DIR"
}

build_plugin() {
    echo "📦 Building $PLUGIN_NAME..."
    
    # Change to plugin directory
    cd "$PLUGIN_DIR"
    
    # Dependency management
    log_verbose "Managing Go dependencies..."
    go mod tidy
    
    # Set build flags for optimization
    BUILD_FLAGS="-ldflags=-s -ldflags=-w"
    if [ "$VERBOSE" = "true" ]; then
        BUILD_FLAGS="$BUILD_FLAGS -v"
    fi
    
    # Build the plugin directly to install directory (matching AWS/Azure pattern)
    log_verbose "Building with flags: $BUILD_FLAGS"
    go build $BUILD_FLAGS -o "$INSTALL_DIR/$PLUGIN_NAME" .
    
    # Return to original directory
    cd - >/dev/null
}

install_plugin() {
    log_verbose "Setting plugin permissions..."
    
    # Make executable (already built to install directory)
    chmod +x "$INSTALL_DIR/$PLUGIN_NAME"
    
    log_verbose "Plugin installed to: $INSTALL_DIR/$PLUGIN_NAME"
}

validate_plugin() {
    echo "🧪 Validating plugin..."
    
    # Check if binary exists and is executable
    if [ ! -x "$INSTALL_DIR/$PLUGIN_NAME" ]; then
        echo "❌ Plugin binary is not executable"
        exit 1
    fi
    
    # Test plugin health
    log_verbose "Running plugin health check..."
    if "$INSTALL_DIR/$PLUGIN_NAME" --version >/dev/null 2>&1; then
        log_verbose "✅ Plugin responds to --version"
    else
        log_verbose "⚠️  Plugin does not respond to --version (may be normal)"
    fi
    
    # Test basic functionality
    if "$INSTALL_DIR/$PLUGIN_NAME" --test >/dev/null 2>&1; then
        log_verbose "✅ Plugin passes --test"
    else
        log_verbose "⚠️  Plugin test may require configuration"
    fi
}

show_usage_info() {
    echo ""
    echo "🎉 Build complete!"
    echo ""
    echo "📁 Plugin location: $INSTALL_DIR/$PLUGIN_NAME"
    echo "📊 Size: $(du -h "$INSTALL_DIR/$PLUGIN_NAME" | cut -f1)"
    echo ""
    echo "🚀 Usage:"
    echo "  ./corkscrew scan --provider gcp"
    echo "  ./corkscrew plugin status"
    echo "  ./corkscrew plugin list"
    echo ""
    echo "🧪 Testing:"
    echo "  export GCP_PROJECT_ID=your-project-id"
    echo "  export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json"
    echo "  $INSTALL_DIR/$PLUGIN_NAME --test-gcp"
    echo ""
    echo "🗃️  Cloud Asset Inventory:"
    echo "  $INSTALL_DIR/$PLUGIN_NAME --check-asset-inventory"
    echo ""
    echo "🔧 Auto-Discovery:"
    echo "  make gcp-analyze-libraries"
    echo "  make gcp-generate-scanners"
}

cleanup_on_error() {
    echo "❌ Build failed during: $1"
    echo "🧹 Cleaning up incomplete build..."
    rm -f "$INSTALL_DIR/$PLUGIN_NAME"
    exit 1
}

# Main execution
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --verbose|-v)
                VERBOSE=true
                shift
                ;;
            --help|-h)
                echo "Usage: $0 [--verbose] [--help]"
                echo "Build the GCP provider plugin for Corkscrew"
                echo ""
                echo "Options:"
                echo "  --verbose, -v    Enable verbose output"
                echo "  --help, -h       Show this help message"
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                echo "Use --help for usage information"
                exit 1
                ;;
        esac
    done
    
    # Execute build pipeline
    trap 'cleanup_on_error "dependency check"' ERR
    check_dependencies
    
    trap 'cleanup_on_error "directory creation"' ERR
    create_directories
    
    trap 'cleanup_on_error "build"' ERR
    build_plugin
    
    trap 'cleanup_on_error "installation"' ERR
    install_plugin
    
    trap 'cleanup_on_error "validation"' ERR
    validate_plugin
    
    # Clear error trap for final steps
    trap - ERR
    
    show_usage_info
}

# Run main function
main "$@"
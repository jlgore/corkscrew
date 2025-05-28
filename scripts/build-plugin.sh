#!/bin/bash

set -e

echo "🔨 Building Corkscrew AWS Plugin..."

# Create plugins directory if it doesn't exist
mkdir -p plugins/bin

# Build the AWS plugin
echo "Building AWS plugin..."
cd plugins/aws
go build -o ../bin/corkscrew-aws .
cd ../..

echo "✅ AWS plugin built successfully: plugins/bin/corkscrew-aws"

# Make it executable
chmod +x plugins/bin/corkscrew-aws

# Build the example
echo "Building example program..."
cd examples
go build -o basic_usage basic_usage.go
cd ..

echo "✅ Example program built: examples/basic_usage"

echo "🎉 Build completed!"
echo ""
echo "To test the plugin:"
echo "  export CORKSCREW_PLUGIN_DIR=./plugins/bin"
echo "  ./examples/basic_usage"

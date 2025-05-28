#!/bin/bash

echo "🔧 Building AWS Provider Plugin..."

# Ensure build directory exists
mkdir -p plugins/build

# Build the AWS provider
cd plugins/aws-provider
echo "📦 Building aws-provider..."
go build -o ../build/aws-provider .

if [ $? -eq 0 ]; then
    echo "✅ AWS Provider built successfully!"
    echo "📁 Binary location: plugins/build/aws-provider"
    echo "📊 Size: $(du -h ../build/aws-provider | cut -f1)"
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew plugin install aws-provider"
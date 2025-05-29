#!/bin/bash

echo "🔧 Building Azure Provider Plugin..."

# Ensure build directory exists
mkdir -p plugins/build

# Build the Azure provider
cd plugins/azure-provider
echo "📦 Building azure-provider..."
go build -o ../build/corkscrew-azure .

if [ $? -eq 0 ]; then
    echo "✅ Azure Provider built successfully!"
    echo "📁 Binary location: plugins/build/corkscrew-azure"
    echo "📊 Size: $(du -h ../build/corkscrew-azure | cut -f1)"
    
    # Make it executable
    chmod +x ../build/corkscrew-azure
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew scan --provider azure --services compute,storage --region eastus" 
#!/bin/bash

echo "🔧 Building Azure Provider Plugin..."

# Ensure corkscrew plugin directory exists
mkdir -p ~/.corkscrew/bin/plugin

# Build the Azure provider
cd plugins/azure-provider
echo "📦 Building azure-provider..."
go build -o ~/.corkscrew/bin/plugin/corkscrew-azure .

if [ $? -eq 0 ]; then
    echo "✅ Azure Provider built successfully!"
    echo "📁 Binary location: ~/.corkscrew/bin/plugin/corkscrew-azure"
    echo "📊 Size: $(du -h ~/.corkscrew/bin/plugin/corkscrew-azure | cut -f1)"
    
    # Make it executable
    chmod +x ~/.corkscrew/bin/plugin/corkscrew-azure
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew scan --provider azure" 
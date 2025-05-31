#!/bin/bash

echo "🔧 Building AWS Provider Plugin..."

# Ensure corkscrew plugin directory exists
mkdir -p ~/.corkscrew/bin/plugin

# Build the AWS provider
cd plugins/aws-provider
echo "📦 Building aws-provider..."
go build -o ~/.corkscrew/bin/plugin/corkscrew-aws .

if [ $? -eq 0 ]; then
    echo "✅ AWS Provider built successfully!"
    echo "📁 Binary location: ~/.corkscrew/bin/plugin/corkscrew-aws"
    echo "📊 Size: $(du -h ~/.corkscrew/bin/plugin/corkscrew-aws | cut -f1)"
    
    # Make it executable
    chmod +x ~/.corkscrew/bin/plugin/corkscrew-aws
else
    echo "❌ Build failed!"
    exit 1
fi

echo ""
echo "🎉 Build complete! You can now use:"
echo "  ./corkscrew scan --provider aws"
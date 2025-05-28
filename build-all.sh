#!/bin/bash

set -e

echo "🔨 Building Corkscrew Cloud Resource Scanner"
echo "============================================"

# Build main corkscrew binary
echo "📦 Building main corkscrew binary..."
cd cmd/corkscrew
go build -o ../../corkscrew
cd ../..
echo "✅ Main binary built: ./corkscrew"

# Build AWS provider plugin
echo "📦 Building AWS provider plugin..."
cd plugins/aws-provider
go build -o aws-provider
cd ../..
echo "✅ AWS provider plugin built: ./plugins/aws-provider/aws-provider"

# Make binaries executable
chmod +x corkscrew
chmod +x plugins/aws-provider/aws-provider

echo ""
echo "🎉 Build complete!"
echo ""
echo "📊 Binary sizes:"
ls -lh corkscrew plugins/aws-provider/aws-provider

echo ""
echo "🚀 Ready to use:"
echo "  ./corkscrew info                                    # Show provider info"
echo "  ./corkscrew discover --verbose                      # Discover AWS services"
echo "  ./corkscrew list --service s3 --verbose             # List S3 resources"
echo "  ./corkscrew scan --services s3,ec2 --verbose        # Full scan"
echo ""
echo "🔧 With AWS credentials configured, try:"
echo "  export AWS_REGION=us-east-1"
echo "  ./corkscrew scan --services iam --verbose"
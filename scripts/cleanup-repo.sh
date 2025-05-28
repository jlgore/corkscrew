#!/bin/bash

# Cleanup script for corkscrew repository before PR

echo "🧹 Cleaning up corkscrew repository..."

# Remove binary files
echo "📦 Removing binary files..."
rm -f corkscrew
rm -f resource-lister
rm -f cmd/corkscrew/corkscrew
rm -f cmd/generator/generator
rm -f cmd/generator/main
rm -f cmd/aws-service-discovery-web/aws-discovery-web
rm -f cmd/plugin-test/plugin-test
rm -f cmd/resource-lister/resource-lister

# Remove build artifacts
echo "🏗️  Removing build directories..."
rm -rf build/
rm -rf plugins/build/
rm -rf plugins/*/build/

# Remove generated files
echo "🤖 Removing generated files..."
rm -rf generated/
rm -rf cmd/generator/generated/
rm -rf github.com/  # This appears to be a generated directory

# Remove test results and temporary files
echo "🧪 Removing test results..."
rm -rf test-results/
rm -rf temp/
rm -f hierarchical_discovery_results.json
rm -f iam-resources.json
rm -f *.tmp
rm -f *.log
rm -f *.out

# Remove plugin binaries
echo "🔌 Removing plugin binaries..."
find plugins/ -name "*.so" -delete

# Remove node_modules if it exists
echo "📦 Removing node_modules..."
rm -rf node_modules/

# Remove IDE and editor files
echo "📝 Removing IDE files..."
rm -rf .idea/
rm -rf .vscode/
rm -rf .cursor/
rm -f *.swp
rm -f *.swo

# Remove personal config files
echo "⚙️  Removing personal config files..."
rm -f .env
rm -f .env.example
rm -f .taskmasterconfig
rm -f .roomodes
rm -f .windsurfrules
rm -rf .roo/

# Remove database files
echo "💾 Removing database files..."
find . -name "*.db" -delete
find . -name "*.duckdb" -delete

# Clean up go.mod files in temp directories (if they exist)
echo "📄 Cleaning up temporary go.mod files..."
find . -path "*/temp/*" -name "go.mod" -delete
find . -path "*/temp/*" -name "go.sum" -delete

# Update .gitignore to ensure all these patterns are included
echo "📝 Checking .gitignore..."
cat >> .gitignore << 'EOF'

# Cleanup additions
corkscrew
resource-lister
hierarchical_discovery_results.json
iam-resources.json
test-results/
temp/
.cursor/
.env.example
.roo/
.roomodes
.taskmasterconfig
.windsurfrules
github.com/
EOF

# Remove duplicate lines from .gitignore
echo "🔧 Cleaning up .gitignore..."
awk '!seen[$0]++' .gitignore > .gitignore.tmp && mv .gitignore.tmp .gitignore

# Show what's left that might need attention
echo ""
echo "⚠️  Files that might need manual review:"
echo ""

# Check for large files
echo "Large files (>1MB):"
find . -type f -size +1M -not -path "./.git/*" -not -path "./node_modules/*" -not -path "./vendor/*" | head -10

echo ""
echo "✅ Cleanup complete!"
echo ""
echo "📋 Next steps:"
echo "1. Review the changes with: git status"
echo "2. Add the changes: git add -A"
echo "3. Commit: git commit -m 'chore: cleanup repository for PR'"
echo "4. Make sure tests pass: make test"
echo "5. Create your PR!"
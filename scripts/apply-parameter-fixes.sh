#!/bin/bash

# Script to automatically apply parameter validation fixes
# Based on the output from test-all-services.sh

set -e

PARAM_ERRORS_FILE="./test-results/parameter_errors_summary.txt"
SOURCE_FILE="internal/scanner/aws_resource_lister.go"
BACKUP_FILE="internal/scanner/aws_resource_lister.go.backup"

echo "🔧 AWS Parameter Validation Fix Applier"
echo "========================================"

# Check if parameter errors file exists
if [[ ! -f "$PARAM_ERRORS_FILE" ]]; then
    echo "❌ Parameter errors file not found: $PARAM_ERRORS_FILE"
    echo "Please run ./scripts/test-all-services.sh first"
    exit 1
fi

# Create backup
echo "📋 Creating backup of $SOURCE_FILE..."
cp "$SOURCE_FILE" "$BACKUP_FILE"
echo "✅ Backup created: $BACKUP_FILE"

# Extract new parameter patterns
echo "🔍 Extracting parameter validation errors..."
NEW_PATTERNS=$(grep '^"' "$PARAM_ERRORS_FILE" | sort -u)

if [[ -z "$NEW_PATTERNS" ]]; then
    echo "✅ No new parameter validation errors found!"
    exit 0
fi

echo "📝 Found the following operations that need to be excluded:"
echo "$NEW_PATTERNS"
echo ""

# Read current parameterizedPatterns
echo "🔧 Updating parameterizedPatterns in $SOURCE_FILE..."

# Create a temporary file with the new patterns
TEMP_FILE=$(mktemp)

# Extract the current parameterizedPatterns section
awk '
/parameterizedPatterns := \[\]string{/ {
    print $0
    in_patterns = 1
    next
}
in_patterns && /^[[:space:]]*}/ {
    # Add new patterns before closing brace
    while ((getline line < "'"$PARAM_ERRORS_FILE"'") > 0) {
        if (line ~ /^"/) {
            print "\t\t" line
        }
    }
    close("'"$PARAM_ERRORS_FILE"'")
    print $0
    in_patterns = 0
    next
}
in_patterns {
    print $0
    next
}
!in_patterns {
    print $0
}
' "$SOURCE_FILE" > "$TEMP_FILE"

# Replace the original file
mv "$TEMP_FILE" "$SOURCE_FILE"

echo "✅ Parameter patterns updated successfully!"

# Rebuild the resource-lister
echo "🔨 Rebuilding resource-lister..."
go build -o resource-lister ./cmd/resource-lister
echo "✅ Build complete!"

echo ""
echo "🎉 Parameter validation fixes applied successfully!"
echo ""
echo "Next steps:"
echo "1. Test specific services that had errors: ./resource-lister --services <service> --debug"
echo "2. Run full test suite again: ./scripts/test-all-services.sh"
echo "3. If issues persist, check the backup: $BACKUP_FILE" 
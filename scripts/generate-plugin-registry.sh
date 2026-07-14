#!/bin/bash
set -euo pipefail

# Generate plugin registry at build time
# Usage: ./scripts/generate-plugin-registry.sh [version] [repo-owner/repo-name]

VERSION=${1:-"dev"}
GITHUB_REPO=${2:-"jlgore/corkscrew"}
OUTPUT_FILE=${3:-"plugins/registry.json"}

echo "🔧 Generating plugin registry..."
echo "   Version: $VERSION"
echo "   Repository: $GITHUB_REPO"
echo "   Output: $OUTPUT_FILE"

# Ensure output directory exists
mkdir -p "$(dirname "$OUTPUT_FILE")"

# Start building the JSON
cat > "$OUTPUT_FILE" << EOF
{
  "version": "1.0",
  "generated_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "generated_for_version": "$VERSION",
  "plugins": {
EOF

FIRST_PLUGIN=true

# Scan for plugin directories
for plugin_dir in plugins/*-provider; do
  if [[ -d "$plugin_dir" && -f "$plugin_dir/main.go" ]]; then
    PLUGIN_NAME=$(basename "$plugin_dir")
    PROVIDER_TYPE=${PLUGIN_NAME%-provider}
    
    # Extract plugin info from go.mod or defaults
    PLUGIN_VERSION="1.0.0"
    if [[ -f "$plugin_dir/go.mod" ]]; then
      # Try to extract version from go.mod comment or use default
      PLUGIN_VERSION=$(grep -E "^// version:" "$plugin_dir/go.mod" | cut -d: -f2 | tr -d ' ' || echo "1.0.0")
    fi
    
    # Determine status based on plugin maturity
    STATUS="beta"
    case $PROVIDER_TYPE in
      aws|azure) STATUS="stable" ;;
      *) STATUS="beta" ;;
    esac
    
    # Determine capabilities by checking for specific files/patterns
    CAPABILITIES=()
    if [[ -f "$plugin_dir/discovery.go" || -f "$plugin_dir/scanner.go" ]]; then
      CAPABILITIES+=("discover" "scan")
    fi
    if [[ -f "$plugin_dir/relationships.go" ]]; then
      CAPABILITIES+=("relationships")
    fi
    # Default capabilities if none detected
    if [[ ${#CAPABILITIES[@]} -eq 0 ]]; then
      CAPABILITIES=("discover" "scan")
    fi
    
    # Format capabilities as JSON array
    CAPS_JSON=$(printf '"%s",' "${CAPABILITIES[@]}" | sed 's/,$//')
    
    # Determine description
    DESCRIPTION="Provider for $PROVIDER_TYPE"
    case $PROVIDER_TYPE in
      aws) DESCRIPTION="Amazon Web Services provider" ;;
      azure) DESCRIPTION="Microsoft Azure provider" ;;
      cloudflare) DESCRIPTION="Cloudflare provider" ;;
      gcp) DESCRIPTION="Google Cloud Platform provider" ;;
      kubernetes) DESCRIPTION="Kubernetes provider" ;;
    esac
    
    # Add comma if not first plugin
    if [[ "$FIRST_PLUGIN" == "false" ]]; then
      echo "," >> "$OUTPUT_FILE"
    fi
    FIRST_PLUGIN=false
    
    # Add plugin entry
    cat >> "$OUTPUT_FILE" << EOF
    "$PROVIDER_TYPE": {
      "name": "$PLUGIN_NAME",
      "description": "$DESCRIPTION",
      "version": "$PLUGIN_VERSION",
      "source": "$plugin_dir",
      "binary": "$PLUGIN_NAME",
      "releases": {
        "darwin-arm64": "https://github.com/$GITHUB_REPO/releases/download/$VERSION/$PLUGIN_NAME-darwin-arm64",
        "linux-amd64": "https://github.com/$GITHUB_REPO/releases/download/$VERSION/$PLUGIN_NAME-linux-amd64",
        "windows-amd64": "https://github.com/$GITHUB_REPO/releases/download/$VERSION/$PLUGIN_NAME-windows-amd64.exe"
      },
      "capabilities": [$CAPS_JSON],
      "status": "$STATUS"
    }
EOF
  fi
done

# Close the JSON
cat >> "$OUTPUT_FILE" << EOF

  }
}
EOF

echo "✅ Plugin registry generated: $OUTPUT_FILE"

# Validate JSON syntax
if command -v jq > /dev/null 2>&1; then
  if jq . "$OUTPUT_FILE" > /dev/null; then
    echo "✅ Registry JSON is valid"
  else
    echo "❌ Registry JSON is invalid"
    exit 1
  fi
else
  echo "⚠️  jq not available - skipping JSON validation"
fi

# Show summary
PLUGIN_COUNT=$(jq '.plugins | length' "$OUTPUT_FILE" 2>/dev/null || echo "unknown")
echo "📊 Generated registry with $PLUGIN_COUNT plugins"

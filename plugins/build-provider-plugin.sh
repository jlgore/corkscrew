#!/bin/bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <provider> [go-build-args...]"
  exit 1
fi

PROVIDER="$1"
shift

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLUGIN_DIR="$PROJECT_ROOT/plugins/$PROVIDER-provider"
INSTALL_DIR="$HOME/.corkscrew/plugins/official/$PROVIDER"
PLUGIN_BINARY="$PROVIDER-provider"

if [[ ! -d "$PLUGIN_DIR" ]]; then
  echo "❌ Provider directory not found: $PLUGIN_DIR"
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "❌ Go is not installed. Please install Go 1.26.2 or later."
  exit 1
fi

echo "🔧 Building $PROVIDER Provider Plugin..."
echo "📦 Output: $INSTALL_DIR/$PLUGIN_BINARY"

mkdir -p "$INSTALL_DIR"

(
  cd "$PLUGIN_DIR"
  go build "$@" -o "$INSTALL_DIR/$PLUGIN_BINARY" .
)

chmod +x "$INSTALL_DIR/$PLUGIN_BINARY"
cp "$PLUGIN_DIR/plugin.json" "$INSTALL_DIR/plugin.json"

echo "✅ $PROVIDER provider built successfully"
echo "📁 Binary location: $INSTALL_DIR/$PLUGIN_BINARY"
echo "📄 Manifest location: $INSTALL_DIR/plugin.json"
echo "📊 Size: $(du -h "$INSTALL_DIR/$PLUGIN_BINARY" | cut -f1)"

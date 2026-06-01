#!/bin/bash
set -euo pipefail

"$(dirname "$0")/build-provider-plugin.sh" github "$@"

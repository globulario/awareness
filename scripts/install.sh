#!/usr/bin/env bash
# install.sh — install Awareness CLI and MCP server
#
# Requirements: Go 1.21+ installed and ~/go/bin on PATH.
#
# Usage:
#   curl -sfL https://raw.githubusercontent.com/globulario/awareness/master/scripts/install.sh | bash
#   # or locally:
#   bash scripts/install.sh
set -euo pipefail

VERSION="${AWARENESS_VERSION:-v0.1.0}"

echo "Installing Awareness ${VERSION}..."

go install "github.com/globulario/awareness/cmd/awareness@${VERSION}"
go install "github.com/globulario/awareness/cmd/awareness-mcp@${VERSION}"

echo ""
echo "Awareness ${VERSION} installed."
echo ""

# Verify installations.
if ! command -v awareness >/dev/null 2>&1; then
  echo "WARNING: 'awareness' not found on PATH."
  echo "Add ~/go/bin to your PATH:"
  echo "  export PATH=\"\$PATH:\$(go env GOPATH)/bin\""
  echo ""
else
  echo "  awareness $(awareness --version 2>/dev/null || echo '(installed)')"
fi

if ! command -v awareness-mcp >/dev/null 2>&1; then
  echo "WARNING: 'awareness-mcp' not found on PATH."
  echo "Add ~/go/bin to your PATH:"
  echo "  export PATH=\"\$PATH:\$(go env GOPATH)/bin\""
  echo ""
fi

echo ""
echo "Next steps:"
echo "  1. In your project directory:"
echo "     mkdir -p .awareness"
echo "     cat > .awareness.yaml <<'YAML'"
echo "     project:"
echo "       name: my-project"
echo "       kind: generic"
echo "     runtime:"
echo "       enabled: false"
echo "       adapter: null"
echo "     YAML"
echo ""
echo "  2. Run preflight:"
echo "     awareness profile doctor"
echo "     awareness preflight --changed"
echo ""
echo "  3. Start the MCP server:"
echo "     awareness-mcp --project-root ."
echo ""
echo "  4. Full docs:"
echo "     https://github.com/globulario/awareness"

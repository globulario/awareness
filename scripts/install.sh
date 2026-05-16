#!/usr/bin/env bash
# install.sh — install Awareness CLI and MCP server.
#
# Usage:
#   # Latest release via go install:
#   bash scripts/install.sh
#
#   # Specific version:
#   VERSION=v0.1.0 bash scripts/install.sh
#
#   # From curl (once hosted):
#   curl -fsSL https://raw.githubusercontent.com/globulario/awareness/master/scripts/install.sh | bash
#
# The script installs via 'go install' (no sudo required, no binary download yet).
# Binary release downloads will be added in a future version once release archives
# are served at a stable URL.
#
# Requirements: Go 1.21+ installed and $(go env GOPATH)/bin on PATH.
set -euo pipefail

VERSION="${VERSION:-v0.1.0}"
INSTALL_DIR="${INSTALL_DIR:-$(go env GOPATH)/bin}"

echo "Installing Awareness ${VERSION}..."
echo "Install dir: ${INSTALL_DIR}"
echo ""

# Ensure install dir exists.
mkdir -p "$INSTALL_DIR"

go install "github.com/globulario/awareness/cmd/awareness@${VERSION}"
go install "github.com/globulario/awareness/cmd/awareness-mcp@${VERSION}"

echo ""
echo "Awareness ${VERSION} installed."
echo ""

# Verify installations.
AWARENESS_OK=false
MCP_OK=false

if command -v awareness >/dev/null 2>&1; then
  echo "  awareness: $(awareness --help 2>&1 | head -1 || echo '(installed)')"
  AWARENESS_OK=true
else
  echo "  awareness: not found on PATH"
fi

if command -v awareness-mcp >/dev/null 2>&1; then
  echo "  awareness-mcp: installed"
  MCP_OK=true
else
  echo "  awareness-mcp: not found on PATH"
fi

if ! $AWARENESS_OK || ! $MCP_OK; then
  echo ""
  echo "PATH guidance: add Go binaries to your PATH:"
  echo "  export PATH=\"\$PATH:\$(go env GOPATH)/bin\""
  echo ""
  echo "Add this to ~/.bashrc or ~/.zshrc to make it permanent."
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
echo "  3. Build a graph:"
echo "     awareness graph build"
echo "     awareness graph inspect"
echo ""
echo "  4. Start the MCP server:"
echo "     awareness-mcp --project-root ."
echo ""
echo "  5. Full docs:"
echo "     https://github.com/globulario/awareness/blob/master/docs/getting-started.md"

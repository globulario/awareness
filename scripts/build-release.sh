#!/usr/bin/env bash
# build-release.sh — build binary release archives for all supported platforms.
#
# Usage:
#   scripts/build-release.sh [VERSION]
#   VERSION=v0.1.1 scripts/build-release.sh
#
# Outputs:
#   dist/awareness_${VERSION}_linux_amd64.tar.gz
#   dist/awareness_${VERSION}_linux_arm64.tar.gz
#   dist/awareness_${VERSION}_darwin_amd64.tar.gz
#   dist/awareness_${VERSION}_darwin_arm64.tar.gz
#   dist/awareness_${VERSION}_windows_amd64.zip
#   dist/checksums.txt
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="${1:-${VERSION:-}}"
if [ -z "$VERSION" ]; then
  echo "Usage: scripts/build-release.sh VERSION (e.g. v0.1.0)"
  echo "Or:    VERSION=v0.1.0 scripts/build-release.sh"
  exit 1
fi

DIST="$REPO_ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST"

TARGETS=(
  "linux   amd64"
  "linux   arm64"
  "darwin  amd64"
  "darwin  arm64"
  "windows amd64"
)

LDFLAGS="-s -w -X main.version=${VERSION}"

build_target() {
  local os="$1"
  local arch="$2"
  local suffix=""
  local archive_ext="tar.gz"

  if [ "$os" = "windows" ]; then
    suffix=".exe"
    archive_ext="zip"
  fi

  local dir="${DIST}/awareness_${VERSION}_${os}_${arch}"
  mkdir -p "$dir"

  echo "Building ${os}/${arch}..."

  GOOS="$os" GOARCH="$arch" go build \
    -ldflags "$LDFLAGS" \
    -trimpath \
    -o "${dir}/awareness${suffix}" \
    ./cmd/awareness

  GOOS="$os" GOARCH="$arch" go build \
    -ldflags "$LDFLAGS" \
    -trimpath \
    -o "${dir}/awareness-mcp${suffix}" \
    ./cmd/awareness-mcp

  # Copy README.
  if [ -f "$REPO_ROOT/README.md" ]; then
    cp "$REPO_ROOT/README.md" "$dir/README.md"
  fi

  # Archive.
  local archive="${DIST}/awareness_${VERSION}_${os}_${arch}.${archive_ext}"
  if [ "$archive_ext" = "tar.gz" ]; then
    tar -czf "$archive" -C "$DIST" "$(basename "$dir")"
  else
    (cd "$DIST" && zip -q -r "$(basename "$archive")" "$(basename "$dir")")
  fi

  rm -rf "$dir"
  echo "  -> $(basename "$archive")"
}

for target in "${TARGETS[@]}"; do
  read -r goos goarch <<<"$target"
  build_target "$goos" "$goarch"
done

# Generate checksums.
echo ""
echo "Generating checksums..."
(cd "$DIST" && sha256sum awareness_*.tar.gz awareness_*.zip 2>/dev/null > checksums.txt || \
 shasum -a 256 awareness_*.tar.gz awareness_*.zip 2>/dev/null > checksums.txt || true)

echo ""
echo "=== Build complete: ${VERSION} ==="
ls -lh "$DIST"
echo ""
cat "$DIST/checksums.txt"

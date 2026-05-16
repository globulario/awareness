#!/usr/bin/env bash
# check-import-wall.sh — verify the standalone awareness repo has no forbidden imports.
#
# The standalone module must NEVER import github.com/globulario/services or
# use internal Globular runtime bridges.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

FAIL=0

check() {
  local label="$1"
  local pattern="$2"
  local results
  results=$(grep -rn "$pattern" . --include="*.go" 2>/dev/null || true)
  if [ -n "$results" ]; then
    echo "FAIL: $label"
    echo "$results" | sed 's/^/  /'
    FAIL=1
  else
    echo "OK:   $label"
  fi
}

echo "=== Awareness import wall check ==="
echo ""

# Check for actual imports (leading quote = inside import block)
check "no services import"    '"github.com/globulario/services'
check "no etcd import"        '"go.etcd.io/'
check "no Scylla/gocql import" '"github.com/gocql'
check "no MinIO import"       '"github.com/minio'

# Check for actual code references (not comments)
check "no newLiveBridge calls"   'newLiveBridge'
check "no bridge.Snapshot calls" 'bridge\.Snapshot'

echo ""
if [ "$FAIL" -ne 0 ]; then
  echo "Import wall VIOLATED — fix before release."
  exit 1
fi
echo "Import wall OK."

#!/usr/bin/env bash
# smoke-cadence-mcp.sh — verify the standalone awareness-mcp against Cadence.
#
# Checks:
#   1. No services imports in standalone awareness repo.
#   2. Standalone module builds cleanly.
#   3. awareness CLI: profile doctor succeeds on Cadence.
#   4. awareness CLI: preflight --changed succeeds on Cadence.
#   5. awareness-mcp: binary builds.
#   6. awareness-mcp: responds to tools/list with expected tools.
#   7. awareness-mcp: awareness_runtime_status returns runtime_disabled.
#
# Usage:
#   cd /path/to/awareness
#   bash scripts/smoke-cadence-mcp.sh

set -euo pipefail

AWARENESS_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CADENCE_ROOT="/home/dave/Documents/github.com/globulario/cadence"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; exit 1; }

cd "$AWARENESS_REPO"

echo "=== Smoke test: awareness-mcp on Cadence ==="
echo "Awareness repo: $AWARENESS_REPO"
echo "Cadence root:   $CADENCE_ROOT"
echo ""

# 1. Import wall check.
echo "[1/7] Import wall: no services imports in standalone awareness"
if grep -R "github.com/globulario/services" . --include="*.go" -q 2>/dev/null; then
    fail "standalone awareness imports services"
fi
pass "no services imports"

# 2. Build all packages.
echo "[2/7] Build standalone module"
go build ./... || fail "go build failed"
pass "go build ./..."

# 3. awareness profile doctor on Cadence.
echo "[3/7] awareness profile doctor (Cadence)"
if [ ! -f "$CADENCE_ROOT/.awareness.yaml" ]; then
    fail "Cadence .awareness.yaml not found at $CADENCE_ROOT"
fi
OUTPUT=$(go run ./cmd/awareness profile doctor --project-root "$CADENCE_ROOT" 2>&1)
echo "$OUTPUT"
if ! echo "$OUTPUT" | grep -q "status: ok"; then
    fail "profile doctor did not return 'status: ok'"
fi
if ! echo "$OUTPUT" | grep -q "runtime: disabled"; then
    fail "profile doctor did not return 'runtime: disabled'"
fi
pass "profile doctor"

# 4. awareness preflight --changed on Cadence.
echo "[4/7] awareness preflight --changed (Cadence)"
OUTPUT=$(go run ./cmd/awareness preflight --changed --project-root "$CADENCE_ROOT" 2>&1)
echo "$OUTPUT"
if ! echo "$OUTPUT" | grep -q "status: ok"; then
    fail "preflight did not return 'status: ok'"
fi
pass "preflight --changed"

# 5. Build awareness-mcp binary.
echo "[5/7] Build awareness-mcp"
TMPBIN=$(mktemp -d)
go build -o "$TMPBIN/awareness-mcp" ./cmd/awareness-mcp || fail "awareness-mcp build failed"
pass "awareness-mcp builds"

# 6. tools/list — verify expected tools are registered.
echo "[6/7] awareness-mcp tools/list (Cadence, NullAdapter)"
TOOLS_REQ='{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
TOOLS_RESP=$(printf 'Content-Length: %d\r\n\r\n%s' "${#TOOLS_REQ}" "$TOOLS_REQ" \
    | timeout 5 "$TMPBIN/awareness-mcp" --project-root "$CADENCE_ROOT" 2>/dev/null || true)

for tool in awareness_profile_doctor awareness_preflight awareness_runtime_status awareness_context; do
    if ! echo "$TOOLS_RESP" | grep -q "$tool"; then
        fail "tools/list missing: $tool"
    fi
done
pass "tools/list: all expected tools present"

# 7. awareness_runtime_status — must return runtime_disabled.
echo "[7/7] awareness-mcp awareness_runtime_status → runtime_disabled"
CALL_REQ='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"awareness_runtime_status","arguments":{}}}'
CALL_RESP=$(printf 'Content-Length: %d\r\n\r\n%s' "${#CALL_REQ}" "$CALL_REQ" \
    | timeout 5 "$TMPBIN/awareness-mcp" --project-root "$CADENCE_ROOT" 2>/dev/null || true)

if ! echo "$CALL_RESP" | grep -q "runtime_disabled"; then
    fail "awareness_runtime_status did not return runtime_disabled"
fi
pass "awareness_runtime_status: runtime_disabled"

# Cleanup.
rm -rf "$TMPBIN"

echo ""
echo "=== All smoke tests passed ==="

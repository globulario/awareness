#!/usr/bin/env bash
# smoke-graph.sh — smoke test graph build, query, and inspect.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

CADENCE_ROOT="${CADENCE_ROOT:-/home/dave/Documents/github.com/globulario/cadence}"

FAIL=0
PASS=0

run_check() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "OK:   $label"
    PASS=$((PASS+1))
  else
    echo "FAIL: $label"
    "$@" 2>&1 | head -10 | sed 's/^/  /' || true
    FAIL=$((FAIL+1))
  fi
}

echo "=== Graph smoke tests ==="
echo ""

EXAMPLES=(
  "examples/go-service"
  "examples/bpmn-engine"
  "examples/mixed-monorepo"
)

for example in "${EXAMPLES[@]}"; do
  name=$(basename "$example")
  out="/tmp/awareness-graph-smoke-${name}.json"
  run_check "$name: graph build" go run ./cmd/awareness graph build --project-root "$example" --out "$out"
  run_check "$name: graph inspect (via --out)" go run ./cmd/awareness graph inspect --project-root "$example"
done

# Cadence smoke.
if [ -d "$CADENCE_ROOT" ]; then
  out="/tmp/awareness-graph-smoke-cadence.json"
  run_check "cadence: graph build" go run ./cmd/awareness graph build --project-root "$CADENCE_ROOT" --out "$out"
  run_check "cadence: graph query 'token'" go run ./cmd/awareness graph query --project-root "$CADENCE_ROOT" --query "token"
  run_check "cadence: graph inspect" go run ./cmd/awareness graph inspect --project-root "$CADENCE_ROOT"
fi

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed."

if [ "$FAIL" -ne 0 ]; then
  echo "Graph smoke tests FAILED."
  exit 1
fi
echo "Graph smoke tests OK."

#!/usr/bin/env bash
# smoke-examples.sh — smoke test all 4 example projects.
#
# Verifies: profile doctor, preflight, and bundle build for each example.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

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
    "$@" 2>&1 | sed 's/^/  /' || true
    FAIL=$((FAIL+1))
  fi
}

EXAMPLES=(
  "examples/go-service"
  "examples/typescript-app"
  "examples/bpmn-engine"
  "examples/mixed-monorepo"
)

echo "=== Awareness example smoke tests ==="
echo ""

for example in "${EXAMPLES[@]}"; do
  name=$(basename "$example")

  run_check "$name: profile doctor" \
    go run ./cmd/awareness profile doctor --project-root "$example"

  run_check "$name: preflight --changed" \
    go run ./cmd/awareness preflight --changed --project-root "$example"

  outdir="/tmp/awareness-smoke-${name}"
  rm -rf "$outdir"
  run_check "$name: bundle build" \
    go run ./cmd/awareness bundle build --project-root "$example" --out "$outdir"
done

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed."

if [ "$FAIL" -ne 0 ]; then
  echo "Example smoke tests FAILED."
  exit 1
fi
echo "Example smoke tests OK."

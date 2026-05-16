#!/usr/bin/env bash
# check-release.sh — full pre-release verification gate.
#
# Runs: go test, go build, import wall, example smoke, Cadence smoke.
# Pass all checks before tagging a release.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

CADENCE_ROOT="${CADENCE_ROOT:-/home/dave/Documents/github.com/globulario/cadence}"

FAIL=0
PASS=0

section() { echo ""; echo "=== $1 ==="; echo ""; }

pass() { echo "OK:   $1"; PASS=$((PASS+1)); }
fail() { echo "FAIL: $1"; FAIL=$((FAIL+1)); }

run() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    pass "$label"
  else
    echo "FAIL: $label"
    "$@" 2>&1 | head -20 | sed 's/^/  /' || true
    FAIL=$((FAIL+1))
  fi
}

# ── go tests ──────────────────────────────────────────────────────────────────
section "Go tests"
run "go test ./..." go test ./...

# ── go build ──────────────────────────────────────────────────────────────────
section "Go build"
run "go build ./..."   go build ./...
run "awareness CLI"    go build ./cmd/awareness
run "awareness-mcp"    go build ./cmd/awareness-mcp

# ── import wall ───────────────────────────────────────────────────────────────
section "Import wall"
if bash scripts/check-import-wall.sh; then
  pass "import wall"
else
  fail "import wall"
fi

# ── examples ──────────────────────────────────────────────────────────────────
section "Example smoke"
if bash scripts/smoke-examples.sh; then
  pass "all examples"
else
  fail "some examples"
fi

# ── Graph smoke ───────────────────────────────────────────────────────────────
section "Graph smoke"
if bash scripts/smoke-graph.sh; then
  pass "all graph tests"
else
  fail "some graph tests"
fi

# ── Cadence smoke ─────────────────────────────────────────────────────────────
if [ -d "$CADENCE_ROOT" ]; then
  section "Cadence smoke"
  run "cadence: profile doctor" \
    go run ./cmd/awareness profile doctor --project-root "$CADENCE_ROOT"
  run "cadence: preflight --changed" \
    go run ./cmd/awareness preflight --changed --project-root "$CADENCE_ROOT"
  run "cadence: bundle build" \
    go run ./cmd/awareness bundle build --project-root "$CADENCE_ROOT" --out /tmp/cadence-awareness-release-check
  run "cadence: graph build" \
    go run ./cmd/awareness graph build --project-root "$CADENCE_ROOT"
else
  echo "(skipping Cadence smoke — CADENCE_ROOT not found: $CADENCE_ROOT)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "======================================="
echo "Release check: ${PASS} passed, ${FAIL} failed."
echo "======================================="

if [ "$FAIL" -ne 0 ]; then
  echo "NOT READY FOR RELEASE."
  exit 1
fi
echo "READY FOR RELEASE."

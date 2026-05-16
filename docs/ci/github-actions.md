# Using Awareness in GitHub Actions

This guide shows how to run Awareness preflight checks in CI for any project — no Globular cluster required.

---

## Prerequisites

Your repository needs a `.awareness.yaml` file at the root:

```yaml
name: my-project
kind: application

runtime:
  enabled: false
  adapter: null

awareness:
  root: .awareness
  invariants:
    - .awareness/invariants.yaml
  failure_modes:
    - .awareness/failure_modes.yaml
  forbidden_fixes:
    - .awareness/forbidden_fixes.yaml
```

See [adoption/non-globular-project.md](../adoption/non-globular-project.md) for the full setup guide.

---

## Basic Preflight Workflow

```yaml
name: Awareness Preflight

on:
  pull_request:
  push:
    branches: [main, master]

jobs:
  awareness:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Install Awareness
        run: go install github.com/globulario/awareness/cmd/awareness@latest

      - name: Run Awareness preflight (changed files)
        run: awareness preflight --changed --format json
```

The `--changed` flag detects files modified in the current branch (via `git diff HEAD` and `git status`). The `--format json` flag emits machine-readable output.

---

## Fail CI Only on Blocking Findings

To block merges only when specific invariants are violated, parse the JSON output:

```yaml
      - name: Run Awareness preflight and check for blockers
        run: |
          awareness preflight --changed --format json > preflight.json
          cat preflight.json

          # Fail if any failure mode with severity=critical was matched.
          # Adjust the jq filter to match your project's severity rules.
          CRITICAL=$(jq '[.raw_matches[] | select(.kind == "failure_mode" and .score >= 3)] | length' preflight.json)
          if [ "$CRITICAL" -gt 0 ]; then
            echo "Awareness: $CRITICAL critical failure mode(s) matched — review required"
            exit 1
          fi
```

---

## Local Dev Build (module not yet published)

If `github.com/globulario/awareness` is not yet published to the Go module proxy, use a local build:

```yaml
      - name: Build Awareness from source
        run: go build -o /usr/local/bin/awareness ./cmd/awareness
        working-directory: path/to/awareness-source

      - name: Run Awareness preflight
        run: awareness preflight --changed --format json
```

Or run without installing:

```bash
go run ./cmd/awareness preflight --changed --format json
```

---

## Bundle Build in CI

To produce a project Awareness bundle as a CI artifact:

```yaml
      - name: Build Awareness bundle
        run: |
          awareness bundle build \
            --out /tmp/awareness-bundle \
            --revision ${{ github.sha }}

      - uses: actions/upload-artifact@v4
        with:
          name: awareness-bundle
          path: /tmp/awareness-bundle/
```

---

## Standalone MCP Server

To run the Awareness MCP server for AI-assisted workflows in CI:

```bash
awareness-mcp --project-root .
```

Or via the source:

```bash
go run ./cmd/awareness-mcp --project-root .
```

The server listens on stdin/stdout (JSON-RPC 2.0). It selects `NullAdapter` automatically for projects with `runtime.enabled: false`.

# Getting Started With Awareness

This guide takes you from zero to a working Awareness setup in about 10 minutes. No Globular cluster required.

---

## Prerequisites

- Go 1.21 or later
- `~/go/bin` on your `PATH`

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

---

## Step 1 — Install

```bash
go install github.com/globulario/awareness/cmd/awareness@v0.1.0
go install github.com/globulario/awareness/cmd/awareness-mcp@v0.1.0
```

Verify:

```bash
awareness --help
awareness-mcp --help
```

---

## Step 2 — Create a Project Profile

In your project root, create a `.awareness.yaml` file:

```bash
cd my-project
mkdir -p .awareness

cat > .awareness.yaml <<'YAML'
project:
  name: my-project
  kind: generic

runtime:
  enabled: false
  adapter: null

languages:
  go: true
  yaml: true

awareness:
  root: .awareness
YAML
```

**`kind`** classifies your project type. Common values: `generic`, `application`, `library`, `bpmn-workflow-engine`, `distributed-platform`.

---

## Step 3 — Write an Invariant

Create `.awareness/invariants.yaml`:

```yaml
invariants:
  - id: no.silent.errors
    title: Errors must not be silently discarded
    description: >
      Every error must be returned, logged, or wrapped. Using _ to
      discard an error return is forbidden. Silent errors hide bugs and
      make incidents undiagnosable.
    severity: critical
    tags: [errors, reliability]

  - id: no.global.mutable.state
    title: Global mutable state is forbidden
    description: >
      Package-level variables must be read-only after init().
      Mutable globals cause data races and non-deterministic tests.
    severity: error
    tags: [concurrency, testing]
```

---

## Step 4 — Write a Failure Mode

Create `.awareness/failure_modes.yaml`:

```yaml
failure_modes:
  - id: error.swallowed.on.retry
    title: Error swallowed silently on retry causes invisible data loss
    description: >
      A handler that catches an error and retries without logging or
      returning it masks failures. The caller sees success but the
      operation may have only partially completed.
    symptoms:
      - missing records with no error in logs
      - count divergence between caller and store
    severity: critical
    tags: [errors, retry, reliability]
```

---

## Step 5 — Write a Forbidden Fix

Create `.awareness/forbidden_fixes.yaml`:

```yaml
forbidden_fixes:
  - id: no.discard.error.with.underscore
    title: Do not discard errors with _ assignment
    description: >
      Using `_, err = op()` and then not checking err, or `_ = op()`
      is forbidden. Always return, log, or wrap the error.
    tags: [errors, reliability]
```

---

## Step 6 — Run Profile Doctor

```bash
awareness profile doctor
```

Example output:

```
project:   my-project
kind:      generic
root:      /path/to/my-project
runtime:   runtime_disabled

checks:
  ✓ awareness.root   /path/to/my-project/.awareness
  ✓ invariants       /path/to/my-project/.awareness/invariants.yaml
  ✓ failure_modes    /path/to/my-project/.awareness/failure_modes.yaml
  ✓ forbidden_fixes  /path/to/my-project/.awareness/forbidden_fixes.yaml
```

---

## Step 7 — Run Preflight

```bash
awareness preflight --changed
```

Or for a specific task:

```bash
awareness preflight --task "fix error handling in the payment handler"
```

For JSON output (for CI):

```bash
awareness preflight --changed --format json
```

---

## Step 8 — Start the MCP Server

```bash
awareness-mcp --project-root .
```

The server listens on stdin/stdout and serves 9 tools to any MCP-capable AI agent.

For Claude Code, add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/absolute/path/to/my-project"]
    }
  }
}
```

---

## Step 9 — Build a Bundle

```bash
awareness bundle build --project-root . --out /tmp/my-project-bundle
```

This creates a portable directory with `bundle.json` and copies of your knowledge files. Use it for CI artifacts, distribution, or agent context.

---

## Step 10 — Add to CI

```yaml
- name: Install Awareness
  run: go install github.com/globulario/awareness/cmd/awareness@v0.1.0

- name: Awareness preflight
  run: |
    awareness preflight --changed --format json > preflight.json
    MATCH_COUNT=$(jq '(.invariants | length) + (.failure_modes | length)' preflight.json)
    if [ "$MATCH_COUNT" -gt 0 ]; then
      echo "Awareness: $MATCH_COUNT knowledge items in scope"
      jq '{invariants, failure_modes, forbidden_fixes}' preflight.json
    fi
```

---

## What's Next

- Read [concepts/invariants.md](concepts/invariants.md) to write better invariants
- Read [concepts/failure-modes.md](concepts/failure-modes.md) for failure mode patterns
- See [mcp/claude-code.md](mcp/claude-code.md) for full MCP configuration
- See [ci/github-actions.md](ci/github-actions.md) for complete CI integration

---

## Troubleshooting

**`awareness: command not found`**
Add Go binaries to your PATH: `export PATH="$PATH:$(go env GOPATH)/bin"`

**`could not find .awareness.yaml`**
Run `awareness profile doctor` from your project root, or pass `--project-root /path/to/project`.

**`runtime: runtime_disabled`**
This is expected for non-Globular projects. All preflight, context, and bundle tools work normally without a runtime adapter.

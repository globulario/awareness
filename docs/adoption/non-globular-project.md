# Adding Awareness to a Non-Globular Project

Awareness works on any repository — Go, TypeScript, mixed, BPMN, or any other. You do not need a Globular cluster.

Projects that use `runtime.enabled: false` (the default for non-Globular projects) run with `NullAdapter`. The NullAdapter returns `runtime_disabled` for any runtime-only call and never tries to connect to a cluster.

---

## Step 1 — Add `.awareness.yaml`

Create `.awareness.yaml` at your project root:

```yaml
name: my-project
kind: application   # application | library | bpmn-workflow-engine | distributed-platform

languages:
  go: true          # set the languages present in your repo

source_roots:
  - cmd
  - internal
  - pkg

awareness:
  root: .awareness
  invariants:
    - .awareness/invariants.yaml
  failure_modes:
    - .awareness/failure_modes.yaml
  forbidden_fixes:
    - .awareness/forbidden_fixes.yaml
  decisions_dir: .awareness/decisions

runtime:
  enabled: false
  adapter: null     # null = NullAdapter, no cluster required

graph:
  cache_dir: .awareness/cache
  freshness_ttl: 24h
```

---

## Step 2 — Create the `.awareness/` directory

```bash
mkdir -p .awareness/decisions .awareness/proposals .awareness/cache
```

---

## Step 3 — Write invariants

Create `.awareness/invariants.yaml`:

```yaml
invariants:
  - id: api.backwards.compatibility
    title: Public API must remain backwards-compatible across minor versions
    description: >
      Removing or renaming exported types, functions, or interfaces in a minor
      release breaks downstream consumers. Use deprecation notices and keep the
      old symbol for at least one major version.
    severity: critical
    tags: [api, compatibility]

  - id: error.wrapping.required
    title: Errors must be wrapped with context before propagation
    description: >
      Every error returned from a package boundary must include enough context
      to locate the call site without a stack trace. Use fmt.Errorf("verb: %w", err).
    severity: error
    tags: [errors, debugging]
```

---

## Step 4 — Write failure modes

Create `.awareness/failure_modes.yaml`:

```yaml
failure_modes:
  - id: goroutine.leak.on.cancel
    title: Goroutine leaks when context is cancelled before channel receive
    description: >
      A goroutine that blocks on a channel receive without a context select
      will leak when the caller cancels. Use select with ctx.Done() on every
      blocking receive.
    symptoms:
      - goroutine count grows linearly with request rate
      - pprof goroutine dump shows blocked channel receives
    severity: error
    tags: [concurrency, goroutine, context]
```

---

## Step 5 — Write forbidden fixes

Create `.awareness/forbidden_fixes.yaml`:

```yaml
forbidden_fixes:
  - id: no.panic.in.http.handler
    title: Do not call panic() inside an HTTP handler
    description: >
      panic() in an HTTP handler kills the entire server unless there is a
      recover() at the top of every request path. Use error returns instead.
    pattern: "panic("
    applies_to: [http, handler]
    rationale: Enforces api.backwards.compatibility and prevents outage.
```

---

## Step 6 — Verify

```bash
# Resolve profile
awareness profile doctor

# Run preflight on changed files
awareness preflight --changed

# Run preflight for a specific task
awareness preflight --task "add new public API method"
```

Expected output for a healthy project:

```text
project: my-project
kind:    application
root:    /home/user/my-project
profile: /home/user/my-project/.awareness.yaml
runtime: disabled

status: ok
```

---

## How NullAdapter Works

`runtime.enabled: false` selects `NullAdapter`. The NullAdapter:

- returns `runtime_disabled` for `adapter.Doctor()`
- returns empty `RuntimeSignals` for `adapter.CollectSignals()`
- never opens a network connection
- never imports Globular packages

This means all Awareness features that operate on static project knowledge (invariants, failure modes, forbidden fixes, preflight, MCP context tools) work without a cluster. Only the Globular-specific runtime tools (`live_collect_signals`, `live_convergence`, etc.) are disabled.

---

## MCP Server for AI Agents

Run the standalone MCP server so AI agents (Claude Code, Codex, etc.) get project-aware context:

```bash
awareness-mcp --project-root /path/to/my-project
```

Or configure it in `.mcp.json`:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/path/to/my-project"]
    }
  }
}
```

The MCP server exposes four tools:

| Tool | Description |
|------|-------------|
| `awareness_profile_doctor` | Static profile health check |
| `awareness_preflight` | Pre-edit preflight with task and file analysis |
| `awareness_runtime_status` | Adapter status (returns `runtime_disabled` for NullAdapter) |
| `awareness_context` | Raw knowledge search across invariants/failure_modes/forbidden_fixes |

---

## CI Integration

See [ci/github-actions.md](../ci/github-actions.md) for GitHub Actions examples.

---

## Real Example: Cadence (BPMN Engine)

The Cadence BPMN workflow engine repository uses Awareness with NullAdapter:

```yaml
# cadence/.awareness.yaml
name: cadence
kind: bpmn-workflow-engine
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

```bash
cd cadence
awareness profile doctor
# project: cadence
# runtime: disabled
# status:  ok

awareness preflight --changed --task "fix gateway condition evaluation"
# Matches: gateway.exclusive.single.exit, no.wall.clock.in.flow.condition
```

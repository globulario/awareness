# Migration Guide — Adding Awareness to Any Repo

This guide walks through adding Awareness to an existing repository. No Globular cluster is required.

---

## Step 1 — Install the CLI

```bash
go install github.com/globulario/awareness/cmd/awareness@latest
go install github.com/globulario/awareness/cmd/awareness-mcp@latest
```

Verify:

```bash
awareness --help
awareness-mcp --help
```

---

## Step 2 — Add `.awareness.yaml`

Create `.awareness.yaml` at your project root (same level as `.git`):

```yaml
name: my-project
kind: application   # application | library | bpmn-workflow-engine | distributed-platform

languages:
  go: true          # set to match your repo
  typescript: false
  proto: false
  yaml: true
  markdown: true

source_roots:
  - src             # directories containing source code
  - cmd
  - internal

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
  enabled: false    # false = NullAdapter, no cluster required
  adapter: null

graph:
  cache_dir: .awareness/cache
  freshness_ttl: 24h
```

The adoption model:
- The `awareness` binary provides the shared engine.
- Each repo carries its own `.awareness.yaml` profile and `.awareness/` knowledge files.
- Runtime integration is optional and adapter-driven. Non-Globular repos always use `NullAdapter`.

---

## Step 3 — Create the `.awareness/` Directory

```bash
mkdir -p .awareness/decisions .awareness/proposals .awareness/cache
echo ".awareness/cache/" >> .gitignore
```

---

## Step 4 — Write `invariants.yaml`

Invariants are rules that must always hold. Start with 3-5 that represent the most important constraints in your project.

```yaml
invariants:
  - id: my.core.invariant
    title: Short title
    description: >
      What must always be true, and what breaks if it is violated.
    severity: critical   # critical | error | warning
    tags: [relevant, keywords]
```

Good invariants describe **consequences** of violation, not just rules. "State X must not be mutated" is weaker than "Mutating state X bypasses change detection and causes stale renders."

---

## Step 5 — Write `failure_modes.yaml`

Failure modes are known ways things break. Describe each in terms of **observable symptoms** so an agent can match them to a real problem.

```yaml
failure_modes:
  - id: my.known.failure
    title: Short title
    description: >
      How this failure happens and what state it leaves the system in.
    symptoms:
      - symptom visible in logs
      - symptom visible in behaviour
    severity: error
    tags: [relevant, keywords]
```

---

## Step 6 — Write `forbidden_fixes.yaml`

Forbidden fixes are patches that look right but cause harm. These often encode hard-won lessons.

```yaml
forbidden_fixes:
  - id: no.bad.pattern
    title: Do not use X to fix Y
    description: >
      Why X looks like it works but actually causes Z.
    pattern: "code pattern to warn on"   # optional grep hint
    applies_to: [domain, layer]
    rationale: Enforces my.core.invariant.
```

---

## Step 7 — Verify the Profile

```bash
awareness profile doctor
```

Expected output:

```text
project: my-project
kind:    application
root:    /path/to/my-project
profile: /path/to/my-project/.awareness.yaml
runtime: disabled

status: ok
```

All listed checks should show `ok`. Warnings for missing `cache_dir` are fine (it's created lazily).

---

## Step 8 — Run Preflight

```bash
awareness preflight --changed --task "add new endpoint"
```

Or in JSON mode:

```bash
awareness preflight --changed --format json | jq .
```

The preflight scans your awareness YAML files and returns matched invariants, failure modes, and forbidden fixes relevant to the changed files and task description.

---

## Step 9 (Optional) — Run the MCP Server

For AI-assisted development, start the MCP server so agents can query project knowledge:

```bash
awareness-mcp --project-root .
```

Configure Claude Code to use it:

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

---

## Step 10 (Optional) — Build an Awareness Bundle

A bundle is a portable snapshot of your project's awareness knowledge:

```bash
awareness bundle build --out /tmp/my-project-bundle
```

Output:

```
bundle: /tmp/my-project-bundle
schema: awareness.bundle.v1
project: my-project (application)
invariants: 1
failure_modes: 1
forbidden_fixes: 1
runtime_signals: false
status: ok
```

Bundles can be used as CI artifacts or distributed to teams.

---

## Step 11 — Add CI

See [`../ci/github-actions.md`](../ci/github-actions.md) for a ready-to-use GitHub Actions workflow.

---

## Examples

Working examples for common project types are in the [`examples/`](../../examples/) directory:

| Example | Kind | Focus |
|---------|------|-------|
| `examples/go-service` | application | Idempotency, context, goroutines |
| `examples/typescript-app` | application | State mutation, API types, errors |
| `examples/bpmn-engine` | bpmn-workflow-engine | Determinism, token lifecycle |
| `examples/mixed-monorepo` | application | Proto compat, shared types, versioning |

Test any example:

```bash
awareness profile doctor --project-root examples/go-service
awareness preflight --changed --project-root examples/go-service
awareness bundle build --project-root examples/go-service --out /tmp/go-service-bundle
```

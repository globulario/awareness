# Invariants

Invariants are laws the code must uphold at all times. They are not guidelines or style preferences — they are non-negotiable rules whose violation causes real problems: data loss, races, incorrect behaviour, or system failure.

The distinction matters for AI agents: an invariant is a hard constraint that the agent must check before making any change that could affect it. When preflight returns an invariant, the agent must verify that its proposed change does not break that invariant, not just note it and move on.

---

## YAML Format

Invariants live in one or more YAML files listed under `awareness.invariants` in `.awareness.yaml`. Each file uses a top-level `invariants` key containing a list of entries.

```yaml
invariants:
  - id: <dot.separated.id>
    title: <short human-readable title>
    description: >
      Multi-line description of the law. Be specific about what must be true,
      what breaks when it is violated, and what the correct pattern is.
    severity: critical | error | warning
    tags:
      - tag1
      - tag2
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Dot-separated identifier. Used in tool output, preflight results, and session records. Must be unique across all invariant files for the project. |
| `title` | yes | One-line description. Shown in preflight output and agent context. |
| `description` | yes | Full explanation. Should describe what must be true, the consequence of violation, and the correct pattern. |
| `severity` | yes | `critical`, `error`, or `warning`. See below. |
| `tags` | no | Keywords used for search and matching. Include the domain, pattern name, and any identifiers an agent might search for. |

### Severity Values

| Severity | Meaning |
|----------|---------|
| `critical` | Violation causes data corruption, security breach, or system failure. An agent must not proceed with a change that violates a critical invariant. |
| `error` | Violation causes incorrect behaviour, resource leaks, or broken reliability guarantees. Requires deliberate justification before proceeding. |
| `warning` | Violation is a code quality or maintainability problem. The agent should note it but may proceed if there is a clear reason. |

---

## Good Invariants

A good invariant is specific, falsifiable, and explains the consequence of violation.

```yaml
invariants:
  - id: idempotency.required
    title: All mutating operations must be idempotent
    description: >
      Any RPC, HTTP handler, or background job that modifies state must produce
      the same result when called more than once with the same input. Use upsert
      semantics, deduplication keys, or conditional writes. A non-idempotent
      mutation called twice on retry will corrupt state without returning an error.
    severity: critical
    tags: [idempotency, rpc, mutation, retry]

  - id: context.propagation.required
    title: context.Context must be threaded through every call chain
    description: >
      Every function that performs I/O, holds a lock, or calls a downstream
      service must accept a context.Context as its first argument and respect
      cancellation. Blocking calls without context leave goroutines running
      after the caller's deadline expires, causing resource leaks and incorrect
      timeout behaviour.
    severity: critical
    tags: [context, goroutine, timeout, io]

  - id: goroutine.bounds.enforced
    title: Goroutines must be bounded and always terminate
    description: >
      Every goroutine must have a known termination condition: context
      cancellation, channel close, or an explicit stop signal. Spawning
      goroutines in a loop without a bound or a done-signal leaks memory
      proportional to request volume. Use a bounded worker pool or a semaphore.
    severity: error
    tags: [goroutine, resource, concurrency, leak]

  - id: no.global.mutable.state
    title: Global mutable state is forbidden outside explicit registries
    description: >
      Package-level variables must be read-only after init(). Mutable global
      state causes data races, makes tests non-deterministic, and breaks
      concurrent request handling. Use dependency injection or per-instance
      state instead. The only exception is a package-level registry that uses
      a sync.RWMutex and is explicitly documented as a registry.
    severity: critical
    tags: [concurrency, testing, state, race]
```

---

## Bad Invariants

Bad invariants are vague, untestable, or duplicate what a linter already enforces.

```yaml
# BAD — too vague, not actionable
- id: write.good.code
  title: Code should be of high quality
  description: Write code that is readable and maintainable.
  severity: warning

# BAD — a linter rule, not an invariant
- id: use.camelcase
  title: Variable names should use camelCase
  description: Follow naming conventions.
  severity: warning

# BAD — no consequence described
- id: add.tests
  title: Add tests for new code
  description: New code should have tests.
  severity: warning
```

Invariants should express domain laws that a linter cannot check — architectural constraints, protocol rules, and safety properties specific to your project.

---

## Where to Put Invariants

The convention is a `.awareness/` directory at the project root:

```
my-project/
  .awareness.yaml
  .awareness/
    invariants.yaml
    failure_modes.yaml
    forbidden_fixes.yaml
```

For large projects, split by subsystem:

```
.awareness/
  invariants.yaml           # core-wide laws
  invariants-storage.yaml   # storage-specific laws
  invariants-network.yaml   # network-specific laws
```

List all files under `awareness.invariants` in `.awareness.yaml`:

```yaml
awareness:
  invariants:
    - .awareness/invariants.yaml
    - .awareness/invariants-storage.yaml
    - .awareness/invariants-network.yaml
```

---

## How Agents Use Invariants

### Preflight

`awareness preflight` scans invariant files for entries whose `id`, `title`, `description`, and `tags` match the task description or changed file paths. Matched invariants are returned with a score:

```json
{
  "invariants": ["idempotency.required", "context.propagation.required"],
  "raw_matches": [
    { "kind": "invariant", "id": "idempotency.required", "score": 3 },
    { "kind": "invariant", "id": "context.propagation.required", "score": 2 }
  ]
}
```

The agent must read each matched invariant and verify that the proposed change does not violate it.

### MCP Tool: awareness_invariant_lookup

Search invariants interactively by keyword:

```json
{
  "name": "awareness_invariant_lookup",
  "arguments": { "query": "goroutine context cancel", "limit": 5 }
}
```

Returns full invariant entries including id, title, description, severity, tags, and source file path.

### MCP Tool: awareness_node_context

Look up invariants related to a specific file:

```json
{
  "name": "awareness_node_context",
  "arguments": { "path": "internal/service/handler.go" }
}
```

---

## See Also

- [examples/go-service/.awareness/invariants.yaml](../../examples/go-service/.awareness/invariants.yaml) — Working example
- [examples/bpmn-engine/.awareness/invariants.yaml](../../examples/bpmn-engine/.awareness/invariants.yaml) — Workflow engine example
- [failure-modes.md](failure-modes.md) — Known failure patterns
- [forbidden-fixes.md](forbidden-fixes.md) — Patches that must not be applied

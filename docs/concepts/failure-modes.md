# Failure Modes

Failure modes are known ways the system can break. They are not abstract risk categories — they are concrete failure patterns that have been observed (or foreseen based on architecture) and written down so that agents can recognize them when symptoms appear.

The key insight: an agent that has seen a failure mode can recognize the pattern from symptoms alone, without needing to re-derive the root cause from first principles. "Goroutine count growing linearly under pprof" is a symptom. The failure mode tells the agent what it means and — crucially — what not to do about it.

---

## YAML Format

Failure modes live in one or more YAML files listed under `awareness.failure_modes` in `.awareness.yaml`. Each file uses a top-level `failure_modes` key.

```yaml
failure_modes:
  - id: <dot.separated.id>
    title: <short description of the failure>
    description: >
      What happens when this failure mode is active. Include the mechanism,
      not just the outcome. Explain why the system fails this way.
    symptoms:
      - Observable symptom 1 (what an operator or agent would see)
      - Observable symptom 2
    wrong_fixes:
      - A plausible but incorrect fix that makes things worse or masks the problem
      - Another wrong fix
    severity: critical | error | warning
    tags:
      - tag1
      - tag2
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Dot-separated identifier. Unique across all failure mode files for the project. |
| `title` | yes | One-line description of the failure. |
| `description` | yes | Mechanism of failure. Explain why it happens, not just that it happens. |
| `symptoms` | yes | Observable indicators. Write what a developer, agent, or monitoring system would actually see — log messages, metric patterns, tool output. |
| `wrong_fixes` | no | Fixes that look plausible but are incorrect. This is the most valuable field for agents: it prevents them from applying a fix that looks reasonable but makes the situation worse. |
| `severity` | yes | `critical`, `error`, or `warning`. |
| `tags` | no | Keywords for search and matching. |

---

## Why Symptoms Matter

Symptoms are the bridge between observation and diagnosis. An agent working a live incident sees log output, metric graphs, and command output — not root causes. Symptoms let the agent ask: "does what I'm seeing match a known failure mode?" before attempting a fix.

Good symptoms are specific and observable:

```yaml
symptoms:
  - goroutine count grows linearly with request rate under pprof /debug/pprof/goroutine
  - heap profile shows channel receive stacks accumulating
  - service becomes unresponsive after 10-15 minutes of sustained traffic
  - no errors in logs — service appears healthy from the outside
```

Bad symptoms are vague and require interpretation:

```yaml
symptoms:
  - service is slow        # too vague — could be anything
  - memory increases       # vague — what tool, what rate, what threshold?
  - errors occur           # not specific enough to match
```

---

## Example Failure Modes

### Goroutine Leak on Context Cancel

```yaml
failure_modes:
  - id: goroutine.leak.on.cancel
    title: Goroutine leaks when context is cancelled before channel receive
    description: >
      A goroutine blocking on a channel receive without a context select will
      hang after the caller cancels. The goroutine cannot be collected because
      nothing closes the channel. Under load, leaked goroutines exhaust file
      descriptors and memory. The service appears healthy from the outside
      (no errors returned) because the leaked goroutines are silent.
    symptoms:
      - goroutine count grows linearly with request rate under pprof /debug/pprof/goroutine
      - heap profile shows channel receive stacks accumulating
      - service becomes unresponsive after sustained traffic without any error log entries
    wrong_fixes:
      - Increasing server timeouts — this delays the symptom but does not fix the leak
      - Restarting the service — clears the leak temporarily but the goroutines return immediately
      - Adding a recover() in the handler — the goroutines are not panicking, they are hung
    severity: error
    tags: [goroutine, context, channel, leak, resource]

  - id: double.write.on.retry
    title: Non-idempotent handler writes duplicate records on retry
    description: >
      A handler that inserts a new row without checking for an existing record
      will create duplicates when the client retries after a timeout. The first
      call may have partially succeeded before the connection dropped. The caller
      sees success on the second attempt but the database now has two copies of
      the entity with different primary keys.
    symptoms:
      - duplicate entity IDs appear in the database
      - count divergence between the source-of-truth store and derived views
      - eventual consistency violation on downstream consumers: they process the
        same logical event twice
      - no error in logs on either the first or second call
    wrong_fixes:
      - Deduplicating downstream: treats the symptom, not the cause. The database
        record is already corrupted.
      - Adding a retry limit to the client: reduces frequency but does not prevent
        duplicates when they do occur.
    severity: critical
    tags: [idempotency, database, retry, mutation]
```

---

## How Agents Use Failure Modes

### Preflight

`awareness preflight` scans failure mode files for entries whose fields match the task description or changed file paths. Matched failure modes are returned alongside invariants:

```json
{
  "failure_modes": ["goroutine.leak.on.cancel"],
  "raw_matches": [
    { "kind": "failure_mode", "id": "goroutine.leak.on.cancel", "score": 4 }
  ]
}
```

When a failure mode matches, the agent should check `wrong_fixes` before proposing any remediation. If the proposed fix appears in `wrong_fixes`, it must be rejected.

### MCP Tool: awareness_failure_mode_lookup

Search failure modes interactively by keyword:

```json
{
  "name": "awareness_failure_mode_lookup",
  "arguments": { "query": "goroutine leak context cancel channel", "limit": 5 }
}
```

Returns full failure mode entries including id, title, description, symptoms, wrong_fixes, severity, tags, and source file path.

### MCP Tool: awareness_context

Broad search across all knowledge files (invariants, failure modes, forbidden fixes) for a query:

```json
{
  "name": "awareness_context",
  "arguments": { "query": "retry idempotency duplicate write" }
}
```

---

## Where to Put Failure Modes

Same convention as invariants — a `.awareness/` directory at the project root:

```
my-project/
  .awareness/
    failure_modes.yaml
```

For large projects, split by subsystem. List all files under `awareness.failure_modes` in `.awareness.yaml`:

```yaml
awareness:
  failure_modes:
    - .awareness/failure_modes.yaml
    - .awareness/failure_modes-storage.yaml
```

---

## See Also

- [examples/go-service/.awareness/failure_modes.yaml](../../examples/go-service/.awareness/failure_modes.yaml) — Working example
- [examples/bpmn-engine/.awareness/failure_modes.yaml](../../examples/bpmn-engine/.awareness/failure_modes.yaml) — Workflow engine example
- [invariants.md](invariants.md) — Laws the code must uphold
- [forbidden-fixes.md](forbidden-fixes.md) — Patches that must not be applied

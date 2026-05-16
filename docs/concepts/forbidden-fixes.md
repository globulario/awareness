# Forbidden Fixes

Forbidden fixes are patches that look plausible but are wrong for this project. They are the class of "obvious" solutions that an agent — or a developer under time pressure — might reach for, but that the project has learned, through experience, to avoid.

---

## The Difference Between Invariants and Forbidden Fixes

Both invariants and forbidden fixes constrain what agents may do. The distinction is:

- **Invariants** say what must be true about the code at all times. They are laws on the system's state and behaviour. Example: "all mutating operations must be idempotent."
- **Forbidden fixes** say what must not be done as a remedy when something is broken. They are constraints on the repair action, not the code itself. Example: "do not fix a retry-loop bug by adding `time.Sleep()` in the handler."

An agent can satisfy an invariant by writing correct code in any number of ways. A forbidden fix rules out a specific approach that would violate a deeper principle, even though it appears to fix the surface symptom.

---

## YAML Format

Forbidden fixes live in one or more YAML files listed under `awareness.forbidden_fixes` in `.awareness.yaml`. Each file uses a top-level `forbidden_fixes` key.

```yaml
forbidden_fixes:
  - id: <dot.separated.id>
    title: <short description of what must not be done>
    description: >
      Why this fix is wrong. Explain what it appears to fix, what it actually
      does, and what the correct approach is instead.
    applies_when: >
      Optional. Describe the symptom or situation that tempts an agent into
      applying this fix.
    tags:
      - tag1
      - tag2
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Dot-separated identifier. Unique across all forbidden fix files for the project. |
| `title` | yes | One-line description of the fix that must not be applied. |
| `description` | yes | Explanation of why the fix is wrong and what the correct approach is. |
| `applies_when` | no | The symptom or condition that makes this fix tempting. Helps agents recognize when they are about to make this mistake. |
| `tags` | no | Keywords for search and matching. |

---

## Examples

### General Go Service

```yaml
forbidden_fixes:
  - id: no.time.sleep.in.handler
    title: Do not use time.Sleep() inside an HTTP or RPC handler
    description: >
      time.Sleep() blocks the goroutine and ignores context cancellation. If
      a handler is slow because a downstream dependency is slow, adding a sleep
      makes the handler slower and still non-cancellable. Use time.After with
      a select{}, or context.WithTimeout, so the wait respects the request
      deadline and releases the goroutine when the caller cancels.
    applies_when: >
      A handler is failing with timeout errors or returning too quickly,
      and the fix appears to be "give it more time."
    tags: [handler, rpc, http, context, goroutine, sleep]

  - id: no.goroutine.in.init
    title: Do not spawn goroutines in init() functions
    description: >
      Goroutines started in init() have no context, no shutdown signal, and
      run before the program has fully initialised. They cannot be tested in
      isolation, cannot be cancelled, and cannot be observed during graceful
      shutdown. Move background work to an explicit Start() method that receives
      a context.Context.
    applies_when: >
      A background task is needed at startup and init() seems like the
      most convenient place to start it.
    tags: [goroutine, init, startup, context]

  - id: no.discard.error.with.underscore
    title: Do not discard errors with _ assignment
    description: >
      Using `_ = op()` or checking `if err != nil {}` and then doing nothing
      silently drops errors. Silently discarded errors turn real failures into
      invisible data loss. Always return, log at error level, or wrap the error.
      If the error is genuinely ignorable, add a comment explaining why.
    applies_when: >
      A function returns an error that the current code path does not know
      how to handle, and suppressing it seems simpler than propagating it.
    tags: [errors, reliability, silent-failure]

  - id: no.package.level.var.mutex
    title: Do not use a package-level var to hold a sync.Mutex or sync.RWMutex
    description: >
      A package-level mutex is effectively a global lock. It serialises all
      callers across the entire binary, cannot be tested in isolation, and
      creates a hidden coupling between unrelated code paths. Embed the mutex
      in the struct that owns the data it protects.
    applies_when: >
      Multiple goroutines need to access shared state, and adding a global
      mutex appears to be a quick fix.
    tags: [concurrency, mutex, state, global]
```

### Distributed System

```yaml
forbidden_fixes:
  - id: no.localhost.for.remote-address
    title: Do not use localhost or 127.0.0.1 for a remote service address
    description: >
      Using localhost as a remote address works only when both services are
      on the same machine. In a distributed system, service addresses must be
      resolved from the configuration store (e.g. etcd) or service discovery.
      A hardcoded localhost address silently breaks on any node other than the
      one where it was written.
    applies_when: >
      A service is not reachable and adding localhost as a fallback appears
      to fix the immediate connection error in local testing.
    tags: [networking, address, distributed, hardcoded]

  - id: no.hardcoded.port-for-service
    title: Do not hardcode gRPC service port numbers
    description: >
      Service ports are assigned by the platform configuration store, not by
      the service code. Hardcoding a port makes the binary unable to run with
      a different configuration and breaks multi-instance deployments. Read the
      port from configuration at startup.
    applies_when: >
      A service connection fails and the fix appears to be specifying the
      port explicitly in the source code.
    tags: [networking, port, grpc, configuration, hardcoded]
```

---

## How Agents Use Forbidden Fixes

### Preflight

`awareness preflight` returns forbidden fixes whose fields match the task description or changed files:

```json
{
  "forbidden_fixes": ["no.time.sleep.in.handler"],
  "raw_matches": [
    { "kind": "forbidden_fix", "id": "no.time.sleep.in.handler", "score": 3 }
  ]
}
```

When a forbidden fix matches, the agent must check whether its proposed change falls into that category. If it does, the agent must choose a different approach.

### MCP Tool: awareness_context

`awareness_context` searches across all knowledge files including forbidden fixes:

```json
{
  "name": "awareness_context",
  "arguments": { "query": "sleep handler timeout" }
}
```

### MCP Tool: awareness_node_context

`awareness_node_context` returns forbidden fixes related to a specific file path or node ID:

```json
{
  "name": "awareness_node_context",
  "arguments": { "path": "internal/handler/payment.go" }
}
```

---

## Where to Put Forbidden Fixes

Same convention as invariants and failure modes:

```
my-project/
  .awareness/
    forbidden_fixes.yaml
```

List the file under `awareness.forbidden_fixes` in `.awareness.yaml`:

```yaml
awareness:
  forbidden_fixes:
    - .awareness/forbidden_fixes.yaml
```

---

## See Also

- [examples/go-service/.awareness/forbidden_fixes.yaml](../../examples/go-service/.awareness/forbidden_fixes.yaml) — Working example
- [invariants.md](invariants.md) — Laws the code must uphold
- [failure-modes.md](failure-modes.md) — Known failure patterns

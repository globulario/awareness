# Example: Go Service

This example shows how to add Awareness to a Go service.

## Invariants

- **idempotency.required** — All mutating operations must be idempotent (upsert semantics).
- **context.propagation.required** — `context.Context` threaded through every call chain.
- **goroutine.bounds.enforced** — Goroutines must be bounded and always terminate.
- **no.global.mutable.state** — Package-level variables must be read-only after `init()`.
- **interface.boundary.discipline** — Exported interfaces may only grow.

## Quick start

```bash
awareness profile doctor  --project-root .
awareness preflight       --task "add new Create endpoint"
awareness bundle build    --out /tmp/go-service-bundle
```

## Source structure

```
internal/service/handler.go   — idiomatic Create with upsert semantics
```

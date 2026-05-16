# Example: Mixed Monorepo

This example shows how to add Awareness to a monorepo with Go backend, TypeScript frontend, and proto contracts.

## Invariants

- **proto.backwards.compatibility** — Proto changes must be backwards-compatible.
- **backend.frontend.contract.versioned** — API breaking changes require version bump.
- **shared.types.single.source** — Shared types defined once in proto, generated for each language.
- **docs.kept.current** — Architecture docs must reflect the current system.

## Quick start

```bash
awareness profile doctor  --project-root .
awareness preflight       --task "add new user API endpoint"
awareness bundle build    --out /tmp/mixed-monorepo-bundle
```

## Source structure

```
backend/server.go    — Go HTTP server
frontend/           — TypeScript frontend (placeholder)
proto/              — Proto contracts (placeholder)
docs/               — Architecture docs (placeholder)
```

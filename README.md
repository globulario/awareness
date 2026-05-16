# Awareness

Awareness is a project-aware reasoning engine for AI-assisted software maintenance.

It builds and queries a knowledge graph from source code, invariants, failure modes, forbidden fixes, design decisions, and optional runtime facts.

The engine is generic. Each repository carries its own `.awareness.yaml` profile and `.awareness/` knowledge directory.

Globular is the first rich runtime adapter, but not a dependency of this module.

---

## How It Works

```
Repository
  .awareness.yaml          ← project identity, paths, runtime config
  .awareness/
    invariants.yaml        ← what must always be true
    failure_modes.yaml     ← known ways things break
    forbidden_fixes.yaml   ← patches that look right but cause harm
    decisions/             ← architectural decision records
    cache/
      graph.db             ← built knowledge graph (gitignored)

Awareness engine
  reads .awareness.yaml
  builds knowledge graph from source + invariants + failure modes
  evaluates preflight requests from AI agents
  answers "what does changing this file affect?"

RuntimeAdapter (optional)
  NullAdapter              ← default, no live system required
  GlobularAdapter          ← Globular cluster facts (lives in services repo)
```

---

## Quick Start

Add a `.awareness.yaml` to your project root:

```yaml
name: my-project
kind: application

languages:
  go: true

runtime:
  enabled: false
  adapter: null

graph:
  cache_dir: .awareness/cache
  freshness_ttl: 24h
```

Then:

```bash
cd my-project
awareness profile doctor
```

---

## Using Awareness With Cadence

Cadence uses `NullAdapter` — no Globular cluster required.

```yaml
# cadence/.awareness.yaml
name: cadence
kind: application
runtime:
  enabled: false
  adapter: null
```

```bash
cd cadence
awareness profile doctor
# project: cadence
# runtime: disabled
# status:  ok
```

---

## Using Awareness With Globular

Globular uses `GlobularAdapter` (defined in `services/golang/awareness/runtime/`).

```yaml
# services/.awareness.yaml
name: globular-services
kind: distributed-platform
runtime:
  enabled: true
  adapter: globular
```

```bash
cd services
awareness profile doctor
# project: globular-services
# runtime: adapter:globular
# status:  ok
```

---

## Module Boundary

This module must never import `github.com/globulario/services`.

```bash
grep -R "github.com/globulario/services" .
# expected: no matches
```

The `GlobularAdapter` (which may import etcd, workflow protos, and cluster services) lives in the services repository and is injected at startup.

---

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the full migration plan and module boundary diagram.

---

## Development

```bash
go test ./...           # run all tests
go build ./...          # build all packages
go run ./cmd/awareness  # CLI smoke test
```

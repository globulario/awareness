# Awareness Architecture

## Module Boundary

```
github.com/globulario/awareness          ← this module
  project/      — profile discovery and loading
  runtime/      — RuntimeAdapter interface, NullAdapter
  graph/        — knowledge graph primitives
  finding/      — canonical finding types
  evidence/     — generic evidence primitives
  bundle/       — generic bundle manifest format
  preflight/    — preflight API contracts
  cmd/awareness — CLI

services/golang/awareness                ← Globular implementation (separate module)
  runtime/globular_adapter.go           — GlobularAdapter (imports etcd, workflow, doctor, etc.)
  extractors/                           — Globular runtime evidence collectors
  evidence/                             — Globular-specific evidence pipeline
  graph/                                — production SQLite graph engine (migrating here)
  preflight/                            — production preflight engine (migrating here)
  bundlesync/                           — Globular bundle delivery pipeline
  project/                              — compatibility shim → delegates to this module

mcp server (services/golang/mcp)
  Awareness tool group — resolves ProjectProfile at startup, uses RuntimeAdapter
  Globular runtime tools — cluster, node, etcd, workflow, repository, objectstore
```

## Design Principle

```
The binary provides the shared brainstem.
Each repository carries its own nervous system.
```

- Awareness core is reusable — it reasons about any codebase, not just Globular.
- Project-specific knowledge lives in `.awareness.yaml` and `.awareness/` within each repository.
- Runtime integration happens through `RuntimeAdapter`. Awareness core depends only on the interface.
- `NullAdapter` is the default. Cadence uses NullAdapter successfully.
- `GlobularAdapter` lives in services, not here. It may import etcd, workflow service, doctor, Prometheus.

## Critical Rule

```
Do not make Awareness reusable by deleting Globular knowledge.
Make Awareness reusable by moving Globular knowledge behind a profile and RuntimeAdapter.
```

Globular knowledge is rich and load-bearing. The refactor moves the boundary — it does not erase the knowledge.

## Migration Phases

### Phase A (done): Skeleton
This module. Establishes the clean boundary. Proves `github.com/globulario/awareness` does not depend on `github.com/globulario/services`.

### Phase B: Shared ProjectProfile
`services/golang/awareness` uses the same `ProjectProfile` from this module. CLI and MCP resolve profile once at startup.

### Phase C: RuntimeAdapter Boundary
Move all Globular runtime calls behind `GlobularAdapter` in services. `NullAdapter` passes all core tests without a cluster.

### Phase D: Package Migration
Move generic packages from services into this module one by one:
- `graph/` (production SQLite engine)
- `preflight/` (full preflight engine)
- `scan/`, `analysis/`, `semanticdiff/`, `sessionoracle/`, `failuregraph/`, `fixledger/`
- `assurance/`, `contextfreshness/`, `coordination/`

### Phase E: MCP Project Awareness
MCP Awareness tool group resolves `ProjectProfile` at startup. Both Globular and Cadence work from their respective cwd.

## Forbidden Dependencies

`github.com/globulario/awareness` must never import:

```
github.com/globulario/services
go.etcd.io/etcd
github.com/gocql/gocql
github.com/minio/minio-go
github.com/prometheus/client_golang (runtime client)
```

Check with:
```bash
grep -R "github.com/globulario/services" .
```
Expected: no matches.

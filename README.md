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

## Commands

```bash
# Profile
awareness profile show    [--project-root PATH]
awareness profile doctor  [--project-root PATH]

# Preflight
awareness preflight       [--changed] [--project-root PATH] [--format text|json] [--task TEXT]

# Bundle
awareness bundle build    --out PATH [--project-root PATH] [--revision REV] [--version VER]

# MCP server (standalone, NullAdapter by default)
awareness-mcp             [--project-root PATH]
```

---

## MCP Server

The standalone MCP server (`cmd/awareness-mcp`) exposes project Awareness tools over JSON-RPC 2.0 / stdio. It works without a Globular cluster.

```bash
awareness-mcp --project-root /path/to/project
```

Or via the source:

```bash
go run ./cmd/awareness-mcp --project-root /path/to/project
```

Tools exposed:

| Tool | Description |
|------|-------------|
| `awareness_profile_doctor` | Static profile health check |
| `awareness_preflight` | Pre-edit preflight with task and file analysis |
| `awareness_runtime_status` | Adapter status (`runtime_disabled` for NullAdapter) |
| `awareness_context` | Raw knowledge search across invariants/failure_modes/forbidden_fixes |

---

## Bundle Schema

Every bundle includes a `bundle.json` manifest with `schema_version: "awareness.bundle.v1"`.

Key fields:

| Field | Description |
|-------|-------------|
| `schema_version` | Always `"awareness.bundle.v1"` |
| `project_name` | From `.awareness.yaml` |
| `invariants_paths` | Relative paths to invariant YAML files |
| `failure_modes_paths` | Relative paths to failure_modes YAML files |
| `forbidden_fixes_paths` | Relative paths to forbidden_fixes YAML files |
| `runtime_signals_included` | `false` for NullAdapter bundles |
| `includes_runtime_overlay` | v1 compatibility alias for `runtime_signals_included` |

---

## CI Integration

See [`docs/ci/github-actions.md`](docs/ci/github-actions.md) for GitHub Actions examples.

See [`docs/adoption/non-globular-project.md`](docs/adoption/non-globular-project.md) for the full adoption guide.

---

## Smoke Tests

```bash
bash scripts/smoke-cadence-mcp.sh
```

Verifies: import wall, CLI commands, awareness-mcp tools/list, runtime_disabled behavior.

---

## Development

```bash
go test ./...           # run all tests
go build ./...          # build all packages
go run ./cmd/awareness  # CLI smoke test
go run ./cmd/awareness-mcp --project-root /path/to/project  # MCP server
```

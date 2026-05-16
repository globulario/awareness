# Dogfood: Applying Awareness to the Awareness Repo

This document records the first external dogfood run of Awareness — applying it to itself.

---

## Project

**Name:** awareness
**Repo:** github.com/globulario/awareness
**Kind:** library (Go CLI + library, no runtime cluster)

---

## Why This Project Was Selected

Using a tool on itself is the highest-fidelity proof of concept. If Awareness can describe its own constraints, invariants, and failure modes — and if preflight and MCP tools work against the awareness repo — then the tool is general enough for any Go library project, not just Globular-specific code.

---

## What Was Added

`.awareness.yaml`, `.awareness/invariants.yaml`, `.awareness/failure_modes.yaml`, `.awareness/forbidden_fixes.yaml`.

Key invariants: `import.wall.maintained` (critical), `schema.version.canonical` (critical), `graph.format.stable` (error), `nulladapter.preferred` (error), `test.coverage.per.package` (error).

Key failure modes: `services.import.leak` (critical), `schema.version.drift` (critical), `adapter.boundary.breach` (error).

Key forbidden fixes: `no.hardcode.services.import`, `no.duplicate.schema.version`, `no.silent.graph.truncation`.

---

## What Preflight Found

Running `awareness preflight --changed` immediately surfaced `import.wall.maintained` and `schema.version.canonical` as relevant invariants — all true positives for active Phase K work.

---

## What Was Confusing

- `awareness_graph_query` returns `graph_not_available` until `awareness graph build` is run. This is correct but surprising on first use.
- `awareness graph build` default output path (`awareness.root/cache/graph.json`) must match what MCP tools and `graph inspect` look for.

---

## Dogfood Results

| Check | Result |
|-------|--------|
| `awareness profile doctor` | OK |
| `awareness preflight --changed` | OK |
| `awareness bundle build` | OK |
| `awareness graph build` | OK (12 nodes, 21 edges) |
| All 9 MCP tools | OK |
| Import wall check | OK |

**Conclusion:** Awareness works correctly on itself. NullAdapter, all MCP tools, bundle, graph, and preflight all pass. The dogfood is successful.

# Awareness Module Inventory — Phase 1

Generated: 2026-05-16
Status: INVENTORY ONLY — no code changes made yet

## Purpose

Classify every module in `services/golang/awareness/` to guide the refocus
into a lean standalone repo (`github.com/globulario/awareness`) + a thin
Globular-specific adapter layer in services.

## Labels

| Label | Meaning |
|-------|---------|
| MOVE_GENERIC | No SQLite, no services imports — move to standalone repo |
| KEEP_SERVICES | Imports etcd/workflow/node-agent/repo/objectstore/xDS/ai_memory — stays in services |
| SHRINK | Mixes generic logic with services-specific code — needs split before moving |
| LEGACY | Not used by preflight/MCP/CLI — mark legacy, delete after knowledge preserved |
| DELETE_LATER | Already replaced by standalone — delete after callers removed |

## Standalone Repo State

`github.com/globulario/awareness` already exists with:
- `bundle/` — manifest, build, inspect
- `cmd/awareness` and `cmd/awareness-mcp` — standalone CLI + MCP
- `evidence/` — evidence types
- `finding/` — finding types
- `graph/` — JSON-based graph (no SQLite)
- `knowledge/` — invariants, failure modes, forbidden fixes, incident patterns
- `preflight/` — classify, match, verdict, trace, result
- `project/` — profile, resolver, doctor
- `runtime/` — adapter interface, NullAdapter, signals, registry

Dependencies: `gopkg.in/yaml.v3` only. Zero SQLite. Zero services imports.

---

## Module Classification

### Main Modules

| Module | SQLite? | Services imports? | Standalone equivalent? | Classification | Reason |
|--------|---------|-------------------|------------------------|---------------|--------|
| analysis | NO | NO | NO | MOVE_GENERIC | Pure graph traversal, cycle detection, impact analysis |
| assurance | NO | NO | NO | MOVE_GENERIC | Coverage computation, freshness checks — generic to any knowledge graph |
| bundlesync | NO | NO | bundle/ (partial) | MOVE_GENERIC | Bundle verification and manifests — fold into standalone bundle/ |
| checkedit | NO | NO | NO | MOVE_GENERIC | Edit validation against invariants — pure graph logic |
| context | NO | NO | NO | MOVE_GENERIC | Node context, neighborhoods, explanations — pure graph queries |
| contextfreshness | YES (database/sql) | NO | NO | SHRINK | File-read timestamp tracking; split generic staleness from session binding |
| debugsession | NO | NO | NO | MOVE_GENERIC | Guided investigation plans — pure logic on failure/invariant context |
| enforce | NO | NO | NO | MOVE_GENERIC | Contract enforcement, graph integrity, annotation validation |
| evidence | NO | NO (fsutil only) | evidence/ (partial) | MOVE_GENERIC | Evidence types; minimal fsutil bridge — fold into standalone evidence/ |
| failuregraph | YES (database/sql) | NO | knowledge/ (partial) | MOVE_GENERIC | Failure mode matching; migrate to YAML + in-memory index in standalone |
| failurelearning | YES (database/sql) | NO | NO | MOVE_GENERIC | Failure learning proposals; fold into knowledge/ after SQLite removal |
| fixledger | NO | NO | NO | MOVE_GENERIC | Known fix guardrails — fold into knowledge/forbidden_fixes |
| graph | YES (modernc.org/sqlite, mattn/go-sqlite3) | NO | graph/ (EXISTS) | DELETE_LATER | SQLite graph DB; standalone already has JSON graph — migrate then delete |
| incidentpattern | YES (database/sql) | NO | knowledge/ (partial) | MOVE_GENERIC | Incident pattern matching; migrate to YAML + in-memory in standalone |
| integrity | NO | NO | NO | MOVE_GENERIC | Impact path, trust, cycle detection — pure graph analysis |
| learning | NO | NO | NO | MOVE_GENERIC | Proposal handling, promotion, incident bundles — pure logic |
| livecluster | YES (database/sql) | YES (cluster_controller, cluster_doctor) | NO | KEEP_SERVICES | Live cluster state via doctor/workflow gRPC — must stay in services |
| preflight | NO | NO | preflight/ (EXISTS) | DELETE_LATER | Safety decisions engine; standalone already has equivalent — migrate callers |
| runtime | NO | YES (cluster_controller, cluster_doctor, workflow) | runtime/ (partial — NullAdapter only) | KEEP_SERVICES | Runtime bridge to live cluster — must stay in services as GlobularAdapter |
| scan | NO | NO | NO | MOVE_GENERIC | AST scanning, code pattern detection — generic to any Go codebase |
| selfcheck | NO | NO | NO | MOVE_GENERIC | Self-review, capability gaps — pure logic on knowledge graph |
| semantic | NO | NO | NO | MOVE_GENERIC | Graph traversal, path finding, semantic distance — pure algorithms |
| sessionoracle | YES (database/sql) | YES (ai_memory) | NO | KEEP_SERVICES | Session management with AI memory gRPC bridge — must stay in services |

### Extractors Sub-packages

| Module | SQLite? | Services imports? | Classification | Reason |
|--------|---------|-------------------|---------------|--------|
| extractors/clusterspec | NO | NO | MOVE_GENERIC | Generic cluster spec parsing — no Globular runtime deps |
| extractors/clusterstate | NO | NO | MOVE_GENERIC | Generic cluster state extraction — no Globular runtime deps |
| extractors/dns | NO | NO | MOVE_GENERIC | Generic DNS config parsing |
| extractors/docs | NO | NO | MOVE_GENERIC | Generic markdown/doc extraction |
| extractors/doctor | NO | NO | MOVE_GENERIC | Pure data transformation of doctor reports |
| extractors/goast | NO | NO | MOVE_GENERIC | Generic Go AST analysis |
| extractors/manual | NO | NO | MOVE_GENERIC | YAML knowledge loading — this is core, needed by standalone |
| extractors/metrics | NO | NO | MOVE_GENERIC | Generic metrics data transformation |
| extractors/packages | NO | NO | MOVE_GENERIC | Generic package/artifact metadata parsing |
| extractors/pki | NO | NO | MOVE_GENERIC | Generic certificate parsing |
| extractors/proto | NO | NO | MOVE_GENERIC | Generic proto definition extraction |
| extractors/rbac | NO | NO | MOVE_GENERIC | Generic RBAC policy parsing |
| extractors/scripts | NO | NO | MOVE_GENERIC | Generic build/script extraction |
| extractors/tests | NO | NO | MOVE_GENERIC | Generic test metadata extraction |
| extractors/workflows | NO | NO | MOVE_GENERIC | Pure workflow data structures |
| extractors/workflowstate | NO | YES (workflow/workflowpb) | KEEP_SERVICES | Workflow state via gRPC — must stay in services |

---

## Summary

| Classification | Count | Notes |
|---------------|-------|-------|
| MOVE_GENERIC | 34 | 20 main + 14 extractors — all pure logic |
| KEEP_SERVICES | 4 | livecluster, runtime, sessionoracle, extractors/workflowstate |
| SHRINK | 1 | contextfreshness (split staleness logic from session binding) |
| DELETE_LATER | 2 | graph (SQLite → JSON already in standalone), preflight (already in standalone) |

**SQLite-backed modules: 7** — graph, failuregraph, failurelearning, incidentpattern, contextfreshness, livecluster, sessionoracle

**Services-importing modules: 4** — livecluster, runtime, sessionoracle, extractors/workflowstate

**Key finding:** 34 of 39 modules have zero Globular service imports. Awareness is already decoupled from the rest of services — it just hasn't been extracted yet.

---

## Phased Work Plan

### Phase 2 — Preserve Knowledge (NEXT)
Before deleting or moving any SQLite-backed module, extract all embedded knowledge to YAML:
- `failuregraph` → `.awareness/failure_modes.yaml`
- `incidentpattern` → `.awareness/incident_patterns.yaml`
- `extractors/manual` invariants/failure_modes/forbidden_fixes → confirm YAML exists
- Verify `preflight` rules are captured in standalone

### Phase 3 — Define Lean Core
Validate the standalone knowledge model covers all types:
`Invariant`, `FailureMode`, `ForbiddenFix`, `IncidentPattern`, `EvidenceContract`,
`PreflightVerdict`, `AssuranceReport`, `SelfcheckReport`

No SQLite. YAML loaders + JSON graph cache + in-memory search.

### Phase 4 — Replace Heavy Paths
Migrate MOVE_GENERIC modules to standalone, replacing SQLite paths with YAML/JSON.
Update MCP and CLI callers to import from standalone.

### Phase 5 — Services As GlobularAdapter
Keep services/golang/awareness as:
- GlobularAdapter (implements standalone runtime.Adapter interface)
- livecluster (live cluster signals)
- runtime bridge (doctor, workflow, metrics)
- sessionoracle (AI memory bridge)
- extractors/workflowstate

### Phase 6 — Delete After Proof
Remove SQLite graph module and services preflight module after:
- all callers migrated
- tests pass
- knowledge confirmed preserved

---

## Forbidden Actions

- Do not delete failuregraph/incidentpattern without extracting their YAML knowledge first
- Do not move livecluster/runtime/sessionoracle to standalone (they have services imports)
- Do not introduce SQLite into standalone
- Do not break services MCP or CLI builds at any phase
- Do not remove enforce until selfcheck (which depends on it) is migrated

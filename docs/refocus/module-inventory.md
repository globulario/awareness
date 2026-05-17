# Module Inventory — Lean Awareness Refocus

**Date:** 2026-05-16 (updated 2026-05-17 — post-migration accuracy pass)  
**Phase:** 1 — Inventory only. No code changes.

> **2026-05-17 update:** The SQLite graph migration (commit `0e4ceb8 lean: remove SQLite graph`)
> completed before this document was revised. All references below to `graph/db.go (SQLite)` are
> historical. The `graph/` package in services is now JSON-backed (`store.go` + `store_json.go`,
> no `database/sql` import). `failuregraph/`, `incidentpattern/`, and `sessionoracle/` all use
> JSON file persistence. The "Future Deletion Path" in `phase6-deletion-audit.md` for SQLite
> removal is no longer needed. All six phases of the lean refocus are complete.

## Classification Key

| Label | Meaning |
|-------|---------|
| `KEEP_CORE` | Already in standalone lean core. Keep as-is. |
| `KEEP_SERVICES` | Globular-specific. Must stay in services. Do not move to standalone. |
| `MOVE_GENERIC` | No Globular/SQLite imports. Useful to multiple projects. Candidate for standalone. |
| `SHRINK` | Has valuable logic mixed with heavy machinery. Simplify; extract knowledge; reduce coupling. |
| `LEGACY` | Low daily use. Not on CLI/MCP/preflight hot path. Mark legacy; remove only after knowledge preserved and callers gone. |
| `DELETE_LATER` | Confirmed dead code. Remove only after: no imports, tests pass, knowledge preserved. |

---

## Part 1 — Standalone Awareness (`github.com/globulario/awareness`)

The standalone repo is already lean. It is the target architecture.

### Packages

| Package | Classification | Imports | Daily use | Risk if removed | Notes |
|---------|--------------|---------|-----------|-----------------|-------|
| `project/` | KEEP_CORE | yaml.v3 only | Yes — profile resolution | High — every command depends on it | Profile resolver, doctor, tests |
| `preflight/` | KEEP_CORE | `finding/` only | Yes — main analysis entry | High — CLI and MCP depend on it | Currently thin stubs; full behavior migrating from services |
| `graph/` | KEEP_CORE | stdlib only | Yes — JSON graph cache | Medium — graph queries depend on it | JSON-based, not SQLite. Correct design. |
| `bundle/` | KEEP_CORE | yaml.v3 | Yes — bundle build/inspect | Medium | Manifest, builder, test |
| `runtime/` | KEEP_CORE | `project/` | Yes — adapter boundary | High — defines Adapter interface | NullAdapter, registry, types |
| `finding/` | KEEP_CORE | stdlib | Yes — shared finding type | High — used by preflight and MCP | Severity, Finding struct |
| `evidence/` | KEEP_CORE | stdlib | Yes — evidence types | Low — thin wrapper | Generic evidence primitives |
| `cmd/awareness/` | KEEP_CORE | internal only | Yes — CLI | High — primary user surface | Main CLI |
| `cmd/awareness-mcp/` | KEEP_CORE | internal only | Yes — MCP tools | High — AI agent surface | MCP server, knowledge tools |
| `.awareness/invariants.yaml` | KEEP_CORE | — | Yes — preflight input | High | Project invariants for Awareness itself |
| `.awareness/failure_modes.yaml` | KEEP_CORE | — | Yes — preflight input | High | Failure modes for Awareness itself |
| `.awareness/forbidden_fixes.yaml` | KEEP_CORE | — | Yes — preflight input | High | Forbidden fixes for Awareness itself |
| `scripts/check-import-wall.sh` | KEEP_CORE | — | Yes — release gate | High | Verifies no services imports in standalone |
| `scripts/check-release.sh` | KEEP_CORE | — | Yes — release gate | High | Full release verification |

**Note:** `preflight/` currently holds only the request/result types (stubs). The full preflight engine still lives in `services/golang/awareness/preflight`. This is the primary migration target.

---

## Part 2 — Services Awareness (`github.com/globulario/services/golang/awareness`)

This is the large repo. 340+ Go files across 40+ packages.

**SQLite has been removed.** The graph package (`store.go` + `store_json.go`) is now backed by in-memory maps with JSON file persistence. No `database/sql` or `mattn/go-sqlite3` import exists in any awareness package. The migration is complete.

### `graph/` — JSON Knowledge Graph Engine *(was SQLite; migration complete)*

| Attribute | Value |
|-----------|-------|
| **Classification** | KEEP_SERVICES |
| **Imports** | `sync`, `encoding/json`, `os`, `path/filepath`, `uuid`, stdlib — no SQLite |
| **Daily use** | Used by: `incidentpattern`, `failuregraph`, `assurance`, `selfcheck`, `preflight`, `analysis`, `semantic`, `integrity`, `contextfreshness`, `sessionoracle`, `checkedit` |
| **Risk if removed** | Critical — everything else breaks |
| **Replacement path** | Already the lean design. JSON-backed, in-memory maps, no DB dependency. No further migration needed. |
| **Notes** | `store.go` + `store_json.go` are the JSON persistence layer (split in commit `b73d0f05`). Migration complete. Do NOT move to standalone — Globular graph traversal is services-specific. |

### `failuregraph/` — Failure Knowledge Graph

| Attribute | Value |
|-----------|-------|
| **Classification** | KEEP_SERVICES |
| **Imports** | `graph/` (JSON-backed), `uuid`, stdlib |
| **Daily use** | Used by MCP error matching, preflight warning |
| **Risk if removed** | High — failure knowledge is core Awareness value |
| **Replacement path** | Seeds already preserved in `.awareness/failure_modes.yaml` (Phase 2 complete). The Go JSON store is the lean design — no further migration needed. |
| **Notes** | **Seeds are high-value production knowledge.** 9 YAML seeds cover known Globular incidents. Phase 2 migrated them to `.awareness/failure_modes.yaml`. The store is JSON-backed. |

### `incidentpattern/` — Incident Pattern Matching

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), `uuid`, stdlib |
| **Daily use** | MCP pattern matching before edits |
| **Risk if removed** | Medium — prevents repeated blindness |
| **Replacement path** | The model types (`IncidentPattern`, `PatternFile`, `PatternSymbol`, `FailedFix`) are generic and valuable. The matcher scoring algorithm is valuable. The SQLite store can be replaced by YAML-backed in-memory store. Phase 2: extract patterns to YAML. Phase 4: rebuild matcher on YAML loader. |
| **Notes** | Scoring logic uses well-calibrated weights (file=0.20, symbol=0.20, invariant=0.25, failed-fix=0.30). Preserve these in the lean matcher. |

### `evidence/` — Evidence Facts and Contracts

| Attribute | Value |
|-----------|-------|
| **Classification** | MOVE_GENERIC → SHRINK |
| **Imports** | `graph/` (SQLite for some files), stdlib for facts |
| **Daily use** | Evidence collection and classification |
| **Risk if removed** | Medium — evidence contracts prevent wrong inference |
| **Replacement path** | `facts.go` (FactKind constants) has no Globular imports — move to standalone. Evidence contracts can be YAML files. The Globular-specific fact collection (systemd, PKI, xDS) stays in services. |
| **Notes** | Split: generic FactKind + EvidenceContract → standalone; Globular-specific evidence collectors → services. |

### `fixledger/` — Fix Tracking

| Attribute | Value |
|-----------|-------|
| **Classification** | MOVE_GENERIC |
| **Imports** | `gopkg.in/yaml.v3`, stdlib only — **no Globular, no SQLite** |
| **Daily use** | Fix case tracking, guardrail matching |
| **Risk if removed** | Medium — encodes fix obligations and forbidden patterns |
| **Replacement path** | Already YAML-based and generic. `FixCase`, `Guardrail`, `FixStatus` types and YAML loaders are clean. Merge concepts into `.awareness/forbidden_fixes.yaml`. Consider moving loader to standalone `knowledge/` package. |
| **Notes** | This is already the lean design. The model is right. The question is whether standalone needs the loader or if YAML files suffice. |

### `failurelearning/` — Learning From Failures

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite) |
| **Daily use** | Proposal review, seed writing, closure hooks |
| **Risk if removed** | Low for core; valuable knowledge in the workflow |
| **Replacement path** | Fold useful behavioral patterns into failure modes and incident patterns (YAML). The seed-writing mechanism (`seed_writer.go`) is the most valuable part — it shows how to convert incidents to seeds. Keep the seed format, retire the DB-backed workflow. |
| **Notes** | This package feeds `failuregraph/seeds/`. Preserve the seed format as the canonical knowledge artifact. |

### `learning/` — Knowledge Promotion Workflow

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK → LEGACY |
| **Imports** | `graph/` (SQLite) |
| **Daily use** | Proposal lifecycle, alias management |
| **Risk if removed** | Low — mostly workflow ceremony |
| **Replacement path** | The `Proposal` struct and promotion model encode useful concepts. The YAML formats in `testdata/` show what good proposals look like. Fold into a simple YAML-based pending-proposals list. Retire the SQLite-backed workflow. |
| **Notes** | The `aliases.go` (mapping error aliases to canonical patterns) is potentially valuable. |

### `assurance/` — Coverage and Assurance Reports

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), stdlib |
| **Daily use** | Coverage reports, freshness checks |
| **Risk if removed** | Medium — prevents fake confidence |
| **Replacement path** | The coverage concept (show what Awareness covers vs. misses) is valuable. The implementation is graph-coupled. Phase 3: rebuild over YAML knowledge index. Assurance should answer: "which invariants have tests? which failure modes have patterns? which patterns are stale?" |
| **Notes** | `detector_lifecycle.go` and `envelope.go` encode important operational knowledge about what detectors are active and what they cover. |

### `selfcheck/` — Awareness Self-Validation

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/`, `analysis/`, `checkedit/`, `debugsession/`, `enforce/`, `preflight/`, `semantic/` |
| **Daily use** | Self-health check for Awareness knowledge base |
| **Risk if removed** | Medium — detects stale/disconnected knowledge |
| **Replacement path** | The checks are valuable (disconnected invariants, failure modes without tests, forbidden fixes not referenced). The implementation is over-coupled. Phase 3: rebuild as simple YAML checker that runs over `.awareness/*.yaml` files. Should take <100ms and produce a clean report. |
| **Notes** | Heavy coupling to many packages. The lean version should depend only on the YAML knowledge model. |

### `preflight/` — Full Preflight Engine

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK → migrate to standalone |
| **Imports** | `graph/` (SQLite), `analysis/`, `contextnav/`, `runtime/`, `semantic/`, `incidentpattern/`, `integrity/`, `failuregraph/` |
| **Daily use** | The real preflight engine — used by MCP |
| **Risk if removed** | Critical — this is what services uses for real preflight |
| **Replacement path** | This is the primary migration target. The lean standalone `preflight/` package currently has only stubs. Phase 3 should rebuild preflight over YAML knowledge (invariants, failure modes, forbidden fixes, incident patterns) WITHOUT graph DB. The output: `PreflightVerdict` with proceed/warn/block/ask_for_evidence + decision trace. |
| **Notes** | `classify.go` (task classification), `format.go` (report formatting), `report.go` (report structure), `raw_fallback.go` (fallback when graph is unavailable) are the most migration-ready files. |

### `analysis/` — Code and Impact Analysis

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), `integrity/` |
| **Daily use** | Impact path, cycle detection, service review |
| **Risk if removed** | Medium — some preflight and MCP tools use it |
| **Replacement path** | Generic analysis concepts (impact paths, package admission, cycle detection) can work over the JSON graph cache. Separate generic analysis from graph-DB-specific queries. |
| **Notes** | `service_review.go` is Globular-specific. `impact.go` and `cycles.go` are generic. |

### `analysis/contextnav/` — Evidence Navigation

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), `analysis/` |
| **Daily use** | MCP context navigation tools |
| **Risk if removed** | Medium — MCP context tools depend on it |
| **Replacement path** | The navigation concept (find related nodes, pivots, falsifiers, finding context) is valuable for MCP. Phase 3: implement over JSON graph + YAML knowledge. |
| **Notes** | `pivots.go`, `falsifiers.go`, `finding.go` encode real navigation logic. Worth migrating to lean implementation. |

### `semantic/` — Semantic Scoring

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), `integrity/` |
| **Daily use** | Scoring system for relevance/confidence |
| **Risk if removed** | Low — heuristic weights, not critical path |
| **Replacement path** | Scoring weights and heuristics are portable. The `weights.go` file is the most valuable part (calibrated trust weights). The graph-traversal parts can be rebuilt over JSON. |
| **Notes** | `weights.go` encodes confidence calibration that came from real incident experience. Preserve the weight values. |

### `integrity/` — Graph Integrity Checks

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite), `analysis/` |
| **Daily use** | Integrity validation of knowledge graph |
| **Risk if removed** | Low for core — graph-specific |
| **Replacement path** | The integrity checks (cross-link validation, contradiction detection, trust scores) are valuable concepts. Migrate the conceptual checks to YAML validator. The SQLite-specific parts retire. |
| **Notes** | `cross_link.go` and `shapes.go` encode structural integrity rules worth preserving in the lean selfcheck. |

### `scan/` — Static Analysis Scanner

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `go/ast`, `go/parser`, `go/token`, `go/types` — no Globular imports (grpc only in tests) |
| **Daily use** | AST scanning for import violations, annotations |
| **Risk if removed** | Low — used for code-level checks only |
| **Replacement path** | `ast_scanner.go` (import checker) and `allowlist.go` are generic. These can move to standalone if needed. Keep only the simple matchers the instructions describe. |
| **Notes** | `check-import-wall.sh` already covers the most important import-wall checks without AST scanning. |

### `context/` — Context Explanation

| Attribute | Value |
|-----------|-------|
| **Classification** | SHRINK |
| **Imports** | `graph/` (SQLite) |
| **Daily use** | Context explanation for nodes |
| **Risk if removed** | Low — partially replaced by JSON graph queries |
| **Replacement path** | Simple node context (neighborhood, explain) can work over JSON graph. |
| **Notes** | Used by some MCP tools. |

---

### Globular-Specific: KEEP_SERVICES

These packages touch Globular internals and must not move to standalone.

| Package | Classification | Why KEEP_SERVICES |
|---------|--------------|-------------------|
| `runtime/` | KEEP_SERVICES | etcd, workflow, systemd, prometheus, xDS, gRPC config, grpc services, objectstore, repository, installed state |
| `livecluster/` | KEEP_SERVICES | Live cluster collection, snapshot, source controller |
| `extractors/clusterspec/` | KEEP_SERVICES | Cluster specification extraction |
| `extractors/clusterstate/` | KEEP_SERVICES | Cluster state convergence |
| `extractors/dns/` | KEEP_SERVICES | DNS-based cluster discovery |
| `extractors/doctor/` | KEEP_SERVICES | Globular doctor output extraction |
| `extractors/metrics/` | KEEP_SERVICES | Prometheus metrics extraction |
| `extractors/pki/` | KEEP_SERVICES | PKI/certificate extraction |
| `extractors/rbac/` | KEEP_SERVICES | Globular RBAC extraction |
| `extractors/scripts/` | KEEP_SERVICES | Cluster script runners |
| `extractors/workflows/` | KEEP_SERVICES | Globular workflow extraction |
| `extractors/workflowstate/` | KEEP_SERVICES | Workflow state extraction |
| `bundlesync/` | KEEP_SERVICES | Bundle delivery to Globular nodes (production path) |

---

### Low-Value Machinery: LEGACY

These do not serve the core preflight/MCP/profile/selfcheck answers. Mark legacy, retire after knowledge preserved.

| Package | Classification | Reason | Risk if removed |
|---------|--------------|--------|-----------------|
| `contextfreshness/` | LEGACY | Session freshness tracking. Uses graph. Not on hot CLI path. | Low — not referenced by preflight core |
| `debugsession/` | LEGACY | Debug session orchestration. Heavy, rarely used. | Low — functionality absorbed by preflight verdict |
| `enforce/` | LEGACY | Pragma annotation linter. Overengineered. `check-import-wall.sh` covers the real need. | Low |
| `checkedit/` | LEGACY | Check-before-edit machinery. Graph-coupled. Low daily use. | Low |
| `sessionoracle/` | LEGACY | Agent session tracking. Uses `graph` + `contextfreshness` + Globular proto (`ai_memorypb`). Not actively serving core answers. | Low — not on hot path |

---

### Generic Extractors: SHRINK

These extractors are potentially generic but currently tied to the graph DB. Evaluate for standalone use.

| Package | Classification | Notes |
|---------|--------------|-------|
| `extractors/docs/` | SHRINK | Doc extraction — generic. No Globular runtime imports. |
| `extractors/goast/` | SHRINK | Go AST extraction — generic. `etcd_evidence_test.go` is Globular-specific but `extract.go` is generic. |
| `extractors/manual/` | SHRINK | Manual decision rules and causal rules — YAML-based. High value. |
| `extractors/packages/` | SHRINK | Package-level extraction — generic. |
| `extractors/proto/` | SHRINK | Proto symbol extraction — mostly Globular-specific. |
| `extractors/tests/` | SHRINK | Test file extraction — generic. |

---

## Part 3 — Knowledge at Risk

The following knowledge is currently embedded in SQLite or workflow machinery and must be preserved before any code is removed.

### Failuregraph Seeds (HIGH VALUE)

Location: `services/golang/awareness/failuregraph/seeds/*.yaml`

These 9 YAML seed files encode real production failure knowledge:

| Seed file | What it encodes |
|-----------|----------------|
| `empty_advertise_ip_misclassifies_node.yaml` | Node identity IP lookup bug causing false cluster health classification |
| `empty_store_result_deserialization.yaml` | Deserialization failure on empty store results |
| `endpoint_identity_scope_violation.yaml` | Endpoint scope mismatch causing identity lookup failures |
| `installed_state_build_id_missing.yaml` | Missing build ID causes installed-state misclassification |
| `legacy_authority_path_still_called.yaml` | Legacy authority path called after migration |
| `topology_gated_package_false_drift.yaml` | Topology-gated package incorrectly classified as drift |
| `vip_used_as_member_endpoint.yaml` | VIP address used as member endpoint causing routing failures |
| `workflow_blocked_reason_unclassified.yaml` | Workflow blocked with no classified reason |
| `workflow_resume_without_receipt.yaml` | Workflow resumes without idempotency receipt causing duplicate dispatch |

**Action (Phase 2):** Convert these seeds to `.awareness/failure_modes.yaml` format. The seed format already closely matches the target YAML structure.

### Incidentpattern Store

Location: `services/golang/awareness/incidentpattern/` (SQLite-backed)

Any incident patterns stored in the live SQLite DB must be exported to YAML before the DB is retired.

**Action (Phase 2):** Export active incident patterns to `.awareness/incident_patterns.yaml`.

### Fixledger Cases and Guardrails

Location: `services/golang/awareness/fixledger/` (already YAML-based)

The `fix_cases.yaml` and `guardrails.yaml` files encode fix obligations and architectural guardrails.

**Action (Phase 2):** Validate these are fully represented in `.awareness/forbidden_fixes.yaml`. Migrate any missing fixes.

### Semantic Weights

Location: `services/golang/awareness/semantic/weights.go`

Confidence calibration weights derived from real incident experience.

**Action (Phase 2):** Preserve weight values in docs or YAML constants. Apply to lean matcher in Phase 4.

---

## Part 4 — Migration Priority Order

For Phase 3+, migrate in this order (each step unblocks the next):

```text
1. Preserve YAML knowledge (Phase 2):
   - Export failuregraph seeds → .awareness/failure_modes.yaml
   - Export incident patterns → .awareness/incident_patterns.yaml  
   - Validate fixledger cases → .awareness/forbidden_fixes.yaml
   - Preserve semantic weights

2. Build lean knowledge model (Phase 3):
   - knowledge/invariants.go   (load .awareness/invariants.yaml)
   - knowledge/failure_modes.go
   - knowledge/incident_patterns.go
   - knowledge/forbidden_fixes.go
   - knowledge/evidence_contracts.go
   - knowledge/loader.go
   - knowledge/search.go

3. Build lean preflight over YAML (Phase 3):
   - preflight/match.go        (task text + file pattern matcher)
   - preflight/classify.go     (verdict: proceed/warn/block/ask_for_evidence)
   - preflight/verdict.go      (decision trace)
   - preflight/result.go       (PreflightResult)

4. Build lean selfcheck (Phase 3):
   - selfcheck/check.go        (validates .awareness/*.yaml coherence)
   - selfcheck/report.go

5. Build lean assurance (Phase 3):
   - assurance/coverage.go     (which invariants have tests, which failures have patterns)
   - assurance/report.go

6. Move fixledger loader to standalone knowledge/ (Phase 4)

7. Replace graph coupling with JSON graph queries (Phase 4):
   - analysis/impact.go over JSON graph
   - contextnav/ over JSON graph + YAML
   - semantic scoring over YAML (drop SQLite traversal)

8. Retire legacy packages (Phase 6):
   - contextfreshness/
   - debugsession/
   - enforce/
   - checkedit/
   - sessionoracle/
```

---

## Part 5 — Import Wall Status

### Standalone repo — verified clean

```bash
grep -R "github.com/globulario/services" . --include="*.go"
# Expected: no output ✓
```

### Services repo — No awareness SQLite dependency (migration complete)

```bash
grep -R "mattn/go-sqlite3" awareness/ --include="*.go"
# Expected: no output — SQLite removed from all awareness packages ✓
# (mattn/go-sqlite3 remains in golang/sql/ and golang/persistence/ — unrelated)
```

### Standalone must never acquire these imports:

```text
github.com/globulario/services/...
mattn/go-sqlite3
modernc.org/sqlite
database/sql
etcd
grpc (Globular protos)
```

---

## Summary

| Location | Package count | Status |
|----------|-------------|--------|
| `awareness/` (standalone) | 8 packages | ✅ Already lean. Target architecture. |
| `services/golang/awareness/` (services) | 40+ packages | ⚠️ Monster. Needs refocus. |
| Services: KEEP_SERVICES | 13 packages | ✅ Correct home. |
| Services: SHRINK | 15 packages | 🔶 High value, heavy implementation. Migrate knowledge first. |
| Services: LEGACY | 5 packages | 🔴 Low value. Mark, then retire. |
| Services: MOVE_GENERIC | 2 packages (`fixledger/`, parts of `evidence/`) | ⬆️ Can move to standalone after validation. |
| Knowledge at risk | 3 stores (seeds, patterns, fixes) | ⚠️ Must preserve before any deletion. |

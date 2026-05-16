# Phase 6 — Deletion Audit

## Result: No Deletions — All Remaining Packages Have Callers

Phase 6 requires proof before deletion: no imports, tests pass, behavior covered, knowledge preserved.
This audit checked every package against those criteria.

---

## Packages That No Longer Exist (Already Gone)

The refocus instructions listed these as low-value candidates. They do not exist in the repo:

| Package | Status |
|---------|--------|
| `coordination/` | Does not exist — already removed or never created |
| `semanticdiff/` | Does not exist — already removed or never created |
| `architecture/` | Does not exist — already removed or never created |

No action needed.

---

## Packages That Exist With Active Production Callers (Cannot Delete)

All remaining services/golang/awareness packages have at least one non-test caller.
Deletion criteria not met. Leave in place.

| Package | Production Callers | Verdict |
|---------|-------------------|---------|
| `analysis/` | 11 | HAS_CALLERS |
| `assurance/` | active (MCP coverage tool) | HAS_CALLERS |
| `bundlesync/` | active (node delivery) | HAS_CALLERS — KEEP_SERVICES |
| `checkedit/` | 2 | HAS_CALLERS |
| `context/` | 3 | HAS_CALLERS |
| `contextfreshness/` | 4 | HAS_CALLERS |
| `debugsession/` | 3 | HAS_CALLERS |
| `enforce/` | 4 | HAS_CALLERS |
| `evidence/` | active (MCP evidence tools) | HAS_CALLERS |
| `extractors/*` | active (live cluster) | HAS_CALLERS — KEEP_SERVICES |
| `failuregraph/` | active (seeds, seeder) | HAS_CALLERS |
| `failurelearning/` | active (learning loop) | HAS_CALLERS |
| `fixledger/` | 5 | HAS_CALLERS |
| `graph/` | 92+ | HEAVILY_USED — do not touch |
| `incidentpattern/` | active (MCP, doctor) | HAS_CALLERS |
| `integrity/` | 9 | HAS_CALLERS |
| `learning/` | 11 | HAS_CALLERS |
| `livecluster/` | active (MCP live tools) | HAS_CALLERS — KEEP_SERVICES |
| `preflight/` | active (MCP, CLI, cluster_doctor) | HAS_CALLERS |
| `runtime/` | active (live bridge) | HAS_CALLERS — KEEP_SERVICES |
| `scan/` | 2 | HAS_CALLERS |
| `selfcheck/` | active (MCP selfcheck tool) | HAS_CALLERS |
| `semantic/` | 6 | HAS_CALLERS |
| `sessionoracle/` | 4 | HAS_CALLERS |

**graph/db.go (SQLite):** 92+ production callers. Removing SQLite from services requires replacing
the graph store across all callers — this is a major future project, not Phase 6 scope.

---

## Standalone Core — What Was Simplified

The standalone `preflight/scan.go` was simplified in Phase 4:
- Before: 160 lines of raw `map[string]interface{}` YAML scanning
- After: 77 lines backed by the typed `knowledge` package

No further deletion is needed in the standalone repo.

---

## Knowledge Preservation Status

All high-value knowledge from the services packages was preserved in Phase 2:

| Source | Preserved In |
|--------|-------------|
| `failuregraph/seeds/*.yaml` (9 files) | `.awareness/failure_modes.yaml` |
| `learning/testdata/incidents/envoy_desired_hash_restart_storm.yaml` | `.awareness/failure_modes.yaml` |
| `incidentpattern/model.go` (incident shapes) | `.awareness/incident_patterns.yaml` |
| `fixledger/` (fix obligations) | `.awareness/forbidden_fixes.yaml` |
| `failurelearning/testdata/retry_storm_incident.yaml` | `incident_patterns.yaml` (pat.deterministic_failure_retry_storm) |

---

## Phase 6 Conclusion

**Zero deletions performed.** All candidates either:
- Don't exist (already gone)
- Have active production callers (deletion criteria not met)

This is the correct outcome. The refocus mission was to make the **standalone** lean (done: Phases 1–5),
not to dismantle services. Services packages with active callers should stay until a separate
migration project replaces their callers one by one.

---

## Future Deletion Path (Post-Refocus)

If future work wants to remove SQLite from services, the required steps are:
1. Replace `graph.Graph` with the lean JSON graph in each of the 92 callers
2. Migrate `incidentpattern/store.go` to YAML-backed storage
3. Migrate `failuregraph/` SQLite seeder to use the lean YAML loader
4. Remove `graph/db.go` after all callers are gone
5. Run full services test suite

This is a separate project estimated at 3–5 sprints.

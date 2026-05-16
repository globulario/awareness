# Preserved Knowledge — Lean Awareness Refocus Phase 2

This document records what operational knowledge was extracted and preserved before any code
changes. Nothing in the listed sources was deleted as part of this record.

---

## What Was Preserved

### Failure Modes (`.awareness/failure_modes.yaml`)

**Original entries (already in standalone):** 3 generic failure modes encoding awareness-module
development failures:
- `services.import.leak` — transitive Globular import breaks standalone
- `schema.version.drift` — duplicated schema version constant diverges
- `adapter.boundary.breach` — GlobularAdapter code moves into standalone core

**Added: 10 Globular cluster failure modes** extracted from production incidents:

| id | source | severity |
|----|--------|----------|
| `globular.empty_advertise_ip_misclassifies_node` | failuregraph/seeds/empty_advertise_ip_misclassifies_node.yaml | critical |
| `globular.empty_store_result_deserialization` | failuregraph/seeds/empty_store_result_deserialization.yaml | warning |
| `globular.endpoint_identity_scope_violation` | failuregraph/seeds/endpoint_identity_scope_violation.yaml | critical |
| `globular.installed_state_build_id_missing` | failuregraph/seeds/installed_state_build_id_missing.yaml | critical |
| `globular.legacy_authority_path_still_called` | failuregraph/seeds/legacy_authority_path_still_called.yaml | warning |
| `globular.topology_gated_package_false_drift` | failuregraph/seeds/topology_gated_package_false_drift.yaml | warning |
| `globular.vip_used_as_member_endpoint` | failuregraph/seeds/vip_used_as_member_endpoint.yaml | critical |
| `globular.workflow_blocked_reason_unclassified` | failuregraph/seeds/workflow_blocked_reason_unclassified.yaml | warning |
| `globular.workflow_resume_without_receipt` | failuregraph/seeds/workflow_resume_without_receipt.yaml | critical |
| `globular.infra_desired_hash_mismatch_restart_storm` | learning/testdata/incidents/envoy_desired_hash_restart_storm.yaml | critical |

Each entry preserves: summary, symptoms, root cause, correct approach, known bad fixes, regression test names.

### Incident Patterns (`.awareness/incident_patterns.yaml`) — NEW FILE

7 patterns extracted from the failuregraph seeds and incident archives, encoding the *shape* of
dangerous edits (not exact diffs):

| id | title |
|----|-------|
| `pat.vip_identity_confusion` | VIP used where stable NIC IP required |
| `pat.advertise_ip_empty_misclassification` | Empty AdvertiseIp causes false node classification |
| `pat.workflow_no_receipt_duplicate_dispatch` | No receipt on resume causes duplicate dispatch |
| `pat.desired_hash_identity_mismatch` | Two hash paths make convergence impossible |
| `pat.drift_eligibility_disagreement` | Drift scanner and target selection disagree on eligibility |
| `pat.authority_refactor_stale_caller` | Legacy caller bypasses authority refactor |
| `pat.deterministic_failure_retry_storm` | Deterministic failure retried forever |

Each pattern encodes: root cause, lesson, dangerous edit shapes, wrong fixes, related files/symbols.

---

## What Was NOT Preserved (and Why)

### Incident pattern SQLite store (`incidentpattern/store.go`)

The IncidentPattern _data_ is stored in SQLite at runtime (populated from past sessions).
There are no seed YAML files for incident patterns — the 7 patterns above were reconstructed
from the failuregraph seeds and incident YAML files. The SQLite-stored runtime patterns are
session-specific and not reproducible from source.

**Risk:** If any live cluster has incident patterns recorded in its SQLite graph that are not
captured by the failuregraph seeds, those patterns are not preserved here.
**Mitigation:** The failuregraph seeds cover the known high-value incidents. Session-specific
patterns are ephemeral by nature.

### fixledger fix_cases.yaml

The fixledger reads from a runtime `fix_cases.yaml` file. There are no committed `fix_cases.yaml`
files in either repo — they are generated at runtime by the `failurelearning` workflow.
The actual fix case knowledge is already encoded in:
- `.awareness/forbidden_fixes.yaml` (standalone) — 3 generic forbidden fixes
- `failure_modes[*].known_bad_fixes` — 10+ Globular-specific wrong fixes extracted from seeds
- Invariants cross-referenced from the envoy restart storm incident YAML

**No separate fixledger YAML preserved** — knowledge was folded into failure_modes.yaml and
forbidden_fixes.yaml as specified by the refocus instructions.

### failurelearning/testdata/learning_loop/retry_storm_incident.yaml

This is a test fixture encoding a deterministic-failure retry storm. It was used to inform
`pat.deterministic_failure_retry_storm` in incident_patterns.yaml. The fixture itself remains
in services (it is a test file, not production knowledge).

### Semantic scoring weights (`semantic/weights.go`)

The scoring weights are derivable from the source file. Not preserved to memory — they can be
read from `services/golang/awareness/semantic/weights.go` when needed.

### SQLite graph schema (`graph/db.go`)

Not preserved — this is machinery, not knowledge. The schema can be read from the source file.
The refocus plan calls for replacing the SQLite graph with YAML/JSON loaders in the lean core.

---

## Source Files (Do Not Delete Before Phase 6)

These files contain the source knowledge extracted above. They must remain until Phase 6 deletion
criteria are met (callers removed, tests pass, knowledge preserved):

```
services/golang/awareness/failuregraph/seeds/*.yaml         (9 files — KEEP until Phase 6)
services/golang/awareness/learning/testdata/incidents/      (incident YAML — test fixture)
services/golang/awareness/failurelearning/testdata/         (test fixture)
services/golang/awareness/incidentpattern/model.go          (model — useful reference)
```

---

## Phase 2 Status

| Item | Status |
|------|--------|
| failuregraph seeds (9) → failure_modes.yaml | DONE |
| envoy restart storm incident → failure_modes.yaml | DONE |
| incident patterns (7) → incident_patterns.yaml | DONE |
| forbidden fixes | Already in .awareness/forbidden_fixes.yaml |
| invariants | Already in .awareness/invariants.yaml |
| fixledger cases | Folded into failure_modes.yaml (no separate file needed) |
| SQLite schema | Not preserved — machinery, not knowledge |
| Semantic weights | Not preserved — derivable from source |

---

## Next: Phase 3 — Define Lean Core

Implement the lean knowledge model in the standalone repo:

```
awareness/
  knowledge/
    invariants.go        — load/validate/search .awareness/invariants.yaml
    failure_modes.go     — load/search .awareness/failure_modes.yaml
    forbidden_fixes.go   — load/search .awareness/forbidden_fixes.yaml
    incident_patterns.go — load/search .awareness/incident_patterns.yaml
    loader.go            — unified loader (all knowledge files)
    search.go            — keyword/path/task-text matching
```

No SQLite dependency. YAML loaders only. In-memory search index.

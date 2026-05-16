# Awareness Knowledge Preservation — Phase 2

Generated: 2026-05-16
Status: COMPLETE — all knowledge confirmed in YAML, nothing trapped in SQLite

## Key Finding

All Globular awareness knowledge is already in YAML files under `docs/awareness/`.
The SQLite graph database (`graph/`) is a *build artifact* populated from these YAML files at
graph build time — it is not the authoritative store. No knowledge extraction is needed.

---

## Knowledge Inventory

### Primary Knowledge Files (`docs/awareness/`)

| File | Entries | Status |
|------|---------|--------|
| `invariants.yaml` | 70 invariants | ✅ PRESERVED |
| `convergence_rules.yaml` | ~8 convergence invariants | ✅ PRESERVED |
| `awareness_self_invariants.yaml` | ~12 awareness-system invariants | ✅ PRESERVED |
| `failure_modes.yaml` | 46 failure modes | ✅ PRESERVED |
| `forbidden_fixes.yaml` | 142 forbidden fixes | ✅ PRESERVED |
| `design_patterns.yaml` | ~20 design patterns | ✅ PRESERVED |
| `services.yaml` | ~40 service definitions | ✅ PRESERVED |
| `patterns.yaml` | ~25 patterns | ✅ PRESERVED |
| `fix_cases.yaml` | fix case records | ✅ PRESERVED |
| `guardrails.yaml` | runtime guardrails | ✅ PRESERVED |
| `detector_mapping.yaml` | doctor→failure_mode mappings | ✅ PRESERVED |
| `context_aliases.yaml` | agent context aliases | ✅ PRESERVED |
| `learning_rules.yaml` | learning rules | ✅ PRESERVED |

### Incident Knowledge (`docs/awareness/failuregraph_seeds/`)

13 rich ErrorCategory records with signatures, symptoms, causes, resolutions, wrong_fixes:
- `vip_used_as_member_endpoint.yaml` — VIP contamination root cause
- `vip_transition_evicts_etcd_member.yaml` — etcd eviction via keepalived VIP
- `vip_identity_poisoning_cross_layer.yaml` — cross-layer VIP poisoning
- `installed_state_build_id_missing.yaml` — build_id authority gap
- `endpoint_identity_scope_violation.yaml` — endpoint identity confusion
- `topology_gated_package_false_drift.yaml` — false drift in topology gates
- `legacy_authority_path_still_called.yaml` — stale authority path
- `workflow_resume_without_receipt.yaml` — workflow receipt gap
- `workflow_blocked_reason_unclassified.yaml` — unclassified workflow block
- `empty_store_result_deserialization.yaml` — deserialization failure
- `gateway_join_bin_truncates_large_binary.yaml` — binary truncation on join
- `ca_signing_no_proxy_no_etcd_fallback.yaml` — CA signing path gap
- `dns_reconciler_publishes_vip_under_node_a_record.yaml` — DNS VIP publishing

### Real Incidents (`docs/awareness/incidents/`)

5 incident post-mortems:
- `INC-2026-0001.yaml` — annotation validator false positives
- `INC-2026-0002.yaml`
- `INC-2026-0003.yaml`
- `INC-2026-0004.yaml` — write_quorum_lost and installed_state_runtime_mismatch false positives

### Failuregraph Seeds (in `golang/awareness/failuregraph/seeds/`)

9 YAML seeds — all are SUBSETS of `docs/awareness/failuregraph_seeds/` (same format).
These are duplicated; `docs/awareness/failuregraph_seeds/` is authoritative.
The `golang/awareness/failuregraph/seeds/` copies can be removed when failuregraph is migrated.

---

## What SQLite Stores (Ephemeral Build Artifacts)

The SQLite database (`graph/`) stores a *compiled* version of the YAML knowledge:
- Nodes and edges derived from invariants, failure modes, forbidden fixes
- Session records (ephemeral — per-run state, not knowledge)
- Incident pattern match history (ephemeral)
- Failure learning proposals (ephemeral workflow state)

**None of this is irreplaceable.** The graph is rebuilt from YAML at each `awareness build` run.
Session/proposal history is operational, not architectural knowledge.

---

## Project Profile

Created `.awareness.yaml` at the services repo root, pointing to `docs/awareness/` as the
knowledge root. This enables the standalone `github.com/globulario/awareness` tool to:
- Discover Globular's invariants and failure modes
- Run preflight against Globular knowledge
- Build a JSON graph cache from YAML sources

---

## Knowledge NOT in YAML (Safe to Lose)

| What | Why it's OK |
|------|-------------|
| SQLite session records | Ephemeral per-run state |
| Failure learning proposal history | Operational workflow state, not knowledge |
| Context freshness timestamps | Runtime tracking, not knowledge |
| Agent coordination locks/claims | Runtime coordination, not knowledge |
| Semantic diff store | Removed in Phase 1 cleanup |
| Sessionoracle history | Ephemeral session state |

---

## Next Step: Phase 3

Validate the standalone lean knowledge model covers the Globular knowledge schema:
- `Invariant` struct must match the 70-invariant YAML format
- `FailureMode` struct must match the 46-entry YAML format
- `ForbiddenFix` struct must match the 142-entry YAML format
- `IncidentPattern` struct must handle the ErrorCategory seed format
- `EvidenceContract` struct (new — not yet in docs/awareness/)
- Preflight must load from `.awareness.yaml` profile → YAML files → in-memory index

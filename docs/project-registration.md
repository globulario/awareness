# Project Registration Procedure

This document defines the repeatable procedure for registering a project into awareness.
Follow it whenever you onboard a new codebase, service, library, or frontend application.

**Core rule:**

```
Scan the code, but do not worship the code.
```

Code is evidence of what currently exists. It is not automatic proof of what is correct.
Awareness is built from observed implementation **plus** declared intent **plus** tests/proof
**plus** industry patterns **plus** incidents **plus** maintainer decisions.

The lifecycle is:

```
Discover → Classify → Infer candidates → Validate intent → Promote → Link proof → Add preflight guidance
```

Or more concisely:

```
source reality → meaning hypothesis → verified contract → graph knowledge
```

---

## Step 1 — Understand the Goal Before Starting

Awareness serves the AI agent before it edits code. The frontend (or runtime) does not call
awareness. Awareness describes the project so that an AI agent can make safe edits.

Before registering, answer:

```
What can go wrong if an AI edits this project without understanding its contracts?
What authority sources does this project read and write?
What operations are destructive or irreversible?
```

If you cannot answer these, the scan in the next steps will surface them.

---

## Step 2 — Check for an Existing Profile

Look for an existing `.awareness.yaml` at the project root or a central registry entry.

```bash
cat .awareness.yaml 2>/dev/null || echo "no profile yet"
```

If a profile exists: update it.
If no profile exists: create one using the template at `docs/profile_template.yaml`.

Possible project kinds:

```
frontend  backend  service  library  cli  infrastructure  workflow  operator  package
```

**Frontend rule:** The frontend does not call awareness at runtime. Awareness describes the
frontend as a subject for AI-agent analysis.

---

## Step 3 — Discover Project Structure

Scan the project and record its shape before writing any awareness content.

What to look for depending on project type:

| Category | Files to inspect |
|----------|-----------------|
| Go service | `go.mod`, `cmd/`, `internal/`, `*_server.go`, `*_test.go` |
| Frontend | `package.json`, `tsconfig.json`, `vite.config.*`, `src/`, `apps/` |
| Proto API | `*.proto`, generated `*_grpc.go` / `*_pb.ts` |
| Infra / ops | `Makefile`, `Taskfile.yaml`, `Dockerfile`, `docker-compose.yaml` |
| Workflow | `*.workflow.yaml`, workflow handler files |
| Config | `*.yaml`, `.env.example`, etcd key schemas |
| Docs | `README.md`, `docs/`, `ADR/`, decision files |
| Tests | `*_test.go`, `tests/`, `e2e/`, CI config |

Deliver a short structure summary before writing any awareness content:

```
Project: cluster-doctor
Kind: service (Go)
Entry: cmd/cluster_doctor/main.go
Rules: golang/cluster_doctor/cluster_doctor_server/rules/*.go  (50+ files)
Tests: *_test.go alongside each rule
Proto: proto/cluster_doctor.proto
Docs: docs/operators/cluster-doctor.md
```

---

## Step 4 — Extract Authority Sources

Identify every source of truth the project reads from or writes to.

For each authority, answer:

```
What truth does this authority own?
What truth must NOT be inferred from it?
What stale/unknown/error states exist?
Who consumes this authority?
What tests prove it?
```

Example:

```yaml
authority: objectstore health snapshot
owns:
  - MinIO node reachability status (per-node)
  - pool member IP list
  - write quorum state
must_not_infer:
  - whether MinIO process is running on a node whose record is absent
  - global quorum-loss from a partial (DataIncomplete) snapshot
stale_states:
  - DataIncomplete=true (collector could not reach some nodes)
  - NodeRecord absent but node may be healthy
consumers:
  - objectstore_physical_overlap.go (write_quorum_lost rule)
  - objectstore_topology.go (fingerprint_divergence rule)
tests:
  - TestWriteQuorumLost_PartialSnapshot_NoFalsePositive
  - TestFingerprintDivergence_DataIncomplete_SuppressesMissing
```

---

## Step 5 — Extract the State Model

Find the state layers the project operates on. Globular projects follow the 4-layer model:

```
Repository → Desired → Installed → Runtime
```

Add project-specific layers on top:

```
Workflow state (accepted / running / succeeded / failed / blocked)
Doctor finding (info / warn / error / critical)
RBAC permission (allowed / denied)
Frontend local state (loading / optimistic / confirmed / error)
Cached state (fresh / stale / expired)
```

For each state, record:

```yaml
state: objectstore_quorum_health
authority: objectstore health snapshot (DataIncomplete flag, NodeRecord presence)
must_not_derive_from:
  - desired objectstore topology alone
  - MinIO installed-state records
  - partial snapshot where DataIncomplete=true and no knownDown evidence
failure_if_confused:
  - false CRITICAL quorum-loss finding (false positive)
  - missed real quorum loss (false negative)
  - wrong remediation proposal
```

---

## Step 6 — Extract Mutating Actions

Scan for handlers, RPCs, workflow steps, and UI actions that mutate state.

Search patterns: `Install*`, `Delete*`, `Apply*`, `Set*`, `Approve*`, `Reject*`, `Restart*`,
`Promote*`, `Failover*`, `Rotate*`, `Migrate*`, `Wipe*`.

For each mutation, extract:

```yaml
action: ApplyObjectstoreTopology
kind: mutating_action
mutates:
  - /globular/objectstore/config (etcd desired state)
  - MinIO pool membership
  - objectstore watcher state in cluster-controller
requires:
  - cluster leader authorization
  - approved topology transition record
must_not:
  - proceed without generation-matched approval marker
  - silently succeed if the apply-watcher goroutine is dead
  - report topology applied before convergence is confirmed
proof:
  tests:
    - TestApplyTopology_RequiresGenerationMatch
  docs:
    - docs/operational-knowledge/runbooks/recover-stuck-topology-apply.yaml
```

If proof is missing, mark it:

```yaml
proof_status: missing
needs_tests:
  - apply-watcher recovery on compaction error
```

---

## Step 7 — Extract Dangerous Operations

Flag operations that could damage data, identity, availability, or security.

Danger classes: `destructive`, `identity_change`, `availability_risk`, `security_risk`,
`data_migration`, `permission_change`, `topology_change`, `certificate_rotation`,
`network_routing_change`, `cluster_membership_change`.

For each dangerous operation:

```yaml
danger_class: topology_change
operation: objectstore apply topology
must_show:
  - desired topology (pool IPs, erasure coding config)
  - applied generation
  - runtime health before and after
  - destructive transition warning when drive count changes
  - manual action required reason when wiper needed
forbidden:
  - optimistic success before convergence
  - hidden warning on drive count change
  - proceeding without approval when format.json wipe is required
```

---

## Step 8 — Extract Tests and Proofs

Map existing tests to the candidate awareness entries you found in steps 4–7.

```yaml
candidate: objectstore.partial_snapshot_unknown_not_down
proof_status: proven
tests:
  - TestWriteQuorumLost_PartialSnapshot_NoFalsePositive
  - TestWriteQuorumLost_DataIncomplete_NoCritical
  - TestFingerprintDivergence_DataIncomplete_SuppressesMissing
source_files:
  - objectstore_physical_overlap.go
  - objectstore_physical_overlap_test.go
confidence: very_high (regression tests + production incident)
```

If no proof exists, do not fabricate it:

```yaml
candidate: apply_watcher_recovery_on_compaction
proof_status: missing
needs_tests:
  - TestApplyWatcher_RecoveryOnCompactionError
  - TestApplyWatcher_ReEstablishesWatch_AfterChannelClose
```

---

## Step 9 — Scan Docs, Comments, TODOs, Incidents

Extract intent from every non-code source:

```
README.md  docs/  comments  TODO  FIXME  ADR files  incident notes  failuregraph seeds
```

Classify every extracted item:

```yaml
origin:
  type: code_comment | doc | incident | decision | test | inferred_from_code | industry_pattern | principle
  confidence: low | medium | high | very_high
```

Trust hierarchy:

```
principle (law)               → high
industry pattern (borrowed pain) → medium/high
Globular incident (scar)      → very_high
passing regression test       → high/very_high
maintainer decision           → high/very_high
doc / README                  → intent (medium, may be stale)
code comment                  → intent (medium, not law)
TODO / FIXME                  → known gap (low confidence until fixed)
inferred from code pattern    → medium (needs validation)
```

---

## Step 10 — Infer Candidate Invariants

Scan code patterns and produce candidate invariants. **Do not auto-promote them.**

Example code pattern:

```go
if snap.DataIncomplete && len(knownDownNodes) == 0 {
    return nil
}
```

Candidate:

```yaml
candidate_invariant:
  id: objectstore.incomplete_without_known_down_suppresses_quorum_loss
  statement: |
    Incomplete objectstore snapshots must not trigger quorum-loss findings
    unless at least one node is confirmed known-down.
  origin:
    type: inferred_from_code
    confidence: medium
  needs_validation:
    - confirm intent via test (TestWriteQuorumLost_DataIncomplete_NoCritical)
    - confirm via incident (objectstore_partial_snapshot)
    - confirm with maintainer that suppression is intentional
```

Promotion rule:

```
code does X  AND  tests protect X  AND  docs/incidents say X is intended
→ X can become accepted awareness
```

Forbidden promotion rule:

```
code does X → X is correct
```

Never fossilize bugs into awareness.

---

## Step 11 — Compare Code Against Existing Awareness

After discovering code facts, run:

```bash
awareness preflight --mode standard
```

Look for gaps:

```
code pattern with no invariant
invariant with no code coverage
dangerous action with no forbidden fix
state display with no authority contract
workflow action with no proof of completion semantics
permission check missing from UI or handler
runtime state inferred from desired state
unknown state rendered as healthy
```

Example gap report:

```
Rule objectstore_physical_overlap.go implements DataIncomplete suppression.
Invariant objectstore.partial_snapshot_unknown_not_down exists — COVERED.
Rule objectstore_topology.go uses same DataIncomplete guard — COVERED.
Apply-watcher recovery on compaction error — NO TEST, NO INVARIANT — GAP.
```

---

## Step 12 — Promote Knowledge Carefully

Use confidence levels. Do not promote low-confidence candidates as law.

Confidence model:

```
principle              → high
industry pattern       → medium/high
Globular incident      → very_high
passing regression test → high/very_high
maintainer decision    → high/very_high
runtime observation    → fresh but temporary
AI session memory      → useful but must be verified
inferred from code     → medium until validated
comment/TODO           → low/medium until validated
```

If the schema does not have a `status` field for candidates, add a comment:

```yaml
# origin: { type: inferred_from_code, confidence: medium, status: candidate }
```

---

## Step 13 — First Registration Pass Should Be Small

Preferred first change set:

```
- Add or update project profile (.awareness.yaml)
- Add authority map (docs or .awareness/authority_rules.yaml)
- Add candidate invariants (mark clearly if not yet proven)
- Add candidate failure modes
- Add obvious forbidden fixes
- Link existing tests as proof
- Run awareness validation
```

Do not try to complete the full project in one pass.
The goal is useful initial awareness, not perfect total awareness.

---

## Step 14 — Register Awareness Files

Prefer local files when the project supports self-contained awareness:

```
.awareness.yaml                    ← project profile
.awareness/invariants.yaml         ← project-specific invariants
.awareness/failure_modes.yaml      ← project-specific failure modes
.awareness/forbidden_fixes.yaml    ← project-specific forbidden fixes
.awareness/decisions.yaml          ← architectural decisions (optional)
.awareness/authority_rules.yaml    ← authority map (optional)
.awareness/required_tests.yaml     ← required test contracts (optional)
```

If the project feeds into a central awareness repository (e.g., `globulario/services` →
`docs/awareness/`), use the central layout instead. Do not invent duplicate locations.

---

## Step 15 — Validation Requirements

After every change:

```bash
awareness graph build         # check graph builds cleanly
awareness selfcheck           # check self-consistency
go test ./...                 # all tests pass
```

Rules:
- Do not weaken tests.
- Do not ignore duplicate IDs.
- Do not ignore parse warnings.
- Do not add entries that break graph loading.

If a command cannot be run, report it clearly.

---

## Step 16 — Required Deliverable Format

At the end of every registration pass, produce this report:

```
1. Project type and language profile
2. Main authority sources
3. State model discovered
4. Mutating actions discovered
5. Dangerous operations discovered
6. Existing tests/proofs discovered
7. Candidate invariants
8. Candidate failure modes
9. Candidate forbidden fixes
10. Existing awareness coverage
11. Missing awareness coverage
12. Recommended first awareness files to add or update
13. Validation commands run
14. Graph node/edge count if available
15. Recommended next pass
```

---

## Core Rules to Preserve

```
Code is one witness, not the judge.
Tests are proof, but only for what they actually assert.
Docs express intent, but may be stale.
Incidents are high-value scars.
Industry patterns are borrowed pain.
Principles are architectural law.
Runtime observations are fresh but temporary.
Awareness must separate candidate knowledge from accepted knowledge.
```

**Final sentence to remember:**

```
Scan the code. Extract candidates. Validate with intent and proof.
Promote only what deserves to become law.
```

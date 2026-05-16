---
id: desired_hash_is_convergence_identity
type: architecture_decision
status: accepted
summary: DesiredHash is the convergence identity used for InfrastructureRelease, workflow desired_hash, LocalHash, installed-state checksum, and convergence commit paths. Raw artifact digest is artifact identity and must not be substituted.
invariants:
  - infra.desired_hash_consistency
failure_modes:
  - infra.desired_hash_mismatch_restart_storm
symbols:
  - ComputeInfrastructureDesiredHash
  - lookupServiceReleaseBuildID
  - classifyPackageConvergence
forbidden_fixes:
  - use_raw_artifact_digest_as_desired_hash
tests:
  - TestDriftWorkflowUsesDesiredHash
  - TestInfrastructureDesiredHashConsistency
---

## DesiredHash Is Convergence Identity

DesiredHash is computed from the declared spec of an InfrastructureRelease — not
from the artifact blob digest. The convergence path (desired → installed) uses
DesiredHash as its identity anchor. LocalHash on the node-agent side is computed
from the same inputs and compared against DesiredHash to decide whether an install
is needed.

Raw artifact digest (SHA-256 of the blob) is the artifact storage identity used by
the repository layer. It must never be substituted for DesiredHash in convergence
logic. Doing so causes restart storms when digests differ due to re-packing, signing,
or storage normalization while the declared spec is unchanged.

**Forbidden fixes:**
- Using artifact digest as desired_hash
- Restarting a service to "fix" a hash mismatch without checking the spec

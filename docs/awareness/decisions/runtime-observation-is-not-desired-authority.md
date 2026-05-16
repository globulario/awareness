---
id: runtime_observation_is_not_desired_authority
type: architecture_decision
status: accepted
summary: Runtime heartbeat, ManagedInstalled state, or service observation may report discovered state but must not create authoritative desired state by itself.
invariants:
  - infra.heartbeat_not_desired_authority
  - critical_state.registry_ownership_required
failure_modes:
  - infra.heartbeat_creates_spurious_desired_state
forbidden_fixes:
  - create_infra_release_from_heartbeat_only
  - set_desired_version_from_runtime_observation
---

## Runtime Observation Is Not Desired Authority

The node-agent heartbeat reports what is installed and running. It is the Layer 3
(Installed Observed) signal. It must never write to Layer 2 (Desired Release) unless
an explicit operator action has been approved and logged.

Similarly, the cluster controller must not infer desired state from the absence of
a heartbeat, from a service being healthy, or from any runtime observation. The
desired state is set by operators and upgrade pipelines — not auto-discovered from
runtime.

**Forbidden fixes:**
- Creating InfrastructureRelease records from heartbeat data alone
- Setting ServiceDesiredVersion from observed installed versions

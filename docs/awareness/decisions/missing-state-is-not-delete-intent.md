---
id: missing_state_is_not_delete_intent
type: architecture_decision
status: accepted
summary: Missing, invalid, or temporarily unavailable control-plane state must not imply stop, delete, disable, or destructive action.
invariants:
  - critical_state.absence_is_not_destructive_intent
  - critical_state.deletion_requires_audited_intent
failure_modes:
  - critical_state.absence_triggers_destructive_action
forbidden_fixes:
  - treat_missing_etcd_key_as_delete_intent
  - stop_service_on_config_load_failure
---

## Missing State Is Not Delete Intent

If etcd is temporarily unavailable, a key has been accidentally deleted, or a
config record fails to unmarshal, the system must not interpret that as a signal
to stop, uninstall, or delete anything. The correct response is to enter a safe
degraded mode and wait for state to be restored.

Absence of a record is ambiguous. Only an explicit tombstone or a signed operator
intent can trigger a destructive action. The cluster controller must require
audited intent before any destructive operation proceeds.

**Forbidden fixes:**
- Stopping a service because its config key is temporarily missing
- Treating a failed etcd GET as confirmation that no desired state exists

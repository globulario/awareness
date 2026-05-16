---
id: local_success_is_not_global_convergence
type: architecture_decision
status: accepted
summary: A node completing local work does not prove global convergence. Installed-state, result promotion, and action cleanup must be durably committed.
invariants:
  - install.result.atomic_commit
  - convergence.no_infinite_retry
failure_modes:
  - convergence.partial_commit_leaves_ghost_state
forbidden_fixes:
  - assume_install_succeeded_without_etcd_confirmation
  - skip_result_write_on_local_success
tests:
  - TestInstallResultCommittedToEtcd
  - TestConvergenceNoInfiniteRetry
---

## Local Success Is Not Global Convergence

A node-agent completing an install step locally does not mean the cluster has
converged. The result must be written to etcd (Layer 3) before the reconciler
can observe convergence. If the result write fails, the reconciler will retry
the install on the next cycle — which is correct behavior.

Never assume a local success is visible globally. Never skip the result commit.
Never use in-memory state as a substitute for etcd confirmation.

**Forbidden fixes:**
- Skipping the installed-state write after a successful local install
- Using in-memory installed-state as the source of truth across restarts

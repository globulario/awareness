---
id: awareness_graph_is_compiled_context_not_authority
type: architecture_decision
status: accepted
summary: The awareness graph is a compiled semantic context map built from source, metadata, docs, runtime evidence, and learned scars. It informs AI and audits but is not the authority for Desired/Installed/Runtime state.
invariants:
  - awareness.annotation_scanner.production_source_only
failure_modes:
  - awareness.graph_used_as_state_authority
forbidden_fixes:
  - use_awareness_graph_as_desired_state_source
  - trust_graph_over_etcd_for_cluster_decisions
---

## Awareness Graph Is Compiled Context, Not Authority

The awareness graph connects source code, invariants, failure modes, design
decisions, runtime evidence, and learned scars into a queryable SQLite graph.
It is a *compiled semantic context map* — not an operational source of truth.

Cluster decisions (what to install, what to start, what version to run) come from
etcd. The awareness graph exists to help AI agents, operators, and auditors
understand the system — not to drive it.

If the graph is stale, corrupt, or missing, the cluster continues operating through
its deterministic convergence model. The graph is supplementary context.

**Forbidden fixes:**
- Reading desired state from the awareness graph instead of etcd
- Trusting graph edge presence as proof of runtime health

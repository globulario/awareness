// ObjectStoreTopologyPage — scanner fixture for frontend-awareness.
//
// This is not production UI. It exists so the tsast scanner can extract
// frontend_component, frontend_backend_call, frontend_state_atom, and
// frontend_permission_check nodes for awareness graph tests.
//
// Authority contract (declared in .awareness/invariants.yaml):
//   desired topology     → ObjectStoreDesiredState authority
//   applied generation   → objectstore runtime/status authority
//   runtime health       → MinIO runtime health authority
//   destructive risk     → topology transition approval authority
//
// Forbidden (declared in .awareness/forbidden_fixes.yaml):
//   - infer applied generation from desired state
//   - hide destructive wipe risk
//   - show healthy when runtime authority is unknown
//   - enable topology apply without permission

import { useState } from "react";
import { ObjectStoreClient } from "../clients/ObjectStoreClient";
import { usePermission } from "../hooks/usePermission";

export function ObjectStoreTopologyPage() {
  const [desiredTopology, setDesiredTopology] = useState(null);
  const [runtimeHealth, setRuntimeHealth] = useState(null);
  const [appliedGeneration, setAppliedGeneration] = useState(null);
  const [destructiveRisk, setDestructiveRisk] = useState(false);
  const canApplyTopology = usePermission("objectstore.topology.apply");

  const client = new ObjectStoreClient();

  async function loadTopology() {
    const topology = await client.getDesiredTopology();
    const health = await client.getRuntimeHealth();
    const status = await client.getAppliedStatus();
    setDesiredTopology(topology);
    setRuntimeHealth(health);
    setAppliedGeneration(status.generation);
    setDestructiveRisk(status.destructiveTransitionPending);
  }

  return (
    <div className="topology-page">
      {destructiveRisk && (
        <div className="warning-banner" role="alert" aria-live="assertive">
          Destructive topology transition pending — data wipe risk.
          Manual approval required.
        </div>
      )}
      <div className="topology-grid md:grid-cols-2">
        <div className="desired-topology">
          {desiredTopology ? JSON.stringify(desiredTopology) : "Loading..."}
        </div>
        <div className="runtime-health">
          {runtimeHealth ?? "Unknown"}
        </div>
      </div>
      <button
        disabled={!canApplyTopology}
        onClick={loadTopology}
        aria-label="Apply topology"
      >
        Apply
      </button>
    </div>
  );
}

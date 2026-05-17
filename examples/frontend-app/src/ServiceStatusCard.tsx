// ServiceStatusCard — scanner fixture for frontend-awareness.
//
// Authority contract:
//   runtime state badge  → runtime health authority (NOT desired state)
//   desired version      → desired state authority
//   installed version    → installed state authority
//   degraded reason      → doctor findings authority
//
// Forbidden: show green badge from desired.enabled without runtime health.

import { useState } from "react";
import { useSelector } from "../hooks/useSelector";

export function ServiceStatusCard({ serviceId }: { serviceId: string }) {
  const runtimeHealth = useSelector((s) => s.runtime[serviceId]);
  const desiredState = useSelector((s) => s.desired[serviceId]);
  const installedState = useSelector((s) => s.installed[serviceId]);
  const [expanded, setExpanded] = useState(false);

  const isHealthy = runtimeHealth?.status === "healthy";
  const degradedReason = runtimeHealth?.degradedReason ?? null;

  return (
    <div className="service-card overflow-hidden">
      <div className="card-header md:flex">
        <span className="service-name truncate" title={serviceId}>
          {serviceId}
        </span>
        <span
          className={`badge ${isHealthy ? "badge-green" : "badge-unknown"}`}
          aria-label={`runtime status: ${runtimeHealth?.status ?? "unknown"}`}
        >
          {runtimeHealth?.status ?? "unknown"}
        </span>
      </div>
      {degradedReason && (
        <div className="degraded-reason" role="alert">
          {degradedReason}
        </div>
      )}
      <div className="version-row">
        <span>Desired: {desiredState?.version ?? "—"}</span>
        <span>Installed: {installedState?.version ?? "—"}</span>
      </div>
    </div>
  );
}

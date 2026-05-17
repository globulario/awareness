// InstallButton — scanner fixture for frontend-awareness.
//
// Authority contract:
//   can install     → RBAC authority (not session presence alone)
//   install action  → ApplicationsManager.InstallApplication
//   success state   → backend workflow receipt (not click event)
//
// Forbidden:
//   - mark installed before backend confirmation
//   - retry permission denied
//   - hide backend error reason

import { useState } from "react";
import { ApplicationsManagerClient } from "../clients/ApplicationsManagerClient";
import { hasPermission } from "../hooks/hasPermission";

export function InstallButton({ packageId, targetDomain }: { packageId: string; targetDomain: string }) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const canInstall = hasPermission("packages.install");
  const client = new ApplicationsManagerClient();

  async function handleInstall() {
    if (!canInstall) return;
    setLoading(true);
    setError(null);
    try {
      await client.installApplication({ packageId, targetDomain });
    } catch (err: any) {
      setError(err?.message ?? "Install failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="install-button-wrapper">
      {error && (
        <div className="error-banner" role="alert" aria-live="polite">
          {error}
        </div>
      )}
      <button
        onClick={handleInstall}
        disabled={!canInstall || loading}
        aria-label={canInstall ? "Install package" : "Install requires permission"}
      >
        {loading ? "Installing…" : "Install"}
      </button>
    </div>
  );
}

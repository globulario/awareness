package evidence

import "time"

// ServiceObservation is the raw observed state of a single systemd service unit.
type ServiceObservation struct {
	Name        string `json:"name"`
	UnitName    string `json:"unit_name"`
	ActiveState string `json:"active_state"` // active, inactive, failed
	SubState    string `json:"sub_state"`    // running, exited, start-limit-hit, dead
	ExitCode    int    `json:"exit_code,omitempty"`
	// LogExcerpt holds the last few lines from journalctl (bounded, not unlimited).
	LogExcerpt string `json:"log_excerpt,omitempty"`
}

// PortObservation is the observed listening state of a single TCP/UDP port.
type PortObservation struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"` // tcp, udp
	Listening bool   `json:"listening"`
}

// AwarenessBundleStatus describes the locally installed awareness bundle.
type AwarenessBundleStatus struct {
	Present bool   `json:"present"`
	Version string `json:"version,omitempty"`
	BuildID string `json:"build_id,omitempty"`
	// Status is LOADED, MISSING, STALE, MISMATCH, or CORRUPT.
	Status string `json:"status"`
}

// ReleaseInfo is the platform version/build_id from the local release-index.
// Present distinguishes "release-index.json absent" from "present but version
// unreadable" — the two cases produce different facts in the normalizer.
type ReleaseInfo struct {
	Present bool   `json:"present"`
	Version string `json:"version,omitempty"`
	BuildID string `json:"build_id,omitempty"`
}

// NodeRuntimeSnapshot is a local, self-contained observation of a single node's runtime state.
// It is separate from the cluster-wide RuntimeSnapshot (awareness/runtime.RuntimeSnapshot):
// that snapshot uses etcd-connected sources; this one is purely local reads.
//
// Files written to /var/lib/globular/awareness/runtime/latest_snapshot.json.
// Published to etcd at /globular/awareness/runtime/<node-id>/snapshot.
type NodeRuntimeSnapshot struct {
	NodeID      string    `json:"node_id"`
	Address     string    `json:"address"`
	Phase       Phase     `json:"phase"`
	CollectedAt time.Time `json:"collected_at"`

	Release         ReleaseInfo           `json:"release"`
	AwarenessBundle AwarenessBundleStatus `json:"awareness_bundle"`

	// PKI is the local PKI file presence observation.
	PKI PKIObservation `json:"pki"`

	// ScyllaConfig holds key fields parsed from /etc/scylla/scylla.yaml.
	ScyllaConfig ScyllaConfigObservation `json:"scylla_config"`

	Services []ServiceObservation `json:"services"`
	Ports    []PortObservation    `json:"ports"`

	// Facts are the normalized facts derived from the raw observations by the Normalizer.
	Facts []RuntimeFact `json:"facts"`

	// RawEvidenceRefs are short pointers to the raw data (unit names, log lines, file paths).
	RawEvidenceRefs []string `json:"raw_evidence_refs,omitempty"`
}

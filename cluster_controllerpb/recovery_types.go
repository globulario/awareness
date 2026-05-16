package cluster_controllerpb

import "time"

// NodeRecoveryPhase represents the current phase of a full-reseed recovery workflow.
type NodeRecoveryPhase int32

const (
	NodeRecoveryPhaseUnspecified      NodeRecoveryPhase = 0
	NodeRecoveryPhasePrecheck         NodeRecoveryPhase = 1
	NodeRecoveryPhaseSnapshot         NodeRecoveryPhase = 2
	NodeRecoveryPhaseFenceNode        NodeRecoveryPhase = 3
	NodeRecoveryPhaseRemoveOrDrain    NodeRecoveryPhase = 4
	NodeRecoveryPhaseAwaitReprovision NodeRecoveryPhase = 5
	NodeRecoveryPhaseAwaitRejoin      NodeRecoveryPhase = 6
	NodeRecoveryPhaseReseedArtifacts  NodeRecoveryPhase = 7
	NodeRecoveryPhaseVerifyArtifacts  NodeRecoveryPhase = 8
	NodeRecoveryPhaseVerifyRuntime    NodeRecoveryPhase = 9
	NodeRecoveryPhaseUnfenceNode      NodeRecoveryPhase = 10
	NodeRecoveryPhaseComplete         NodeRecoveryPhase = 11
	NodeRecoveryPhaseFailed           NodeRecoveryPhase = 12
)

func (p NodeRecoveryPhase) String() string {
	switch p {
	case NodeRecoveryPhasePrecheck:
		return "PRECHECK"
	case NodeRecoveryPhaseSnapshot:
		return "SNAPSHOT"
	case NodeRecoveryPhaseFenceNode:
		return "FENCE_NODE"
	case NodeRecoveryPhaseRemoveOrDrain:
		return "REMOVE_OR_DRAIN"
	case NodeRecoveryPhaseAwaitReprovision:
		return "AWAIT_REPROVISION"
	case NodeRecoveryPhaseAwaitRejoin:
		return "AWAIT_REJOIN"
	case NodeRecoveryPhaseReseedArtifacts:
		return "RESEED_ARTIFACTS"
	case NodeRecoveryPhaseVerifyArtifacts:
		return "VERIFY_ARTIFACTS"
	case NodeRecoveryPhaseVerifyRuntime:
		return "VERIFY_RUNTIME"
	case NodeRecoveryPhaseUnfenceNode:
		return "UNFENCE_NODE"
	case NodeRecoveryPhaseComplete:
		return "COMPLETE"
	case NodeRecoveryPhaseFailed:
		return "FAILED"
	default:
		return "UNSPECIFIED"
	}
}

// IsTerminal returns true if the phase is a terminal state.
func (p NodeRecoveryPhase) IsTerminal() bool {
	return p == NodeRecoveryPhaseComplete || p == NodeRecoveryPhaseFailed
}

// NodeRecoveryMode controls whether exact captured builds are required.
type NodeRecoveryMode int32

const (
	NodeRecoveryModeUnspecified            NodeRecoveryMode = 0
	NodeRecoveryModeExactReplayRequired    NodeRecoveryMode = 1
	NodeRecoveryModeAllowResolutionFallback NodeRecoveryMode = 2
)

func (m NodeRecoveryMode) String() string {
	switch m {
	case NodeRecoveryModeExactReplayRequired:
		return "EXACT_REPLAY_REQUIRED"
	case NodeRecoveryModeAllowResolutionFallback:
		return "ALLOW_RESOLUTION_FALLBACK"
	default:
		return "UNSPECIFIED"
	}
}

// RecoveryArtifactStatus tracks per-artifact install progress.
type RecoveryArtifactStatus int32

const (
	RecoveryArtifactStatusUnspecified            RecoveryArtifactStatus = 0
	RecoveryArtifactStatusPending                RecoveryArtifactStatus = 1
	RecoveryArtifactStatusSkippedAlreadyVerified RecoveryArtifactStatus = 2
	RecoveryArtifactStatusInstalling             RecoveryArtifactStatus = 3
	RecoveryArtifactStatusInstalled              RecoveryArtifactStatus = 4
	RecoveryArtifactStatusVerified               RecoveryArtifactStatus = 5
	RecoveryArtifactStatusFailed                 RecoveryArtifactStatus = 6
)

func (s RecoveryArtifactStatus) String() string {
	switch s {
	case RecoveryArtifactStatusPending:
		return "PENDING"
	case RecoveryArtifactStatusSkippedAlreadyVerified:
		return "SKIPPED_ALREADY_VERIFIED"
	case RecoveryArtifactStatusInstalling:
		return "INSTALLING"
	case RecoveryArtifactStatusInstalled:
		return "INSTALLED"
	case RecoveryArtifactStatusVerified:
		return "VERIFIED"
	case RecoveryArtifactStatusFailed:
		return "FAILED"
	default:
		return "UNSPECIFIED"
	}
}

// SnapshotArtifact is a single artifact entry in a NodeRecoverySnapshot.
// It captures everything known about an installed artifact at snapshot time.
type SnapshotArtifact struct {
	PublisherID string `json:"publisher_id,omitempty"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // SERVICE / APPLICATION / INFRASTRUCTURE / COMMAND
	Version     string `json:"version,omitempty"`
	BuildID     string `json:"build_id,omitempty"`
	BuildNumber int64  `json:"build_number,omitempty"`
	Checksum    string `json:"checksum,omitempty"` // SHA-256 of binary / archive

	// Ordering metadata
	Priority int32    `json:"priority,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Provides []string `json:"provides,omitempty"`

	// Resolution metadata
	Provisional         bool   `json:"provisional,omitempty"`
	ExactBuildAvailable bool   `json:"exact_build_available,omitempty"`
	InstalledSource     string `json:"installed_source,omitempty"` // "repository" / "local" / "bootstrap"
	InstallState        string `json:"install_state,omitempty"`    // installed / partial_apply / failed
	OriginalNodeID      string `json:"original_node_id,omitempty"`
}

// NodeRecoverySnapshot is a persisted inventory of a node taken before wipe.
//
// Stored at: /globular/recovery/nodes/<node_id>/snapshots/<snapshot_id>
//
// Rule A: A full reseed is only valid if we have a persisted snapshot of the
// node before any destructive boundary is crossed.
type NodeRecoverySnapshot struct {
	SnapshotID string `json:"snapshot_id"`
	ClusterID  string `json:"cluster_id,omitempty"`
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name,omitempty"`
	Hostname   string `json:"hostname,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
	Reason    string    `json:"reason,omitempty"`

	SourceNodeEpoch    string   `json:"source_node_epoch,omitempty"`
	ProfileFingerprint string   `json:"profile_fingerprint,omitempty"`
	Profiles           []string `json:"profiles,omitempty"`

	Artifacts []SnapshotArtifact `json:"artifacts"`

	SnapshotHash        string   `json:"snapshot_hash,omitempty"`
	ExactReplayPossible bool     `json:"exact_replay_possible,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

// NodeRecoveryState tracks the workflow ownership and phase for a recovering node.
//
// Stored at: /globular/recovery/nodes/<node_id>/state
//
// This is the authoritative fencing record. When ReconciliationPaused is true
// the normal reconciler MUST skip all convergence work for this node.
type NodeRecoveryState struct {
	NodeID     string           `json:"node_id"`
	WorkflowID string           `json:"workflow_id,omitempty"`
	SnapshotID string           `json:"snapshot_id,omitempty"`
	Phase      NodeRecoveryPhase `json:"phase"`
	Mode       NodeRecoveryMode  `json:"mode"`

	ReconciliationPaused       bool `json:"reconciliation_paused"`
	DestructiveBoundaryCrossed bool `json:"destructive_boundary_crossed,omitempty"`

	OldNodeIdentity string `json:"old_node_identity,omitempty"`
	NewNodeIdentity string `json:"new_node_identity,omitempty"`

	StartedAt   time.Time  `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	LastError   string   `json:"last_error,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	RequestedBy string   `json:"requested_by,omitempty"`

	VerificationPassed bool `json:"verification_passed,omitempty"`

	// ReprovisionAcked is set when the operator calls AckNodeReprovisioned.
	// The await_reprovision actor polls for this flag.
	ReprovisionAcked bool `json:"reprovision_acked,omitempty"`
}

// PlannedRecoveryArtifact is the ordered install plan entry returned to the CLI.
type PlannedRecoveryArtifact struct {
	PublisherID string `json:"publisher_id,omitempty"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Version     string `json:"version,omitempty"`
	BuildID     string `json:"build_id,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
	Order       int32  `json:"order"`
	Source      string `json:"source"` // SNAPSHOT_EXACT / REPOSITORY_RESOLVED
}

// NodeRecoveryArtifactResult tracks the install + verification result for one artifact.
//
// Stored at: /globular/recovery/nodes/<node_id>/artifacts/<name>
//
// Persisted per-artifact so a workflow restart can skip already-verified entries
// (Rule: idempotent reseed — do not re-apply successfully verified artifacts).
type NodeRecoveryArtifactResult struct {
	WorkflowID string `json:"workflow_id"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	NodeID     string `json:"node_id"`

	PublisherID string `json:"publisher_id,omitempty"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`

	RequestedVersion  string `json:"requested_version,omitempty"`
	RequestedBuildID  string `json:"requested_build_id,omitempty"`
	RequestedChecksum string `json:"requested_checksum,omitempty"`

	InstalledVersion  string `json:"installed_version,omitempty"`
	InstalledBuildID  string `json:"installed_build_id,omitempty"`
	InstalledChecksum string `json:"installed_checksum,omitempty"`

	Order  int32  `json:"order"`
	Source string `json:"source,omitempty"` // SNAPSHOT_EXACT / REPOSITORY_RESOLVED / LOCAL_CACHE

	Status     RecoveryArtifactStatus `json:"status"`
	StartedAt  time.Time              `json:"started_at"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

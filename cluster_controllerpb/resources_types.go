package cluster_controllerpb

// NOTE: This file provides minimal resource types to enable the new resource
// store and RPC scaffolding. It intentionally avoids full generated code while
// keeping field shapes compatible with future proto generation.

type ObjectMeta struct {
	Name            string            `json:"name,omitempty"`
	ResourceVersion string            `json:"resource_version,omitempty"`
	Generation      int64             `json:"generation,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
}

type ObjectStatus struct {
	ObservedGeneration int64 `json:"observed_generation,omitempty"`
}

type ClusterNetwork struct {
	Meta   *ObjectMeta         `json:"meta,omitempty"`
	Spec   *ClusterNetworkSpec `json:"spec,omitempty"`
	Status *ObjectStatus       `json:"status,omitempty"`
}

//go:schemalint:ignore — implementation type, not schema owner
type ServiceDesiredVersionSpec struct {
	ServiceName string `json:"service_name,omitempty"`
	Version     string `json:"version,omitempty"`
	BuildNumber int64  `json:"build_number,omitempty"` // Build iteration within version (0 = legacy)
	BuildID     string `json:"build_id,omitempty"`     // Phase 2: exact artifact identity (UUIDv7, repository-allocated)
	// PublisherID is normally empty (defaults to core@globular.io). It is set
	// explicitly only when a local override is active — allowing the release
	// resolver to look up the artifact under the correct identity lane
	// (e.g. local@<cluster-id>). Cleared when the override is removed.
	PublisherID string `json:"publisher_id,omitempty"`
}

// LocalOverride is stored at /globular/releases/local_overrides/{service_name}.
// It records an active local-package override: which local build is active, why,
// and the official snapshot needed to undo the override.
//
// +globular:schema:key="/globular/releases/local_overrides/{name}"
// +globular:schema:writer="globular-cli (pkg override)"
// +globular:schema:readers="globular-cluster-controller,globular-cluster-doctor,globular-cli"
// +globular:schema:description="Active local package override — official BOM untouched; effective BOM uses this build."
type LocalOverride struct {
	ServiceName    string `json:"service_name"`
	PublisherID    string `json:"publisher_id"`     // e.g. local@ryzen
	Version        string `json:"version"`           // e.g. 1.2.43+local.ryzen.1
	BuildID        string `json:"build_id"`          // local artifact build_id
	BuildNumber    int64  `json:"build_number,omitempty"`
	BasedOnVersion string `json:"based_on_version,omitempty"` // official version this is derived from
	BasedOnBuildID string `json:"based_on_build_id,omitempty"` // official build_id this is derived from
	PatchReason    string `json:"patch_reason"`
	CreatedBy      string `json:"created_by,omitempty"`      // operator hostname / identity
	CreatedAtUnixS int64  `json:"created_at_unix_s"`

	// OfficialSnapshot holds the official ServiceDesiredVersionSpec that was
	// active before the override. Restored verbatim by 'pkg override remove'.
	OfficialSnapshot *LocalOverrideSnapshot `json:"official_snapshot"`
}

// LocalOverrideSnapshot is the pre-override state of a ServiceDesiredVersionSpec.
type LocalOverrideSnapshot struct {
	ServiceName string `json:"service_name"`
	Version     string `json:"version"`
	BuildNumber int64  `json:"build_number,omitempty"`
	BuildID     string `json:"build_id,omitempty"`
	PublisherID string `json:"publisher_id,omitempty"` // empty = core@globular.io
	Generation  int64  `json:"generation,omitempty"`
}

// LocalOverrideKeyPrefix is the etcd key prefix for active local overrides.
const LocalOverrideKeyPrefix = "/globular/releases/local_overrides/"

// ServiceDesiredVersion is the cluster-wide desired version pointer for a
// single SERVICE-kind package. It is the authoritative "which version
// should be running?" for services across every node.
//
// +globular:schema:key="/globular/resources/ServiceDesiredVersion/{name}"
// +globular:schema:writer="globular-cluster-controller"
// +globular:schema:readers="globular-node-agent,globular-repository,globular-cluster-doctor"
// +globular:schema:description="Cluster-wide desired version for a SERVICE-kind package."
// +globular:schema:invariants="Only set for packages with kind=SERVICE; version MUST be present in the repository catalog; meta.generation monotonically increases."
type ServiceDesiredVersion struct {
	Meta   *ObjectMeta                `json:"meta,omitempty"`
	Spec   *ServiceDesiredVersionSpec `json:"spec,omitempty"`
	Status *ObjectStatus              `json:"status,omitempty"`
}

//go:schemalint:ignore — implementation type, not schema owner
type NodeSpec struct {
	Labels map[string]string `json:"labels,omitempty"`
	Roles  []string          `json:"roles,omitempty"`
}

type Node struct {
	Meta   *ObjectMeta   `json:"meta,omitempty"`
	Spec   *NodeSpec     `json:"spec,omitempty"`
	Status *ObjectStatus `json:"status,omitempty"`
}

// ── Service Lifecycle v1 ──────────────────────────────────────────────────────

// RolloutStrategy constants for ServiceReleaseSpec.RolloutStrategy.
const (
	RolloutRolling   = "ROLLING"     // One batch at a time; gates on MinReadySeconds
	RolloutAllAtOnce = "ALL_AT_ONCE" // All nodes concurrently up to MaxParallelNodes
)

// ReleasePhase constants for ServiceReleaseStatus.Phase and NodeReleaseStatus.Phase.
const (
	ReleasePhasePending    = "PENDING"     // Created/updated, awaiting resolution
	ReleasePhaseWaiting    = "WAITING"     // Artifact not yet available in repository — keep current version, retry later
	ReleasePhaseResolved   = "RESOLVED"    // Exact version + artifact digest known
	ReleasePhasePlanned    = "PLANNED"     // NodePlans written to plan store
	ReleasePhaseApplying   = "APPLYING"    // LEGACY: workflow-native code no longer writes this; derive "is-applying" from workflow run state
	ReleasePhaseAvailable  = "AVAILABLE"   // All target nodes at desired version
	ReleasePhaseDegraded   = "DEGRADED"    // Some nodes failed; min replicas still met
	ReleasePhaseFailed     = "FAILED"      // Cannot reach desired state; retries exhausted
	ReleasePhaseRolledBack = "ROLLED_BACK" // LEGACY: rollback is now a manual operation, not an automatic phase
)

// NodeAssignment is an optional per-node version override within a ServiceRelease.
type NodeAssignment struct {
	NodeID  string            `json:"node_id,omitempty"`
	Version string            `json:"version,omitempty"` // Empty = use release default
	Pins    map[string]string `json:"pins,omitempty"`
}

// ServiceReleaseSpec declares the desired state of a service across the cluster.
// Both PublisherID and ServiceName are required; they form the ArtifactRef identity.
type ServiceReleaseSpec struct {
	PublisherID      string            `json:"publisher_id,omitempty"`
	ServiceName      string            `json:"service_name,omitempty"`
	Version          string            `json:"version,omitempty"`      // Exact; empty = resolve latest published
	BuildNumber      int64             `json:"build_number,omitempty"` // Build iteration within version (0 = legacy)
	BuildID          string            `json:"build_id,omitempty"`     // Phase 2: exact artifact identity (UUIDv7)
	Channel          string            `json:"channel,omitempty"`      // Deprecated: functionally ignored, will be removed
	RepositoryID     string            `json:"repository_id,omitempty"`
	Platform         string            `json:"platform,omitempty"`         // e.g. "linux_amd64"
	RolloutStrategy  string            `json:"rollout_strategy,omitempty"` // RolloutRolling | RolloutAllAtOnce
	MaxParallelNodes uint32            `json:"max_parallel_nodes,omitempty"`
	MinReadySeconds  uint32            `json:"min_ready_seconds,omitempty"`
	MaxUnavailable   uint32            `json:"max_unavailable,omitempty"`
	NodeAssignments  []*NodeAssignment `json:"node_assignments,omitempty"`
	Config           map[string]string `json:"config,omitempty"`
	Paused           bool              `json:"paused,omitempty"`
	Removing         bool              `json:"removing,omitempty"`
	Replicas         *ReplicaSpec      `json:"replicas,omitempty"`
}

// ReplicaSpec declares min/max replicas for a release.
type ReplicaSpec struct {
	Min int32 `json:"min,omitempty"`
	Max int32 `json:"max,omitempty"`
}

// NodeReleaseStatus tracks per-node progress within a ServiceRelease rollout.
type NodeReleaseStatus struct {
	NodeID                string `json:"node_id,omitempty"`
	Phase                 string `json:"phase,omitempty"` // ReleasePhase* constants
	InstalledVersion      string `json:"installed_version,omitempty"`
	InstalledBuildNumber  int64  `json:"installed_build_number,omitempty"` // build iteration on this node
	InstalledBuildID      string `json:"installed_build_id,omitempty"`     // Phase 2: exact installed artifact identity
	ErrorMessage          string `json:"error_message,omitempty"`
	UpdatedUnixMs         int64  `json:"updated_unix_ms,omitempty"`
	FailedStepID          string `json:"failed_step_id,omitempty"` // step that failed (from plan status)
}

// ServiceReleaseStatus is the controller-managed status of a ServiceRelease.
type ServiceReleaseStatus struct {
	Phase                  string               `json:"phase,omitempty"`
	ResolvedVersion        string               `json:"resolved_version,omitempty"`
	ResolvedBuildNumber    int64                `json:"resolved_build_number,omitempty"`    // resolved build iteration
	ResolvedBuildID        string               `json:"resolved_build_id,omitempty"`       // Phase 2: resolved exact artifact identity
	ResolvedArtifactDigest string               `json:"resolved_artifact_digest,omitempty"` // SHA256 hex
	DesiredHash            string               `json:"desired_hash,omitempty"`             // SHA256 of (publisher+name+version+build_number+config)
	Nodes                  []*NodeReleaseStatus `json:"nodes,omitempty"`
	Message                string               `json:"message,omitempty"`
	LastTransitionUnixMs   int64                `json:"last_transition_unix_ms,omitempty"`
	ObservedGeneration     int64                `json:"observed_generation,omitempty"`
	WorkflowKind           string               `json:"workflow_kind,omitempty"`       // "install", "upgrade", "remove"
	StartedAtUnixMs        int64                `json:"started_at_unix_ms,omitempty"`  // workflow begin timestamp
	TransitionReason       string               `json:"transition_reason,omitempty"`   // structured reason for last phase change

	// Transient-retry state — populated when a workflow dispatch fails with a
	// recoverable error (circuit breaker open, Scylla unavailable, etc.).
	// The reconciler skips re-dispatch until NextRetryUnixMs has passed.
	RetryCount         int64  `json:"retry_count,omitempty"`          // number of transient retry attempts since last success
	LastRetryUnixMs    int64  `json:"last_retry_unix_ms,omitempty"`   // wall-clock timestamp of the most recent retry attempt
	NextRetryUnixMs    int64  `json:"next_retry_unix_ms,omitempty"`   // earliest time to attempt the next dispatch
	LastTransientError string `json:"last_transient_error,omitempty"` // last transient error message
	BlockedReason      string `json:"blocked_reason,omitempty"`       // structured reason: workflow_unavailable, workflow_circuit_open, …
}

// ServiceRelease is the top-level desired-state object for service lifecycle.
// The cluster-controller watches ServiceRelease objects and drives reconciliation.
type ServiceRelease struct {
	Meta   *ObjectMeta           `json:"meta,omitempty"`
	Spec   *ServiceReleaseSpec   `json:"spec,omitempty"`
	Status *ServiceReleaseStatus `json:"status,omitempty"`
}

// ── Application Lifecycle v1 ──────────────────────────────────────────────────

// ApplicationReleaseSpec declares the desired state of a web application deployment.
type ApplicationReleaseSpec struct {
	PublisherID     string            `json:"publisher_id,omitempty"`
	AppName         string            `json:"app_name,omitempty"`
	Version         string            `json:"version,omitempty"`
	BuildNumber     int64             `json:"build_number,omitempty"` // Build iteration within version (0 = legacy)
	BuildID         string            `json:"build_id,omitempty"`     // Phase 2: exact artifact identity (UUIDv7)
	Channel         string            `json:"channel,omitempty"` // Deprecated: functionally ignored
	RepositoryID    string            `json:"repository_id,omitempty"`
	Platform        string            `json:"platform,omitempty"`
	NodeAssignments []*NodeAssignment `json:"node_assignments,omitempty"`
	Route           string            `json:"route,omitempty"`      // URL path, e.g. "/apps/myapp"
	IndexFile       string            `json:"index_file,omitempty"` // Entry HTML file, default "index.html"
	Removing        bool              `json:"removing,omitempty"`
}

// ApplicationReleaseStatus is the controller-managed status of an ApplicationRelease.
type ApplicationReleaseStatus struct {
	Phase                  string               `json:"phase,omitempty"`
	ResolvedVersion        string               `json:"resolved_version,omitempty"`
	ResolvedBuildNumber    int64                `json:"resolved_build_number,omitempty"`
	ResolvedBuildID        string               `json:"resolved_build_id,omitempty"`       // Phase 2
	ResolvedArtifactDigest string               `json:"resolved_artifact_digest,omitempty"`
	DesiredHash            string               `json:"desired_hash,omitempty"`
	Nodes                  []*NodeReleaseStatus `json:"nodes,omitempty"`
	Message                string               `json:"message,omitempty"`
	LastTransitionUnixMs   int64                `json:"last_transition_unix_ms,omitempty"`
	ObservedGeneration     int64                `json:"observed_generation,omitempty"`
	WorkflowKind           string               `json:"workflow_kind,omitempty"`
	StartedAtUnixMs        int64                `json:"started_at_unix_ms,omitempty"`
	TransitionReason       string               `json:"transition_reason,omitempty"`
}

// ApplicationRelease is the top-level desired-state object for web application lifecycle.
type ApplicationRelease struct {
	Meta   *ObjectMeta               `json:"meta,omitempty"`
	Spec   *ApplicationReleaseSpec   `json:"spec,omitempty"`
	Status *ApplicationReleaseStatus `json:"status,omitempty"`
}

// ── Infrastructure Lifecycle v1 ───────────────────────────────────────────────

// InfrastructureReleaseSpec declares the desired state of an infrastructure component (etcd, minio, envoy, etc.).
type InfrastructureReleaseSpec struct {
	PublisherID      string            `json:"publisher_id,omitempty"`
	Component        string            `json:"component,omitempty"` // e.g. "etcd", "minio", "envoy"
	Version          string            `json:"version,omitempty"`
	BuildNumber      int64             `json:"build_number,omitempty"` // Build iteration within version (0 = legacy)
	BuildID          string            `json:"build_id,omitempty"`     // Phase 2: exact artifact identity (UUIDv7)
	Channel          string            `json:"channel,omitempty"` // Deprecated: functionally ignored
	RepositoryID     string            `json:"repository_id,omitempty"`
	Platform         string            `json:"platform,omitempty"`
	NodeAssignments  []*NodeAssignment `json:"node_assignments,omitempty"`
	DataDirs         string            `json:"data_dirs,omitempty"`          // Comma-separated directories to create
	Unit             string            `json:"unit,omitempty"`               // Systemd unit name (default: globular-{component}.service)
	UpgradeStrategy  string            `json:"upgrade_strategy,omitempty"`   // "stop-start" (default), "rolling"
	HealthEndpoint   string            `json:"health_endpoint,omitempty"`    // Health check URL or command
	RolloutStrategy  string            `json:"rollout_strategy,omitempty"`   // RolloutRolling | RolloutAllAtOnce
	MaxParallelNodes uint32            `json:"max_parallel_nodes,omitempty"`
	Removing         bool              `json:"removing,omitempty"`
}

// InfrastructureReleaseStatus is the controller-managed status of an InfrastructureRelease.
type InfrastructureReleaseStatus struct {
	Phase                  string               `json:"phase,omitempty"`
	ResolvedVersion        string               `json:"resolved_version,omitempty"`
	ResolvedBuildNumber    int64                `json:"resolved_build_number,omitempty"`
	ResolvedBuildID        string               `json:"resolved_build_id,omitempty"`       // Phase 2
	ResolvedArtifactDigest string               `json:"resolved_artifact_digest,omitempty"`
	DesiredHash            string               `json:"desired_hash,omitempty"`
	Nodes                  []*NodeReleaseStatus `json:"nodes,omitempty"`
	Message                string               `json:"message,omitempty"`
	LastTransitionUnixMs   int64                `json:"last_transition_unix_ms,omitempty"`
	ObservedGeneration     int64                `json:"observed_generation,omitempty"`
	WorkflowKind           string               `json:"workflow_kind,omitempty"`
	StartedAtUnixMs        int64                `json:"started_at_unix_ms,omitempty"`
	TransitionReason       string               `json:"transition_reason,omitempty"`
}

// InfrastructureRelease is the top-level desired-state object for infrastructure component lifecycle.
//
// +globular:schema:key="/globular/resources/InfrastructureRelease/{publisher}/{name}"
// +globular:schema:writer="globular-cluster-controller"
// +globular:schema:readers="globular-node-agent,globular-repository,globular-cluster-doctor"
// +globular:schema:description="Cluster-wide desired version for an INFRASTRUCTURE-kind package (etcd, minio, scylla, etc.)."
// +globular:schema:invariants="Only set for packages with kind=INFRASTRUCTURE; publisher segment identifies the artifact namespace; status.phase drives the rollout workflow."
type InfrastructureRelease struct {
	Meta   *ObjectMeta                  `json:"meta,omitempty"`
	Spec   *InfrastructureReleaseSpec   `json:"spec,omitempty"`
	Status *InfrastructureReleaseStatus `json:"status,omitempty"`
}

// ── Install Policy ──────────────────────────────────────────────────────────

// InstallPolicySpec controls which artifacts a cluster will accept during resolution.
//go:schemalint:ignore — implementation type, not schema owner
type InstallPolicySpec struct {
	VerifiedPublishersOnly bool     `json:"verified_publishers_only,omitempty"` // only allow artifacts from claimed namespaces
	AllowedNamespaces      []string `json:"allowed_namespaces,omitempty"`       // whitelist (empty = allow all)
	BlockedNamespaces      []string `json:"blocked_namespaces,omitempty"`       // blacklist (checked after allowed)
	BlockDeprecated        bool     `json:"block_deprecated,omitempty"`         // skip DEPRECATED artifacts in resolution
	BlockYanked            bool     `json:"block_yanked,omitempty"`             // true by default in resolution logic
}

// InstallPolicyResource is the top-level desired-state object for consumer install policy.
// Stored in etcd at /globular/resources/InstallPolicy/{name}.
// Named "Resource" to avoid collision with the proto-generated InstallPolicy message.
//
// +globular:schema:key="/globular/resources/InstallPolicy/{name}"
// +globular:schema:writer="globular-cluster-controller"
// +globular:schema:readers="globular-cluster-controller"
// +globular:schema:description="Cluster install policy: which publishers/namespaces are trusted for artifact resolution."
// +globular:schema:invariants="Evaluated during release resolution; verified_publishers_only + allowed_namespaces AND-intersect."
type InstallPolicyResource struct {
	Meta   *ObjectMeta        `json:"meta,omitempty"`
	Spec   *InstallPolicySpec `json:"spec,omitempty"`
	Status *ObjectStatus      `json:"status,omitempty"`
}

// ── State Alignment Report ───────────────────────────────────────────────────

// PackageAlignmentStatus describes the state alignment of a single package
// across the 4 layers: artifact, desired release, installed observed, runtime.
//go:schemalint:ignore — implementation type, not schema owner
type PackageAlignmentStatus struct {
	Name                string `json:"name"`
	Kind                string `json:"kind"`                              // SERVICE, APPLICATION, INFRASTRUCTURE
	Status              string `json:"status"`                            // aligned, repaired, missing_in_repo, unmanaged, drifted, orphaned
	InstalledVersion    string `json:"installed_version,omitempty"`       // from installed-state registry
	InstalledBuildNum   int64  `json:"installed_build_number,omitempty"`  // installed build iteration
	InstalledBuildID    string `json:"installed_build_id,omitempty"`      // Phase 2: exact installed artifact identity
	DesiredVersion      string `json:"desired_version,omitempty"`         // from desired release
	DesiredBuildNum     int64  `json:"desired_build_number,omitempty"`    // desired build iteration
	DesiredBuildID      string `json:"desired_build_id,omitempty"`        // Phase 2: exact desired artifact identity
	RepoVersion         string `json:"repo_version,omitempty"`            // latest available in repository
	RepoBuildNum        int64  `json:"repo_build_number,omitempty"`       // latest build in repository
	Message             string `json:"message,omitempty"`                 // human-readable explanation
}

// StateAlignmentReport is the result of a repair/convergence check.
type StateAlignmentReport struct {
	Packages       []*PackageAlignmentStatus `json:"packages"`
	Aligned        int                       `json:"aligned"`
	Repaired       int                       `json:"repaired"`
	Drifted        int                       `json:"drifted"`
	Unmanaged      int                       `json:"unmanaged"`
	MissingInRepo  int                       `json:"missing_in_repo"`
	Orphaned       int                       `json:"orphaned"`
	Errors         int                       `json:"errors"`
	RepositoryAddr string                    `json:"repository_addr,omitempty"`
}

// RepairStateAlignmentRequest controls the repair operation.
type RepairStateAlignmentRequest struct {
	DryRun bool `json:"dry_run,omitempty"` // if true, report only — don't repair
}

// WatchEvent is a resource change notification sent over the Watch stream.
type WatchEvent struct {
	EventType             string                 `json:"event_type,omitempty"`
	ResourceVersion       string                 `json:"resource_version,omitempty"`
	ClusterNetwork        *ClusterNetwork        `json:"cluster_network,omitempty"`
	ServiceDesiredVersion *ServiceDesiredVersion `json:"service_desired_version,omitempty"`
	ServiceRelease        *ServiceRelease        `json:"service_release,omitempty"`
	ApplicationRelease    *ApplicationRelease    `json:"application_release,omitempty"`
	InfrastructureRelease *InfrastructureRelease `json:"infrastructure_release,omitempty"`
}

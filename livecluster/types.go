// Package livecluster integrates live cluster signals with Awareness preflight.
// Static awareness is the skeleton; live signals are the pulse.
package livecluster

import "context"

// CollectSignalsRequest specifies what to collect and for which context.
type CollectSignalsRequest struct {
	ClusterID       string
	SessionID       string
	Task            string
	Files           []string
	Components      []string
	Services        []string
	LookbackHours   int
	RequireLiveData bool
}

// ClusterSignalSnapshot is the collected point-in-time view of the live cluster.
type ClusterSignalSnapshot struct {
	ID               string
	ClusterID        string
	NodeID           string
	CollectedAt      int64
	CollectorVersion string
	Status           string // healthy | degraded | critical | unknown
	Summary          string
	Services         []ServiceLiveState
	Errors           []RecentErrorSignature
	Convergence      []RuntimeConvergenceState
	Incidents        []ActiveClusterIncident
	Sources          []SignalSourceStatus
}

// ServiceLiveState is the observed health of a single service.
type ServiceLiveState struct {
	ServiceName         string `json:"service_name"`
	Component           string `json:"component"`
	NodeID              string `json:"node_id"`
	Status              string `json:"status"`    // running | stopped | restarting | unknown
	Health              string `json:"health"`    // healthy | degraded | unhealthy | unreachable | unknown
	HeartbeatAgeSeconds int64  `json:"heartbeat_age_seconds"`
	Readiness           string `json:"readiness"` // ready | not_ready | unknown
	DependencyState     string `json:"dependency_state"`
	LastError           string `json:"last_error,omitempty"`
}

// RecentErrorSignature is a deduplicated error pattern observed in logs/events.
type RecentErrorSignature struct {
	ServiceName       string   `json:"service_name"`
	Component         string   `json:"component"`
	NodeID            string   `json:"node_id"`
	Signature         string   `json:"signature"`
	Severity          string   `json:"severity"` // info | warning | critical
	Count             int      `json:"count"`
	FirstSeen         int64    `json:"first_seen"`
	LastSeen          int64    `json:"last_seen"`
	Sample            string   `json:"sample,omitempty"`
	RelatedFiles      []string `json:"related_files,omitempty"`
	RelatedInvariants []string `json:"related_invariants,omitempty"`
}

// RuntimeConvergenceState describes how a component's desired/installed/runtime states align.
type RuntimeConvergenceState struct {
	Component         string `json:"component"`
	DesiredState      string `json:"desired_state"`
	InstalledState    string `json:"installed_state"`
	RuntimeState      string `json:"runtime_state"`
	ConvergenceStatus string `json:"convergence_status"` // converged | pending | in_progress | blocked | stuck | flapping | diverged | unknown
	BlockedReason     string `json:"blocked_reason,omitempty"`
	RetryCount        int    `json:"retry_count"`
	AgeSeconds        int64  `json:"age_seconds"`
	RelatedKey        string `json:"related_key,omitempty"`
}

// ActiveClusterIncident is an ongoing incident surfaced from any source.
type ActiveClusterIncident struct {
	IncidentID  string `json:"incident_id"`
	Source      string `json:"source"` // doctor | ai_watcher | pattern | manual
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Status      string `json:"status"` // active | investigating | mitigating | resolved
	Component   string `json:"component"`
	ServiceName string `json:"service_name"`
	NodeID      string `json:"node_id"`
	Summary     string `json:"summary"`
	StartedAt   int64  `json:"started_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// SignalSourceStatus reports the collection outcome from a single source.
type SignalSourceStatus struct {
	Name        string `json:"name"`
	Status      string `json:"status"`      // ok | degraded | unavailable | not_configured | timeout
	Message     string `json:"message,omitempty"`
	CollectedAt int64  `json:"collected_at"`
}

// LivePreflightRequest specifies what to check in a live preflight.
type LivePreflightRequest struct {
	SessionID       string
	Task            string
	Files           []string
	Components      []string
	Services        []string
	StaticResultID  string
	LookbackHours   int
	RequireLiveData bool
}

// LivePreflightResult is the combined static+live verdict.
type LivePreflightResult struct {
	ID               string
	SessionID        string
	Task             string
	Files            []string
	Components       []string
	StaticResultID   string
	SignalSnapshotID string
	Verdict          string // allow | allow_with_warnings | block | unknown
	Severity         string // info | warning | critical
	Summary          string
	Blockers         []LivePreflightFinding
	Warnings         []LivePreflightFinding
	Confirmations    []LivePreflightFinding
}

// LivePreflightFinding is a single signal that contributed to the verdict.
type LivePreflightFinding struct {
	Kind        string `json:"kind"`         // active_incident | convergence | service_health | recent_errors | source_unavailable
	Severity    string `json:"severity"`
	Component   string `json:"component,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Message     string `json:"message"`
	Evidence    string `json:"evidence,omitempty"`
}

// SignalCollector is the interface for modular live signal sources.
//
// Collect receives a per-collector context with the collector timeout
// already applied. Implementations must honor cancellation so the snapshot
// loop can degrade individual sources without blocking the whole call.
type SignalCollector interface {
	Name() string
	Available(ctx context.Context) bool
	Collect(ctx context.Context, req CollectSignalsRequest) (*SignalSourceResult, error)
}

// SignalSourceResult carries what a single collector found.
type SignalSourceResult struct {
	Source      SignalSourceStatus
	Services    []ServiceLiveState
	Errors      []RecentErrorSignature
	Convergence []RuntimeConvergenceState
	Incidents   []ActiveClusterIncident
}

package runtime

import "time"

// Fact is a live operational fact collected from a running system.
type Fact struct {
	Source    string            `json:"source"`
	Kind      string            `json:"kind"`
	ID        string            `json:"id"`
	Value     any               `json:"value,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
}

// EvidenceQuery filters evidence collection.
type EvidenceQuery struct {
	Kind   string            `json:"kind,omitempty"`
	ID     string            `json:"id,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Evidence is a runtime observation returned in response to a query.
type Evidence struct {
	Source    string            `json:"source"`
	Kind      string            `json:"kind"`
	ID        string            `json:"id"`
	Summary   string            `json:"summary,omitempty"`
	Payload   any               `json:"payload,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
}

// DoctorReport is the health summary returned by an adapter's Doctor call.
type DoctorReport struct {
	Adapter  string          `json:"adapter"`
	Enabled  bool            `json:"enabled"`
	Status   string          `json:"status"` // ok | degraded | unavailable | runtime_disabled | error
	Findings []DoctorFinding `json:"findings,omitempty"`
}

// DoctorFinding is a single health observation within a DoctorReport.
type DoctorFinding struct {
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Description string `json:"description,omitempty"`
	RuleRef     string `json:"rule_ref,omitempty"` // maps to InvariantRef in Globular
	Suppressed  bool   `json:"suppressed,omitempty"`
}

// SourceInfo describes the health of one runtime data source.
type SourceInfo struct {
	Source  string `json:"source"`
	Healthy bool   `json:"healthy"`
	Noop    bool   `json:"noop"`
}

// ServiceStatusFact is a runtime service health observation.
type ServiceStatusFact struct {
	ServiceID string `json:"service_id"`
	NodeID    string `json:"node_id"`
	State     string `json:"state"`
	Version   string `json:"version,omitempty"`
}

// WorkflowReceiptFact is a runtime workflow execution observation.
type WorkflowReceiptFact struct {
	WorkflowType string `json:"workflow_type"`
	Status       string `json:"status"`
	ServiceID    string `json:"service_id,omitempty"`
	ErrorMsg     string `json:"error_msg,omitempty"`
}

// StateDeltaFact is a runtime desired-vs-installed state mismatch.
type StateDeltaFact struct {
	ServiceID        string `json:"service_id"`
	NodeID           string `json:"node_id,omitempty"`
	DeltaType        string `json:"delta_type"`
	DesiredVersion   string `json:"desired_version,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
}

// SignalOptions configures a CollectSignals call.
type SignalOptions struct {
	Window            time.Duration
	KnownInvariants   []string
	KnownFailureModes []string
}

// RuntimeSignals is an adapter-agnostic collection of live runtime evidence.
type RuntimeSignals struct {
	ID                  string
	CapturedAt          time.Time
	DoctorFindings      []DoctorFinding
	ServiceStatuses     []ServiceStatusFact
	WorkflowReceipts    []WorkflowReceiptFact
	StateDeltas         []StateDeltaFact
	MatchedInvariants   []string
	MatchedFailureModes []string
	Warnings            []string
	SourceInfo          []SourceInfo
	Facts               []Fact
}

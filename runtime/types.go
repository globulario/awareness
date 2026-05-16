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
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

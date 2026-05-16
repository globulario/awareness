// Package finding defines the canonical finding type shared across all
// Awareness analysis: preflight, graph queries, runtime health checks,
// and bundle validation.
package finding

// Severity classifies the urgency of a finding.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
)

// Finding is a single actionable observation produced by Awareness analysis.
type Finding struct {
	Code       string            `json:"code"`
	Severity   Severity          `json:"severity"`
	Message    string            `json:"message"`
	File       string            `json:"file,omitempty"`
	Line       int               `json:"line,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

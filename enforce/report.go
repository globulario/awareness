// Package enforce validates awareness annotations, data contracts, test
// obligations, and graph drift. It provides the backing logic for
// "globular awareness audit" and Claude Code hooks.
package enforce

// FindingSeverity classifies the urgency of an audit finding.
type FindingSeverity string

const (
	SeverityError   FindingSeverity = "ERROR"
	SeverityWarning FindingSeverity = "WARNING"
	SeverityInfo    FindingSeverity = "INFO"
)

// Finding is a single actionable audit result.
type Finding struct {
	Code     string          // machine-readable code, e.g. "MALFORMED_STATE_TRANSITION"
	Severity FindingSeverity
	File     string // source file (relative path or "")
	Symbol   string // function / type name ("" when file-level)
	Message  string // human-readable explanation
}

// AuditResult aggregates all findings from a full enforcement run.
type AuditResult struct {
	Findings     []Finding
	ErrorCount   int
	WarningCount int
	InfoCount    int
	Pass         bool // true when ErrorCount == 0
}

// newAuditResult assembles an AuditResult from a raw finding slice.
func newAuditResult(findings []Finding) *AuditResult {
	r := &AuditResult{Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case SeverityError:
			r.ErrorCount++
		case SeverityWarning:
			r.WarningCount++
		default:
			r.InfoCount++
		}
	}
	r.Pass = r.ErrorCount == 0
	return r
}

// HookResult is the structured output of a Claude Code pre-edit hook run.
// It is consumed by awareness_enforce_cmd.go and serialised to stdout for
// the Claude Code hook runtime.
type HookResult struct {
	HasFindings bool
	ShouldBlock bool // true when any ERROR finding exists
	Summary     string
	Findings    []Finding
}

// PRReport is produced by "globular awareness pr-report --from-git-diff".
// It focuses on changed files only and is intended for CI annotations.
type PRReport struct {
	ChangedFiles []string
	Findings     []Finding
	ErrorCount   int
	WarningCount int
	Pass         bool
}

package preflight

// PreflightResult is the lightweight result type produced by the standalone
// awareness preflight CLI command. It does not depend on any services types.
type PreflightResult struct {
	ProjectName    string             `json:"project_name"`
	Task           string             `json:"task,omitempty"`
	ChangedFiles   []string           `json:"changed_files,omitempty"`
	Classification []TaskClass        `json:"classification,omitempty"`
	Invariants     []string           `json:"invariants,omitempty"`
	FailureModes   []string           `json:"failure_modes,omitempty"`
	ForbiddenFixes []string           `json:"forbidden_fixes,omitempty"`
	RawMatches     []RawKnowledgeMatch `json:"raw_matches,omitempty"`
	RuntimeStatus  string             `json:"runtime_status"` // "disabled" | "ok"
	Warnings       []string           `json:"warnings,omitempty"`
	OK             bool               `json:"ok"`
}

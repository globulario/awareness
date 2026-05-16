package preflight

// PreflightResult is the lightweight result type produced by the standalone
// awareness preflight CLI command. It does not depend on any services types.
type PreflightResult struct {
	ProjectName      string              `json:"project_name"`
	Task             string              `json:"task,omitempty"`
	ChangedFiles     []string            `json:"changed_files,omitempty"`
	Classification   []TaskClass         `json:"classification,omitempty"`
	Invariants       []string            `json:"invariants,omitempty"`
	FailureModes     []string            `json:"failure_modes,omitempty"`
	ForbiddenFixes   []string            `json:"forbidden_fixes,omitempty"`
	IncidentPatterns []string            `json:"incident_patterns,omitempty"`
	// Extended knowledge fields (missing pieces).
	Decisions            []string `json:"decisions,omitempty"`
	ForbiddenAssumptions []string `json:"forbidden_assumptions,omitempty"`
	AuthorityRules       []string `json:"authority_rules,omitempty"`
	RequiredTests        []string `json:"required_tests,omitempty"`
	PreflightQuestions   []string `json:"preflight_questions,omitempty"`
	Questions            []string `json:"questions,omitempty"`
	RemediationContracts []string `json:"remediation_contracts,omitempty"`
	Verdict              string   `json:"verdict,omitempty"`   // "ok" | "warn" | "ask_for_evidence"
	Confidence           string   `json:"confidence,omitempty"` // knowledge.Confidence constants
	RawMatches           []RawKnowledgeMatch `json:"raw_matches,omitempty"`
	RuntimeStatus        string              `json:"runtime_status"` // "disabled" | "ok"
	Warnings             []string            `json:"warnings,omitempty"`
	OK                   bool                `json:"ok"`
}

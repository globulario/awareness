package preflight

import (
	"github.com/globulario/awareness/knowledge"
	"github.com/globulario/awareness/project"
)

// UniqueStrings deduplicates a string slice, preserving order.
func UniqueStrings(in []string) []string {
	return uniqueStrings(in)
}

// PreflightResult is a lightweight preflight result for standalone (offline) use.
// For full graph-backed results see Report.
type PreflightResult struct {
	ProjectName          string              `json:"project_name"`
	Task                 string              `json:"task"`
	ChangedFiles         []string            `json:"changed_files"`
	Classification       []TaskClass         `json:"classification"`
	Invariants           []string            `json:"invariants"`
	FailureModes         []string            `json:"failure_modes"`
	ForbiddenFixes       []string            `json:"forbidden_fixes"`
	IncidentPatterns     []string            `json:"incident_patterns"`
	Decisions            []string            `json:"decisions"`
	ForbiddenAssumptions []string            `json:"forbidden_assumptions"`
	AuthorityRules       []string            `json:"authority_rules"`
	RequiredTests        []string            `json:"required_tests"`
	PreflightQuestions   []string            `json:"preflight_questions"`
	Questions            []string            `json:"questions"`
	RemediationContracts []string            `json:"remediation_contracts"`
	Verdict              string              `json:"verdict"`
	Confidence           string              `json:"confidence"`
	RawMatches           []RawKnowledgeMatch `json:"raw_matches"`
	RuntimeStatus        string              `json:"runtime_status"`
	Warnings             []string            `json:"warnings"`
	OK                   bool                `json:"ok"`
}

// ExtendedPreflightItems holds path-pattern and task-term matched knowledge.
type ExtendedPreflightItems struct {
	RequiredTests        []string
	PreflightQuestions   []string
	Questions            []string
	Decisions            []string
	ForbiddenAssumptions []string
	AuthorityRules       []string
	RemediationContracts []string
}

// RawKnowledgeFallbackFromPaths runs keyword-scored knowledge search over
// the awareness paths in the project profile.
func RawKnowledgeFallbackFromPaths(task string, files []string, aware project.AwarenessPaths) []RawKnowledgeMatch {
	return RawKnowledgeFallback(task, files, aware.Root)
}

// ExtendedPreflightItemsFromPaths uses path-glob and task-term matching to
// find required tests, preflight questions, decisions, forbidden assumptions,
// authority rules, and remediation contracts relevant to the current task.
func ExtendedPreflightItemsFromPaths(task string, files []string, aware project.AwarenessPaths) *ExtendedPreflightItems {
	base, err := knowledge.LoadFromPaths(
		aware.Invariants,
		aware.FailureModes,
		aware.ForbiddenFixes,
		aware.IncidentPatterns,
		aware.Root,
	)
	if err != nil || base == nil {
		return nil
	}

	matched := knowledge.MatchedPreflightItems(base, task, files)

	return &ExtendedPreflightItems{
		RequiredTests:        matched.RequiredTests,
		PreflightQuestions:   matched.PreflightQuestions,
		Questions:            matched.Questions,
		Decisions:            matched.Decisions,
		ForbiddenAssumptions: matched.ForbiddenAssumptions,
		AuthorityRules:       matched.AuthorityRules,
		RemediationContracts: matched.RemediationContracts,
	}
}

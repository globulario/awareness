package preflight

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Format is the output format for a preflight report.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
	FormatAgent    Format = "agent"
)

const (
	agentTopRootCauseLimit     = 8
	agentTopForbiddenLimit     = 8
	agentTopCodeSmellsLimit    = 8
	agentTopInspectLimit       = 8
	agentTopRequiredTestsLimit = 12
)

type Verbosity string

const (
	VerbosityCompact  Verbosity = "compact"
	VerbosityStandard Verbosity = "standard"
	VerbosityFull     Verbosity = "full"
)

// Budget is a high-level control that selects which sections appear in agent
// format output and implies a default verbosity. It takes precedence over
// the Verbosity field when set. Safety fields (safety_status, risk_tier,
// confidence, coverage, trust, forbidden_fixes, required_tests) are always
// present regardless of budget.
type Budget string

const (
	// BudgetCompact emits only the essential safety fields: classification
	// header, safety_status, risk_tier, confidence, trust, warnings, top-3
	// findings, top-3 forbidden fixes, top-5 required tests, agent
	// instruction. All other sections (decision traces, design patterns,
	// anti-patterns, code smells, did-we-fix, experience hints, required
	// searches, investigation order, package admission, cycles) are omitted.
	BudgetCompact Budget = "compact"

	// BudgetStandard is the current default: all sections with existing top-N
	// limits. Equivalent to no --budget flag.
	BudgetStandard Budget = "standard"

	// BudgetDeep is standard plus full decision traces (no truncation) with
	// all pivots expanded. Use for architecture-sensitive changes.
	BudgetDeep Budget = "deep"

	// BudgetForensic is full verbosity across all sections. Use only when the
	// cluster is actively broken or the root cause is unknown.
	BudgetForensic Budget = "forensic"
)

type RenderOptions struct {
	Verbosity Verbosity
	Budget    Budget
}

// effectiveVerbosity returns the verbosity to use for rendering, applying
// the budget's implied verbosity when a budget is set.
func effectiveVerbosity(opts RenderOptions) Verbosity {
	switch opts.Budget {
	case BudgetCompact:
		return VerbosityCompact
	case BudgetForensic:
		return VerbosityFull
	}
	if opts.Verbosity != "" {
		return opts.Verbosity
	}
	return VerbosityStandard
}

// Render formats a Report for the given output format.
func Render(r *Report, format Format) (string, error) {
	return RenderWithOptions(r, format, RenderOptions{Verbosity: VerbosityStandard})
}

// RenderWithOptions formats a Report for the given output format and render options.
func RenderWithOptions(r *Report, format Format, opts RenderOptions) (string, error) {
	switch format {
	case FormatJSON:
		return renderJSON(r)
	case FormatAgent:
		return renderAgent(r, opts), nil
	default:
		return renderMarkdown(r), nil
	}
}

// renderJSON returns the report as a canonical JSON document.
func renderJSON(r *Report) (string, error) {
	// Build the JSON-friendly shape matching the spec schema.
	type didWeFixJSON struct {
		Status          string   `json:"status"`
		MatchedPatterns []string `json:"matched_patterns"`
		FixCases        []string `json:"fix_cases"`
		RemainingGaps   []string `json:"remaining_gaps"`
	}

	type jsonReport struct {
		Task                        string                   `json:"task"`
		Classification              []TaskClass              `json:"classification"`
		MatchedAliases              []string                 `json:"matched_aliases"`
		Services                    []string                 `json:"services"`
		Packages                    []string                 `json:"packages"`
		Files                       []string                 `json:"files"`
		Invariants                  []string                 `json:"invariants"`
		FailureModes                []string                 `json:"failure_modes"`
		ForbiddenFixes              []string                 `json:"forbidden_fixes"`
		CodeSmells                  []string                 `json:"code_smells,omitempty"`
		DesignPatterns              []string                 `json:"design_patterns,omitempty"`
		AntiPatterns                []string                 `json:"anti_patterns,omitempty"`
		HashSchemas                 []string                 `json:"hash_schemas,omitempty"`
		StateTransitions            []string                 `json:"state_transitions,omitempty"`
		DidWeFix                    *DidWeFixSection         `json:"did_we_fix"`
		PackageAdmission            *PackageAdmissionSection `json:"package_admission,omitempty"`
		Cycles                      []CycleWarning           `json:"cycles"`
		RequiredTests               []string                 `json:"required_tests"`
		RequiredSearches            []string                 `json:"required_searches"`
		MatchedDecisions            []string                 `json:"matched_decisions,omitempty"`
		MatchedForbiddenAssumptions []string                 `json:"matched_forbidden_assumptions,omitempty"`
		MatchedAuthorityRules       []string                 `json:"matched_authority_rules,omitempty"`
		MatchedPreflightQuestions   []string                 `json:"matched_preflight_questions,omitempty"`
		MatchedRemediationContracts []string                 `json:"matched_remediation_contracts,omitempty"`
		RecommendedInvestigation    []string                 `json:"recommended_investigation_order"`
		AgentInstruction            string                   `json:"agent_instruction"`
		Warnings                    []string                 `json:"warnings"`
		Runtime                     *RuntimeSection          `json:"runtime,omitempty"`
		Confidence                  Confidence               `json:"confidence"`
		ConfidenceReason            string                   `json:"confidence_reason"`
		Coverage                    Coverage                 `json:"coverage"`
		BlindSpots                  []string                 `json:"blind_spots,omitempty"`
		GraphFreshness              *GraphFreshnessReport    `json:"graph_freshness,omitempty"`
		GraphAvailable              bool                     `json:"graph_available"`
		GraphMatchCount             int                      `json:"graph_match_count"`
		GraphFilteredByTrustCount   int                      `json:"graph_filtered_by_trust_count"`
		RawYAMLMatchCount           int                      `json:"raw_yaml_match_count"`
		FilteredMatches             []FilteredMatch          `json:"filtered_matches,omitempty"`
		ConfidenceFactors           ConfidenceFactors        `json:"confidence_factors"`
		SafetyStatus                SafetyStatus             `json:"safety_status"`
		DegradedMode                DegradedModePlaybook     `json:"degraded_mode"`
		RiskTier                    RiskTier                 `json:"risk_tier"`
		FastPathApplied             bool                     `json:"fast_path_applied"`
		ExperienceHints             []ExperienceHint         `json:"experience_hints,omitempty"`
		Trust                       interface{}              `json:"trust,omitempty"`
	}

	jr := jsonReport{
		Task:                      r.Task,
		Classification:            r.Classification,
		MatchedAliases:            orEmpty(r.MatchedAliases),
		Services:                  orEmpty(r.Services),
		Packages:                  orEmpty(r.Packages),
		Files:                     orEmpty(r.Files),
		Invariants:                orEmpty(r.Invariants),
		FailureModes:              orEmpty(r.FailureModes),
		ForbiddenFixes:            orEmpty(r.ForbiddenFixes),
		CodeSmells:                r.CodeSmells,
		DesignPatterns:            r.DesignPatterns,
		AntiPatterns:              r.AntiPatterns,
		HashSchemas:               r.HashSchemas,
		StateTransitions:          r.StateTransitions,
		DidWeFix:                  r.DidWeFix,
		PackageAdmission:          r.PackageAdmission,
		Cycles:                    r.Cycles,
		RequiredTests:               orEmpty(r.RequiredTests),
		RequiredSearches:            orEmpty(r.RequiredSearches),
		MatchedDecisions:            r.MatchedDecisions,
		MatchedForbiddenAssumptions: r.MatchedForbiddenAssumptions,
		MatchedAuthorityRules:       r.MatchedAuthorityRules,
		MatchedPreflightQuestions:   r.MatchedPreflightQuestions,
		MatchedRemediationContracts: r.MatchedRemediationContracts,
		RecommendedInvestigation:    orEmpty(r.RecommendedOrder),
		AgentInstruction:          r.AgentInstruction,
		Warnings:                  orEmpty(r.Warnings),
		Runtime:                   r.Runtime,
		Confidence:                r.Confidence,
		ConfidenceReason:          r.ConfidenceReason,
		Coverage:                  r.Coverage,
		BlindSpots:                r.BlindSpots,
		GraphFreshness:            r.GraphFreshness,
		GraphAvailable:            r.GraphAvailable,
		GraphMatchCount:           r.GraphMatchCount,
		GraphFilteredByTrustCount: r.GraphFilteredByTrustCount,
		RawYAMLMatchCount:         r.RawYAMLMatchCount,
		FilteredMatches:           r.FilteredMatches,
		ConfidenceFactors:         r.ConfidenceFactors,
		SafetyStatus:              r.SafetyStatus,
		DegradedMode:              r.DegradedMode,
		RiskTier:                  r.RiskTier,
		FastPathApplied:           r.FastPathApplied,
		ExperienceHints:           r.ExperienceHints,
		Trust:                     r.Trust,
	}

	b, err := json.MarshalIndent(jr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal preflight report: %w", err)
	}
	return string(b), nil
}

// renderMarkdown formats the report as GitHub-flavored Markdown.
func renderMarkdown(r *Report) string {
	var sb strings.Builder

	sb.WriteString("# Globular Awareness Preflight\n\n")

	// Task.
	sb.WriteString("## Task\n\n")
	sb.WriteString(r.Task + "\n\n")

	// Classification.
	sb.WriteString("## Classification\n\n")
	if len(r.Classification) == 0 {
		sb.WriteString("- LOCAL_CODE_CHANGE\n\n")
	} else {
		for _, c := range r.Classification {
			sb.WriteString("- " + string(c) + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Risk tier: `%s`\n\n", r.RiskTier))
	sb.WriteString(fmt.Sprintf("Fast path applied: `%t`\n\n", r.FastPathApplied))
	if r.Trust != nil {
		sb.WriteString("## Trust\n\n")
		sb.WriteString(fmt.Sprintf("- verdict: `%s`\n", r.Trust.Verdict))
		sb.WriteString(fmt.Sprintf("- confidence: `%s`\n", r.Trust.Confidence))
		sb.WriteString(fmt.Sprintf("- freshness: `%s`\n", r.Trust.Freshness))
		sb.WriteString(fmt.Sprintf("- coverage: `%s`\n", r.Trust.Coverage))
		if r.Trust.Reason != "" {
			sb.WriteString("- reason: " + r.Trust.Reason + "\n")
		}
		if len(r.Trust.Limitations) > 0 {
			sb.WriteString("- limitations: " + strings.Join(r.Trust.Limitations, "; ") + "\n")
		}
		if len(r.Trust.RequiredActions) > 0 {
			sb.WriteString("- required_action: " + strings.Join(r.Trust.RequiredActions, "; ") + "\n")
		}
		sb.WriteString("\n")
	}

	// Immediate warnings.
	if len(r.Warnings) > 0 {
		sb.WriteString("## Immediate warning\n\n")
		for _, w := range r.Warnings {
			sb.WriteString("> " + w + "\n")
		}
		sb.WriteString("\n")
	}

	// Matched awareness (aliases).
	sb.WriteString("## Matched awareness\n\n")
	if len(r.MatchedAliases) == 0 {
		sb.WriteString("No context aliases matched.\n\n")
	} else {
		for _, a := range r.MatchedAliases {
			sb.WriteString("- " + a + "\n")
		}
		sb.WriteString("\n")
	}

	// Did we already fix this?
	sb.WriteString("## Did we already fix this?\n\n")
	if r.DidWeFix != nil {
		sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", r.DidWeFix.Status))
		if len(r.DidWeFix.MatchedPatterns) > 0 {
			sb.WriteString("Matched patterns: " + strings.Join(r.DidWeFix.MatchedPatterns, ", ") + "\n\n")
		}
		if len(r.DidWeFix.FixCases) > 0 {
			sb.WriteString("Fix cases: " + strings.Join(r.DidWeFix.FixCases, ", ") + "\n\n")
		}
		if len(r.DidWeFix.RemainingGaps) > 0 {
			sb.WriteString("Remaining gaps:\n")
			for _, g := range r.DidWeFix.RemainingGaps {
				sb.WriteString("- " + g + "\n")
			}
			sb.WriteString("\n")
		}
		if r.DidWeFix.NextAction != "" {
			sb.WriteString("Next action: " + r.DidWeFix.NextAction + "\n\n")
		}
	} else {
		sb.WriteString("No fix-ledger data available.\n\n")
	}

	// Relevant invariants.
	writeListSection(&sb, "## Relevant invariants\n\n", r.Invariants, "No invariants matched.")

	// Known failure modes.
	writeListSection(&sb, "## Known failure modes\n\n", r.FailureModes, "No failure modes matched.")

	// Forbidden fixes.
	writeListSection(&sb, "## Forbidden fixes\n\n", r.ForbiddenFixes, "No forbidden fixes identified.")

	// Matched decisions.
	if len(r.MatchedDecisions) > 0 {
		writeListSection(&sb, "## Matched decisions\n\n", r.MatchedDecisions, "")
	}

	// Forbidden assumptions.
	if len(r.MatchedForbiddenAssumptions) > 0 {
		writeListSection(&sb, "## Forbidden assumptions\n\n", r.MatchedForbiddenAssumptions, "")
	}

	// Authority rules.
	if len(r.MatchedAuthorityRules) > 0 {
		writeListSection(&sb, "## Authority rules\n\n", r.MatchedAuthorityRules, "")
	}

	// Preflight questions.
	if len(r.MatchedPreflightQuestions) > 0 {
		writeListSection(&sb, "## Preflight questions\n\n", r.MatchedPreflightQuestions, "")
	}

	// Remediation contracts.
	if len(r.MatchedRemediationContracts) > 0 {
		writeListSection(&sb, "## Remediation contracts\n\n", r.MatchedRemediationContracts, "")
	}

	// Design pattern layer.
	if len(r.DesignPatterns) > 0 {
		writeListSection(&sb, "## Relevant design patterns\n\n", r.DesignPatterns, "")
	}
	if len(r.AntiPatterns) > 0 {
		writeListSection(&sb, "## Anti-patterns to avoid\n\n", r.AntiPatterns, "")
	}

	// Code smells from patterns.
	if len(r.CodeSmells) > 0 {
		writeListSection(&sb, "## Code smells to watch for\n\n", r.CodeSmells, "")
	}

	// Impacted files.
	writeListSection(&sb, "## Impacted files\n\n", r.Files, "No files provided.")

	if len(r.ExperienceHints) > 0 {
		sb.WriteString("## Similar experiences\n\n")
		for i, h := range r.ExperienceHints {
			sb.WriteString(fmt.Sprintf("%d. `%s` (score %.2f)\n", i+1, h.ExperienceID, h.Score))
			if h.Strategy != "" {
				sb.WriteString("   - strategy: " + h.Strategy + "\n")
			}
			if h.Hint != "" {
				sb.WriteString("   - hint: " + h.Hint + "\n")
			}
			if h.Summary != "" {
				sb.WriteString("   - summary: " + h.Summary + "\n")
			}
			if h.Verdict != "" {
				sb.WriteString("   - verdict: " + h.Verdict + "\n")
			}
			if h.FinalScore > 0 {
				sb.WriteString(fmt.Sprintf("   - final score: %.2f\n", h.FinalScore))
			}
			if len(h.Reasons) > 0 {
				sb.WriteString("   - reasons: " + strings.Join(h.Reasons, ", ") + "\n")
			}
			if len(h.WorkedPaths) > 0 {
				sb.WriteString("   - worked paths: " + strings.Join(h.WorkedPaths, " | ") + "\n")
			}
			if len(h.FailedPaths) > 0 {
				sb.WriteString("   - failed paths: " + strings.Join(h.FailedPaths, " | ") + "\n")
			}
			if len(h.EvidenceTypes) > 0 {
				sb.WriteString("   - expected evidence: " + strings.Join(h.EvidenceTypes, ", ") + "\n")
			}
		}
		sb.WriteString("\n")
	}

	// Package admission.
	sb.WriteString("## Package admission\n\n")
	if r.PackageAdmission == nil {
		sb.WriteString("No package provided.\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", r.PackageAdmission.Status))
		for _, reason := range r.PackageAdmission.Reasons {
			sb.WriteString("- " + reason + "\n")
		}
		sb.WriteString("\n")
	}

	// Dependency/cycle risks.
	sb.WriteString("## Dependency/cycle risks\n\n")
	if len(r.Cycles) == 0 {
		sb.WriteString("No dependency cycles detected.\n\n")
	} else {
		for i, c := range r.Cycles {
			sb.WriteString(fmt.Sprintf("**Cycle %d** [%s] phase=%s\n", i+1, c.Classification, c.Phase))
			sb.WriteString("Path: " + strings.Join(c.Path, " → ") + "\n")
			sb.WriteString("Reason: " + c.Reason + "\n\n")
		}
	}

	// Hash schemas (from protocol annotations).
	if len(r.HashSchemas) > 0 {
		writeListSection(&sb, "## Hash schemas\n\n", r.HashSchemas, "")
	}

	// State transitions (from protocol annotations).
	if len(r.StateTransitions) > 0 {
		writeListSection(&sb, "## State transitions\n\n", r.StateTransitions, "")
	}

	// Required tests.
	writeListSection(&sb, "## Required tests\n\n", r.RequiredTests, "No required tests identified.")

	// Required searches.
	writeListSection(&sb, "## Required searches\n\n", r.RequiredSearches, "No required searches identified.")

	if r.DegradedMode.Enabled {
		sb.WriteString("## Degraded-mode playbook\n\n")
		if r.DegradedMode.Reason != "" {
			sb.WriteString("Reason: " + r.DegradedMode.Reason + "\n\n")
		}
		writeListSection(&sb, "### Allowed next steps\n\n", r.DegradedMode.AllowedNextSteps, "None.")
		writeListSection(&sb, "### Blocked actions\n\n", r.DegradedMode.BlockedActions, "None.")
		writeListSection(&sb, "### Stop conditions\n\n", r.DegradedMode.StopConditions, "None.")
	}

	// Recommended investigation order.
	sb.WriteString("## Recommended investigation order\n\n")
	for i, step := range r.RecommendedOrder {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
	}
	sb.WriteString("\n")

	// Agent instruction.
	sb.WriteString("## Agent instruction\n\n")
	sb.WriteString(r.AgentInstruction + "\n\n")

	// Explicit do-not-do list.
	sb.WriteString("## Do not do\n\n")
	if len(r.ForbiddenFixes) > 0 {
		for _, ff := range r.ForbiddenFixes {
			sb.WriteString("- " + ff + "\n")
		}
	} else {
		sb.WriteString("No explicit prohibitions from current graph context.\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// renderAgent formats the report as a directive agent instruction block.
//
// Section inclusion by budget:
//
//	compact   — safety header, root-cause(3), forbidden(3), tests(5), warnings, safety/risk/confidence/trust, agent instruction
//	standard  — all sections with top-N limits (current default)
//	deep      — standard + full decision traces (no truncation)
//	forensic  — all sections at full verbosity
func renderAgent(r *Report, opts RenderOptions) string {
	var sb strings.Builder
	budget := opts.Budget
	verbosity := effectiveVerbosity(opts)

	sb.WriteString("AGENT PREFLIGHT RESULT\n\n")

	// Classification summary — always shown.
	if hasClass(r.Classification, ClassArchitectureSensitive) || hasClass(r.Classification, ClassConvergenceRisk) {
		sb.WriteString("This is architecture-sensitive.\n")
	}
	if hasClass(r.Classification, ClassRestartStorm) {
		sb.WriteString("Restart storm detected — do not patch local restart behavior first.\n")
	}
	if hasClass(r.Classification, ClassStateMismatch) {
		sb.WriteString("State mismatch detected — resolve at the correct layer (Desired → Installed → Runtime).\n")
	}
	if hasClass(r.Classification, ClassDependencyCycle) {
		sb.WriteString("Dependency cycle detected — resolve the cycle before any code change.\n")
	}
	sb.WriteString("\n")
	if r.Coverage.Runtime == CoverageNoop || r.Coverage.IncidentStore == CoverageNotChecked {
		sb.WriteString("Static-only confidence: runtime/incident evidence not fully checked in this run.\n\n")
	}

	// Decision traces: skipped in compact; full expansion in deep/forensic.
	if budget != BudgetCompact {
		traceVerbosity := verbosity
		if budget == BudgetDeep || budget == BudgetForensic {
			traceVerbosity = VerbosityFull
		}
		writeDecisionTraces(&sb, r.DecisionTraces, traceVerbosity)
	}

	// Root-cause area — always shown.
	rootCause := append([]string{}, r.Invariants...)
	rootCause = append(rootCause, r.FailureModes...)
	if len(rootCause) > 0 {
		sb.WriteString("Likely root-cause area:\n")
		rootCause = rankForTask(rootCause, r.Task, r.Files, r.Packages)
		writeAgentTopList(&sb, rootCause, agentLimit(verbosity, agentTopRootCauseLimit, 3))
		sb.WriteString("\n")
	}

	// Forbidden fixes — always shown.
	if len(r.ForbiddenFixes) > 0 {
		sb.WriteString("Forbidden fixes:\n")
		forbidden := rankForTask(r.ForbiddenFixes, r.Task, r.Files, r.Packages)
		writeAgentTopList(&sb, forbidden, agentLimit(verbosity, agentTopForbiddenLimit, 3))
		sb.WriteString("\n")
	}

	// Matched decisions, forbidden assumptions, authority rules, preflight
	// questions, remediation contracts — skipped in compact.
	if budget != BudgetCompact {
		if len(r.MatchedDecisions) > 0 {
			sb.WriteString("Matched decisions:\n")
			for _, d := range r.MatchedDecisions {
				sb.WriteString("- " + d + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.MatchedForbiddenAssumptions) > 0 {
			sb.WriteString("Forbidden assumptions:\n")
			for _, fa := range r.MatchedForbiddenAssumptions {
				sb.WriteString("- " + fa + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.MatchedAuthorityRules) > 0 {
			sb.WriteString("Authority rules:\n")
			for _, ar := range r.MatchedAuthorityRules {
				sb.WriteString("- " + ar + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.MatchedPreflightQuestions) > 0 {
			sb.WriteString("Preflight questions:\n")
			for _, pq := range r.MatchedPreflightQuestions {
				sb.WriteString("- " + pq + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.MatchedRemediationContracts) > 0 {
			sb.WriteString("Remediation guidance:\n")
			for _, rc := range r.MatchedRemediationContracts {
				sb.WriteString("- " + rc + "\n")
			}
			sb.WriteString("\n")
		}
	}

	// Design patterns, anti-patterns, code smells — skipped in compact.
	if budget != BudgetCompact {
		if len(r.DesignPatterns) > 0 {
			sb.WriteString("Relevant design patterns:\n")
			for _, p := range r.DesignPatterns {
				sb.WriteString("- " + p + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.AntiPatterns) > 0 {
			sb.WriteString("Anti-patterns to avoid:\n")
			for _, p := range r.AntiPatterns {
				sb.WriteString("- " + p + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.CodeSmells) > 0 {
			sb.WriteString("Code smells:\n")
			smells := rankForTask(r.CodeSmells, r.Task, r.Files, r.Packages)
			writeAgentTopList(&sb, smells, agentLimit(verbosity, agentTopCodeSmellsLimit, 5))
			sb.WriteString("\n")
		}
	}

	// Did-we-fix and experience hints — skipped in compact.
	if budget != BudgetCompact {
		if r.DidWeFix != nil && r.DidWeFix.Status != "" && r.DidWeFix.Status != "UNKNOWN" {
			sb.WriteString(fmt.Sprintf("Did-we-fix status: %s\n", r.DidWeFix.Status))
			if r.DidWeFix.NextAction != "" {
				sb.WriteString("Next action: " + r.DidWeFix.NextAction + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.ExperienceHints) > 0 {
			sb.WriteString("Similar experiences:\n")
			for _, h := range r.ExperienceHints {
				sb.WriteString(fmt.Sprintf("- %s (score %.2f)\n", h.ExperienceID, h.Score))
				if h.Strategy != "" {
					sb.WriteString("  strategy: " + h.Strategy + "\n")
				}
				if h.Hint != "" {
					sb.WriteString("  hint: " + h.Hint + "\n")
				}
				if h.Verdict != "" {
					sb.WriteString("  verdict: " + h.Verdict + "\n")
				}
				if h.FinalScore > 0 {
					sb.WriteString(fmt.Sprintf("  final score: %.2f\n", h.FinalScore))
				}
				if len(h.Reasons) > 0 {
					sb.WriteString("  reasons: " + strings.Join(h.Reasons, ", ") + "\n")
				}
				if len(h.WorkedPaths) > 0 {
					sb.WriteString("  worked paths: " + strings.Join(h.WorkedPaths, " | ") + "\n")
				}
				if len(h.FailedPaths) > 0 {
					sb.WriteString("  failed paths: " + strings.Join(h.FailedPaths, " | ") + "\n")
				}
				if len(h.EvidenceTypes) > 0 {
					sb.WriteString("  expected evidence: " + strings.Join(h.EvidenceTypes, ", ") + "\n")
				}
			}
			sb.WriteString("\n")
		}
	}

	// Required searches — skipped in compact.
	if budget != BudgetCompact && len(r.RequiredSearches) > 0 {
		sb.WriteString("You must inspect:\n")
		searches := rankForTask(r.RequiredSearches, r.Task, r.Files, r.Packages)
		writeAgentTopList(&sb, searches, agentLimit(verbosity, agentTopInspectLimit, 5))
		sb.WriteString("\n")
	}

	// Required tests — always shown.
	if len(r.RequiredTests) > 0 {
		sb.WriteString("You must run:\n")
		tests := rankForTask(r.RequiredTests, r.Task, r.Files, r.Packages)
		writeAgentTopList(&sb, tests, agentLimit(verbosity, agentTopRequiredTestsLimit, 5))
		sb.WriteString("\n")
	}

	// Investigation order, package admission, cycles — skipped in compact.
	if budget != BudgetCompact {
		if len(r.RecommendedOrder) > 0 {
			sb.WriteString("Investigation order:\n")
			for i, step := range r.RecommendedOrder {
				sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step))
			}
			sb.WriteString("\n")
		}
		if r.PackageAdmission != nil {
			sb.WriteString(fmt.Sprintf("Package admission: %s\n", r.PackageAdmission.Status))
			for _, reason := range r.PackageAdmission.Reasons {
				sb.WriteString("  - " + reason + "\n")
			}
			sb.WriteString("\n")
		}
		if len(r.Cycles) > 0 {
			sb.WriteString("Cycle warnings:\n")
			for _, c := range r.Cycles {
				sb.WriteString(fmt.Sprintf("  [%s] %s → %s\n", c.Classification, c.Phase, strings.Join(c.Path, " → ")))
			}
			sb.WriteString("\n")
		}
	}

	// Warnings — always shown.
	if len(r.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, w := range r.Warnings {
			sb.WriteString("- " + w + "\n")
		}
		sb.WriteString("\n")
	}

	// Safety / confidence / risk / trust — always shown.
	sb.WriteString(fmt.Sprintf("Safety status: %s\n", r.SafetyStatus))
	sb.WriteString(fmt.Sprintf("Confidence: %s (%s)\n\n", r.Confidence, r.ConfidenceReason))
	sb.WriteString(fmt.Sprintf("Risk tier: %s (fast path: %t)\n\n", r.RiskTier, r.FastPathApplied))
	if r.Trust != nil {
		sb.WriteString("Trust envelope:\n")
		sb.WriteString(fmt.Sprintf("  verdict: %s\n", r.Trust.Verdict))
		sb.WriteString(fmt.Sprintf("  confidence: %s\n", r.Trust.Confidence))
		sb.WriteString(fmt.Sprintf("  freshness: %s\n", r.Trust.Freshness))
		sb.WriteString(fmt.Sprintf("  coverage: %s\n", r.Trust.Coverage))
		if len(r.Trust.Limitations) > 0 {
			sb.WriteString("  limitations:\n")
			for _, l := range r.Trust.Limitations {
				sb.WriteString("  - " + l + "\n")
			}
		}
		if len(r.Trust.RequiredActions) > 0 {
			sb.WriteString("  required_action:\n")
			for _, a := range r.Trust.RequiredActions {
				sb.WriteString("  - " + a + "\n")
			}
		}
		sb.WriteString("\n")
	}

	// Degraded mode — always shown.
	if r.DegradedMode.Enabled {
		sb.WriteString("Degraded-mode playbook:\n")
		if r.DegradedMode.Reason != "" {
			sb.WriteString("  reason: " + r.DegradedMode.Reason + "\n")
		}
		if len(r.DegradedMode.AllowedNextSteps) > 0 {
			sb.WriteString("  allowed next steps:\n")
			for _, step := range r.DegradedMode.AllowedNextSteps {
				sb.WriteString("  - " + step + "\n")
			}
		}
		if len(r.DegradedMode.BlockedActions) > 0 {
			sb.WriteString("  blocked actions:\n")
			for _, step := range r.DegradedMode.BlockedActions {
				sb.WriteString("  - " + step + "\n")
			}
		}
		if len(r.DegradedMode.StopConditions) > 0 {
			sb.WriteString("  stop conditions:\n")
			for _, step := range r.DegradedMode.StopConditions {
				sb.WriteString("  - " + step + "\n")
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(r.AgentInstruction + "\n")

	return sb.String()
}

// writeDecisionTraces emits the Phase 9 "Decision traces" section in the
// agent format. Compact by default: top 5 traces (sorted by risk —
// forbidden_fix > invariant > failure_mode > raw_knowledge > experience),
// with each trace capped to top 3 pivots, top 3 actions, top 2 falsifiers,
// and top 3 evidence entries. Full detail is in the JSON output. A normal
// preflight stays well under 200 agent-format lines with this budget.
//
// Render order is the doc's risk ladder, which differs from the JSON
// order (FailureMode first) — agents reading the text get the
// most-actionable findings up top.
func writeDecisionTraces(sb *strings.Builder, traces []DecisionTrace, verbosity Verbosity) {
	if len(traces) == 0 {
		return
	}

	// Cap traces by verbosity. Full verbosity shows everything; standard
	// and compact trim to the top 5.
	maxTraces := agentLimit(verbosity, 5, 3)
	maxPivots := agentLimit(verbosity, 3, 2)
	maxActions := agentLimit(verbosity, 3, 2)
	maxFalsifiers := agentLimit(verbosity, 2, 2)
	maxEvidence := agentLimit(verbosity, 3, 2)

	ranked := make([]DecisionTrace, len(traces))
	copy(ranked, traces)
	sort.SliceStable(ranked, func(i, j int) bool {
		ri, rj := agentTraceRank(ranked[i].FindingType), agentTraceRank(ranked[j].FindingType)
		if ri != rj {
			return ri < rj
		}
		return ranked[i].FindingID < ranked[j].FindingID
	})
	if verbosity != VerbosityFull && len(ranked) > maxTraces {
		ranked = ranked[:maxTraces]
	}

	sb.WriteString("Decision traces:\n")
	for i, tr := range ranked {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("- finding: %s.%s\n", tr.FindingType, tr.FindingID))
		sb.WriteString(fmt.Sprintf("  confidence: %s\n", tr.Confidence))

		if owner := renderOwnerOneLine(tr.Owner); owner != "" {
			sb.WriteString("  owner: " + owner + "\n")
		}

		if len(tr.MatchedBy) > 0 {
			sb.WriteString("  why:\n")
			n := len(tr.MatchedBy)
			if n > maxEvidence {
				n = maxEvidence
			}
			for _, ev := range tr.MatchedBy[:n] {
				sb.WriteString("    - " + renderEvidenceOneLine(ev) + "\n")
			}
		}

		// Pivots: split forbidden_fix entries into their own line group so
		// the agent sees "do not do X" without having to read every pivot.
		var forbiddenPivots, otherPivots []ContextPivot
		for _, p := range tr.Pivots {
			if p.Kind == "forbidden_fix" {
				forbiddenPivots = append(forbiddenPivots, p)
			} else {
				otherPivots = append(otherPivots, p)
			}
		}
		if len(otherPivots) > 0 {
			sb.WriteString("  pivots:\n")
			n := len(otherPivots)
			if n > maxPivots {
				n = maxPivots
			}
			for _, p := range otherPivots[:n] {
				sb.WriteString(fmt.Sprintf("    - %s: %s\n", p.Kind, p.ID))
			}
		}
		if len(forbiddenPivots) > 0 {
			sb.WriteString("  forbidden:\n")
			n := len(forbiddenPivots)
			if n > maxPivots {
				n = maxPivots
			}
			for _, p := range forbiddenPivots[:n] {
				sb.WriteString("    - " + p.ID + "\n")
			}
		}

		if len(tr.NextActions) > 0 {
			sb.WriteString("  next:\n")
			n := len(tr.NextActions)
			if n > maxActions {
				n = maxActions
			}
			for _, a := range tr.NextActions[:n] {
				if a.Command != "" {
					sb.WriteString("    - " + a.Command + "\n")
				} else {
					sb.WriteString("    - " + a.Kind + ": " + a.Reason + "\n")
				}
			}
		}

		if len(tr.Falsifiers) > 0 {
			sb.WriteString("  falsify:\n")
			n := len(tr.Falsifiers)
			if n > maxFalsifiers {
				n = maxFalsifiers
			}
			for _, f := range tr.Falsifiers[:n] {
				sb.WriteString("    - " + f.Claim + "\n")
			}
		}
	}
	if verbosity != VerbosityFull && len(traces) > maxTraces {
		sb.WriteString(fmt.Sprintf("\n  ... %d more trace(s) — see JSON output for full detail\n",
			len(traces)-maxTraces))
	}
	sb.WriteString("\n")
}

// agentTraceRank orders DecisionTrace findings for agent-format display.
// Per the design doc: forbidden_fix > invariant > runtime failure_mode >
// raw_knowledge > experience. The JSON ordering (FailureMode first) is
// preserved for that channel; this is render-only.
func agentTraceRank(t FindingType) int {
	switch t {
	case FindingForbiddenFix:
		return 0
	case FindingInvariant:
		return 1
	case FindingFailureMode:
		return 2
	case FindingRuntime:
		return 3
	case FindingRawKnowledge:
		return 4
	case FindingExperience:
		return 5
	}
	return 99
}

// renderOwnerOneLine collapses an OwnerContext to a single "layer /
// service / package" line, skipping empty fields. Returns "" when every
// owner field is empty.
func renderOwnerOneLine(o OwnerContext) string {
	parts := make([]string, 0, 3)
	if o.Layer != "" && o.Layer != "unknown" {
		parts = append(parts, o.Layer)
	}
	if o.Service != "" {
		parts = append(parts, o.Service)
	}
	if o.Package != "" {
		parts = append(parts, o.Package)
	}
	return strings.Join(parts, " / ")
}

// renderEvidenceOneLine summarises an EvidenceRef into one short line.
// Format: "<source>: <reason>" or "<source> <path_summary>" — whichever
// is more informative. Confidence + freshness appear in parentheses when
// non-default.
func renderEvidenceOneLine(ev EvidenceRef) string {
	main := ev.Source
	switch {
	case ev.PathSummary != "":
		main = ev.Source + ": " + ev.PathSummary
	case ev.Reason != "":
		main = ev.Source + ": " + ev.Reason
	}
	suffix := ""
	if ev.Freshness != "" && ev.Freshness != "unknown" {
		suffix = " [" + ev.Freshness + "]"
	}
	return main + suffix
}

// writeListSection writes a markdown section with a bullet list or fallback message.
func writeListSection(sb *strings.Builder, header string, items []string, empty string) {
	sb.WriteString(header)
	if len(items) == 0 {
		sb.WriteString(empty + "\n\n")
		return
	}
	for _, item := range items {
		sb.WriteString("- " + item + "\n")
	}
	sb.WriteString("\n")
}

// orEmpty returns a non-nil empty slice when in is nil — keeps JSON output stable.
func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func writeAgentTopList(sb *strings.Builder, items []string, limit int) {
	if len(items) == 0 {
		return
	}
	if limit <= 0 || len(items) <= limit {
		for _, item := range items {
			sb.WriteString("- " + item + "\n")
		}
		return
	}
	for _, item := range items[:limit] {
		sb.WriteString("- " + item + "\n")
	}
	sb.WriteString(fmt.Sprintf("- ... %d more (use --format json for full list)\n", len(items)-limit))
}

func agentLimit(v Verbosity, standard, compact int) int {
	switch v {
	case VerbosityFull:
		return 0
	case VerbosityCompact:
		return compact
	default:
		return standard
	}
}

func rankForTask(items []string, task string, files, packages []string) []string {
	if len(items) <= 1 {
		return items
	}
	tokens := make(map[string]struct{})
	for _, tok := range splitTokens(task) {
		tokens[tok] = struct{}{}
	}
	for _, f := range files {
		for _, tok := range splitTokens(f) {
			tokens[tok] = struct{}{}
		}
	}
	for _, p := range packages {
		for _, tok := range splitTokens(p) {
			tokens[tok] = struct{}{}
		}
	}
	type scored struct {
		item  string
		score int
	}
	out := make([]scored, 0, len(items))
	for _, item := range items {
		score := 0
		for _, tok := range splitTokens(item) {
			if _, ok := tokens[tok]; ok {
				score++
			}
		}
		out = append(out, scored{item: item, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].score > out[j].score
	})
	ordered := make([]string, 0, len(out))
	for _, s := range out {
		ordered = append(ordered, s.item)
	}
	return ordered
}

func splitTokens(s string) []string {
	raw := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	out := make([]string, 0, len(raw))
	for _, tok := range raw {
		if len(tok) < 3 {
			continue
		}
		out = append(out, tok)
	}
	return out
}

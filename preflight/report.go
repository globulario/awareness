// Package preflight composes all awareness capabilities into a single
// agent-facing report. No new graph theory — pure composition.
package preflight

import (
	"github.com/globulario/awareness/analysis/contextnav"
	"github.com/globulario/awareness/assurance"
)

// TaskClass labels the nature of a task for agent routing.
type TaskClass string

const (
	ClassLocalCodeChange       TaskClass = "LOCAL_CODE_CHANGE"
	ClassArchitectureSensitive TaskClass = "ARCHITECTURE_SENSITIVE"
	ClassConvergenceRisk       TaskClass = "CONVERGENCE_RISK"
	ClassPackageAdmission      TaskClass = "PACKAGE_ADMISSION"
	ClassRuntimeIncident       TaskClass = "RUNTIME_INCIDENT"
	ClassRetryLoop             TaskClass = "RETRY_LOOP"
	ClassRestartStorm          TaskClass = "RESTART_STORM"
	ClassStateMismatch         TaskClass = "STATE_MISMATCH"
	ClassDependencyCycle       TaskClass = "DEPENDENCY_CYCLE"
	ClassUnknownImpact         TaskClass = "UNKNOWN_IMPACT"
	ClassStaticFallback        TaskClass = "STATIC_KNOWLEDGE_FALLBACK"
)

// CollectorHealthSummary is a compact view of a single collector's outcome.
// Populated from the most recent graph_builds.collector_health_json record.
type CollectorHealthSummary struct {
	CollectorID  string `json:"collector_id"`
	Status       string `json:"status"` // "ok" | "skipped" | "error"
	NodesEmitted int    `json:"nodes_emitted"`
	Error        string `json:"error,omitempty"`
}

// DidWeFixSection summarises the fix-ledger lookup result.
type DidWeFixSection struct {
	Status          string   `json:"status"`
	MatchedPatterns []string `json:"matched_patterns"`
	FixCases        []string `json:"fix_cases"`
	RemainingGaps   []string `json:"remaining_gaps"`
	NextAction      string   `json:"next_action"`
}

// PackageAdmissionSection holds package validation results.
type PackageAdmissionSection struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}

// CycleWarning is a simplified cycle summary for preflight output.
type CycleWarning struct {
	Phase          string   `json:"phase"`
	Classification string   `json:"classification"`
	Path           []string `json:"path"`
	Reason         string   `json:"reason"`
}

// RawKnowledgeMatch is a conservative fallback match from the source YAML files.
// It exists to make NO_MATCH honest: graph lookup can be silent while the
// hand-authored truth files still contain relevant knowledge.
type RawKnowledgeMatch struct {
	Source       string   `json:"source"`
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Score        int      `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
}

// FilteredMatch is a graph match that has low-trust provenance.
// The match is still present in the main invariants/failure_modes lists
// (with lower confidence) but is also reported here so callers understand
// why graph_filtered_by_trust_count > 0.
type FilteredMatch struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`   // invariant, failure_mode, forbidden_fix
	Reason     string `json:"reason"` // stale, inferred, invalid, proposal, missing_provenance
	TrustLevel string `json:"trust_level"`
}

// RuntimeSection holds live cluster evidence included in the preflight.
type RuntimeSection struct {
	Included            bool                     `json:"included"`
	CapturedAt          string                   `json:"captured_at,omitempty"`
	DoctorFindings      []DoctorFindingSummary   `json:"doctor_findings"`
	ServiceStatuses     []ServiceStatusSummary   `json:"service_statuses"`
	WorkflowReceipts    []WorkflowReceiptSummary `json:"workflow_receipts"`
	StateDeltas         []StateDeltaSummary      `json:"state_deltas"`
	MatchedInvariants   []string                 `json:"matched_invariants"`
	MatchedFailureModes []string                 `json:"matched_failure_modes"`
	MetricWarnings      []string                 `json:"metric_warnings,omitempty"`
	Warnings            []string                 `json:"warnings"`
	WorkflowRuntime     *WorkflowRuntimeSection  `json:"workflow_runtime,omitempty"`
}

// DoctorFindingSummary is a compact view of a DoctorFinding.
type DoctorFindingSummary struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
}

// ServiceStatusSummary is a compact view of a ServiceStatus.
type ServiceStatusSummary struct {
	ServiceID string `json:"service_id"`
	State     string `json:"state"`
	NodeID    string `json:"node_id"`
}

// WorkflowReceiptSummary is a compact view of a WorkflowReceipt.
type WorkflowReceiptSummary struct {
	WorkflowType string `json:"workflow_type"`
	Status       string `json:"status"`
	ErrorMsg     string `json:"error_msg,omitempty"`
}

// StateDeltaSummary is a compact view of a StateDelta.
type StateDeltaSummary struct {
	ServiceID string `json:"service_id"`
	DeltaType string `json:"delta_type"`
	Desired   string `json:"desired_version,omitempty"`
	Installed string `json:"installed_version,omitempty"`
}

// Confidence describes how much the preflight knows about the task's safety.
type Confidence string

const (
	ConfidenceHigh                Confidence = "high"                 // graph + raw YAML + runtime + metrics all ran
	ConfidenceMedium              Confidence = "medium"               // graph + raw YAML ran; runtime partial
	ConfidenceLow                 Confidence = "low"                  // only static analysis; runtime unavailable
	ConfidenceUnknown             Confidence = "unknown"              // nothing ran (no graph, no docs dir)
	ConfidenceInsufficientEvidence Confidence = "insufficient_evidence" // partial data cannot confirm or deny
	ConfidenceSuspected           Confidence = "suspected"            // likely but not confirmed by evidence
	ConfidenceStale               Confidence = "stale"                // evidence exists but is too old to trust
	ConfidenceContradicted        Confidence = "contradicted"         // multiple signals disagree
)

// CoverageState describes the result of a single coverage layer check.
type CoverageState string

const (
	CoverageNotChecked       CoverageState = "not_checked"
	CoverageNotApplicable    CoverageState = "not_applicable"
	CoverageCheckedClean     CoverageState = "checked_clean"
	CoverageCheckedWithMatch CoverageState = "checked_with_matches"
	CoverageFailed           CoverageState = "failed"
	CoverageNoop             CoverageState = "noop"
	CoverageStale            CoverageState = "stale"
)

// WorkflowRuntimeSection summarises live workflow execution coverage in the preflight.
type WorkflowRuntimeSection struct {
	Coverage        string `json:"coverage"`  // checked_clean | checked_with_matches | failed | stale | disabled | not_checked
	Freshness       string `json:"freshness"` // fresh | stale | unknown
	Source          string `json:"source"`    // workflow_service_grpc | graph_cache | none
	RunsSeen        int    `json:"runs_seen"`
	FailedRuns      int    `json:"failed_runs"`
	BlockedRuns     int    `json:"blocked_runs"`
	RetryStorms     int    `json:"retry_storms"`
	CollectedAt     string `json:"collected_at,omitempty"`
	TTLSeconds      int    `json:"ttl_seconds,omitempty"`
	CollectorStatus string `json:"collector_status,omitempty"` // ok | partial | failed | disabled
}

// Coverage tracks which evidence layers were checked and their result.
type Coverage struct {
	Graph           CoverageState `json:"graph"`
	RawYAML         CoverageState `json:"raw_yaml"`
	Runtime         CoverageState `json:"runtime"`
	Metrics         CoverageState `json:"metrics"`
	CodeScan        CoverageState `json:"code_scan"`
	IncidentStore   CoverageState `json:"incident_store"`
	WorkflowRuntime CoverageState `json:"workflow_runtime"`
}

// GraphFreshnessReport summarises graph staleness for the report.
type GraphFreshnessReport struct {
	BuiltAt             string  `json:"built_at,omitempty"`
	AgeSeconds          float64 `json:"age_seconds,omitempty"`
	Stale               bool    `json:"stale"`
	StaleReason         string  `json:"stale_reason,omitempty"`
	KnowledgeMtime      string  `json:"knowledge_mtime,omitempty"`
	KnowledgeSourceHash string  `json:"knowledge_source_hash,omitempty"`
	RebuildRecommended  bool    `json:"rebuild_recommended"`
	LastBuildDurationMs int64   `json:"last_build_duration_ms,omitempty"`
}

// GoFileCoverageReport holds the graph Go-file coverage metrics as reported by
// preflight. It mirrors enforce.GoFileCoverageResult but lives here to avoid a
// circular import (enforce imports preflight via strict.go).
type GoFileCoverageReport struct {
	EligibleGoFilesTotal   int      `json:"eligible_go_files_total"`
	IndexedGoFilesTotal    int      `json:"indexed_go_files_total"`
	CoveragePercentGoFiles float64  `json:"coverage_percent_go_files"`
	EligibleNonTestGoFiles int      `json:"eligible_non_test_go_files_total"`
	IndexedNonTestGoFiles  int      `json:"indexed_non_test_go_files_total"`
	MissingFiles           []string `json:"missing_files,omitempty"`
	BlindSpots             []string `json:"blind_spots,omitempty"`
	// ConfidenceImpact: none | low | medium | high
	ConfidenceImpact string `json:"confidence_impact"`
}

// ConfidenceFactors explains why a confidence level was assigned.
type ConfidenceFactors struct {
	Coverage        CoverageState `json:"coverage"`
	Provenance      string        `json:"provenance"`
	GraphFreshness  CoverageState `json:"graph_freshness"`
	PathQuality     string        `json:"path_quality"`
	RuntimeEvidence CoverageState `json:"runtime_evidence"`
}

// SafetyStatus indicates whether the current evidence quality is safe enough
// to proceed without escalation.
type SafetyStatus string

const (
	SafetyStatusProceed        SafetyStatus = "PROCEED"
	SafetyStatusUnknownNotSafe SafetyStatus = "UNKNOWN_NOT_SAFE"
)

type RiskTier string

const (
	RiskLow    RiskTier = "low"
	RiskMedium RiskTier = "medium"
	RiskHigh   RiskTier = "high"
)

// DegradedModePlaybook provides deterministic guidance when evidence quality
// is degraded and preflight cannot safely produce high-confidence decisions.
type DegradedModePlaybook struct {
	Enabled          bool     `json:"enabled"`
	Reason           string   `json:"reason,omitempty"`
	AllowedNextSteps []string `json:"allowed_next_steps,omitempty"`
	BlockedActions   []string `json:"blocked_actions,omitempty"`
	StopConditions   []string `json:"stop_conditions,omitempty"`
}

// LiveOverlayFreshness reports the current freshness of the live mirror overlay.
// It is separate from graph freshness: the graph can be fresh (recently built)
// while the live overlay is stale (no recent live-snapshot run).
type LiveOverlayFreshness struct {
	Status      string                   `json:"status"` // "fresh" | "stale" | "absent" | "failed" | "partial"
	AgeSeconds  float64                  `json:"age_seconds"`
	CollectedAt string                   `json:"collected_at,omitempty"`
	Collectors  []CollectorHealthSummary `json:"collectors,omitempty"`
}

// LiveOverlayTTLSeconds is how long a live snapshot stays "fresh".
const LiveOverlayTTLSeconds = 300 // 5 minutes
// LiveOverlayStaleSeconds is when a live snapshot is considered "stale" but not absent.
const LiveOverlayStaleSeconds = 900 // 15 minutes

// FindingType, EvidenceRef, OwnerContext, ContextPivot, DiagnosticAction,
// Falsifier, and DecisionTrace live in analysis/contextnav as of Phase 2 of
// the context-navigation effort. The aliases below preserve the
// preflight.DecisionTrace surface — callers (CLI, MCP, format helpers) can
// keep importing them from preflight while the build logic and any
// graph-aware analysis moves into contextnav.
type (
	FindingType      = contextnav.FindingType
	EvidenceRef      = contextnav.EvidenceRef
	OwnerContext     = contextnav.OwnerContext
	ContextPivot     = contextnav.ContextPivot
	DiagnosticAction = contextnav.DiagnosticAction
	Falsifier        = contextnav.Falsifier
	DecisionTrace    = contextnav.DecisionTrace
)

const (
	FindingInvariant    = contextnav.FindingInvariant
	FindingFailureMode  = contextnav.FindingFailureMode
	FindingForbiddenFix = contextnav.FindingForbiddenFix
	FindingRawKnowledge = contextnav.FindingRawKnowledge
	FindingRuntime      = contextnav.FindingRuntime
	FindingExperience   = contextnav.FindingExperience
)

// ExperienceHint is a compact similar-experience suggestion shown during preflight.
type ExperienceHint struct {
	ExperienceID  string   `json:"experience_id"`
	Score         float64  `json:"score"`
	Strategy      string   `json:"strategy"`
	Hint          string   `json:"hint"`
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	Verdict       string   `json:"verdict,omitempty"`
	FinalScore    float64  `json:"final_score,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
	WorkedPaths   []string `json:"worked_paths,omitempty"`
	FailedPaths   []string `json:"failed_paths,omitempty"`
	EvidenceTypes []string `json:"evidence_types,omitempty"`
}

// Report is the complete output of a preflight run.
type Report struct {
	Task                string                   `json:"task"`
	CollectorHealth     []CollectorHealthSummary `json:"collector_health,omitempty"`
	Classification      []TaskClass              `json:"classification"`
	MatchedAliases      []string                 `json:"matched_aliases"`
	Services            []string                 `json:"services"`
	Packages            []string                 `json:"packages"`
	Files               []string                 `json:"files"`
	Invariants          []string                 `json:"invariants"`
	FailureModes        []string                 `json:"failure_modes"`
	ForbiddenFixes      []string                 `json:"forbidden_fixes"`
	CodeSmells          []string                 `json:"code_smells,omitempty"`
	DesignPatterns      []string                 `json:"design_patterns,omitempty"`
	AntiPatterns        []string                 `json:"anti_patterns,omitempty"`
	HashSchemas         []string                 `json:"hash_schemas,omitempty"`
	StateTransitions    []string                 `json:"state_transitions,omitempty"`
	DidWeFix            *DidWeFixSection         `json:"did_we_fix"`
	PackageAdmission    *PackageAdmissionSection `json:"package_admission,omitempty"`
	Cycles              []CycleWarning           `json:"cycles"`
	RequiredTests               []string                 `json:"required_tests"`
	RequiredSearches            []string                 `json:"required_searches"`
	MatchedDecisions            []string                 `json:"matched_decisions,omitempty"`
	MatchedForbiddenAssumptions []string                 `json:"matched_forbidden_assumptions,omitempty"`
	MatchedAuthorityRules       []string                 `json:"matched_authority_rules,omitempty"`
	MatchedPreflightQuestions   []string                 `json:"matched_preflight_questions,omitempty"`
	MatchedRemediationContracts []string                 `json:"matched_remediation_contracts,omitempty"`
	RecommendedOrder    []string                 `json:"recommended_investigation_order"`
	AgentInstruction    string                   `json:"agent_instruction"`
	Warnings            []string                 `json:"warnings"`
	RawKnowledgeMatches []RawKnowledgeMatch      `json:"raw_knowledge_matches,omitempty"`
	Runtime             *RuntimeSection          `json:"runtime,omitempty"`
	Confidence          Confidence               `json:"confidence"`
	ConfidenceReason    string                   `json:"confidence_reason"`
	Coverage            Coverage                 `json:"coverage"`
	BlindSpots          []string                 `json:"blind_spots,omitempty"`
	GraphFreshness      *GraphFreshnessReport    `json:"graph_freshness,omitempty"`

	// Graph coverage detail — tells callers WHY a result has no/few matches.
	GraphAvailable            bool                     `json:"graph_available"`
	GraphDBPath               string                   `json:"graph_db_path,omitempty"`
	GraphMatchCount           int                      `json:"graph_match_count"`
	GraphFilteredByTrustCount int                      `json:"graph_filtered_by_trust_count"`
	RawYAMLMatchCount         int                      `json:"raw_yaml_match_count"`
	FilteredMatches           []FilteredMatch          `json:"filtered_matches,omitempty"`
	ConfidenceFactors         ConfidenceFactors        `json:"confidence_factors"`
	SafetyStatus              SafetyStatus             `json:"safety_status"`
	DegradedMode              DegradedModePlaybook     `json:"degraded_mode"`
	RiskTier                  RiskTier                 `json:"risk_tier"`
	FastPathApplied           bool                     `json:"fast_path_applied"`
	GoFileCoverage            *GoFileCoverageReport    `json:"go_file_coverage,omitempty"`
	LiveOverlay               *LiveOverlayFreshness    `json:"live_overlay,omitempty"`
	ExperienceHints           []ExperienceHint         `json:"experience_hints,omitempty"`
	Trust                     *assurance.TrustEnvelope `json:"trust,omitempty"`

	// DecisionTraces is one record per matched finding describing why it
	// fired, ranked pivots into related awareness state, and (eventually)
	// safe next actions and falsifiers. Empty (not nil) when no findings
	// match — the trust envelope is the authority on NO_MATCH safety, and a
	// fabricated trace would only compete with it. See
	// claude_codex_awareness_context_navigation_improvement.md for the full
	// (multi-phase) design; Phase 1 emits MatchedBy + Pivots + generic
	// Falsifier only.
	DecisionTraces []DecisionTrace `json:"decision_traces"`
}

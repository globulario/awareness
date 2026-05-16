// Package contextnav is the per-finding navigation layer of awareness. Given
// what preflight already matched (invariants, failure modes, forbidden fixes,
// raw-yaml fallback hits, runtime evidence, alias triggers), it composes the
// "why did this fire and where do I look next?" decision trace.
//
// Phase 2 of claude_codex_awareness_context_navigation_improvement.md carves
// out this package so subsequent phases (owner inference, ranked pivots,
// per-finding falsifiers, diagnostic actions) have a clean home that does not
// bloat preflight. Phase 2 itself is a structural move: types and the Build
// entry point land here, but the populator logic is the same one that shipped
// in Phase 1 — no new graph traversal, no new analysis.
//
// Why a separate package: preflight is the composition point that runs
// classifiers, hits the graph, and produces a Report. Decision traces are a
// downstream rendering — pure functions over what Report already knows. Phase
// 3+ will introduce graph-aware helpers (owner inference, pivot ranking) that
// would otherwise pull more of analysis/graph into preflight. Keeping the two
// packages separate prevents that creep.
//
// Direction of imports: preflight imports contextnav, never the reverse.
// contextnav has its own Confidence enum because preflight.Confidence is used
// in 70+ places — moving it would touch the whole package. The two enums
// share string values so JSON output stays identical.
package contextnav

// FindingType labels the kind of awareness object a decision trace describes.
// The enum mirrors the four canonical Report match buckets so callers can
// route on type without re-classifying the underlying id.
type FindingType string

const (
	FindingInvariant    FindingType = "invariant"
	FindingFailureMode  FindingType = "failure_mode"
	FindingForbiddenFix FindingType = "forbidden_fix"
	FindingRawKnowledge FindingType = "raw_knowledge"
	FindingRuntime      FindingType = "runtime"
	FindingExperience   FindingType = "experience"
)

// Confidence mirrors preflight.Confidence with identical string values. The
// duplicate type exists to keep contextnav free of a preflight import (that
// would create a cycle). Callers in preflight cast across the two types when
// composing inputs and reading traces.
type Confidence string

const (
	ConfidenceHigh    Confidence = "high"
	ConfidenceMedium  Confidence = "medium"
	ConfidenceLow     Confidence = "low"
	ConfidenceUnknown Confidence = "unknown"
)

// EvidenceRef is one strand of why a finding fired. A DecisionTrace can carry
// multiple — graph match, raw-yaml fallback, alias hit, runtime observation —
// and the verdict reader can see exactly how the match landed instead of
// having to re-derive it from prose. The Source classification is critical:
// agents must distinguish "graph proved this" from "alias guessed this".
type EvidenceRef struct {
	Source      string  `json:"source"` // graph | raw_yaml | runtime | alias | experience
	NodeID      string  `json:"node_id,omitempty"`
	EdgeKind    string  `json:"edge_kind,omitempty"`
	PathSummary string  `json:"path_summary,omitempty"`
	Confidence  float64 `json:"confidence"`
	Freshness   string  `json:"freshness,omitempty"` // fresh | stale | unknown | absent
	Reason      string  `json:"reason,omitempty"`
}

// OwnerContext names which layer owns a finding. Phase 2 leaves most fields
// empty; Phase 3 will infer Layer/Service/Package from graph edges and task
// hints. The type is stable from day one so MCP consumers don't have to
// chase shape changes when Phase 3 lands.
type OwnerContext struct {
	Layer    string   `json:"layer,omitempty"` // repository | desired | installed | runtime | workflow | pki | dns | rbac | unknown
	Service  string   `json:"service,omitempty"`
	Package  string   `json:"package,omitempty"`
	Files    []string `json:"files,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`
	StateIDs []string `json:"state_ids,omitempty"`
}

// ContextPivot is one ranked next-hop a reader can navigate to from a
// finding. Kind discriminates the destination (source_invariant, required_test,
// forbidden_fix, runtime_evidence, etc.) so agents render or follow pivots
// without parsing the ID.
type ContextPivot struct {
	Kind        string  `json:"kind"`
	ID          string  `json:"id"`
	Title       string  `json:"title,omitempty"`
	WhyRelevant string  `json:"why_relevant,omitempty"`
	Command     string  `json:"command,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
}

// DiagnosticAction is a safe-to-run remediation command suggested for a
// finding. SafeToRun=true means read-only / test / build only. Anything
// cluster-mutating MUST set RequiresAck=true and explain why in Reason.
type DiagnosticAction struct {
	Kind        string `json:"kind"` // inspect | test | rebuild | runtime_collect | grep | runbook | stop
	Command     string `json:"command,omitempty"`
	Reason      string `json:"reason"`
	SafeToRun   bool   `json:"safe_to_run"`
	RequiresAck bool   `json:"requires_ack,omitempty"`
}

// Falsifier records "what evidence would prove this diagnosis wrong". Forces
// the agent to think in falsifiable claims, not just match output. Phase 2
// emits a generic fallback; Phase 6 will ship per-failure_mode templates.
type Falsifier struct {
	Claim      string `json:"claim"`
	HowToCheck string `json:"how_to_check"`
	Command    string `json:"command,omitempty"`
}

// DecisionTrace is the per-finding "why did this fire, and what now?" record.
// One trace per matched invariant / failure_mode / forbidden_fix / raw_yaml
// fallback / runtime match.
//
// IMPORTANT: when no findings match, callers should pass empty slices (length
// 0), NOT nil — the trust envelope is the single source of safety verdicts
// under NO_MATCH, and a fabricated trace would compete with it.
type DecisionTrace struct {
	FindingID       string             `json:"finding_id"`
	FindingType     FindingType        `json:"finding_type"`
	Summary         string             `json:"summary,omitempty"`
	Confidence      Confidence         `json:"confidence"`
	ConfidenceScore float64            `json:"confidence_score,omitempty"`
	MatchedBy       []EvidenceRef      `json:"matched_by"`
	Owner           OwnerContext       `json:"owner"`
	Pivots          []ContextPivot     `json:"pivots"`
	NextActions     []DiagnosticAction `json:"next_actions"`
	Falsifiers      []Falsifier        `json:"falsifiers"`
	Warnings        []string           `json:"warnings,omitempty"`
}

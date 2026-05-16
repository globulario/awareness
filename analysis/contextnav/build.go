package contextnav

import (
	"context"
	"fmt"
	"sort"

	"github.com/globulario/awareness/graph"
)

// BuildInputs is the contextnav-local view of preflight state. Defined as a
// plain struct (no preflight import) so this package stays independent —
// preflight constructs Inputs from its Report and calls Build.
//
// Field semantics mirror the analogous preflight fields:
//   - Invariants/FailureModes/ForbiddenFixes: graph match ids
//   - RequiredTests: test obligations surfaced by the graph for the match set
//   - MatchedAliases: task-phrase alias hits (weaker signal than graph)
//   - RawKnowledge: graph-missed-but-raw-yaml-hit fallback matches
//   - Runtime: live-cluster evidence (failure_mode/invariant ids that
//     matched against runtime observations)
//   - Confidence: the Report's overall confidence; per-evidence confidence is
//     derived from this plus freshness
//   - GraphStale: whether the graph build is past its freshness window
//   - LiveOverlayStatus: "fresh" | "stale" | "absent" | "" — controls
//     runtime evidence freshness label and confidence cap
type BuildInputs struct {
	Invariants     []string
	FailureModes   []string
	ForbiddenFixes []string
	RequiredTests  []string
	MatchedAliases []string

	RawKnowledge []RawKnowledgeRef
	Runtime      RuntimeRef

	Confidence Confidence

	// GraphFreshnessKnown=false means no GraphFreshness section was captured —
	// the freshness label renders as "unknown". When true, GraphStale chooses
	// between "stale" and "fresh".
	GraphFreshnessKnown bool
	GraphStale          bool

	// LiveOverlayStatus is "" when the overlay section was absent. Non-empty
	// values mirror preflight: "fresh" | "stale" | "absent" | "failed" |
	// "partial". The freshness label collapses anything outside fresh/stale/
	// absent to "unknown".
	LiveOverlayStatus string

	// Graph + Ctx + Task + Files drive owner inference (Phase 3). When
	// Graph is nil OR Ctx is nil, owner inference is skipped and traces
	// carry an empty OwnerContext. This keeps Build callable from pure
	// unit tests that don't want to spin up a graph DB.
	Graph *graph.Graph
	Ctx   context.Context
	Task  string
	Files []string

	// Phase 5: cross-cutting evidence sources. Each non-raw-knowledge
	// trace gets a per-finding view of these so the agent doesn't have to
	// separately consult Report.Trust / Report.DidWeFix / Report.ExperienceHints.

	// TrustVerdict / TrustConfidence / TrustFreshness / TrustReason mirror
	// the relevant fields on the TrustEnvelope. Empty when no trust
	// envelope was computed.
	TrustVerdict    string
	TrustConfidence string
	TrustFreshness  string
	TrustReason     string

	// FixCases is the fix_ledger lookup result. Status reflects whether
	// the prior fix is complete, partial, or otherwise. First entry is
	// used as the primary attribution.
	FixCases []FixCaseRef

	// FixLedgerGaps lists ids/labels of remaining_gap entries the fix
	// ledger reported for this task. Used to distinguish "fully fixed
	// before" from "fix exists but gaps remain".
	FixLedgerGaps []string

	// Experiences is the ranked list of similar-past-attempt hints. Build
	// uses only the first entry today; later phases may surface more.
	Experiences []ExperienceRef

	// MetricWarnings is the live Prometheus warnings list as of the
	// preflight run. Empty when no live overlay was collected or the
	// overlay carried no warnings.
	MetricWarnings []string
}

// RawKnowledgeRef captures the minimum information needed to render a
// raw-yaml-fallback trace. Kept separate from preflight.RawKnowledgeMatch so
// contextnav has no preflight dependency.
type RawKnowledgeRef struct {
	Source       string
	Kind         string
	ID           string
	MatchedTerms []string
}

// RuntimeRef carries the runtime-overlay match ids. Empty slices when no
// runtime evidence is present (IncludeRuntime=false in preflight).
type RuntimeRef struct {
	MatchedFailureModes []string
	MatchedInvariants   []string
}

// rawKnowledgeFallbackHint is the generic falsifier used when no
// failure_mode-specific template exists yet. Tells the agent how to
// invalidate the match without prescribing the diagnosis. Phase 6 will swap
// in per-failure_mode templates.
const rawKnowledgeFallbackHint = "Re-run preflight after a graph rebuild and a live-snapshot collect; if the same finding does not reappear, the original match was a stale/fallback artifact."

// Build composes a per-finding navigation layer from data preflight already
// collected. Pure: no graph or storage I/O, no goroutines, deterministic.
// Returns an empty (non-nil) slice when no findings match — the trust
// envelope is the authority on NO_MATCH safety; a synthetic trace would
// compete with it.
func Build(in BuildInputs) []DecisionTrace {
	aliasSet := make(map[string]bool, len(in.MatchedAliases))
	for _, a := range in.MatchedAliases {
		aliasSet[a] = true
	}

	runtimeFMSet := map[string]bool{}
	for _, fm := range in.Runtime.MatchedFailureModes {
		runtimeFMSet[fm] = true
	}
	runtimeInvSet := map[string]bool{}
	for _, id := range in.Runtime.MatchedInvariants {
		runtimeInvSet[id] = true
	}

	// Track whether a raw-yaml id is already covered by a graph match so we
	// don't double-emit the same finding under two FindingTypes.
	covered := map[string]bool{}
	for _, id := range in.Invariants {
		covered["invariant:"+id] = true
	}
	for _, id := range in.FailureModes {
		covered["failure_mode:"+id] = true
	}
	for _, id := range in.ForbiddenFixes {
		covered["forbidden_fix:"+id] = true
	}

	traces := make([]DecisionTrace, 0,
		len(in.Invariants)+len(in.FailureModes)+len(in.ForbiddenFixes)+len(in.RawKnowledge))

	for _, fmID := range in.FailureModes {
		t := traceForFailureMode(&in, fmID, aliasSet, runtimeFMSet)
		attachCrossCuttingEvidence(&t, &in)
		t.NextActions = InferActions(&t, &in)
		traces = append(traces, t)
	}
	for _, invID := range in.Invariants {
		t := traceForInvariant(&in, invID, aliasSet, runtimeInvSet)
		attachCrossCuttingEvidence(&t, &in)
		t.NextActions = InferActions(&t, &in)
		traces = append(traces, t)
	}
	for _, ffID := range in.ForbiddenFixes {
		t := traceForForbiddenFix(&in, ffID, aliasSet)
		attachCrossCuttingEvidence(&t, &in)
		t.NextActions = InferActions(&t, &in)
		traces = append(traces, t)
	}
	for _, raw := range in.RawKnowledge {
		nsID := raw.Kind + ":" + raw.ID
		if covered[nsID] {
			continue
		}
		// Raw-knowledge traces intentionally skip cross-cutting evidence:
		// graph missed the finding, so layering trust/fix/experience
		// signals on top would compete with the trace's own fallback
		// warning. The agent reads raw_yaml as "treat as hint", and the
		// MatchedBy chain stays single-sourced to enforce that.
		t := traceForRawKnowledge(&in, raw)
		t.NextActions = InferActions(&t, &in)
		traces = append(traces, t)
	}

	sort.SliceStable(traces, func(i, j int) bool {
		if traces[i].FindingType != traces[j].FindingType {
			return findingTypeRank(traces[i].FindingType) < findingTypeRank(traces[j].FindingType)
		}
		return traces[i].FindingID < traces[j].FindingID
	})

	// Phase 3+4: owner inference + graph-walked pivots. Skipped when
	// Graph/Ctx aren't supplied so pure unit tests can call Build without
	// a DB. Raw-knowledge traces don't have a graph node to walk from
	// (the graph missed them by definition), so they fall through to the
	// file-hint enrichment for owner and skip pivot inference.
	if in.Graph != nil && in.Ctx != nil {
		pivotOpts := PivotOptions{
			IncludeRuntime: len(in.Runtime.MatchedFailureModes) > 0 ||
				len(in.Runtime.MatchedInvariants) > 0,
		}
		for i := range traces {
			traces[i].Owner = ownerForTrace(in.Ctx, in.Graph, &traces[i], in.Task, in.Files)
			if traces[i].Owner.Layer == LayerUnknown {
				traces[i].Warnings = append(traces[i].Warnings,
					"owner: layer inference returned unknown — no graph neighbor mapped to a layer, no task hint matched")
			}
			if anchor := pivotAnchorID(&traces[i]); anchor != "" {
				walked := InferPivots(in.Ctx, in.Graph, anchor, pivotOpts)
				traces[i].Pivots = mergeAndRankPivots(traces[i].Pivots, walked, pivotOpts.MaxResults)
			} else {
				traces[i].Pivots = rankAndCapPivots(traces[i].Pivots, 0)
			}
		}
	} else {
		// No graph: still rank the Phase 2 Report-derived pivots so the
		// JSON output ordering matches what the graph-enriched path
		// produces. Determinism > entropy.
		for i := range traces {
			traces[i].Pivots = rankAndCapPivots(traces[i].Pivots, 0)
		}
	}

	return traces
}

// attachCrossCuttingEvidence wires Phase 5 evidence sources (trust
// envelope, fix ledger, experience store, metric warnings) onto a trace.
// Each appender is a no-op when its input section is empty, so this is
// safe to call unconditionally for non-raw-knowledge traces.
func attachCrossCuttingEvidence(t *DecisionTrace, in *BuildInputs) {
	appendTrustEvidence(t, in)
	appendFixLedgerEvidence(t, in)
	appendExperienceEvidence(t, in)
	appendMetricsEvidence(t, in)
}

// pivotAnchorID returns the graph node id to walk from when generating
// graph-walked pivots for a trace. Raw-knowledge findings (graph misses by
// definition) return "" — pivot inference is skipped for them.
func pivotAnchorID(t *DecisionTrace) string {
	switch t.FindingType {
	case FindingFailureMode:
		return "failure_mode:" + t.FindingID
	case FindingInvariant:
		return "invariant:" + t.FindingID
	case FindingForbiddenFix:
		return "forbidden_fix:" + t.FindingID
	}
	return ""
}

// ownerForTrace dispatches to InferOwner with the correct prefixed node id
// for each finding type. Raw-knowledge findings have no graph anchor (the
// graph missed them), so we fall back to the file-hint-only path.
func ownerForTrace(ctx context.Context, g *graph.Graph, t *DecisionTrace, task string, files []string) OwnerContext {
	var nodeID string
	switch t.FindingType {
	case FindingFailureMode:
		nodeID = "failure_mode:" + t.FindingID
	case FindingInvariant:
		nodeID = "invariant:" + t.FindingID
	case FindingForbiddenFix:
		nodeID = "forbidden_fix:" + t.FindingID
	case FindingRawKnowledge, FindingRuntime, FindingExperience:
		// Either no anchor node (raw_knowledge) or anchor not modelled in
		// the graph yet (runtime/experience). Use the file-hint path.
		return enrichWithFileHint(OwnerContext{Layer: LayerUnknown}, files)
	default:
		return OwnerContext{Layer: LayerUnknown}
	}
	return InferOwner(ctx, g, nodeID, task, files)
}

func findingTypeRank(t FindingType) int {
	switch t {
	case FindingFailureMode:
		return 0
	case FindingInvariant:
		return 1
	case FindingForbiddenFix:
		return 2
	case FindingRawKnowledge:
		return 3
	case FindingRuntime:
		return 4
	case FindingExperience:
		return 5
	}
	return 99
}

func traceForFailureMode(in *BuildInputs, fmID string, aliasSet, runtimeFMSet map[string]bool) DecisionTrace {
	t := DecisionTrace{
		FindingID:   fmID,
		FindingType: FindingFailureMode,
		Confidence:  in.Confidence,
		MatchedBy:   []EvidenceRef{},
		Pivots:      []ContextPivot{},
		NextActions: []DiagnosticAction{},
		Falsifiers:  falsifiersOrGeneric("failure_mode", fmID, in),
	}

	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "graph",
		NodeID:     "failure_mode:" + fmID,
		Confidence: graphEvidenceConfidence(in),
		Freshness:  graphFreshnessLabel(in),
		Reason:     "matched against awareness graph failure_modes",
	})
	if aliasSet[fmID] {
		t.MatchedBy = append(t.MatchedBy, EvidenceRef{
			Source:     "alias",
			NodeID:     fmID,
			Confidence: 0.55,
			Reason:     "task phrase matched the failure_mode's context_aliases",
		})
	}
	if runtimeFMSet[fmID] {
		t.MatchedBy = append(t.MatchedBy, EvidenceRef{
			Source:     "runtime",
			NodeID:     "runtime:" + fmID,
			Confidence: runtimeEvidenceConfidence(in),
			Freshness:  runtimeFreshnessLabel(in),
			Reason:     "live runtime evidence indicated this failure_mode",
		})
	}

	for _, invID := range in.Invariants {
		t.Pivots = append(t.Pivots, ContextPivot{
			Kind:        "source_invariant",
			ID:          "invariant:" + invID,
			WhyRelevant: "co-matched invariant; failure_mode likely violates it",
			Confidence:  0.6,
		})
	}
	for _, tn := range in.RequiredTests {
		t.Pivots = append(t.Pivots, ContextPivot{
			Kind:        "required_test",
			ID:          tn,
			WhyRelevant: "test obligation surfaced by graph for this match set",
			Confidence:  0.8,
		})
	}
	for _, ffID := range in.ForbiddenFixes {
		t.Pivots = append(t.Pivots, ContextPivot{
			Kind:        "forbidden_fix",
			ID:          ffID,
			WhyRelevant: "do-not-do pattern paired with this match set",
			Confidence:  0.85,
		})
	}
	return t
}

func traceForInvariant(in *BuildInputs, invID string, aliasSet, runtimeInvSet map[string]bool) DecisionTrace {
	t := DecisionTrace{
		FindingID:   invID,
		FindingType: FindingInvariant,
		Confidence:  in.Confidence,
		MatchedBy:   []EvidenceRef{},
		Pivots:      []ContextPivot{},
		NextActions: []DiagnosticAction{},
		Falsifiers:  falsifiersOrGeneric("invariant", invID, in),
	}
	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "graph",
		NodeID:     "invariant:" + invID,
		Confidence: graphEvidenceConfidence(in),
		Freshness:  graphFreshnessLabel(in),
		Reason:     "matched against awareness graph invariants",
	})
	if aliasSet[invID] {
		t.MatchedBy = append(t.MatchedBy, EvidenceRef{
			Source: "alias", NodeID: invID, Confidence: 0.55,
			Reason: "task phrase matched the invariant's context_aliases",
		})
	}
	if runtimeInvSet[invID] {
		t.MatchedBy = append(t.MatchedBy, EvidenceRef{
			Source:     "runtime",
			NodeID:     "runtime:" + invID,
			Confidence: runtimeEvidenceConfidence(in),
			Freshness:  runtimeFreshnessLabel(in),
			Reason:     "live runtime evidence implicated this invariant",
		})
	}
	for _, ffID := range in.ForbiddenFixes {
		t.Pivots = append(t.Pivots, ContextPivot{
			Kind:        "forbidden_fix",
			ID:          ffID,
			WhyRelevant: "do-not-do pattern paired with this invariant",
			Confidence:  0.85,
		})
	}
	for _, tn := range in.RequiredTests {
		t.Pivots = append(t.Pivots, ContextPivot{
			Kind:        "required_test",
			ID:          tn,
			WhyRelevant: "test obligation surfaced by graph for this invariant",
			Confidence:  0.8,
		})
	}
	return t
}

func traceForForbiddenFix(in *BuildInputs, ffID string, aliasSet map[string]bool) DecisionTrace {
	t := DecisionTrace{
		FindingID:   ffID,
		FindingType: FindingForbiddenFix,
		Confidence:  in.Confidence,
		MatchedBy: []EvidenceRef{{
			Source:     "graph",
			NodeID:     "forbidden_fix:" + ffID,
			Confidence: graphEvidenceConfidence(in),
			Freshness:  graphFreshnessLabel(in),
			Reason:     "matched against awareness graph forbidden_fixes",
		}},
		Pivots:      []ContextPivot{},
		NextActions: []DiagnosticAction{},
		Falsifiers:  falsifiersOrGeneric("forbidden_fix", ffID, in),
	}
	if aliasSet[ffID] {
		t.MatchedBy = append(t.MatchedBy, EvidenceRef{
			Source: "alias", NodeID: ffID, Confidence: 0.55,
			Reason: "task phrase matched the forbidden_fix's context_aliases",
		})
	}
	return t
}

func traceForRawKnowledge(in *BuildInputs, raw RawKnowledgeRef) DecisionTrace {
	conf := in.Confidence
	if conf != ConfidenceUnknown {
		conf = ConfidenceLow
	}
	t := DecisionTrace{
		FindingID:   raw.ID,
		FindingType: FindingRawKnowledge,
		Summary:     fmt.Sprintf("%s match from %s (graph missed; YAML caught it)", raw.Kind, raw.Source),
		Confidence:  conf,
		MatchedBy: []EvidenceRef{{
			Source: "raw_yaml",
			NodeID: raw.Kind + ":" + raw.ID,
			// Per the Phase 5 confidence matrix: raw YAML fallback at
			// 0.65 — strong enough to surface, low enough that the
			// trace's Confidence=Low still gates downstream decisions.
			Confidence: 0.65,
			Reason:     fmt.Sprintf("matched %s in %s by terms: %v", raw.Kind, raw.Source, raw.MatchedTerms),
		}},
		Pivots:      []ContextPivot{},
		NextActions: []DiagnosticAction{},
		Falsifiers: []Falsifier{{
			Claim:      "this finding is real (not a stale YAML artifact)",
			HowToCheck: "rebuild the awareness graph and re-run preflight; if the same finding reappears under graph (not raw_yaml), it's load-bearing",
			Command:    "globular awareness build --clean",
		}},
		Warnings: []string{"fallback match — graph silence does not prove safety; treat as a hint, not proof"},
	}
	return t
}

// falsifiersOrGeneric returns the templated falsifiers for a finding's
// family (Phase 6), or a single-entry generic-falsifier slice when no
// template matches. Every DecisionTrace MUST carry at least one
// falsifier — the design-doc contract is that an agent reads findings
// as falsifiable claims, not as match output.
func falsifiersOrGeneric(kind, id string, in *BuildInputs) []Falsifier {
	if templated := falsifiersForFinding(kind, id); len(templated) > 0 {
		return templated
	}
	return []Falsifier{genericFalsifier(in)}
}

func genericFalsifier(in *BuildInputs) Falsifier {
	if in.GraphStale {
		return Falsifier{
			Claim:      "the graph path that produced this finding still exists after a fresh rebuild",
			HowToCheck: "rebuild the awareness graph and re-run preflight; the finding must reappear under the same FindingType",
			Command:    "globular awareness build --clean",
		}
	}
	return Falsifier{
		Claim:      "the graph path that produced this finding is intact",
		HowToCheck: rawKnowledgeFallbackHint,
	}
}

// graphEvidenceConfidence translates BuildInputs.Confidence into a 0..1 score
// for a graph EvidenceRef, with a stale-graph cap so high confidence cannot
// survive a stale graph in the per-evidence reading.
func graphEvidenceConfidence(in *BuildInputs) float64 {
	base := 0.85
	switch in.Confidence {
	case ConfidenceHigh:
		base = 0.9
	case ConfidenceMedium:
		base = 0.75
	case ConfidenceLow:
		base = 0.55
	case ConfidenceUnknown:
		base = 0.4
	}
	if in.GraphStale && base > 0.5 {
		base = 0.5
	}
	return base
}

func runtimeEvidenceConfidence(in *BuildInputs) float64 {
	switch runtimeFreshnessLabel(in) {
	case "fresh":
		return 0.9
	case "stale":
		return 0.5
	}
	return 0.4
}

func graphFreshnessLabel(in *BuildInputs) string {
	if !in.GraphFreshnessKnown {
		return "unknown"
	}
	if in.GraphStale {
		return "stale"
	}
	return "fresh"
}

func runtimeFreshnessLabel(in *BuildInputs) string {
	switch in.LiveOverlayStatus {
	case "fresh":
		return "fresh"
	case "stale":
		return "stale"
	case "absent":
		return "absent"
	}
	return "unknown"
}

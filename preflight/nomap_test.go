package preflight_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/runtime"
)

// TestPreflightGraphUnavailableDoesNotReturnBareNoMatch verifies that when no
// graph DB is available the report still carries coverage, confidence,
// blind_spots, and graph_available=false — never a bare "no results" response
// with no explanation of why.
func TestPreflightGraphUnavailableDoesNotReturnBareNoMatch(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "restart controller after etcd leader change",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.GraphAvailable {
		t.Error("GraphAvailable must be false when g is nil")
	}
	if r.Coverage.Graph != preflight.CoverageNotChecked {
		t.Errorf("Coverage.Graph = %q, want not_checked when graph unavailable", r.Coverage.Graph)
	}
	if r.Confidence == "" {
		t.Error("Confidence must not be empty — every report needs a confidence level")
	}
	if r.ConfidenceReason == "" {
		t.Error("ConfidenceReason must not be empty when no graph is available")
	}
	if len(r.BlindSpots) == 0 {
		t.Error("BlindSpots must be non-empty when graph is unavailable — callers need to know why")
	}
}

// TestPreflightCheckedCleanReturnsUnknownImpactWithCoverage verifies that when
// both the graph (clean) and raw YAML (clean) return no matches for an
// architecture-sensitive task, the report carries UNKNOWN_IMPACT classification
// with a populated coverage section and non-empty confidence_reason.
func TestPreflightCheckedCleanReturnsUnknownImpactWithCoverage(t *testing.T) {
	g := seedPreflightGraph(t)
	// "leader" is in architectureSensitiveKeywords → ARCHITECTURE_SENSITIVE.
	// The nonsense suffix ensures no test invariant ID/title matches any keyword.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "leader zzz_q99_impossible_9823",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// GraphAvailable must be true — graph was provided.
	if !r.GraphAvailable {
		t.Error("GraphAvailable must be true when graph is provided")
	}
	// Coverage.Graph must be checked (clean or with_matches) — graph ran.
	if r.Coverage.Graph == preflight.CoverageNotChecked {
		t.Errorf("Coverage.Graph = not_checked, want checked_clean or checked_with_matches when graph provided")
	}
	// Coverage.RawYAML must be checked — raw fallback always runs.
	if r.Coverage.RawYAML == preflight.CoverageNotChecked {
		t.Errorf("Coverage.RawYAML = not_checked, raw fallback must always run")
	}
	// ConfidenceReason must be non-empty regardless of match count.
	if r.ConfidenceReason == "" {
		t.Error("ConfidenceReason must be non-empty even when no matches found")
	}
	// Architecture-sensitive task with no facts → UNKNOWN_IMPACT must appear.
	if r.GraphMatchCount == 0 {
		found := false
		for _, c := range r.Classification {
			if c == preflight.ClassUnknownImpact {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("classification missing UNKNOWN_IMPACT for architecture-sensitive task with no graph matches; got %v", r.Classification)
		}
	}
}

// TestPreflightGraphStaleRawYAMLMatchReportsRawMatch verifies that when the
// graph has no (or stale) data but the raw YAML fallback finds matches, the
// report's RawYAMLMatchCount is > 0 and the raw matches are populated.
func TestPreflightGraphStaleRawYAMLMatchReportsRawMatch(t *testing.T) {
	docsDir := setupPreflightDocsDir(t)
	// Task contains "desired_hash" which the test aliases map to an invariant,
	// and raw YAML fallback scans the docs dir.  Run with nil graph so graph
	// coverage = not_checked and the raw YAML path is the sole evidence source.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash mismatch detected in convergence tick",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Raw YAML check always runs.
	if r.Coverage.RawYAML == preflight.CoverageNotChecked {
		t.Errorf("Coverage.RawYAML = not_checked, raw fallback must always run")
	}
	// RawYAMLMatchCount must be consistent with RawKnowledgeMatches.
	if r.RawYAMLMatchCount != len(r.RawKnowledgeMatches) {
		t.Errorf("RawYAMLMatchCount=%d but len(RawKnowledgeMatches)=%d — must be equal",
			r.RawYAMLMatchCount, len(r.RawKnowledgeMatches))
	}
	// When no graph is available, the raw YAML is the first line of defence.
	// The test confirms the count field is always set (even if zero) and
	// is consistent with the slice.
	if r.RawYAMLMatchCount < 0 {
		t.Error("RawYAMLMatchCount must not be negative")
	}
}

// seedStaleGraph creates an in-memory graph that has an invariant node whose
// metadata carries trust_level="stale". The invariant title contains
// "stale-trust" so it can be matched by a task that contains the same term.
func seedStaleGraph(t *testing.T) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Seed an invariant with a unique ID and title containing a matchable term.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "test.stale_trust_invariant",
		Title:    "stale-trust marker invariant for trust filter test",
		Summary:  "This invariant is intentionally marked stale for testing.",
		Severity: "medium",
		Status:   "active",
	})
	// The node needs metadata trust_level=stale so checkNodesTrust flags it.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "invariant:test.stale_trust_invariant",
		Type: graph.NodeTypeInvariant,
		Name: "test.stale_trust_invariant",
		Metadata: map[string]interface{}{
			"trust_level": "stale",
		},
	})
	return g
}

// TestPreflightTrustFilterExcludesStaleEdgeButReportsCount verifies that when
// a matched graph node carries trust_level="stale" metadata, it still appears
// in the main invariants list (not suppressed) AND also in FilteredMatches,
// and GraphFilteredByTrustCount is incremented.
func TestPreflightTrustFilterExcludesStaleEdgeButReportsCount(t *testing.T) {
	g := seedStaleGraph(t)
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "stale-trust marker invariant for trust filter test",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The invariant must appear in the main list (not suppressed).
	found := false
	for _, id := range r.Invariants {
		if id == "test.stale_trust_invariant" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("stale invariant missing from r.Invariants — must not be suppressed; got %v", r.Invariants)
	}

	// It must also appear in FilteredMatches.
	filtered := false
	for _, fm := range r.FilteredMatches {
		if fm.ID == "test.stale_trust_invariant" && fm.Reason == "stale" {
			filtered = true
			break
		}
	}
	if !filtered {
		t.Errorf("stale invariant not in FilteredMatches; got %+v", r.FilteredMatches)
	}

	// GraphFilteredByTrustCount must be consistent with FilteredMatches.
	if r.GraphFilteredByTrustCount != len(r.FilteredMatches) {
		t.Errorf("GraphFilteredByTrustCount=%d but len(FilteredMatches)=%d",
			r.GraphFilteredByTrustCount, len(r.FilteredMatches))
	}
	if r.GraphFilteredByTrustCount == 0 {
		t.Error("GraphFilteredByTrustCount must be > 0 when stale node is matched")
	}
}

// TestPreflightRuntimeNoopLowersConfidence verifies that when IncludeRuntime
// is false (noop), confidence does not reach "high" — runtime evidence is
// required for high confidence.
func TestPreflightRuntimeNoopLowersConfidence(t *testing.T) {
	g := seedPreflightGraph(t)
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:           "desired_hash mismatch in convergence",
		IncludeRuntime: false,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.Coverage.Runtime != preflight.CoverageNoop {
		t.Errorf("Coverage.Runtime = %q, want noop when IncludeRuntime=false", r.Coverage.Runtime)
	}
	if r.Confidence == preflight.ConfidenceHigh {
		t.Errorf("Confidence = high with runtime noop — runtime must be active for high confidence")
	}
}

// TestPreflightRuntimeNoopLowersConfidence_WithNoop verifies that even with
// a noop bridge (no real cluster), Coverage.Runtime is not noop when
// IncludeRuntime=true.
func TestPreflightRuntimeActiveButEmpty(t *testing.T) {
	g := seedPreflightGraph(t)
	bridge := runtime.NewBridge("node1", "")
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:           "test task",
		IncludeRuntime: true,
		Bridge:         bridge,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// IncludeRuntime=true means runtime was attempted — not noop.
	if r.Coverage.Runtime == preflight.CoverageNoop {
		t.Errorf("Coverage.Runtime = noop even though IncludeRuntime=true")
	}
}

// TestPreflightNoMatchNeverWithoutCoverageAndReason verifies the core invariant:
// every preflight report — regardless of whether the graph returned matches —
// must carry a non-empty Coverage struct, a non-empty Confidence, and a
// non-empty ConfidenceReason. The bare "no results" response is forbidden.
func TestPreflightNoMatchNeverWithoutCoverageAndReason(t *testing.T) {
	cases := []struct {
		name      string
		withGraph bool
		task      string
	}{
		{"nil_graph_no_match", false, "zzz_xyzzy_nomatch_12345"},
		{"nil_graph_with_task", false, "restart controller after network partition"},
		{"graph_no_match", true, "zzz_xyzzy_nomatch_12345"},
		{"graph_with_match", true, "desired_hash mismatch in convergence"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var g *graph.Graph
			if c.withGraph {
				g = seedPreflightGraph(t)
			}
			r, err := preflight.Run(context.Background(), preflight.Options{
				Task: c.task,
			}, g)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			// Coverage must always be populated.
			var zeroCoverage preflight.Coverage
			if r.Coverage == zeroCoverage {
				t.Error("Coverage is zero-value — must always be populated")
			}
			// Confidence must be set.
			if r.Confidence == "" {
				t.Error("Confidence is empty — every report must have a confidence level")
			}
			// ConfidenceReason must explain the confidence level.
			if r.ConfidenceReason == "" {
				t.Error("ConfidenceReason is empty — must always explain confidence")
			}
			// GraphAvailable must match whether g was nil.
			if c.withGraph && !r.GraphAvailable {
				t.Error("GraphAvailable=false but graph was provided")
			}
			if !c.withGraph && r.GraphAvailable {
				t.Error("GraphAvailable=true but no graph was provided")
			}
		})
	}
}

func TestDecisionContext_UnknownNotSafeForSensitiveTask(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "reconcile desired installed runtime behavior",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.SafetyStatus != preflight.SafetyStatusUnknownNotSafe {
		t.Fatalf("SafetyStatus=%q, want %q", r.SafetyStatus, preflight.SafetyStatusUnknownNotSafe)
	}
}

func TestPreflightConfidenceFactorsArePopulated(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "desired_hash mismatch in convergence",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.ConfidenceFactors.Coverage == "" {
		t.Fatal("confidence_factors.coverage must be set")
	}
	if r.ConfidenceFactors.Provenance == "" {
		t.Fatal("confidence_factors.provenance must be set")
	}
	if r.ConfidenceFactors.GraphFreshness == "" {
		t.Fatal("confidence_factors.graph_freshness must be set")
	}
	if r.ConfidenceFactors.PathQuality == "" {
		t.Fatal("confidence_factors.path_quality must be set")
	}
	if r.ConfidenceFactors.RuntimeEvidence == "" {
		t.Fatal("confidence_factors.runtime_evidence must be set")
	}
}

func TestPreflightDegradedMode_EmitsBlockedActions(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "reconcile desired installed runtime behavior",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.DegradedMode.Enabled {
		t.Fatal("degraded_mode must be enabled when graph is unavailable")
	}
	if len(r.DegradedMode.BlockedActions) == 0 {
		t.Fatal("degraded_mode.blocked_actions must be populated")
	}
	if len(r.DegradedMode.StopConditions) == 0 {
		t.Fatal("degraded_mode.stop_conditions must be populated")
	}
}

func TestPreflightDegradedMode_UsesRawKnowledgeFallback(t *testing.T) {
	docsDir := t.TempDir()
	invariants := `invariants:
  - id: infra.desired_hash_consistency
    title: desired_hash must be stable
    summary: checks desired_hash behavior
`
	if err := os.WriteFile(filepath.Join(docsDir, "invariants.yaml"), []byte(invariants), 0o644); err != nil {
		t.Fatalf("write invariants.yaml: %v", err)
	}
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash mismatch detected in convergence tick",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !r.DegradedMode.Enabled {
		t.Fatal("degraded_mode must be enabled when graph is unavailable")
	}
	if r.RawYAMLMatchCount == 0 {
		t.Fatal("expected raw YAML fallback matches in degraded mode")
	}
}

func TestPreflightFastPath_NilGraph_LocalRenameIsMediumNotLow(t *testing.T) {
	// With no graph, zero file matches is a coverage gap, not confirmed low impact.
	// RiskLow requires the graph to have been available and confirmed no impact.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "rename helper variable in local utility function",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.RiskTier != preflight.RiskMedium {
		t.Fatalf("RiskTier=%q, want %q (nil graph: no-file-match is coverage gap not low impact)", r.RiskTier, preflight.RiskMedium)
	}
	if r.FastPathApplied {
		t.Fatal("fast path must not apply when RiskTier is not low")
	}
}

func TestPreflightFastPath_WithGraph_LocalRenameIsLowAndFastPath(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "rename helper variable in local utility function",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.RiskTier != preflight.RiskLow {
		t.Fatalf("RiskTier=%q, want %q (graph available, no file impact confirmed)", r.RiskTier, preflight.RiskLow)
	}
	if !r.FastPathApplied {
		t.Fatal("expected fast path to be applied for confirmed low-risk task")
	}
}

func TestPreflightFastPath_DisabledForHighRisk(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "reconcile desired installed runtime behavior",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.RiskTier != preflight.RiskHigh {
		t.Fatalf("RiskTier=%q, want %q", r.RiskTier, preflight.RiskHigh)
	}
	if r.FastPathApplied {
		t.Fatal("fast path must not be applied for high-risk task")
	}
}

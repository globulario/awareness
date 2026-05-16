package contextnav

// pivots_test.go — Phase 4 acceptance tests for InferPivots and the
// graph-walked pivot merge. Seeds an in-memory graph with finding nodes
// connected to pivot-typed neighbors and verifies the ranking + dedup.

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestInferPivots_SourceInvariantSurfaced pins the Phase 4 acceptance
// criterion: "A failure mode trace includes at least one source_invariant
// when graph edges exist." The failure_mode --violates--> invariant edge
// is the load-bearing relationship; if pivot generation misses it, the
// agent loses the most actionable cross-link.
func TestInferPivots_SourceInvariantSurfaced(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.x", Type: graph.NodeTypeFailureMode, Name: "fm.x"})
	seed(t, g, graph.Node{ID: "invariant:inv.y", Type: graph.NodeTypeInvariant, Name: "inv.y"})
	link(t, g, "failure_mode:fm.x", "violates", "invariant:inv.y")

	pivots := InferPivots(ctx, g, "failure_mode:fm.x", PivotOptions{})
	if !pivotKindPresent(pivots, PivotKindSourceInvariant) {
		t.Errorf("missing source_invariant pivot; got %+v", pivots)
	}
}

// TestInferPivots_FixCaseStatusInWhyRelevant pins the criterion: "A
// finding connected to a fix case includes fix case status or remaining
// gap when available." The status metadata MUST flow into WhyRelevant so
// the agent sees "fix is partial" without a second hop.
func TestInferPivots_FixCaseStatusInWhyRelevant(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.fix", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{
		ID:       "fix_case:fc.partial",
		Type:     graph.NodeTypeFixCase,
		Name:     "fc.partial",
		Metadata: map[string]any{"status": "partial"},
	})
	link(t, g, "failure_mode:fm.fix", "addressed_by", "fix_case:fc.partial")

	pivots := InferPivots(ctx, g, "failure_mode:fm.fix", PivotOptions{})
	var fc *ContextPivot
	for i := range pivots {
		if pivots[i].Kind == PivotKindFixCase {
			fc = &pivots[i]
			break
		}
	}
	if fc == nil {
		t.Fatalf("missing fix_case pivot; got %+v", pivots)
	}
	if !contains(fc.WhyRelevant, "partial") {
		t.Errorf("WhyRelevant should mention fix status \"partial\"; got %q", fc.WhyRelevant)
	}
}

// TestInferPivots_RuntimeEvidenceGatedByIncludeRuntime pins the criterion:
// "A finding connected to runtime evidence includes freshness" — but ONLY
// when IncludeRuntime is true. With IncludeRuntime=false (the default for
// graph-only preflights), runtime nodes must not appear in pivots, so the
// agent doesn't read a stale runtime hint when no live snapshot was
// collected.
func TestInferPivots_RuntimeEvidenceGatedByIncludeRuntime(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.runtime", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "runtime_service_status:workflow@nuc", Type: graph.NodeTypeRuntimeServiceStatus, Name: "workflow@nuc"})
	link(t, g, "failure_mode:fm.runtime", "observed_in", "runtime_service_status:workflow@nuc")

	withRuntime := InferPivots(ctx, g, "failure_mode:fm.runtime", PivotOptions{IncludeRuntime: true})
	if !pivotKindPresent(withRuntime, PivotKindRuntimeEvidence) {
		t.Errorf("runtime pivot missing with IncludeRuntime=true; got %+v", withRuntime)
	}

	withoutRuntime := InferPivots(ctx, g, "failure_mode:fm.runtime", PivotOptions{IncludeRuntime: false})
	if pivotKindPresent(withoutRuntime, PivotKindRuntimeEvidence) {
		t.Errorf("runtime pivot leaked with IncludeRuntime=false; got %+v", withoutRuntime)
	}
}

// TestInferPivots_DeterministicCappedOrdering pins the cap + sort
// contract: pivots are sorted by usefulness rank then ID, and capped to
// MaxResults. Output must be byte-stable across runs so JSON diffs in
// audit trails don't drift on identical input.
func TestInferPivots_DeterministicCappedOrdering(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.many", Type: graph.NodeTypeFailureMode})
	// One of each pivot kind from a curated set, so the rank order is
	// observable: required_test then forbidden_fix then source_invariant
	// then fix_case (incident omitted because the rank only matters when
	// kinds differ).
	seed(t, g, graph.Node{ID: "test:T1", Type: graph.NodeTypeTest, Name: "T1"})
	seed(t, g, graph.Node{ID: "forbidden_fix:ff.bad", Type: graph.NodeTypeForbiddenFix, Name: "ff.bad"})
	seed(t, g, graph.Node{ID: "invariant:inv.src", Type: graph.NodeTypeInvariant, Name: "inv.src"})
	seed(t, g, graph.Node{ID: "fix_case:fc.x", Type: graph.NodeTypeFixCase, Name: "fc.x"})
	link(t, g, "failure_mode:fm.many", "tested_by", "test:T1")
	link(t, g, "failure_mode:fm.many", "forbids", "forbidden_fix:ff.bad")
	link(t, g, "failure_mode:fm.many", "violates", "invariant:inv.src")
	link(t, g, "failure_mode:fm.many", "addressed_by", "fix_case:fc.x")

	pivots := InferPivots(ctx, g, "failure_mode:fm.many", PivotOptions{MaxResults: 3})
	if len(pivots) != 3 {
		t.Fatalf("expected 3 pivots after cap, got %d: %+v", len(pivots), pivots)
	}
	wantOrder := []string{
		PivotKindRequiredTest,
		PivotKindForbiddenFix,
		PivotKindSourceInvariant,
	}
	for i, want := range wantOrder {
		if pivots[i].Kind != want {
			t.Errorf("pivots[%d].Kind = %q, want %q", i, pivots[i].Kind, want)
		}
	}

	// Run twice — output must be byte-stable.
	pivots2 := InferPivots(ctx, g, "failure_mode:fm.many", PivotOptions{MaxResults: 3})
	if len(pivots2) != len(pivots) {
		t.Fatalf("len differs across runs: %d vs %d", len(pivots), len(pivots2))
	}
	for i := range pivots {
		if pivots[i].Kind != pivots2[i].Kind || pivots[i].ID != pivots2[i].ID {
			t.Errorf("pivots differ at [%d]: %+v vs %+v", i, pivots[i], pivots2[i])
		}
	}
}

// TestInferPivots_NilGraphReturnsNil is the safety contract: callers
// outside preflight (e.g. CLI listings, MCP tools) may invoke with no
// graph. Function must not panic; empty result is fine.
func TestInferPivots_NilGraphReturnsNil(t *testing.T) {
	got := InferPivots(context.Background(), nil, "failure_mode:x", PivotOptions{})
	if got != nil {
		t.Errorf("expected nil pivots when graph is nil, got %+v", got)
	}
}

// TestMergeAndRankPivots_DropsDuplicates pins the dedup contract: when a
// Report-derived pivot and a graph-walked pivot reference the same
// (Kind, ID), only ONE entry survives — the Report-derived one (because
// it appears first in the merge order).
func TestMergeAndRankPivots_DropsDuplicates(t *testing.T) {
	report := []ContextPivot{
		{Kind: PivotKindRequiredTest, ID: "T1", WhyRelevant: "from report"},
	}
	graphWalked := []ContextPivot{
		{Kind: PivotKindRequiredTest, ID: "T1", WhyRelevant: "from graph walk"},
		{Kind: PivotKindFixCase, ID: "fc.x", WhyRelevant: "from graph walk"},
	}
	merged := mergeAndRankPivots(report, graphWalked, 0)
	if len(merged) != 2 {
		t.Fatalf("expected 2 unique pivots, got %d: %+v", len(merged), merged)
	}
	// First pivot should be the T1 entry from report (WhyRelevant preserved).
	if merged[0].Kind != PivotKindRequiredTest || merged[0].WhyRelevant != "from report" {
		t.Errorf("expected report-derived T1 first; got %+v", merged[0])
	}
}

// TestBuild_PivotsIncludeGraphWalkedEntries is the end-to-end Phase 4
// test: Build, when invoked with Graph+Ctx, produces traces whose Pivots
// list includes graph-walked entries (incidents, fix_cases, etc.) that
// the Phase 2 Report-derived population wouldn't catch.
func TestBuild_PivotsIncludeGraphWalkedEntries(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.e2e", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "incident:INC-1", Type: graph.NodeTypeIncident, Name: "INC-1"})
	seed(t, g, graph.Node{
		ID:       "fix_case:fc.e2e",
		Type:     graph.NodeTypeFixCase,
		Name:     "fc.e2e",
		Metadata: map[string]any{"status": "complete"},
	})
	link(t, g, "failure_mode:fm.e2e", "occurred_during", "incident:INC-1")
	link(t, g, "failure_mode:fm.e2e", "addressed_by", "fix_case:fc.e2e")

	traces := Build(BuildInputs{
		FailureModes:        []string{"fm.e2e"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		Graph:               g,
		Ctx:                 ctx,
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if !pivotKindPresent(traces[0].Pivots, PivotKindIncident) {
		t.Errorf("missing incident pivot from graph walk; got %+v", traces[0].Pivots)
	}
	if !pivotKindPresent(traces[0].Pivots, PivotKindFixCase) {
		t.Errorf("missing fix_case pivot from graph walk; got %+v", traces[0].Pivots)
	}
}

// pivotKindPresent returns true if any pivot has the given kind.
func pivotKindPresent(pivots []ContextPivot, kind string) bool {
	for _, p := range pivots {
		if p.Kind == kind {
			return true
		}
	}
	return false
}

// contains is a tiny strings.Contains shim so tests stay terse.
func contains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && (haystack == needle ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	n, h := len(needle), len(haystack)
	for i := 0; i+n <= h; i++ {
		if haystack[i:i+n] == needle {
			return i
		}
	}
	return -1
}

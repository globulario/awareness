package enforce_test

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

// mustOpenMemory opens an in-memory graph or fails the test.
func mustOpenMemory(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// addInvariantNode adds an invariant record AND a corresponding invariant-type node.
func addInvariantNode(t *testing.T, ctx context.Context, g *graph.Graph, id string) {
	t.Helper()
	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID:    id,
		Title: id,
	}); err != nil {
		t.Fatalf("UpsertInvariant %s: %v", id, err)
	}
	if err := g.AddNode(ctx, graph.Node{
		ID:   "invariant:" + id,
		Type: graph.NodeTypeInvariant,
		Name: id,
	}); err != nil {
		t.Fatalf("AddNode invariant:%s: %v", id, err)
	}
}

// addEdge adds a directed edge to the graph.
func addEdge(t *testing.T, ctx context.Context, g *graph.Graph, src, kind, dst string) {
	t.Helper()
	if err := g.AddEdge(ctx, graph.Edge{Src: src, Kind: kind, Dst: dst}); err != nil {
		t.Fatalf("AddEdge %s-[%s]->%s: %v", src, kind, dst, err)
	}
}

// TestCoverageReport_InvariantImplementationCoverage verifies that implemented /
// missing counts are correct for a simple 3-invariant graph.
func TestCoverageReport_InvariantImplementationCoverage(t *testing.T) {
	ctx := context.Background()
	g := mustOpenMemory(t)

	// Add 3 invariants. inv1 and inv2 will have implements edges; inv3 will not.
	addInvariantNode(t, ctx, g, "inv1")
	addInvariantNode(t, ctx, g, "inv2")
	addInvariantNode(t, ctx, g, "inv3")

	// source_file node implementing inv1 (direct implements).
	if err := g.AddNode(ctx, graph.Node{ID: "src:file1", Type: graph.NodeTypeSourceFile, Name: "file1.go"}); err != nil {
		t.Fatalf("AddNode src:file1: %v", err)
	}
	addEdge(t, ctx, g, "src:file1", graph.EdgeImplements, "invariant:inv1")

	// source_file node implementing inv2 via partially_implements.
	if err := g.AddNode(ctx, graph.Node{ID: "src:file2", Type: graph.NodeTypeSourceFile, Name: "file2.go"}); err != nil {
		t.Fatalf("AddNode src:file2: %v", err)
	}
	addEdge(t, ctx, g, "src:file2", graph.EdgePartiallyImplements, "invariant:inv2")

	res := enforce.InvariantImplementationCoverage(ctx, g, enforce.InvariantImplCoverageOptions{
		MinPercent: 60.0,
		Enforced:   false, // don't emit finding — just measure
	})

	if res.Total != 3 {
		t.Errorf("expected Total=3, got %d", res.Total)
	}
	if res.Implemented != 2 {
		t.Errorf("expected Implemented=2, got %d", res.Implemented)
	}
	if len(res.MissingImplementation) != 1 {
		t.Errorf("expected 1 missing, got %v", res.MissingImplementation)
	} else if res.MissingImplementation[0] != "inv3" {
		t.Errorf("expected missing inv3, got %v", res.MissingImplementation)
	}
	wantPct := 2.0 / 3.0 * 100
	if res.ImplementedPercent < wantPct-0.01 || res.ImplementedPercent > wantPct+0.01 {
		t.Errorf("expected ImplementedPercent≈%.2f, got %.2f", wantPct, res.ImplementedPercent)
	}
}

// TestAudit_FailsWhenCoverageBelowThreshold verifies that setting the threshold
// above current coverage emits a CodeInvariantCoverageBelowThreshold finding.
func TestAudit_FailsWhenCoverageBelowThreshold(t *testing.T) {
	ctx := context.Background()
	g := mustOpenMemory(t)

	// Two invariants, none implemented → 0% coverage.
	addInvariantNode(t, ctx, g, "inv1")
	addInvariantNode(t, ctx, g, "inv2")

	res := enforce.InvariantImplementationCoverage(ctx, g, enforce.InvariantImplCoverageOptions{
		MinPercent: 100.0,
		Enforced:   true,
	})

	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantCoverageBelowThreshold {
			if f.Severity != enforce.SeverityError {
				t.Errorf("expected SeverityError for threshold breach, got %s", f.Severity)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding with code %s, got findings: %v",
			enforce.CodeInvariantCoverageBelowThreshold, res.Findings)
	}
}

// TestAudit_AllowsExplicitException verifies that an invariant in the exceptions
// list with no expiry (or future expiry) is excluded from MissingImplementation.
func TestAudit_AllowsExplicitException(t *testing.T) {
	ctx := context.Background()
	g := mustOpenMemory(t)

	// inv1: no implementation, but excused.
	// inv2: no implementation, not excused.
	addInvariantNode(t, ctx, g, "inv1")
	addInvariantNode(t, ctx, g, "inv2")

	res := enforce.InvariantImplementationCoverage(ctx, g, enforce.InvariantImplCoverageOptions{
		MinPercent: 0,
		Enforced:   false,
		Exceptions: []enforce.ImplCoverageException{
			{
				InvariantID: "inv1",
				Reason:      "documentation_only",
			},
		},
	})

	// inv1 should NOT appear in missing — it is excepted.
	for _, id := range res.MissingImplementation {
		if id == "inv1" {
			t.Error("inv1 should be excluded from MissingImplementation via exception")
		}
	}
	// inv2 has no implementation and no exception — it MUST appear.
	found := false
	for _, id := range res.MissingImplementation {
		if id == "inv2" {
			found = true
		}
	}
	if !found {
		t.Errorf("inv2 should appear in MissingImplementation, got %v", res.MissingImplementation)
	}
}

// TestAudit_FailsExpiredException verifies that an exception whose ExpiresAt is
// in the past no longer shields the invariant from MissingImplementation.
func TestAudit_FailsExpiredException(t *testing.T) {
	ctx := context.Background()
	g := mustOpenMemory(t)

	addInvariantNode(t, ctx, g, "inv1")

	// Exception that expired yesterday.
	yesterday := time.Now().Add(-25 * time.Hour).Format("2006-01-02")

	res := enforce.InvariantImplementationCoverage(ctx, g, enforce.InvariantImplCoverageOptions{
		MinPercent: 0,
		Enforced:   false,
		Exceptions: []enforce.ImplCoverageException{
			{
				InvariantID: "inv1",
				Reason:      "pending_design",
				ExpiresAt:   yesterday,
			},
		},
	})

	found := false
	for _, id := range res.MissingImplementation {
		if id == "inv1" {
			found = true
		}
	}
	if !found {
		t.Errorf("inv1 with expired exception should appear in MissingImplementation; got %v", res.MissingImplementation)
	}
}

// TestCoverageRatchet_PreventsRegression verifies that removing an implement
// edge from a previously-covered invariant triggers a threshold finding.
func TestCoverageRatchet_PreventsRegression(t *testing.T) {
	ctx := context.Background()
	g := mustOpenMemory(t)

	// Two invariants — both covered initially.
	addInvariantNode(t, ctx, g, "inv1")
	addInvariantNode(t, ctx, g, "inv2")

	if err := g.AddNode(ctx, graph.Node{ID: "src:file1", Type: graph.NodeTypeSourceFile, Name: "file1.go"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	addEdge(t, ctx, g, "src:file1", graph.EdgeImplements, "invariant:inv1")
	addEdge(t, ctx, g, "src:file1", graph.EdgeImplements, "invariant:inv2")

	// Baseline: 100% coverage at threshold 100 should pass.
	resBaseline := enforce.InvariantImplementationCoverage(ctx, g, enforce.InvariantImplCoverageOptions{
		MinPercent: 100.0,
		Enforced:   true,
	})
	for _, f := range resBaseline.Findings {
		if f.Code == enforce.CodeInvariantCoverageBelowThreshold {
			t.Errorf("baseline should pass 100%% threshold, got finding: %s", f.Message)
		}
	}

	// Now build a second graph with only 1 implement edge — simulating regression.
	g2 := mustOpenMemory(t)
	addInvariantNode(t, ctx, g2, "inv1")
	addInvariantNode(t, ctx, g2, "inv2")
	if err := g2.AddNode(ctx, graph.Node{ID: "src:file1", Type: graph.NodeTypeSourceFile, Name: "file1.go"}); err != nil {
		t.Fatalf("AddNode g2: %v", err)
	}
	// Only inv1 covered — inv2 lost its edge (regression).
	addEdge(t, ctx, g2, "src:file1", graph.EdgeImplements, "invariant:inv1")

	resRegressed := enforce.InvariantImplementationCoverage(ctx, g2, enforce.InvariantImplCoverageOptions{
		MinPercent: 100.0,
		Enforced:   true,
	})
	found := false
	for _, f := range resRegressed.Findings {
		if f.Code == enforce.CodeInvariantCoverageBelowThreshold {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s finding after removing implement edge, got %v",
			enforce.CodeInvariantCoverageBelowThreshold, resRegressed.Findings)
	}
}

package analysis_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/graph"
)

// buildCycleGraph creates a test graph with service dependency edges.
// Required cycle: node-agent → repository → minio → node-agent (phase=recovery)
// Optional cycle: svc-a → svc-b → svc-a (phase=normal, required=false)
func buildCycleGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := openGraph(t)
	ctx := context.Background()

	services := []struct{ id, name string }{
		{"service:node-agent", "node-agent"},
		{"service:repository", "repository"},
		{"service:minio", "minio"},
		{"service:svc-a", "svc-a"},
		{"service:svc-b", "svc-b"},
	}
	for _, s := range services {
		_ = g.AddNode(ctx, graph.Node{ID: s.id, Type: graph.NodeTypeGlobularService, Name: s.name})
	}

	// Required cycle during recovery: node-agent → repository → minio → node-agent
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:node-agent", Kind: graph.EdgeDependsOn,
		Dst: "service:repository", Phase: "recovery", Required: true,
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:repository", Kind: graph.EdgeDependsOn,
		Dst: "service:minio", Phase: "recovery", Required: true,
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:minio", Kind: graph.EdgeDependsOn,
		Dst: "service:node-agent", Phase: "recovery", Required: true,
	})

	// Optional cycle: svc-a → svc-b → svc-a (required=false)
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:svc-a", Kind: graph.EdgeDependsOn,
		Dst: "service:svc-b", Phase: "normal", Required: false,
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:svc-b", Kind: graph.EdgeDependsOn,
		Dst: "service:svc-a", Phase: "normal", Required: false,
	})

	return g
}

// Test 7: cycle detector finds required phase-specific cycles.
func TestFindCyclesRequiredCycleInRecovery(t *testing.T) {
	g := buildCycleGraph(t)
	ctx := context.Background()

	cycles, err := analysis.FindCycles(ctx, g, "recovery")
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}

	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle in recovery phase, got none")
	}

	var dangerous bool
	for _, c := range cycles {
		if c.Classification == analysis.CycleDangerous {
			dangerous = true
			t.Logf("DANGEROUS cycle: %v (phase=%s)", c.Path, c.Phase)
		}
	}
	if !dangerous {
		t.Error("expected at least one DANGEROUS cycle in recovery phase")
	}
}

// Test 8: optional dependency cycles are not classified as dangerous.
func TestFindCyclesOptionalCycleIsSafe(t *testing.T) {
	g := buildCycleGraph(t)
	ctx := context.Background()

	cycles, err := analysis.FindCycles(ctx, g, "normal")
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}

	if len(cycles) == 0 {
		t.Fatal("expected the optional svc-a ↔ svc-b cycle, got none")
	}

	for _, c := range cycles {
		if c.Classification == analysis.CycleDangerous {
			t.Errorf("optional cycle classified as DANGEROUS: %v", c.Path)
		}
	}

	// At least one should be SAFE.
	var safe bool
	for _, c := range cycles {
		if c.Classification == analysis.CycleSafe {
			safe = true
		}
	}
	if !safe {
		t.Error("expected at least one SAFE cycle for optional edges")
	}
}

// Test: phase filter — recovery cycles not returned when filtering by normal.
func TestFindCyclesPhaseFilter(t *testing.T) {
	g := buildCycleGraph(t)
	ctx := context.Background()

	cycles, err := analysis.FindCycles(ctx, g, "normal")
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}

	for _, c := range cycles {
		if c.Phase != "normal" {
			t.Errorf("expected only normal-phase cycles, got phase=%q in %v", c.Phase, c.Path)
		}
	}
}

// Test: no phase filter returns cycles from all phases.
func TestFindCyclesNoPhasFilter(t *testing.T) {
	g := buildCycleGraph(t)
	ctx := context.Background()

	cycles, err := analysis.FindCycles(ctx, g, "")
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}

	// Should have both the recovery cycle and the normal optional cycle.
	if len(cycles) < 2 {
		t.Errorf("expected >=2 cycles without phase filter, got %d", len(cycles))
	}
}

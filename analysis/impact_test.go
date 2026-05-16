package analysis_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/graph"
)

func openGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// Test 5: ImpactByFile returns related invariants.
func TestImpactByFileReturnsRelatedInvariants(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	// Create source_file node.
	fileID := "source_file:golang/node_agent/node_agent_server/installed_services.go"
	filePath := "golang/node_agent/node_agent_server/installed_services.go"
	_ = g.AddNode(ctx, graph.Node{
		ID:   fileID,
		Type: graph.NodeTypeSourceFile,
		Name: "installed_services.go",
		Path: filePath,
	})

	// Create symbol node defined in this file.
	symID := "symbol:commitInstallResult"
	_ = g.AddNode(ctx, graph.Node{
		ID:   symID,
		Type: graph.NodeTypeSymbol,
		Name: "commitInstallResult",
		Path: filePath,
	})
	_ = g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeDefines, Dst: symID})

	// Create invariant that protects this file.
	invID := "invariant:install.result.atomic_commit"
	_ = g.AddNode(ctx, graph.Node{
		ID:      invID,
		Type:    graph.NodeTypeInvariant,
		Name:    "install.result.atomic_commit",
		Summary: "Commit must be atomic",
	})
	// Invariant protects the file.
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeProtects, Dst: fileID})
	// Invariant protects the symbol.
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeProtects, Dst: symID})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "install.result.atomic_commit",
		Title:    "Atomic commit",
		Severity: "critical",
	})

	// Also create a test linked from the invariant.
	testID := "test:TestLeaderFailoverDuringResultCommit"
	_ = g.AddNode(ctx, graph.Node{ID: testID, Type: graph.NodeTypeTest, Name: "TestLeaderFailoverDuringResultCommit"})
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeTestedBy, Dst: testID})

	// Now run impact analysis on the file.
	// The file is protected by the invariant (incoming edge from invariant to file),
	// but ImpactByFile traverses OUTGOING edges from the file. We need to traverse
	// from file → symbol → invariant via the protects edges going the other direction.
	//
	// Since protects is inv→file (incoming to file), we also add an enforces edge
	// from symbol to invariant so the traversal can reach it.
	_ = g.AddEdge(ctx, graph.Edge{Src: symID, Kind: graph.EdgeEnforces, Dst: invID})

	result, err := analysis.ImpactByFile(ctx, g, filePath)
	if err != nil {
		t.Fatalf("ImpactByFile: %v", err)
	}

	// The file node should be found.
	if result.SourceFile == nil {
		t.Error("SourceFile nil — file node not found at given path")
	}

	// Invariant should be reachable via symbol→enforces→invariant.
	var foundInvariant bool
	for _, n := range result.Invariants {
		if n.Name == "install.result.atomic_commit" {
			foundInvariant = true
			break
		}
	}
	if !foundInvariant {
		t.Error("invariant install.result.atomic_commit not found in impact result")
	}
}

// Additional: ImpactByFile on unknown file returns empty result without error.
func TestImpactByFileUnknownFile(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	result, err := analysis.ImpactByFile(ctx, g, "golang/does/not/exist.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SourceFile != nil {
		t.Error("expected nil SourceFile for unknown path")
	}
	if len(result.Invariants) != 0 {
		t.Errorf("expected no invariants, got %d", len(result.Invariants))
	}
}

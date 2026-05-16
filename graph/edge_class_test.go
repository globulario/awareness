package graph_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

func TestEdgeClass_DecisionEdgeHighWeight(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "a", Type: "invariant", Name: "a"})
	_ = g.AddNode(ctx, graph.Node{ID: "b", Type: "failure_mode", Name: "b"})

	// EdgeBlocks is a decision-class edge.
	_ = g.AddEdge(ctx, graph.Edge{Src: "a", Kind: graph.EdgeBlocks, Dst: "b"})

	edges, err := g.EdgesByClass(ctx, graph.EdgeClassDecision)
	if err != nil {
		t.Fatalf("EdgesByClass: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "a" && e.Kind == graph.EdgeBlocks && e.Dst == "b" {
			found = true
		}
	}
	if !found {
		t.Error("expected blocks edge in decision class")
	}
}

func TestEdgeClass_InformationEdgeLowWeight(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "x", Type: "source_file", Name: "x"})
	_ = g.AddNode(ctx, graph.Node{ID: "y", Type: "source_file", Name: "y"})

	// EdgeMentionedIn is an information-class edge.
	_ = g.AddEdge(ctx, graph.Edge{Src: "x", Kind: graph.EdgeMentionedIn, Dst: "y"})

	// Should appear in information class, not decision class.
	decisionEdges, _ := g.EdgesByClass(ctx, graph.EdgeClassDecision)
	for _, e := range decisionEdges {
		if e.Src == "x" && e.Dst == "y" {
			t.Error("information edge should not appear in decision class")
		}
	}

	infoEdges, err := g.EdgesByClass(ctx, graph.EdgeClassInformation)
	if err != nil {
		t.Fatalf("EdgesByClass information: %v", err)
	}
	found := false
	for _, e := range infoEdges {
		if e.Src == "x" && e.Dst == "y" {
			found = true
		}
	}
	if !found {
		t.Error("expected mentioned_in edge in information class")
	}
}

func TestGraphPath_ClassFilteredDecisionOnly(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	// Chain: root →[blocks]→ mid →[requires]→ leaf
	// Plus: root →[mentions]→ noise (information class)
	_ = g.AddNode(ctx, graph.Node{ID: "root", Type: "invariant", Name: "root"})
	_ = g.AddNode(ctx, graph.Node{ID: "mid", Type: "failure_mode", Name: "mid"})
	_ = g.AddNode(ctx, graph.Node{ID: "leaf", Type: "test", Name: "leaf"})
	_ = g.AddNode(ctx, graph.Node{ID: "noise", Type: "source_file", Name: "noise"})

	_ = g.AddEdge(ctx, graph.Edge{Src: "root", Kind: graph.EdgeBlocks, Dst: "mid"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "mid", Kind: graph.EdgeRequires, Dst: "leaf"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "root", Kind: graph.EdgeMentionedIn, Dst: "noise"})

	result, err := g.TraverseDecision(ctx, "root", 4)
	if err != nil {
		t.Fatalf("TraverseDecision: %v", err)
	}

	nodeIDs := map[string]bool{}
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}

	if !nodeIDs["mid"] {
		t.Error("expected mid node via decision path")
	}
	if !nodeIDs["leaf"] {
		t.Error("expected leaf node via decision path")
	}
	if nodeIDs["noise"] {
		t.Error("noise node should not appear in decision-only traversal")
	}
}

func TestGraphNearest_WeightRankedResults(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "svc", Type: "globular_service", Name: "svc"})
	_ = g.AddNode(ctx, graph.Node{ID: "inv1", Type: "invariant", Name: "inv1"})
	_ = g.AddNode(ctx, graph.Node{ID: "doc1", Type: "documentation_section", Name: "doc1"})

	// Decision edge to inv1, info edge to doc1.
	_ = g.AddEdge(ctx, graph.Edge{Src: "svc", Kind: graph.EdgeEnforces, Dst: "inv1"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "svc", Kind: graph.EdgeMentionedIn, Dst: "doc1"})

	// Decision traversal should reach inv1 but not doc1.
	result, err := g.TraverseDecision(ctx, "svc", 2)
	if err != nil {
		t.Fatalf("TraverseDecision: %v", err)
	}

	nodeIDs := map[string]bool{}
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["inv1"] {
		t.Error("expected inv1 via decision edge (enforces)")
	}
	if nodeIDs["doc1"] {
		t.Error("doc1 should not appear via decision traversal (info edge)")
	}
}

func TestDecisionContext_EdgeClassField(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "p", Type: "package", Name: "p"})
	_ = g.AddNode(ctx, graph.Node{ID: "q", Type: "invariant", Name: "q"})

	// Explicitly set class=decision.
	_ = g.AddEdge(ctx, graph.Edge{
		Src:   "p",
		Kind:  graph.EdgeEnforces,
		Dst:   "q",
		Class: graph.EdgeClassDecision,
	})

	edges, err := g.EdgesByClass(ctx, graph.EdgeClassDecision)
	if err != nil {
		t.Fatalf("EdgesByClass: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "p" && e.Dst == "q" {
			found = true
		}
	}
	if !found {
		t.Error("explicitly-classed decision edge not found in decision class")
	}
}

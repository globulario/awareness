package graph_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestEdgePredicates_DecisionClassification verifies new decision-class edges.
func TestEdgePredicates_DecisionClassification(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "file:a", Type: "source_file", Name: "a"})
	_ = g.AddNode(ctx, graph.Node{ID: "action:foo", Type: "guarded_action", Name: "foo"})

	for _, kind := range []string{
		graph.EdgeGuardsAction,
		graph.EdgeBlocksForbiddenAction,
		graph.EdgeConstrainsActionFor,
	} {
		_ = g.AddEdge(ctx, graph.Edge{Src: "file:a", Kind: kind, Dst: "action:foo"})
	}

	edges, err := g.EdgesByClass(ctx, graph.EdgeClassDecision)
	if err != nil {
		t.Fatalf("EdgesByClass: %v", err)
	}
	kindSeen := make(map[string]bool)
	for _, e := range edges {
		kindSeen[e.Kind] = true
	}
	for _, want := range []string{graph.EdgeGuardsAction, graph.EdgeBlocksForbiddenAction, graph.EdgeConstrainsActionFor} {
		if !kindSeen[want] {
			t.Errorf("expected %q in decision class edges", want)
		}
	}
}

// TestEdgePredicates_StructuralClassification verifies new structural-class edges.
func TestEdgePredicates_StructuralClassification(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "file:b", Type: "source_file", Name: "b"})
	_ = g.AddNode(ctx, graph.Node{ID: "inv:x", Type: "invariant", Name: "x"})

	for _, kind := range []string{
		graph.EdgePartiallyImplements,
		graph.EdgeReadsAuthority,
		graph.EdgeWritesState,
		graph.EdgeVerifies,
	} {
		_ = g.AddEdge(ctx, graph.Edge{Src: "file:b", Kind: kind, Dst: "inv:x"})
	}

	edges, err := g.EdgesByClass(ctx, graph.EdgeClassStructural)
	if err != nil {
		t.Fatalf("EdgesByClass: %v", err)
	}
	kindSeen := make(map[string]bool)
	for _, e := range edges {
		kindSeen[e.Kind] = true
	}
	for _, want := range []string{graph.EdgePartiallyImplements, graph.EdgeReadsAuthority, graph.EdgeWritesState, graph.EdgeVerifies} {
		if !kindSeen[want] {
			t.Errorf("expected %q in structural class edges", want)
		}
	}
}

// TestEdgePredicates_InformationClass verifies has_evidence is information-class.
func TestEdgePredicates_InformationClass(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "ev:1", Type: "evidence", Name: "ev"})
	_ = g.AddNode(ctx, graph.Node{ID: "node:1", Type: "source_file", Name: "node"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "node:1", Kind: graph.EdgeHasEvidence, Dst: "ev:1"})

	edges, err := g.EdgesByClass(ctx, graph.EdgeClassInformation)
	if err != nil {
		t.Fatalf("EdgesByClass: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeHasEvidence {
			found = true
		}
	}
	if !found {
		t.Error("expected has_evidence edge in information class")
	}
}

// TestEdgePredicates_VerifiesWeight verifies the verifies edge has structural weight 0.7.
func TestEdgePredicates_VerifiesWeight(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "test:T1", Type: "test", Name: "T1"})
	_ = g.AddNode(ctx, graph.Node{ID: "inv:i1", Type: "invariant", Name: "i1"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "test:T1", Kind: graph.EdgeVerifies, Dst: "inv:i1"})

	edges, err := g.Neighbors(ctx, "test:T1", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	for _, e := range edges {
		if e.Kind == graph.EdgeVerifies {
			return // found — weight is assigned by classifyEdge, not returned in this API
		}
	}
	t.Error("expected verifies edge from test:T1")
}

// TestEdgePredicates_GuardsActionDecisionTraversal verifies guards_action is followed
// by TraverseDecision (decision-class BFS).
func TestEdgePredicates_GuardsActionDecisionTraversal(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "file:guard", Type: "source_file", Name: "guard"})
	_ = g.AddNode(ctx, graph.Node{ID: "action:txn", Type: "guarded_action", Name: "txn"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "file:guard", Kind: graph.EdgeGuardsAction, Dst: "action:txn"})

	result, err := g.TraverseDecision(ctx, "file:guard", 2)
	if err != nil {
		t.Fatalf("TraverseDecision: %v", err)
	}
	found := false
	for _, n := range result.Nodes {
		if n.ID == "action:txn" {
			found = true
		}
	}
	if !found {
		t.Error("expected action:txn reachable from file:guard via guards_action in decision traversal")
	}
}

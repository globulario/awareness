package graph_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestAddEdgeWithProvenance_RoundTrip pins the consolidated edge-provenance
// contract surfaced in docs/awareness/composed_path_failures.md (edge
// provenance home). The edges.provenance_json column is the single canonical
// home. Writes via AddEdgeWithProvenance must populate the column;
// AllEdges/Neighbors must read it back into Edge.Provenance.
//
// If this test fails, provenance has split into two homes again (column +
// metadata mirror) and the integrity check will be reading the wrong one.
func TestAddEdgeWithProvenance_RoundTrip(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	if err := g.AddNode(ctx, graph.Node{ID: "a", Type: graph.NodeTypeSymbol, Name: "a"}); err != nil {
		t.Fatalf("AddNode a: %v", err)
	}
	if err := g.AddNode(ctx, graph.Node{ID: "b", Type: graph.NodeTypeSymbol, Name: "b"}); err != nil {
		t.Fatalf("AddNode b: %v", err)
	}

	pe := graph.ProvenanceEdge{
		Edge: graph.Edge{
			Src:        "a",
			Kind:       graph.EdgeImplements,
			Dst:        "b",
			Confidence: 0.95,
		},
		SourceType:        "manual_yaml",
		SourceFile:        "docs/awareness/test.yaml",
		SourceCommit:      "deadbeef",
		CreatedBy:         "test-extractor",
		LastVerifiedAt:    1700000000,
		LastVerifiedBy:    "ci-check",
		VerificationLevel: "verified",
		StalePolicy:       []string{"source_file_modified"},
	}
	if err := g.AddEdgeWithProvenance(ctx, pe); err != nil {
		t.Fatalf("AddEdgeWithProvenance: %v", err)
	}

	edges, err := g.AllEdges(ctx)
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("AllEdges = %d, want 1", len(edges))
	}
	got := edges[0]
	if len(got.Provenance) == 0 {
		t.Fatalf("Edge.Provenance empty — column not populated or scanEdges not reading it")
	}
	if got.Provenance["source_type"] != "manual_yaml" {
		t.Errorf("Provenance.source_type = %v, want manual_yaml", got.Provenance["source_type"])
	}
	if got.Provenance["created_by"] != "test-extractor" {
		t.Errorf("Provenance.created_by = %v, want test-extractor", got.Provenance["created_by"])
	}
	if got.Provenance["verification_level"] != "verified" {
		t.Errorf("Provenance.verification_level = %v, want verified", got.Provenance["verification_level"])
	}

	// The metadata mirror is gone: caller-supplied Metadata must be empty/nil
	// here, NOT contain a provenance_json key. Pinning the asymmetry.
	if _, ok := got.Metadata["provenance_json"]; ok {
		t.Errorf("metadata['provenance_json'] = %v; provenance must NOT be mirrored to metadata anymore",
			got.Metadata["provenance_json"])
	}
}

// TestAddEdge_NoProvenance_DoesNotClobber ensures a plain AddEdge call
// (without Provenance) does NOT wipe an earlier AddEdgeWithProvenance on the
// same (src, kind, dst, phase). This is the upsert-vs-stub asymmetry that
// burned us with nodes; pinning it here for edges.
func TestAddEdge_NoProvenance_DoesNotClobber(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	if err := g.AddNode(ctx, graph.Node{ID: "a", Type: graph.NodeTypeSymbol, Name: "a"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := g.AddNode(ctx, graph.Node{ID: "b", Type: graph.NodeTypeSymbol, Name: "b"}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Step 1: write with provenance.
	if err := g.AddEdgeWithProvenance(ctx, graph.ProvenanceEdge{
		Edge:       graph.Edge{Src: "a", Kind: graph.EdgeImplements, Dst: "b"},
		SourceType: "first",
		CreatedBy:  "first-pass",
	}); err != nil {
		t.Fatalf("AddEdgeWithProvenance: %v", err)
	}
	// Step 2: a different extractor touches the same edge with no provenance.
	if err := g.AddEdge(ctx, graph.Edge{Src: "a", Kind: graph.EdgeImplements, Dst: "b"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	edges, err := g.AllEdges(ctx)
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Provenance["source_type"] != "first" {
		t.Errorf("Provenance was clobbered by no-provenance AddEdge: source_type = %v, want first",
			edges[0].Provenance["source_type"])
	}
}

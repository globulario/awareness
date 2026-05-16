package graph_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestEnsureNode_DoesNotClobberExistingMetadata pins the contract that
// surfaced from the 2026-05-10 lifecycle-metadata-loss incident
// (docs/awareness/composed_path_failures.md). EnsureNode must be a no-op
// when the node already exists — including its metadata. If this test
// ever fails, the canonical loaders' lifecycle hints (deprecated,
// intentional_gap) can be silently wiped by stub-creation in another
// extractor, and the trust verdict will lie.
func TestEnsureNode_DoesNotClobberExistingMetadata(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	id := "failure_mode:test_lifecycle_preserved"

	// Canonical loader writes full metadata.
	if err := g.AddNode(ctx, graph.Node{
		ID:      id,
		Type:    graph.NodeTypeFailureMode,
		Name:    "test_lifecycle_preserved",
		Summary: "canonical summary",
		Metadata: map[string]any{
			"intentional_gap": true,
			"severity":        "high",
		},
	}); err != nil {
		t.Fatalf("AddNode (canonical): %v", err)
	}

	// Stub-creating extractor reaches the same id with no metadata.
	if err := g.EnsureNode(ctx, graph.Node{
		ID:      id,
		Type:    graph.NodeTypeFailureMode,
		Name:    "test_lifecycle_preserved",
		Summary: "(stub — populated by failure_mode loader)",
	}); err != nil {
		t.Fatalf("EnsureNode: %v", err)
	}

	got, err := g.FindNode(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("FindNode: %v node=%v", err, got)
	}
	if got.Summary != "canonical summary" {
		t.Errorf("Summary clobbered: got %q want %q", got.Summary, "canonical summary")
	}
	if ig, _ := got.Metadata["intentional_gap"].(bool); !ig {
		t.Errorf("intentional_gap metadata lost after EnsureNode: metadata=%+v", got.Metadata)
	}
	if sev, _ := got.Metadata["severity"].(string); sev != "high" {
		t.Errorf("severity metadata clobbered: got %q want %q (metadata=%+v)",
			sev, "high", got.Metadata)
	}
}

// TestEnsureNode_CreatesWhenAbsent verifies the basic INSERT case.
func TestEnsureNode_CreatesWhenAbsent(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	id := "failure_mode:test_ensure_creates"
	if err := g.EnsureNode(ctx, graph.Node{
		ID:   id,
		Type: graph.NodeTypeFailureMode,
		Name: "test_ensure_creates",
		Metadata: map[string]any{
			"stub": true,
		},
	}); err != nil {
		t.Fatalf("EnsureNode: %v", err)
	}
	got, err := g.FindNode(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("FindNode after EnsureNode: %v node=%v", err, got)
	}
	if v, _ := got.Metadata["stub"].(bool); !v {
		t.Errorf("expected stub=true on freshly-ensured node, got metadata=%+v", got.Metadata)
	}
}

// TestAddNode_StillClobbers_UseEnsureNodeForStubs is a regression guard
// against accidental "softening" of AddNode. The asymmetry between
// AddNode (clobbers) and EnsureNode (preserves) is the contract; both
// behaviors are load-bearing for different callers.
func TestAddNode_StillClobbers_UseEnsureNodeForStubs(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	id := "failure_mode:test_addnode_clobbers"
	if err := g.AddNode(ctx, graph.Node{
		ID:   id,
		Type: graph.NodeTypeFailureMode,
		Name: "v1",
		Metadata: map[string]any{
			"intentional_gap": true,
		},
	}); err != nil {
		t.Fatalf("AddNode v1: %v", err)
	}
	// AddNode again with empty metadata — must clobber. This documents the
	// intended behavior; callers that don't want clobbering must use
	// EnsureNode.
	if err := g.AddNode(ctx, graph.Node{
		ID:   id,
		Type: graph.NodeTypeFailureMode,
		Name: "v2",
	}); err != nil {
		t.Fatalf("AddNode v2: %v", err)
	}
	got, err := g.FindNode(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("FindNode: %v", err)
	}
	if got.Name != "v2" {
		t.Errorf("AddNode did not update Name as expected: got %q want %q", got.Name, "v2")
	}
	if ig, _ := got.Metadata["intentional_gap"].(bool); ig {
		t.Errorf("AddNode preserved metadata when contract says it should clobber; behavior changed: metadata=%+v", got.Metadata)
	}
}

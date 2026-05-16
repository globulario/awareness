package integrity_test

// cross_link_test.go — acceptance tests for the cross-link density
// audit. Each test seeds a tiny in-memory graph with a specific gap
// shape and verifies the counter fires exactly when expected.

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
)

func newGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

func seed(t *testing.T, g *graph.Graph, n graph.Node) {
	t.Helper()
	if err := g.AddNode(context.Background(), n); err != nil {
		t.Fatalf("AddNode %s: %v", n.ID, err)
	}
}

func link(t *testing.T, g *graph.Graph, src, kind, dst string) {
	t.Helper()
	if err := g.AddEdge(context.Background(), graph.Edge{Src: src, Kind: kind, Dst: dst}); err != nil {
		t.Fatalf("AddEdge %s -%s-> %s: %v", src, kind, dst, err)
	}
}

// integrity.Check requires DocsDir to load YAMLs. We pass an empty
// temp dir — the YAML loaders treat missing files as empty and the
// graph-dependent checks still run.
func runCheck(t *testing.T, g *graph.Graph) *integrity.IntegrityResult {
	t.Helper()
	r, err := integrity.Check(context.Background(), integrity.Options{
		DocsDir: t.TempDir(),
	}, g)
	if err != nil {
		t.Fatalf("integrity.Check: %v", err)
	}
	return r
}

// TestCrossLink_AllZeroForWellLinkedGraph pins the happy path: when
// every failure_mode has BOTH a tested_by edge to a test AND a
// violates edge to an invariant, every counter is zero.
func TestCrossLink_AllZeroForWellLinkedGraph(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "failure_mode:fm.good", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "test:TestGood", Type: graph.NodeTypeTest, Name: "TestGood"})
	seed(t, g, graph.Node{ID: "invariant:inv.good", Type: graph.NodeTypeInvariant})
	link(t, g, "failure_mode:fm.good", "tested_by", "test:TestGood")
	link(t, g, "failure_mode:fm.good", "violates", "invariant:inv.good")

	r := runCheck(t, g)
	cld := r.CrossLinkDensity
	if cld.OrphanFailureModes != 0 {
		t.Errorf("OrphanFailureModes = %d, want 0", cld.OrphanFailureModes)
	}
	if cld.OrphanInvariants != 0 {
		t.Errorf("OrphanInvariants = %d, want 0", cld.OrphanInvariants)
	}
	if cld.FailureModesWithoutTests != 0 {
		t.Errorf("FailureModesWithoutTests = %d, want 0; ids=%v",
			cld.FailureModesWithoutTests, cld.FailureModesWithoutTestsIDs)
	}
	if cld.FailureModesWithoutInvariants != 0 {
		t.Errorf("FailureModesWithoutInvariants = %d, want 0; ids=%v",
			cld.FailureModesWithoutInvariants, cld.FailureModesWithoutInvariantIDs)
	}
}

// TestCrossLink_FailureModeWithoutTestsCounted pins: a failure_mode
// with a violates edge but no tested_by/verifies/validates edge to a
// test node bumps FailureModesWithoutTests by one.
func TestCrossLink_FailureModeWithoutTestsCounted(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "failure_mode:fm.no_test", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "invariant:inv.x", Type: graph.NodeTypeInvariant})
	link(t, g, "failure_mode:fm.no_test", "violates", "invariant:inv.x")

	r := runCheck(t, g)
	if r.CrossLinkDensity.FailureModesWithoutTests != 1 {
		t.Errorf("FailureModesWithoutTests = %d, want 1; ids=%v",
			r.CrossLinkDensity.FailureModesWithoutTests,
			r.CrossLinkDensity.FailureModesWithoutTestsIDs)
	}
	if r.CrossLinkDensity.FailureModesWithoutInvariants != 0 {
		t.Errorf("FailureModesWithoutInvariants = %d, want 0",
			r.CrossLinkDensity.FailureModesWithoutInvariants)
	}
}

// TestCrossLink_FailureModeWithoutInvariantsCounted pins the inverse:
// a failure_mode with tested_by but no violates edge to an invariant
// bumps FailureModesWithoutInvariants.
func TestCrossLink_FailureModeWithoutInvariantsCounted(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "failure_mode:fm.no_inv", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "test:TestX", Type: graph.NodeTypeTest})
	link(t, g, "failure_mode:fm.no_inv", "tested_by", "test:TestX")

	r := runCheck(t, g)
	if r.CrossLinkDensity.FailureModesWithoutInvariants != 1 {
		t.Errorf("FailureModesWithoutInvariants = %d, want 1; ids=%v",
			r.CrossLinkDensity.FailureModesWithoutInvariants,
			r.CrossLinkDensity.FailureModesWithoutInvariantIDs)
	}
	if r.CrossLinkDensity.FailureModesWithoutTests != 0 {
		t.Errorf("FailureModesWithoutTests = %d, want 0",
			r.CrossLinkDensity.FailureModesWithoutTests)
	}
}

// TestCrossLink_OrphanFailureModeNotDoubleCounted pins that a totally
// orphan failure_mode (no edges at all) bumps ONLY OrphanFailureModes,
// not the two missing-link counts. Otherwise an orphan would show up
// three times and inflate the alarm.
func TestCrossLink_OrphanFailureModeNotDoubleCounted(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "failure_mode:fm.orphan", Type: graph.NodeTypeFailureMode})

	r := runCheck(t, g)
	if r.CrossLinkDensity.OrphanFailureModes != 1 {
		t.Errorf("OrphanFailureModes = %d, want 1", r.CrossLinkDensity.OrphanFailureModes)
	}
	if r.CrossLinkDensity.FailureModesWithoutTests != 0 {
		t.Errorf("orphan should not also count as failure_modes_without_tests; got %d",
			r.CrossLinkDensity.FailureModesWithoutTests)
	}
	if r.CrossLinkDensity.FailureModesWithoutInvariants != 0 {
		t.Errorf("orphan should not also count as failure_modes_without_invariants; got %d",
			r.CrossLinkDensity.FailureModesWithoutInvariants)
	}
}

// TestCrossLink_OrphanInvariantCounted pins: an invariant with no
// edges at all counts in OrphanInvariants.
func TestCrossLink_OrphanInvariantCounted(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "invariant:inv.orphan", Type: graph.NodeTypeInvariant})

	r := runCheck(t, g)
	if r.CrossLinkDensity.OrphanInvariants != 1 {
		t.Errorf("OrphanInvariants = %d, want 1", r.CrossLinkDensity.OrphanInvariants)
	}
}

// TestCrossLink_AcceptsAlternateEdgeKinds pins the multiple-edge-kind
// contract: tested_by / verifies / validates / validated_by all count
// as test-link evidence, and violates / constrains / implements all
// count as invariant-link evidence. Without this, stylistic variation
// would inflate the warnings.
func TestCrossLink_AcceptsAlternateEdgeKinds(t *testing.T) {
	g := newGraph(t)
	seed(t, g, graph.Node{ID: "failure_mode:fm.v", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "test:T1", Type: graph.NodeTypeTest})
	seed(t, g, graph.Node{ID: "invariant:inv.c", Type: graph.NodeTypeInvariant})
	// Use the alternate edge kinds.
	link(t, g, "test:T1", "verifies", "failure_mode:fm.v")
	link(t, g, "failure_mode:fm.v", "constrains", "invariant:inv.c")

	r := runCheck(t, g)
	if r.CrossLinkDensity.FailureModesWithoutTests != 0 {
		t.Errorf("'verifies' edge should count as test-link; got %d missing",
			r.CrossLinkDensity.FailureModesWithoutTests)
	}
	if r.CrossLinkDensity.FailureModesWithoutInvariants != 0 {
		t.Errorf("'constrains' edge should count as invariant-link; got %d missing",
			r.CrossLinkDensity.FailureModesWithoutInvariants)
	}
}

// TestCrossLink_NilGraphIsSafe pins safety: when the integrity check
// runs without a graph (DocsDir-only mode), CrossLinkDensity stays
// zero-valued and the audit doesn't panic.
func TestCrossLink_NilGraphIsSafe(t *testing.T) {
	r, err := integrity.Check(context.Background(), integrity.Options{DocsDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatalf("integrity.Check (nil graph): %v", err)
	}
	if r.CrossLinkDensity.OrphanFailureModes != 0 ||
		r.CrossLinkDensity.OrphanInvariants != 0 ||
		r.CrossLinkDensity.FailureModesWithoutTests != 0 ||
		r.CrossLinkDensity.FailureModesWithoutInvariants != 0 {
		t.Errorf("expected zero-valued CrossLinkDensity for nil graph; got %+v",
			r.CrossLinkDensity)
	}
}

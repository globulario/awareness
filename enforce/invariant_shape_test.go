package enforce_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

// addBasicInvariant adds an invariant node with its graph UpsertInvariant record.
func addBasicInvariant(t *testing.T, g *graph.Graph, id string) {
	t.Helper()
	ctx := context.Background()
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:" + id, Type: graph.NodeTypeInvariant, Name: id})
	_ = g.UpsertInvariant(ctx, graph.Invariant{ID: id, Title: id, Severity: "high", Status: "active"})
}

// TestInvariantShape_NoImplementation verifies the no-implementation check fires
// when no file implements the invariant.
func TestInvariantShape_NoImplementation(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.no_impl")

	// Add a test so the no-test-coverage check doesn't also fire (isolate check 1).
	_ = g.AddNode(ctx, graph.Node{ID: "test:TTest", Type: graph.NodeTypeTest, Name: "TTest"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:test.shape.no_impl", Kind: graph.EdgeTestedBy, Dst: "test:TTest"})

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantNoImplementation {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_NO_IMPLEMENTATION finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_NoTestCoverage verifies the no-test-coverage check fires.
func TestInvariantShape_NoTestCoverage(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.no_test")

	// Add an implementation so check 1 is satisfied.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:foo.go", Type: graph.NodeTypeSourceFile, Name: "foo.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:foo.go", Kind: graph.EdgeImplements, Dst: "invariant:test.shape.no_test"})

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantNoTestCoverage {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_NO_TEST_COVERAGE finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_NoFinding verifies a fully-connected invariant produces no shape finding.
func TestInvariantShape_NoFinding(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.complete")

	invID := "invariant:test.shape.complete"

	// Implementation.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:impl.go", Type: graph.NodeTypeSourceFile, Name: "impl.go", Path: "impl.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:impl.go", Kind: graph.EdgeImplements, Dst: invID})
	// Test via verifies (stronger than tested_by).
	_ = g.AddNode(ctx, graph.Node{ID: "test:TComplete", Type: graph.NodeTypeTest, Name: "TComplete"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "test:TComplete", Kind: graph.EdgeVerifies, Dst: invID})
	// tested_by for good measure.
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeTestedBy, Dst: "test:TComplete"})
	// Failure mode.
	_ = g.AddNode(ctx, graph.Node{ID: "failure_mode:fm1", Type: graph.NodeTypeFailureMode, Name: "fm1"})
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeAffects, Dst: "failure_mode:fm1"})
	// Forbidden fix.
	_ = g.AddNode(ctx, graph.Node{ID: "forbidden_fix:ff1", Type: graph.NodeTypeForbiddenFix, Name: "ff1"})
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeForbids, Dst: "forbidden_fix:ff1"})
	// Authority (satisfies check 6).
	_ = g.AddNode(ctx, graph.Node{ID: "authority:/etcd/key", Type: "authority_source", Name: "/etcd/key"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:impl.go", Kind: graph.EdgeReadsAuthority, Dst: "authority:/etcd/key"})

	res := enforce.InvariantShapeCheck(ctx, g)
	for _, f := range res.Findings {
		if f.File == invID || f.File == "invariant:test.shape.complete" {
			t.Errorf("unexpected finding for complete invariant: %+v", f)
		}
	}
}

// TestInvariantShape_OrphanImplementation verifies the orphan check fires when
// an implementing file node is missing from the graph.
func TestInvariantShape_OrphanImplementation(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.orphan")

	invID := "invariant:test.shape.orphan"

	// Add an implements edge whose src node does NOT exist in the graph.
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:ghost.go", Kind: graph.EdgeImplements, Dst: invID})
	// Add a test so coverage check doesn't fire.
	_ = g.AddNode(ctx, graph.Node{ID: "test:TOrphan", Type: graph.NodeTypeTest, Name: "TOrphan"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "test:TOrphan", Kind: graph.EdgeVerifies, Dst: invID})

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantOrphanImpl {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_ORPHAN_IMPLEMENTATION finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_ViolatedNoTest verifies the violated-no-test check fires.
func TestInvariantShape_ViolatedNoTest(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.violated_no_test")

	invID := "invariant:test.shape.violated_no_test"

	// Failure mode violates invariant.
	_ = g.AddNode(ctx, graph.Node{ID: "failure_mode:fm.breach", Type: graph.NodeTypeFailureMode, Name: "fm.breach"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "failure_mode:fm.breach", Kind: graph.EdgeViolates, Dst: invID})

	// No test — no tested_by, no verifies.

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantViolatedNoTest {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_VIOLATED_NO_TEST finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_ForbiddenFixNoGuard verifies the forbidden-fix-no-guard check fires.
func TestInvariantShape_ForbiddenFixNoGuard(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.fix_no_guard")

	invID := "invariant:test.shape.fix_no_guard"

	// Forbidden fix declares blocks_forbidden_action but no test exists.
	_ = g.AddNode(ctx, graph.Node{ID: "forbidden_fix:bad_act", Type: graph.NodeTypeForbiddenFix, Name: "bad_act"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "forbidden_fix:bad_act", Kind: graph.EdgeBlocksForbiddenAction, Dst: invID})

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantForbiddenFixNoGuard {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_FORBIDDEN_FIX_NO_GUARD finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_UnverifiedImplementation verifies the unverified-implementation
// check fires when only tested_by (not verifies) exists.
func TestInvariantShape_UnverifiedImplementation(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.unverified")

	invID := "invariant:test.shape.unverified"

	// Implementation present.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:unv.go", Type: graph.NodeTypeSourceFile, Name: "unv.go", Path: "unv.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:unv.go", Kind: graph.EdgeImplements, Dst: invID})
	// Only tested_by — no verifies.
	_ = g.AddNode(ctx, graph.Node{ID: "test:TUnv", Type: graph.NodeTypeTest, Name: "TUnv"})
	_ = g.AddEdge(ctx, graph.Edge{Src: invID, Kind: graph.EdgeTestedBy, Dst: "test:TUnv"})

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantUnverifiedImpl {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_UNVERIFIED_IMPLEMENTATION finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_GuardsActionUntested verifies the guards-unreachable check fires
// when a file guards an action but no test verifies the invariant.
func TestInvariantShape_GuardsActionUntested(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.guards_unreachable")

	invID := "invariant:test.shape.guards_unreachable"

	// Implementation with guards_action declared.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:guard.go", Type: graph.NodeTypeSourceFile, Name: "guard.go", Path: "guard.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:guard.go", Kind: graph.EdgeImplements, Dst: invID})
	_ = g.AddNode(ctx, graph.Node{ID: "action:rpc.Do", Type: "guarded_action", Name: "rpc.Do"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:guard.go", Kind: graph.EdgeGuardsAction, Dst: "action:rpc.Do"})

	// No test verifies the invariant.

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantGuardsUnreachable {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_GUARDS_ACTION_UNREACHABLE finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_MissingAuthority verifies the missing-authority check fires
// when implementations exist but no reads_authority edge is declared.
func TestInvariantShape_MissingAuthority(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.missing_auth")

	invID := "invariant:test.shape.missing_auth"

	_ = g.AddNode(ctx, graph.Node{ID: "source_file:na.go", Type: graph.NodeTypeSourceFile, Name: "na.go", Path: "na.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:na.go", Kind: graph.EdgeImplements, Dst: invID})
	// No reads_authority edges anywhere.

	res := enforce.InvariantShapeCheck(ctx, g)
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantMissingAuthority {
			found = true
		}
	}
	if !found {
		t.Errorf("expected INVARIANT_MISSING_AUTHORITY finding; got %+v", res.Findings)
	}
}

// TestInvariantShape_WiredIntoAudit verifies InvariantShapeCheck findings appear
// in Audit() output when a graph is provided and SkipInvariantShape is false.
func TestInvariantShape_WiredIntoAudit(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()
	addBasicInvariant(t, g, "test.shape.audit_wired")
	// Invariant has no implementation or test — shape check should fire.

	result := enforce.Audit(ctx, g, enforce.AuditOptions{
		SkipAnnotations: true,
		SkipContracts:   true,
		SkipTests:       true,
		SkipDrift:       true,
		SkipScaffold:    true,
		// SkipInvariantShape: false (default)
	})

	found := false
	for _, f := range result.Findings {
		if f.Code == enforce.CodeInvariantNoImplementation || f.Code == enforce.CodeInvariantNoTestCoverage {
			found = true
		}
	}
	if !found {
		t.Errorf("expected shape findings in Audit() output; got %d findings", len(result.Findings))
	}
}

package contextnav

// owner_test.go — Phase 3 acceptance tests for InferOwner.
// Seeds an in-memory graph with finding nodes connected to layer-typed
// neighbors and verifies the resolver picks the right layer.

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

// newGraph spins up an in-memory awareness graph for tests.
func newGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// seed inserts the node, ignoring upsert errors so the helper stays terse.
func seed(t *testing.T, g *graph.Graph, n graph.Node) {
	t.Helper()
	if err := g.AddNode(context.Background(), n); err != nil {
		t.Fatalf("AddNode %s: %v", n.ID, err)
	}
}

// link inserts an edge between src and dst.
func link(t *testing.T, g *graph.Graph, src, kind, dst string) {
	t.Helper()
	if err := g.AddEdge(context.Background(), graph.Edge{
		Src: src, Kind: kind, Dst: dst,
	}); err != nil {
		t.Fatalf("AddEdge %s -%s-> %s: %v", src, kind, dst, err)
	}
}

// TestInferOwner_RuntimeWins seeds a failure_mode connected to a runtime
// service status + systemd unit and verifies the layer resolves to runtime.
func TestInferOwner_RuntimeWins(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.x", Type: graph.NodeTypeFailureMode, Name: "fm.x"})
	seed(t, g, graph.Node{ID: "runtime_service_status:workflow@nuc", Type: graph.NodeTypeRuntimeServiceStatus, Name: "workflow@nuc"})
	seed(t, g, graph.Node{ID: "systemd_unit:workflow", Type: graph.NodeTypeSystemdUnit, Name: "workflow.service"})
	link(t, g, "failure_mode:fm.x", "observed_in", "runtime_service_status:workflow@nuc")
	link(t, g, "failure_mode:fm.x", "observed_in", "systemd_unit:workflow")

	got := InferOwner(ctx, g, "failure_mode:fm.x", "", nil)
	if got.Layer != LayerRuntime {
		t.Errorf("Layer = %q, want %q (runtime evidence dominant)", got.Layer, LayerRuntime)
	}
}

// TestInferOwner_RepositoryWins seeds neighbors in the repository layer
// (package + service_release + repository_status) and verifies the layer.
func TestInferOwner_RepositoryWins(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.publish", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "package:workflow", Type: graph.NodeTypePackage, Name: "workflow"})
	seed(t, g, graph.Node{ID: "service_release:workflow.1.2.3", Type: graph.NodeTypeServiceRelease, Name: "workflow.1.2.3"})
	seed(t, g, graph.Node{ID: "repository_status:workflow", Type: graph.NodeTypeRepositoryStatus, Name: "workflow"})
	link(t, g, "failure_mode:fm.publish", "implicates", "package:workflow")
	link(t, g, "failure_mode:fm.publish", "implicates", "service_release:workflow.1.2.3")
	link(t, g, "failure_mode:fm.publish", "implicates", "repository_status:workflow")

	got := InferOwner(ctx, g, "failure_mode:fm.publish", "", nil)
	if got.Layer != LayerRepository {
		t.Errorf("Layer = %q, want %q", got.Layer, LayerRepository)
	}
	if got.Package != "workflow" {
		t.Errorf("Package = %q, want workflow", got.Package)
	}
}

// TestInferOwner_DesiredWins covers the desired-state layer.
func TestInferOwner_DesiredWins(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "invariant:inv.desired", Type: graph.NodeTypeInvariant})
	seed(t, g, graph.Node{ID: "desired_service:workflow", Type: graph.NodeTypeDesiredService, Name: "workflow"})
	seed(t, g, graph.Node{ID: "desired_state_record:workflow", Type: graph.NodeTypeDesiredStateRecord})
	link(t, g, "invariant:inv.desired", "constrains", "desired_service:workflow")
	link(t, g, "invariant:inv.desired", "constrains", "desired_state_record:workflow")

	got := InferOwner(ctx, g, "invariant:inv.desired", "", nil)
	if got.Layer != LayerDesired {
		t.Errorf("Layer = %q, want %q", got.Layer, LayerDesired)
	}
}

// TestInferOwner_InstalledWins covers the installed layer.
func TestInferOwner_InstalledWins(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.install", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "node_installed_package:nuc/workflow", Type: graph.NodeTypeNodeInstalledPackage})
	seed(t, g, graph.Node{ID: "installed_state_record:nuc/workflow", Type: graph.NodeTypeInstalledStateRecord})
	link(t, g, "failure_mode:fm.install", "implicates", "node_installed_package:nuc/workflow")
	link(t, g, "failure_mode:fm.install", "implicates", "installed_state_record:nuc/workflow")

	got := InferOwner(ctx, g, "failure_mode:fm.install", "", nil)
	if got.Layer != LayerInstalled {
		t.Errorf("Layer = %q, want %q", got.Layer, LayerInstalled)
	}
}

// TestInferOwner_ServicePackageFilesExtracted verifies that neighbor
// service / package / source_file / symbol nodes get pulled into the
// OwnerContext alongside the layer label.
func TestInferOwner_ServicePackageFilesExtracted(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.svc", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "globular_service:workflow", Type: graph.NodeTypeGlobularService, Name: "workflow"})
	seed(t, g, graph.Node{ID: "package:workflow", Type: graph.NodeTypePackage, Name: "workflow"})
	seed(t, g, graph.Node{ID: "source_file:golang/workflow/engine.go", Type: graph.NodeTypeSourceFile, Path: "golang/workflow/engine.go"})
	seed(t, g, graph.Node{ID: "symbol:Engine.Resume", Type: graph.NodeTypeSymbol, Name: "Engine.Resume"})
	link(t, g, "failure_mode:fm.svc", "owned_by", "globular_service:workflow")
	link(t, g, "failure_mode:fm.svc", "owned_by", "package:workflow")
	link(t, g, "failure_mode:fm.svc", "implemented_in", "source_file:golang/workflow/engine.go")
	link(t, g, "failure_mode:fm.svc", "implemented_in", "symbol:Engine.Resume")

	got := InferOwner(ctx, g, "failure_mode:fm.svc", "", nil)
	if got.Service != "workflow" {
		t.Errorf("Service = %q, want workflow", got.Service)
	}
	if got.Package != "workflow" {
		t.Errorf("Package = %q, want workflow", got.Package)
	}
	if len(got.Files) == 0 || got.Files[0] != "golang/workflow/engine.go" {
		t.Errorf("Files = %v, want [golang/workflow/engine.go]", got.Files)
	}
	if len(got.Symbols) == 0 || got.Symbols[0] != "Engine.Resume" {
		t.Errorf("Symbols = %v, want [Engine.Resume]", got.Symbols)
	}
}

// TestInferOwner_NoNeighborsReturnsUnknown pins the no-match path: a
// finding with no graph edges to layer-typed nodes returns
// Layer="unknown" (NOT a default-pick layer).
func TestInferOwner_NoNeighborsReturnsUnknown(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.orphan", Type: graph.NodeTypeFailureMode})

	got := InferOwner(ctx, g, "failure_mode:fm.orphan", "", nil)
	if got.Layer != LayerUnknown {
		t.Errorf("Layer = %q, want %q (no neighbors)", got.Layer, LayerUnknown)
	}
}

// TestInferOwner_FileHintEnrichesUnknown pins that file hints flow into
// OwnerContext.Files even when no graph neighbor was found — the agent
// still gets handles to inspect.
func TestInferOwner_FileHintEnrichesUnknown(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	got := InferOwner(ctx, g, "failure_mode:no-such-node", "",
		[]string{"golang/repository/server.go"})
	if got.Layer != LayerUnknown {
		t.Errorf("Layer = %q, want %q", got.Layer, LayerUnknown)
	}
	if len(got.Files) == 0 || got.Files[0] != "golang/repository/server.go" {
		t.Errorf("Files = %v, want [golang/repository/server.go]", got.Files)
	}
}

// TestInferOwner_TaskClassBreaksTie pins the task-class tiebreaker: when
// two layers tie on neighbor count, the runtime-incident task hint pulls
// the verdict toward runtime over desired.
func TestInferOwner_TaskClassBreaksTie(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.tie", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "desired_service:x", Type: graph.NodeTypeDesiredService})
	seed(t, g, graph.Node{ID: "runtime_service_status:x", Type: graph.NodeTypeRuntimeServiceStatus})
	link(t, g, "failure_mode:fm.tie", "constrains", "desired_service:x")
	link(t, g, "failure_mode:fm.tie", "observed_in", "runtime_service_status:x")

	// Both scored 1; task hint says runtime incident → runtime wins.
	got := InferOwner(ctx, g, "failure_mode:fm.tie", "we are debugging a restart storm incident", nil)
	if got.Layer != LayerRuntime {
		t.Errorf("Layer = %q, want %q (task=runtime_incident should break tie)", got.Layer, LayerRuntime)
	}
}

// TestInferOwner_NilGraphIsSafe pins the graceful-degradation path: a nil
// graph reference returns Layer="unknown" without panicking. Build relies
// on this when called from pure unit tests.
func TestInferOwner_NilGraphIsSafe(t *testing.T) {
	got := InferOwner(context.Background(), nil, "failure_mode:x", "", []string{"a.go"})
	if got.Layer != LayerUnknown {
		t.Errorf("Layer = %q, want %q", got.Layer, LayerUnknown)
	}
	if len(got.Files) != 1 || got.Files[0] != "a.go" {
		t.Errorf("Files = %v, want [a.go]", got.Files)
	}
}

// TestBuild_OwnerPopulatedWhenGraphProvided is the end-to-end test for
// the Phase 3 wiring: Build called with Graph+Ctx populates Owner on
// each non-rawKnowledge trace, AND callers don't see an "owner: unknown"
// warning when a graph-derived layer was resolved.
func TestBuild_OwnerPopulatedWhenGraphProvided(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.workflow", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "workflow:wf-1", Type: graph.NodeTypeWorkflow, Name: "wf-1"})
	link(t, g, "failure_mode:fm.workflow", "occurs_in", "workflow:wf-1")

	traces := Build(BuildInputs{
		FailureModes:        []string{"fm.workflow"},
		Confidence:          ConfidenceHigh,
		GraphFreshnessKnown: true,
		Graph:               g,
		Ctx:                 ctx,
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Owner.Layer != LayerWorkflow {
		t.Errorf("Owner.Layer = %q, want %q", traces[0].Owner.Layer, LayerWorkflow)
	}
	for _, w := range traces[0].Warnings {
		if w == "owner: layer inference returned unknown — no graph neighbor mapped to a layer, no task hint matched" {
			t.Errorf("unexpected unknown-owner warning: %q", w)
		}
	}
}

// TestBuild_OwnerWarningWhenUnknown pins the unknown-owner warning path:
// a failure_mode with no layer-typed neighbors AND no task hint produces
// the warn message so the agent sees the gap.
func TestBuild_OwnerWarningWhenUnknown(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:fm.orphan", Type: graph.NodeTypeFailureMode})

	traces := Build(BuildInputs{
		FailureModes:        []string{"fm.orphan"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		Graph:               g,
		Ctx:                 ctx,
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].Owner.Layer != LayerUnknown {
		t.Errorf("Owner.Layer = %q, want %q", traces[0].Owner.Layer, LayerUnknown)
	}
	var sawWarning bool
	for _, w := range traces[0].Warnings {
		if w == "owner: layer inference returned unknown — no graph neighbor mapped to a layer, no task hint matched" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Errorf("expected unknown-owner warning; got %+v", traces[0].Warnings)
	}
}

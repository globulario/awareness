package semantic_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/semantic"
)

// ---- fixture ----

// openFixtureGraph builds a small graph that mirrors the DesiredHash domain.
//
// Nodes:
//
//	symbol:lookupServiceReleaseBuildID  (NodeTypeSymbol)
//	invariant:infra.desired_hash_consistency (NodeTypeInvariant, critical)
//	failure_mode:infra.desired_hash_mismatch_restart_storm (NodeTypeFailureMode)
//	forbidden_fix:use_raw_artifact_digest_as_desired_hash (NodeTypeForbiddenFix)
//	test:TestDriftWorkflowUsesDesiredHash (NodeTypeTest)
//	file:golang/cluster_controller/hash.go (NodeTypeSourceFile)
//	service:cluster_controller (NodeTypeGlobularService)
//
// Edges:
//
//	symbol   --enforces-->      invariant
//	invariant--caused_by-->     failure_mode
//	invariant--forbids-->       forbidden_fix
//	invariant--tested_by-->     test
//	file     --defines-->       symbol
//	service  --owns-->          file
func openFixtureGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "symbol:lookupServiceReleaseBuildID", Type: graph.NodeTypeSymbol, Name: "lookupServiceReleaseBuildID"},
		{ID: "invariant:infra.desired_hash_consistency", Type: graph.NodeTypeInvariant, Name: "infra.desired_hash_consistency", Summary: "guards hash consistency"},
		{ID: "failure_mode:infra.desired_hash_mismatch_restart_storm", Type: graph.NodeTypeFailureMode, Name: "infra.desired_hash_mismatch_restart_storm", Summary: "hash mismatch causes restart storm"},
		{ID: "forbidden_fix:use_raw_artifact_digest_as_desired_hash", Type: graph.NodeTypeForbiddenFix, Name: "use_raw_artifact_digest_as_desired_hash"},
		{ID: "test:TestDriftWorkflowUsesDesiredHash", Type: graph.NodeTypeTest, Name: "TestDriftWorkflowUsesDesiredHash"},
		{ID: "file:golang/cluster_controller/hash.go", Type: graph.NodeTypeSourceFile, Name: "hash.go", Path: "golang/cluster_controller/hash.go"},
		{ID: "service:cluster_controller", Type: graph.NodeTypeGlobularService, Name: "cluster_controller"},
	}
	for _, n := range nodes {
		if err := g.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode %s: %v", n.ID, err)
		}
	}

	edges := []graph.Edge{
		{Src: "symbol:lookupServiceReleaseBuildID", Kind: graph.EdgeEnforces, Dst: "invariant:infra.desired_hash_consistency", Confidence: 1.0},
		{Src: "invariant:infra.desired_hash_consistency", Kind: graph.EdgeCausedBy, Dst: "failure_mode:infra.desired_hash_mismatch_restart_storm", Confidence: 1.0},
		{Src: "invariant:infra.desired_hash_consistency", Kind: graph.EdgeForbids, Dst: "forbidden_fix:use_raw_artifact_digest_as_desired_hash", Confidence: 1.0},
		{Src: "invariant:infra.desired_hash_consistency", Kind: graph.EdgeTestedBy, Dst: "test:TestDriftWorkflowUsesDesiredHash", Confidence: 1.0},
		{Src: "file:golang/cluster_controller/hash.go", Kind: graph.EdgeDefines, Dst: "symbol:lookupServiceReleaseBuildID", Confidence: 1.0},
		{Src: "service:cluster_controller", Kind: graph.EdgeOwns, Dst: "file:golang/cluster_controller/hash.go", Confidence: 1.0},
	}
	for _, e := range edges {
		if err := g.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge %s-[%s]->%s: %v", e.Src, e.Kind, e.Dst, err)
		}
	}

	// Upsert invariant and failure mode records.
	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "infra.desired_hash_consistency",
		Title:    "infra.desired_hash_consistency",
		Summary:  "guards hash consistency across the cluster",
		Severity: "critical",
		Status:   "active",
	}); err != nil {
		t.Fatalf("UpsertInvariant: %v", err)
	}
	if err := g.UpsertFailureMode(ctx, graph.FailureMode{
		ID:      "infra.desired_hash_mismatch_restart_storm",
		Title:   "infra.desired_hash_mismatch_restart_storm",
		Summary: "hash mismatch between controller and node-agent causes restart storm",
	}); err != nil {
		t.Fatalf("UpsertFailureMode: %v", err)
	}

	return g
}

// ---- weight tests ----

func TestEdgeWeight_EnforcesChreaperThanImports(t *testing.T) {
	enforces := graph.Edge{Kind: graph.EdgeEnforces, Confidence: 1.0}
	imports := graph.Edge{Kind: graph.EdgeImports, Confidence: 1.0}

	wEnforces := semantic.EdgeWeight(semantic.DimensionArch, enforces)
	wImports := semantic.EdgeWeight(semantic.DimensionArch, imports)

	if wEnforces >= wImports {
		t.Errorf("expected enforces (%.2f) < imports (%.2f) in architecture dimension", wEnforces, wImports)
	}
}

func TestEdgeWeight_ExplicitBoost(t *testing.T) {
	base := graph.Edge{Kind: graph.EdgeCalls, Confidence: 1.0}
	explicit := graph.Edge{Kind: graph.EdgeCalls, Confidence: 1.0, Metadata: map[string]any{"explicit": true}}

	wBase := semantic.EdgeWeight(semantic.DimensionAll, base)
	wExplicit := semantic.EdgeWeight(semantic.DimensionAll, explicit)

	if wExplicit >= wBase {
		t.Errorf("expected explicit edge (%.2f) cheaper than non-explicit (%.2f)", wExplicit, wBase)
	}
}

func TestEdgeWeight_ArchDimension_PrioritizesEnforces(t *testing.T) {
	enforces := graph.Edge{Kind: graph.EdgeEnforces, Confidence: 1.0}
	imports := graph.Edge{Kind: graph.EdgeImports, Confidence: 1.0}

	wEnforces := semantic.EdgeWeight(semantic.DimensionArch, enforces)
	wImports := semantic.EdgeWeight(semantic.DimensionArch, imports)

	if wEnforces >= wImports {
		t.Errorf("architecture: enforces (%.2f) should cost less than imports (%.2f)", wEnforces, wImports)
	}
}

// ---- path tests ----

func TestShortestPath_SymbolToFailureMode(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	path, err := semantic.ShortestPath(ctx, g,
		"symbol:lookupServiceReleaseBuildID",
		"failure_mode:infra.desired_hash_mismatch_restart_storm",
		semantic.PathOptions{Dimension: semantic.DimensionArch},
	)
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	if !path.Found {
		t.Fatal("expected path to be found")
	}
	if len(path.Steps) < 3 {
		t.Errorf("expected at least 3 steps (symbol -> invariant -> failure_mode), got %d", len(path.Steps))
	}
	// The path should pass through the invariant.
	found := false
	for _, s := range path.Steps {
		if s.NodeType == graph.NodeTypeInvariant {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected path to pass through invariant node")
	}
}

func TestShortestPath_PrefersEnforcesOverImports(t *testing.T) {
	// Build a graph with two paths from A to C:
	//   A --enforces--> B --enforces--> C  (cheap)
	//   A --imports-->  D --imports-->  C  (expensive)
	g, err := graph.Open(filepath.Join(t.TempDir(), "pref.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	ctx := context.Background()
	for _, n := range []graph.Node{
		{ID: "A", Type: graph.NodeTypeSymbol, Name: "A"},
		{ID: "B", Type: graph.NodeTypeInvariant, Name: "B"},
		{ID: "C", Type: graph.NodeTypeTest, Name: "C"},
		{ID: "D", Type: graph.NodeTypeSourceFile, Name: "D"},
	} {
		_ = g.AddNode(ctx, n)
	}
	_ = g.AddEdge(ctx, graph.Edge{Src: "A", Kind: graph.EdgeEnforces, Dst: "B", Confidence: 1.0})
	_ = g.AddEdge(ctx, graph.Edge{Src: "B", Kind: graph.EdgeEnforces, Dst: "C", Confidence: 1.0})
	_ = g.AddEdge(ctx, graph.Edge{Src: "A", Kind: graph.EdgeImports, Dst: "D", Confidence: 1.0})
	_ = g.AddEdge(ctx, graph.Edge{Src: "D", Kind: graph.EdgeImports, Dst: "C", Confidence: 1.0})

	path, err := semantic.ShortestPath(ctx, g, "A", "C", semantic.PathOptions{Dimension: semantic.DimensionArch})
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	if !path.Found {
		t.Fatal("expected path found")
	}
	// Enforces path should be chosen — check that node B (InvariantType) is in steps.
	usedEnforces := false
	for _, s := range path.Steps {
		if s.EdgeKind == graph.EdgeEnforces {
			usedEnforces = true
			break
		}
	}
	if !usedEnforces {
		t.Error("expected path to prefer enforces edges over imports")
	}
}

func TestShortestPath_NoPath(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	// Add an isolated node.
	_ = g.AddNode(ctx, graph.Node{ID: "isolated:x", Type: graph.NodeTypeEtcdKey, Name: "isolated"})

	path, err := semantic.ShortestPath(ctx, g,
		"symbol:lookupServiceReleaseBuildID",
		"isolated:x",
		semantic.PathOptions{Dimension: semantic.DimensionAll},
	)
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	if path.Found {
		t.Error("expected no path to isolated node")
	}
}

// ---- related tests ----

func TestRelated_NearestInvariant(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	results, err := semantic.Related(ctx, g, "symbol:lookupServiceReleaseBuildID", semantic.RelatedOptions{
		Dimension:   semantic.DimensionArch,
		TargetTypes: []string{graph.NodeTypeInvariant},
		MaxResults:  5,
	})
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one invariant result")
	}
	if results[0].Node.Type != graph.NodeTypeInvariant {
		t.Errorf("expected invariant node, got %s", results[0].Node.Type)
	}
}

func TestNearest_TestForInvariant(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	results, err := semantic.Nearest(ctx, g, "invariant:infra.desired_hash_consistency", graph.NodeTypeTest, semantic.RelatedOptions{
		Dimension:  semantic.DimensionTest,
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Nearest: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one test result")
	}
	found := false
	for _, r := range results {
		if r.Node.Name == "TestDriftWorkflowUsesDesiredHash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TestDriftWorkflowUsesDesiredHash in results")
	}
}

// ---- why tests ----

func TestWhyRelated_SymbolToFailureMode(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	result, err := semantic.WhyRelated(ctx, g,
		"symbol:lookupServiceReleaseBuildID",
		"failure_mode:infra.desired_hash_mismatch_restart_storm",
		semantic.WhyOptions{Dimension: semantic.DimensionArch},
	)
	if err != nil {
		t.Fatalf("WhyRelated: %v", err)
	}
	if result.RelationshipSummary == "" {
		t.Error("expected non-empty RelationshipSummary")
	}
	if result.Path == nil {
		t.Fatal("expected non-nil Path")
	}
	if !result.Path.Found {
		t.Error("expected path to be found")
	}
}

// ---- neighbourhood tests ----

func TestSemanticNeighborhood_RespectsLimit(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	results, err := semantic.SemanticNeighborhood(ctx, g, "service:cluster_controller", semantic.RelatedOptions{
		Dimension:  semantic.DimensionAll,
		MaxResults: 3,
		MaxDepth:   6,
	})
	if err != nil {
		t.Fatalf("SemanticNeighborhood: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected <= 3 results, got %d", len(results))
	}
}

// ---- format tests ----

func TestFormatPath_JSON(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	path, err := semantic.ShortestPath(ctx, g,
		"symbol:lookupServiceReleaseBuildID",
		"failure_mode:infra.desired_hash_mismatch_restart_storm",
		semantic.PathOptions{Dimension: semantic.DimensionArch},
	)
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}

	out := semantic.FormatPath(path, "json")

	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("FormatPath JSON not valid: %v\noutput: %s", err, out)
	}
	if _, ok := m["steps"]; !ok {
		t.Error("expected 'steps' key in JSON output")
	}
}

func TestFormatRelated_Agent(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	results, err := semantic.Related(ctx, g, "symbol:lookupServiceReleaseBuildID", semantic.RelatedOptions{
		Dimension:  semantic.DimensionArch,
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Related: %v", err)
	}

	out := semantic.FormatRelated(results, "symbol:lookupServiceReleaseBuildID", semantic.DimensionArch, "agent")
	if !strings.Contains(out, "AGENT SEMANTIC RELATED") {
		t.Errorf("expected 'AGENT SEMANTIC RELATED' in output, got:\n%s", out)
	}
}

func TestFormatWhy_Agent(t *testing.T) {
	g := openFixtureGraph(t)
	ctx := context.Background()

	result, err := semantic.WhyRelated(ctx, g,
		"symbol:lookupServiceReleaseBuildID",
		"failure_mode:infra.desired_hash_mismatch_restart_storm",
		semantic.WhyOptions{Dimension: semantic.DimensionArch},
	)
	if err != nil {
		t.Fatalf("WhyRelated: %v", err)
	}

	out := semantic.FormatWhy(result, "agent")
	if !strings.Contains(out, "AGENT WHY RELATED") {
		t.Errorf("expected 'AGENT WHY RELATED' in output, got:\n%s", out)
	}
}

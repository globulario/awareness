package manual_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/checkedit"
	"github.com/globulario/awareness/extractors/manual"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func writeDesignPatternsYAML(t *testing.T, dir string) string {
	t.Helper()
	content := `design_patterns:
  - id: pattern.circuit_breaker
    title: Circuit Breaker
    type: design_pattern
    summary: Stop retrying a failing dependency after N failures.
    applies_to:
      - golang/cluster_controller/cluster_controller_server/reconciler.go
    invariants:
      - convergence.no_infinite_retry
      - critical_queries.must_be_bounded
    failure_modes:
      - critical_queries.unbounded_hang
    forbidden_fixes:
      - blind_reconcile_retry
    code_smells:
      - "Retry loop with no circuit state or backoff"
      - "context.Background() on hot reconcile call"
    required_tests:
      - TestQueryTimeoutMappedToDegradedCategory
    recommended_searches:
      - "withBounded"
    safe_fix_rule: Pair every retry with a singleflight gate and a bounded context.

  - id: antipattern.unbounded_critical_query
    title: Unbounded Critical Query
    type: anti_pattern
    summary: Critical reconcile path with no deadline; hangs indefinitely.
    applies_to:
      - golang/dns/dns_server/reconciler.go
    invariants:
      - critical_queries.must_be_bounded
    failure_modes:
      - critical_queries.unbounded_hang
    forbidden_fixes:
      - block_controller_loop_on_external_query
    code_smells:
      - "etcd.Get with context.Background() in reconciler"
      - "No withBounded() wrapper on hot path"
    required_tests:
      - TestSlowBackendTriggersTimeoutAndLaneRecovery
    safe_fix_rule: Wrap every etcd and gRPC call with withBounded() or a deadline.
`
	path := filepath.Join(dir, "design_patterns.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write design_patterns.yaml: %v", err)
	}
	return path
}

func makeTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	return g
}

// ── Test 1: loader creates design_pattern and anti_pattern nodes ──────────────

func TestDesignPatternLoaderCreatesNodes(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	if err := manual.LoadDesignPatterns(context.Background(), g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	ctx := context.Background()

	// design_pattern node must exist.
	dpNode, err := g.FindNode(ctx, "design_pattern:pattern.circuit_breaker")
	if err != nil {
		t.Fatalf("FindNode design_pattern: %v", err)
	}
	if dpNode == nil {
		t.Fatal("design_pattern:pattern.circuit_breaker node not created")
	}
	if dpNode.Type != graph.NodeTypeDesignPattern {
		t.Errorf("node type = %q, want %q", dpNode.Type, graph.NodeTypeDesignPattern)
	}

	// anti_pattern node must exist.
	apNode, err := g.FindNode(ctx, "anti_pattern:antipattern.unbounded_critical_query")
	if err != nil {
		t.Fatalf("FindNode anti_pattern: %v", err)
	}
	if apNode == nil {
		t.Fatal("anti_pattern:antipattern.unbounded_critical_query node not created")
	}
	if apNode.Type != graph.NodeTypeAntiPattern {
		t.Errorf("node type = %q, want %q", apNode.Type, graph.NodeTypeAntiPattern)
	}
}

// ── Test 2: anti_pattern loader creates code_smell nodes ─────────────────────

func TestAntiPatternLoaderCreatesCodeSmellNodes(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	if err := manual.LoadDesignPatterns(context.Background(), g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	ctx := context.Background()

	// code_smell nodes must exist for the anti_pattern.
	smells, err := g.FindNodesByType(ctx, graph.NodeTypeCodeSmell)
	if err != nil {
		t.Fatalf("FindNodesByType code_smell: %v", err)
	}
	if len(smells) == 0 {
		t.Fatal("no code_smell nodes created")
	}

	// Verify EdgeSmellsLike edges exist from anti_pattern to code_smell.
	apNodeID := "anti_pattern:antipattern.unbounded_critical_query"
	edges, err := g.OutgoingEdges(ctx, apNodeID)
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}
	smellEdgeCount := 0
	for _, e := range edges {
		if e.Kind == graph.EdgeSmellsLike {
			smellEdgeCount++
		}
	}
	if smellEdgeCount == 0 {
		t.Error("no smells_like edges from anti_pattern node")
	}
}

// ── Test 3: loader creates EdgeViolates from anti_pattern → invariant ─────────

func TestAntiPatternLinksToInvariantViaViolates(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	if err := manual.LoadDesignPatterns(context.Background(), g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	ctx := context.Background()
	apNodeID := "anti_pattern:antipattern.unbounded_critical_query"
	edges, err := g.OutgoingEdges(ctx, apNodeID)
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}

	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeViolates && e.Dst == "invariant:critical_queries.must_be_bounded" {
			found = true
			break
		}
	}
	if !found {
		t.Error("anti_pattern does not have violates edge to invariant:critical_queries.must_be_bounded")
	}
}

// ── Test 4: loader creates EdgeRequires from design_pattern → invariant ───────

func TestDesignPatternLinksToInvariantViaRequires(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	if err := manual.LoadDesignPatterns(context.Background(), g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	ctx := context.Background()
	dpNodeID := "design_pattern:pattern.circuit_breaker"
	edges, err := g.OutgoingEdges(ctx, dpNodeID)
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}

	foundRequires := false
	foundMitigates := false
	for _, e := range edges {
		if e.Kind == graph.EdgeRequires && e.Dst == "invariant:critical_queries.must_be_bounded" {
			foundRequires = true
		}
		if e.Kind == graph.EdgeMitigates {
			foundMitigates = true
		}
	}
	if !foundRequires {
		t.Error("design_pattern does not have requires edge to invariant:critical_queries.must_be_bounded")
	}
	if !foundMitigates {
		t.Error("design_pattern does not have mitigates edge to failure_mode")
	}
}

// ── Test 5: DesignContextForInvariants surfaces relevant pattern ──────────────

func TestPreflightSurfacesRelevantDesignPattern(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	if err := manual.LoadDesignPatterns(context.Background(), g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	ctx := context.Background()

	// Query with the invariant that the circuit_breaker design_pattern requires.
	dc, err := g.DesignContextForInvariants(ctx, []string{
		"invariant:critical_queries.must_be_bounded",
	})
	if err != nil {
		t.Fatalf("DesignContextForInvariants: %v", err)
	}

	foundDesignPattern := false
	for _, p := range dc.DesignPatterns {
		if p == "pattern.circuit_breaker" {
			foundDesignPattern = true
		}
	}
	if !foundDesignPattern {
		t.Errorf("pattern.circuit_breaker not in design patterns; got %v", dc.DesignPatterns)
	}

	foundAntiPattern := false
	for _, p := range dc.AntiPatterns {
		if p == "antipattern.unbounded_critical_query" {
			foundAntiPattern = true
		}
	}
	if !foundAntiPattern {
		t.Errorf("antipattern.unbounded_critical_query not in anti patterns; got %v", dc.AntiPatterns)
	}

	if len(dc.CodeSmells) == 0 {
		t.Error("expected code smells from anti-pattern; got none")
	}
}

// ── Test 6: preflight Run surfaces design patterns ────────────────────────────

func TestPreflightRunSurfacesDesignPatterns(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	// Seed an invariant so preflight can match the pattern.
	ctx := context.Background()
	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "critical_queries.must_be_bounded",
		Title:    "Queries must be bounded",
		Severity: "critical",
		Summary:  "Every critical query must have a deadline.",
	}); err != nil {
		t.Fatalf("UpsertInvariant: %v", err)
	}

	dir := t.TempDir()
	writeDesignPatternsYAML(t, dir)
	// Write minimal required alias + fix files so preflight doesn't error.
	for _, f := range []string{"context_aliases.yaml", "fix_cases.yaml", "guardrails.yaml"} {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("{}\n"), 0o644)
	}
	// Write context_aliases so the task matches the invariant.
	aliases := `context_aliases:
  critical_queries.must_be_bounded:
    - unbounded query
    - context.Background in reconciler
`
	if err := os.WriteFile(filepath.Join(dir, "context_aliases.yaml"), []byte(aliases), 0o644); err != nil {
		t.Fatalf("write aliases: %v", err)
	}

	// Load design patterns into the graph.
	if err := manual.LoadDesignPatterns(ctx, g, filepath.Join(dir, "design_patterns.yaml")); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	opts := preflight.Options{
		Task:    "fix unbounded query in dns reconciler",
		DocsDir: dir,
	}
	r, err := preflight.Run(ctx, opts, g)
	if err != nil {
		t.Fatalf("preflight.Run: %v", err)
	}

	foundAntiPattern := false
	for _, ap := range r.AntiPatterns {
		if ap == "antipattern.unbounded_critical_query" {
			foundAntiPattern = true
		}
	}
	if !foundAntiPattern {
		t.Errorf("antipattern.unbounded_critical_query not in preflight anti-patterns; got %v", r.AntiPatterns)
	}
}

// ── Test 7: check-edit surfaces code smells for a file ───────────────────────

func TestCheckEditSurfacesCodeSmell(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	ctx := context.Background()

	dir := t.TempDir()
	writeDesignPatternsYAML(t, dir)
	if err := manual.LoadDesignPatterns(ctx, g, filepath.Join(dir, "design_patterns.yaml")); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	// Create a source_file node for the reconciler and link it to the invariant
	// via an EdgeProtects edge so checkedit's impact analysis can find it.
	fileNodeID := "source_file:golang/dns/dns_server/reconciler.go"
	invNodeID := "invariant:critical_queries.must_be_bounded"
	_ = g.AddNode(ctx, graph.Node{
		ID:   fileNodeID,
		Type: graph.NodeTypeSourceFile,
		Name: "golang/dns/dns_server/reconciler.go",
		Path: "golang/dns/dns_server/reconciler.go",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:      invNodeID,
		Type:    graph.NodeTypeInvariant,
		Name:    "critical_queries.must_be_bounded",
		Summary: "Queries must be bounded.",
	})
	// Edge: invariant protects file (so ImpactByFile reaches the invariant).
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  invNodeID,
		Kind: graph.EdgeProtects,
		Dst:  fileNodeID,
	})

	result, err := checkedit.Run(ctx, g, checkedit.Options{
		File: "golang/dns/dns_server/reconciler.go",
	})
	if err != nil {
		t.Fatalf("checkedit.Run: %v", err)
	}

	if len(result.AntiPatterns) == 0 && len(result.CodeSmells) == 0 {
		t.Error("expected anti-patterns or code smells for dns reconciler file, got none")
	}
}

// ── Test 8: missing file is silently skipped ──────────────────────────────────

func TestLoadDesignPatternsMissingFileIsSkipped(t *testing.T) {
	g := makeTestGraph(t)
	defer g.Close()

	err := manual.LoadDesignPatterns(context.Background(), g, "/nonexistent/design_patterns.yaml")
	if err != nil {
		t.Errorf("expected nil error for missing file, got: %v", err)
	}
}

// ── Test 9: go test ./golang/awareness/... -race passes ──────────────────────

func TestGoTestRacePassesForDesignPatterns(t *testing.T) {
	// This test verifies the loader is race-safe by running concurrent loads.
	g := makeTestGraph(t)
	defer g.Close()

	dir := t.TempDir()
	path := writeDesignPatternsYAML(t, dir)

	ctx := context.Background()

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- manual.LoadDesignPatterns(ctx, g, path)
		}()
	}
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent LoadDesignPatterns: %v", err)
		}
	}
}

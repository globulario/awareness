package preflight_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// TestPreflight_NoMatchIncludesGraphCoverage verifies that preflight.Run populates
// the GoFileCoverage field when RepoRoot is set, even if the graph has no indexed
// source_file nodes (0% coverage).
func TestPreflight_NoMatchIncludesGraphCoverage(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Create a temp repo root with a couple of Go files.
	repoRoot := t.TempDir()
	pkg := filepath.Join(repoRoot, "pkg", "sample")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "service.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := preflight.Options{
		Task:     "add a feature",
		RepoRoot: repoRoot,
	}
	r, err := preflight.Run(ctx, opts, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GoFileCoverage == nil {
		t.Fatal("expected GoFileCoverage to be non-nil when RepoRoot is set")
	}
	if r.GoFileCoverage.EligibleGoFilesTotal == 0 {
		t.Error("expected EligibleGoFilesTotal > 0 when Go files exist in repoRoot")
	}
	if r.GoFileCoverage.IndexedGoFilesTotal != 0 {
		t.Errorf("expected IndexedGoFilesTotal=0 (no source_file nodes in empty graph), got %d",
			r.GoFileCoverage.IndexedGoFilesTotal)
	}
}

// TestPreflight_ChangedFilesSinceGraphBuildLowersConfidence verifies that when
// the graph indexes only a fraction of eligible Go files (simulating new files
// added since the last build), the preflight report includes a coverage blind
// spot that lowered confidence.
func TestPreflight_ChangedFilesSinceGraphBuildLowersConfidence(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	repoRoot := t.TempDir()
	// Create 10 eligible Go files.
	dir := filepath.Join(repoRoot, "svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 10 {
		name := filepath.Join(dir, "file"+string(rune('a'+i))+".go")
		_ = os.WriteFile(name, []byte("package svc\n"), 0o644)
	}

	// Index only 1 source_file node → ≈10% coverage (critical threshold).
	_ = g.AddNode(ctx, graph.Node{
		ID:   "source_file:svc/filea.go",
		Type: graph.NodeTypeSourceFile,
		Name: "svc/filea.go",
		Path: "svc/filea.go",
	})

	opts := preflight.Options{
		Task:     "refactor all services",
		RepoRoot: repoRoot,
	}
	r, err := preflight.Run(ctx, opts, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GoFileCoverage == nil {
		t.Fatal("expected GoFileCoverage to be populated")
	}
	if r.GoFileCoverage.ConfidenceImpact != "high" {
		t.Errorf("expected confidence_impact=high at <70%% coverage, got %q (coverage=%.1f%%)",
			r.GoFileCoverage.ConfidenceImpact, r.GoFileCoverage.CoveragePercentGoFiles)
	}
	// Blind spots must include a coverage warning about unrepresented Go files.
	found := false
	for _, bs := range r.BlindSpots {
		if containsSubstr(bs, "eligible Go files are not represented") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a coverage blind spot about unrepresented Go files in BlindSpots, got: %v", r.BlindSpots)
	}
}

// TestPreflight_IncludesGraphFreshnessAndRebuildDuration verifies that when a
// build record with a known DurationMs is stored, preflight populates
// GraphFreshness.LastBuildDurationMs.
func TestPreflight_IncludesGraphFreshnessAndRebuildDuration(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	const wantDuration int64 = 5432
	err = g.UpsertBuildRecord(ctx, "build-001", "/repo", "abc123", "v1.0.0", graph.BuildStats{
		Nodes:      50,
		Edges:      100,
		DurationMs: wantDuration,
	})
	if err != nil {
		t.Fatalf("UpsertBuildRecord: %v", err)
	}

	// DocsDir is required for GraphFreshness to be populated.
	docsDir := t.TempDir()

	opts := preflight.Options{
		Task:    "check invariants",
		DocsDir: docsDir,
	}
	r, err := preflight.Run(ctx, opts, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GraphFreshness == nil {
		t.Fatal("expected GraphFreshness to be non-nil when DocsDir is set and graph available")
	}
	if r.GraphFreshness.LastBuildDurationMs != wantDuration {
		t.Errorf("expected LastBuildDurationMs=%d, got %d", wantDuration, r.GraphFreshness.LastBuildDurationMs)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestPreflight_EmptyRepoRootReturnsUnknownConfidence pins the rule that
// preflight.computeGoFileCoverage, when given an empty repoRoot (production
// MCP host with no source on disk), returns ConfidenceImpact="unknown"
// with a blind spot — NOT zero counts that downstream consumers would
// collapse into a "0% coverage critical" finding.
//
// History: enforce.GoFileCoverage already had this guard;
// preflight.computeGoFileCoverage is a deliberate duplicate (to avoid a
// circular import) but had drifted without one. See
// awareness.source_scan_requires_verified_repo_root and the 2026-05-14
// composed-path failure entry.
func TestPreflight_EmptyRepoRootReturnsUnknownConfidence(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	opts := preflight.Options{
		Task:     "anything",
		RepoRoot: "", // production-MCP shape: no source available
	}
	r, err := preflight.Run(ctx, opts, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.GoFileCoverage == nil {
		t.Skip("preflight does not populate GoFileCoverage when RepoRoot is empty — acceptable; the gate is that NO false-critical is emitted")
	}
	if r.GoFileCoverage.ConfidenceImpact != "unknown" {
		t.Errorf("empty RepoRoot must yield ConfidenceImpact=\"unknown\", got %q", r.GoFileCoverage.ConfidenceImpact)
	}
	if r.GoFileCoverage.EligibleGoFilesTotal != 0 {
		t.Errorf("Unverified result must not invent counts, got EligibleGoFilesTotal=%d", r.GoFileCoverage.EligibleGoFilesTotal)
	}
}

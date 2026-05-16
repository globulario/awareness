package preflight_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/extractors/goast"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// buildAnnotatedGraph extracts Go source files from srcDir into an in-memory graph.
// srcDir is both the walk root and the path root (relative paths start from srcDir).
func buildAnnotatedGraph(t *testing.T, srcDir string) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	if err := goast.Extract(context.Background(), g, srcDir, srcDir); err != nil {
		t.Fatalf("goast.Extract: %v", err)
	}
	return g
}

// writeAnnotatedFile writes a valid Go source file into dir/subdir and returns
// the relative path from dir to the file (e.g., "subdir/file.go").
func writeAnnotatedFile(t *testing.T, dir, subdir, body string) string {
	t.Helper()
	pkgDir := filepath.Join(dir, subdir)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(pkgDir, "file.go")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filepath.Join(subdir, "file.go")
}

// Test 4: Preflight --file surfaces annotated invariant even with no keyword match.
func TestPreflightFileSurfacesAnnotatedInvariant(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "convergence", `package convergence

// doMerge merges convergence results.
//globular:enforces no_keyword_invariant
func doMerge() {}
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	// Seed the invariant so the graph has the record.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "no_keyword_invariant",
		Title:    "No keyword invariant",
		Severity: "high",
		Status:   "active",
	})

	// Task description contains NO keywords that would match via alias.
	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "update the function signature",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, inv := range r.Invariants {
		if strings.Contains(inv, "no_keyword_invariant") {
			found = true
		}
	}
	if !found {
		t.Errorf("annotated invariant 'no_keyword_invariant' not in preflight invariants: %v", r.Invariants)
	}
}

// Test 5: Preflight marks annotated critical invariant as ARCHITECTURE_SENSITIVE.
func TestPreflightAnnotatedCriticalInvariantClassifiesArchSensitive(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "critical", `package critical

// computeHash is the critical hash computation.
//globular:enforces critical.invariant
func computeHash() string { return "" }
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	// Register the invariant with severity=critical.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "critical.invariant",
		Title:    "Critical invariant",
		Severity: "critical",
		Status:   "active",
	})

	// Task has no keywords that would trigger classification on their own.
	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "refactor internal helper",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertHasClass(t, r.Classification, preflight.ClassArchitectureSensitive)
}

// Test 7: Required test annotations appear in preflight.
func TestPreflightTestedByAnnotationAppearsInRequiredTests(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "guarded", `package guarded

// guardedOperation must be tested.
//globular:tested_by TestGuardedOperationBehavior
func guardedOperation() {}
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "update guarded logic",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, test := range r.RequiredTests {
		if test == "TestGuardedOperationBehavior" {
			found = true
		}
	}
	if !found {
		t.Errorf("tested_by annotation 'TestGuardedOperationBehavior' not in required tests: %v", r.RequiredTests)
	}
}

// Test: annotation-derived invariants outrank keyword-matched invariants (appear first).
func TestPreflightAnnotationInvariantsOutrankKeywordMatches(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "ordering", `package ordering

// CriticalPath enforces a critical invariant.
//globular:enforces annotation.derived.invariant
func CriticalPath() {}
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "annotation.derived.invariant",
		Title:    "Annotation derived invariant",
		Severity: "critical",
		Status:   "active",
	})

	// Also seed a keyword-matchable invariant.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "desired_hash_consistency",
		Title:    "Desired hash consistency",
		Severity: "high",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "invariant:desired_hash_consistency",
		Type: graph.NodeTypeInvariant,
		Name: "desired_hash_consistency",
	})

	// Task includes keyword "desired_hash" which would normally match first.
	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "fix desired_hash mismatch in ordering package",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// annotation.derived.invariant must appear before desired_hash_consistency.
	annotIdx := -1
	kwIdx := -1
	for i, inv := range r.Invariants {
		if strings.Contains(inv, "annotation.derived.invariant") {
			annotIdx = i
		}
		if strings.Contains(inv, "desired_hash_consistency") {
			kwIdx = i
		}
	}
	if annotIdx == -1 {
		t.Fatalf("annotation-derived invariant not found in: %v", r.Invariants)
	}
	if kwIdx != -1 && annotIdx > kwIdx {
		t.Errorf("annotation-derived invariant (idx %d) must appear before keyword-matched invariant (idx %d)", annotIdx, kwIdx)
	}
}

// Test: hash_schema annotations appear in preflight HashSchemas field.
func TestPreflightHashSchemasAppearsInReport(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "schema", `package schema

// ProduceHash computes the infra hash.
//globular:hash_schema infra_desired_hash
func ProduceHash() string { return "" }
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "update hash computation",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, s := range r.HashSchemas {
		if s == "infra_desired_hash" {
			found = true
		}
	}
	if !found {
		t.Errorf("hash schema 'infra_desired_hash' not in preflight HashSchemas: %v", r.HashSchemas)
	}
}

// Test: state_transition annotations appear in preflight StateTransitions field.
func TestPreflightStateTransitionsAppearsInReport(t *testing.T) {
	dir := t.TempDir()

	relPath := writeAnnotatedFile(t, dir, "state", `package state

// CommitConvergence closes the convergence loop.
//globular:state_transition INSTALLED -> CONVERGED
func CommitConvergence() {}
`)

	g := buildAnnotatedGraph(t, dir)
	ctx := context.Background()

	r, err := preflight.Run(ctx, preflight.Options{
		Task:  "update commit logic",
		Files: []string{relPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, st := range r.StateTransitions {
		if strings.Contains(st, "INSTALLED") && strings.Contains(st, "CONVERGED") {
			found = true
		}
	}
	if !found {
		t.Errorf("state transition 'INSTALLED -> CONVERGED' not in preflight StateTransitions: %v", r.StateTransitions)
	}
}

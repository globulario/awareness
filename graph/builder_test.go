package graph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/graph"
)

func TestBuild_Empty(t *testing.T) {
	res, err := graph.Build(graph.BuildInput{
		ProjectName: "test-empty",
		ProjectKind: "generic",
	}, graph.BuildOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Graph == nil {
		t.Fatal("result.Graph is nil")
	}
	if res.Graph.SchemaVersion != graph.CurrentGraphSchemaVersion {
		t.Errorf("schema_version = %q, want %q", res.Graph.SchemaVersion, graph.CurrentGraphSchemaVersion)
	}
	if res.Graph.Project != "test-empty" {
		t.Errorf("project = %q, want %q", res.Graph.Project, "test-empty")
	}
	// Only the project node.
	if res.NodeCount != 1 {
		t.Errorf("node_count = %d, want 1", res.NodeCount)
	}
	if res.EdgeCount != 0 {
		t.Errorf("edge_count = %d, want 0", res.EdgeCount)
	}
}

func TestBuild_InvariantsAndFailureModes(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "invariants.yaml"), `
invariants:
  - id: state.determinism
    title: Deterministic State
    description: State transitions must be deterministic
    severity: critical
    tags: [state, determinism]
  - id: idempotency.required
    title: Idempotency Required
    description: All writes must be idempotent
    severity: error
    tags: [idempotency]
`)

	writeFile(t, filepath.Join(dir, "failure_modes.yaml"), `
failure_modes:
  - id: nondeterministic.replay
    title: Nondeterministic Replay
    description: Nondeterminism causes state divergence during replay
    symptoms:
      - divergent state after replay
    severity: critical
    tags: [state, determinism, replay]
`)

	res, err := graph.Build(graph.BuildInput{
		ProjectName:       "test-project",
		ProjectKind:       "application",
		InvariantPaths:    []string{filepath.Join(dir, "invariants.yaml")},
		FailureModePaths:  []string{filepath.Join(dir, "failure_modes.yaml")},
	}, graph.BuildOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.InvariantCount != 2 {
		t.Errorf("invariant_count = %d, want 2", res.InvariantCount)
	}
	if res.FailureModeCount != 1 {
		t.Errorf("failure_mode_count = %d, want 1", res.FailureModeCount)
	}

	// Total nodes: 1 project + 2 invariants + 1 failure_mode = 4
	if res.NodeCount != 4 {
		t.Errorf("node_count = %d, want 4", res.NodeCount)
	}

	// Verify project→invariant edges.
	projectID := "project:test-project"
	assertEdge(t, res.Graph.Edges, projectID, "invariant:state.determinism", "defines")
	assertEdge(t, res.Graph.Edges, projectID, "invariant:idempotency.required", "defines")
	assertEdge(t, res.Graph.Edges, projectID, "failure_mode:nondeterministic.replay", "knows_failure_mode")

	// Verify cross-link: failure_mode → related_to → invariant (state.determinism matches)
	assertEdge(t, res.Graph.Edges, "failure_mode:nondeterministic.replay", "invariant:state.determinism", "related_to")
}

func TestBuild_ForbiddenFixes(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "invariants.yaml"), `
invariants:
  - id: no.global.state
    title: No Global State
    description: Global mutable state is forbidden
    severity: critical
    tags: [state, concurrency]
`)

	writeFile(t, filepath.Join(dir, "forbidden_fixes.yaml"), `
forbidden_fixes:
  - id: no.global.var.mutex
    title: Do not add a global mutex
    description: Adding a global mutex to protect global state treats the symptom not the cause
    tags: [state, global]
`)

	res, err := graph.Build(graph.BuildInput{
		ProjectName:       "test-ff",
		InvariantPaths:    []string{filepath.Join(dir, "invariants.yaml")},
		ForbiddenFixPaths: []string{filepath.Join(dir, "forbidden_fixes.yaml")},
	}, graph.BuildOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ForbiddenFixCount != 1 {
		t.Errorf("forbidden_fix_count = %d, want 1", res.ForbiddenFixCount)
	}

	// forbidden_fix → protects → invariant (state matches)
	assertEdge(t, res.Graph.Edges, "forbidden_fix:no.global.var.mutex", "invariant:no.global.state", "protects")
}

func TestBuild_SourceFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "invariants.yaml"), `
invariants:
  - id: context.propagation
    title: Context Propagation
    description: context must be propagated
    severity: error
    tags: [context]
`)

	srcDir := filepath.Join(dir, "internal")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(srcDir, "handler.go"), `package internal`)
	writeFile(t, filepath.Join(srcDir, "context.go"), `package internal`)

	res, err := graph.Build(graph.BuildInput{
		ProjectName:    "test-src",
		ProjectRoot:    dir,
		InvariantPaths: []string{filepath.Join(dir, "invariants.yaml")},
		SourceRoots:    []string{srcDir},
	}, graph.BuildOptions{IncludeSourceFiles: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.SourceFileCount != 2 {
		t.Errorf("source_file_count = %d, want 2", res.SourceFileCount)
	}

	// Verify source_file nodes exist.
	found := 0
	for _, n := range res.Graph.Nodes {
		if n.Kind == "source_file" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("found %d source_file nodes, want 2", found)
	}
}

func TestBuild_SchemaVersion(t *testing.T) {
	res, err := graph.Build(graph.BuildInput{ProjectName: "x"}, graph.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Graph.SchemaVersion, "awareness.graph.") {
		t.Errorf("unexpected schema_version: %q", res.Graph.SchemaVersion)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertEdge(t *testing.T, edges []graph.Edge, from, to, kind string) {
	t.Helper()
	for _, e := range edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return
		}
	}
	t.Errorf("missing edge %s -[%s]-> %s", from, kind, to)
}

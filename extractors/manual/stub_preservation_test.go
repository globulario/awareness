package manual_test

// Pinning tests for the EnsureNode contract across stub-creating extractors.
// Documented in docs/awareness/composed_path_failures.md (lifecycle metadata
// loss, 2026-05-10): every extractor that references a node it doesn't own
// must use EnsureNode, not AddNode, so canonical loaders' metadata survives.
//
// If any of these tests ever fails, a stub-creator regressed back to
// AddNode. The bug class clobbers lifecycle hints (deprecated,
// intentional_gap), severity, and any other metadata the canonical loader
// owns — silently producing wrong trust verdicts on the joined path.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/docs"
	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

func openStubTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// seedCanonicalInvariant writes an invariant node WITH metadata, mirroring
// what the canonical invariant loader would produce. The stub-creating
// extractors under test must NOT clobber this.
func seedCanonicalInvariant(t *testing.T, g *graph.Graph, id string) {
	t.Helper()
	if err := g.AddNode(context.Background(), graph.Node{
		ID:      "invariant:" + id,
		Type:    graph.NodeTypeInvariant,
		Name:    id,
		Summary: "canonical invariant summary",
		Metadata: map[string]any{
			"severity": "critical",
			"status":   "active",
		},
	}); err != nil {
		t.Fatalf("seed canonical invariant %s: %v", id, err)
	}
}

func seedCanonicalForbiddenFix(t *testing.T, g *graph.Graph, id string) {
	t.Helper()
	if err := g.AddNode(context.Background(), graph.Node{
		ID:      "forbidden_fix:" + id,
		Type:    graph.NodeTypeForbiddenFix,
		Name:    id,
		Summary: "canonical forbidden_fix description",
		Metadata: map[string]any{
			"reason": "violates invariant X",
		},
	}); err != nil {
		t.Fatalf("seed canonical forbidden_fix %s: %v", id, err)
	}
}

// seedCanonicalSourceFile writes a source_file node as the goast extractor
// would (with path, language metadata).
func seedCanonicalSourceFile(t *testing.T, g *graph.Graph, path string) {
	t.Helper()
	if err := g.AddNode(context.Background(), graph.Node{
		ID:      "source_file:" + path,
		Type:    graph.NodeTypeSourceFile,
		Name:    path,
		Path:    path,
		Summary: "canonical goast-extracted file",
		Metadata: map[string]any{
			"language": "go",
		},
	}); err != nil {
		t.Fatalf("seed canonical source_file %s: %v", path, err)
	}
}

func seedCanonicalTest(t *testing.T, g *graph.Graph, name string) {
	t.Helper()
	if err := g.AddNode(context.Background(), graph.Node{
		ID:      "test:" + name,
		Type:    graph.NodeTypeTest,
		Name:    name,
		Summary: "canonical test (extracted by goast tests)",
		Metadata: map[string]any{
			"package": "x",
		},
	}); err != nil {
		t.Fatalf("seed canonical test %s: %v", name, err)
	}
}

// assertMetadataPreserved verifies the canonical metadata is intact AFTER
// the stub-creating loader has run. Any clobber breaks the trust verdict.
func assertMetadataPreserved(t *testing.T, g *graph.Graph, nodeID string, want map[string]any) {
	t.Helper()
	got, err := g.FindNode(context.Background(), nodeID)
	if err != nil || got == nil {
		t.Fatalf("FindNode %s: %v", nodeID, err)
	}
	for k, v := range want {
		if got.Metadata[k] != v {
			t.Errorf("node %s metadata[%q] = %v, want %v (full metadata=%+v)",
				nodeID, k, got.Metadata[k], v, got.Metadata)
		}
	}
	if got.Summary == "" || got.Summary[0] == '(' {
		t.Errorf("node %s summary = %q; canonical summary appears to have been clobbered with a stub",
			nodeID, got.Summary)
	}
}

// ─── design_patterns loader ───────────────────────────────────────────────

func writeDesignPatternsForStubTest(t *testing.T, dir string) string {
	t.Helper()
	body := `design_patterns:
  - id: pattern.test_stub_preservation
    title: Test Stub Preservation
    type: design_pattern
    summary: Verifies stub creators do not clobber canonical metadata.
    applies_to:
      - golang/canonical/file.go
    invariants:
      - canonical.invariant_under_test
    forbidden_fixes:
      - canonical_forbidden_fix
    required_tests:
      - TestCanonicalUnderTest
`
	path := filepath.Join(dir, "design_patterns.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDesignPatternsLoader_PreservesCanonicalMetadata(t *testing.T) {
	ctx := context.Background()
	g := openStubTestGraph(t)

	// Seed canonical content first.
	seedCanonicalInvariant(t, g, "canonical.invariant_under_test")
	seedCanonicalForbiddenFix(t, g, "canonical_forbidden_fix")
	seedCanonicalTest(t, g, "TestCanonicalUnderTest")
	seedCanonicalSourceFile(t, g, "golang/canonical/file.go")

	// Run the stub-creating loader against the same ids.
	dir := t.TempDir()
	path := writeDesignPatternsForStubTest(t, dir)
	if err := manual.LoadDesignPatterns(ctx, g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}

	// All four canonical nodes must retain their metadata + summary.
	assertMetadataPreserved(t, g, "invariant:canonical.invariant_under_test",
		map[string]any{"severity": "critical", "status": "active"})
	assertMetadataPreserved(t, g, "forbidden_fix:canonical_forbidden_fix",
		map[string]any{"reason": "violates invariant X"})
	assertMetadataPreserved(t, g, "test:TestCanonicalUnderTest",
		map[string]any{"package": "x"})
	assertMetadataPreserved(t, g, "source_file:golang/canonical/file.go",
		map[string]any{"language": "go"})
}

// ─── patterns loader ──────────────────────────────────────────────────────

func TestPatternsLoader_PreservesCanonicalInvariantMetadata(t *testing.T) {
	ctx := context.Background()
	g := openStubTestGraph(t)

	seedCanonicalInvariant(t, g, "canonical.patterns_invariant")

	body := `patterns:
  - id: pattern.refers_invariant
    title: Refers an existing invariant
    definition: A pattern that targets an already-loaded invariant.
    related_invariants:
      - canonical.patterns_invariant
`
	path := filepath.Join(t.TempDir(), "patterns.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manual.LoadPatterns(ctx, g, path); err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}

	assertMetadataPreserved(t, g, "invariant:canonical.patterns_invariant",
		map[string]any{"severity": "critical", "status": "active"})
}

// ─── docs extractor ───────────────────────────────────────────────────────

func TestDocsExtractor_FindOrSynthesize_PreservesCanonicalMetadata(t *testing.T) {
	ctx := context.Background()
	g := openStubTestGraph(t)

	// Canonical invariant + failure_mode that the docs front-matter will reference.
	seedCanonicalInvariant(t, g, "canonical.docs_invariant")
	if err := g.AddNode(ctx, graph.Node{
		ID:      "failure_mode:canonical.docs_failure",
		Type:    graph.NodeTypeFailureMode,
		Name:    "canonical.docs_failure",
		Summary: "canonical failure_mode summary",
		Metadata: map[string]any{
			"intentional_gap": true,
			"severity":        "high",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Write a docs file with front-matter that decision-edges into both ids.
	docsRoot := t.TempDir()
	doc := `---
id: dec.docs_stub_test
type: architecture_decision
summary: Decision referencing canonical ids.
status: active
invariants:
  - canonical.docs_invariant
failure_modes:
  - canonical.docs_failure
---

# Body

Body of the decision.
`
	docPath := filepath.Join(docsRoot, "decision.md")
	if err := os.WriteFile(docPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := docs.Extract(ctx, g, docsRoot); err != nil {
		t.Fatalf("docs.Extract: %v", err)
	}

	assertMetadataPreserved(t, g, "invariant:canonical.docs_invariant",
		map[string]any{"severity": "critical", "status": "active"})
	assertMetadataPreserved(t, g, "failure_mode:canonical.docs_failure",
		map[string]any{"intentional_gap": true, "severity": "high"})
}

// ─── source_file regression: goast must not be clobbered by design_pattern ───

func TestSourceFile_NotClobberedByDesignPatternStub(t *testing.T) {
	ctx := context.Background()
	g := openStubTestGraph(t)

	// goast wrote the canonical source_file with its language + path.
	seedCanonicalSourceFile(t, g, "golang/canonical/svc/server.go")

	// design_patterns.yaml's applies_to references the same path.
	body := `design_patterns:
  - id: pattern.touches_canonical_file
    title: Pattern that touches a goast-owned file
    type: design_pattern
    summary: Should not clobber the source_file metadata.
    applies_to:
      - golang/canonical/svc/server.go
`
	path := filepath.Join(t.TempDir(), "design_patterns.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manual.LoadDesignPatterns(ctx, g, path); err != nil {
		t.Fatalf("LoadDesignPatterns: %v", err)
	}
	assertMetadataPreserved(t, g, "source_file:golang/canonical/svc/server.go",
		map[string]any{"language": "go"})
}

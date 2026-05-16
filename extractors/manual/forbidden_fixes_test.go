package manual_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

func TestLoadForbiddenFixesCreatesNodesAndForbidsEdges(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })

	dir := t.TempDir()
	path := filepath.Join(dir, "forbidden_fixes.yaml")
	src := `forbidden_fixes:
  - id: validate_globular_annotations_inside_test_fixtures
    summary: "x"
    related_invariants:
      - awareness.annotation_scanner.production_source_only
    required_tests:
      - TestValidateAnnotationsSkipsTestFiles
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	if err := manual.LoadForbiddenFixes(ctx, g, path); err != nil {
		t.Fatalf("LoadForbiddenFixes: %v", err)
	}

	n, err := g.FindNode(ctx, "forbidden_fix:validate_globular_annotations_inside_test_fixtures")
	if err != nil || n == nil {
		t.Fatalf("expected forbidden_fix node, got node=%v err=%v", n, err)
	}

	edges, err := g.EdgesByKind(ctx, graph.EdgeForbids)
	if err != nil {
		t.Fatalf("EdgesByKind forbids: %v", err)
	}
	foundForbid := false
	for _, e := range edges {
		if e.Src == "invariant:awareness.annotation_scanner.production_source_only" &&
			e.Dst == "forbidden_fix:validate_globular_annotations_inside_test_fixtures" {
			foundForbid = true
			break
		}
	}
	if !foundForbid {
		t.Fatalf("expected invariant -> forbids -> forbidden_fix edge")
	}

	testEdges, err := g.EdgesByKind(ctx, graph.EdgeTestedBy)
	if err != nil {
		t.Fatalf("EdgesByKind tested_by: %v", err)
	}
	foundTestLink := false
	for _, e := range testEdges {
		if e.Src == "forbidden_fix:validate_globular_annotations_inside_test_fixtures" &&
			e.Dst == "test:TestValidateAnnotationsSkipsTestFiles" {
			foundTestLink = true
			break
		}
	}
	if !foundTestLink {
		t.Fatalf("expected forbidden_fix -> tested_by -> TestValidateAnnotationsSkipsTestFiles edge")
	}
}

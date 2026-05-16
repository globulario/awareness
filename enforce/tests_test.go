package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

// Test 8: tested_by pointing to existing test node → no findings.
func TestValidateRequiredTestsPresent(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.DoWork", Type: graph.NodeTypeSymbol, Name: "DoWork"})
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestDoWorkBehavior", Type: graph.NodeTypeTest, Name: "TestDoWorkBehavior", Path: "pkg/work_test.go"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.DoWork", Kind: graph.EdgeTestedBy, Dst: "test:TestDoWorkBehavior"})

	findings := enforce.ValidateRequiredTests(ctx, g)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got: %v", findings)
	}
}

// Test 9: tested_by pointing to non-existent test node → ERROR.
func TestValidateRequiredTestsMissing(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.DoWork", Type: graph.NodeTypeSymbol, Name: "DoWork"})
	// Add the edge but NOT the test node.
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.DoWork", Kind: graph.EdgeTestedBy, Dst: "test:TestDoWorkBehavior"})

	findings := enforce.ValidateRequiredTests(ctx, g)
	found := false
	for _, f := range findings {
		if f.Code == "REQUIRED_TEST_MISSING" && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected REQUIRED_TEST_MISSING ERROR, got: %v", findings)
	}
}

func TestValidateRequiredTestsMissingPathDeduplicatesByTestName(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.A", Type: graph.NodeTypeSymbol, Name: "A"})
	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.B", Type: graph.NodeTypeSymbol, Name: "B"})
	// Test node exists but has empty path (the noisy case from awareness YAML fan-out).
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestShared", Type: graph.NodeTypeTest, Name: "TestShared", Path: ""})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.A", Kind: graph.EdgeTestedBy, Dst: "test:TestShared"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.B", Kind: graph.EdgeTestedBy, Dst: "test:TestShared"})

	findings := enforce.ValidateRequiredTests(ctx, g)
	count := 0
	for _, f := range findings {
		if f.Code == "REQUIRED_TEST_NO_PATH" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one REQUIRED_TEST_NO_PATH finding per test target, got %d: %+v", count, findings)
	}
}

func TestValidateRequiredTestsWithRepoResolvesMissingPathFromFilesystem(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	repoRoot := t.TempDir()
	testFile := filepath.Join(repoRoot, "pkg", "work_test.go")
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("package pkg\n\nimport \"testing\"\n\nfunc TestShared(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.A", Type: graph.NodeTypeSymbol, Name: "A"})
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestShared", Type: graph.NodeTypeTest, Name: "TestShared", Path: ""})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.A", Kind: graph.EdgeTestedBy, Dst: "test:TestShared"})

	findings := enforce.ValidateRequiredTestsWithRepo(ctx, g, repoRoot)
	for _, f := range findings {
		if f.Code == "REQUIRED_TEST_NO_PATH" {
			t.Fatalf("expected no REQUIRED_TEST_NO_PATH when test exists on disk, got: %+v", findings)
		}
	}
}

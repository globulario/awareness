package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

func TestAnnotationCoverageReportsHighRiskFileWithoutAnnotations(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	src := filepath.Join(repo, "golang")
	if err := os.MkdirAll(filepath.Join(src, "cluster_controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(src, "cluster_controller", "reconcile_runtime.go")
	if err := os.WriteFile(file, []byte("package cluster_controller\nfunc Reconcile() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	watch := filepath.Join(repo, "watch.yaml")
	if err := os.WriteFile(watch, []byte("files:\n  - golang/cluster_controller/reconcile_runtime.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g, _ := graph.OpenMemory()
	t.Cleanup(func() { _ = g.Close() })
	result := enforce.AnnotationCoverage(ctx, g, enforce.AnnotationCoverageOptions{
		RepoRoot:      repo,
		SrcDir:        src,
		WatchlistPath: watch,
		DocsDir:       filepath.Join(repo, "docs", "awareness"),
	})
	found := false
	for _, f := range result.Findings {
		if f.Code == "HIGH_RISK_FILE_NO_ANNOTATIONS" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected HIGH_RISK_FILE_NO_ANNOTATIONS, got %+v", result.Findings)
	}
}

func TestAnnotationCoverageReportsCriticalInvariantWithoutEnforcer(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	g, _ := graph.OpenMemory()
	t.Cleanup(func() { _ = g.Close() })
	_ = g.UpsertInvariant(ctx, graph.Invariant{ID: "infra.critical", Title: "x", Severity: "critical", Status: "active"})

	result := enforce.AnnotationCoverage(ctx, g, enforce.AnnotationCoverageOptions{
		RepoRoot: repo,
		SrcDir:   filepath.Join(repo, "golang"),
		DocsDir:  filepath.Join(repo, "docs", "awareness"),
	})
	found := false
	for _, f := range result.Findings {
		if f.Code == "CRITICAL_INVARIANT_NO_ENFORCER" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected CRITICAL_INVARIANT_NO_ENFORCER, got %+v", result.Findings)
	}
}

func TestAnnotationCoverageReportsHashSchemaWithoutTestedBy(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	g, _ := graph.OpenMemory()
	t.Cleanup(func() { _ = g.Close() })

	_ = g.AddNode(ctx, graph.Node{ID: "symbol:pkg.Producer", Type: graph.NodeTypeSymbol, Name: "Producer"})
	_ = g.AddNode(ctx, graph.Node{ID: "hash_schema:infra_hash", Type: graph.NodeTypeHashSchema, Name: "infra_hash"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:pkg.Producer", Kind: graph.EdgeProduces, Dst: "hash_schema:infra_hash"})

	result := enforce.AnnotationCoverage(ctx, g, enforce.AnnotationCoverageOptions{
		RepoRoot: repo,
		SrcDir:   filepath.Join(repo, "golang"),
		DocsDir:  filepath.Join(repo, "docs", "awareness"),
	})
	found := false
	for _, f := range result.Findings {
		if f.Code == "HASH_SCHEMA_WITHOUT_TEST" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected HASH_SCHEMA_WITHOUT_TEST, got %+v", result.Findings)
	}
}

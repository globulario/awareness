package preflight_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

func openCollectorHealthGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// TestCollectorHealth_OKWhenAllCollectorsSucceed verifies that preflight reports
// no collector warnings when all collectors succeed.
func TestCollectorHealth_OKWhenAllCollectorsSucceed(t *testing.T) {
	g := openCollectorHealthGraph(t)
	ctx := context.Background()

	buildID := "test-build-ok"
	_ = g.UpsertBuildRecord(ctx, buildID, "/repo", "abc123", "v1.0.0", graph.BuildStats{})
	_ = g.SetBuildCollectorHealth(ctx, buildID, []graph.CollectorHealthItem{
		{CollectorID: "go_ast", Status: "ok"},
		{CollectorID: "etcd", Status: "ok"},
		{CollectorID: "services", Status: "ok"},
	})

	opts := preflight.Options{DocsDir: t.TempDir()}
	r, _ := preflight.Run(ctx, opts, g)
	if r == nil {
		t.Fatal("Run returned nil report")
	}

	for _, w := range r.Warnings {
		if len(w) >= 16 && w[:16] == "COLLECTOR_ERROR:" {
			t.Errorf("unexpected collector error warning when all OK: %s", w)
		}
	}
}

// TestCollectorHealth_DegradedWhenP0CollectorFails verifies that preflight
// surfaces a warning when a collector reports status=error.
func TestCollectorHealth_DegradedWhenP0CollectorFails(t *testing.T) {
	g := openCollectorHealthGraph(t)
	ctx := context.Background()

	buildID := "test-build-err"
	_ = g.UpsertBuildRecord(ctx, buildID, "/repo", "abc123", "v1.0.0", graph.BuildStats{})
	_ = g.SetBuildCollectorHealth(ctx, buildID, []graph.CollectorHealthItem{
		{CollectorID: "etcd", Status: "error", Error: "connection refused: etcd:2379"},
	})

	opts := preflight.Options{DocsDir: t.TempDir()}
	r, _ := preflight.Run(ctx, opts, g)
	if r == nil {
		t.Fatal("Run returned nil report")
	}

	found := false
	for _, w := range r.Warnings {
		if len(w) >= 16 && w[:16] == "COLLECTOR_ERROR:" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected COLLECTOR_ERROR warning for status=error collector; warnings: %v", r.Warnings)
	}
}

// TestCollectorHealth_SkippedWhenRepoMissing verifies that a skipped collector
// (no factory configured) produces a blind spot, not an error warning.
func TestCollectorHealth_SkippedWhenRepoMissing(t *testing.T) {
	g := openCollectorHealthGraph(t)
	ctx := context.Background()

	buildID := "test-build-skip"
	_ = g.UpsertBuildRecord(ctx, buildID, "/repo", "abc123", "v1.0.0", graph.BuildStats{})
	_ = g.SetBuildCollectorHealth(ctx, buildID, []graph.CollectorHealthItem{
		{CollectorID: "packages", Status: "skipped", Error: "no packages directory"},
	})

	opts := preflight.Options{DocsDir: t.TempDir()}
	r, _ := preflight.Run(ctx, opts, g)
	if r == nil {
		t.Fatal("Run returned nil report")
	}

	// Skipped should not produce a COLLECTOR_ERROR warning.
	for _, w := range r.Warnings {
		if len(w) >= 16 && w[:16] == "COLLECTOR_ERROR:" {
			t.Errorf("skipped collector should not produce COLLECTOR_ERROR warning: %s", w)
		}
	}
}

// TestPreflightOutput_IncludesCollectorHealth verifies that the preflight Report
// includes a CollectorHealth field when collector data is available.
func TestPreflightOutput_IncludesCollectorHealth(t *testing.T) {
	g := openCollectorHealthGraph(t)
	ctx := context.Background()

	buildID := "test-build-ch"
	_ = g.UpsertBuildRecord(ctx, buildID, "/repo", "abc123", "v1.0.0", graph.BuildStats{})
	_ = g.SetBuildCollectorHealth(ctx, buildID, []graph.CollectorHealthItem{
		{CollectorID: "go_ast", Status: "ok"},
		{CollectorID: "etcd", Status: "error", Error: "timeout"},
	})

	opts := preflight.Options{DocsDir: t.TempDir()}
	r, _ := preflight.Run(ctx, opts, g)
	if r == nil {
		t.Fatal("Run returned nil report")
	}

	if len(r.CollectorHealth) == 0 {
		t.Error("expected non-empty CollectorHealth in preflight Report when build record has collector data")
	}
}

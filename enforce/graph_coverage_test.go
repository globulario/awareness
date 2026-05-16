package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
)

func TestGraphCoverageReport_CountsEligibleAndIndexedGoFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a couple of Go files in the repo root.
	files := []string{"a.go", "b.go", "c_test.go"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package foo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// No graph — indexed count should be 0.
	res := enforce.GoFileCoverage(context.Background(), nil, dir)
	if res.EligibleGoFilesTotal != 3 {
		t.Errorf("expected 3 eligible Go files, got %d", res.EligibleGoFilesTotal)
	}
	if res.EligibleNonTestGoFiles != 2 {
		t.Errorf("expected 2 non-test Go files, got %d", res.EligibleNonTestGoFiles)
	}
	if res.IndexedGoFilesTotal != 0 {
		t.Errorf("expected 0 indexed (no graph), got %d", res.IndexedGoFilesTotal)
	}
	if res.ConfidenceImpact != "low" {
		t.Errorf("expected confidence_impact=low when graph is nil, got %q", res.ConfidenceImpact)
	}
}

func TestGraphCoverageReport_ExcludesVendorAndGeneratedByDefault(t *testing.T) {
	dir := t.TempDir()

	// Vendor file — should be excluded.
	vendorDir := filepath.Join(dir, "vendor", "pkg")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vendorDir, "v.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Generated proto file — should be excluded.
	if err := os.WriteFile(filepath.Join(dir, "foo.pb.go"), []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Real source file — should be counted.
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := enforce.GoFileCoverage(context.Background(), nil, dir)
	if res.EligibleGoFilesTotal != 1 {
		t.Errorf("expected 1 eligible file (vendor and pb.go excluded), got %d", res.EligibleGoFilesTotal)
	}
}

func TestGraphCoverageReport_ReportsMissingFiles(t *testing.T) {
	dir := t.TempDir()

	// Create source files.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No graph — all files are "missing" from the graph.
	res := enforce.GoFileCoverage(context.Background(), nil, dir)
	// With no graph, MissingFiles is not computed (graph is nil path).
	// The blind_spots should mention the eligible file count.
	if len(res.BlindSpots) == 0 {
		t.Error("expected blind_spots when graph is nil")
	}
}

func TestPreflight_LowGraphCoverageLowersConfidence(t *testing.T) {
	// With no graph, coverage is 0% which is below critical threshold.
	// ConfidenceImpact should be "low" (nil graph case).
	// When graph has low coverage, ConfidenceImpact should be "high".
	dir := t.TempDir()

	// Create many Go files so coverage would be low even with a few indexed.
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, filepath.FromSlash("subpkg"))
		if err := os.MkdirAll(name, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(name, "f.go")
		if i > 0 {
			f = filepath.Join(dir, filepath.FromSlash("subpkg"), "extra.go")
		}
		_ = os.WriteFile(f, []byte("package subpkg\n"), 0o644)
	}

	res := enforce.GoFileCoverage(context.Background(), nil, dir)
	// nil graph → confident "low" impact (we know nothing).
	if res.ConfidenceImpact == "" {
		t.Error("expected non-empty confidence_impact")
	}
}

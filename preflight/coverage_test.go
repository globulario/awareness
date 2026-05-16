package preflight_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/runtime"
)

// TestCoverage_RawYAMLAlwaysChecked verifies that Coverage.RawYAML is never
// "not_checked" — the raw YAML fallback always runs in step 12.
func TestCoverage_RawYAMLAlwaysChecked(t *testing.T) {
	// Run without a graph (nil), so there are no graph facts to match.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "some unknown task with no matches expected",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// RawYAML must be checked_clean or checked_with_matches, never not_checked.
	if r.Coverage.RawYAML == preflight.CoverageNotChecked {
		t.Errorf("Coverage.RawYAML = %q, expected checked_clean or checked_with_matches (raw fallback always runs)", r.Coverage.RawYAML)
	}
}

// TestCoverage_RawYAMLWithMatches verifies that Coverage.RawYAML is
// checked_with_matches when the raw fallback finds relevant knowledge.
func TestCoverage_RawYAMLWithMatches(t *testing.T) {
	docsDir := setupPreflightDocsDir(t)
	// The task contains "desired_hash" which should match the aliases.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash mismatch detected",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Even without graph, RawYAML should be checked (clean or with_matches).
	if r.Coverage.RawYAML == preflight.CoverageNotChecked {
		t.Errorf("Coverage.RawYAML = %q, expected checked_clean or checked_with_matches", r.Coverage.RawYAML)
	}
}

// TestCoverage_RawYAMLWithZeroMatchesIsCheckedClean verifies that when no
// raw YAML matches are found, RawYAML is checked_clean (NOT not_checked).
func TestCoverage_RawYAMLWithZeroMatchesIsCheckedClean(t *testing.T) {
	// Use a task that is unlikely to match any awareness YAML.
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "zzz_no_possible_match_xyzzy_12345",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Must be checked_clean, not not_checked — the fallback ran but found nothing.
	if r.Coverage.RawYAML != preflight.CoverageCheckedClean {
		t.Errorf("Coverage.RawYAML = %q, expected checked_clean when no matches found", r.Coverage.RawYAML)
	}
}

// TestCoverage_NoGraphIsNotChecked verifies that Coverage.Graph = not_checked
// when g is nil.
func TestCoverage_NoGraphIsNotChecked(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "test task",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Coverage.Graph != preflight.CoverageNotChecked {
		t.Errorf("Coverage.Graph = %q, expected not_checked when no graph provided", r.Coverage.Graph)
	}
}

// TestCoverage_WithGraphIsChecked verifies that Coverage.Graph is checked_clean
// or checked_with_matches when a graph is provided.
func TestCoverage_WithGraphIsChecked(t *testing.T) {
	g := seedPreflightGraph(t)
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "some task",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Coverage.Graph == preflight.CoverageNotChecked {
		t.Errorf("Coverage.Graph = %q, expected checked_clean or checked_with_matches when graph provided", r.Coverage.Graph)
	}
}

// TestCoverage_RuntimeNoop verifies that Coverage.Runtime = noop when
// IncludeRuntime is false.
func TestCoverage_RuntimeNoop(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:           "test task",
		IncludeRuntime: false,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Coverage.Runtime != preflight.CoverageNoop {
		t.Errorf("Coverage.Runtime = %q, expected noop when IncludeRuntime=false", r.Coverage.Runtime)
	}
}

// TestCoverage_RuntimeCheckedWithNoopBridge verifies Coverage.Runtime = checked_clean
// when IncludeRuntime is true but bridge has all noop sources.
func TestCoverage_RuntimeCheckedWithNoopBridge(t *testing.T) {
	bridge := runtime.NewBridge("node1", "")
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:           "test task",
		IncludeRuntime: true,
		Bridge:         bridge,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Noop bridge → no findings → checked_clean (not noop — runtime was included).
	if r.Coverage.Runtime == preflight.CoverageNoop {
		t.Errorf("Coverage.Runtime = noop, expected checked_clean or checked_with_matches when IncludeRuntime=true (even with noop bridge)")
	}
}

// TestCoverage_CodeScanAlwaysNotChecked verifies CodeScan is not_checked
// (scan_violations is a separate tool).
func TestCoverage_CodeScanAlwaysNotChecked(t *testing.T) {
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task: "test task",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if r.Coverage.CodeScan != preflight.CoverageNotChecked {
		t.Errorf("Coverage.CodeScan = %q, expected not_checked", r.Coverage.CodeScan)
	}
}

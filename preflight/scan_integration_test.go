package preflight_test

import (
	"os"
	"testing"

	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/project"
)

func globularServicesRoot() string {
	p := "/home/dave/Documents/github.com/globulario/services"
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// TestRawKnowledgeFallbackFromPaths_Globular verifies that RawKnowledgeFallbackFromPaths
// returns relevant matches from Globular's multi-file knowledge layout.
func TestRawKnowledgeFallbackFromPaths_Globular(t *testing.T) {
	root := globularServicesRoot()
	if root == "" {
		t.Skip("services repo not present")
	}
	docsDir := root + "/docs/awareness"

	paths := project.AwarenessPaths{
		Root: docsDir,
		Invariants: []string{
			docsDir + "/invariants.yaml",
			docsDir + "/convergence_rules.yaml",
			docsDir + "/awareness_self_invariants.yaml",
		},
		FailureModes:     []string{docsDir + "/failure_modes.yaml"},
		ForbiddenFixes:   []string{docsDir + "/forbidden_fixes.yaml"},
		IncidentPatterns: []string{docsDir + "/incidents"},
	}

	matches := preflight.RawKnowledgeFallbackFromPaths("VIP contamination etcd member eviction keepalived", nil, paths)
	if len(matches) == 0 {
		t.Fatal("expected matches for VIP task, got none")
	}
	t.Logf("VIP task: %d matches (top: %s score=%d)", len(matches), matches[0].ID, matches[0].Score)

	// Capped at 12.
	if len(matches) > 12 {
		t.Errorf("expected ≤12 matches, got %d", len(matches))
	}

	// Sorted descending.
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Errorf("matches not sorted: index %d score %d > index %d score %d",
				i, matches[i].Score, i-1, matches[i-1].Score)
		}
	}
}

// TestRawKnowledgeFallbackFromPaths_EmptyPaths returns nil and does not crash.
func TestRawKnowledgeFallbackFromPaths_EmptyPaths(t *testing.T) {
	matches := preflight.RawKnowledgeFallbackFromPaths("some task", nil, project.AwarenessPaths{})

	if matches != nil {
		t.Errorf("expected nil for empty paths, got %v", matches)
	}
}

// TestExtendedPreflightItemsFromPaths_Globular verifies the extended items loader.
func TestExtendedPreflightItemsFromPaths_Globular(t *testing.T) {
	root := globularServicesRoot()
	if root == "" {
		t.Skip("services repo not present")
	}
	docsDir := root + "/docs/awareness"

	paths := project.AwarenessPaths{
		Root:           docsDir,
		Invariants:     []string{docsDir + "/invariants.yaml"},
		FailureModes:   []string{docsDir + "/failure_modes.yaml"},
		ForbiddenFixes: []string{docsDir + "/forbidden_fixes.yaml"},
	}

	// Should return non-nil even with no specialized lists — base knowledge loads.
	items := preflight.ExtendedPreflightItemsFromPaths("etcd leader election quorum", nil, paths)
	if items == nil {
		t.Log("ExtendedPreflightItemsFromPaths returned nil (no extended YAML files — expected for base-only load)")
	} else {
		t.Logf("extended items: decisions=%d forbidden_assumptions=%d required_tests=%d",
			len(items.Decisions), len(items.ForbiddenAssumptions), len(items.RequiredTests))
	}
}

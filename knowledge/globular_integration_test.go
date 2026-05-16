package knowledge

import (
	"os"
	"testing"
)

// globularServicesRoot returns the path to the services repo, or "" if absent.
func globularServicesRoot() string {
	p := "/home/dave/Documents/github.com/globulario/services"
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// TestLoadFromPaths_GlobularYAML loads the real Globular knowledge YAML files
// and verifies that invariants, failure modes, and forbidden fixes are parsed
// without data loss. This test is skipped when the services repo is absent.
func TestLoadFromPaths_GlobularYAML(t *testing.T) {
	root := globularServicesRoot()
	if root == "" {
		t.Skip("services repo not present — skipping Globular integration test")
	}

	docsDir := root + "/docs/awareness"

	base, err := LoadFromPaths(
		[]string{
			docsDir + "/invariants.yaml",
			docsDir + "/convergence_rules.yaml",
			docsDir + "/awareness_self_invariants.yaml",
		},
		[]string{docsDir + "/failure_modes.yaml"},
		[]string{docsDir + "/forbidden_fixes.yaml"},
		[]string{
			docsDir + "/failuregraph_seeds",
			docsDir + "/incidents",
		},
	)
	if err != nil {
		t.Fatalf("LoadFromPaths: %v", err)
	}

	// Invariants: expect at least 70
	if len(base.Invariants) < 70 {
		t.Errorf("invariants: got %d, want ≥70", len(base.Invariants))
	}
	// Every invariant must have an id and at least one of title/summary
	for _, inv := range base.Invariants {
		if inv.ID == "" {
			t.Errorf("invariant with empty id: %+v", inv)
		}
		if inv.Title == "" && inv.Summary == "" {
			t.Errorf("invariant %q: both title and summary are empty", inv.ID)
		}
	}

	// Failure modes: expect at least 40
	if len(base.FailureModes) < 40 {
		t.Errorf("failure_modes: got %d, want ≥40", len(base.FailureModes))
	}
	for _, fm := range base.FailureModes {
		if fm.ID == "" {
			t.Errorf("failure_mode with empty id")
		}
		// Must have at least one of the name fields
		if fm.Title == "" && fm.Summary == "" {
			t.Errorf("failure_mode %q: both title and summary are empty", fm.ID)
		}
	}

	// Forbidden fixes: expect at least 100
	if len(base.ForbiddenFixes) < 100 {
		t.Errorf("forbidden_fixes: got %d, want ≥100", len(base.ForbiddenFixes))
	}
	for _, ff := range base.ForbiddenFixes {
		if ff.ID == "" {
			t.Errorf("forbidden_fix with empty id")
		}
		if ff.Summary == "" {
			t.Errorf("forbidden_fix %q: empty summary", ff.ID)
		}
	}

	t.Logf("loaded: %d invariants, %d failure_modes, %d forbidden_fixes, %d incident_patterns",
		len(base.Invariants), len(base.FailureModes), len(base.ForbiddenFixes), len(base.IncidentPatterns))
}

// TestSearch_GlobularKnowledge verifies that Search returns relevant results
// from Globular knowledge for a Globular-specific task.
func TestSearch_GlobularKnowledge(t *testing.T) {
	root := globularServicesRoot()
	if root == "" {
		t.Skip("services repo not present — skipping Globular integration test")
	}
	docsDir := root + "/docs/awareness"

	base, err := LoadFromPaths(
		[]string{docsDir + "/invariants.yaml"},
		[]string{docsDir + "/failure_modes.yaml"},
		[]string{docsDir + "/forbidden_fixes.yaml"},
		nil,
	)
	if err != nil {
		t.Fatalf("LoadFromPaths: %v", err)
	}

	// Task that should match VIP-related knowledge
	matches := Search(base, "VIP contamination etcd member eviction keepalived", nil)
	if len(matches) == 0 {
		t.Error("Search returned no matches for VIP task against Globular knowledge")
	}
	t.Logf("VIP task: %d matches", len(matches))
	for _, m := range matches {
		t.Logf("  [%s] %s (%s) score=%d", m.Kind, m.ID, m.Severity, m.Score)
	}

	// Task that should match retry/reconcile knowledge
	matches2 := Search(base, "blind reconcile retry transient failure", nil)
	if len(matches2) == 0 {
		t.Error("Search returned no matches for retry task")
	}
	t.Logf("retry task: %d matches", len(matches2))
}

// TestLoadFromPaths_DirectoryExpansion verifies that directory entries in
// incidentPatterns are expanded to individual YAML files.
func TestLoadFromPaths_DirectoryExpansion(t *testing.T) {
	root := globularServicesRoot()
	if root == "" {
		t.Skip("services repo not present")
	}
	seedsDir := root + "/docs/awareness/failuregraph_seeds"

	base, err := LoadFromPaths(nil, nil, nil, []string{seedsDir})
	if err != nil {
		t.Fatalf("LoadFromPaths (dir): %v", err)
	}
	// The seeds use ErrorCategory format — incident_patterns wrapper may be absent
	// so count is 0 until we add a seeds-specific loader. For now verify no crash.
	t.Logf("seeds dir expanded: %d incident_patterns loaded", len(base.IncidentPatterns))
}

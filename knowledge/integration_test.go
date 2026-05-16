package knowledge_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/globulario/awareness/knowledge"
)

// repoRoot returns the absolute path to the awareness repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..")
}

// TestIntegration_LoadAwarenessDir loads the real .awareness/ directory and
// validates it. This catches broken cross-references and empty IDs early.
func TestIntegration_LoadAwarenessDir(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, ".awareness")

	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load(.awareness): %v", err)
	}

	if len(base.Invariants) == 0 {
		t.Error("expected at least one invariant")
	}
	if len(base.FailureModes) == 0 {
		t.Error("expected at least one failure mode")
	}
	if len(base.ForbiddenFixes) == 0 {
		t.Error("expected at least one forbidden fix")
	}
	if len(base.IncidentPatterns) == 0 {
		t.Error("expected at least one incident pattern")
	}

	t.Logf("loaded: %d invariants, %d failure_modes, %d forbidden_fixes, %d incident_patterns",
		len(base.Invariants), len(base.FailureModes), len(base.ForbiddenFixes), len(base.IncidentPatterns))

	errs := knowledge.Validate(base)
	for _, e := range errs {
		t.Errorf("validation: %v", e)
	}
}

// TestIntegration_SearchRealBase confirms that real knowledge matches real queries.
func TestIntegration_SearchRealBase(t *testing.T) {
	root := repoRoot(t)
	base, err := knowledge.Load(filepath.Join(root, ".awareness"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name    string
		task    string
		files   []string
		wantID  string
	}{
		{
			name:   "vip endpoint query",
			task:   "update Scylla host list to use PrimaryIP",
			wantID: "globular.vip_used_as_member_endpoint",
		},
		{
			name:   "import wall query",
			task:   "add dependency on github.com/globulario/services package",
			wantID: "services.import.leak",
		},
		{
			name:   "workflow receipt query",
			task:   "workflow step dispatches install action then crashes on restart",
			wantID: "globular.workflow_resume_without_receipt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches := knowledge.Search(base, tc.task, tc.files)
			if len(matches) == 0 {
				t.Fatalf("no matches for %q", tc.task)
			}
			found := false
			for _, m := range matches {
				if m.ID == tc.wantID {
					found = true
					t.Logf("found %s (score=%d, terms=%v)", m.ID, m.Score, m.MatchedTerms)
				}
			}
			if !found {
				t.Errorf("expected %q in matches; got: %v", tc.wantID, ids(matches))
			}
		})
	}
}

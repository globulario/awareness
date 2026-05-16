package fixledger_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/fixledger"
)

// guardrailsMDPath returns the path to golang/fix/guardrails.md.
func guardrailsMDPath(t *testing.T) string {
	t.Helper()
	// Walk up from the package directory to find the golang dir.
	dir := "."
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve abs path: %v", err)
	}
	// Go up until we find golang/fix/guardrails.md.
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(abs, "fix", "guardrails.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("guardrails.md not found; skipping test")
	return ""
}

// ---- 1. TestMarkdownFixCaseIngested ----

func TestMarkdownFixCaseIngested(t *testing.T) {
	path := guardrailsMDPath(t)
	sections, err := fixledger.ParseMarkdownFixCases(path)
	if err != nil {
		t.Fatalf("ParseMarkdownFixCases: %v", err)
	}
	if len(sections) == 0 {
		t.Fatal("expected at least one parsed section from guardrails.md")
	}
	// Find a section with a non-empty title and status.
	found := false
	for _, s := range sections {
		if s.Title != "" && s.Status != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one section with non-empty title and status")
	}
}

// ---- 2. TestDoneStatusParsed ----

func TestDoneStatusParsed(t *testing.T) {
	path := guardrailsMDPath(t)
	sections, err := fixledger.ParseMarkdownFixCases(path)
	if err != nil {
		t.Fatalf("ParseMarkdownFixCases: %v", err)
	}
	// Guardrail 2 (JOIN SCRIPT) has STATUS: COMPLETE — should parse as FixDone.
	found := false
	for _, s := range sections {
		if s.Status == fixledger.FixDone {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one section with DONE (COMPLETE) status in guardrails.md")
	}
}

// ---- 3. TestPartialStatusExposeGaps ----

func TestPartialStatusExposeGaps(t *testing.T) {
	cases := []fixledger.FixCase{
		{
			ID:             "partial_case",
			Title:          "Partial fix",
			Status:         fixledger.FixPartial,
			RemainingFiles: []string{"golang/foo.go", "golang/bar.go"},
		},
		{
			ID:     "done_case",
			Title:  "Done fix",
			Status: fixledger.FixDone,
		},
	}
	partials := fixledger.ListPartials(cases)
	if len(partials) != 1 {
		t.Fatalf("expected 1 partial, got %d", len(partials))
	}
	if len(partials[0].RemainingFiles) == 0 {
		t.Error("partial fix case should have remaining files")
	}
}

// ---- 4. TestDidWeFixFindsMatchFromTask ----

func TestDidWeFixFindsMatchFromTask(t *testing.T) {
	cases := []fixledger.FixCase{
		{
			ID:      "desired_hash_consistency",
			Title:   "Fix desired hash consistency",
			Status:  fixledger.FixPartial,
			Pattern: "desired hash",
			TargetInvariants: []string{
				"infra.desired_hash_consistency",
			},
		},
	}
	result := fixledger.DidWeFix("investigate desired hash mismatch causing restart storm", cases, nil)
	if result == nil {
		t.Fatal("DidWeFix returned nil")
	}
	if len(result.MatchedFixCases) == 0 {
		t.Error("expected at least one matched fix case for 'desired hash'")
	}
}

// ---- 5. TestCoverageReportInvariantWithNoTests ----

func TestCoverageReportInvariantWithNoTests(t *testing.T) {
	cases := []fixledger.FixCase{
		{
			ID:               "some_fix",
			Title:            "Some fix",
			Status:           fixledger.FixDone,
			TargetInvariants: []string{"convergence.no_infinite_retry"},
			RequiredTests:    nil, // no tests
		},
	}
	invariants := []string{"convergence.no_infinite_retry", "infra.desired_hash_consistency"}
	report := fixledger.CoverageReport(cases, invariants)

	if report == nil {
		t.Fatal("CoverageReport returned nil")
	}
	// invariant with no fixes should still appear with nil/empty slice.
	_, ok := report["infra.desired_hash_consistency"]
	if !ok {
		t.Error("expected infra.desired_hash_consistency to appear in coverage report with no fixes")
	}
	// invariant with a fix but no required_tests.
	fixesForRetry := report["convergence.no_infinite_retry"]
	if len(fixesForRetry) == 0 {
		t.Error("expected fix case mapped to convergence.no_infinite_retry")
	}
	if len(fixesForRetry[0].RequiredTests) != 0 {
		t.Error("expected fix case to have zero required tests")
	}
}

// ---- 6. TestListPartials ----

func TestListPartials(t *testing.T) {
	cases := []fixledger.FixCase{
		{ID: "a", Status: fixledger.FixDone},
		{ID: "b", Status: fixledger.FixPartial},
		{ID: "c", Status: fixledger.FixInProgress},
		{ID: "d", Status: fixledger.FixPartial},
	}
	partials := fixledger.ListPartials(cases)
	if len(partials) != 2 {
		t.Errorf("expected 2 partials, got %d", len(partials))
	}
	for _, p := range partials {
		if p.Status != fixledger.FixPartial {
			t.Errorf("expected PARTIAL status, got %s", p.Status)
		}
	}
}

// ---- 7. TestListRegressions ----

func TestListRegressions(t *testing.T) {
	cases := []fixledger.FixCase{
		{ID: "a", Status: fixledger.FixDone},
		{ID: "b", Status: fixledger.FixRegressed},
		{ID: "c", Status: fixledger.FixPartial},
	}
	regressions := fixledger.ListRegressions(cases)
	if len(regressions) != 1 {
		t.Errorf("expected 1 regression, got %d", len(regressions))
	}
	if regressions[0].Status != fixledger.FixRegressed {
		t.Errorf("expected REGRESSED status, got %s", regressions[0].Status)
	}
}

// ---- 8. TestDuplicateFixCasesSameInvariant ----

func TestDuplicateFixCasesSameInvariant(t *testing.T) {
	cases := []fixledger.FixCase{
		{
			ID:               "fix_alpha",
			Title:            "Fix alpha",
			Status:           fixledger.FixDone,
			Pattern:          "alpha",
			TargetInvariants: []string{"convergence.no_infinite_retry"},
		},
		{
			ID:               "fix_beta",
			Title:            "Fix beta",
			Status:           fixledger.FixPartial,
			Pattern:          "beta",
			TargetInvariants: []string{"convergence.no_infinite_retry"},
		},
	}
	// PatternStatus should return both if we search for an invariant match.
	result := fixledger.PatternStatus("alpha", cases)
	if len(result) == 0 {
		t.Fatal("expected at least one match for 'alpha'")
	}

	// CoverageReport should return both for the shared invariant.
	report := fixledger.CoverageReport(cases, []string{"convergence.no_infinite_retry"})
	if len(report["convergence.no_infinite_retry"]) != 2 {
		t.Errorf("expected 2 fix cases for convergence.no_infinite_retry, got %d",
			len(report["convergence.no_infinite_retry"]))
	}
}

// ---- status tracker loading tests ----

func TestLoadFixCasesEmptyOnMissing(t *testing.T) {
	cases, err := fixledger.LoadFixCases("/nonexistent/path/fix_cases.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cases != nil {
		t.Errorf("expected nil slice for missing file, got %v", cases)
	}
}

func TestLoadGuardrailsEmptyOnMissing(t *testing.T) {
	guardrails, err := fixledger.LoadGuardrails("/nonexistent/path/guardrails.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if guardrails != nil {
		t.Errorf("expected nil slice for missing file, got %v", guardrails)
	}
}

// ---- in-memory fix_cases.yaml loading ----

func TestLoadFixCasesFromYAML(t *testing.T) {
	dir := t.TempDir()
	content := `fix_cases:
  - id: test_fix
    title: Test fix case
    status: DONE
    pattern: "test pattern"
    target_invariants:
      - convergence.no_infinite_retry
    required_tests:
      - TestSomething
`
	path := filepath.Join(dir, "fix_cases.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fix_cases.yaml: %v", err)
	}

	cases, err := fixledger.LoadFixCases(path)
	if err != nil {
		t.Fatalf("LoadFixCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 fix case, got %d", len(cases))
	}
	if cases[0].ID != "test_fix" {
		t.Errorf("expected id=test_fix, got %q", cases[0].ID)
	}
	if cases[0].Status != fixledger.FixDone {
		t.Errorf("expected status=DONE, got %q", cases[0].Status)
	}
}

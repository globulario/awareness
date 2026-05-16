package enforce_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
)

func TestScaffoldScan_DetectsTodoSkips(t *testing.T) {
	dir := t.TempDir()

	// Write a test file with scaffold TODO skips.
	content := `package foo_test

import "testing"

func TestRealTest(t *testing.T) {
	// real assertion
}

func TestScaffoldOne(t *testing.T) {
	t.Skip("TODO: implement required awareness test")
}

func TestScaffoldTwo(t *testing.T) {
	t.Skip("TODO")
}
`
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := enforce.ScanScaffoldTests(dir, "")
	if res.TotalScaffoldSkips != 2 {
		t.Errorf("expected 2 scaffold skips, got %d", res.TotalScaffoldSkips)
	}
	// Findings should include SCAFFOLD_TODO_SKIP warnings.
	found := 0
	for _, f := range res.Findings {
		if f.Code == enforce.CodeScaffoldTodoSkip {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 SCAFFOLD_TODO_SKIP findings, got %d", found)
	}
}

func TestScaffoldScan_ExcludesVendor(t *testing.T) {
	dir := t.TempDir()

	// Vendor directory should be excluded.
	vendorDir := filepath.Join(dir, "vendor", "pkg")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package foo_test

import "testing"

func TestVendorScaffold(t *testing.T) {
	t.Skip("TODO")
}
`
	if err := os.WriteFile(filepath.Join(vendorDir, "foo_test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := enforce.ScanScaffoldTests(dir, "")
	if res.TotalScaffoldSkips != 0 {
		t.Errorf("expected 0 scaffold skips (vendor excluded), got %d", res.TotalScaffoldSkips)
	}
}

func TestGraphIntegrity_DoneFixCaseWithTodoSkipFails(t *testing.T) {
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a fix_cases.yaml with one DONE case referencing a scaffold test.
	fixCases := `fix_cases:
  - id: fix.test_scaffold_done
    title: Test done with scaffold
    status: DONE
    required_tests:
      - TestDoneScaffoldRequired
`
	if err := os.WriteFile(filepath.Join(docsDir, "fix_cases.yaml"), []byte(fixCases), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a test file where the required test is a scaffold skip.
	testContent := `package foo_test

import "testing"

func TestDoneScaffoldRequired(t *testing.T) {
	t.Skip("TODO: implement required awareness test")
}
`
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(testContent), 0o644); err != nil {
		t.Fatal(err)
	}

	res := enforce.ScanScaffoldTests(dir, docsDir)
	if res.DoneFixcasesWithScaffoldOnly != 1 {
		t.Errorf("expected 1 DONE fix case with scaffold only, got %d", res.DoneFixcasesWithScaffoldOnly)
	}

	errorFound := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeDoneFixcaseScaffoldOnly {
			errorFound = true
			if f.Severity != enforce.SeverityError {
				t.Errorf("expected ERROR severity for %s, got %s", enforce.CodeDoneFixcaseScaffoldOnly, f.Severity)
			}
		}
	}
	if !errorFound {
		t.Error("expected DONE_FIXCASE_SCAFFOLD_ONLY error finding")
	}
}

func TestAuditCmd_MaxTodoScaffoldSkipsZeroFails(t *testing.T) {
	// Verify that ScanScaffoldTests surfaces scaffold findings that would
	// trigger the --max-todo-scaffold-skips=0 gate.
	dir := t.TempDir()
	content := `package foo_test

import "testing"

func TestSomeScaffold(t *testing.T) {
	t.Skip("TODO")
}
`
	if err := os.WriteFile(filepath.Join(dir, "s_test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res := enforce.ScanScaffoldTests(dir, "")
	if res.TotalScaffoldSkips == 0 {
		t.Fatal("expected at least 1 scaffold skip for threshold gate test")
	}
	// There must be a SCAFFOLD_TODO_SKIP finding that the CLI threshold check would count.
	count := 0
	for _, f := range res.Findings {
		if f.Code == enforce.CodeScaffoldTodoSkip {
			count++
		}
	}
	if count == 0 {
		t.Error("expected SCAFFOLD_TODO_SKIP findings for CLI threshold gate")
	}
}

// TestScaffoldScan_EmptyRepoRootReturnsUnverified pins that scanning with
// no repoRoot yields Unverified=true with a reason — NOT a silent zero
// result that downstream callers would read as "no scaffolds found." See
// awareness.source_scan_requires_verified_repo_root and the 2026-05-14
// composed-path failure entry.
func TestScaffoldScan_EmptyRepoRootReturnsUnverified(t *testing.T) {
	res := enforce.ScanScaffoldTests("", "")
	if !res.Unverified {
		t.Error("empty repoRoot must set Unverified=true")
	}
	if res.UnverifiedReason == "" {
		t.Error("Unverified result must carry a non-empty UnverifiedReason")
	}
	if res.TotalScaffoldSkips != 0 {
		t.Errorf("Unverified result must not invent counts, got TotalScaffoldSkips=%d", res.TotalScaffoldSkips)
	}
}

package integrity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TestIssue describes a problem with a required test reference in a fix case.
type TestIssue struct {
	FixCaseID   string `json:"fix_case_id"`
	TestName    string `json:"test_name"`
	Severity    string `json:"severity"` // "critical" | "warning" | "metadata_only"
	Issue       string `json:"issue"`
	Recommended string `json:"recommended,omitempty"`
}

// CITestResults carries optional test execution evidence for upgrading severity.
type CITestResults struct {
	Passed       bool     `json:"passed"`
	FailedTests  []string `json:"failed_tests"`
	SkippedTests []string `json:"skipped_tests"`
}

// CheckTestReferences verifies that required test functions exist on disk.
// For each DONE fix case, every test in RequiredTests is looked up in *_test.go
// files under repoRoot.
//
// Severity rules:
//   - critical:      function missing from all test files AND no CI evidence of existence
//   - warning:       function found in CI results but no disk scan possible (no repoRoot)
//   - metadata_only: CI results unavailable and no repoRoot to scan
func CheckTestReferences(fixCases []FixCase, repoRoot string, ci *CITestResults) []TestIssue {
	var issues []TestIssue

	// Build CI evidence sets.
	ciPassed := map[string]bool{}
	ciFailed := map[string]bool{}
	ciSkipped := map[string]bool{}
	hasCIResults := ci != nil
	if hasCIResults {
		for _, t := range ci.FailedTests {
			ciFailed[t] = true
		}
		for _, t := range ci.SkippedTests {
			ciSkipped[t] = true
		}
		if ci.Passed {
			// When the suite passed and a test isn't in the failed/skipped list,
			// we treat it as having passed.
		}
	}
	_ = ciPassed

	// Scan test files if we have a repo root.
	foundInSource := map[string]string{} // funcName → file path
	if repoRoot != "" {
		_ = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			content := string(data)
			rel, _ := filepath.Rel(repoRoot, path)
			for _, fc := range fixCases {
				for _, testName := range fc.RequiredTests {
					name := normalizeTestName(testName)
					if strings.Contains(content, "func "+name+"(") {
						foundInSource[name] = rel
					}
				}
			}
			return nil
		})
	}

	for _, fc := range fixCases {
		if strings.ToUpper(fc.Status) != "DONE" {
			continue
		}
		for _, testEntry := range fc.RequiredTests {
			name := normalizeTestName(testEntry)
			if name == "" || !isValidTestFuncName(name) {
				issues = append(issues, TestIssue{
					FixCaseID: fc.ID,
					TestName:  testEntry,
					Severity:  "warning",
					Issue:     fmt.Sprintf("REQUIRED_TEST_INVALID_NAME: %q is not a valid Go test function name (must start with TestXxx)", testEntry),
				})
				continue
			}

			sourcePath, foundOnDisk := foundInSource[name]

			switch {
			case foundOnDisk:
				if ci != nil && ciFailed[name] {
					issues = append(issues, TestIssue{
						FixCaseID:   fc.ID,
						TestName:    name,
						Severity:    "critical",
						Issue:       fmt.Sprintf("REQUIRED_TEST_FAILED: %q exists at %s but failed in CI", name, sourcePath),
						Recommended: "Fix the failing test before marking the fix case DONE",
					})
				} else if ci != nil && ciSkipped[name] {
					issues = append(issues, TestIssue{
						FixCaseID:   fc.ID,
						TestName:    name,
						Severity:    "critical",
						Issue:       fmt.Sprintf("REQUIRED_TEST_SKIPPED: %q exists at %s but is skipped in CI — skipped tests do not prove behavior", name, sourcePath),
						Recommended: "Remove t.Skip() or provide a skip reason that explains when it will be un-skipped",
					})
				}
				// Found on disk and not failed/skipped: PASS (no issue).
			case repoRoot != "":
				// We scanned the repo and didn't find the function.
				issues = append(issues, TestIssue{
					FixCaseID:   fc.ID,
					TestName:    name,
					Severity:    "critical",
					Issue:       fmt.Sprintf("REQUIRED_TEST_MISSING: %q not found in any *_test.go file under %s", name, repoRoot),
					Recommended: fmt.Sprintf("Add func %s(...) to a *_test.go file in the relevant package", name),
				})
			case hasCIResults:
				// CI results available but no repo scan — use CI evidence.
				if ciFailed[name] {
					issues = append(issues, TestIssue{
						FixCaseID:   fc.ID,
						TestName:    name,
						Severity:    "critical",
						Issue:       fmt.Sprintf("REQUIRED_TEST_FAILED: %q failed in CI", name),
						Recommended: "Fix the failing test",
					})
				} else if ciSkipped[name] {
					issues = append(issues, TestIssue{
						FixCaseID:   fc.ID,
						TestName:    name,
						Severity:    "critical",
						Issue:       fmt.Sprintf("REQUIRED_TEST_SKIPPED: %q is skipped in CI results", name),
					})
				} else {
					// Function is in CI results but we don't know the source path.
					issues = append(issues, TestIssue{
						FixCaseID:   fc.ID,
						TestName:    name,
						Severity:    "warning",
						Issue:       fmt.Sprintf("REQUIRED_TEST_NO_PATH: %q exists in CI results but source file path is unknown — rebuild graph with test source path extractor", name),
						Recommended: "Run 'globular awareness build' with source path extraction enabled",
					})
				}
			default:
				// No repo root and no CI results.
				issues = append(issues, TestIssue{
					FixCaseID:   fc.ID,
					TestName:    name,
					Severity:    "metadata_only",
					Issue:       fmt.Sprintf("REQUIRED_TEST_UNVERIFIED: %q cannot be verified — no repo root or CI results available", name),
				})
			}
		}
	}

	return issues
}

// normalizeTestName strips annotation suffixes like "TestFoo (not objectstore)".
func normalizeTestName(entry string) string {
	if idx := strings.IndexByte(entry, ' '); idx >= 0 {
		return strings.TrimSpace(entry[:idx])
	}
	return strings.TrimSpace(entry)
}

// isValidTestFuncName returns true for names like TestFoo, TestBar_Baz.
func isValidTestFuncName(name string) bool {
	return len(name) >= 5 && strings.HasPrefix(name, "Test") && name[4] >= 'A' && name[4] <= 'Z'
}

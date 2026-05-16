package selfcheck

import (
	"strings"
	"testing"

	"github.com/globulario/awareness/enforce"
)

func TestSummarizeWarningGroups(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "a"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "b"},
		{Code: "INVARIANT_NO_ENFORCER", Severity: enforce.SeverityInfo, Message: "c"},
		{Code: "MISSING_FORBIDDEN_FIX", Severity: enforce.SeverityWarning, Message: "d"},
	}

	got := summarizeWarningGroups(findings, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 summary lines, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "REQUIRED_TEST_NO_PATH") {
		t.Fatalf("expected top group REQUIRED_TEST_NO_PATH first, got: %s", got[0])
	}
	if !strings.Contains(got[0], "(2)") {
		t.Fatalf("expected count 2 in first group, got: %s", got[0])
	}
}

func TestMaxWarningGroupCount(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "A", Severity: enforce.SeverityWarning, Message: "1"},
		{Code: "A", Severity: enforce.SeverityWarning, Message: "2"},
		{Code: "B", Severity: enforce.SeverityWarning, Message: "3"},
		{Code: "I", Severity: enforce.SeverityInfo, Message: "4"},
	}
	if got := maxWarningGroupCount(findings); got != 2 {
		t.Fatalf("maxWarningGroupCount=%d, want 2", got)
	}
}

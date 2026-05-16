package enforce_test

import (
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/enforce"
)

// Test 1: Warnings are grouped by finding code.
func TestGroupFindingsByCode(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "a"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "b"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "c"},
		{Code: enforce.CodeHashSchemaNoConsumer, Severity: enforce.SeverityWarning, Message: "d"},
	}

	groups := enforce.GroupFindings(findings)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// The larger group should come first (REQUIRED_TEST_NO_PATH with 3).
	if groups[0].Code != "REQUIRED_TEST_NO_PATH" {
		t.Errorf("expected REQUIRED_TEST_NO_PATH first (highest count), got %s", groups[0].Code)
	}
	if groups[0].Count != 3 {
		t.Errorf("expected count=3, got %d", groups[0].Count)
	}
}

// Test 2: Required test warnings are grouped; rendered output does NOT contain 1000 individual lines.
func TestGroupedRenderDoesNotFloodOutput(t *testing.T) {
	// Build 200 REQUIRED_TEST_NO_PATH warnings.
	var findings []enforce.Finding
	for i := 0; i < 200; i++ {
		findings = append(findings, enforce.Finding{
			Code:     "REQUIRED_TEST_NO_PATH",
			Severity: enforce.SeverityWarning,
			Message:  strings.Repeat("test has no path ", 3),
		})
	}

	// Triage with no suppressions.
	result := enforce.Triage(
		&enforce.AuditResult{Findings: findings, WarningCount: len(findings)},
		&enforce.SuppressionFile{},
		time.Now(),
	)

	rendered := enforce.RenderTriagedMarkdown(result, enforce.RenderOptions{})

	// Count lines — should be far fewer than 200.
	lineCount := strings.Count(rendered, "\n")
	if lineCount >= 200 {
		t.Errorf("expected grouped output to have fewer than 200 lines, got %d", lineCount)
	}
	// Should still mention the group and count.
	if !strings.Contains(rendered, "REQUIRED_TEST_NO_PATH") {
		t.Error("expected grouped output to mention REQUIRED_TEST_NO_PATH")
	}
	if !strings.Contains(rendered, "200") {
		t.Error("expected grouped output to mention count 200")
	}
}

// Test 8: Triage summary includes suppressed_count.
func TestTriageSummaryIncludesSuppressedCount(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "m1"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "m2"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.test",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "backlog",
		Owner:       "dave",
		CreatedAt:   "2026-05-01",
		ExpiresAt:   "2026-06-01",
	}}}

	result := enforce.Triage(
		&enforce.AuditResult{Findings: findings, WarningCount: 2},
		sf,
		refNow,
	)

	if result.SuppressedCount != 2 {
		t.Errorf("expected SuppressedCount=2, got %d", result.SuppressedCount)
	}
	if result.WarningCount != 0 {
		t.Errorf("expected WarningCount=0 after suppression, got %d", result.WarningCount)
	}

	rendered := enforce.RenderTriagedMarkdown(result, enforce.RenderOptions{})
	if !strings.Contains(rendered, "Suppressed") {
		t.Error("expected rendered output to mention Suppressed")
	}
	if !strings.Contains(rendered, "2") {
		t.Error("expected rendered output to mention count 2")
	}
}

// Test 9: FailsWarningThreshold returns true when unsuppressed warnings exceed threshold.
func TestFailsWarningThreshold(t *testing.T) {
	r := &enforce.TriagedResult{
		AuditResult: &enforce.AuditResult{WarningCount: 5},
	}

	if !enforce.FailsWarningThreshold(r, 4) {
		t.Error("expected threshold=4 to fail when warnings=5")
	}
	if enforce.FailsWarningThreshold(r, 5) {
		t.Error("expected threshold=5 to pass when warnings=5")
	}
	if enforce.FailsWarningThreshold(r, -1) {
		t.Error("expected threshold=-1 (disabled) to always pass")
	}
}

// Test 10: --warning-threshold: FailsWarningThreshold is false at threshold, true above.
func TestWarningThresholdBoundary(t *testing.T) {
	r := &enforce.TriagedResult{
		AuditResult: &enforce.AuditResult{WarningCount: 0},
	}
	if enforce.FailsWarningThreshold(r, 0) {
		t.Error("expected threshold=0 to pass when warnings=0")
	}

	r2 := &enforce.TriagedResult{
		AuditResult: &enforce.AuditResult{WarningCount: 1},
	}
	if !enforce.FailsWarningThreshold(r2, 0) {
		t.Error("expected threshold=0 to fail when warnings=1")
	}
}

// Test 11: --show-suppressed includes suppressed finding detail in output.
func TestShowSuppressedIncludesDetail(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "test X has no path"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.test",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "backlog",
		Owner:       "dave",
		CreatedAt:   "2026-05-01",
		ExpiresAt:   "2026-06-01",
	}}}

	result := enforce.Triage(
		&enforce.AuditResult{Findings: findings, WarningCount: 1},
		sf,
		refNow,
	)

	// Without ShowSuppressed: the detail section is absent.
	withoutDetail := enforce.RenderTriagedMarkdown(result, enforce.RenderOptions{ShowSuppressed: false})
	if strings.Contains(withoutDetail, "Suppressed finding detail") {
		t.Error("expected detail section to be absent when ShowSuppressed=false")
	}

	// With ShowSuppressed: the detail section (and the raw message) is present.
	withDetail := enforce.RenderTriagedMarkdown(result, enforce.RenderOptions{ShowSuppressed: true})
	if !strings.Contains(withDetail, "Suppressed finding detail") {
		t.Error("expected detail section to appear when ShowSuppressed=true")
	}
	if !strings.Contains(withDetail, "test X has no path") {
		t.Error("expected individual finding message to appear when ShowSuppressed=true")
	}
}

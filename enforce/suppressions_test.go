package enforce_test

import (
	"testing"
	"time"

	"github.com/globulario/awareness/enforce"
)

// reference time used across suppression tests (2026-05-06)
var refNow = time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

// Test 3: A suppression hides a matching WARNING from the unsuppressed list.
func TestSuppressionHidesMatchingWarning(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "test has no path"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.test",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "backlog",
		Owner:       "dave",
		CreatedAt:   "2026-05-01",
		ExpiresAt:   "2026-06-01",
	}}}

	result := enforce.ApplySuppressions(findings, sf, refNow)

	if len(result.Unsuppressed) != 0 {
		t.Errorf("expected 0 unsuppressed, got %d", len(result.Unsuppressed))
	}
	if len(result.Suppressed) != 1 {
		t.Errorf("expected 1 suppressed, got %d", len(result.Suppressed))
	}
}

// Test 4: A suppression NEVER hides an ERROR finding.
func TestSuppressionDoesNotHideError(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityError, Message: "this is actually an error"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.all",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "backlog",
		Owner:       "dave",
		CreatedAt:   "2026-05-01",
		ExpiresAt:   "2026-06-01",
	}}}

	result := enforce.ApplySuppressions(findings, sf, refNow)

	if len(result.Unsuppressed) != 1 {
		t.Errorf("expected ERROR to remain unsuppressed, got unsuppressed=%d suppressed=%d",
			len(result.Unsuppressed), len(result.Suppressed))
	}
}

// Test 5: An expired suppression is ignored — the finding stays unsuppressed.
func TestExpiredSuppressionIsIgnored(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "test has no path"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.expired",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "backlog",
		Owner:       "dave",
		CreatedAt:   "2026-01-01",
		ExpiresAt:   "2026-04-30", // before refNow
	}}}

	result := enforce.ApplySuppressions(findings, sf, refNow)

	if len(result.Unsuppressed) != 1 {
		t.Errorf("expected expired suppression to leave finding unsuppressed, got unsuppressed=%d", len(result.Unsuppressed))
	}
	if len(result.Expired) != 1 {
		t.Errorf("expected 1 expired suppression, got %d", len(result.Expired))
	}
}

// Test 6: A suppression with missing required fields is invalid and ignored.
func TestInvalidSuppressionMissingFields(t *testing.T) {
	cases := []struct {
		name string
		sup  enforce.Suppression
	}{
		{"missing_reason", enforce.Suppression{ID: "s1", FindingCode: "X", Owner: "dave", CreatedAt: "2026-05-01", ExpiresAt: "2026-06-01"}},
		{"missing_owner", enforce.Suppression{ID: "s2", FindingCode: "X", Reason: "r", CreatedAt: "2026-05-01", ExpiresAt: "2026-06-01"}},
		{"missing_expires_at", enforce.Suppression{ID: "s3", FindingCode: "X", Reason: "r", Owner: "dave", CreatedAt: "2026-05-01"}},
	}
	findings := []enforce.Finding{
		{Code: "X", Severity: enforce.SeverityWarning, Message: "a warning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{tc.sup}}
			result := enforce.ApplySuppressions(findings, sf, refNow)
			if len(result.Invalid) != 1 {
				t.Errorf("expected 1 invalid suppression, got %d", len(result.Invalid))
			}
			if len(result.Unsuppressed) != 1 {
				t.Errorf("invalid suppression should leave finding unsuppressed, got %d unsuppressed", len(result.Unsuppressed))
			}
		})
	}
}

// Test 7: When matched count exceeds max_count, a MaxCountViolation is reported.
func TestMaxCountViolationReported(t *testing.T) {
	findings := []enforce.Finding{
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "a"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "b"},
		{Code: "REQUIRED_TEST_NO_PATH", Severity: enforce.SeverityWarning, Message: "c"},
	}
	sf := &enforce.SuppressionFile{Suppressions: []enforce.Suppression{{
		ID:          "suppress.tight",
		FindingCode: "REQUIRED_TEST_NO_PATH",
		Reason:      "tight budget",
		Owner:       "dave",
		CreatedAt:   "2026-05-01",
		ExpiresAt:   "2026-06-01",
		MaxCount:    2, // only 2 allowed, 3 present
	}}}

	result := enforce.ApplySuppressions(findings, sf, refNow)

	// The suppression caps at max_count=2: first 2 suppressed, 3rd escapes to unsuppressed.
	if len(result.Suppressed) != 2 {
		t.Errorf("expected 2 suppressed (capped at max_count), got %d", len(result.Suppressed))
	}
	if len(result.Unsuppressed) != 1 {
		t.Errorf("expected 1 unsuppressed (over max_count), got %d", len(result.Unsuppressed))
	}
	if len(result.MaxCountViolations) != 1 {
		t.Errorf("expected 1 max_count violation, got %d", len(result.MaxCountViolations))
	}
	v := result.MaxCountViolations[0]
	if v.MaxCount != 2 || v.ActualCount != 3 {
		t.Errorf("expected max=2 actual=3, got max=%d actual=%d", v.MaxCount, v.ActualCount)
	}
}

// Test: ValidateSuppression rejects each missing field individually.
func TestValidateSuppressionFields(t *testing.T) {
	base := enforce.Suppression{
		ID:        "s",
		FindingCode: "X",
		Reason:    "r",
		Owner:     "o",
		CreatedAt: "2026-01-01",
		ExpiresAt: "2026-12-31",
	}
	if err := enforce.ValidateSuppression(base); err != nil {
		t.Fatalf("valid suppression failed: %v", err)
	}

	for _, tc := range []struct {
		field string
		s     enforce.Suppression
	}{
		{"id", enforce.Suppression{FindingCode: "X", Reason: "r", Owner: "o", CreatedAt: "2026-01-01", ExpiresAt: "2026-12-31"}},
		{"reason", enforce.Suppression{ID: "s", FindingCode: "X", Owner: "o", CreatedAt: "2026-01-01", ExpiresAt: "2026-12-31"}},
		{"owner", enforce.Suppression{ID: "s", FindingCode: "X", Reason: "r", CreatedAt: "2026-01-01", ExpiresAt: "2026-12-31"}},
		{"created_at", enforce.Suppression{ID: "s", FindingCode: "X", Reason: "r", Owner: "o", ExpiresAt: "2026-12-31"}},
		{"expires_at", enforce.Suppression{ID: "s", FindingCode: "X", Reason: "r", Owner: "o", CreatedAt: "2026-01-01"}},
	} {
		t.Run("missing_"+tc.field, func(t *testing.T) {
			if err := enforce.ValidateSuppression(tc.s); err == nil {
				t.Errorf("expected error for missing %s field", tc.field)
			}
		})
	}
}

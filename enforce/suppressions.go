package enforce

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// SuppressionFile is the parsed content of audit_suppressions.yaml.
type SuppressionFile struct {
	Suppressions []Suppression `yaml:"suppressions"`
}

// Suppression defines a named, time-bounded, owner-attributed rule that
// moves matching WARNING/INFO findings from "unsuppressed" to "suppressed"
// in audit output. ERRORs are never suppressible.
type Suppression struct {
	ID            string          `yaml:"id"`
	FindingCode   string          `yaml:"finding_code"` // exact code or "*"
	Scope         SuppressionScope `yaml:"scope"`
	Reason        string          `yaml:"reason"`
	Owner         string          `yaml:"owner"`
	CreatedAt     string          `yaml:"created_at"` // YYYY-MM-DD
	ExpiresAt     string          `yaml:"expires_at"` // YYYY-MM-DD
	MaxCount      int             `yaml:"max_count"`  // 0 = unlimited
	SeverityFloor string          `yaml:"severity_floor"` // informational, must be "warning"
}

// SuppressionScope restricts which findings a suppression applies to.
// Each field is a glob pattern or "*" (matches anything).
type SuppressionScope struct {
	Invariant string `yaml:"invariant"`
	Test      string `yaml:"test"`
	File      string `yaml:"file"`
	Symbol    string `yaml:"symbol"`
}

// LoadSuppressions reads a suppression YAML file.
// Returns an empty set if path is "" or the file does not exist.
func LoadSuppressions(path string) (*SuppressionFile, error) {
	if path == "" {
		return &SuppressionFile{}, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &SuppressionFile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read suppressions %s: %w", path, err)
	}
	var sf SuppressionFile
	if err := yaml.Unmarshal(b, &sf); err != nil {
		return nil, fmt.Errorf("parse suppressions %s: %w", path, err)
	}
	return &sf, nil
}

// ValidateSuppression checks that a suppression has all required fields.
func ValidateSuppression(s Suppression) error {
	if s.ID == "" {
		return fmt.Errorf("suppression missing id")
	}
	if s.Reason == "" {
		return fmt.Errorf("suppression %s: reason is required", s.ID)
	}
	if s.Owner == "" {
		return fmt.Errorf("suppression %s: owner is required", s.ID)
	}
	if s.CreatedAt == "" {
		return fmt.Errorf("suppression %s: created_at is required", s.ID)
	}
	if s.ExpiresAt == "" {
		return fmt.Errorf("suppression %s: expires_at is required", s.ID)
	}
	if _, err := time.Parse("2006-01-02", s.ExpiresAt); err != nil {
		return fmt.Errorf("suppression %s: expires_at must be YYYY-MM-DD, got %q", s.ID, s.ExpiresAt)
	}
	return nil
}

// SuppressionResult is the outcome of applying suppressions to a finding list.
type SuppressionResult struct {
	// Unsuppressed contains findings that were not matched by any active suppression.
	Unsuppressed []Finding

	// Suppressed contains non-ERROR findings matched by an active suppression.
	Suppressed []Finding

	// SuppressedBy is parallel to Suppressed: the suppression ID that matched each finding.
	SuppressedBy []string

	// Expired suppressions were valid but past their expiry date — they were ignored.
	Expired []Suppression

	// MaxCountViolations are suppressions where the matched count exceeded max_count.
	MaxCountViolations []MaxCountViolation

	// Invalid suppressions had missing required fields and were ignored.
	Invalid []InvalidSuppression
}

// MaxCountViolation describes a suppression that matched more findings than its max_count allows.
type MaxCountViolation struct {
	SuppressionID string
	MaxCount      int
	ActualCount   int
}

// InvalidSuppression describes a suppression that failed schema validation.
type InvalidSuppression struct {
	SuppressionID string
	Error         string
}

// ApplySuppressions partitions findings into suppressed and unsuppressed sets.
//
// Rules:
//   - ERROR findings are never suppressed (hard rule).
//   - Expired suppressions are ignored and reported in result.Expired.
//   - Invalid suppressions (missing required fields) are ignored and reported.
//   - If matched count exceeds max_count, the violation is reported but suppression still applies.
//
// now is the reference time for expiry checks — pass time.Now() in production.
func ApplySuppressions(findings []Finding, sf *SuppressionFile, now time.Time) SuppressionResult {
	if sf == nil || len(sf.Suppressions) == 0 {
		return SuppressionResult{Unsuppressed: findings}
	}

	type supMeta struct {
		sup     Suppression
		expired bool
		invalid string
	}

	var metas []supMeta
	var result SuppressionResult

	for _, s := range sf.Suppressions {
		if err := ValidateSuppression(s); err != nil {
			result.Invalid = append(result.Invalid, InvalidSuppression{
				SuppressionID: s.ID,
				Error:         err.Error(),
			})
			metas = append(metas, supMeta{sup: s, invalid: err.Error()})
			continue
		}
		exp, _ := time.Parse("2006-01-02", s.ExpiresAt)
		// Expire at end of the day (23:59:59) so a same-day suppression is still valid.
		exp = exp.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		if now.After(exp) {
			result.Expired = append(result.Expired, s)
			metas = append(metas, supMeta{sup: s, expired: true})
			continue
		}
		metas = append(metas, supMeta{sup: s})
	}

	matchCounts := map[string]int{}

	for _, f := range findings {
		// Rule 1: ERRORs are never suppressible.
		if f.Severity == SeverityError {
			result.Unsuppressed = append(result.Unsuppressed, f)
			continue
		}

		matched := ""
		for _, m := range metas {
			if m.expired || m.invalid != "" {
				continue
			}
			if suppressionMatches(m.sup, f) {
				matched = m.sup.ID
				matchCounts[m.sup.ID]++
				break
			}
		}

		if matched != "" {
			// max_count bounds how many findings can be suppressed by this rule.
			var sup *Suppression
			for i := range sf.Suppressions {
				if sf.Suppressions[i].ID == matched {
					sup = &sf.Suppressions[i]
					break
				}
			}
			if sup != nil && sup.MaxCount > 0 && matchCounts[matched] > sup.MaxCount {
				result.Unsuppressed = append(result.Unsuppressed, f)
				continue
			}
			result.Suppressed = append(result.Suppressed, f)
			result.SuppressedBy = append(result.SuppressedBy, matched)
		} else {
			result.Unsuppressed = append(result.Unsuppressed, f)
		}
	}

	// Report max_count violations (after all findings are processed).
	for _, m := range metas {
		if m.expired || m.invalid != "" {
			continue
		}
		if m.sup.MaxCount > 0 && matchCounts[m.sup.ID] > m.sup.MaxCount {
			result.MaxCountViolations = append(result.MaxCountViolations, MaxCountViolation{
				SuppressionID: m.sup.ID,
				MaxCount:      m.sup.MaxCount,
				ActualCount:   matchCounts[m.sup.ID],
			})
		}
	}

	return result
}

// suppressionMatches returns true when suppression s applies to finding f.
func suppressionMatches(s Suppression, f Finding) bool {
	// Code must match.
	if s.FindingCode != "" && s.FindingCode != "*" && s.FindingCode != f.Code {
		return false
	}
	// Scope: file.
	if !globMatch(s.Scope.File, f.File) {
		return false
	}
	// Scope: symbol.
	if !globMatch(s.Scope.Symbol, f.Symbol) {
		return false
	}
	return true
}

// globMatch returns true if pattern is empty/"*" or matches s via filepath.Match.
func globMatch(pattern, s string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	ok, _ := filepath.Match(pattern, s)
	return ok
}

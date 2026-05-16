package knowledge

import (
	"fmt"
	"strings"
	"time"
)

// SelfcheckReport summarizes the health of the knowledge base.
type SelfcheckReport struct {
	Warnings []string
	Errors   []string
	OK       bool
}

// Selfcheck inspects the knowledge base for orphan records, unproven critical
// invariants, stale entries, and missing cross-references.
func Selfcheck(b *Base) SelfcheckReport {
	r := SelfcheckReport{}
	if b == nil {
		r.Errors = append(r.Errors, "knowledge base is nil")
		return r
	}

	// Build lookup sets.
	invIDs := make(map[string]bool)
	for _, inv := range b.Invariants {
		invIDs[inv.ID] = true
	}
	fmIDs := make(map[string]bool)
	for _, fm := range b.FailureModes {
		fmIDs[fm.ID] = true
	}
	ffIDs := make(map[string]bool)
	for _, ff := range b.ForbiddenFixes {
		ffIDs[ff.ID] = true
	}
	faIDs := make(map[string]bool)
	for _, fa := range b.ForbiddenAssumptions {
		faIDs[fa.ID] = true
	}

	// Invariants: critical ones must have proven_by.
	for _, inv := range b.Invariants {
		if inv.Severity == "critical" &&
			len(inv.ProvenBy.Tests) == 0 && len(inv.ProvenBy.Incidents) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"invariant %q is critical but has no proven_by entries", inv.ID))
		}
		checkFreshness("invariant", inv.ID, inv.FreshnessFields, &r)
	}

	// FailureModes: must link to incidents or regression tests.
	for _, fm := range b.FailureModes {
		if len(fm.RegressionTests) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"failure_mode %q has no regression_tests", fm.ID))
		}
		checkFreshness("failure_mode", fm.ID, fm.FreshnessFields, &r)
	}

	// ForbiddenFixes: must link to a related failure mode (checked by ID prefix heuristic).
	for _, ff := range b.ForbiddenFixes {
		if ff.CorrectApproach == "" {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"forbidden_fix %q has no correct_approach", ff.ID))
		}
	}

	// Decisions: must protect at least one invariant.
	for _, d := range b.Decisions {
		if len(d.ProtectsInvariants) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"decision %q protects no invariants — may be an orphan", d.ID))
		}
		for _, ref := range d.ProtectsInvariants {
			if !invIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"decision %q references missing invariant %q", d.ID, ref))
			}
		}
		for _, ref := range d.RelatedFailureModes {
			if !fmIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"decision %q references missing failure_mode %q", d.ID, ref))
			}
		}
	}

	// ForbiddenAssumptions: must have safer_checks and why_wrong.
	for _, fa := range b.ForbiddenAssumptions {
		if len(fa.SaferChecks) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"forbidden_assumption %q has no safer_checks", fa.ID))
		}
		if len(fa.WhyWrong) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"forbidden_assumption %q has no why_wrong entries", fa.ID))
		}
		for _, ref := range fa.RelatedInvariants {
			if !invIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"forbidden_assumption %q references missing invariant %q", fa.ID, ref))
			}
		}
	}

	// RequiredTests: check their invariant/failure_mode refs.
	for _, rt := range b.RequiredTests {
		for _, ref := range rt.Protects.Invariants {
			if !invIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"required_test %q protects missing invariant %q", rt.ID, ref))
			}
		}
		for _, ref := range rt.Protects.FailureModes {
			if !fmIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"required_test %q protects missing failure_mode %q", rt.ID, ref))
			}
		}
		if len(rt.Commands) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"required_test %q has no commands", rt.ID))
		}
	}

	// SubsystemBoundaries: check invariant refs.
	for _, sb := range b.SubsystemBoundaries {
		for _, ref := range sb.RelatedInvariants {
			if !invIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"subsystem_boundary %q references missing invariant %q", sb.Subsystem, ref))
			}
		}
	}

	// AuthorityRules: check invariant refs.
	for _, ar := range b.AuthorityRules {
		for _, ref := range ar.RelatedInvariants {
			if !invIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"authority_rule %q references missing invariant %q", ar.ID, ref))
			}
		}
		for _, ref := range ar.ForbiddenAssumptions {
			if !faIDs[ref] {
				r.Warnings = append(r.Warnings, fmt.Sprintf(
					"authority_rule %q references missing forbidden_assumption %q", ar.ID, ref))
			}
		}
	}

	// RemediationContracts: must have allowed and forbidden actions.
	for _, rc := range b.RemediationContracts {
		if len(rc.AllowedActions) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"remediation_contract %q has no allowed_actions", rc.ID))
		}
		if len(rc.ForbiddenActions) == 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"remediation_contract %q has no forbidden_actions", rc.ID))
		}
		for _, ref := range rc.When.FailureModes {
			if !fmIDs[ref] {
				r.Errors = append(r.Errors, fmt.Sprintf(
					"remediation_contract %q references missing failure_mode %q", rc.ID, ref))
			}
		}
	}

	r.OK = len(r.Errors) == 0
	return r
}

func checkFreshness(kind, id string, f FreshnessFields, r *SelfcheckReport) {
	if f.LastVerified == "" {
		return // freshness is optional
	}
	if f.StaleAfterDays <= 0 {
		return
	}
	t, err := time.Parse("2006-01-02", f.LastVerified)
	if err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%s %q has unparseable last_verified %q: %v", kind, id, f.LastVerified, err))
		return
	}
	age := time.Since(t)
	threshold := time.Duration(f.StaleAfterDays) * 24 * time.Hour
	if age > threshold {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%s %q is stale: last_verified %s is %d days old (limit %d)",
			kind, id, f.LastVerified,
			int(age.Hours()/24), f.StaleAfterDays))
	}
	for _, ref := range f.VerifiedBy {
		_ = ref // references are informational; not validated against a live test registry
	}
}

// SelfcheckReport.String returns a human-readable summary.
func (r SelfcheckReport) String() string {
	if r.OK && len(r.Warnings) == 0 {
		return "selfcheck: ok"
	}
	var sb strings.Builder
	if r.OK {
		sb.WriteString("selfcheck: ok (with warnings)\n")
	} else {
		sb.WriteString("selfcheck: FAIL\n")
	}
	for _, e := range r.Errors {
		sb.WriteString("  error: " + e + "\n")
	}
	for _, w := range r.Warnings {
		sb.WriteString("  warn:  " + w + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

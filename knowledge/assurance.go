package knowledge

import "fmt"

// AssuranceReport summarizes knowledge coverage across the base.
type AssuranceReport struct {
	Counts          AssuranceCounts
	SubsystemsNamed []string
	Warnings        []string
}

// AssuranceCounts holds totals for each knowledge type.
type AssuranceCounts struct {
	Invariants           int
	FailureModes         int
	ForbiddenFixes       int
	IncidentPatterns     int
	Decisions            int
	ForbiddenAssumptions int
	RequiredTests        int
	SubsystemBoundaries  int
	AuthorityRules       int
	PreflightQuestions   int
	RemediationContracts int
}

// Assurance returns a coverage report over the knowledge base.
func Assurance(b *Base) AssuranceReport {
	r := AssuranceReport{}
	if b == nil {
		r.Warnings = append(r.Warnings, "knowledge base is nil")
		return r
	}

	r.Counts = AssuranceCounts{
		Invariants:           len(b.Invariants),
		FailureModes:         len(b.FailureModes),
		ForbiddenFixes:       len(b.ForbiddenFixes),
		IncidentPatterns:     len(b.IncidentPatterns),
		Decisions:            len(b.Decisions),
		ForbiddenAssumptions: len(b.ForbiddenAssumptions),
		RequiredTests:        len(b.RequiredTests),
		SubsystemBoundaries:  len(b.SubsystemBoundaries),
		AuthorityRules:       len(b.AuthorityRules),
		PreflightQuestions:   len(b.PreflightQuestions),
		RemediationContracts: len(b.RemediationContracts),
	}

	for _, sb := range b.SubsystemBoundaries {
		r.SubsystemsNamed = append(r.SubsystemsNamed, sb.Subsystem)
	}

	// Flag critical invariants that have no proven_by.
	unproven := 0
	for _, inv := range b.Invariants {
		if inv.Severity == "critical" &&
			len(inv.ProvenBy.Tests) == 0 && len(inv.ProvenBy.Incidents) == 0 {
			unproven++
		}
	}
	if unproven > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%d critical invariant(s) have no proven_by entries", unproven))
	}

	// Flag failure modes with no regression tests.
	untested := 0
	for _, fm := range b.FailureModes {
		if len(fm.RegressionTests) == 0 {
			untested++
		}
	}
	if untested > 0 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(
			"%d failure_mode(s) have no regression_tests", untested))
	}

	// Flag if no subsystem boundaries are defined.
	if len(b.SubsystemBoundaries) == 0 {
		r.Warnings = append(r.Warnings, "no subsystem_boundaries defined")
	}

	// Flag if no decisions are defined.
	if len(b.Decisions) == 0 {
		r.Warnings = append(r.Warnings, "no decisions defined — consider adding decision records")
	}

	return r
}

// Lines returns the report as printable lines for CLI/MCP output.
func (r AssuranceReport) Lines() []string {
	c := r.Counts
	out := []string{
		fmt.Sprintf("invariants:            %d", c.Invariants),
		fmt.Sprintf("failure_modes:         %d", c.FailureModes),
		fmt.Sprintf("forbidden_fixes:       %d", c.ForbiddenFixes),
		fmt.Sprintf("incident_patterns:     %d", c.IncidentPatterns),
		fmt.Sprintf("decisions:             %d", c.Decisions),
		fmt.Sprintf("forbidden_assumptions: %d", c.ForbiddenAssumptions),
		fmt.Sprintf("required_tests:        %d", c.RequiredTests),
		fmt.Sprintf("subsystem_boundaries:  %d", c.SubsystemBoundaries),
		fmt.Sprintf("authority_rules:       %d", c.AuthorityRules),
		fmt.Sprintf("preflight_questions:   %d", c.PreflightQuestions),
		fmt.Sprintf("remediation_contracts: %d", c.RemediationContracts),
	}
	if len(r.SubsystemsNamed) > 0 {
		out = append(out, "subsystems: "+joinStrings(r.SubsystemsNamed, ", "))
	}
	for _, w := range r.Warnings {
		out = append(out, "warn: "+w)
	}
	return out
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

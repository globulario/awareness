package fixledger

import (
	"strings"
)

// ContextAliasMap is a map from a target ID to a list of natural-language alias phrases.
// This mirrors learning.ContextAliasMap to avoid an import cycle.
type ContextAliasMap map[string][]string

// DidWeFixResult summarises the outcome of a "did we fix" query for a given task.
type DidWeFixResult struct {
	MatchedPattern  string
	MatchedFixCases []FixCase
	Invariants      []string
	OverallStatus   FixStatus
	FixedFiles      []string
	RemainingFiles  []string
	RequiredTests   []string
	NextAction      string
}

// DidWeFix queries whether a given task is covered by known fix cases.
//
// Matching pipeline:
//  1. Lowercase task
//  2. Match fix case patterns via substring match against task
//  3. Match via aliases → get invariant IDs
//  4. For each matched invariant include fix cases targeting it
//  5. Deduplicate
//  6. Compute OverallStatus
//  7. Derive NextAction from status
func DidWeFix(task string, fixCases []FixCase, aliases ContextAliasMap) *DidWeFixResult {
	lower := strings.ToLower(task)
	seen := make(map[string]bool)
	var matched []FixCase

	// Step 2: direct pattern match.
	for _, fc := range fixCases {
		if seen[fc.ID] {
			continue
		}
		if fc.Pattern != "" && strings.Contains(lower, strings.ToLower(fc.Pattern)) {
			matched = append(matched, fc)
			seen[fc.ID] = true
		}
	}

	// Step 3: alias match → invariant IDs.
	var matchedInvariants []string
	invSeen := make(map[string]bool)
	for targetID, phrases := range aliases {
		for _, phrase := range phrases {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				// Extract bare invariant ID (strip prefix if present).
				bareID := targetID
				for _, prefix := range []string{"invariant:", "failure_mode:", "service:"} {
					if strings.HasPrefix(targetID, prefix) {
						bareID = strings.TrimPrefix(targetID, prefix)
						break
					}
				}
				if !invSeen[bareID] {
					matchedInvariants = append(matchedInvariants, bareID)
					invSeen[bareID] = true
				}
				break
			}
		}
	}

	// Step 4: include fix cases targeting matched invariants.
	for _, invID := range matchedInvariants {
		for _, fc := range fixCases {
			if seen[fc.ID] {
				continue
			}
			for _, ti := range fc.TargetInvariants {
				if ti == invID {
					matched = append(matched, fc)
					seen[fc.ID] = true
					break
				}
			}
		}
	}

	if len(matched) == 0 {
		return &DidWeFixResult{
			MatchedPattern: task,
			OverallStatus:  FixUnknown,
			NextAction:     "No matching fix cases found. Consider creating a fix case in docs/awareness/fix_cases.yaml.",
		}
	}

	// Collect file and test sets.
	fixedSeen := make(map[string]bool)
	remainingSeen := make(map[string]bool)
	testSeen := make(map[string]bool)
	var fixedFiles, remainingFiles, requiredTests []string

	for _, fc := range matched {
		for _, f := range fc.FixedFiles {
			if !fixedSeen[f] {
				fixedFiles = append(fixedFiles, f)
				fixedSeen[f] = true
			}
		}
		for _, f := range fc.RemainingFiles {
			if !remainingSeen[f] {
				remainingFiles = append(remainingFiles, f)
				remainingSeen[f] = true
			}
		}
		for _, t := range fc.RequiredTests {
			if !testSeen[t] {
				requiredTests = append(requiredTests, t)
				testSeen[t] = true
			}
		}
	}

	overall := computeOverallStatus(matched)

	return &DidWeFixResult{
		MatchedPattern:  task,
		MatchedFixCases: matched,
		Invariants:      matchedInvariants,
		OverallStatus:   overall,
		FixedFiles:      fixedFiles,
		RemainingFiles:  remainingFiles,
		RequiredTests:   requiredTests,
		NextAction:      deriveNextAction(overall, remainingFiles, requiredTests),
	}
}

// computeOverallStatus derives a single status from a set of fix cases.
//
// Rules:
//   - If any REGRESSED → REGRESSED
//   - If all DONE → DONE
//   - If any PARTIAL → PARTIAL
//   - If any IN_PROGRESS → IN_PROGRESS
//   - If all PROPOSED → PROPOSED
//   - else UNKNOWN
func computeOverallStatus(cases []FixCase) FixStatus {
	if len(cases) == 0 {
		return FixUnknown
	}
	allDone := true
	allProposed := true
	anyRegressed := false
	anyPartial := false
	anyInProgress := false

	for _, fc := range cases {
		if fc.Status == FixRegressed {
			anyRegressed = true
		}
		if fc.Status != FixDone {
			allDone = false
		}
		if fc.Status != FixProposed {
			allProposed = false
		}
		if fc.Status == FixPartial {
			anyPartial = true
		}
		if fc.Status == FixInProgress {
			anyInProgress = true
		}
	}

	switch {
	case anyRegressed:
		return FixRegressed
	case allDone:
		return FixDone
	case anyPartial:
		return FixPartial
	case anyInProgress:
		return FixInProgress
	case allProposed:
		return FixProposed
	default:
		return FixUnknown
	}
}

// deriveNextAction returns a human-readable next action based on overall status.
func deriveNextAction(status FixStatus, remainingFiles, requiredTests []string) string {
	switch status {
	case FixDone:
		if len(requiredTests) > 0 {
			return "Fix is DONE. Verify all required tests pass and are present in the test suite."
		}
		return "Fix is DONE. No further action required."
	case FixPartial:
		return "Some gaps remain. Review remaining_files and add required tests."
	case FixInProgress:
		return "Fix is in progress. Complete the remaining files and tests."
	case FixProposed:
		return "Fix is proposed but not started. Implement the fix cases."
	case FixRegressed:
		return "REGRESSION detected. Investigate and restore the fix."
	default:
		return "Status unclear. Review fix cases and update status."
	}
}

// PatternStatus returns all fix cases whose pattern matches the given query string.
func PatternStatus(pattern string, fixCases []FixCase) []FixCase {
	lower := strings.ToLower(pattern)
	var matched []FixCase
	for _, fc := range fixCases {
		if fc.Pattern != "" && strings.Contains(lower, strings.ToLower(fc.Pattern)) {
			matched = append(matched, fc)
			continue
		}
		// Also match against ID or title.
		if strings.Contains(strings.ToLower(fc.ID), lower) ||
			strings.Contains(strings.ToLower(fc.Title), lower) {
			matched = append(matched, fc)
		}
	}
	return matched
}

// ListPartials returns fix cases with PARTIAL status.
func ListPartials(fixCases []FixCase) []FixCase {
	var out []FixCase
	for _, fc := range fixCases {
		if fc.Status == FixPartial {
			out = append(out, fc)
		}
	}
	return out
}

// ListRegressions returns fix cases with REGRESSED status.
func ListRegressions(fixCases []FixCase) []FixCase {
	var out []FixCase
	for _, fc := range fixCases {
		if fc.Status == FixRegressed {
			out = append(out, fc)
		}
	}
	return out
}

// CoverageReport builds a map from invariant ID to the fix cases that target it.
func CoverageReport(fixCases []FixCase, invariants []string) map[string][]FixCase {
	report := make(map[string][]FixCase, len(invariants))
	for _, invID := range invariants {
		report[invID] = nil // initialise so invariants with no fixes appear
	}
	for _, fc := range fixCases {
		for _, invID := range fc.TargetInvariants {
			report[invID] = append(report[invID], fc)
		}
	}
	return report
}

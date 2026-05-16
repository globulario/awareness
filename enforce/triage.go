package enforce

import (
	"fmt"
	"sort"
	"strings"
)

// FindingGroup aggregates findings that share the same finding code.
type FindingGroup struct {
	Code            string
	Severity        FindingSeverity
	Count           int   // unsuppressed count
	SuppressedCount int   // suppressed count
	SuppressedBy    string // suppression ID, if uniformly suppressed by one rule
	Example         Finding
	SuggestedAction string
	Findings        []Finding // unsuppressed (up to all)
	Suppressed      []Finding // suppressed
}

// GroupFindings aggregates a finding slice into groups by code.
// Groups are sorted by severity (ERROR first) then by count descending.
func GroupFindings(findings []Finding) []FindingGroup {
	byCode := map[string]*FindingGroup{}
	for _, f := range findings {
		g, ok := byCode[f.Code]
		if !ok {
			g = &FindingGroup{
				Code:            f.Code,
				Severity:        f.Severity,
				Example:         f,
				SuggestedAction: SuggestAction(f.Code),
			}
			byCode[f.Code] = g
		}
		g.Findings = append(g.Findings, f)
		g.Count++
	}
	var groups []FindingGroup
	for _, g := range byCode {
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		si := severityOrder(groups[i].Severity)
		sj := severityOrder(groups[j].Severity)
		if si != sj {
			return si < sj
		}
		return groups[i].Count > groups[j].Count
	})
	return groups
}

// GroupSuppressed aggregates suppressed findings by code.
// suppressedBy is parallel to suppressed — entry i was suppressed by suppressedBy[i].
func GroupSuppressed(suppressed []Finding, suppressedBy []string) []FindingGroup {
	type groupEntry struct {
		g   FindingGroup
		ids map[string]bool
	}
	byCode := map[string]*groupEntry{}
	for i, f := range suppressed {
		sid := ""
		if i < len(suppressedBy) {
			sid = suppressedBy[i]
		}
		e, ok := byCode[f.Code]
		if !ok {
			e = &groupEntry{
				g: FindingGroup{
					Code:            f.Code,
					Severity:        f.Severity,
					Example:         f,
					SuggestedAction: SuggestAction(f.Code),
				},
				ids: map[string]bool{},
			}
			byCode[f.Code] = e
		}
		e.g.Suppressed = append(e.g.Suppressed, f)
		e.g.SuppressedCount++
		if sid != "" {
			e.ids[sid] = true
		}
	}
	var groups []FindingGroup
	for _, e := range byCode {
		// Collapse to a single SuppressedBy string when one rule dominates.
		if len(e.ids) == 1 {
			for id := range e.ids {
				e.g.SuppressedBy = id
			}
		}
		groups = append(groups, e.g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].SuppressedCount > groups[j].SuppressedCount
	})
	return groups
}

// SuggestAction returns a concise remediation hint for a finding code.
func SuggestAction(code string) string {
	switch code {
	case CodeAnnotationUnknownDirective:
		return "Remove or correct the unknown //globular: directive"
	case CodeAnnotationMissingValue:
		return "Add the missing value after the //globular: directive"
	case CodeAnnotationBadStateTrans: // includes CodeMalformedStateTransition alias
		return "Format: //globular:state_transition FROM -> TO"
	case CodeAnnotationBadIdentifier:
		return "Use a single dot-separated identifier with no spaces"
	case CodeAnnotationBadTestName:
		return "Value must start with Test, Benchmark, or Example"
	case CodeAnnotationUnknownInvariant: // includes CodeAnnotationRefInvariantMissing alias
		return "Add the invariant to docs/awareness/invariants.yaml, then run 'globular awareness build'"
	case CodeAnnotationRefTestMissing:
		return "Implement the test function or update the tested_by target name"
	case CodeRequiredTestMissing:
		return "Implement the missing test function named in the //globular:tested_by annotation"
	case CodeRequiredTestNoPath:
		return "Implement the test function in a *_test.go file — it is declared in awareness YAML but has no Go implementation yet"
	case CodeHashSchemaNoProducer: // includes CodeMissingHashProducer alias
		return "Add //globular:hash_schema <name> to the function that computes this hash"
	case CodeHashSchemaNoConsumer: // includes CodeMissingHashConsumer alias
		return "Add //globular:expects_hash_schema <name> to the function that validates this hash"
	case CodeHashSchemaOrphaned: // includes CodeOrphanedHashSchema alias
		return "Add a producer and consumer, or remove the hash_schema node"
	case CodeGraphSourceFileMissing: // includes CodeStaleSourceFileNode alias
		return "Run 'globular awareness build' to remove stale graph nodes"
	case CodeInvariantNoEnforcer: // includes CodeOrphanedInvariantNode alias
		return "Add //globular:enforces <invariant-id> to the function that checks this invariant"
	case CodePackageContractMissing:
		return "Run 'globular awareness admit-package' to register this package's contract"
	case CodeDependencyCycleDangerous:
		return "Break the cycle — run 'globular awareness cycles' for details"
	case CodeNoGraph:
		return "Run 'globular awareness build' to create the graph DB"
	default:
		return "See docs/awareness/enforcement.md for remediation guidance"
	}
}

// BurnDownRecommendations generates a burn-down section for suppressed groups.
// Returns "" when there are no suppressed findings.
func BurnDownRecommendations(suppressedGroups []FindingGroup) string {
	if len(suppressedGroups) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Burn-down recommendations\n\n")
	sb.WriteString("Suppressed warnings are a technical debt backlog. Suggested burn-down order:\n\n")
	for i, g := range suppressedGroups {
		sid := ""
		if g.SuppressedBy != "" {
			sid = fmt.Sprintf(" (suppression: `%s`)", g.SuppressedBy)
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** — %d suppressed%s\n   _%s_\n\n",
			i+1, g.Code, g.SuppressedCount, sid, g.SuggestedAction))
	}
	return sb.String()
}

func severityOrder(s FindingSeverity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

package enforce

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TriagedResult extends AuditResult with suppression metadata and grouped findings.
// The embedded AuditResult reflects only UNSUPPRESSED findings so that ErrorCount,
// WarningCount, and Pass are directly actionable by CI gate logic.
type TriagedResult struct {
	*AuditResult

	// Groups contains unsuppressed findings grouped by code.
	Groups []FindingGroup

	// SuppressedGroups contains suppressed findings grouped by code.
	SuppressedGroups []FindingGroup

	// SuppressedCount is the total number of suppressed findings.
	SuppressedCount int

	// Problems with the suppression rules themselves.
	ExpiredSuppressions []Suppression
	MaxCountViolations  []MaxCountViolation
	InvalidSuppressions []InvalidSuppression
}

// RenderOptions controls what sections appear in the audit markdown output.
type RenderOptions struct {
	// Summary prints a compact one-line summary only.
	Summary bool

	// ShowSuppressed includes full detail of every suppressed finding.
	ShowSuppressed bool
}

// Triage applies suppressions to an AuditResult and returns a TriagedResult.
// The embedded AuditResult inside TriagedResult counts only unsuppressed findings.
// now is the reference time for expiry — pass time.Now() in production callers.
func Triage(result *AuditResult, sf *SuppressionFile, now time.Time) *TriagedResult {
	sr := ApplySuppressions(result.Findings, sf, now)

	// Recount from unsuppressed only.
	var errCount, warnCount, infoCount int
	for _, f := range sr.Unsuppressed {
		switch f.Severity {
		case SeverityError:
			errCount++
		case SeverityWarning:
			warnCount++
		default:
			infoCount++
		}
	}

	return &TriagedResult{
		AuditResult: &AuditResult{
			Findings:     sr.Unsuppressed,
			ErrorCount:   errCount,
			WarningCount: warnCount,
			InfoCount:    infoCount,
			Pass:         errCount == 0,
		},
		Groups:              GroupFindings(sr.Unsuppressed),
		SuppressedGroups:    GroupSuppressed(sr.Suppressed, sr.SuppressedBy),
		SuppressedCount:     len(sr.Suppressed),
		ExpiredSuppressions: sr.Expired,
		MaxCountViolations:  sr.MaxCountViolations,
		InvalidSuppressions: sr.Invalid,
	}
}

// RenderTriagedMarkdown renders a full grouped audit report.
func RenderTriagedMarkdown(r *TriagedResult, opts RenderOptions) string {
	if opts.Summary {
		return renderSummaryOnly(r)
	}
	return renderFullReport(r, opts.ShowSuppressed)
}

// RenderTriagedJSON returns a machine-readable JSON representation.
func RenderTriagedJSON(r *TriagedResult) string {
	type jsonFinding struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		File     string `json:"file,omitempty"`
		Symbol   string `json:"symbol,omitempty"`
		Message  string `json:"message"`
	}
	type jsonGroup struct {
		Code            string `json:"code"`
		Severity        string `json:"severity"`
		Count           int    `json:"count"`
		SuppressedCount int    `json:"suppressed_count"`
		SuppressedBy    string `json:"suppressed_by,omitempty"`
		SuggestedAction string `json:"suggested_action"`
	}
	type jsonViolation struct {
		SuppressionID string `json:"suppression_id"`
		MaxCount      int    `json:"max_count"`
		ActualCount   int    `json:"actual_count"`
	}
	type jsonReport struct {
		Pass             bool          `json:"pass"`
		ErrorCount       int           `json:"error_count"`
		WarningCount     int           `json:"warning_count"`
		UnsuppressedCount int          `json:"unsuppressed_count"`
		InfoCount        int           `json:"info_count"`
		SuppressedCount  int           `json:"suppressed_count"`
		Findings         []jsonFinding `json:"findings"`
		Groups           []jsonGroup   `json:"groups"`
		SuppressedGroups []jsonGroup   `json:"suppressed_groups"`
		MaxCountViolations []jsonViolation `json:"max_count_violations,omitempty"`
		ExpiredSuppressions []string   `json:"expired_suppressions,omitempty"`
	}

	jr := jsonReport{
		Pass:             r.Pass,
		ErrorCount:       r.ErrorCount,
		WarningCount:     r.WarningCount,
		UnsuppressedCount: len(r.Findings),
		InfoCount:        r.InfoCount,
		SuppressedCount:  r.SuppressedCount,
		Findings:         []jsonFinding{},
		Groups:           []jsonGroup{},
		SuppressedGroups: []jsonGroup{},
	}
	for _, f := range r.Findings {
		jr.Findings = append(jr.Findings, jsonFinding{f.Code, string(f.Severity), f.File, f.Symbol, f.Message})
	}
	for _, g := range r.Groups {
		jr.Groups = append(jr.Groups, jsonGroup{g.Code, string(g.Severity), g.Count, 0, "", g.SuggestedAction})
	}
	for _, g := range r.SuppressedGroups {
		jr.SuppressedGroups = append(jr.SuppressedGroups, jsonGroup{g.Code, string(g.Severity), 0, g.SuppressedCount, g.SuppressedBy, g.SuggestedAction})
	}
	for _, v := range r.MaxCountViolations {
		jr.MaxCountViolations = append(jr.MaxCountViolations, jsonViolation{v.SuppressionID, v.MaxCount, v.ActualCount})
	}
	for _, s := range r.ExpiredSuppressions {
		jr.ExpiredSuppressions = append(jr.ExpiredSuppressions, s.ID)
	}

	b, _ := json.MarshalIndent(jr, "", "  ")
	return string(b)
}

// FailsWarningThreshold returns true when unsuppressed warnings exceed threshold.
// threshold < 0 means disabled.
func FailsWarningThreshold(r *TriagedResult, threshold int) bool {
	if threshold < 0 {
		return false
	}
	return r.WarningCount > threshold
}

// HasSuppressionProblems returns true when any suppression is expired, invalid,
// or has violated its max_count. Used for strict CI gate.
func HasSuppressionProblems(r *TriagedResult) bool {
	return len(r.ExpiredSuppressions) > 0 ||
		len(r.InvalidSuppressions) > 0 ||
		len(r.MaxCountViolations) > 0
}

// renderSummaryOnly produces a compact single-paragraph summary.
func renderSummaryOnly(r *TriagedResult) string {
	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	return fmt.Sprintf(
		"# Awareness Audit: %s\n\nErrors: %d | Warnings (unsuppressed): %d | Suppressed: %d | Info: %d\n",
		status, r.ErrorCount, r.WarningCount, r.SuppressedCount, r.InfoCount,
	)
}

// renderFullReport produces the complete grouped markdown report.
func renderFullReport(r *TriagedResult, showSuppressed bool) string {
	var sb strings.Builder

	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	sb.WriteString(fmt.Sprintf("# Awareness Audit: %s\n\n", status))

	// --- Summary table ---
	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Count |\n|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Errors | %d |\n", r.ErrorCount))
	sb.WriteString(fmt.Sprintf("| Warnings (unsuppressed) | %d |\n", r.WarningCount))
	sb.WriteString(fmt.Sprintf("| Info | %d |\n", r.InfoCount))
	sb.WriteString(fmt.Sprintf("| Suppressed | %d |\n\n", r.SuppressedCount))

	// --- Suppression health warnings ---
	if len(r.ExpiredSuppressions) > 0 {
		sb.WriteString("### Expired suppressions (re-activate or remove)\n\n")
		for _, s := range r.ExpiredSuppressions {
			sb.WriteString(fmt.Sprintf("- **%s** expired %s (owner: %s)\n", s.ID, s.ExpiresAt, s.Owner))
		}
		sb.WriteString("\n")
	}
	if len(r.MaxCountViolations) > 0 {
		sb.WriteString("### Max-count violations (new growth detected)\n\n")
		for _, v := range r.MaxCountViolations {
			sb.WriteString(fmt.Sprintf("- **%s**: %d found, max_count=%d — update suppression or fix new instances\n",
				v.SuppressionID, v.ActualCount, v.MaxCount))
		}
		sb.WriteString("\n")
	}
	if len(r.InvalidSuppressions) > 0 {
		sb.WriteString("### Invalid suppressions (missing required fields)\n\n")
		for _, inv := range r.InvalidSuppressions {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", inv.SuppressionID, inv.Error))
		}
		sb.WriteString("\n")
	}

	// --- Top finding groups table ---
	if len(r.Groups) > 0 || len(r.SuppressedGroups) > 0 {
		sb.WriteString("## Top finding groups\n\n")
		sb.WriteString("| Code | Severity | Count | Example | Suggested action |\n")
		sb.WriteString("|------|----------|-------|---------|------------------|\n")
		for _, g := range r.Groups {
			ex := truncate(g.Example.Message, 55)
			sb.WriteString(fmt.Sprintf("| `%s` | %s | %d | %s | %s |\n",
				g.Code, string(g.Severity), g.Count, ex, g.SuggestedAction))
		}
		for _, g := range r.SuppressedGroups {
			ex := truncate(g.Example.Message, 55)
			sb.WriteString(fmt.Sprintf("| `%s` | %s | ~~%d~~ suppressed | %s | %s |\n",
				g.Code, string(g.Severity), g.SuppressedCount, ex, g.SuggestedAction))
		}
		sb.WriteString("\n")
	}

	// --- Errors (always shown in full) ---
	errors := filterBySeverity(r.Findings, SeverityError)
	if len(errors) > 0 {
		sb.WriteString(fmt.Sprintf("## Errors (%d)\n\n", len(errors)))
		for _, f := range errors {
			loc := formatFindingLoc(f)
			sb.WriteString(fmt.Sprintf("- **%s**%s: %s\n", f.Code, loc, f.Message))
		}
		sb.WriteString("\n")
	}

	// --- Unsuppressed warnings (grouped, capped at 5 examples per group) ---
	unsuppressed := filterBySeverity(r.Findings, SeverityWarning)
	if len(unsuppressed) > 0 {
		sb.WriteString(fmt.Sprintf("## Unsuppressed warnings (%d)\n\n", len(unsuppressed)))
		warnGroups := GroupFindings(unsuppressed)
		for _, g := range warnGroups {
			sb.WriteString(fmt.Sprintf("### %s (%d)\n\n", g.Code, g.Count))
			sb.WriteString(fmt.Sprintf("_%s_\n\n", g.SuggestedAction))
			shown := g.Findings
			if len(shown) > 5 {
				shown = shown[:5]
			}
			for _, f := range shown {
				loc := formatFindingLoc(f)
				sb.WriteString(fmt.Sprintf("- %s%s\n", f.Message, loc))
			}
			if len(g.Findings) > 5 {
				sb.WriteString(fmt.Sprintf("- … and %d more\n", len(g.Findings)-5))
			}
			sb.WriteString("\n")
		}
	}

	// --- Suppressed warnings ---
	if r.SuppressedCount > 0 {
		sb.WriteString(fmt.Sprintf("## Suppressed warnings (%d)\n\n", r.SuppressedCount))
		for _, g := range r.SuppressedGroups {
			sb.WriteString(fmt.Sprintf("- **`%s`**: %d suppressed by `%s`\n",
				g.Code, g.SuppressedCount, g.SuppressedBy))
		}
		sb.WriteString("\n")

		if showSuppressed {
			sb.WriteString("### Suppressed finding detail\n\n")
			for _, g := range r.SuppressedGroups {
				sb.WriteString(fmt.Sprintf("#### %s (%d)\n\n", g.Code, g.SuppressedCount))
				for _, f := range g.Suppressed {
					sb.WriteString(fmt.Sprintf("- %s\n", f.Message))
				}
				sb.WriteString("\n")
			}
		}
	}

	// --- Burn-down ---
	if bd := BurnDownRecommendations(r.SuppressedGroups); bd != "" {
		sb.WriteString(bd)
	}

	if len(r.Findings) == 0 && r.SuppressedCount == 0 {
		sb.WriteString("No findings — annotations are well-formed and all contracts satisfied.\n")
	}

	return sb.String()
}

func formatFindingLoc(f Finding) string {
	if f.File == "" {
		return ""
	}
	loc := f.File
	if f.Symbol != "" {
		loc += " (" + f.Symbol + ")"
	}
	return " — `" + loc + "`"
}

func filterBySeverity(findings []Finding, sev FindingSeverity) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Severity == sev {
			out = append(out, f)
		}
	}
	return out
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

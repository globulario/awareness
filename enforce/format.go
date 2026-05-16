package enforce

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderAuditMarkdown formats an AuditResult as a markdown report.
func RenderAuditMarkdown(r *AuditResult) string {
	var sb strings.Builder

	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	sb.WriteString(fmt.Sprintf("# Awareness Audit: %s\n\n", status))
	sb.WriteString(fmt.Sprintf("| Severity | Count |\n|----------|-------|\n"))
	sb.WriteString(fmt.Sprintf("| ERROR    | %d |\n", r.ErrorCount))
	sb.WriteString(fmt.Sprintf("| WARNING  | %d |\n", r.WarningCount))
	sb.WriteString(fmt.Sprintf("| INFO     | %d |\n\n", r.InfoCount))

	if len(r.Findings) == 0 {
		sb.WriteString("No findings — annotations are well-formed and all contracts satisfied.\n")
		return sb.String()
	}

	// Group by severity.
	bySeverity := map[FindingSeverity][]Finding{
		SeverityError:   nil,
		SeverityWarning: nil,
		SeverityInfo:    nil,
	}
	for _, f := range r.Findings {
		bySeverity[f.Severity] = append(bySeverity[f.Severity], f)
	}

	for _, sev := range []FindingSeverity{SeverityError, SeverityWarning, SeverityInfo} {
		group := bySeverity[sev]
		if len(group) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s (%d)\n\n", string(sev), len(group)))
		for _, f := range group {
			loc := ""
			if f.File != "" {
				loc = f.File
				if f.Symbol != "" {
					loc += " (" + f.Symbol + ")"
				}
				loc = " — `" + loc + "`"
			}
			sb.WriteString(fmt.Sprintf("- **%s**%s: %s\n", f.Code, loc, f.Message))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// RenderAuditJSON formats an AuditResult as a JSON string.
func RenderAuditJSON(r *AuditResult) string {
	type jsonFinding struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		File     string `json:"file,omitempty"`
		Symbol   string `json:"symbol,omitempty"`
		Message  string `json:"message"`
	}
	type jsonReport struct {
		Pass         bool          `json:"pass"`
		ErrorCount   int           `json:"error_count"`
		WarningCount int           `json:"warning_count"`
		InfoCount    int           `json:"info_count"`
		Findings     []jsonFinding `json:"findings"`
	}

	jr := jsonReport{
		Pass:         r.Pass,
		ErrorCount:   r.ErrorCount,
		WarningCount: r.WarningCount,
		InfoCount:    r.InfoCount,
	}
	for _, f := range r.Findings {
		jr.Findings = append(jr.Findings, jsonFinding{
			Code:     f.Code,
			Severity: string(f.Severity),
			File:     f.File,
			Symbol:   f.Symbol,
			Message:  f.Message,
		})
	}
	if jr.Findings == nil {
		jr.Findings = []jsonFinding{}
	}

	b, _ := json.MarshalIndent(jr, "", "  ")
	return string(b)
}

// RenderHookText formats a HookResult as plain text for Claude Code hook stdout.
func RenderHookText(r *HookResult) string {
	return r.Summary
}

// RenderPRReport formats a PRReport as a markdown summary for CI annotations.
func RenderPRReport(r *PRReport) string {
	var sb strings.Builder

	status := "PASS"
	if !r.Pass {
		status = "FAIL"
	}
	sb.WriteString(fmt.Sprintf("# Awareness PR Report: %s\n\n", status))

	if len(r.ChangedFiles) > 0 {
		sb.WriteString("**Changed files**: " + strings.Join(r.ChangedFiles, ", ") + "\n\n")
	}

	if len(r.Findings) == 0 {
		sb.WriteString("No annotation or contract findings for changed files.\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("%d errors, %d warnings\n\n", r.ErrorCount, r.WarningCount))
	for _, f := range r.Findings {
		icon := "⚠"
		if f.Severity == SeverityError {
			icon = "✗"
		}
		loc := f.File
		if f.Symbol != "" {
			loc += " (" + f.Symbol + ")"
		}
		if loc != "" {
			sb.WriteString(fmt.Sprintf("%s [%s] %s: %s\n", icon, f.Code, loc, f.Message))
		} else {
			sb.WriteString(fmt.Sprintf("%s [%s] %s\n", icon, f.Code, f.Message))
		}
	}

	return sb.String()
}

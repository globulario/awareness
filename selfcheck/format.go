package selfcheck

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format identifies the output format for a self-check report.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
	FormatAgent    Format = "agent"
)

// Render serialises r into the requested format.
func Render(r *Report, format Format) (string, error) {
	switch format {
	case FormatJSON:
		return renderJSON(r)
	case FormatAgent:
		return renderAgent(r), nil
	default:
		return renderMarkdown(r), nil
	}
}

func renderJSON(r *Report) (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal self-check report: %w", err)
	}
	return string(b), nil
}

func renderMarkdown(r *Report) string {
	var sb strings.Builder

	sb.WriteString("# Awareness Self-Check Report\n\n")
	sb.WriteString(fmt.Sprintf("**Generated:** %s\n\n", r.GeneratedAt.Format("2006-01-02T15:04:05Z")))

	overallStatus := "✓ PASS"
	if !r.Pass {
		overallStatus = "✗ FAIL"
	}
	sb.WriteString(fmt.Sprintf("**Overall:** %s\n\n", overallStatus))

	// Summary table.
	pass, fail, weak, skip := 0, 0, 0, 0
	for _, cr := range r.Checks {
		switch cr.Status {
		case StatusPass:
			pass++
		case StatusFail:
			fail++
		case StatusWeak:
			weak++
		case StatusSkipped:
			skip++
		}
	}
	sb.WriteString(fmt.Sprintf("| PASS | FAIL | WEAK | SKIPPED |\n|------|------|------|---------|\n| %d | %d | %d | %d |\n\n",
		pass, fail, weak, skip))

	// Check results.
	sb.WriteString("## Check Results\n\n")
	for _, cr := range r.Checks {
		icon := statusIcon(cr.Status)
		sb.WriteString(fmt.Sprintf("### %s %s (`%s`)\n\n", icon, cr.Name, string(cr.Kind)))
		sb.WriteString(cr.Detail + "\n")
		if len(cr.FalseSilences) > 0 {
			sb.WriteString("\n**False silences:**\n")
			for _, fs := range cr.FalseSilences {
				sb.WriteString("- " + fs + "\n")
			}
		}
		if len(cr.Noisy) > 0 {
			sb.WriteString("\n**Noisy:**\n")
			for _, n := range cr.Noisy {
				sb.WriteString("- " + n + "\n")
			}
		}
		sb.WriteString("\n")
	}

	// Aggregated issues.
	if len(r.FalseSilences) > 0 {
		sb.WriteString("## False Silence Risks\n\n")
		for _, fs := range r.FalseSilences {
			sb.WriteString("- " + fs + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.MCPIssues) > 0 {
		sb.WriteString("## MCP Exposure Issues\n\n")
		for _, issue := range r.MCPIssues {
			sb.WriteString("- " + issue + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.StaleRefs) > 0 {
		sb.WriteString("## Stale Graph References\n\n")
		for _, ref := range r.StaleRefs {
			sb.WriteString("- " + ref + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.RecommendedFixes) > 0 {
		sb.WriteString("## Recommended Fixes\n\n")
		for i, fix := range r.RecommendedFixes {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, fix))
		}
		sb.WriteString("\n")
	}

	if r.ShouldCreateIncident {
		sb.WriteString("> **Incident recommended:** re-run with `--create-incident` to bundle evidence.\n")
	}

	return sb.String()
}

func renderAgent(r *Report) string {
	var sb strings.Builder

	if r.Pass {
		sb.WriteString("SELF-CHECK PASS\n\n")
	} else {
		sb.WriteString("SELF-CHECK FAIL\n\n")
		sb.WriteString("False silences (invariants not surfaced when expected):\n")
		for _, fs := range r.FalseSilences {
			sb.WriteString("  - " + fs + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.MCPIssues) > 0 {
		sb.WriteString("MCP SAFETY VIOLATION:\n")
		for _, issue := range r.MCPIssues {
			sb.WriteString("  - " + issue + "\n")
		}
		sb.WriteString("\n")
	}

	// Failed checks only.
	for _, cr := range r.Checks {
		if cr.Status == StatusFail || cr.Status == StatusWeak {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", cr.Status, cr.Name, cr.Detail))
		}
	}

	if len(r.RecommendedFixes) > 0 {
		sb.WriteString("\nRecommended fixes:\n")
		for _, fix := range r.RecommendedFixes {
			sb.WriteString("  - " + fix + "\n")
		}
	}

	return sb.String()
}

func statusIcon(s CheckStatus) string {
	switch s {
	case StatusPass:
		return "✓"
	case StatusFail:
		return "✗"
	case StatusWeak:
		return "⚠"
	default:
		return "–"
	}
}

package debugsession

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatReport renders a DebugSessionReport in the requested format.
// Supported formats: "agent" (default), "markdown", "json".
func FormatReport(r *DebugSessionReport, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(r, "", "  ")
		return string(b)
	case "markdown":
		return formatMarkdown(r)
	default:
		return formatAgent(r)
	}
}

// ---- agent format -----------------------------------------------------------

func formatAgent(r *DebugSessionReport) string {
	var b strings.Builder

	b.WriteString("AGENT DEBUG SESSION\n\n")

	b.WriteString(fmt.Sprintf("Task:\n%s\n\n", r.Task))

	b.WriteString(fmt.Sprintf("Classification:\n%s\n\n", classString(r.Classification)))

	b.WriteString(fmt.Sprintf("Confidence: %s\n\n", r.Confidence))

	// Starting nodes.
	if len(r.StartingNodes) > 0 {
		b.WriteString("Start here:\n")
		for i, n := range r.StartingNodes {
			path := ""
			if n.Path != "" {
				path = " [" + n.Path + "]"
			}
			b.WriteString(fmt.Sprintf("%d. %s (%s)%s — via %s\n", i+1, n.Name, n.NodeType, path, n.Source))
		}
		b.WriteString("\n")
	}

	// Likely root-cause paths.
	if len(r.LikelyRootCausePaths) > 0 {
		b.WriteString("Likely root-cause paths:\n")
		for i, p := range r.LikelyRootCausePaths {
			severity := ""
			if p.Severity == "critical" {
				severity = " [CRITICAL]"
			}
			b.WriteString(fmt.Sprintf("%d. %s%s (cost %.1f)\n", i+1, p.PathSummary, severity, p.SemanticCost))
			if p.WhyItMatters != "" {
				b.WriteString(fmt.Sprintf("   Why it matters: %s\n", p.WhyItMatters))
			}
		}
		b.WriteString("\n")
	}

	// Do not do.
	if len(r.DoNotDo) > 0 {
		b.WriteString("Do not:\n")
		for _, d := range r.DoNotDo {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
		b.WriteString("\n")
	}

	// Inspect.
	inspectItems := append(r.SuggestedFiles, r.SuggestedSymbols...)
	if len(inspectItems) > 0 {
		b.WriteString("Inspect:\n")
		for _, item := range inspectItems {
			b.WriteString(fmt.Sprintf("- %s\n", item))
		}
		b.WriteString("\n")
	}

	// Invariants.
	if len(r.RelevantInvariants) > 0 {
		b.WriteString("Relevant invariants:\n")
		for _, inv := range r.RelevantInvariants {
			b.WriteString(fmt.Sprintf("- %s\n", inv))
		}
		b.WriteString("\n")
	}

	// Failure modes.
	if len(r.RelevantFailureModes) > 0 {
		b.WriteString("Known failure modes:\n")
		for _, fm := range r.RelevantFailureModes {
			b.WriteString(fmt.Sprintf("- %s\n", fm))
		}
		b.WriteString("\n")
	}

	// Run.
	if len(r.RequiredTests) > 0 {
		b.WriteString("Run:\n")
		for _, t := range r.RequiredTests {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
		b.WriteString("\n")
	}

	// Investigation plan.
	if len(r.InvestigationPlan) > 0 {
		b.WriteString("Investigation plan:\n")
		for i, step := range r.InvestigationPlan {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		b.WriteString("\n")
	}

	// Learning recommendation.
	if r.LearningRecommendation != "" {
		b.WriteString("If this is new:\n")
		b.WriteString(fmt.Sprintf("- %s\n\n", r.LearningRecommendation))
	}

	// Warnings.
	if len(r.Warnings) > 0 {
		b.WriteString("Warnings:\n")
		for _, w := range r.Warnings {
			b.WriteString(fmt.Sprintf("! %s\n", w))
		}
	}

	return b.String()
}

// ---- markdown format --------------------------------------------------------

func formatMarkdown(r *DebugSessionReport) string {
	var b strings.Builder

	b.WriteString("# Debug Session\n\n")
	b.WriteString(fmt.Sprintf("**Task**: %s\n\n", r.Task))
	b.WriteString(fmt.Sprintf("**Classification**: %s\n\n", classString(r.Classification)))
	b.WriteString(fmt.Sprintf("**Confidence**: %s\n\n", r.Confidence))

	if len(r.StartingNodes) > 0 {
		b.WriteString("## Starting Nodes\n\n")
		for _, n := range r.StartingNodes {
			b.WriteString(fmt.Sprintf("- `%s` (%s) — source: %s\n", n.Name, n.NodeType, n.Source))
		}
		b.WriteString("\n")
	}

	if len(r.LikelyRootCausePaths) > 0 {
		b.WriteString("## Likely Root-Cause Paths\n\n")
		for i, p := range r.LikelyRootCausePaths {
			sev := ""
			if p.Severity == "critical" {
				sev = " **[CRITICAL]**"
			}
			b.WriteString(fmt.Sprintf("%d. `%s`%s (cost %.1f)\n", i+1, p.PathSummary, sev, p.SemanticCost))
			if p.WhyItMatters != "" {
				b.WriteString(fmt.Sprintf("   > %s\n", p.WhyItMatters))
			}
		}
		b.WriteString("\n")
	}

	writeMarkdownList := func(heading string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", heading))
		for _, item := range items {
			b.WriteString(fmt.Sprintf("- %s\n", item))
		}
		b.WriteString("\n")
	}

	writeMarkdownList("Relevant Invariants", r.RelevantInvariants)
	writeMarkdownList("Known Failure Modes", r.RelevantFailureModes)
	writeMarkdownList("Forbidden Fixes", r.ForbiddenFixes)
	writeMarkdownList("Files to Inspect", r.SuggestedFiles)
	writeMarkdownList("Symbols to Inspect", r.SuggestedSymbols)
	writeMarkdownList("Required Tests", r.RequiredTests)

	if len(r.InvestigationPlan) > 0 {
		b.WriteString("## Investigation Plan\n\n")
		for i, step := range r.InvestigationPlan {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		b.WriteString("\n")
	}

	if r.LearningRecommendation != "" {
		b.WriteString("## Learning Recommendation\n\n")
		b.WriteString(r.LearningRecommendation + "\n\n")
	}

	writeMarkdownList("Warnings", r.Warnings)

	return b.String()
}

package semantic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatPath renders a SemanticPath in the requested format ("agent", "json", "markdown").
func FormatPath(p *SemanticPath, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(p, "", "  ")
		return string(b)
	case "markdown":
		return formatPathMarkdown(p)
	default:
		return formatPathAgent(p)
	}
}

// FormatRelated renders a slice of SemanticRelated results.
func FormatRelated(results []SemanticRelated, nodeID, dim string, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(results, "", "  ")
		return string(b)
	case "markdown":
		return formatRelatedMarkdown(results, nodeID, dim)
	default:
		return formatRelatedAgent(results, nodeID, dim)
	}
}

// FormatWhy renders a WhyResult.
func FormatWhy(r *WhyResult, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(r, "", "  ")
		return string(b)
	case "markdown":
		return formatWhyMarkdown(r)
	default:
		return formatWhyAgent(r)
	}
}

// FormatSemanticNeighborhood renders neighbourhood results.
func FormatSemanticNeighborhood(results []SemanticRelated, nodeID, dim string, format string) string {
	// Neighbourhood and Related share the same rendering.
	return FormatRelated(results, nodeID, dim, format)
}

// ---- agent formatters ----

func formatPathAgent(p *SemanticPath) string {
	var b strings.Builder
	b.WriteString("AGENT SEMANTIC PATH\n\n")
	b.WriteString(fmt.Sprintf("From: %s\n", p.From))
	b.WriteString(fmt.Sprintf("To:   %s\n", p.To))
	b.WriteString(fmt.Sprintf("Dimension: %s\n", p.Dimension))

	if !p.Found {
		b.WriteString("\nNo path found within search constraints.\n")
		if p.Truncated {
			b.WriteString("(Search was truncated)\n")
		}
		return b.String()
	}

	b.WriteString(fmt.Sprintf("Total cost: %.2f\n", p.TotalCost))
	if p.Truncated {
		b.WriteString("(Search was truncated)\n")
	}
	b.WriteString("\nPath:\n")

	for i, step := range p.Steps {
		b.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, step.NodeName, step.NodeType))
		if i < len(p.Steps)-1 && i+1 < len(p.Steps) {
			next := p.Steps[i+1]
			if next.EdgeKind != "" {
				b.WriteString(fmt.Sprintf("   %s%s%s\n", next.EdgeDir, next.EdgeKind, next.EdgeDir))
			}
		}
	}

	b.WriteString(fmt.Sprintf("\nExplanation:\n%s\n", p.Explanation))
	return b.String()
}

func formatPathMarkdown(p *SemanticPath) string {
	var b strings.Builder
	b.WriteString("## Semantic Path\n\n")
	b.WriteString(fmt.Sprintf("- **From**: `%s`\n", p.From))
	b.WriteString(fmt.Sprintf("- **To**: `%s`\n", p.To))
	b.WriteString(fmt.Sprintf("- **Dimension**: %s\n", p.Dimension))

	if !p.Found {
		b.WriteString("\n_No path found within search constraints._\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("- **Total cost**: %.2f\n\n", p.TotalCost))
	b.WriteString("### Steps\n\n")

	for i, step := range p.Steps {
		if i == 0 {
			b.WriteString(fmt.Sprintf("1. `%s` (%s)\n", step.NodeName, step.NodeType))
		} else {
			b.WriteString(fmt.Sprintf("   — *%s* %s\n", step.EdgeKind, step.EdgeDir))
			b.WriteString(fmt.Sprintf("%d. `%s` (%s)\n", i+1, step.NodeName, step.NodeType))
		}
	}

	b.WriteString(fmt.Sprintf("\n**Path**: `%s`\n", p.Explanation))
	return b.String()
}

func formatRelatedAgent(results []SemanticRelated, nodeID, dim string) string {
	var b strings.Builder
	b.WriteString("AGENT SEMANTIC RELATED\n\n")
	b.WriteString(fmt.Sprintf("Node: %s\n", nodeID))
	b.WriteString(fmt.Sprintf("Dimension: %s\n", dim))

	if len(results) == 0 {
		b.WriteString("\nNo related nodes found.\n")
		return b.String()
	}

	b.WriteString("\nResults:\n")
	for i, r := range results {
		nodeName := r.Node.ID
		nodeType := ""
		if r.Node != nil {
			nodeName = r.Node.Name
			nodeType = r.Node.Type
		}
		b.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, nodeName, nodeType))
		b.WriteString(fmt.Sprintf("   distance: %.2f\n", r.Distance))
		b.WriteString(fmt.Sprintf("   reason: %s\n", r.Reason))
		if r.PathSummary != "" {
			b.WriteString(fmt.Sprintf("   path: %s\n", r.PathSummary))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatRelatedMarkdown(results []SemanticRelated, nodeID, dim string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Semantic Related: `%s`\n\n", nodeID))
	b.WriteString(fmt.Sprintf("**Dimension**: %s\n\n", dim))

	if len(results) == 0 {
		b.WriteString("_No related nodes found._\n")
		return b.String()
	}

	for i, r := range results {
		nodeName := r.Node.ID
		nodeType := ""
		if r.Node != nil {
			nodeName = r.Node.Name
			nodeType = r.Node.Type
		}
		b.WriteString(fmt.Sprintf("%d. **%s** (`%s`) — distance: %.2f — via: *%s*\n",
			i+1, nodeName, nodeType, r.Distance, r.Reason))
	}
	return b.String()
}

func formatWhyAgent(r *WhyResult) string {
	var b strings.Builder
	b.WriteString("AGENT WHY RELATED\n\n")
	b.WriteString(fmt.Sprintf("From: %s\n", r.From))
	b.WriteString(fmt.Sprintf("To:   %s\n", r.To))
	b.WriteString(fmt.Sprintf("Dimension: %s\n", r.Dimension))
	b.WriteString("\n")
	b.WriteString(r.RelationshipSummary)
	b.WriteString("\n")

	if r.WhyItMatters != "" {
		b.WriteString("\nWhy it matters:\n")
		b.WriteString(r.WhyItMatters)
		b.WriteString("\n")
	}

	if len(r.ForbiddenFixes) > 0 {
		b.WriteString("\nDo not:\n")
		for _, ff := range r.ForbiddenFixes {
			b.WriteString(fmt.Sprintf("- %s\n", ff))
		}
	}

	if len(r.RequiredTests) > 0 {
		b.WriteString("\nRun:\n")
		for _, t := range r.RequiredTests {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
	}

	if len(r.EditWarnings) > 0 {
		b.WriteString("\nEdit warnings:\n")
		for _, w := range r.EditWarnings {
			b.WriteString(fmt.Sprintf("> %s\n", w))
		}
	}

	return b.String()
}

func formatWhyMarkdown(r *WhyResult) string {
	var b strings.Builder
	b.WriteString("## Why Related\n\n")
	b.WriteString(fmt.Sprintf("- **From**: `%s`\n", r.From))
	b.WriteString(fmt.Sprintf("- **To**: `%s`\n", r.To))
	b.WriteString(fmt.Sprintf("- **Dimension**: %s\n\n", r.Dimension))

	b.WriteString(r.RelationshipSummary)
	b.WriteString("\n\n")

	if r.WhyItMatters != "" {
		b.WriteString("### Why It Matters\n\n")
		b.WriteString(r.WhyItMatters)
		b.WriteString("\n\n")
	}

	if len(r.ForbiddenFixes) > 0 {
		b.WriteString("### Forbidden Fixes\n\n")
		for _, ff := range r.ForbiddenFixes {
			b.WriteString(fmt.Sprintf("- %s\n", ff))
		}
		b.WriteString("\n")
	}

	if len(r.RequiredTests) > 0 {
		b.WriteString("### Required Tests\n\n")
		for _, t := range r.RequiredTests {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
		b.WriteString("\n")
	}

	if len(r.EditWarnings) > 0 {
		b.WriteString("### Edit Warnings\n\n")
		for _, w := range r.EditWarnings {
			b.WriteString(fmt.Sprintf("> %s\n", w))
		}
	}
	return b.String()
}

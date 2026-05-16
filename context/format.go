package awarectx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// FormatNodeContext renders a NodeContext in the requested format.
// Supported formats: "markdown", "json", "agent" (default: "markdown").
func FormatNodeContext(nc *NodeContext, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(nc, "", "  ")
		return string(b)
	case "agent":
		return formatNodeContextAgent(nc)
	default:
		return formatNodeContextMarkdown(nc)
	}
}

// FormatNeighborhood renders a NeighborhoodResult in the requested format.
func FormatNeighborhood(nr *NeighborhoodResult, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(nr, "", "  ")
		return string(b)
	case "agent":
		return formatNeighborhoodAgent(nr)
	default:
		return formatNeighborhoodMarkdown(nr)
	}
}

// FormatExplanation renders an Explanation in the requested format.
func FormatExplanation(ex *Explanation, format string) string {
	switch format {
	case "json":
		b, _ := json.MarshalIndent(ex, "", "  ")
		return string(b)
	case "agent":
		return formatExplanationAgent(ex)
	default:
		return formatExplanationMarkdown(ex)
	}
}

// --- NodeContext formatters ---

func formatNodeContextMarkdown(nc *NodeContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Node Context: %s\n\n", nc.Name)
	fmt.Fprintf(&b, "- **ID**: `%s`\n", nc.NodeID)
	fmt.Fprintf(&b, "- **Type**: `%s`\n", nc.NodeType)
	if nc.Path != "" {
		fmt.Fprintf(&b, "- **Path**: `%s`\n", nc.Path)
	}
	fmt.Fprintf(&b, "- **Source confidence**: `%s`\n", nc.SourceLabel)
	if nc.Summary != "" {
		fmt.Fprintf(&b, "- **Summary**: %s\n", nc.Summary)
	}
	if nc.Package != "" {
		fmt.Fprintf(&b, "- **Package**: `%s`\n", nc.Package)
	}
	if nc.Service != "" {
		fmt.Fprintf(&b, "- **Service**: `%s`\n", nc.Service)
	}
	b.WriteString("\n")

	writeMarkdownStrings(&b, "### Design decisions", nc.DesignDecisions)
	writeMarkdownStrings(&b, "### Forbidden fixes", nc.ForbiddenFixes)
	writeMarkdownStrings(&b, "### State reads", nc.StateReads)
	writeMarkdownStrings(&b, "### State writes", nc.StateWrites)
	writeMarkdownStrings(&b, "### Dependency phases", nc.DependencyPhases)
	writeMarkdownStrings(&b, "### Required tests", nc.RequiredTests)
	writeMarkdownStrings(&b, "### Fix cases", nc.FixCases)
	writeMarkdownStrings(&b, "### Anti-patterns", nc.AntiPatterns)

	if len(nc.RelatedInvariants) > 0 {
		b.WriteString("### Related invariants\n\n")
		for _, inv := range nc.RelatedInvariants {
			fmt.Fprintf(&b, "- **%s** [%s] — %s\n", inv.ID, inv.Severity, inv.Summary)
		}
		b.WriteString("\n")
	}
	if len(nc.RelatedFailureModes) > 0 {
		b.WriteString("### Related failure modes\n\n")
		for _, fm := range nc.RelatedFailureModes {
			fmt.Fprintf(&b, "- **%s** — %s\n", fm.ID, fm.Summary)
		}
		b.WriteString("\n")
	}
	if len(nc.EditWarnings) > 0 {
		b.WriteString("### Edit warnings\n\n")
		for _, w := range nc.EditWarnings {
			fmt.Fprintf(&b, "> %s\n", w)
		}
		b.WriteString("\n")
	}
	if len(nc.RuntimeEvidence) > 0 {
		b.WriteString("### Runtime evidence\n\n")
		for _, r := range nc.RuntimeEvidence {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}
	writeMarkdownStrings(&b, "### Recommended searches", nc.RecommendedSearches)

	if len(nc.DirectAnnotations) > 0 {
		b.WriteString("### Safety annotations\n\n")
		for _, e := range nc.DirectAnnotations {
			fmt.Fprintf(&b, "- `%s` → `%s` (%s)\n", e.Kind, e.TargetName, e.TargetType)
		}
		b.WriteString("\n")
	}

	if len(nc.Truncated) > 0 {
		b.WriteString("### Truncated\n\n")
		for k, n := range nc.Truncated {
			fmt.Fprintf(&b, "- %s: %d more item(s) not shown\n", k, n)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatNodeContextAgent(nc *NodeContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "node_id: %s\n", nc.NodeID)
	fmt.Fprintf(&b, "node_type: %s\n", nc.NodeType)
	fmt.Fprintf(&b, "name: %s\n", nc.Name)
	fmt.Fprintf(&b, "confidence: %s\n", nc.SourceLabel)
	if nc.Summary != "" {
		fmt.Fprintf(&b, "summary: %s\n", nc.Summary)
	}
	if nc.Service != "" {
		fmt.Fprintf(&b, "service: %s\n", nc.Service)
	}
	if nc.Package != "" {
		fmt.Fprintf(&b, "package: %s\n", nc.Package)
	}
	writeAgentList(&b, "design_decisions", nc.DesignDecisions)
	writeAgentList(&b, "forbidden_fixes", nc.ForbiddenFixes)
	writeAgentList(&b, "state_reads", nc.StateReads)
	writeAgentList(&b, "state_writes", nc.StateWrites)
	writeAgentList(&b, "required_tests", nc.RequiredTests)
	writeAgentList(&b, "edit_warnings", nc.EditWarnings)
	writeAgentInvariants(&b, nc.RelatedInvariants)
	writeAgentFailureModes(&b, nc.RelatedFailureModes)
	writeAgentList(&b, "recommended_searches", nc.RecommendedSearches)
	return b.String()
}

// --- NeighborhoodResult formatters ---

func formatNeighborhoodMarkdown(nr *NeighborhoodResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Neighborhood: %s (depth %d)\n\n", nr.Center.Name, nr.Depth)
	fmt.Fprintf(&b, "Total nodes: %d, edges: %d\n\n", len(nr.Nodes), len(nr.Edges))
	writeMarkdownNodes(&b, "### Services", nr.Services)
	writeMarkdownNodes(&b, "### Symbols", nr.Symbols)
	writeMarkdownNodes(&b, "### Files", nr.Files)
	writeMarkdownNodes(&b, "### Invariants", nr.Invariants)
	writeMarkdownNodes(&b, "### Failure modes", nr.FailureModes)
	writeMarkdownNodes(&b, "### Tests", nr.Tests)
	writeMarkdownNodes(&b, "### Other", nr.Other)
	return b.String()
}

func formatNeighborhoodAgent(nr *NeighborhoodResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "center: %s (%s)\n", nr.Center.Name, nr.Center.Type)
	fmt.Fprintf(&b, "depth: %d\n", nr.Depth)
	fmt.Fprintf(&b, "total_nodes: %d\n", len(nr.Nodes))
	fmt.Fprintf(&b, "total_edges: %d\n", len(nr.Edges))
	writeAgentNodeList(&b, "services", nr.Services)
	writeAgentNodeList(&b, "symbols", nr.Symbols)
	writeAgentNodeList(&b, "invariants", nr.Invariants)
	writeAgentNodeList(&b, "failure_modes", nr.FailureModes)
	writeAgentNodeList(&b, "tests", nr.Tests)
	writeAgentNodeList(&b, "files", nr.Files)
	writeAgentNodeList(&b, "other", nr.Other)
	return b.String()
}

// --- Explanation formatters ---

func formatExplanationMarkdown(ex *Explanation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Explanation: %s\n\n", ex.Name)
	fmt.Fprintf(&b, "**Type**: `%s`  \n", ex.NodeType)
	fmt.Fprintf(&b, "**ID**: `%s`\n\n", ex.NodeID)
	fmt.Fprintf(&b, "### Role\n\n%s\n\n", ex.Role)
	writeMarkdownStrings(&b, "### Protects", ex.Protects)
	writeMarkdownStrings(&b, "### Risks", ex.Risks)
	if len(ex.Warnings) > 0 {
		b.WriteString("### Warnings\n\n")
		for _, w := range ex.Warnings {
			fmt.Fprintf(&b, "> %s\n", w)
		}
		b.WriteString("\n")
	}
	writeMarkdownStrings(&b, "### Required tests", ex.Tests)
	writeMarkdownStrings(&b, "### Recommended searches", ex.Searches)
	return b.String()
}

func formatExplanationAgent(ex *Explanation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "node_id: %s\n", ex.NodeID)
	fmt.Fprintf(&b, "node_type: %s\n", ex.NodeType)
	fmt.Fprintf(&b, "name: %s\n", ex.Name)
	fmt.Fprintf(&b, "role: %s\n", ex.Role)
	writeAgentList(&b, "protects", ex.Protects)
	writeAgentList(&b, "risks", ex.Risks)
	writeAgentList(&b, "warnings", ex.Warnings)
	writeAgentList(&b, "tests", ex.Tests)
	writeAgentList(&b, "searches", ex.Searches)
	return b.String()
}

// --- shared write helpers ---

func writeMarkdownStrings(b *strings.Builder, header string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\n", header)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
	b.WriteString("\n")
}

func writeMarkdownNodes(b *strings.Builder, header string, nodes []*graph.Node) {
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n\n", header)
	for _, n := range nodes {
		if n.Summary != "" {
			fmt.Fprintf(b, "- **%s** — %s\n", n.Name, n.Summary)
		} else {
			fmt.Fprintf(b, "- %s\n", n.Name)
		}
	}
	b.WriteString("\n")
}

func writeAgentList(b *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, strings.Join(items, "; "))
}

func writeAgentNodeList(b *strings.Builder, key string, nodes []*graph.Node) {
	if len(nodes) == 0 {
		return
	}
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	fmt.Fprintf(b, "%s: %s\n", key, strings.Join(names, "; "))
}

func writeAgentInvariants(b *strings.Builder, invs []graph.Invariant) {
	if len(invs) == 0 {
		return
	}
	ids := make([]string, 0, len(invs))
	for _, inv := range invs {
		ids = append(ids, inv.ID)
	}
	fmt.Fprintf(b, "related_invariants: %s\n", strings.Join(ids, "; "))
}

func writeAgentFailureModes(b *strings.Builder, fms []graph.FailureMode) {
	if len(fms) == 0 {
		return
	}
	ids := make([]string, 0, len(fms))
	for _, fm := range fms {
		ids = append(ids, fm.ID)
	}
	fmt.Fprintf(b, "related_failure_modes: %s\n", strings.Join(ids, "; "))
}

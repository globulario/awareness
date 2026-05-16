// Package checkedit provides a post-edit awareness check: given a file path,
// it collects forbidden fixes and code smells that apply to that file and
// surfaces them to the agent before the change is committed.
package checkedit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/graph"
)

// CheckEditResult is the output of a post-edit awareness check.
type CheckEditResult struct {
	File           string   `json:"file"`
	HasIssues      bool     `json:"has_issues"`
	ForbiddenFixes []string `json:"forbidden_fixes"`
	DesignPatterns []string `json:"design_patterns,omitempty"`
	AntiPatterns   []string `json:"anti_patterns,omitempty"`
	CodeSmells     []string `json:"code_smells"`
	Warnings       []string `json:"warnings"`
}

// Options configures a check-edit run.
type Options struct {
	File string // repo-relative path being edited
}

// Run performs a post-edit awareness check for the given file.
// Returns (result, nil) always — errors are surfaced as warnings inside the result.
func Run(ctx context.Context, g *graph.Graph, opts Options) (*CheckEditResult, error) {
	r := &CheckEditResult{File: opts.File}

	if g == nil {
		r.Warnings = append(r.Warnings, "no awareness graph — run 'globular awareness build' first")
		return r, nil
	}

	// Step 1: verify the file has a node in the graph and do transitive impact analysis.
	impact, err := analysis.ImpactByFile(ctx, g, opts.File)
	if err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("impact analysis: %v", err))
	}
	if impact == nil || impact.SourceFile == nil {
		r.Warnings = append(r.Warnings, "no graph node for this file — run 'globular awareness build' to index it")
	}

	// Step 2: collect forbidden fixes from the transitive impact closure.
	if impact != nil {
		for _, n := range impact.ForbiddenFixes {
			r.ForbiddenFixes = appendUniq(r.ForbiddenFixes, n.Name)
		}
	}

	// Step 3: collect invariant node IDs from the impact result, then get code smells.
	var invNodeIDs []string
	if impact != nil {
		for _, n := range impact.Invariants {
			invNodeIDs = append(invNodeIDs, n.ID)
		}
	}
	if len(invNodeIDs) > 0 {
		// Legacy pattern nodes (patterns.yaml).
		smells, err := g.CodeSmellsForInvariants(ctx, invNodeIDs)
		if err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("CodeSmellsForInvariants: %v", err))
		} else {
			r.CodeSmells = smells
		}
		// Design pattern layer (design_patterns.yaml).
		if dc, err := g.DesignContextForInvariants(ctx, invNodeIDs); err == nil {
			r.DesignPatterns = dc.DesignPatterns
			r.AntiPatterns = dc.AntiPatterns
			for _, s := range dc.CodeSmells {
				r.CodeSmells = appendUniq(r.CodeSmells, s)
			}
		}
	}

	r.HasIssues = len(r.ForbiddenFixes) > 0 || len(r.CodeSmells) > 0 || len(r.AntiPatterns) > 0

	return r, nil
}

// RenderCheckEdit formats a CheckEditResult as markdown (default), json, or agent text.
func RenderCheckEdit(r *CheckEditResult, format string) string {
	switch strings.ToLower(format) {
	case "json":
		return renderCheckEditJSON(r)
	case "agent":
		return renderCheckEditAgent(r)
	default:
		return renderCheckEditMarkdown(r)
	}
}

func renderCheckEditMarkdown(r *CheckEditResult) string {
	var sb strings.Builder
	sb.WriteString("# Awareness Check-Edit: " + r.File + "\n\n")

	if len(r.Warnings) > 0 {
		sb.WriteString("## Warnings\n\n")
		for _, w := range r.Warnings {
			sb.WriteString("> " + w + "\n")
		}
		sb.WriteString("\n")
	}

	if !r.HasIssues {
		sb.WriteString("No forbidden fixes, anti-patterns, or code smells detected for this file.\n")
		return sb.String()
	}

	if len(r.ForbiddenFixes) > 0 {
		sb.WriteString("## Forbidden fixes — do not apply\n\n")
		for _, ff := range r.ForbiddenFixes {
			sb.WriteString("- " + ff + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.DesignPatterns) > 0 {
		sb.WriteString("## Relevant design patterns\n\n")
		for _, p := range r.DesignPatterns {
			sb.WriteString("- " + p + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.AntiPatterns) > 0 {
		sb.WriteString("## Anti-patterns to avoid\n\n")
		for _, p := range r.AntiPatterns {
			sb.WriteString("- " + p + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.CodeSmells) > 0 {
		sb.WriteString("## Code smells to avoid\n\n")
		for _, s := range r.CodeSmells {
			sb.WriteString("- " + s + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func renderCheckEditAgent(r *CheckEditResult) string {
	if !r.HasIssues && len(r.Warnings) == 0 {
		return "CHECK-EDIT CLEAR: no known issues for " + r.File + "\n"
	}

	var sb strings.Builder
	sb.WriteString("CHECK-EDIT ALERT: " + r.File + "\n\n")

	for _, w := range r.Warnings {
		sb.WriteString("Warning: " + w + "\n")
	}

	if len(r.ForbiddenFixes) > 0 {
		sb.WriteString("\nForbidden fixes — do not apply:\n")
		for _, ff := range r.ForbiddenFixes {
			sb.WriteString("  - " + ff + "\n")
		}
	}

	if len(r.DesignPatterns) > 0 {
		sb.WriteString("\nRelevant design patterns:\n")
		for _, p := range r.DesignPatterns {
			sb.WriteString("  - " + p + "\n")
		}
	}

	if len(r.AntiPatterns) > 0 {
		sb.WriteString("\nAnti-patterns to avoid:\n")
		for _, p := range r.AntiPatterns {
			sb.WriteString("  - " + p + "\n")
		}
	}

	if len(r.CodeSmells) > 0 {
		sb.WriteString("\nCode smells to avoid in this file:\n")
		for _, s := range r.CodeSmells {
			sb.WriteString("  - " + s + "\n")
		}
	}

	return sb.String()
}

func renderCheckEditJSON(r *CheckEditResult) string {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

func appendUniq(slice []string, s string) []string {
	if s == "" {
		return slice
	}
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

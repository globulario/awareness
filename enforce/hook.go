package enforce

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// RunHook executes a pre-edit awareness hook for Claude Code.
// It returns a HookResult suitable for serialisation into the hook's stdout.
//
// The hook runs two passes:
//  1. Annotation validation on the specific files being edited.
//  2. AnnotationsForFile to surface invariants, forbidden fixes, and risks
//     that Claude must be aware of for those files.
//
// It never blocks on graph absence — it degrades to annotation-only output.
func RunHook(ctx context.Context, g *graph.Graph, files []string, task string) (*HookResult, error) {
	var findings []Finding

	// Pass 1: annotation well-formedness on the edited files.
	for _, f := range files {
		findings = append(findings, validateFileAnnotations(f, f)...)
	}

	// Pass 2: surface invariants and risks from the graph (if available).
	var contextLines []string
	if g != nil {
		for _, f := range files {
			ann, err := g.AnnotationsForFile(ctx, f)
			if err != nil {
				continue
			}
			for _, inv := range ann.Invariants {
				contextLines = append(contextLines, "INVARIANT: "+inv)
			}
			for _, fix := range ann.ForbiddenFixes {
				contextLines = append(contextLines, "FORBIDDEN FIX: "+fix)
			}
			for _, risk := range ann.Risks {
				contextLines = append(contextLines, "RISK: "+risk)
			}
			for _, st := range ann.StateTransitions {
				contextLines = append(contextLines, "STATE TRANSITION: "+st)
			}
		}
	}

	result := &AuditResult{}
	for _, f := range findings {
		switch f.Severity {
		case SeverityError:
			result.ErrorCount++
		case SeverityWarning:
			result.WarningCount++
		}
	}

	shouldBlock := result.ErrorCount > 0
	summary := buildHookSummary(task, files, findings, contextLines, shouldBlock)

	return &HookResult{
		HasFindings: len(findings) > 0 || len(contextLines) > 0,
		ShouldBlock: shouldBlock,
		Summary:     summary,
		Findings:    findings,
	}, nil
}

func buildHookSummary(task string, files []string, findings []Finding, contextLines []string, blocked bool) string {
	var sb strings.Builder

	if blocked {
		sb.WriteString("## Awareness hook: BLOCKED\n\n")
	} else {
		sb.WriteString("## Awareness hook: PASS\n\n")
	}

	if task != "" {
		sb.WriteString("**Task**: " + task + "\n\n")
	}

	if len(contextLines) > 0 {
		sb.WriteString("### Architecture constraints for edited files\n\n")
		for _, line := range contextLines {
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("\n")
	}

	if len(findings) > 0 {
		errorCount := 0
		for _, f := range findings {
			if f.Severity == SeverityError {
				errorCount++
			}
		}
		sb.WriteString(fmt.Sprintf("### Annotation findings (%d errors)\n\n", errorCount))
		for _, f := range findings {
			icon := "⚠"
			if f.Severity == SeverityError {
				icon = "✗"
			}
			loc := f.File
			if f.Symbol != "" {
				loc += " (" + f.Symbol + ")"
			}
			if loc != "" {
				sb.WriteString(icon + " [" + string(f.Severity) + "] " + loc + ": " + f.Message + "\n")
			} else {
				sb.WriteString(icon + " [" + string(f.Severity) + "] " + f.Message + "\n")
			}
		}
		sb.WriteString("\n")
	}

	if len(files) > 0 {
		sb.WriteString("**Files checked**: " + strings.Join(files, ", ") + "\n")
	}
	if task != "" {
		sb.WriteString("\n")
		sb.WriteString("Run before editing:\n")
		sb.WriteString("`globular awareness preflight --task \"" + task + "\" --format agent`\n")
	}

	return sb.String()
}

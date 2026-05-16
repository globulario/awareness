package failuregraph

import (
	"context"
	"fmt"
	"strings"
)

// ExplainCategory traverses the failure knowledge graph from a category node
// and returns a full FailureExplanation with causes, resolutions, wrong fixes,
// tests, and invariants.
func ExplainCategory(ctx context.Context, s *Store, categoryID string) (*FailureExplanation, error) {
	cat, err := s.LoadNode(ctx, categoryID)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: explain %s: %w", categoryID, err)
	}

	exp := &FailureExplanation{Category: *cat}

	exp.Symptoms, _ = s.NodesReachable(ctx, categoryID, EdgeObservedAs)
	exp.LikelyCauses, _ = s.NodesReachable(ctx, categoryID, EdgeCommonlyCausedBy)
	exp.WrongFixes, _ = s.NodesReachable(ctx, categoryID, EdgeAvoidFix)
	exp.RequiredTests, _ = s.NodesReachable(ctx, categoryID, EdgeClosureRequires)
	exp.RelatedInvariants, _ = s.NodesReachable(ctx, categoryID, EdgeViolates)

	// Resolutions: via causes → fixed_by
	for _, cause := range exp.LikelyCauses {
		resNodes, _ := s.NodesReachable(ctx, cause.ID, EdgeFixedBy)
		exp.Resolutions = append(exp.Resolutions, resNodes...)
	}

	exp.WorkflowModes, _ = s.LoadWorkflowModes(ctx, categoryID)
	exp.RecommendedAction = buildRecommendedAction(exp)
	return exp, nil
}

// buildRecommendedAction synthesizes a one-sentence recommended action from the explanation.
func buildRecommendedAction(exp *FailureExplanation) string {
	if len(exp.Resolutions) > 0 {
		return fmt.Sprintf("Apply the known resolution: %s", exp.Resolutions[0].Summary)
	}
	if len(exp.LikelyCauses) > 0 {
		return fmt.Sprintf("Investigate root cause: %s", exp.LikelyCauses[0].Summary)
	}
	if exp.Category.Summary != "" {
		return fmt.Sprintf("This matches category %s — %s", exp.Category.Name, exp.Category.Summary)
	}
	return "Review the matched failure category and consult linked incidents."
}

// ExplanationMarkdown renders a FailureExplanation as a compact markdown block
// suitable for inclusion in agent-context output.
func ExplanationMarkdown(exp FailureExplanation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", exp.Category.Name)
	if exp.Category.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", exp.Category.Summary)
	}
	if exp.Confidence != "" {
		fmt.Fprintf(&b, "Confidence: **%s** (score %.2f)\n\n", exp.Confidence, exp.Score)
	}
	if len(exp.LikelyCauses) > 0 {
		fmt.Fprintf(&b, "**Likely cause:**\n")
		for _, c := range exp.LikelyCauses {
			fmt.Fprintf(&b, "- %s\n", c.Summary)
		}
		fmt.Fprintln(&b)
	}
	if len(exp.Resolutions) > 0 {
		fmt.Fprintf(&b, "**Known resolution:**\n")
		for _, r := range exp.Resolutions {
			fmt.Fprintf(&b, "- %s\n", r.Summary)
		}
		fmt.Fprintln(&b)
	}
	if len(exp.WrongFixes) > 0 {
		fmt.Fprintf(&b, "**Wrong fixes to avoid:**\n")
		for _, w := range exp.WrongFixes {
			fmt.Fprintf(&b, "- %s\n", w.Summary)
		}
		fmt.Fprintln(&b)
	}
	if len(exp.RequiredTests) > 0 {
		fmt.Fprintf(&b, "**Required regression tests:**\n")
		for _, t := range exp.RequiredTests {
			fmt.Fprintf(&b, "- %s\n", t.Summary)
		}
		fmt.Fprintln(&b)
	}
	if exp.RecommendedAction != "" {
		fmt.Fprintf(&b, "**Recommended action:** %s\n", exp.RecommendedAction)
	}
	return b.String()
}

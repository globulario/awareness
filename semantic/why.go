package semantic

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/graph"
)

// WhyResult is the enriched explanation of why two nodes are semantically related.
type WhyResult struct {
	From                string        `json:"from"`
	To                  string        `json:"to"`
	Dimension           string        `json:"dimension"`
	Path                *SemanticPath `json:"path"`
	RelationshipSummary string        `json:"relationship_summary"`
	WhyItMatters        string        `json:"why_it_matters"`
	EditWarnings        []string      `json:"edit_warnings"`
	RequiredTests       []string      `json:"required_tests"`
	ForbiddenFixes      []string      `json:"forbidden_fixes"`
}

// WhyOptions controls WhyRelated.
type WhyOptions struct {
	Dimension      string
	MaxDepth       int
	MaxCost        float64
	IncludeRuntime bool
}

// WhyRelated finds the semantic path between two nodes and enriches it with
// relationship context, edit warnings, required tests, and forbidden fixes.
func WhyRelated(ctx context.Context, g *graph.Graph, fromID, toID string, opts WhyOptions) (*WhyResult, error) {
	dim := opts.Dimension
	if dim == "" {
		dim = DimensionAll
	}

	path, err := ShortestPath(ctx, g, fromID, toID, PathOptions{
		Dimension:      dim,
		MaxDepth:       opts.MaxDepth,
		MaxCost:        opts.MaxCost,
		IncludeRuntime: opts.IncludeRuntime,
	})
	if err != nil {
		return nil, fmt.Errorf("WhyRelated: %w", err)
	}

	result := &WhyResult{
		From:      fromID,
		To:        toID,
		Dimension: dim,
		Path:      path,
	}

	if !path.Found {
		result.RelationshipSummary = "No semantic path found between these nodes within the search constraints."
		return result, nil
	}

	// Resolve start and end node names.
	fromNode, _ := g.FindNode(ctx, fromID)
	toNode, _ := g.FindNode(ctx, toID)
	fromName := fromID
	toName := toID
	if fromNode != nil {
		fromName = fromNode.Name
	}
	if toNode != nil {
		toName = toNode.Name
	}

	n := len(path.Steps)

	// Mid-point step for summary.
	midIdx := n / 2
	midStep := PathStep{}
	if midIdx < n {
		midStep = path.Steps[midIdx]
	}

	result.RelationshipSummary = fmt.Sprintf(
		"%s is related to %s via %d-hop %s path. Key connection: %s through %s.",
		fromName, toName, n-1, dim, midStep.EdgeKind, midStep.NodeName,
	)

	// Collect special nodes from the path.
	var invNames []string
	var fmNames []string
	var ffNames []string
	var topEdge string
	hasInvariant := false
	hasFailureMode := false
	hasForbiddenFix := false

	if len(path.Steps) > 1 {
		topEdge = path.Steps[1].EdgeKind
	}

	for _, step := range path.Steps {
		switch step.NodeType {
		case graph.NodeTypeInvariant:
			hasInvariant = true
			invNames = append(invNames, step.NodeName)
		case graph.NodeTypeFailureMode:
			hasFailureMode = true
			fmNames = append(fmNames, step.NodeName)
		case graph.NodeTypeForbiddenFix:
			hasForbiddenFix = true
			ffNames = append(ffNames, step.NodeName)
		case graph.NodeTypeTest:
			result.RequiredTests = append(result.RequiredTests, step.NodeName)
		}
	}

	// Collect ForbiddenFixes.
	for _, ff := range ffNames {
		result.ForbiddenFixes = append(result.ForbiddenFixes, ff)
	}

	// Build WhyItMatters.
	switch {
	case hasInvariant && len(invNames) > 0:
		invName := invNames[0]
		inv, err := g.FindInvariant(ctx, invName)
		var guardDesc string
		if err == nil && inv != nil && inv.Summary != "" {
			guardDesc = inv.Summary
		} else {
			guardDesc = "a critical cluster invariant"
		}
		result.WhyItMatters = fmt.Sprintf(
			"Changing %s may affect the invariant %s, which %s.",
			fromName, invName, guardDesc,
		)

	case hasFailureMode && len(fmNames) > 0:
		fmName := fmNames[0]
		fm, _ := findFailureMode(ctx, g, fmName)
		var fmDesc string
		if fm != nil && fm.Summary != "" {
			fmDesc = fm.Summary
		} else {
			fmDesc = "is a known failure pattern in this cluster"
		}
		result.WhyItMatters = fmt.Sprintf(
			"This path connects through a known failure mode (%s), which %s.",
			fmName, fmDesc,
		)

	case hasForbiddenFix && len(ffNames) > 0:
		result.WhyItMatters = fmt.Sprintf(
			"A forbidden fix pattern is connected: %s.",
			ffNames[0],
		)

	default:
		result.WhyItMatters = fmt.Sprintf(
			"These nodes are connected through %s relationships in the %s dimension.",
			topEdge, dim,
		)
	}

	// EditWarnings: when there are invariants or failure modes in the path.
	if hasInvariant || hasFailureMode {
		result.EditWarnings = append(result.EditWarnings, "Read connected invariants before editing.")
	}

	return result, nil
}

// FindFailureMode looks up a failure mode by ID using the graph API.
func findFailureMode(ctx context.Context, g *graph.Graph, id string) (*graph.FailureMode, error) {
	fms, err := g.AllFailureModes(ctx)
	if err != nil {
		return nil, err
	}
	for _, fm := range fms {
		if fm.ID == id {
			return fm, nil
		}
	}
	return nil, nil
}

package debugsession

import (
	"context"
	"sort"

	awarectx "github.com/globulario/awareness/context"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/semantic"
)

const (
	maxStartsForPaths = 3
	maxPathsTotal     = 10
)

// buildRootCausePaths generates root-cause paths for the top starting nodes.
// Returns paths, suggested files, and suggested symbols extracted from node contexts.
func buildRootCausePaths(ctx context.Context, g *graph.Graph, starts []StartingNode, _ *preflight.Report) ([]RootCausePath, []string, []string) {
	var paths []RootCausePath
	var files, symbols []string
	seen := map[string]bool{}

	cap := maxStartsForPaths
	if len(starts) < cap {
		cap = len(starts)
	}

	semOpts := semantic.RelatedOptions{
		MaxResults: 5,
		MaxDepth:   4,
		MaxCost:    25,
	}

	for _, start := range starts[:cap] {
		// Find nearest failure modes from this starting node.
		fms, _ := semantic.Nearest(ctx, g, start.NodeID, graph.NodeTypeFailureMode, semOpts)
		for _, fm := range fms {
			if fm.Node == nil {
				continue
			}
			key := start.NodeID + "→fm→" + fm.Node.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			paths = append(paths, makeRootCausePath(ctx, g, start, fm, graph.NodeTypeFailureMode))
		}

		// Find nearest invariants from this starting node.
		invs, _ := semantic.Nearest(ctx, g, start.NodeID, graph.NodeTypeInvariant, semOpts)
		for _, inv := range invs {
			if inv.Node == nil {
				continue
			}
			key := start.NodeID + "→inv→" + inv.Node.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			paths = append(paths, makeRootCausePath(ctx, g, start, inv, graph.NodeTypeInvariant))
		}

		// Collect file and symbol hints from the node's local context.
		f, s := nodeContextHints(ctx, g, start.NodeID)
		files = append(files, f...)
		symbols = append(symbols, s...)
	}

	// Sort: critical severity first, then by ascending semantic cost.
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Severity == "critical" && paths[j].Severity != "critical" {
			return true
		}
		if paths[j].Severity == "critical" && paths[i].Severity != "critical" {
			return false
		}
		return paths[i].SemanticCost < paths[j].SemanticCost
	})

	if len(paths) > maxPathsTotal {
		paths = paths[:maxPathsTotal]
	}

	return paths, dedup(files), dedup(symbols)
}

// makeRootCausePath builds a RootCausePath for a (start, semantic result) pair.
func makeRootCausePath(ctx context.Context, g *graph.Graph, start StartingNode, rel semantic.SemanticRelated, targetType string) RootCausePath {
	// Compute full shortest path for a richer explanation.
	p, _ := semantic.ShortestPath(ctx, g, start.NodeID, rel.Node.ID, semantic.PathOptions{
		MaxDepth: 6,
		MaxCost:  30,
	})
	pathSummary := rel.PathSummary
	if p != nil && p.Found && p.Explanation != "" {
		pathSummary = p.Explanation
	}

	severity := "warning"
	whyItMatters := rel.Node.Summary

	if targetType == graph.NodeTypeInvariant {
		if inv, _ := g.FindInvariant(ctx, rel.Node.ID); inv != nil {
			if inv.Severity == "critical" {
				severity = "critical"
			}
			if inv.Summary != "" {
				whyItMatters = inv.Summary
			}
		}
	}

	// Collect forbidden fixes and required tests attached to the target node.
	var forbids, tests []string
	outEdges, _ := g.Neighbors(ctx, rel.Node.ID, "out")
	for _, e := range outEdges {
		switch e.Kind {
		case graph.EdgeForbids:
			if n, _ := g.FindNode(ctx, e.Dst); n != nil {
				forbids = append(forbids, n.Name)
			}
		case graph.EdgeTestedBy:
			if n, _ := g.FindNode(ctx, e.Dst); n != nil {
				tests = append(tests, n.Name)
			}
		}
	}

	return RootCausePath{
		StartNodeID:    start.NodeID,
		StartNodeName:  start.Name,
		TargetNodeID:   rel.Node.ID,
		TargetNodeName: rel.Node.Name,
		TargetNodeType: targetType,
		PathSummary:    pathSummary,
		SemanticCost:   rel.Distance,
		WhyItMatters:   whyItMatters,
		ForbiddenFixes: forbids,
		RequiredTests:  tests,
		Severity:       severity,
	}
}

// nodeContextHints extracts file paths and symbol names from a node's local context.
func nodeContextHints(ctx context.Context, g *graph.Graph, nodeID string) (files, symbols []string) {
	nc, err := awarectx.Build(ctx, g, nodeID, awarectx.Options{
		Zoom:     awarectx.ZoomLocal,
		MaxItems: 5,
		Depth:    1,
	})
	if err != nil {
		return nil, nil
	}
	if nc.Path != "" {
		files = append(files, nc.Path)
	}
	for _, e := range nc.OutgoingEdges {
		switch e.TargetType {
		case graph.NodeTypeSourceFile:
			if e.TargetID != "" {
				files = append(files, e.TargetID)
			}
		case graph.NodeTypeSymbol:
			if e.TargetName != "" {
				symbols = append(symbols, e.TargetName)
			}
		}
	}
	return
}

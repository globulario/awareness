package debugsession

import (
	"context"
	"strings"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// selectStartingNodes picks the best graph entry points for debugging.
// Priority: explicit files → preflight services → matched aliases → runtime evidence → semantic keyword search.
func selectStartingNodes(ctx context.Context, g *graph.Graph, opts Options, pf *preflight.Report) []StartingNode {
	var nodes []StartingNode
	seen := map[string]bool{}

	add := func(n StartingNode) {
		if n.NodeID != "" && !seen[n.NodeID] {
			seen[n.NodeID] = true
			nodes = append(nodes, n)
		}
	}

	// 1. Explicit file arguments.
	for _, f := range opts.Files {
		graphNodes, _ := g.FindNodesByPath(ctx, f)
		for _, gn := range graphNodes {
			add(StartingNode{
				NodeID: gn.ID, NodeType: gn.Type,
				Name: gn.Name, Path: gn.Path, Summary: gn.Summary,
				Source: "file",
			})
		}
	}

	// 2. Services resolved from preflight impact.
	for _, svc := range pf.Services {
		gn, _ := g.FindNodeByTypeAndName(ctx, graph.NodeTypeGlobularService, svc)
		if gn != nil {
			add(StartingNode{
				NodeID: gn.ID, NodeType: gn.Type,
				Name: gn.Name, Source: "preflight_impact",
			})
		}
	}

	// 3. Matched aliases → resolve to graph nodes.
	for _, alias := range pf.MatchedAliases {
		bareID := strings.TrimPrefix(alias, "invariant:")
		bareID = strings.TrimPrefix(bareID, "failure_mode:")
		bareID = strings.TrimPrefix(bareID, "service:")
		for _, ntype := range []string{
			graph.NodeTypeGlobularService,
			graph.NodeTypeInvariant,
			graph.NodeTypeFailureMode,
		} {
			gn, _ := g.FindNodeByTypeAndName(ctx, ntype, bareID)
			if gn != nil {
				add(StartingNode{
					NodeID: gn.ID, NodeType: gn.Type,
					Name: gn.Name, Source: "alias",
				})
				break
			}
		}
	}

	// 4. Runtime evidence — doctor findings, state-delta services, matched invariants.
	if pf.Runtime != nil {
		for _, df := range pf.Runtime.DoctorFindings {
			gn, _ := g.FindNodeByTypeAndName(ctx, graph.NodeTypeDoctorFinding, df.ID)
			if gn != nil {
				add(StartingNode{
					NodeID: gn.ID, NodeType: gn.Type,
					Name: gn.Name, Summary: df.Title, Source: "runtime",
				})
			}
		}
		for _, sd := range pf.Runtime.StateDeltas {
			gn, _ := g.FindNodeByTypeAndName(ctx, graph.NodeTypeGlobularService, sd.ServiceID)
			if gn != nil {
				add(StartingNode{
					NodeID: gn.ID, NodeType: gn.Type,
					Name: gn.Name, Source: "runtime",
				})
			}
		}
		for _, invName := range pf.Runtime.MatchedInvariants {
			gn, _ := g.FindNodeByTypeAndName(ctx, graph.NodeTypeInvariant, invName)
			if gn != nil {
				add(StartingNode{
					NodeID: gn.ID, NodeType: gn.Type,
					Name: gn.Name, Source: "runtime",
				})
			}
		}
	}

	// 5. Semantic keyword search — last resort when nothing resolved so far.
	if len(nodes) == 0 {
		for _, kw := range extractDebugKeywords(opts.Task) {
			results, _ := g.FindNodesByNameLike(ctx, kw)
			for _, gn := range results {
				if isUsefulStartType(gn.Type) {
					add(StartingNode{
						NodeID: gn.ID, NodeType: gn.Type,
						Name: gn.Name, Path: gn.Path, Source: "semantic",
					})
				}
				if len(nodes) >= 5 {
					break
				}
			}
			if len(nodes) >= 5 {
				break
			}
		}
	}

	return nodes
}

// extractDebugKeywords splits a task into meaningful tokens for graph search.
func extractDebugKeywords(task string) []string {
	words := strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '"' || r == '\''
	})
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "in": true,
		"on": true, "at": true, "to": true, "for": true, "and": true,
		"or": true, "not": true, "fix": true, "that": true, "this": true,
		"with": true, "when": true, "how": true, "why": true,
	}
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if len(w) < 4 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// isUsefulStartType returns true for node types that make good debug entry points.
func isUsefulStartType(t string) bool {
	switch t {
	case graph.NodeTypeGlobularService,
		graph.NodeTypeGoPackage,
		graph.NodeTypeSourceFile,
		graph.NodeTypeSymbol,
		graph.NodeTypeInvariant,
		graph.NodeTypeFailureMode,
		graph.NodeTypeEtcdKey,
		graph.NodeTypeWorkflow:
		return true
	}
	return false
}

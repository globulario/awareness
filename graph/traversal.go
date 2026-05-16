package graph

import (
	"context"
	"fmt"
)

// TraversalResult holds the nodes and edges discovered during a traversal.
type TraversalResult struct {
	Nodes []*Node
	Edges []Edge
}

// Traverse performs BFS from startID up to maxDepth hops.
// If edgeKinds is non-empty, only those edge kinds are followed.
// The start node itself is included in Nodes regardless of depth.
func (g *Graph) Traverse(ctx context.Context, startID string, maxDepth int, edgeKinds []string) (*TraversalResult, error) {
	kindSet := make(map[string]bool, len(edgeKinds))
	for _, k := range edgeKinds {
		kindSet[k] = true
	}

	visited := make(map[string]bool)
	result := &TraversalResult{}

	type item struct {
		id    string
		depth int
	}
	queue := []item{{startID, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur.id] {
			continue
		}
		visited[cur.id] = true

		node, err := g.FindNode(ctx, cur.id)
		if err != nil {
			return nil, err
		}
		if node != nil {
			result.Nodes = append(result.Nodes, node)
		}

		if cur.depth >= maxDepth {
			continue
		}

		edges, err := g.Neighbors(ctx, cur.id, "out")
		if err != nil {
			return nil, fmt.Errorf("Traverse neighbors %s: %w", cur.id, err)
		}

		for _, e := range edges {
			if len(kindSet) > 0 && !kindSet[e.Kind] {
				continue
			}
			result.Edges = append(result.Edges, e)
			if !visited[e.Dst] {
				queue = append(queue, item{e.Dst, cur.depth + 1})
			}
		}
	}

	return result, nil
}

// ImpactByFile finds all nodes impacted by or protecting the source_file at filePath.
// It collects:
//   - Nodes reachable via outgoing edges from the file (depth 6).
//   - Nodes that protect/enforce this file via incoming edges (invariants, etc.).
//   - Nodes reachable outward from those protecting nodes (depth 3).
func (g *Graph) ImpactByFile(ctx context.Context, filePath string) (*TraversalResult, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE type = ? AND path = ?
	`, NodeTypeSourceFile, filePath)
	if err != nil {
		return nil, fmt.Errorf("ImpactByFile: %w", err)
	}
	fileNodes, err := scanNodes(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	combined := &TraversalResult{}
	visited := make(map[string]bool)

	addNodes := func(nodes []*Node) {
		for _, n := range nodes {
			if !visited[n.ID] {
				visited[n.ID] = true
				combined.Nodes = append(combined.Nodes, n)
			}
		}
	}

	for _, fn := range fileNodes {
		// Outgoing traversal: symbols, packages, invariants reachable from file.
		outRes, err := g.Traverse(ctx, fn.ID, 6, nil)
		if err != nil {
			return nil, err
		}
		addNodes(outRes.Nodes)
		combined.Edges = append(combined.Edges, outRes.Edges...)

		// Incoming traversal: find what protects/enforces this file.
		inEdges, err := g.Neighbors(ctx, fn.ID, "in")
		if err != nil {
			return nil, fmt.Errorf("ImpactByFile incoming: %w", err)
		}
		for _, e := range inEdges {
			combined.Edges = append(combined.Edges, e)
			if visited[e.Src] {
				continue
			}
			// Collect the protecting node and traverse outward from it (shallow).
			protRes, err := g.Traverse(ctx, e.Src, 3, nil)
			if err != nil {
				return nil, err
			}
			addNodes(protRes.Nodes)
			combined.Edges = append(combined.Edges, protRes.Edges...)
		}
	}

	return combined, nil
}

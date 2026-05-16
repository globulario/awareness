package awarectx

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/graph"
)

// NeighborhoodResult holds the BFS neighborhood of a node up to a given depth.
type NeighborhoodResult struct {
	Center *graph.Node `json:"center"`
	Depth  int         `json:"depth"`

	// All reachable nodes (including center).
	Nodes []*graph.Node `json:"nodes"`
	// All edges traversed.
	Edges []graph.Edge `json:"edges"`

	// Nodes partitioned by type for quick access.
	Symbols      []*graph.Node `json:"symbols"`
	Files        []*graph.Node `json:"files"`
	Services     []*graph.Node `json:"services"`
	Invariants   []*graph.Node `json:"invariants"`
	FailureModes []*graph.Node `json:"failure_modes"`
	Tests        []*graph.Node `json:"tests"`
	Other        []*graph.Node `json:"other"`
}

// Neighborhood performs a BFS from nodeID up to depth hops in all directions.
// The result partitions discovered nodes by type.
func Neighborhood(ctx context.Context, g *graph.Graph, nodeID string, depth int) (*NeighborhoodResult, error) {
	if depth <= 0 {
		depth = 1
	}

	center, err := g.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if center == nil {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	nr := &NeighborhoodResult{Center: center, Depth: depth}

	// BFS in both directions up to depth.
	visited := map[string]bool{nodeID: true}
	edgeSeen := map[string]bool{}
	type item struct {
		id    string
		depth int
	}
	queue := []item{{nodeID, 0}}
	nr.Nodes = append(nr.Nodes, center)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth >= depth {
			continue
		}

		edges, err := g.Neighbors(ctx, cur.id, "both")
		if err != nil {
			return nil, fmt.Errorf("neighborhood neighbors %s: %w", cur.id, err)
		}
		for _, e := range edges {
			eKey := e.Src + "|" + e.Kind + "|" + e.Dst
			if !edgeSeen[eKey] {
				edgeSeen[eKey] = true
				nr.Edges = append(nr.Edges, e)
			}

			other := e.Dst
			if other == cur.id {
				other = e.Src
			}
			if !visited[other] {
				visited[other] = true
				n, err := g.FindNode(ctx, other)
				if err != nil {
					return nil, err
				}
				if n != nil {
					nr.Nodes = append(nr.Nodes, n)
					queue = append(queue, item{other, cur.depth + 1})
				}
			}
		}
	}

	// Partition by type.
	for _, n := range nr.Nodes {
		if n.ID == nodeID {
			continue
		}
		switch n.Type {
		case graph.NodeTypeSymbol:
			nr.Symbols = append(nr.Symbols, n)
		case graph.NodeTypeSourceFile:
			nr.Files = append(nr.Files, n)
		case graph.NodeTypeGlobularService:
			nr.Services = append(nr.Services, n)
		case graph.NodeTypeInvariant:
			nr.Invariants = append(nr.Invariants, n)
		case graph.NodeTypeFailureMode:
			nr.FailureModes = append(nr.FailureModes, n)
		case graph.NodeTypeTest:
			nr.Tests = append(nr.Tests, n)
		default:
			nr.Other = append(nr.Other, n)
		}
	}

	return nr, nil
}

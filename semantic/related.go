package semantic

import (
	"container/heap"
	"context"
	"fmt"
	"sort"

	"github.com/globulario/awareness/graph"
)

// SemanticRelated is a node ranked by semantic distance from a query node.
type SemanticRelated struct {
	Node        *graph.Node `json:"node"`
	Distance    float64     `json:"distance"`
	Reason      string      `json:"reason"`       // edge kind used to arrive
	PathSummary string      `json:"path_summary"` // brief path description
	Dimension   string      `json:"dimension"`
	Confidence  float64     `json:"confidence"`
}

// RelatedOptions controls Related and Nearest queries.
type RelatedOptions struct {
	Dimension         string
	TargetTypes       []string
	MaxResults        int
	MaxDepth          int
	MaxCost           float64
	IncludeRuntime    bool
	IncludeProvenance bool
}

// Related returns nodes reachable from nodeID, ranked by semantic distance.
// If TargetTypes is non-empty, only nodes of those types are returned.
func Related(ctx context.Context, g *graph.Graph, nodeID string, opts RelatedOptions) ([]SemanticRelated, error) {
	dim, maxDepth, maxCost, maxResults := applyRelatedDefaults(opts)

	targetSet := make(map[string]bool, len(opts.TargetTypes))
	for _, t := range opts.TargetTypes {
		targetSet[t] = true
	}

	type visitRecord struct {
		dist     float64
		edgeKind string
		prevID   string
		prevName string
	}

	dist := map[string]float64{nodeID: 0}
	record := map[string]visitRecord{nodeID: {}}
	visited := map[string]bool{}
	depthMap := map[string]int{nodeID: 0}

	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{nodeID: nodeID, cost: 0, depth: 0})

	var collected []SemanticRelated

	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*pqItem)
		if visited[cur.nodeID] {
			continue
		}
		visited[cur.nodeID] = true

		// Collect if this node matches target types (and it's not the start node).
		if cur.nodeID != nodeID {
			// Look up the node.
			n, err := g.FindNode(ctx, cur.nodeID)
			if err != nil {
				return nil, err
			}
			if n != nil && (len(targetSet) == 0 || targetSet[n.Type]) {
				rec := record[cur.nodeID]
				ps := buildPathSummary(rec.prevName, rec.edgeKind, n.Name)
				nodeCost := cur.cost

				// Severity boost for critical invariants.
				effectiveDist := nodeCost
				if n.Type == graph.NodeTypeInvariant {
					inv, _ := g.FindInvariant(ctx, n.ID)
					if inv != nil && inv.Severity == "critical" {
						effectiveDist -= 0.5
					}
				}
				if effectiveDist < 0 {
					effectiveDist = 0
				}

				collected = append(collected, SemanticRelated{
					Node:        n,
					Distance:    effectiveDist,
					Reason:      rec.edgeKind,
					PathSummary: ps,
					Dimension:   dim,
					Confidence:  1.0,
				})
			}
		}

		if cur.depth >= maxDepth {
			continue
		}

		edges, err := g.Neighbors(ctx, cur.nodeID, "both")
		if err != nil {
			return nil, fmt.Errorf("Related neighbors %s: %w", cur.nodeID, err)
		}

		for _, e := range edges {
			var neighbourID, edgeKind string
			if e.Src == cur.nodeID {
				neighbourID = e.Dst
				edgeKind = e.Kind
			} else {
				neighbourID = e.Src
				edgeKind = e.Kind
			}

			if visited[neighbourID] {
				continue
			}

			if !opts.IncludeRuntime {
				n, _ := g.FindNode(ctx, neighbourID)
				if n != nil && runtimeNodeTypes[n.Type] {
					continue
				}
			}

			w := EdgeWeight(dim, e)
			newCost := cur.cost + w
			if newCost > maxCost {
				continue
			}

			prevCost, seen := dist[neighbourID]
			if !seen || newCost < prevCost {
				dist[neighbourID] = newCost
				depthMap[neighbourID] = cur.depth + 1

				// Record who reached this node and via what edge.
				curNode, _ := g.FindNode(ctx, cur.nodeID)
				prevName := cur.nodeID
				if curNode != nil {
					prevName = curNode.Name
				}
				record[neighbourID] = visitRecord{
					dist:     newCost,
					edgeKind: edgeKind,
					prevID:   cur.nodeID,
					prevName: prevName,
				}
				heap.Push(pq, &pqItem{nodeID: neighbourID, cost: newCost, depth: cur.depth + 1})
			}
		}
	}

	// Sort by effective distance (already stored in Distance after severity boost).
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Distance < collected[j].Distance
	})

	if maxResults > 0 && len(collected) > maxResults {
		collected = collected[:maxResults]
	}
	return collected, nil
}

// Nearest is a convenience wrapper: Related with a single target type.
func Nearest(ctx context.Context, g *graph.Graph, nodeID, targetType string, opts RelatedOptions) ([]SemanticRelated, error) {
	opts.TargetTypes = []string{targetType}
	return Related(ctx, g, nodeID, opts)
}

// SemanticNeighborhood returns ranked related nodes up to a given depth, across all target types.
// The start node itself is excluded.
func SemanticNeighborhood(ctx context.Context, g *graph.Graph, nodeID string, opts RelatedOptions) ([]SemanticRelated, error) {
	opts.TargetTypes = nil // all types
	results, err := Related(ctx, g, nodeID, opts)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// ---- helpers ----

func applyRelatedDefaults(opts RelatedOptions) (dim string, maxDepth int, maxCost float64, maxResults int) {
	dim = opts.Dimension
	if dim == "" {
		dim = DimensionAll
	}
	maxDepth = opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 4
	}
	maxCost = opts.MaxCost
	if maxCost <= 0 {
		maxCost = 20
	}
	maxResults = opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	return
}

func buildPathSummary(prevName, edgeKind, nodeName string) string {
	if prevName == "" || edgeKind == "" {
		return nodeName
	}
	return fmt.Sprintf("%s --%s--> %s", prevName, edgeKind, nodeName)
}

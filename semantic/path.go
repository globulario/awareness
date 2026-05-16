package semantic

import (
	"container/heap"
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// PathStep is one hop in a semantic path.
type PathStep struct {
	NodeID   string  `json:"node_id"`
	NodeName string  `json:"node_name"`
	NodeType string  `json:"node_type"`
	EdgeKind string  `json:"edge_kind"` // edge used to arrive (empty for first step)
	EdgeDir  string  `json:"edge_dir"`  // "→" or "←" (direction of traversal)
	Cost     float64 `json:"cost"`      // cumulative cost to reach this node
}

// SemanticPath is the result of finding a weighted shortest path between two nodes.
type SemanticPath struct {
	From        string     `json:"from"`
	To          string     `json:"to"`
	TotalCost   float64    `json:"total_cost"`
	Steps       []PathStep `json:"steps"`
	Explanation string     `json:"explanation"`
	Dimension   string     `json:"dimension"`
	Found       bool       `json:"found"`
	Truncated   bool       `json:"truncated"` // true if search was capped
}

// PathOptions controls ShortestPath behaviour.
type PathOptions struct {
	Dimension      string
	MaxDepth       int
	MaxCost        float64
	AvoidWeakEdges bool // skip edges with base cost >= 6
	IncludeRuntime bool
}

// ---- priority queue ----

type pqItem struct {
	nodeID string
	cost   float64
	depth  int
	index  int // position in the heap
}

type priorityQueue []*pqItem

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].cost < pq[j].cost }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *priorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*pqItem)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[:n-1]
	return item
}

// ---- predecessor record ----

type predRecord struct {
	prev     string
	edgeKind string
	edgeDir  string
	stepCost float64 // cost of the edge hop (not cumulative)
}

// ---- runtime-related node types ----

var runtimeNodeTypes = map[string]bool{
	graph.NodeTypeRuntimeSnapshot:     true,
	graph.NodeTypeRuntimeServiceStatus: true,
	graph.NodeTypeWorkflowReceipt:     true,
	graph.NodeTypeStateDelta:          true,
	graph.NodeTypeRepositoryStatus:    true,
	graph.NodeTypeObjectstoreStatus:   true,
	graph.NodeTypeXDSStatus:           true,
	graph.NodeTypeSystemdStatus:       true,
	graph.NodeTypeDoctorEvidence:      true,
	graph.NodeTypeRuntimeState:        true,
}

// ShortestPath finds the lowest-cost semantic path from fromID to toID.
// Uses Dijkstra over both outgoing and incoming edges.
// If no path is found within constraints, returns SemanticPath{Found: false}.
func ShortestPath(ctx context.Context, g *graph.Graph, fromID, toID string, opts PathOptions) (*SemanticPath, error) {
	// Apply defaults.
	dim := opts.Dimension
	if dim == "" {
		dim = DimensionAll
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 6
	}
	if maxDepth > 8 {
		maxDepth = 8
	}
	maxCost := opts.MaxCost
	if maxCost <= 0 {
		maxCost = 30
	}
	if maxCost > 50 {
		maxCost = 50
	}

	const nodeVisitCap = 5000

	dist := map[string]float64{fromID: 0}
	prev := map[string]predRecord{}
	depth := map[string]int{fromID: 0}
	visited := map[string]bool{}
	truncated := false

	pq := &priorityQueue{}
	heap.Init(pq)
	heap.Push(pq, &pqItem{nodeID: fromID, cost: 0, depth: 0})

	for pq.Len() > 0 {
		if len(visited) >= nodeVisitCap {
			truncated = true
			break
		}

		cur := heap.Pop(pq).(*pqItem)
		if visited[cur.nodeID] {
			continue
		}
		visited[cur.nodeID] = true

		if cur.nodeID == toID {
			break
		}
		if cur.depth >= maxDepth {
			continue
		}

		// Fetch all edges (both directions) from the current node.
		edges, err := g.Neighbors(ctx, cur.nodeID, "both")
		if err != nil {
			return nil, fmt.Errorf("ShortestPath neighbors %s: %w", cur.nodeID, err)
		}

		for _, e := range edges {
			// Determine traversal direction and the neighbour node ID.
			var neighbourID, edgeDir string
			if e.Src == cur.nodeID {
				neighbourID = e.Dst
				edgeDir = "→"
			} else {
				neighbourID = e.Src
				edgeDir = "←"
			}

			if visited[neighbourID] {
				continue
			}

			// Skip runtime evidence nodes unless opted-in.
			if !opts.IncludeRuntime {
				n, _ := g.FindNode(ctx, neighbourID)
				if n != nil && runtimeNodeTypes[n.Type] {
					continue
				}
			}

			// AvoidWeakEdges: skip if base weight >= 6.
			if opts.AvoidWeakEdges {
				bw, ok := baseWeights[e.Kind]
				if !ok {
					bw = 10
				}
				if bw >= 6 {
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
				depth[neighbourID] = cur.depth + 1
				prev[neighbourID] = predRecord{
					prev:     cur.nodeID,
					edgeKind: e.Kind,
					edgeDir:  edgeDir,
					stepCost: w,
				}
				heap.Push(pq, &pqItem{nodeID: neighbourID, cost: newCost, depth: cur.depth + 1})
			}
		}
	}

	// Check if we reached the target.
	if _, reached := dist[toID]; !reached {
		return &SemanticPath{
			Found:     false,
			From:      fromID,
			To:        toID,
			Dimension: dim,
			Truncated: truncated,
		}, nil
	}

	// Reconstruct the path.
	path := reconstructPath(fromID, toID, prev, dist)

	// Enrich with node names/types.
	steps, err := enrichSteps(ctx, g, path)
	if err != nil {
		return nil, err
	}

	explanation := buildExplanation(steps)

	return &SemanticPath{
		From:        fromID,
		To:          toID,
		TotalCost:   dist[toID],
		Steps:       steps,
		Explanation: explanation,
		Dimension:   dim,
		Found:       true,
		Truncated:   truncated,
	}, nil
}

// rawStep is an intermediate representation during reconstruction.
type rawStep struct {
	nodeID   string
	edgeKind string
	edgeDir  string
	cumCost  float64
}

func reconstructPath(fromID, toID string, prev map[string]predRecord, dist map[string]float64) []rawStep {
	var reversed []rawStep
	cur := toID
	for cur != fromID {
		rec, ok := prev[cur]
		if !ok {
			break
		}
		reversed = append(reversed, rawStep{
			nodeID:   cur,
			edgeKind: rec.edgeKind,
			edgeDir:  rec.edgeDir,
			cumCost:  dist[cur],
		})
		cur = rec.prev
	}
	// Add the start node.
	reversed = append(reversed, rawStep{nodeID: fromID, cumCost: 0})

	// Reverse.
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func enrichSteps(ctx context.Context, g *graph.Graph, raw []rawStep) ([]PathStep, error) {
	steps := make([]PathStep, 0, len(raw))
	for i, r := range raw {
		n, err := g.FindNode(ctx, r.nodeID)
		if err != nil {
			return nil, err
		}
		name := r.nodeID
		ntype := ""
		if n != nil {
			name = n.Name
			ntype = n.Type
		}
		edgeKind := ""
		edgeDir := ""
		if i > 0 {
			edgeKind = r.edgeKind
			edgeDir = r.edgeDir
		}
		steps = append(steps, PathStep{
			NodeID:   r.nodeID,
			NodeName: name,
			NodeType: ntype,
			EdgeKind: edgeKind,
			EdgeDir:  edgeDir,
			Cost:     r.cumCost,
		})
	}
	return steps, nil
}

func buildExplanation(steps []PathStep) string {
	if len(steps) == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, steps[0].NodeName)
	for i := 1; i < len(steps); i++ {
		s := steps[i]
		parts = append(parts, fmt.Sprintf("--%s-->", s.EdgeKind), s.NodeName)
	}
	return strings.Join(parts, " ")
}

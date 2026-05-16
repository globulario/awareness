package integrity

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// ImpactStep is one hop in an impact path from a changed file to an affected node.
type ImpactStep struct {
	NodeID    string `json:"node_id"`
	NodeType  string `json:"node_type"`
	NodeName  string `json:"node_name"`
	Predicate string `json:"predicate"` // edge kind
	Trust     string `json:"trust"`     // trust level of this hop
}

// ImpactPath is a typed chain from a changed file to an impacted invariant, test, or failure mode.
type ImpactPath struct {
	ChangedFile string       `json:"changed_file"`
	Steps       []ImpactStep `json:"steps"`
	Confidence  string       `json:"confidence"` // "high" | "medium" | "low"
	Note        string       `json:"note,omitempty"`
}

// ImpactPathQuery is the input for TraverseImpactPaths.
type ImpactPathQuery struct {
	ChangedFiles []string `json:"changed_files"`
	MaxDepth     int      `json:"max_depth"`
}

// impactEdgeKinds are the edge types that represent meaningful impact paths.
var impactEdgeKinds = map[string]bool{
	graph.EdgeImplements:    true,
	graph.EdgeTestedBy:      true,
	graph.EdgeRequiresTest:  true,
	graph.EdgeProtects:      true,
	graph.EdgeEnforces:      true,
	graph.EdgeAffects:       true,
	graph.EdgeForbids:       true,
	graph.EdgeFixes:         true,
	graph.EdgeVerifiedBy:    true,
	graph.EdgeTouchesFile:   true,
	graph.EdgeTouchesSymbol: true,
	graph.EdgeObserves:      true,
	graph.EdgeConfigures:    true,
	graph.EdgeMayAffect:     true,
	// Invariant implementation graph edges.
	graph.EdgePartiallyImplements: true,
	graph.EdgeVerifies:            true,
	graph.EdgeViolates:            true,
}

// highValueNodeTypes are node types that represent meaningful impact targets.
var highValueNodeTypes = map[string]bool{
	graph.NodeTypeInvariant:    true,
	graph.NodeTypeFailureMode:  true,
	graph.NodeTypeTest:         true,
	graph.NodeTypeFixCase:      true,
	graph.NodeTypeForbiddenFix: true,
	graph.NodeTypeGuardrail:    true,
}

// TraverseImpactPaths performs Kevin Bacon-style typed traversal from each
// changed file, returning paths to high-value nodes (invariants, tests, fix cases).
// Only typed edges are followed; vague "related_to" edges are labelled low-confidence.
func TraverseImpactPaths(ctx context.Context, g *graph.Graph, q ImpactPathQuery) ([]ImpactPath, error) {
	if g == nil {
		return nil, fmt.Errorf("graph is required for impact path traversal")
	}
	maxDepth := q.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 6
	}

	var paths []ImpactPath

	for _, file := range q.ChangedFiles {
		filePaths, err := findFileNodes(ctx, g, file)
		if err != nil {
			return nil, fmt.Errorf("impact path for %s: %w", file, err)
		}
		if len(filePaths) == 0 {
			paths = append(paths, ImpactPath{
				ChangedFile: file,
				Steps:       nil,
				Confidence:  "low",
				Note:        fmt.Sprintf("no graph node found for file %q — run 'globular awareness build' to index it", file),
			})
			continue
		}

		for _, fileNodeID := range filePaths {
			discovered := traverseTyped(ctx, g, fileNodeID, maxDepth)
			for _, chain := range discovered {
				if len(chain) == 0 {
					continue
				}
				conf := chainConfidence(chain)
				paths = append(paths, ImpactPath{
					ChangedFile: file,
					Steps:       chain,
					Confidence:  conf,
				})
			}
		}
	}

	return paths, nil
}

// findFileNodes finds all source_file nodes whose path matches the given file.
func findFileNodes(ctx context.Context, g *graph.Graph, filePath string) ([]string, error) {
	nodes, err := g.FindNodesByPath(ctx, filePath)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == graph.NodeTypeSourceFile {
			ids = append(ids, n.ID)
		}
	}
	// Also try by filename suffix if no exact match.
	if len(ids) == 0 {
		allFiles, _ := g.FindNodesByType(ctx, graph.NodeTypeSourceFile)
		for _, n := range allFiles {
			if strings.HasSuffix(n.Path, filePath) || strings.HasSuffix(filePath, n.Path) {
				ids = append(ids, n.ID)
			}
		}
	}
	return ids, nil
}

// traverseTyped performs BFS from startID over typed edges only, collecting
// paths to high-value nodes. Returns a list of step chains.
func traverseTyped(ctx context.Context, g *graph.Graph, startID string, maxDepth int) [][]ImpactStep {
	type state struct {
		nodeID string
		chain  []ImpactStep
	}

	var results [][]ImpactStep
	visited := map[string]bool{startID: true}
	queue := []state{{nodeID: startID, chain: nil}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if len(cur.chain) >= maxDepth {
			continue
		}

		edges, err := g.OutgoingEdges(ctx, cur.nodeID)
		if err != nil {
			continue
		}

		for _, e := range edges {
			if visited[e.Dst] {
				continue
			}

			dst, err := g.FindNode(ctx, e.Dst)
			if err != nil || dst == nil {
				continue
			}

			trust := edgeTrust(e.Kind, impactEdgeKinds[e.Kind])
			step := ImpactStep{
				NodeID:    dst.ID,
				NodeType:  dst.Type,
				NodeName:  dst.Name,
				Predicate: e.Kind,
				Trust:     trust,
			}
			newChain := append(append([]ImpactStep{}, cur.chain...), step)

			if highValueNodeTypes[dst.Type] {
				results = append(results, newChain)
			}

			visited[e.Dst] = true
			queue = append(queue, state{nodeID: e.Dst, chain: newChain})
		}
	}

	return results
}

// edgeTrust returns the trust level for a traversal step.
// Typed edges (in impactEdgeKinds) are "declared"; untyped edges are "inferred".
func edgeTrust(kind string, isTyped bool) string {
	if !isTyped {
		return TrustInferred
	}
	switch kind {
	case graph.EdgeVerifiedBy, graph.EdgeTestedBy, graph.EdgeVerifies:
		// verifies is a direct test-proof edge — same trust as tested_by.
		return TrustVerified
	case graph.EdgeImplements, graph.EdgeFixes, graph.EdgeProtects, graph.EdgeEnforces:
		return TrustDeclared
	case graph.EdgeConfigures, graph.EdgeObserves:
		return TrustDeclared
	case graph.EdgePartiallyImplements, graph.EdgeBlocksForbiddenAction:
		// Declared in YAML (protects.files and forbidden_fixes respectively).
		return TrustDeclared
	case graph.EdgeReadsAuthority, graph.EdgeWritesState, graph.EdgeGuardsAction:
		// May come from YAML (declared) or AST extraction (inferred).
		// Default to inferred — the YAML loader carries metadata trust_level
		// which callers can use to promote if needed.
		return TrustInferred
	case graph.EdgeMayAffect:
		return TrustInferred
	default:
		return TrustDeclared
	}
}

// chainConfidence returns the overall confidence of an impact path.
// Paths with inferred edges are low-confidence.
func chainConfidence(chain []ImpactStep) string {
	hasInferred := false
	for _, s := range chain {
		if s.Trust == TrustInferred {
			hasInferred = true
		}
	}
	if hasInferred {
		return "low"
	}
	if len(chain) <= 2 {
		return "high"
	}
	return "medium"
}

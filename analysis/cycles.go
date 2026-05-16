package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// CycleClassification describes the safety of a detected cycle.
type CycleClassification string

const (
	CycleSafe      CycleClassification = "SAFE"
	CycleWarning   CycleClassification = "WARNING"
	CycleDangerous CycleClassification = "DANGEROUS"
)

// dangerousPhases are phases where a required cycle is classified DANGEROUS.
var dangerousPhases = map[string]bool{
	"recovery":        true,
	"bootstrap":       true,
	"bootstrap_recovery": true,
	"package_install": true,
	"reconcile":       true,
}

// Cycle is a detected dependency cycle in the graph.
type Cycle struct {
	// Path is the sequence of node IDs forming the cycle (first == last).
	Path           []string
	Phase          string
	AllRequired    bool
	Classification CycleClassification
	Reason         string
}

// FindCycles detects cycles in depends_on edges.
// If phase is non-empty, only edges with that phase are considered.
// Only required=true edges are classified as dangerous.
func FindCycles(ctx context.Context, g *graph.Graph, phase string) ([]Cycle, error) {
	edges, err := g.EdgesByKind(ctx, graph.EdgeDependsOn)
	if err != nil {
		return nil, fmt.Errorf("FindCycles: %w", err)
	}

	// Build adjacency list filtered by phase.
	type edgeInfo struct {
		dst      string
		required bool
		phase    string
	}
	adj := make(map[string][]edgeInfo)
	for _, e := range edges {
		if phase != "" && e.Phase != phase {
			continue
		}
		adj[e.Src] = append(adj[e.Src], edgeInfo{e.Dst, e.Required, e.Phase})
	}

	var cycles []Cycle
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	stackPath := []string{}
	// Track edge metadata along the path: required flag per hop.
	stackRequired := []bool{}

	var dfs func(node string)
	dfs = func(node string) {
		if visited[node] {
			return
		}
		inStack[node] = true
		stackPath = append(stackPath, node)
		stackRequired = append(stackRequired, false) // placeholder

		for _, e := range adj[node] {
			if inStack[e.dst] {
				// Found a back edge — extract the cycle.
				cycleStart := -1
				for i, id := range stackPath {
					if id == e.dst {
						cycleStart = i
						break
					}
				}
				if cycleStart < 0 {
					continue
				}
				cyclePath := make([]string, len(stackPath)-cycleStart+1)
				copy(cyclePath, stackPath[cycleStart:])
				cyclePath[len(cyclePath)-1] = e.dst // close the loop

				// Gather required flags for each hop in this cycle.
				allRequired := e.required
				for i := cycleStart; i < len(stackRequired)-1; i++ {
					if !stackRequired[i] {
						allRequired = false
						break
					}
				}

				cycles = append(cycles, classifyCycle(cyclePath, e.phase, allRequired, e.required))
				continue
			}
			if !visited[e.dst] {
				// Record required flag for this hop.
				idx := len(stackPath) - 1
				if idx >= 0 && idx < len(stackRequired) {
					stackRequired[idx] = e.required
				}
				dfs(e.dst)
			}
		}

		stackPath = stackPath[:len(stackPath)-1]
		stackRequired = stackRequired[:len(stackRequired)-1]
		inStack[node] = false
		visited[node] = true
	}

	for node := range adj {
		if !visited[node] {
			dfs(node)
		}
	}

	// Deduplicate cycles by canonical path.
	return dedupCycles(cycles), nil
}

func classifyCycle(path []string, phase string, allRequired, lastRequired bool) Cycle {
	c := Cycle{
		Path:        path,
		Phase:       phase,
		AllRequired: allRequired,
	}

	if !allRequired && !lastRequired {
		c.Classification = CycleSafe
		c.Reason = "All edges in cycle are optional; no required dependency loop."
		return c
	}

	if dangerousPhases[phase] {
		c.Classification = CycleDangerous
		c.Reason = fmt.Sprintf("Required cycle detected in dangerous phase %q. "+
			"This may cause a deadlock during %s.", phase, phase)
		return c
	}

	c.Classification = CycleWarning
	c.Reason = fmt.Sprintf("Required cycle in phase %q. Not in a known dangerous phase, "+
		"but may still cause convergence issues.", phase)
	return c
}

// dedupCycles removes duplicate cycles that represent the same rotation.
func dedupCycles(cycles []Cycle) []Cycle {
	seen := make(map[string]bool)
	var out []Cycle
	for _, c := range cycles {
		key := canonicalCycleKey(c.Path)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// canonicalCycleKey produces a deterministic string key for a cycle path
// by rotating to start at the lexicographically smallest node.
func canonicalCycleKey(path []string) string {
	if len(path) == 0 {
		return ""
	}
	// path[0] == path[last] — work with the interior nodes.
	nodes := path[:len(path)-1]
	if len(nodes) == 0 {
		return ""
	}
	minIdx := 0
	for i, n := range nodes {
		if n < nodes[minIdx] {
			minIdx = i
		}
	}
	rotated := append(nodes[minIdx:], nodes[:minIdx]...)
	return strings.Join(rotated, "→")
}

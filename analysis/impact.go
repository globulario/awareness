// Package analysis provides graph-based impact analysis, cycle detection,
// and agent context generation. No LLM calls — all matching is graph traversal
// and keyword matching against manually declared truth.
package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
)

// ImpactResult collects nodes reachable from a source file, partitioned by type.
type ImpactResult struct {
	SourceFile    *graph.Node
	Symbols       []*graph.Node
	Services      []*graph.Node
	Invariants    []*graph.Node
	FailureModes  []*graph.Node
	ForbiddenFixes []*graph.Node
	Tests         []*graph.Node
	Other         []*graph.Node
}

// ImpactByFile finds all nodes impacted by changes to the file at filePath,
// then partitions them by type into an ImpactResult.
func ImpactByFile(ctx context.Context, g *graph.Graph, filePath string) (*ImpactResult, error) {
	res, err := g.ImpactByFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("ImpactByFile %s: %w", filePath, err)
	}

	result := &ImpactResult{}
	seen := make(map[string]bool)

	for _, n := range res.Nodes {
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true

		switch n.Type {
		case graph.NodeTypeSourceFile:
			if n.Path == filePath {
				result.SourceFile = n
			}
		case graph.NodeTypeSymbol:
			result.Symbols = append(result.Symbols, n)
		case graph.NodeTypeGlobularService:
			result.Services = append(result.Services, n)
		case graph.NodeTypeInvariant:
			result.Invariants = append(result.Invariants, n)
		case graph.NodeTypeFailureMode:
			result.FailureModes = append(result.FailureModes, n)
		case graph.NodeTypeForbiddenFix:
			result.ForbiddenFixes = append(result.ForbiddenFixes, n)
		case graph.NodeTypeTest:
			result.Tests = append(result.Tests, n)
		default:
			result.Other = append(result.Other, n)
		}
	}

	return result, nil
}

// ExplainedFinding is a single impacted node with its graph path trace.
type ExplainedFinding struct {
	NodeID     string
	NodeType   string
	NodeName   string
	Severity   string
	Mandatory  bool     // true if reached via required/enforces/implements edges or is a ForbiddenFix
	EdgePath   []string // human-readable hops: "file → implements → invariant → forbids → forbidden_fix"
	Confidence string   // high/medium/low
	Source     string   // "invariant:xxx" or "" if direct
}

// ExplainedImpactResult is the full explained impact output.
type ExplainedImpactResult struct {
	File           string
	Invariants     []ExplainedFinding
	ForbiddenFixes []ExplainedFinding
	RequiredTests  []ExplainedFinding
	FailureModes   []ExplainedFinding
	MissingLinks   []string // suggested edges not yet in graph
}

// ExplainImpactByFile returns an explained impact result with full graph path traces.
// It calls TraverseImpactPaths and builds ExplainedFinding entries for each high-value
// terminal node, deduplicating by NodeID (keeping shortest path).
func ExplainImpactByFile(ctx context.Context, g *graph.Graph, filePath string) (*ExplainedImpactResult, error) {
	q := integrity.ImpactPathQuery{
		ChangedFiles: []string{filePath},
		MaxDepth:     6,
	}
	paths, err := integrity.TraverseImpactPaths(ctx, g, q)
	if err != nil {
		return nil, fmt.Errorf("ExplainImpactByFile %s: %w", filePath, err)
	}

	result := &ExplainedImpactResult{File: filePath}

	// Track best (shortest) path per NodeID.
	bestByID := make(map[string]ExplainedFinding)

	for _, p := range paths {
		if len(p.Steps) == 0 {
			continue
		}

		terminal := p.Steps[len(p.Steps)-1]

		// Build human-readable edge path.
		edgePath := buildEdgePath(filePath, p.Steps)

		// Determine mandatory: ForbiddenFix is always mandatory;
		// paths containing enforces/requires_test/implements are also mandatory.
		mandatory := terminal.NodeType == graph.NodeTypeForbiddenFix
		if !mandatory {
			for _, s := range p.Steps {
				if s.Predicate == graph.EdgeEnforces ||
					s.Predicate == graph.EdgeRequiresTest ||
					s.Predicate == graph.EdgeImplements {
					mandatory = true
					break
				}
			}
		}

		// Find the invariant source (first invariant in the path).
		source := ""
		for _, s := range p.Steps {
			if s.NodeType == graph.NodeTypeInvariant {
				source = "invariant:" + s.NodeName
				break
			}
		}

		finding := ExplainedFinding{
			NodeID:     terminal.NodeID,
			NodeType:   terminal.NodeType,
			NodeName:   terminal.NodeName,
			Mandatory:  mandatory,
			EdgePath:   edgePath,
			Confidence: p.Confidence,
			Source:     source,
		}

		// Deduplicate — keep the shortest path (fewer hops = clearer).
		if existing, ok := bestByID[terminal.NodeID]; !ok || len(p.Steps) < len(existing.EdgePath) {
			bestByID[terminal.NodeID] = finding
		}
	}

	// Partition findings by node type.
	for _, f := range bestByID {
		switch f.NodeType {
		case graph.NodeTypeInvariant:
			result.Invariants = append(result.Invariants, f)
		case graph.NodeTypeForbiddenFix:
			result.ForbiddenFixes = append(result.ForbiddenFixes, f)
		case graph.NodeTypeTest:
			result.RequiredTests = append(result.RequiredTests, f)
		case graph.NodeTypeFailureMode:
			result.FailureModes = append(result.FailureModes, f)
		}
	}

	// Phase 6: missing-link detection.
	// When no paths are found, suggest why based on file location.
	if len(paths) == 0 {
		result.MissingLinks = detectMissingLinks(filePath)
	}

	return result, nil
}

// detectMissingLinks suggests why a file has no graph edges and what to add.
// These are recommendations only — they do not mutate the graph.
func detectMissingLinks(filePath string) []string {
	var links []string

	// File not indexed at all — most common cause.
	links = append(links, fmt.Sprintf(
		"no graph edges found from %q — run 'globular awareness build --clean' to index this file", filePath))

	// Pattern-based suggestions for well-known high-risk areas.
	type suggestion struct {
		pattern  string
		edgeKind string
		suggest  string
	}

	suggestions := []suggestion{
		{"golang/awareness/", "implements", "This file is in the awareness package. Add it to an invariant's 'files:' list in docs/awareness/invariants.yaml so impact traversal can reach the rules it implements."},
		{"golang/mcp/tools_awareness", "implements", "This is an awareness MCP tool. Add it to awareness.mcp.* invariants in docs/awareness/invariants.yaml with 'files:' so decision_context changes are governed."},
		{"golang/globularcli/awareness", "implements", "This is the awareness CLI. Add it to an awareness invariant's 'files:' list in docs/awareness/invariants.yaml."},
		{"docs/awareness/knowledge/", "configures", "This is an awareness knowledge YAML file. Add it to an invariant's 'configures_files:' list in docs/awareness/invariants.yaml so edits trigger the graph rebuild requirement."},
		{"docs/awareness/invariants.yaml", "configures", "invariants.yaml configures the whole awareness graph. It should appear in 'configures_files:' of awareness.knowledge.graph_rebuild_after_yaml_edit."},
		{"golang/cluster_controller/", "implements", "This is a cluster controller file. Check docs/awareness/invariants.yaml for controller invariants and add this file to the relevant 'files:' or 'enforces_files:' list."},
		{"golang/node_agent/", "implements", "This is a node agent file. Check docs/awareness/invariants.yaml for node agent invariants and add this file to the relevant 'files:' list."},
		{"golang/workflow/", "implements", "This is a workflow engine file. Check for workflow-related invariants and add this file to their 'files:' list."},
	}

	for _, s := range suggestions {
		if strings.Contains(filePath, s.pattern) {
			links = append(links, fmt.Sprintf(
				"missing_link: %q appears to %s an invariant but has no '%s' edge. Suggestion: %s",
				filePath, s.edgeKind, s.edgeKind, s.suggest))
			break
		}
	}

	return links
}

// buildEdgePath constructs a human-readable path string from the impact steps.
// Format: "file →[edge]→ node1 →[edge]→ node2"
func buildEdgePath(startFile string, steps []integrity.ImpactStep) []string {
	if len(steps) == 0 {
		return nil
	}
	parts := make([]string, 0, len(steps)+1)
	prev := startFile
	for _, s := range steps {
		parts = append(parts, fmt.Sprintf("%s →[%s]→ %s", prev, s.Predicate, s.NodeName))
		prev = s.NodeName
	}
	// Return the full chain as a single string (split into hops for clarity).
	return []string{strings.Join(parts, " | ")}
}

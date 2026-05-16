package fixledger

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/graph"
)

// ExtractFixCases loads fix_cases.yaml from fixCasesPath and creates fix_case
// nodes and edges in the awareness graph.
//
// For each fix case:
//   - Creates a "fix_case:<id>" node.
//   - For each target_invariant: edge fix_case → invariant (EdgeFixes or EdgePartiallyFixes).
//   - For each fixed_file: edge fix_case → source_file (EdgeTouchesFile).
//   - For each remaining_file: edge fix_case → source_file (EdgeStillMissing).
//   - For each required_test: edge fix_case → test (EdgeRequiresTest).
func ExtractFixCases(ctx context.Context, g *graph.Graph, fixCasesPath string) error {
	cases, err := LoadFixCases(fixCasesPath)
	if err != nil {
		return fmt.Errorf("ExtractFixCases: %w", err)
	}

	for _, fc := range cases {
		nodeID := "fix_case:" + fc.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypeFixCase,
			Name:    fc.ID,
			Summary: fc.Title,
			Metadata: map[string]any{
				"status":   string(fc.Status),
				"category": fc.Category,
				"pattern":  fc.Pattern,
			},
		}); err != nil {
			return fmt.Errorf("ExtractFixCases: add node %s: %w", nodeID, err)
		}

		// Edges to target invariants.
		edgeKind := graph.EdgeFixes
		if fc.Status == FixPartial {
			edgeKind = graph.EdgePartiallyFixes
		}
		for _, invID := range fc.TargetInvariants {
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: edgeKind,
				Dst:  "invariant:" + invID,
			}); err != nil {
				return fmt.Errorf("ExtractFixCases: edge %s →[%s]→ invariant:%s: %w", nodeID, edgeKind, invID, err)
			}
		}

		// Edges to fixed files.
		for _, file := range fc.FixedFiles {
			fileNodeID := "source_file:" + file
			_ = g.AddNode(ctx, graph.Node{
				ID:   fileNodeID,
				Type: graph.NodeTypeSourceFile,
				Name: file,
				Path: file,
			})
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeTouchesFile,
				Dst:  fileNodeID,
			}); err != nil {
				return fmt.Errorf("ExtractFixCases: edge %s touches_file %s: %w", nodeID, file, err)
			}
		}

		// Edges to remaining (not yet fixed) files.
		for _, file := range fc.RemainingFiles {
			fileNodeID := "source_file:" + file
			_ = g.AddNode(ctx, graph.Node{
				ID:   fileNodeID,
				Type: graph.NodeTypeSourceFile,
				Name: file,
				Path: file,
			})
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeStillMissing,
				Dst:  fileNodeID,
			}); err != nil {
				return fmt.Errorf("ExtractFixCases: edge %s still_missing %s: %w", nodeID, file, err)
			}
		}

		// Edges to required tests.
		for _, testName := range fc.RequiredTests {
			testNodeID := "test:" + testName
			_ = g.AddNode(ctx, graph.Node{
				ID:   testNodeID,
				Type: graph.NodeTypeTest,
				Name: testName,
			})
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeRequiresTest,
				Dst:  testNodeID,
			}); err != nil {
				return fmt.Errorf("ExtractFixCases: edge %s requires_test %s: %w", nodeID, testName, err)
			}
		}
	}

	return nil
}

// ExtractGuardrails loads guardrails.yaml from guardrailsPath and creates
// guardrail nodes and edges in the awareness graph.
func ExtractGuardrails(ctx context.Context, g *graph.Graph, guardrailsPath string) error {
	guardrails, err := LoadGuardrails(guardrailsPath)
	if err != nil {
		return fmt.Errorf("ExtractGuardrails: %w", err)
	}

	for _, gr := range guardrails {
		nodeID := "guardrail:" + gr.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypeGuardrail,
			Name:    gr.ID,
			Summary: gr.Title,
			Metadata: map[string]any{
				"status":   string(gr.Status),
				"priority": gr.Priority,
				"category": gr.Category,
			},
		}); err != nil {
			return fmt.Errorf("ExtractGuardrails: add node %s: %w", nodeID, err)
		}

		// Edges to required fix cases.
		for _, fixID := range gr.RequiredFixes {
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeImplementsGuardrail,
				Dst:  "fix_case:" + fixID,
			}); err != nil {
				return fmt.Errorf("ExtractGuardrails: edge %s implements_guardrail fix_case:%s: %w", nodeID, fixID, err)
			}
		}
	}

	return nil
}

// ExtractMarkdownGuardrails parses a guardrails.md file and creates fix_case
// nodes in the awareness graph for each parsed section.
func ExtractMarkdownGuardrails(ctx context.Context, g *graph.Graph, mdPath string) error {
	sections, err := ParseMarkdownFixCases(mdPath)
	if err != nil {
		return fmt.Errorf("ExtractMarkdownGuardrails: %w", err)
	}

	for _, s := range sections {
		if s.ID == "" || s.Title == "" {
			continue
		}
		nodeID := "fix_case:md." + s.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypeFixCase,
			Name:    s.ID,
			Summary: s.Title,
			Metadata: map[string]any{
				"status": string(s.Status),
				"source": "guardrails.md",
			},
		}); err != nil {
			return fmt.Errorf("ExtractMarkdownGuardrails: add node %s: %w", nodeID, err)
		}

		for _, testName := range s.Tests {
			testNodeID := "test:" + testName
			_ = g.AddNode(ctx, graph.Node{
				ID:   testNodeID,
				Type: graph.NodeTypeTest,
				Name: testName,
			})
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  nodeID,
				Kind: graph.EdgeRequiresTest,
				Dst:  testNodeID,
			}); err != nil {
				return fmt.Errorf("ExtractMarkdownGuardrails: edge requires_test %s: %w", testName, err)
			}
		}
	}

	return nil
}

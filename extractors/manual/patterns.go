package manual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

type patternsFile struct {
	Patterns []yamlPattern `yaml:"patterns"`
}

type yamlPattern struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Definition        string   `yaml:"definition"`
	Detect            string   `yaml:"detect"`
	CodeSmells        []string `yaml:"code_smells"`
	FailureModes      []string `yaml:"failure_modes"` // free-text descriptions — stored in metadata only
	SafeFixRule       string   `yaml:"safe_fix_rule"`
	RelatedInvariants []string `yaml:"related_invariants"`
}

// LoadPatterns loads docs/awareness/patterns.yaml into the graph as NodeTypePattern nodes.
// Missing file is silently skipped.
func LoadPatterns(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadPatterns: read %s: %w", path, err)
	}

	var f patternsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadPatterns: parse %s: %w", path, err)
	}

	for _, p := range f.Patterns {
		if p.ID == "" {
			continue
		}

		smellsJSON, _ := json.Marshal(p.CodeSmells)
		fmJSON, _ := json.Marshal(p.FailureModes)

		nodeID := "pattern:" + p.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypePattern,
			Name:    p.ID,
			Summary: p.Definition,
			Metadata: map[string]any{
				"title":          p.Title,
				"definition":     p.Definition,
				"detect":         p.Detect,
				"code_smells":    json.RawMessage(smellsJSON),
				"failure_modes":  json.RawMessage(fmJSON),
				"safe_fix_rule":  p.SafeFixRule,
			},
		}); err != nil {
			return fmt.Errorf("LoadPatterns: add node %s: %w", nodeID, err)
		}

		// Link to related invariants.
		for _, invID := range p.RelatedInvariants {
			if invID == "" {
				continue
			}
			invNodeID := "invariant:" + invID
			// EnsureNode (not AddNode): canonical invariant loader owns the
			// metadata; the patterns loader only needs the edge target.
			if err := g.EnsureNode(ctx, graph.Node{
				ID:      invNodeID,
				Type:    graph.NodeTypeInvariant,
				Name:    invID,
				Summary: "(stub — populated by invariant loader)",
			}); err != nil {
				return fmt.Errorf("LoadPatterns: stub invariant %s: %w", invNodeID, err)
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:        nodeID,
				Kind:       graph.EdgeRequires,
				Dst:        invNodeID,
				Confidence: 0.9,
			}); err != nil {
				return fmt.Errorf("LoadPatterns: edge %s→%s: %w", nodeID, invNodeID, err)
			}
		}
	}
	return nil
}

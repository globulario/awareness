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

type causalRulesFile struct {
	CausalRules []yamlCausalRule `yaml:"causal_rules"`
}

type yamlCausalRule struct {
	ID                  string             `yaml:"id"`
	RootSignal          string             `yaml:"root_signal"`
	TriggerKeywords     []string           `yaml:"trigger_keywords"`
	Sequence            []yamlCausalEvent  `yaml:"sequence"`
	Confidence          string             `yaml:"confidence"`
	ExplanationTemplate string             `yaml:"explanation_template"`
	RecommendedFixOrder []string           `yaml:"recommended_fix_order"`
}

type yamlCausalEvent struct {
	Event     string   `yaml:"event"`
	Component string   `yaml:"component"`
	Keywords  []string `yaml:"keywords"`
}

// LoadCausalRules loads causal_rules.yaml into the graph as NodeTypeLearningRule nodes.
// Missing file is silently skipped.
func LoadCausalRules(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadCausalRules: read %s: %w", path, err)
	}

	var f causalRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadCausalRules: parse %s: %w", path, err)
	}

	for _, r := range f.CausalRules {
		if r.ID == "" {
			continue
		}
		if err := loadCausalRule(ctx, g, r); err != nil {
			return fmt.Errorf("LoadCausalRules %s: %w", r.ID, err)
		}
	}
	return nil
}

func loadCausalRule(ctx context.Context, g *graph.Graph, r yamlCausalRule) error {
	nodeID := "causal_rule:" + r.ID

	keywordsJSON, _ := json.Marshal(r.TriggerKeywords)
	sequenceJSON, _ := json.Marshal(r.Sequence)
	fixOrderJSON, _ := json.Marshal(r.RecommendedFixOrder)

	if err := g.AddNode(ctx, graph.Node{
		ID:      nodeID,
		Type:    graph.NodeTypeLearningRule,
		Name:    r.ID,
		Summary: r.ExplanationTemplate,
		Metadata: map[string]any{
			"root_signal":           r.RootSignal,
			"trigger_keywords":      json.RawMessage(keywordsJSON),
			"confidence":            r.Confidence,
			"sequence":              json.RawMessage(sequenceJSON),
			"recommended_fix_order": json.RawMessage(fixOrderJSON),
		},
	}); err != nil {
		return err
	}

	// Link to each unique component mentioned in the sequence via EdgeAffects.
	seen := make(map[string]bool)
	for _, event := range r.Sequence {
		if event.Component == "" || seen[event.Component] {
			continue
		}
		seen[event.Component] = true
		svcID := "service:" + event.Component
		if err := g.AddNode(ctx, graph.Node{
			ID:      svcID,
			Type:    graph.NodeTypeGlobularService,
			Name:    event.Component,
			Summary: "(stub — populated by service loader)",
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeAffects, Dst: svcID}); err != nil {
			return err
		}
	}

	return nil
}

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

type decisionRulesFile struct {
	DecisionRules []yamlDecisionRule `yaml:"decision_rules"`
}

type yamlDecisionRule struct {
	ID                        string   `yaml:"id"`
	Title                     string   `yaml:"title"`
	Severity                  string   `yaml:"severity"`
	SourceIncidents           []string `yaml:"source_incidents"`
	Trigger                   []string `yaml:"trigger"`
	Decision                  string   `yaml:"decision"`
	ForbiddenIf               []string `yaml:"forbidden_if"`
	RequiredEvidenceBefore    []string `yaml:"required_evidence_before"`
	RequiredVerificationAfter []string `yaml:"required_verification_after"`
	ForbiddenFixes            []string `yaml:"forbidden_fixes"`
	RelatedInvariants         []string `yaml:"related_invariants"`
	RequiredTests             []string `yaml:"required_tests"`
}

// LoadDecisionRules loads decision_rules.yaml into the graph as NodeTypeDesignRule nodes.
// Missing file is silently skipped.
func LoadDecisionRules(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadDecisionRules: read %s: %w", path, err)
	}

	var f decisionRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadDecisionRules: parse %s: %w", path, err)
	}

	for _, r := range f.DecisionRules {
		if r.ID == "" {
			continue
		}
		if err := loadDecisionRule(ctx, g, r); err != nil {
			return fmt.Errorf("LoadDecisionRules %s: %w", r.ID, err)
		}
	}
	return nil
}

func loadDecisionRule(ctx context.Context, g *graph.Graph, r yamlDecisionRule) error {
	nodeID := "decision_rule:" + r.ID

	triggerJSON, _ := json.Marshal(r.Trigger)
	forbiddenIfJSON, _ := json.Marshal(r.ForbiddenIf)
	evidenceJSON, _ := json.Marshal(r.RequiredEvidenceBefore)
	verifyJSON, _ := json.Marshal(r.RequiredVerificationAfter)

	if err := g.AddNode(ctx, graph.Node{
		ID:      nodeID,
		Type:    graph.NodeTypeDesignRule,
		Name:    r.ID,
		Summary: r.Decision,
		Metadata: map[string]any{
			"title":                       r.Title,
			"severity":                    r.Severity,
			"trigger":                     json.RawMessage(triggerJSON),
			"forbidden_if":                json.RawMessage(forbiddenIfJSON),
			"required_evidence_before":    json.RawMessage(evidenceJSON),
			"required_verification_after": json.RawMessage(verifyJSON),
		},
	}); err != nil {
		return err
	}

	for _, inc := range r.SourceIncidents {
		if inc == "" {
			continue
		}
		incID := "incident:" + inc
		if err := g.AddNode(ctx, graph.Node{
			ID:   incID,
			Type: graph.NodeTypeIncident,
			Name: inc,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeDerivedFrom, Dst: incID}); err != nil {
			return err
		}
	}

	for _, fix := range r.ForbiddenFixes {
		if fix == "" {
			continue
		}
		fixID := "forbidden_fix:" + fix
		if err := g.AddNode(ctx, graph.Node{
			ID:   fixID,
			Type: graph.NodeTypeForbiddenFix,
			Name: fix,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeForbids, Dst: fixID}); err != nil {
			return err
		}
	}

	for _, inv := range r.RelatedInvariants {
		if inv == "" {
			continue
		}
		invID := "invariant:" + inv
		if err := g.AddNode(ctx, graph.Node{
			ID:      invID,
			Type:    graph.NodeTypeInvariant,
			Name:    inv,
			Summary: "(stub — populated by invariant loader)",
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeRequires, Dst: invID}); err != nil {
			return err
		}
	}

	for _, test := range r.RequiredTests {
		if test == "" {
			continue
		}
		testID := "test:" + test
		if err := g.AddNode(ctx, graph.Node{
			ID:   testID,
			Type: graph.NodeTypeTest,
			Name: test,
		}); err != nil {
			return err
		}
		if err := g.AddEdge(ctx, graph.Edge{Src: nodeID, Kind: graph.EdgeTestedBy, Dst: testID}); err != nil {
			return err
		}
	}

	return nil
}

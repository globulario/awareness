package manual_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

const causalRulesYAML = `
causal_rules:
  - id: etcd_pressure_to_workflow_timeout
    root_signal: etcd_disk_pressure
    trigger_keywords:
      - NOSPACE
      - database space exceeded
    sequence:
      - event: etcd_nospace
        component: etcd
        keywords: [NOSPACE, database space]
      - event: workflow_dispatch_timeout
        component: workflow
        keywords: [dispatch timeout, context deadline]
    confidence: medium
    explanation_template: "etcd disk pressure destabilized control-plane, causing workflow dispatch failures."
    recommended_fix_order:
      - etcdctl alarm list
      - compact revision history
`

func TestLoadCausalRulesCreatesLearningRuleNode(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	path := writeYAML(t, t.TempDir(), "causal_rules.yaml", causalRulesYAML)
	if err := manual.LoadCausalRules(ctx, g, path); err != nil {
		t.Fatalf("LoadCausalRules: %v", err)
	}

	node, err := g.FindNode(ctx, "causal_rule:etcd_pressure_to_workflow_timeout")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if node == nil {
		t.Fatal("node not found")
	}
	if node.Type != graph.NodeTypeLearningRule {
		t.Errorf("type = %q, want %q", node.Type, graph.NodeTypeLearningRule)
	}
}

func TestLoadCausalRulesLinksComponentsViaAffects(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	path := writeYAML(t, t.TempDir(), "causal_rules.yaml", causalRulesYAML)
	if err := manual.LoadCausalRules(ctx, g, path); err != nil {
		t.Fatalf("LoadCausalRules: %v", err)
	}

	edges, err := g.OutgoingEdges(ctx, "causal_rule:etcd_pressure_to_workflow_timeout")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}

	want := map[string]bool{
		"affects:service:etcd":     false,
		"affects:service:workflow": false,
	}
	for _, e := range edges {
		key := e.Kind + ":" + e.Dst
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("missing edge %s", key)
		}
	}
}

func TestLoadCausalRulesDeduplicatesComponents(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	yaml := `
causal_rules:
  - id: repeat_component_rule
    root_signal: some_signal
    trigger_keywords: [foo]
    sequence:
      - event: first
        component: etcd
        keywords: [foo]
      - event: second
        component: etcd
        keywords: [bar]
    confidence: high
    explanation_template: "test"
    recommended_fix_order: []
`
	path := writeYAML(t, t.TempDir(), "causal_rules.yaml", yaml)
	if err := manual.LoadCausalRules(ctx, g, path); err != nil {
		t.Fatalf("LoadCausalRules: %v", err)
	}

	edges, err := g.OutgoingEdges(ctx, "causal_rule:repeat_component_rule")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}

	count := 0
	for _, e := range edges {
		if e.Kind == graph.EdgeAffects && e.Dst == "service:etcd" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 affects edge to etcd, got %d", count)
	}
}

func TestLoadCausalRulesMissingFileSkipped(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)
	if err := manual.LoadCausalRules(ctx, g, "/nonexistent/causal_rules.yaml"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

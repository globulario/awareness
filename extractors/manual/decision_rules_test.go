package manual_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

const decisionRulesYAML = `
decision_rules:
  - id: test_rule_orphan_kill
    title: Kill orphan before restart
    severity: critical
    source_incidents:
      - INC-2026-0002
    trigger:
      - address already in use
    decision: Is the port held by an orphaned process?
    forbidden_if:
      - restarting without confirming port is free
    required_evidence_before:
      - ss -tlnp | grep <port>
    required_verification_after:
      - port bound by new PID
    forbidden_fixes:
      - relying_on_systemd_to_kill_processes_outside_the_cgroup
    related_invariants:
      - service.endpoint.cgroup_escape_guard
    required_tests:
      - TestOrphanKilledBeforeServiceRestart
`

func TestLoadDecisionRulesCreatesDesignRuleNode(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	path := writeYAML(t, t.TempDir(), "decision_rules.yaml", decisionRulesYAML)
	if err := manual.LoadDecisionRules(ctx, g, path); err != nil {
		t.Fatalf("LoadDecisionRules: %v", err)
	}

	node, err := g.FindNode(ctx, "decision_rule:test_rule_orphan_kill")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if node == nil {
		t.Fatal("node not found")
	}
	if node.Type != graph.NodeTypeDesignRule {
		t.Errorf("type = %q, want %q", node.Type, graph.NodeTypeDesignRule)
	}
}

func TestLoadDecisionRulesEdges(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	path := writeYAML(t, t.TempDir(), "decision_rules.yaml", decisionRulesYAML)
	if err := manual.LoadDecisionRules(ctx, g, path); err != nil {
		t.Fatalf("LoadDecisionRules: %v", err)
	}

	edges, err := g.OutgoingEdges(ctx, "decision_rule:test_rule_orphan_kill")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}

	want := map[string]bool{
		"forbids:forbidden_fix:relying_on_systemd_to_kill_processes_outside_the_cgroup": false,
		"requires:invariant:service.endpoint.cgroup_escape_guard":                       false,
		"tested_by:test:TestOrphanKilledBeforeServiceRestart":                           false,
		"derived_from:incident:INC-2026-0002":                                           false,
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

func TestLoadDecisionRulesMissingFileSkipped(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)
	if err := manual.LoadDecisionRules(ctx, g, "/nonexistent/decision_rules.yaml"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

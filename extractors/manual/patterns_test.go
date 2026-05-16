package manual_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

func TestLoadPatternsTwoPatternsWithInvariants(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "patterns.yaml", `
patterns:
  - id: restart_storm
    title: Restart Storm Anti-Pattern
    definition: Repeatedly restarting a service on non-transient failure.
    detect: "restart count in systemd journal exceeds 3 within 60s"
    code_smells:
      - "calling Restart() without exponential backoff"
      - "catching all errors and retrying"
    failure_modes:
      - "services never stabilise"
    safe_fix_rule: "Add circuit-breaker before retry"
    related_invariants:
      - convergence.no_infinite_retry
      - infra.restart_gate
  - id: inline_state_change
    title: Inline State Change Anti-Pattern
    definition: Mutating cluster state outside of a workflow.
    detect: "etcd write not inside a workflow step"
    code_smells:
      - "direct etcd.Put in controller handler"
    failure_modes:
      - "state drift not auditable"
    safe_fix_rule: "Route mutations through Workflow Service"
    related_invariants:
      - workflow.all_mutations_gated
`)

	if err := manual.LoadPatterns(ctx, g, dir+"/patterns.yaml"); err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}

	// Two pattern nodes exist.
	patterns, err := g.FindNodesByType(ctx, graph.NodeTypePattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Errorf("want 2 pattern nodes, got %d", len(patterns))
	}

	// Pattern node IDs use "pattern:" prefix.
	node, err := g.FindNode(ctx, "pattern:restart_storm")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil {
		t.Fatal("pattern:restart_storm not found")
	}
	if node.Type != graph.NodeTypePattern {
		t.Errorf("wrong type: %s", node.Type)
	}

	// Related invariant stubs were created.
	for _, id := range []string{
		"invariant:convergence.no_infinite_retry",
		"invariant:infra.restart_gate",
		"invariant:workflow.all_mutations_gated",
	} {
		n, err := g.FindNode(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if n == nil {
			t.Errorf("stub invariant node %q not created", id)
		}
	}

	// Edges created: 2 invariants for restart_storm + 1 for inline_state_change = 3 requires edges.
	edges, err := g.EdgesByKind(ctx, graph.EdgeRequires)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 3 {
		t.Errorf("want 3 requires edges, got %d", len(edges))
	}
}

func TestLoadPatternsMissingFileSkipped(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	err := manual.LoadPatterns(ctx, g, "/nonexistent/patterns.yaml")
	if err != nil {
		t.Errorf("expected nil error for missing file, got %v", err)
	}
}

func TestLoadPatternsCodeSmellsStoredInMetadata(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "patterns.yaml", `
patterns:
  - id: raw_digest_as_hash
    title: Raw Digest as Desired Hash
    definition: Using a raw artifact digest as the desired_hash.
    detect: "sha256 literal in desired hash field"
    code_smells:
      - "raw_artifact_digest_as_desired_hash"
      - "missing version normalization before hash"
    safe_fix_rule: "Use semantic version hash"
    related_invariants:
      - infra.desired_hash_consistency
`)

	if err := manual.LoadPatterns(ctx, g, dir+"/patterns.yaml"); err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}

	// CodeSmellsForInvariants should return the two smells.
	smells, err := g.CodeSmellsForInvariants(ctx, []string{"invariant:infra.desired_hash_consistency"})
	if err != nil {
		t.Fatal(err)
	}
	if len(smells) != 2 {
		t.Errorf("want 2 code smells, got %d: %v", len(smells), smells)
	}
}

func TestLoadAllIncludesPatterns(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "invariants.yaml", `
invariants:
  - id: inv.test
    title: Test Invariant
    severity: high
    status: active
    summary: Test.
`)
	writeYAML(t, dir, "failure_modes.yaml", `
failure_modes:
  - id: fm.test
    title: Test FM
    root_cause: Something.
    architecture_fix: Fix it.
`)
	writeYAML(t, dir, "services.yaml", `
services:
  - id: test-service
    name: test-service
    summary: Test service.
    systemd_unit: test-service.service
`)
	writeYAML(t, dir, "patterns.yaml", `
patterns:
  - id: test_pattern
    title: Test Pattern
    definition: A test anti-pattern.
    code_smells:
      - "bad_thing_in_code"
    related_invariants:
      - inv.test
`)

	if err := manual.LoadAll(ctx, g, dir); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	patterns, err := g.FindNodesByType(ctx, graph.NodeTypePattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 {
		t.Errorf("want 1 pattern node from LoadAll, got %d", len(patterns))
	}
}

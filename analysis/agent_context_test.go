package analysis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/graph"
)

// seedGraph populates a graph with the invariants and failure modes relevant
// to "fix install retry loop" so the agent context tests can assert on them.
func seedGraph(t *testing.T, g *graph.Graph) {
	t.Helper()
	ctx := context.Background()

	// install.result.atomic_commit invariant.
	_ = g.AddNode(ctx, graph.Node{
		ID:      "invariant:install.result.atomic_commit",
		Type:    graph.NodeTypeInvariant,
		Name:    "install.result.atomic_commit",
		Summary: "Installed-state, result promotion, and action cleanup must commit atomically.",
	})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "install.result.atomic_commit",
		Title:    "Install result must commit atomically",
		Summary:  "Installed-state, result promotion, and action cleanup must commit atomically.",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:install.result.atomic_commit",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:retry_install_blindly",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "forbidden_fix:retry_install_blindly",
		Type: graph.NodeTypeForbiddenFix,
		Name: "retry_install_blindly",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:install.result.atomic_commit",
		Kind: graph.EdgeTestedBy,
		Dst:  "test:TestLeaderFailoverDuringResultCommit",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "test:TestLeaderFailoverDuringResultCommit",
		Type: graph.NodeTypeTest,
		Name: "TestLeaderFailoverDuringResultCommit",
	})

	// convergence.no_infinite_retry invariant.
	_ = g.AddNode(ctx, graph.Node{
		ID:      "invariant:convergence.no_infinite_retry",
		Type:    graph.NodeTypeInvariant,
		Name:    "convergence.no_infinite_retry",
		Summary: "Deterministic failures must not retry forever.",
	})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "convergence.no_infinite_retry",
		Title:    "No infinite deterministic retry",
		Summary:  "Deterministic failures must not retry forever.",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:convergence.no_infinite_retry",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:blind_reconcile_retry",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "forbidden_fix:blind_reconcile_retry",
		Type: graph.NodeTypeForbiddenFix,
		Name: "blind_reconcile_retry",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:convergence.no_infinite_retry",
		Kind: graph.EdgeTestedBy,
		Dst:  "test:TestDeterministicFailureDoesNotRetryForever",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "test:TestDeterministicFailureDoesNotRetryForever",
		Type: graph.NodeTypeTest,
		Name: "TestDeterministicFailureDoesNotRetryForever",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:convergence.no_infinite_retry",
		Kind: graph.EdgeTestedBy,
		Dst:  "test:TestPendingSyncRecovery",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "test:TestPendingSyncRecovery",
		Type: graph.NodeTypeTest,
		Name: "TestPendingSyncRecovery",
	})

	// Failure mode: install.result.partial_commit.
	_ = g.AddNode(ctx, graph.Node{
		ID:      "failure_mode:install.result.partial_commit",
		Type:    graph.NodeTypeFailureMode,
		Name:    "install.result.partial_commit",
		Summary: "Leader dies during install result promotion.",
	})
	_ = g.UpsertFailureMode(ctx, graph.FailureMode{
		ID:              "install.result.partial_commit",
		Title:           "Leader dies during install result promotion",
		Summary:         "Partial commit leaves ambiguous state.",
		Symptoms:        []string{"reconciler dispatches same install repeatedly"},
		RootCause:       "Separate etcd writes for install-state and result.",
		ArchitectureFix: "Atomic etcd transaction.",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "failure_mode:install.result.partial_commit",
		Kind: graph.EdgeViolates,
		Dst:  "invariant:install.result.atomic_commit",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "failure_mode:install.result.partial_commit",
		Kind: graph.EdgeViolates,
		Dst:  "invariant:convergence.no_infinite_retry",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "failure_mode:install.result.partial_commit",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:clear_action_before_installed_state",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "forbidden_fix:clear_action_before_installed_state",
		Type: graph.NodeTypeForbiddenFix,
		Name: "clear_action_before_installed_state",
	})
}

// Test 6: agent context includes relevant invariant and forbidden fixes
// for task "fix install retry loop".
func TestAgentContextForInstallRetryLoop(t *testing.T) {
	g := openGraph(t)
	seedGraph(t, g)

	ctx := context.Background()
	md, result, err := analysis.GenerateAgentContext(ctx, g, "fix install retry loop", analysis.AgentContextHints{})
	if err != nil {
		t.Fatalf("GenerateAgentContext: %v", err)
	}

	// Must mention install.result.atomic_commit.
	if !strings.Contains(md, "install.result.atomic_commit") {
		t.Error("Markdown missing install.result.atomic_commit")
	}
	// Must mention convergence.no_infinite_retry.
	if !strings.Contains(md, "convergence.no_infinite_retry") {
		t.Error("Markdown missing convergence.no_infinite_retry")
	}
	// Must mention at least one forbidden fix.
	if !strings.Contains(md, "Forbidden fixes") {
		t.Error("Markdown missing Forbidden fixes section")
	}
	if len(result.ForbiddenFixes) == 0 {
		t.Error("result.ForbiddenFixes is empty")
	}
	// Must mention required tests.
	if !strings.Contains(md, "Required tests") {
		t.Error("Markdown missing Required tests section")
	}
	if len(result.RequiredTests) == 0 {
		t.Error("result.RequiredTests is empty")
	}
	// Architecture rule must be present.
	if !strings.Contains(md, "Architecture rule") {
		t.Error("Markdown missing Architecture rule section")
	}
	// State model must be present.
	if !strings.Contains(md, "state model") {
		t.Error("Markdown missing state model section")
	}

	t.Logf("InvariantIDs: %v", result.InvariantIDs)
	t.Logf("ForbiddenFixes: %v", result.ForbiddenFixes)
	t.Logf("RequiredTests: %v", result.RequiredTests)
}

// Test: empty task still returns architecture rules.
func TestAgentContextEmptyTask(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	md, _, err := analysis.GenerateAgentContext(ctx, g, "", analysis.AgentContextHints{})
	if err != nil {
		t.Fatalf("GenerateAgentContext: %v", err)
	}
	if !strings.Contains(md, "Globular Agent Context") {
		t.Error("header missing")
	}
	if !strings.Contains(md, "Architecture rule") {
		t.Error("Architecture rule missing")
	}
}

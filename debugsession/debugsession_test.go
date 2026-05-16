package debugsession_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/debugsession"
	"github.com/globulario/awareness/graph"
)

// ---- helpers ----------------------------------------------------------------

func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// addInvariant upserts an invariant and its corresponding graph node.
func addInvariant(t *testing.T, ctx context.Context, g *graph.Graph, id, title, summary, severity string) {
	t.Helper()
	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID: id, Title: title, Summary: summary, Severity: severity, Status: "active",
	}); err != nil {
		t.Fatalf("upsert invariant %s: %v", id, err)
	}
	if err := g.AddNode(ctx, graph.Node{
		ID:      "invariant:" + id,
		Type:    graph.NodeTypeInvariant,
		Name:    id,
		Summary: summary,
	}); err != nil {
		t.Fatalf("add invariant node %s: %v", id, err)
	}
}

// addFailureMode upserts a failure mode and its graph node.
func addFailureMode(t *testing.T, ctx context.Context, g *graph.Graph, id, title, summary, rootCause string) {
	t.Helper()
	if err := g.UpsertFailureMode(ctx, graph.FailureMode{
		ID: id, Title: title, Summary: summary, RootCause: rootCause,
	}); err != nil {
		t.Fatalf("upsert failure mode %s: %v", id, err)
	}
	if err := g.AddNode(ctx, graph.Node{
		ID:      "failure_mode:" + id,
		Type:    graph.NodeTypeFailureMode,
		Name:    id,
		Summary: summary,
	}); err != nil {
		t.Fatalf("add failure mode node %s: %v", id, err)
	}
}

// addService adds a globular_service node.
func addService(t *testing.T, ctx context.Context, g *graph.Graph, name string) string {
	t.Helper()
	id := "service:" + name
	if err := g.AddNode(ctx, graph.Node{
		ID: id, Type: graph.NodeTypeGlobularService, Name: name,
		Summary: name + " service",
	}); err != nil {
		t.Fatalf("add service node %s: %v", name, err)
	}
	return id
}

// linkEnforces adds a service --enforces--> invariant edge.
func linkEnforces(t *testing.T, ctx context.Context, g *graph.Graph, srcID, dstID string) {
	t.Helper()
	if err := g.AddEdge(ctx, graph.Edge{
		Src: srcID, Kind: graph.EdgeEnforces, Dst: dstID, Confidence: 1.0,
	}); err != nil {
		t.Fatalf("add enforces edge %s→%s: %v", srcID, dstID, err)
	}
}

// ---- tests ------------------------------------------------------------------

// TestDebugSession_DesiredHashMismatch_FindsInvariant verifies that a task
// about desired_hash mismatch surfaces the infra.desired_hash_consistency invariant.
func TestDebugSession_DesiredHashMismatch_FindsInvariant(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	addInvariant(t, ctx, g,
		"infra.desired_hash_consistency",
		"Desired Hash Consistency",
		"DesiredHash must be computed consistently across all nodes — mismatches cause convergence loops.",
		"critical",
	)
	svcID := addService(t, ctx, g, "cluster_controller")
	linkEnforces(t, ctx, g, svcID, "invariant:infra.desired_hash_consistency")

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "desired_hash mismatch causing convergence loop",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, inv := range report.RelevantInvariants {
		if strings.Contains(inv, "desired_hash_consistency") {
			return
		}
	}
	// Also accept it via root-cause paths.
	for _, p := range report.LikelyRootCausePaths {
		if strings.Contains(p.TargetNodeName, "desired_hash_consistency") {
			return
		}
	}
	t.Errorf("expected infra.desired_hash_consistency in invariants or root-cause paths, got: invariants=%v, paths=%v",
		report.RelevantInvariants, pathTargets(report.LikelyRootCausePaths))
}

// TestDebugSession_RestartStorm_FindsInvariant verifies that a restart-storm task
// surfaces the service.restart_singleflight invariant.
func TestDebugSession_RestartStorm_FindsInvariant(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	addInvariant(t, ctx, g,
		"service.restart_singleflight",
		"Restart Singleflight Gate",
		"Only one restart may be issued per service per convergence tick — concurrent restarts cause a restart storm.",
		"critical",
	)
	svcID := addService(t, ctx, g, "node_agent")
	linkEnforces(t, ctx, g, svcID, "invariant:service.restart_singleflight")

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "restart storm — services restarting on every convergence tick",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, inv := range report.RelevantInvariants {
		if strings.Contains(inv, "restart_singleflight") {
			return
		}
	}
	for _, p := range report.LikelyRootCausePaths {
		if strings.Contains(p.TargetNodeName, "restart_singleflight") {
			return
		}
	}
	t.Errorf("expected service.restart_singleflight in invariants or root-cause paths, got: invariants=%v, paths=%v",
		report.RelevantInvariants, pathTargets(report.LikelyRootCausePaths))
}

// TestDebugSession_MissingKey_FindsInvariant verifies that a task about a missing
// etcd key surfaces the critical_state.absence_is_not_destructive_intent invariant.
func TestDebugSession_MissingKey_FindsInvariant(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	addInvariant(t, ctx, g,
		"critical_state.absence_is_not_destructive_intent",
		"Absence Is Not Destructive Intent",
		"A missing key in etcd must not be treated as a delete command — absence means unknown, not delete.",
		"critical",
	)
	etcdID := "etcd_key:desired_state"
	if err := g.AddNode(ctx, graph.Node{
		ID: etcdID, Type: graph.NodeTypeEtcdKey, Name: "desired_state",
		Summary: "desired state key in etcd",
	}); err != nil {
		t.Fatalf("add etcd node: %v", err)
	}
	linkEnforces(t, ctx, g, etcdID, "invariant:critical_state.absence_is_not_destructive_intent")

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "missing key in etcd stopped runtime convergence",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, inv := range report.RelevantInvariants {
		if strings.Contains(inv, "absence_is_not_destructive_intent") {
			return
		}
	}
	for _, p := range report.LikelyRootCausePaths {
		if strings.Contains(p.TargetNodeName, "absence_is_not_destructive_intent") {
			return
		}
	}
	t.Errorf("expected absence_is_not_destructive_intent in invariants or paths, got: invariants=%v, paths=%v",
		report.RelevantInvariants, pathTargets(report.LikelyRootCausePaths))
}

// TestDebugSession_FileFlag_UsesNodeContext verifies that providing a --file flag
// makes the corresponding graph node a starting node.
func TestDebugSession_FileFlag_UsesNodeContext(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	const testPath = "golang/cluster_controller/server.go"
	if err := g.AddNode(ctx, graph.Node{
		ID:   "source_file:" + testPath,
		Type: graph.NodeTypeSourceFile,
		Name: "server.go",
		Path: testPath,
	}); err != nil {
		t.Fatalf("add file node: %v", err)
	}

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task:  "investigate server.go",
		Files: []string{testPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, n := range report.StartingNodes {
		if n.Source == "file" && strings.Contains(n.Path, "server.go") {
			return
		}
	}
	t.Errorf("expected file-sourced starting node for server.go, got: %+v", report.StartingNodes)
}

// TestDebugSession_WithRuntime_IncludesRuntimeSection verifies that IncludeRuntime=true
// populates the RuntimeEvidence field (even when noop bridge returns an empty snapshot).
func TestDebugSession_WithRuntime_IncludesRuntimeSection(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task:           "check runtime state",
		IncludeRuntime: true,
		// Bridge is nil → preflight uses noop bridge.
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.RuntimeEvidence == nil {
		t.Fatal("expected non-nil RuntimeEvidence when IncludeRuntime=true")
	}
}

// TestDebugSession_ProducesRootCausePath_WhenGraphHasOne verifies that when a
// starting node is connected to an invariant/failure mode, at least one root-cause
// path is produced.
func TestDebugSession_ProducesRootCausePath_WhenGraphHasOne(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	// Service node connected to an invariant.
	svcID := addService(t, ctx, g, "workflow_service")
	addInvariant(t, ctx, g,
		"workflow.must_terminate",
		"Workflow Must Terminate",
		"Every workflow must reach SUCCEEDED or FAILED — silent exits are forbidden.",
		"critical",
	)
	linkEnforces(t, ctx, g, svcID, "invariant:workflow.must_terminate")

	// File node that maps to the service via semantic proximity.
	const testPath = "golang/workflow/server.go"
	fileID := "source_file:" + testPath
	if err := g.AddNode(ctx, graph.Node{
		ID: fileID, Type: graph.NodeTypeSourceFile,
		Name: "server.go", Path: testPath,
	}); err != nil {
		t.Fatalf("add file: %v", err)
	}
	// Define: file→service.
	if err := g.AddEdge(ctx, graph.Edge{
		Src: fileID, Kind: graph.EdgeDefines, Dst: svcID, Confidence: 1.0,
	}); err != nil {
		t.Fatalf("add defines edge: %v", err)
	}

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task:  "workflow service not terminating",
		Files: []string{testPath},
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.LikelyRootCausePaths) == 0 {
		t.Error("expected at least one root-cause path, got none")
	}
}

// TestDebugSession_UnknownImpact_ArchitectureSensitive_NoFacts verifies that an
// architecture-sensitive task with no matching facts receives UNKNOWN_IMPACT + UNKNOWN confidence.
func TestDebugSession_UnknownImpact_ArchitectureSensitive_NoFacts(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t) // empty graph — no matching facts

	report, err := debugsession.Run(ctx, debugsession.Options{
		// Contains "retry" and "convergence" → ARCHITECTURE_SENSITIVE, no facts in graph
		Task: "weird retry loop in convergence path",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	hasUnknownImpact := false
	for _, c := range report.Classification {
		if string(c) == "UNKNOWN_IMPACT" {
			hasUnknownImpact = true
			break
		}
	}
	if !hasUnknownImpact {
		t.Errorf("expected UNKNOWN_IMPACT classification for arch-sensitive task with no facts, got: %v", report.Classification)
	}
	if report.Confidence != "UNKNOWN" {
		t.Errorf("expected Confidence=UNKNOWN, got %q", report.Confidence)
	}
}

// TestDebugSession_FormatAgent_ContainsDirectives verifies that the agent format
// includes the required directive sections.
func TestDebugSession_FormatAgent_ContainsDirectives(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	addInvariant(t, ctx, g,
		"test.invariant",
		"Test Invariant",
		"An invariant for format testing.",
		"warning",
	)
	svcID := addService(t, ctx, g, "test_service")
	linkEnforces(t, ctx, g, svcID, "invariant:test.invariant")

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "test invariant enforcement issue",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := debugsession.FormatReport(report, "agent")

	required := []string{
		"AGENT DEBUG SESSION",
		"Task:",
		"Classification:",
		"Investigation plan:",
	}
	for _, want := range required {
		if !strings.Contains(out, want) {
			t.Errorf("agent output missing %q\nfull:\n%s", want, out)
		}
	}
}

// TestDebugSession_FormatJSON_IsValidJSON verifies that json format produces
// parseable output.
func TestDebugSession_FormatJSON_IsValidJSON(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "json format test",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := debugsession.FormatReport(report, "json")
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("FormatReport(json) produced invalid JSON: %v\noutput: %s", err, out)
	}
	if _, ok := v["task"]; !ok {
		t.Errorf("JSON output missing 'task' field: %v", v)
	}
}

// TestDebugSession_DesiredHashMismatch_AgentOutputHasRootCausePath verifies
// that the agent-format output for a desired_hash mismatch task includes at
// least one "Likely root-cause" section.
func TestDebugSession_DesiredHashMismatch_AgentOutputHasRootCausePath(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	// Set up the invariant and a service that enforces it.
	addInvariant(t, ctx, g,
		"infra.desired_hash_consistency",
		"Desired Hash Consistency",
		"DesiredHash must be computed consistently — mismatches cause restart storms.",
		"critical",
	)
	addFailureMode(t, ctx, g,
		"infra.desired_hash_mismatch_restart_storm",
		"Desired Hash Mismatch Restart Storm",
		"Hash mismatch causes the reconciler to keep reinstalling on every tick.",
		"lookupServiceReleaseBuildID returns inconsistent hash",
	)
	svcID := addService(t, ctx, g, "cluster_controller")
	linkEnforces(t, ctx, g, svcID, "invariant:infra.desired_hash_consistency")

	// Connect the service to the failure mode via an affects edge.
	if err := g.AddEdge(ctx, graph.Edge{
		Src:        svcID,
		Dst:        "failure_mode:infra.desired_hash_mismatch_restart_storm",
		Kind:       graph.EdgeAffects,
		Confidence: 0.9,
	}); err != nil {
		t.Fatalf("add affects edge: %v", err)
	}

	report, err := debugsession.Run(ctx, debugsession.Options{
		Task: "desired_hash mismatch caused install loop and envoy restart storm",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := debugsession.FormatReport(report, "agent")

	if !strings.Contains(out, "Likely root-cause") && !strings.Contains(out, "root-cause") {
		t.Errorf("agent output missing 'root-cause' section for desired_hash mismatch\noutput:\n%s", out)
	}
}

// ---- helpers ----------------------------------------------------------------

func pathTargets(paths []debugsession.RootCausePath) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p.TargetNodeName
	}
	return out
}

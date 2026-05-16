package workflowstate

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
	"google.golang.org/grpc"
)

func newTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(":memory:")
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// TestWorkflowExecutionExtractor_Disabled verifies that nil factory → "disabled".
func TestWorkflowExecutionExtractor_Disabled(t *testing.T) {
	g := newTestGraph(t)
	health, err := Collect(context.Background(), g, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "disabled" {
		t.Errorf("expected status=disabled, got %s", health.Status)
	}
	if health.Coverage != "disabled" {
		t.Errorf("expected coverage=disabled, got %s", health.Coverage)
	}
}

// TestWorkflowExecutionExtractor_Unreachable verifies that connect failure → "failed".
func TestWorkflowExecutionExtractor_Unreachable(t *testing.T) {
	g := newTestGraph(t)
	factory := func() (*grpc.ClientConn, error) {
		return grpc.Dial("127.0.0.1:19999",
			grpc.WithInsecure(), //nolint:staticcheck
			grpc.WithBlock(),
			grpc.WithTimeout(50*time.Millisecond),
		)
	}
	health, err := Collect(context.Background(), g, factory, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "failed" {
		t.Errorf("expected status=failed for unreachable address, got %s", health.Status)
	}
	if health.Coverage != "failed" {
		t.Errorf("expected coverage=failed, got %s", health.Coverage)
	}
}

// TestWorkflowLiveOverlay_RunHasTTL verifies run nodes carry ttl_seconds.
func TestWorkflowLiveOverlay_RunHasTTL(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	run := &workflowpb.WorkflowRun{
		Id:           "aaaa-bbbb-cccc-dddd",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
	}
	if err := indexRun(ctx, g, run, "snap:1", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}

	node, err := g.FindNode(ctx, "workflow_run:aaaa-bbbb-cccc-dddd")
	if err != nil || node == nil {
		t.Fatalf("node not found: %v", err)
	}
	if node.Metadata["ttl_seconds"] == nil {
		t.Error("node missing ttl_seconds metadata")
	}
	if node.Metadata["expires_at"] == nil {
		t.Error("node missing expires_at metadata")
	}
	if node.Metadata["source_tier"] != sourceTier {
		t.Errorf("expected source_tier=%s, got %v", sourceTier, node.Metadata["source_tier"])
	}
}

// TestWorkflowLiveOverlay_RunLinksToDefinition verifies EdgeWorkflowRunInstantiates is set.
func TestWorkflowLiveOverlay_RunLinksToDefinition(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Pre-seed static workflow definition.
	if err := g.AddNode(ctx, graph.Node{
		ID:   "workflow:package.install",
		Type: graph.NodeTypeWorkflow,
		Name: "package.install",
	}); err != nil {
		t.Fatal(err)
	}

	run := &workflowpb.WorkflowRun{
		Id:           "run-1111-2222-3333",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_SUCCEEDED,
	}
	if err := indexRun(ctx, g, run, "snap:1", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}

	edges, err := g.OutgoingEdges(ctx, "workflow_run:run-1111-2222-3333")
	if err != nil {
		t.Fatalf("edges: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeWorkflowRunInstantiates && e.Dst == "workflow:package.install" {
			found = true
		}
	}
	if !found {
		t.Error("expected workflow_run_instantiates_definition edge to workflow:package.install")
	}
}

// TestWorkflowRun_MissingDefinitionReportsBlindSpot verifies blind-spot node created when no static definition.
func TestWorkflowRun_MissingDefinitionReportsBlindSpot(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	run := &workflowpb.WorkflowRun{
		Id:           "run-no-def-1234",
		WorkflowName: "unknown.workflow",
		Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
	}
	if err := indexRun(ctx, g, run, "snap:1", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}

	// Should have created a blind-spot node.
	bsNode, err := g.FindNode(ctx, "workflow_run_definition_missing:unknown.workflow")
	if err != nil || bsNode == nil {
		t.Error("expected blind-spot node for missing definition")
	}
}

// TestWorkflowRun_LinksTargets verifies target edges are emitted when context is set.
func TestWorkflowRun_LinksTargets(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	run := &workflowpb.WorkflowRun{
		Id:           "run-target-5555",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_EXECUTING,
		Context: &workflowpb.WorkflowContext{
			NodeId:        "node-abc123",
			ComponentName: "minio",
		},
	}
	if err := indexRun(ctx, g, run, "snap:1", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}

	edges, err := g.OutgoingEdges(ctx, "workflow_run:run-target-5555")
	if err != nil {
		t.Fatalf("edges: %v", err)
	}
	hasNode := false
	hasSvc := false
	for _, e := range edges {
		if e.Kind == graph.EdgeWorkflowRunTargetsNode {
			hasNode = true
		}
		if e.Kind == graph.EdgeWorkflowRunTargetsService && e.Dst == "service:minio" {
			hasSvc = true
		}
	}
	if !hasNode {
		t.Error("expected workflow_run_targets_node edge")
	}
	if !hasSvc {
		t.Error("expected workflow_run_targets_service edge to service:minio")
	}
}

// TestWorkflowDiagnosis_DetectsRetryStorm verifies retry storm detection in generateCandidates.
func TestWorkflowDiagnosis_DetectsRetryStorm(t *testing.T) {
	summaries := []*workflowpb.WorkflowRunSummary{
		{WorkflowName: "package.install", TotalRuns: 20, SuccessRuns: 5, FailureRuns: 15, LastFailureReason: "timeout"},
	}
	candidates := generateCandidates(nil, summaries, time.Now())
	if len(candidates) == 0 {
		t.Error("expected at least one candidate for retry storm")
	}
	found := false
	for _, c := range candidates {
		if c.WorkflowName == "package.install" && c.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Error("expected retry storm candidate for package.install")
	}
}

// TestWorkflowDiagnosis_DetectsVerificationSkipped verifies validation failure → candidate.
func TestWorkflowDiagnosis_DetectsVerificationSkipped(t *testing.T) {
	runs := []*workflowpb.WorkflowRun{
		{
			Id:           "run-verify-gap",
			WorkflowName: "package.install",
			Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
			FailureClass: workflowpb.FailureClass_FAILURE_CLASS_VALIDATION,
			ErrorMessage: "verify_runtime step timed out",
			RetryCount:   2,
		},
	}
	candidates := generateCandidates(runs, nil, time.Now())
	found := false
	for _, c := range candidates {
		if c.WorkflowName == "package.install" {
			found = true
		}
	}
	if !found {
		t.Error("expected incident candidate for verification failure")
	}
}

// TestWorkflowIncidentCandidate_DoesNotAutoOpen verifies auto_opened = false.
func TestWorkflowIncidentCandidate_DoesNotAutoOpen(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	c := incidentCandidate{
		Title:                 "Test candidate",
		Severity:              "warning",
		Confidence:            "medium",
		WorkflowName:          "test.workflow",
		RecommendedNextAction: "review_and_approve",
	}
	if err := emitIncidentCandidate(ctx, g, c, now); err != nil {
		t.Fatalf("emitIncidentCandidate: %v", err)
	}

	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeIncident)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("no incident nodes found: %v", err)
	}
	for _, n := range nodes {
		if v, ok := n.Metadata["auto_opened"]; ok {
			if v == true {
				t.Error("auto_opened must not be true for incident candidates")
			}
		}
	}
}

// TestCollectorHealth_Included verifies collector health fields are populated.
func TestCollectorHealth_Included(t *testing.T) {
	health := CollectorHealth{
		CollectorID: collectorID,
		SourceTier:  sourceTier,
		Status:      "disabled",
		Coverage:    "disabled",
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
		TTLSeconds:  ttlActiveRun,
	}
	if health.CollectorID == "" {
		t.Error("CollectorID must be set")
	}
	if health.CollectedAt == "" {
		t.Error("CollectedAt must be set")
	}
	if health.TTLSeconds <= 0 {
		t.Error("TTLSeconds must be positive")
	}
}

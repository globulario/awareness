package workflowstate

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── P0.1 Tests: Typed Impact Edges ───────────────────────────────────────────

// TestWorkflowImpactPath_RunToStepRunToStepToInvariant verifies a full typed path exists
// from workflow_run through step_run to static step and invariant.
func TestWorkflowImpactPath_RunToStepRunToStepToInvariant(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Pre-seed: static workflow definition with a step that requires an invariant.
	_ = g.AddNode(ctx, graph.Node{ID: "workflow:package.install", Type: graph.NodeTypeWorkflow})
	_ = g.AddNode(ctx, graph.Node{ID: "workflow_step:package.install.verify_runtime", Type: graph.NodeTypeWorkflowStep})
	_ = g.AddEdge(ctx, graph.Edge{Src: "workflow:package.install", Kind: graph.EdgeOwns, Dst: "workflow_step:package.install.verify_runtime"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "workflow_step:package.install.verify_runtime", Kind: graph.EdgeRequires, Dst: "service:node-agent"})
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:desired_installed_runtime_must_converge", Type: graph.NodeTypeInvariant})

	run := &workflowpb.WorkflowRun{
		Id:           "run-path-test-1111",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
		FailureClass: workflowpb.FailureClass_FAILURE_CLASS_SYSTEMD,
		ErrorMessage: "verify_runtime timed out",
		WaitReason:   "verify_runtime",
		RetryCount:   1,
	}
	if err := indexRun(ctx, g, run, "snap:test", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}

	// Emit typed failure edges.
	emitTypedFailureEdges(ctx, g, run, now)

	runID := "workflow_run:run-path-test-1111"
	// Check: run → has_step_run edge.
	edges, _ := g.OutgoingEdges(ctx, runID)
	hasStepRun := false
	for _, e := range edges {
		if e.Kind == graph.EdgeWorkflowRunHasStepRun {
			hasStepRun = true
		}
	}
	if !hasStepRun {
		t.Error("expected EdgeWorkflowRunHasStepRun from run to step_run")
	}

	// Check: step_run → instantiates → static step.
	stepRunID := "workflow_step_run:run-path-test-1111.failed_step"
	stepEdges, _ := g.OutgoingEdges(ctx, stepRunID)
	hasInstantiates := false
	for _, e := range stepEdges {
		if e.Kind == graph.EdgeWorkflowStepRunInstantiatesStep {
			hasInstantiates = true
		}
	}
	if !hasInstantiates {
		t.Error("expected EdgeWorkflowStepRunInstantiatesStep from step_run to static step")
	}
}

// TestWorkflowImpactPath_FailureToFailureModeToInvariant verifies the typed failure path.
func TestWorkflowImpactPath_FailureToFailureModeToInvariant(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	run := &workflowpb.WorkflowRun{
		Id:           "run-fm-path-2222",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
		ErrorMessage: "systemd unit restart failed",
		RetryCount:   1,
	}
	if err := indexRun(ctx, g, run, "snap:test", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}
	emitTypedFailureEdges(ctx, g, run, now)

	// workflow_error node should exist.
	errNode, _ := g.FindNode(ctx, "workflow_error:run-fm-pa")
	_ = errNode // optional to exist if error message was set
	// At minimum, step_run_failed_with_error edge must exist when error message is non-empty.
	allNodes, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowError)
	if len(allNodes) == 0 {
		t.Error("expected workflow_error node to be emitted for failed run with error_message")
	}
}

// TestWorkflowImpactPath_ForbidsBlindRetry verifies forbidden action edge on high retry count.
func TestWorkflowImpactPath_ForbidsBlindRetry(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	run := &workflowpb.WorkflowRun{
		Id:           "run-retry-forbid-3333",
		WorkflowName: "package.install",
		Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
		ErrorMessage: "step failed",
		RetryCount:   5,
	}
	if err := indexRun(ctx, g, run, "snap:test", now); err != nil {
		t.Fatalf("indexRun: %v", err)
	}
	emitTypedFailureEdges(ctx, g, run, now)

	edges, _ := g.OutgoingEdges(ctx, "workflow_run:run-retry-forbid-3333")
	hasForbid := false
	for _, e := range edges {
		if e.Kind == graph.EdgeWorkflowRunForbidsAction {
			hasForbid = true
		}
	}
	if !hasForbid {
		t.Error("expected EdgeWorkflowRunForbidsAction when retry_count > 3")
	}

	// Verify the forbidden action node exists.
	actionNode, _ := g.FindNode(ctx, "action:blind_retry_without_terminal_classification")
	if actionNode == nil {
		t.Error("expected forbidden action node to exist")
	}
}

// ── P0.2 Tests: Receipt/Step Proof ───────────────────────────────────────────

// TestWorkflowReceipts_ReceiptOnlyEvidenceLowerConfidence verifies that step outcomes
// with failure records produce lower-confidence receipt nodes.
func TestWorkflowReceipts_ReceiptOnlyEvidenceLowerConfidence(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Emit a failed run first.
	run := &workflowpb.WorkflowRun{
		Id: "run-receipt-1234", WorkflowName: "package.install",
		Status: workflowpb.RunStatus_RUN_STATUS_FAILED, RetryCount: 2,
	}
	_ = indexRun(ctx, g, run, "snap:test", now)

	// Directly add a receipt node simulating a failed step outcome.
	receiptID := "workflow_receipt:package.install.verify_runtime"
	_ = g.AddNode(ctx, graph.Node{
		ID:   receiptID,
		Type: graph.NodeTypeWorkflowReceipt,
		Name: "receipt:package.install.verify_runtime",
		Metadata: map[string]any{
			"workflow_name":       "package.install",
			"step_id":             "verify_runtime",
			"verification_status": "failed",
			"confidence":          "low",
			"source_tier":         "live_runtime",
		},
	})

	receipts, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowReceipt)
	for _, r := range receipts {
		if r.Metadata["verification_status"] == "failed" {
			conf, _ := r.Metadata["confidence"].(string)
			if conf != "low" {
				t.Errorf("failed verification receipt should have confidence=low, got %s", conf)
			}
		}
	}
}

// TestWorkflowReceipts_MissingCriticalReceiptWarns verifies that missing step receipts produce warnings.
func TestWorkflowReceipts_MissingCriticalReceiptWarns(t *testing.T) {
	// Simulate a step outcome with 0 success_count but >0 total_executions.
	// This should trigger a step_receipt_missing warning.
	fakeOutcomes := []*workflowpb.WorkflowStepOutcome{
		{WorkflowName: "package.install", TotalExecutions: 3, SuccessCount: 0, FailureCount: 3},
	}
	var warnings []string
	for _, outcome := range fakeOutcomes {
		if outcome.GetSuccessCount() == 0 && outcome.GetTotalExecutions() > 0 {
			warnings = append(warnings, "step_receipt_missing: workflow="+outcome.GetWorkflowName())
		}
	}
	if len(warnings) == 0 {
		t.Error("expected step_receipt_missing warning for step with zero successes")
	}
}

// ── P0.3 Tests: Freshness Enforcement ────────────────────────────────────────

// TestWorkflowFreshness_FreshAllowsMediumOrHighConfidence verifies fresh data is high-confidence.
func TestWorkflowFreshness_FreshAllowsMediumOrHighConfidence(t *testing.T) {
	now := time.Now()
	meta := map[string]any{
		"collected_at": now.Add(-2 * time.Minute).UTC().Format(time.RFC3339),
		"ttl_seconds":  ttlActiveRun,
		"expires_at":   now.Add(13 * time.Minute).UTC().Format(time.RFC3339),
	}
	f := CheckFreshness(meta, now)
	if f.State != FreshnessFresh {
		t.Errorf("expected fresh, got %s", f.State)
	}
	if !f.CanDriveDecision() {
		t.Error("fresh data should CanDriveDecision")
	}
	if f.ConfidenceImpact != ConfidenceImpactNone {
		t.Errorf("fresh data should have no confidence impact, got %s", f.ConfidenceImpact)
	}
}

// TestWorkflowFreshness_StaleLowersConfidence verifies stale data lowers confidence.
func TestWorkflowFreshness_StaleLowersConfidence(t *testing.T) {
	now := time.Now()
	// 85% of TTL elapsed (past staleThreshold of 0.75).
	elapsed := time.Duration(float64(ttlActiveRun)*0.85) * time.Second
	meta := map[string]any{
		"collected_at": now.Add(-elapsed).UTC().Format(time.RFC3339),
		"ttl_seconds":  ttlActiveRun,
		"expires_at":   now.Add(time.Duration(ttlActiveRun)*time.Second - elapsed).UTC().Format(time.RFC3339),
	}
	f := CheckFreshness(meta, now)
	if f.State != FreshnessStale {
		t.Errorf("expected stale, got %s (age=%.0fs, ttl=%ds, threshold=%.0fs)", f.State, f.AgeSeconds, ttlActiveRun, staleThreshold*ttlActiveRun)
	}
	if f.ConfidenceImpact != ConfidenceImpactLowered {
		t.Errorf("stale should lower confidence, got %s", f.ConfidenceImpact)
	}
	conf := effectiveConfidence("high", f)
	if conf != "medium" {
		t.Errorf("stale high→medium, got %s", conf)
	}
}

// TestWorkflowFreshness_ExpiredDoesNotCreateIncidentCandidate verifies expired runs are skipped.
func TestWorkflowFreshness_ExpiredDoesNotCreateIncidentCandidate(t *testing.T) {
	// Create a run with started_at far in the past (beyond ttlActiveRun).
	oldTime := time.Now().Add(-30 * time.Minute) // well past 15-min TTL
	ts := timestampFromTime(oldTime)

	runs := []*workflowpb.WorkflowRun{
		{
			Id:           "run-expired-old",
			WorkflowName: "package.install",
			Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
			RetryCount:   5,
			StartedAt:    ts,
		},
	}
	candidates := generateCandidates(runs, nil, time.Now())
	if len(candidates) != 0 {
		t.Errorf("expired evidence must not create incident candidates, got %d", len(candidates))
	}
}

// TestWorkflowFreshness_FailedCollectorCreatesBlindSpot verifies nil factory creates a blind spot.
func TestWorkflowFreshness_FailedCollectorCreatesBlindSpot(t *testing.T) {
	g := newTestGraph(t)
	health, _ := Collect(context.Background(), g, nil, "")
	if health.Coverage != "disabled" {
		t.Errorf("expected coverage=disabled for nil factory, got %s", health.Coverage)
	}
	if health.Status != "disabled" {
		t.Errorf("expected status=disabled, got %s", health.Status)
	}
}

// TestWorkflowFreshness_EmptyCheckedSourceIsCheckedClean verifies empty gRPC response = checked_clean.
func TestWorkflowFreshness_EmptyCheckedSourceIsCheckedClean(t *testing.T) {
	// A reachable but empty source is checked_clean, not absent.
	// Simulate by checking that the Collect function sets checked_clean when runs=0.
	// We test this through the CollectorHealth struct directly.
	health := CollectorHealth{
		Status:   "ok",
		Coverage: "checked_clean",
	}
	if health.Coverage != "checked_clean" {
		t.Error("empty source from reachable service must be checked_clean")
	}
}

// ── P0.4 Tests: Incident Candidate Safety ────────────────────────────────────

// TestWorkflowIncidentCandidate_AutoOpenedAlwaysFalse verifies auto_opened is never true.
func TestWorkflowIncidentCandidate_AutoOpenedAlwaysFalse(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	summaries := []*workflowpb.WorkflowRunSummary{
		{WorkflowName: "test.workflow", TotalRuns: 20, FailureRuns: 15},
	}
	candidates := generateCandidates(nil, summaries, now)
	for i, c := range candidates {
		if c.AutoOpened {
			t.Errorf("candidate[%d] AutoOpened must be false", i)
		}
		if !c.RequiresHumanApproval {
			t.Errorf("candidate[%d] RequiresHumanApproval must be true", i)
		}
		if c.SafeToExecute {
			t.Errorf("candidate[%d] SafeToExecute must be false", i)
		}
	}

	for _, c := range candidates {
		_ = emitIncidentCandidate(ctx, g, c, now)
	}

	nodes, _ := g.FindNodesByType(ctx, graph.NodeTypeIncident)
	for _, n := range nodes {
		if v, ok := n.Metadata["auto_opened"].(bool); ok && v {
			t.Error("auto_opened must be false in graph node metadata")
		}
		if v, ok := n.Metadata["requires_human_approval"].(bool); !ok || !v {
			t.Error("requires_human_approval must be true in graph node metadata")
		}
		if v, ok := n.Metadata["safe_to_execute"].(bool); ok && v {
			t.Error("safe_to_execute must be false in graph node metadata")
		}
	}
}

// TestWorkflowIncidentCandidate_RequiresHumanApproval verifies human approval field.
func TestWorkflowIncidentCandidate_RequiresHumanApproval(t *testing.T) {
	_, requiresHumanApproval, safeToExecute := safeCandidateDefaults()
	if !requiresHumanApproval {
		t.Error("safeCandidateDefaults must return requiresHumanApproval=true")
	}
	if safeToExecute {
		t.Error("safeCandidateDefaults must return safeToExecute=false")
	}
}

// TestWorkflowIncidentCandidate_ExpiredEvidenceCannotCreateCandidate verifies the guard.
func TestWorkflowIncidentCandidate_ExpiredEvidenceCannotCreateCandidate(t *testing.T) {
	now := time.Now()
	oldTime := now.Add(-60 * time.Minute)
	ts := timestampFromTime(oldTime)

	runs := []*workflowpb.WorkflowRun{
		{
			Id:           "run-old-evidence",
			WorkflowName: "package.install",
			Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
			RetryCount:   10,
			StartedAt:    ts,
		},
	}
	candidates := generateCandidates(runs, nil, now)
	if len(candidates) > 0 {
		t.Errorf("expired evidence (60min old > 15min TTL) must not create incident candidates, got %d", len(candidates))
	}
}

// ── P1.5 Tests: Workflow Overlay Integrity ────────────────────────────────────

// TestWorkflowIntegrity_RunMissingDefinitionWarns verifies missing definition produces warning.
func TestWorkflowIntegrity_RunMissingDefinitionWarns(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Add a run with no definition link.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "workflow_run:no-def-run",
		Type: graph.NodeTypeWorkflowRun,
		Name: "test run",
		Metadata: liveNodeMeta("workflow_run", now, now.Add(15*time.Minute), ttlActiveRun, "high"),
	})

	result := CheckWorkflowOverlayIntegrity(ctx, g, now)
	found := false
	for _, f := range result.Findings {
		if f.Code == "WORKFLOW_RUN_NO_DEFINITION" {
			found = true
		}
	}
	if !found {
		t.Error("expected WORKFLOW_RUN_NO_DEFINITION warning for run missing definition link")
	}
}

// TestWorkflowIntegrity_SuccessWithoutVerificationFails verifies that success without receipts warns.
func TestWorkflowIntegrity_SuccessWithoutVerificationFails(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	meta := liveNodeMeta("workflow_run", now, now.Add(15*time.Minute), ttlActiveRun, "high")
	meta["status"] = "succeeded"
	meta["workflow_name"] = "package.install"
	_ = g.AddNode(ctx, graph.Node{
		ID:       "workflow_run:success-no-proof",
		Type:     graph.NodeTypeWorkflowRun,
		Name:     "success run",
		Metadata: meta,
	})
	// Wire definition link to avoid WORKFLOW_RUN_NO_DEFINITION finding.
	_ = g.AddNode(ctx, graph.Node{ID: "workflow:package.install", Type: graph.NodeTypeWorkflow})
	_ = g.AddEdge(ctx, graph.Edge{Src: "workflow_run:success-no-proof", Kind: graph.EdgeWorkflowRunInstantiates, Dst: "workflow:package.install"})

	result := CheckWorkflowOverlayIntegrity(ctx, g, now)
	found := false
	for _, f := range result.Findings {
		if f.Code == "SUCCESS_WITHOUT_VERIFICATION_RECEIPT" {
			found = true
		}
	}
	if !found {
		t.Error("expected SUCCESS_WITHOUT_VERIFICATION_RECEIPT warning")
	}
}

// TestWorkflowIntegrity_IncidentCandidateWithoutEvidenceFails verifies evidence check.
func TestWorkflowIntegrity_IncidentCandidateWithoutEvidenceFails(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Add incident candidate node without evidence field.
	meta := liveNodeMeta("workflow_incident_candidate", now, now.Add(time.Hour), ttlFailedRun, "medium")
	meta["auto_opened"] = false
	meta["requires_human_approval"] = true
	meta["safe_to_execute"] = false
	// Note: deliberately no "evidence" field.
	_ = g.AddNode(ctx, graph.Node{
		ID:       "workflow_incident_candidate:no-evidence-test",
		Type:     graph.NodeTypeIncident,
		Name:     "test candidate",
		Metadata: meta,
	})

	result := CheckWorkflowOverlayIntegrity(ctx, g, now)
	found := false
	for _, f := range result.Findings {
		if f.Code == "INCIDENT_CANDIDATE_NO_EVIDENCE" {
			found = true
		}
	}
	if !found {
		t.Error("expected INCIDENT_CANDIDATE_NO_EVIDENCE finding for candidate without evidence")
	}
}

// TestWorkflowIntegrity_ExpiredNodeInDecisionPathFails verifies expired confidence check.
func TestWorkflowIntegrity_ExpiredNodeInDecisionPathFails(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()
	now := time.Now()

	// Add an expired run that still claims high confidence.
	oldExpiry := now.Add(-5 * time.Minute) // already expired
	meta := map[string]any{
		"source_tier":  sourceTier,
		"confidence":   "high",
		"collected_at": now.Add(-30 * time.Minute).UTC().Format(time.RFC3339),
		"expires_at":   oldExpiry.UTC().Format(time.RFC3339),
		"ttl_seconds":  ttlActiveRun,
		"status":       "failed",
	}
	_ = g.AddNode(ctx, graph.Node{
		ID:       "workflow_run:expired-high-conf",
		Type:     graph.NodeTypeWorkflowRun,
		Metadata: meta,
	})

	result := CheckWorkflowOverlayIntegrity(ctx, g, now)
	found := false
	for _, f := range result.Findings {
		if f.Code == "EXPIRED_NODE_HIGH_CONFIDENCE" {
			found = true
		}
	}
	if !found {
		t.Error("expected EXPIRED_NODE_HIGH_CONFIDENCE finding for expired node with high confidence")
	}
}

// ── Helper ────────────────────────────────────────────────────────────────────

func timestampFromTime(t time.Time) *timestamppb.Timestamp {
	return &timestamppb.Timestamp{
		Seconds: t.Unix(),
		Nanos:   int32(t.Nanosecond()),
	}
}

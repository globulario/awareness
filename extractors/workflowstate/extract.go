// Package workflowstate collects live workflow execution state from the
// workflow service gRPC API and emits expiring overlay nodes into the
// awareness graph.
//
// Design:
//   - Live run nodes carry TTL metadata and must not be treated as permanent truth.
//   - Each run links to the static workflow_definition node when one exists.
//   - If the gRPC source is unreachable, the collector reports "failed", not "clean".
//   - Empty run list from a reachable service reports "checked_clean".
//   - Failure matching (Layer 3) happens in diagnosis.go.
//   - Incident candidate generation (Layer 3) happens in incidents.go.
package workflowstate

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
)

const (
	collectorID = "workflow_execution_extractor"
	sourceTier  = "live_runtime"

	ttlActiveRun  = 15 * 60  // 15 minutes for active/recent runs
	ttlFailedRun  = 86400    // 24 hours for failed run summaries
	ttlPattern    = 7 * 86400 // 7 days for repeated failure patterns

	maxRuns = 200
)

// GRPCConnFactory returns a connected gRPC client connection to the workflow service.
type GRPCConnFactory func() (*grpc.ClientConn, error)

// CollectorHealth reports the outcome of a collection pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "checked_clean" | "partial" | "failed" | "disabled" | "unavailable"
	Coverage     string // "not_checked" | "disabled" | "checked_clean" | "checked_with_matches" | "failed" | "stale" | "partial"
	Source       string // "workflow_service_grpc"
	RunsSeen     int
	RunsIndexed  int
	FailedRuns   int
	BlockedRuns  int
	RetryStorms  int
	Error        string
	CollectedAt  string
	TTLSeconds   int
	Notes        []string
	NodesEmitted int
}

// Collect fetches recent workflow runs from the gRPC API and emits live overlay
// nodes into the graph. factory may be nil — if so, the collector reports "disabled".
func Collect(ctx context.Context, g *graph.Graph, factory GRPCConnFactory, docsAwarenessDir string) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: collectorID,
		SourceTier:  sourceTier,
		Source:      "workflow_service_grpc",
		TTLSeconds:  ttlActiveRun,
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if factory == nil {
		health.Status = "disabled"
		health.Coverage = "disabled"
		return health, nil
	}

	cc, err := factory()
	if err != nil {
		health.Status = "failed"
		health.Coverage = "failed"
		health.Error = "connect: " + err.Error()
		_ = emitCollectorFailureNode(ctx, g, "connect failed: "+err.Error())
		return health, nil
	}
	defer cc.Close()

	client := workflowpb.NewWorkflowServiceClient(cc)

	// Fetch recent runs (all statuses, limit=200).
	listCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := client.ListRuns(listCtx, &workflowpb.ListRunsRequest{
		Limit: maxRuns,
	})
	if err != nil {
		health.Status = "failed"
		health.Coverage = "failed"
		health.Error = "ListRuns: " + err.Error()
		_ = emitCollectorFailureNode(ctx, g, "ListRuns failed: "+err.Error())
		return health, nil
	}

	runs := resp.GetRuns()
	health.RunsSeen = len(runs)

	// Also fetch workflow summaries for retry storm detection.
	summResp, summErr := client.ListWorkflowSummaries(listCtx, &workflowpb.ListWorkflowSummariesRequest{})
	var summaries []*workflowpb.WorkflowRunSummary
	if summErr == nil {
		summaries = summResp.GetSummaries()
	}

	if len(runs) == 0 {
		health.Status = "ok"
		health.Coverage = "checked_clean"
		return health, nil
	}

	now := time.Now()
	expiresAt := now.Add(ttlActiveRun * time.Second)

	// Emit live snapshot node anchoring this collection.
	snapshotID := fmt.Sprintf("workflow_live_snapshot:%s", now.UTC().Format("20060102T150405Z"))
	_ = g.AddNode(ctx, graph.Node{
		ID:      snapshotID,
		Type:    graph.NodeTypeRuntimeSnapshot,
		Name:    "workflow live snapshot " + now.UTC().Format(time.RFC3339),
		Summary: fmt.Sprintf("Collected %d workflow runs at %s", len(runs), now.UTC().Format(time.RFC3339)),
		Metadata: liveNodeMeta("workflow_live_snapshot", now, expiresAt, ttlActiveRun, "high"),
	})
	health.NodesEmitted++

	// Index run nodes.
	for _, run := range runs {
		if err := indexRun(ctx, g, run, snapshotID, now); err != nil {
			health.Notes = append(health.Notes, "run "+run.GetId()+": "+err.Error())
			continue
		}
		health.RunsIndexed++
		health.NodesEmitted++

		if run.GetStatus() == workflowpb.RunStatus_RUN_STATUS_FAILED {
			health.FailedRuns++
		}
		if run.GetStatus() == workflowpb.RunStatus_RUN_STATUS_BLOCKED {
			health.BlockedRuns++
		}
	}

	// Index summary nodes and detect retry storms.
	for _, s := range summaries {
		if err := indexSummary(ctx, g, s, now); err != nil {
			health.Notes = append(health.Notes, "summary "+s.GetWorkflowName()+": "+err.Error())
			continue
		}
		health.NodesEmitted++
		// Retry storm: >5 failure runs in summary window.
		if s.GetFailureRuns() > 5 {
			health.RetryStorms++
		}
	}

	// Run diagnosis: match failures to failure modes/invariants.
	diagResults := diagnoseRuns(ctx, g, runs, docsAwarenessDir, now)
	health.NodesEmitted += diagResults.nodesEmitted

	// Collect step proof (receipts) via ListStepOutcomes for each unique workflow.
	wfNames := uniqueWorkflowNames(runs)
	for _, wfName := range wfNames {
		n, warnings := CollectStepProof(ctx, g, client, wfName, now)
		health.NodesEmitted += n
		health.Notes = append(health.Notes, warnings...)
	}
	// Cross-link receipts to runs.
	for _, wfName := range wfNames {
		linkReceiptsToRuns(ctx, g, wfName)
	}

	// Emit typed impact edges for failed runs.
	for _, run := range runs {
		if run.GetStatus() == workflowpb.RunStatus_RUN_STATUS_FAILED {
			emitTypedFailureEdges(ctx, g, run, now)
		}
	}

	// Run incident candidate generation — only from fresh evidence.
	candidates := generateCandidates(runs, summaries, now)
	for _, c := range candidates {
		if err := emitIncidentCandidate(ctx, g, c, now); err == nil {
			health.NodesEmitted++
		}
	}

	// Run workflow overlay integrity check and emit findings.
	integrityResult := CheckWorkflowOverlayIntegrity(ctx, g, now)
	emitted := EmitIntegrityFindings(ctx, g, integrityResult, now)
	health.NodesEmitted += emitted
	if integrityResult.CriticalCount > 0 {
		health.Notes = append(health.Notes, fmt.Sprintf("integrity: %d critical, %d warning findings", integrityResult.CriticalCount, integrityResult.WarningCount))
		health.Status = "partial"
	}

	if health.Status != "partial" {
		health.Status = "ok"
	}
	health.Coverage = "checked_with_matches"
	return health, nil
}

// emitTypedFailureEdges emits precise typed edges from a failed run:
//   workflow_run → workflow_run_has_step_run → workflow_step_run
//   workflow_step_run → workflow_step_run_failed_with_error → workflow_error
//   workflow_run → workflow_run_forbids_action → action:blind_retry (when retry_count > 3)
func emitTypedFailureEdges(ctx context.Context, g *graph.Graph, run *workflowpb.WorkflowRun, now time.Time) {
	runID := "workflow_run:" + run.GetId()

	// Emit workflow_error node and wire typed edges.
	if run.GetErrorMessage() != "" {
		errID := fmt.Sprintf("workflow_error:%s", run.GetId()[:8])
		expiresAt := now.Add(ttlFailedRun * time.Second)
		_ = g.AddNode(ctx, graph.Node{
			ID:      errID,
			Type:    graph.NodeTypeWorkflowError,
			Name:    "error:" + run.GetWorkflowName(),
			Summary: run.GetErrorMessage(),
			Metadata: func() map[string]any {
				m := liveNodeMeta("workflow_error", now, expiresAt, ttlFailedRun, "high")
				m["failure_class"] = run.GetFailureClass().String()
				m["error_message"] = run.GetErrorMessage()
				return m
			}(),
		})
		// step_run → failed_with_error → workflow_error.
		stepRunID := "workflow_step_run:" + run.GetId() + ".failed_step"
		if n, _ := g.FindNode(ctx, stepRunID); n != nil {
			_ = g.AddEdge(ctx, graph.Edge{
				Src: stepRunID, Kind: graph.EdgeWorkflowStepRunFailedWithError, Dst: errID, Phase: "live",
			})
		}
		// run → has_step_run.
		_ = g.AddEdge(ctx, graph.Edge{
			Src: runID, Kind: graph.EdgeWorkflowRunHasStepRun, Dst: stepRunID, Phase: "live",
		})
	}

	// Emit forbidden blind-retry action edge when retry count is high.
	if run.GetRetryCount() > 3 {
		actionID := "action:blind_retry_without_terminal_classification"
		_ = g.AddNode(ctx, graph.Node{
			ID:   actionID,
			Type: graph.NodeTypeForbiddenFix,
			Name: "blind_retry_without_terminal_classification",
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src: runID, Kind: graph.EdgeWorkflowRunForbidsAction, Dst: actionID, Phase: "live",
		})
	}

	// Wire step_run → instantiates → static workflow_step (when wait_reason provides step name).
	if run.GetWaitReason() != "" && run.GetWorkflowName() != "" {
		stepRunID := "workflow_step_run:" + run.GetId() + ".failed_step"
		staticStepID := "workflow_step:" + run.GetWorkflowName() + "." + run.GetWaitReason()
		if existing, _ := g.FindNode(ctx, staticStepID); existing != nil {
			_ = g.AddEdge(ctx, graph.Edge{
				Src: stepRunID, Kind: graph.EdgeWorkflowStepRunInstantiatesStep, Dst: staticStepID, Phase: "live",
			})
			// step → verifies → invariant via static step metadata.
			stepEdges, _ := g.OutgoingEdges(ctx, staticStepID)
			for _, e := range stepEdges {
				if e.Kind == graph.EdgeRequires {
					_ = g.AddEdge(ctx, graph.Edge{
						Src: stepRunID, Kind: graph.EdgeWorkflowStepVerifiesInvariant, Dst: e.Dst, Phase: "live",
					})
				}
			}
		}
	}
}

// uniqueWorkflowNames returns deduped workflow names from a run list.
func uniqueWorkflowNames(runs []*workflowpb.WorkflowRun) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range runs {
		if n := r.GetWorkflowName(); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// indexRun emits a workflow_run node and links it to its static definition.
func indexRun(ctx context.Context, g *graph.Graph, run *workflowpb.WorkflowRun, snapshotID string, now time.Time) error {
	wfName := run.GetWorkflowName()
	status := run.GetStatus()
	isFailed := status == workflowpb.RunStatus_RUN_STATUS_FAILED
	isBlocked := status == workflowpb.RunStatus_RUN_STATUS_BLOCKED

	ttl := ttlActiveRun
	if isFailed {
		ttl = ttlFailedRun
	}
	expiresAt := now.Add(time.Duration(ttl) * time.Second)

	confidence := "high"
	if isBlocked {
		confidence = "medium"
	}

	runID := "workflow_run:" + run.GetId()

	meta := liveNodeMeta("workflow_run", now, expiresAt, ttl, confidence)
	meta["workflow_name"] = wfName
	meta["status"] = statusLabel(status)
	meta["failure_class"] = run.GetFailureClass().String()
	meta["error_message"] = run.GetErrorMessage()
	meta["retry_count"] = run.GetRetryCount()
	meta["retry_attempt"] = run.GetRetryAttempt()
	meta["max_retries"] = run.GetMaxRetries()
	meta["wait_reason"] = run.GetWaitReason()
	meta["acknowledged"] = run.GetAcknowledged()
	if run.GetContext() != nil {
		meta["node_id"] = run.GetContext().GetNodeId()
		meta["node_hostname"] = run.GetContext().GetNodeHostname()
		meta["component_name"] = run.GetContext().GetComponentName()
		meta["component_version"] = run.GetContext().GetComponentVersion()
	}
	if ts := run.GetStartedAt(); ts != nil {
		meta["started_at"] = ts.AsTime().UTC().Format(time.RFC3339)
	}
	if ts := run.GetFinishedAt(); ts != nil {
		meta["finished_at"] = ts.AsTime().UTC().Format(time.RFC3339)
	}

	summary := fmt.Sprintf("run %s status=%s", run.GetId()[:8], statusLabel(status))
	if run.GetErrorMessage() != "" {
		summary += " error=" + truncate(run.GetErrorMessage(), 80)
	}

	if err := g.AddNode(ctx, graph.Node{
		ID:       runID,
		Type:     graph.NodeTypeWorkflowRun,
		Name:     wfName + "/" + run.GetId()[:8],
		Summary:  summary,
		Metadata: meta,
	}); err != nil {
		return err
	}

	// Link to live snapshot.
	_ = g.AddEdge(ctx, graph.Edge{Src: snapshotID, Kind: graph.EdgeCapturedIn, Dst: runID, Phase: "live"})

	// Link run → static workflow definition.
	if wfName != "" {
		defID := "workflow:" + wfName
		existing, _ := g.FindNode(ctx, defID)
		if existing != nil {
			_ = g.AddEdge(ctx, graph.Edge{
				Src:   runID,
				Kind:  graph.EdgeWorkflowRunInstantiates,
				Dst:   defID,
				Phase: "live",
			})
		} else {
			// Emit blind-spot node.
			bsID := "workflow_run_definition_missing:" + wfName
			_ = g.AddNode(ctx, graph.Node{
				ID:      bsID,
				Type:    graph.NodeTypeRemainingGap,
				Name:    "missing workflow definition: " + wfName,
				Summary: "workflow_run references '" + wfName + "' but no static definition node exists in graph",
			})
			_ = g.AddEdge(ctx, graph.Edge{Src: runID, Kind: graph.EdgeWorkflowRunInstantiates, Dst: bsID, Phase: "live"})
		}
	}

	// Link run → target node/service if context present.
	if run.GetContext() != nil {
		wctx := run.GetContext()
		if nodeID := wctx.GetNodeId(); nodeID != "" {
			_ = g.AddEdge(ctx, graph.Edge{
				Src: runID, Kind: graph.EdgeWorkflowRunTargetsNode, Dst: "node:" + nodeID, Phase: "live",
			})
		}
		if svc := wctx.GetComponentName(); svc != "" {
			svcID := "service:" + svc
			_ = g.AddNode(ctx, graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: svc})
			_ = g.AddEdge(ctx, graph.Edge{
				Src: runID, Kind: graph.EdgeWorkflowRunTargetsService, Dst: svcID, Phase: "live",
			})
		}
	}

	// Link failed run to its failed step if known.
	if isFailed && run.GetWaitReason() != "" {
		stepRunID := "workflow_step_run:" + run.GetId() + ".failed_step"
		_ = g.AddNode(ctx, graph.Node{
			ID:      stepRunID,
			Type:    graph.NodeTypeWorkflowStepRun,
			Name:    "failed step for " + run.GetId()[:8],
			Summary: "failed_step: " + run.GetWaitReason(),
			Metadata: liveNodeMeta("workflow_step_run", now, expiresAt, ttl, confidence),
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src: runID, Kind: graph.EdgeWorkflowRunFailedAtStep, Dst: stepRunID, Phase: "live",
		})
		// Try to link step_run to static workflow_step.
		if wfName != "" {
			staticStepID := "workflow_step:" + wfName + "." + run.GetWaitReason()
			existing, _ := g.FindNode(ctx, staticStepID)
			if existing != nil {
				_ = g.AddEdge(ctx, graph.Edge{
					Src: stepRunID, Kind: graph.EdgeDependsOn, Dst: staticStepID, Phase: "live",
				})
			}
		}
	}

	return nil
}

// indexSummary emits a workflow_retry_record node summarising aggregate run stats.
func indexSummary(ctx context.Context, g *graph.Graph, s *workflowpb.WorkflowRunSummary, now time.Time) error {
	summID := "workflow_summary:" + s.GetWorkflowName()
	expiresAt := now.Add(ttlFailedRun * time.Second)

	meta := liveNodeMeta("workflow_summary", now, expiresAt, ttlFailedRun, "medium")
	meta["workflow_name"] = s.GetWorkflowName()
	meta["total_runs"] = s.GetTotalRuns()
	meta["success_runs"] = s.GetSuccessRuns()
	meta["failure_runs"] = s.GetFailureRuns()
	meta["last_failure_reason"] = s.GetLastFailureReason()

	if err := g.AddNode(ctx, graph.Node{
		ID:      summID,
		Type:    graph.NodeTypeWorkflowRetryRecord,
		Name:    "summary:" + s.GetWorkflowName(),
		Summary: fmt.Sprintf("workflow=%s total=%d success=%d failed=%d", s.GetWorkflowName(), s.GetTotalRuns(), s.GetSuccessRuns(), s.GetFailureRuns()),
		Metadata: meta,
	}); err != nil {
		return err
	}

	// Link summary to static definition.
	if defID := "workflow:" + s.GetWorkflowName(); s.GetWorkflowName() != "" {
		_ = g.AddEdge(ctx, graph.Edge{
			Src: summID, Kind: graph.EdgeWorkflowRunInstantiates, Dst: defID, Phase: "live",
		})
	}

	return nil
}

// emitCollectorFailureNode writes a failure sentinel into the graph so that
// agents can see the collection was attempted but failed.
func emitCollectorFailureNode(ctx context.Context, g *graph.Graph, errMsg string) error {
	return g.AddNode(ctx, graph.Node{
		ID:      "workflow_collection_failure:" + fmt.Sprintf("%d", time.Now().Unix()),
		Type:    graph.NodeTypeRemainingGap,
		Name:    "workflow collection failed",
		Summary: "workflow execution extractor could not reach source: " + errMsg,
		Metadata: map[string]any{
			"source_tier":        sourceTier,
			"collector":          collectorID,
			"confidence_impact":  "lowers_runtime_confidence",
			"collected_at":       time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// liveNodeMeta returns the standard TTL/freshness metadata for live overlay nodes.
func liveNodeMeta(nodeKind string, now, expiresAt time.Time, ttl int, confidence string) map[string]any {
	return map[string]any{
		"source_tier":   sourceTier,
		"collector":     collectorID,
		"source":        "workflow_service_grpc",
		"cluster_scope": "cluster_wide",
		"collected_at":  now.UTC().Format(time.RFC3339),
		"ttl_seconds":   ttl,
		"expires_at":    expiresAt.UTC().Format(time.RFC3339),
		"trust_level":   "observed",
		"confidence":    confidence,
		"node_kind":     nodeKind,
	}
}

// statusLabel converts a RunStatus proto to a short lowercase string.
func statusLabel(s workflowpb.RunStatus) string {
	switch s {
	case workflowpb.RunStatus_RUN_STATUS_PENDING:
		return "pending"
	case workflowpb.RunStatus_RUN_STATUS_EXECUTING:
		return "executing"
	case workflowpb.RunStatus_RUN_STATUS_BLOCKED:
		return "blocked"
	case workflowpb.RunStatus_RUN_STATUS_RETRYING:
		return "retrying"
	case workflowpb.RunStatus_RUN_STATUS_SUCCEEDED:
		return "succeeded"
	case workflowpb.RunStatus_RUN_STATUS_FAILED:
		return "failed"
	case workflowpb.RunStatus_RUN_STATUS_CANCELED:
		return "canceled"
	case workflowpb.RunStatus_RUN_STATUS_ROLLED_BACK:
		return "rolled_back"
	case workflowpb.RunStatus_RUN_STATUS_SUPERSEDED:
		return "superseded"
	default:
		return "unknown"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

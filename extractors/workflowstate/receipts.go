// receipts.go — collects step-level proof from the workflow service.
//
// Architecture note:
//   Step receipts in Globular are stored in ScyllaDB (workflow.step_receipts),
//   not in a filesystem directory. The proof source is the workflow service gRPC
//   API via ListStepOutcomes, not a receipts_dir path.
//   receipts_dir: N/A — Scylla-backed, accessible only through the service API.
//
// Each WorkflowStepOutcome is the proof-equivalent of a receipt:
//   - It records step-level success/failure counts, last status, and error messages.
//   - It is linked to workflow_step_run nodes as emitted receipts.
//   - Missing step outcomes for verified steps produce step_receipt_missing warnings.
package workflowstate

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
)

// CollectStepProof fetches step outcomes for a workflow name from the gRPC service
// and emits workflow_receipt proof nodes into the graph, linked to any existing
// workflow_step_run or workflow_run nodes for the same workflow.
//
// This is called after Collect() so that run nodes already exist.
// If the gRPC call fails, it emits a step_proof_unavailable warning and returns.
func CollectStepProof(
	ctx context.Context,
	g *graph.Graph,
	client workflowpb.WorkflowServiceClient,
	workflowName string,
	now time.Time,
) (int, []string) {
	nodesEmitted := 0
	var warnings []string

	listCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	resp, err := client.ListStepOutcomes(listCtx, &workflowpb.ListStepOutcomesRequest{
		WorkflowName: workflowName,
	})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("step proof for %s unavailable: %v", workflowName, err))
		return nodesEmitted, warnings
	}

	expiresAt := now.Add(ttlFailedRun * time.Second)

	for _, outcome := range resp.GetOutcomes() {
		// Each step outcome is a proof record.
		receiptID := fmt.Sprintf("workflow_receipt:%s.%s", workflowName, outcome.GetStepId())

		// Determine verification status from last outcome.
		lastStatus := outcome.GetLastStatus()
		verificationStatus := "unverified"
		confidence := "medium"
		if lastStatus == workflowpb.StepStatus_STEP_STATUS_SUCCEEDED {
			verificationStatus = "verified"
			confidence = "high"
		} else if lastStatus == workflowpb.StepStatus_STEP_STATUS_FAILED {
			verificationStatus = "failed"
			confidence = "low"
		}

		meta := liveNodeMeta("workflow_receipt", now, expiresAt, ttlFailedRun, confidence)
		meta["workflow_name"] = workflowName
		meta["step_id"] = outcome.GetStepId()
		meta["verification_status"] = verificationStatus
		meta["total_executions"] = outcome.GetTotalExecutions()
		meta["success_count"] = outcome.GetSuccessCount()
		meta["failure_count"] = outcome.GetFailureCount()
		meta["last_error_message"] = outcome.GetLastErrorMessage()
		meta["receipt_source"] = "workflow_service_grpc_step_outcomes"
		meta["receipts_dir"] = "N/A — Scylla-backed, accessed via gRPC ListStepOutcomes"

		var summary string
		if verificationStatus == "verified" {
			summary = fmt.Sprintf("step %s verified (%d executions, %d success)", outcome.GetStepId(), outcome.GetTotalExecutions(), outcome.GetSuccessCount())
		} else {
			summary = fmt.Sprintf("step %s status=%s (%d failures)", outcome.GetStepId(), verificationStatus, outcome.GetFailureCount())
		}

		if err := g.AddNode(ctx, graph.Node{
			ID:       receiptID,
			Type:     graph.NodeTypeWorkflowReceipt,
			Name:     fmt.Sprintf("step proof: %s.%s", workflowName, outcome.GetStepId()),
			Summary:  summary,
			Metadata: meta,
		}); err != nil {
			warnings = append(warnings, fmt.Sprintf("receipt node %s: %v", receiptID, err))
			continue
		}
		nodesEmitted++

		// Wire: receipt → static workflow_step (instantiates_step semantics).
		staticStepID := "workflow_step:" + workflowName + "." + outcome.GetStepId()
		if existing, _ := g.FindNode(ctx, staticStepID); existing != nil {
			_ = g.AddEdge(ctx, graph.Edge{
				Src: receiptID, Kind: graph.EdgeWorkflowReceiptProvesStepEffect, Dst: staticStepID, Phase: "live",
			})
		}

		// Wire: receipt → failed_error node when step failed.
		if outcome.GetFailureCount() > 0 && outcome.GetLastErrorMessage() != "" {
			errID := fmt.Sprintf("workflow_error:%s.%s", workflowName, outcome.GetStepId())
			_ = g.AddNode(ctx, graph.Node{
				ID:      errID,
				Type:    graph.NodeTypeWorkflowError,
				Name:    fmt.Sprintf("error: %s.%s", workflowName, outcome.GetStepId()),
				Summary: outcome.GetLastErrorMessage(),
				Metadata: map[string]any{
					"source_tier":    sourceTier,
					"collector":      collectorID,
					"error_message":  outcome.GetLastErrorMessage(),
					"last_error_code": outcome.GetLastErrorCode(),
					"failure_count":  outcome.GetFailureCount(),
				},
			})
			_ = g.AddEdge(ctx, graph.Edge{
				Src: receiptID, Kind: graph.EdgeWorkflowReceiptRecordsError, Dst: errID, Phase: "live",
			})
			nodesEmitted++
		}

		// Warn: success run with zero success receipts for a step where verification is expected.
		if outcome.GetSuccessCount() == 0 && outcome.GetTotalExecutions() > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"step_receipt_missing: workflow=%s step=%s has %d executions but zero verified successes",
				workflowName, outcome.GetStepId(), outcome.GetTotalExecutions()))
		}
	}

	return nodesEmitted, warnings
}

// linkReceiptsToRuns cross-links existing workflow_receipt nodes to workflow_run nodes
// for the same workflow. Called after both are emitted.
func linkReceiptsToRuns(ctx context.Context, g *graph.Graph, workflowName string) {
	runs, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowRun)
	receipts, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowReceipt)

	for _, receipt := range receipts {
		wfn, _ := receipt.Metadata["workflow_name"].(string)
		if wfn != workflowName {
			continue
		}
		stepID, _ := receipt.Metadata["step_id"].(string)
		for _, run := range runs {
			runWFN, _ := run.Metadata["workflow_name"].(string)
			if runWFN != workflowName {
				continue
			}
			// Link step_run → receipt for the matching step.
			stepRunID := "workflow_step_run:" + run.ID[len("workflow_run:"):] + "." + stepID
			if existing, _ := g.FindNode(ctx, stepRunID); existing != nil {
				_ = g.AddEdge(ctx, graph.Edge{
					Src: stepRunID, Kind: graph.EdgeWorkflowStepRunEmittedReceipt, Dst: receipt.ID, Phase: "live",
				})
			}
		}
	}
}

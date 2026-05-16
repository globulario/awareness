// integrity.go — workflow overlay integrity checks.
//
// These checks detect stale, unlinked, or proofless reasoning in the live
// workflow execution overlay. They are not confidence-scored graph matches —
// they are structural integrity assertions about the overlay's own health.
package workflowstate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
)

// IntegrityFinding is a single integrity violation found in the workflow overlay.
type IntegrityFinding struct {
	Code     string // machine-readable code
	Severity string // "critical" | "warning"
	Message  string
	NodeID   string // node that triggered the finding (if applicable)
}

// IntegrityResult holds all findings from a workflow overlay integrity check.
type IntegrityResult struct {
	Findings     []IntegrityFinding
	CriticalCount int
	WarningCount  int
}

// CheckWorkflowOverlayIntegrity runs all integrity checks against the live
// workflow overlay in the graph. Returns findings without modifying the graph.
func CheckWorkflowOverlayIntegrity(ctx context.Context, g *graph.Graph, now time.Time) IntegrityResult {
	var res IntegrityResult

	runs, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowRun)
	receipts, _ := g.FindNodesByType(ctx, graph.NodeTypeWorkflowReceipt)
	incidents, _ := g.FindNodesByType(ctx, graph.NodeTypeIncident)

	// Build sets for fast lookup.
	runIDs := map[string]bool{}
	for _, r := range runs {
		runIDs[r.ID] = true
	}

	receiptByWorkflow := map[string]bool{}
	for _, r := range receipts {
		if wfn, ok := r.Metadata["workflow_name"].(string); ok {
			receiptByWorkflow[wfn] = true
		}
	}

	// Check 1: workflow_run without instantiates_definition link.
	for _, run := range runs {
		edges, err := g.OutgoingEdges(ctx, run.ID)
		if err != nil {
			continue
		}
		hasDefinitionLink := false
		for _, e := range edges {
			if e.Kind == graph.EdgeWorkflowRunInstantiates {
				hasDefinitionLink = true
				break
			}
		}
		if !hasDefinitionLink {
			res.addWarning("WORKFLOW_RUN_NO_DEFINITION",
				fmt.Sprintf("workflow_run %s has no definition link — impact paths from this run will be incomplete", run.ID),
				run.ID)
		}
	}

	// Check 2: expired workflow data driving incident candidates.
	for _, inc := range incidents {
		// Only check workflow-originated incidents.
		if !strings.HasPrefix(inc.ID, "workflow_incident_candidate:") {
			continue
		}
		freshness := CheckFreshness(inc.Metadata, now)
		if freshness.State == FreshnessExpired {
			res.addCritical("EXPIRED_EVIDENCE_IN_INCIDENT_CANDIDATE",
				fmt.Sprintf("incident candidate %s is based on expired workflow evidence — it must not drive decisions", inc.ID),
				inc.ID)
		}
		// Check: incident candidate without evidence field.
		ev, _ := inc.Metadata["evidence"].(string)
		if ev == "" {
			res.addCritical("INCIDENT_CANDIDATE_NO_EVIDENCE",
				fmt.Sprintf("incident candidate %s has no evidence field — unproven candidates are forbidden", inc.ID),
				inc.ID)
		}
	}

	// Check 3: success classification without verification receipt where proof is expected.
	for _, run := range runs {
		status, _ := run.Metadata["status"].(string)
		if status != "succeeded" {
			continue
		}
		wfName, _ := run.Metadata["workflow_name"].(string)
		// If we collected step outcomes for this workflow, verify at least one success receipt exists.
		if wfName != "" && !receiptByWorkflow[wfName] {
			res.addWarning("SUCCESS_WITHOUT_VERIFICATION_RECEIPT",
				fmt.Sprintf("workflow_run %s (status=succeeded) has no step proof receipts — success cannot be independently verified", run.ID),
				run.ID)
		}
	}

	// Check 4: expired workflow run nodes still have high confidence metadata.
	for _, run := range runs {
		freshness := CheckFreshness(run.Metadata, now)
		if freshness.State == FreshnessExpired {
			conf, _ := run.Metadata["confidence"].(string)
			if conf == "high" || conf == "medium" {
				res.addCritical("EXPIRED_NODE_HIGH_CONFIDENCE",
					fmt.Sprintf("workflow_run %s is expired but still has confidence=%s — expired data must not drive high-confidence decisions", run.ID, conf),
					run.ID)
			}
		}
	}

	// Check 5: failed run without any failure mode linkage.
	for _, run := range runs {
		status, _ := run.Metadata["status"].(string)
		if status != "failed" {
			continue
		}
		edges, _ := g.OutgoingEdges(ctx, run.ID)
		hasFailureModeLink := false
		for _, e := range edges {
			if e.Kind == graph.EdgeWorkflowFailureIndicates || e.Kind == graph.EdgeWorkflowFailureRisksInvariant {
				hasFailureModeLink = true
				break
			}
		}
		if !hasFailureModeLink {
			res.addWarning("FAILED_RUN_NO_FAILURE_MODE",
				fmt.Sprintf("workflow_run %s (status=failed) has no failure mode link — impact paths cannot trace to invariants", run.ID),
				run.ID)
		}
	}

	res.CriticalCount = 0
	res.WarningCount = 0
	for _, f := range res.Findings {
		if f.Severity == "critical" {
			res.CriticalCount++
		} else {
			res.WarningCount++
		}
	}

	return res
}

func (r *IntegrityResult) addCritical(code, msg, nodeID string) {
	r.Findings = append(r.Findings, IntegrityFinding{
		Code: code, Severity: "critical", Message: msg, NodeID: nodeID,
	})
}

func (r *IntegrityResult) addWarning(code, msg, nodeID string) {
	r.Findings = append(r.Findings, IntegrityFinding{
		Code: code, Severity: "warning", Message: msg, NodeID: nodeID,
	})
}

// EmitIntegrityFindings writes integrity findings into the graph as
// workflow_integrity_finding nodes. This makes them queryable by agents.
func EmitIntegrityFindings(ctx context.Context, g *graph.Graph, res IntegrityResult, now time.Time) int {
	emitted := 0
	for _, f := range res.Findings {
		id := fmt.Sprintf("workflow_integrity:%s:%d", f.Code, now.UnixNano())
		expiresAt := now.Add(ttlFailedRun * time.Second)
		meta := liveNodeMeta("workflow_integrity_finding", now, expiresAt, ttlFailedRun, "high")
		meta["code"] = f.Code
		meta["severity"] = f.Severity
		meta["subject_node"] = f.NodeID

		if err := g.AddNode(ctx, graph.Node{
			ID:      id,
			Type:    graph.NodeTypeWorkflowIntegrityFinding,
			Name:    f.Code,
			Summary: f.Message,
			Metadata: meta,
		}); err == nil {
			emitted++
			// Link to subject node if present.
			if f.NodeID != "" {
				_ = g.AddEdge(ctx, graph.Edge{
					Src: id, Kind: graph.EdgeAffects, Dst: f.NodeID, Phase: "live",
				})
			}
		}
	}
	return emitted
}

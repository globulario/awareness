package workflowstate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
)

const (
	retryStormThreshold = 5 // failure_runs > this → retry storm
)

// incidentCandidate describes a potential incident derived from workflow evidence.
// These are NEVER auto-opened — they require human review and approval.
type incidentCandidate struct {
	Title                   string
	Severity                string // "warning" | "critical"
	Confidence              string // "low" | "medium" | "high"
	WorkflowName            string
	Evidence                []string
	MatchedFailureModes     []string
	ThreatenedInvariants    []string
	RecommendedNextAction   string
	SuggestedIncidentYAML   string
	// Safety fields — always set, never overridable.
	AutoOpened            bool // always false
	RequiresHumanApproval bool // always true
	SafeToExecute         bool // always false — candidates are not executable
	// Freshness fields.
	EvidenceFreshness FreshnessState
}

// safeCandidateDefaults returns the mandatory safety fields for every incident candidate.
func safeCandidateDefaults() (autoOpened, requiresHumanApproval, safeToExecute bool) {
	return false, true, false
}

// generateCandidates inspects runs and summaries and returns incident candidates.
// Expired evidence is skipped — expired workflow data must not drive active candidates.
// Safety fields (auto_opened=false, requires_human_approval=true, safe_to_execute=false)
// are always set and cannot be overridden.
func generateCandidates(
	runs []*workflowpb.WorkflowRun,
	summaries []*workflowpb.WorkflowRunSummary,
	now time.Time,
) []incidentCandidate {
	var candidates []incidentCandidate
	autoOpened, requiresHumanApproval, safeToExecute := safeCandidateDefaults()

	// From summaries: retry storms.
	for _, s := range summaries {
		if s.GetFailureRuns() > retryStormThreshold {
			candidates = append(candidates, incidentCandidate{
				Title:        fmt.Sprintf("Workflow %s retry storm: %d failures", s.GetWorkflowName(), s.GetFailureRuns()),
				Severity:     "warning",
				Confidence:   "medium",
				WorkflowName: s.GetWorkflowName(),
				Evidence: []string{
					fmt.Sprintf("total_runs=%d success=%d failed=%d", s.GetTotalRuns(), s.GetSuccessRuns(), s.GetFailureRuns()),
					"last_failure_reason=" + s.GetLastFailureReason(),
				},
				RecommendedNextAction: "review_and_approve",
				SuggestedIncidentYAML: suggestedYAML("retry_storm", s.GetWorkflowName(), ""),
				AutoOpened:            autoOpened,
				RequiresHumanApproval: requiresHumanApproval,
				SafeToExecute:         safeToExecute,
				EvidenceFreshness:     FreshnessFresh, // summaries don't carry TTL
			})
		}
	}

	// From individual runs: critical failures, verification skips, long-blocked.
	for _, run := range runs {
		// Build a fake metadata map to check freshness using run timestamps.
		// Runs that were started/finished outside the active TTL window are treated as stale.
		runMeta := map[string]any{
			"ttl_seconds": ttlActiveRun,
			"collected_at": now.UTC().Format(time.RFC3339),
			"expires_at":   now.Add(ttlActiveRun * time.Second).UTC().Format(time.RFC3339),
		}
		if ts := run.GetStartedAt(); ts != nil {
			startedAt := ts.AsTime()
			if now.Sub(startedAt).Seconds() > ttlActiveRun {
				// Run is older than TTL — treat as expired for incident candidate purposes.
				runMeta["expires_at"] = startedAt.Add(ttlActiveRun * time.Second).UTC().Format(time.RFC3339)
			}
		}
		freshness := CheckFreshness(runMeta, now)

		// Expired evidence must not drive active incident candidates.
		if freshness.State == FreshnessExpired {
			continue
		}

		if run.GetStatus() == workflowpb.RunStatus_RUN_STATUS_FAILED {
			// High retry count on a single run.
			if run.GetRetryCount() > 3 {
				candidates = append(candidates, incidentCandidate{
					Title:        fmt.Sprintf("Workflow %s deterministic failure (retries=%d)", run.GetWorkflowName(), run.GetRetryCount()),
					Severity:     "warning",
					Confidence:   effectiveConfidence("medium", freshness),
					WorkflowName: run.GetWorkflowName(),
					Evidence: []string{
						"run_id=" + run.GetId(),
						"retry_count=" + fmt.Sprint(run.GetRetryCount()),
						"error=" + truncate(run.GetErrorMessage(), 120),
						"failure_class=" + run.GetFailureClass().String(),
					},
					RecommendedNextAction: "review_and_approve",
					SuggestedIncidentYAML: suggestedYAML("deterministic_failure", run.GetWorkflowName(), run.GetId()),
					AutoOpened:            autoOpened,
					RequiresHumanApproval: requiresHumanApproval,
					SafeToExecute:         safeToExecute,
					EvidenceFreshness:     freshness.State,
				})
			}

			// Validation failure class → verification skipped / convergence threat.
			if run.GetFailureClass() == workflowpb.FailureClass_FAILURE_CLASS_VALIDATION {
				candidates = append(candidates, incidentCandidate{
					Title:        fmt.Sprintf("Workflow %s verification failure — convergence not confirmed", run.GetWorkflowName()),
					Severity:     "warning",
					Confidence:   effectiveConfidence("high", freshness),
					WorkflowName: run.GetWorkflowName(),
					Evidence: []string{
						"run_id=" + run.GetId(),
						"failure_class=VALIDATION",
						"error=" + truncate(run.GetErrorMessage(), 120),
					},
					ThreatenedInvariants: []string{"desired_installed_runtime_must_converge"},
					RecommendedNextAction: "review_and_approve",
					SuggestedIncidentYAML: suggestedYAML("verification_gap", run.GetWorkflowName(), run.GetId()),
					AutoOpened:            autoOpened,
					RequiresHumanApproval: requiresHumanApproval,
					SafeToExecute:         safeToExecute,
					EvidenceFreshness:     freshness.State,
				})
			}
		}

		// Blocked workflow.
		if run.GetStatus() == workflowpb.RunStatus_RUN_STATUS_BLOCKED {
			candidates = append(candidates, incidentCandidate{
				Title:        fmt.Sprintf("Workflow %s blocked: %s", run.GetWorkflowName(), truncate(run.GetWaitReason(), 60)),
				Severity:     "warning",
				Confidence:   effectiveConfidence("low", freshness),
				WorkflowName: run.GetWorkflowName(),
				Evidence: []string{
					"run_id=" + run.GetId(),
					"wait_reason=" + run.GetWaitReason(),
				},
				RecommendedNextAction: "review_and_approve",
				SuggestedIncidentYAML: suggestedYAML("blocked_workflow", run.GetWorkflowName(), run.GetId()),
				AutoOpened:            autoOpened,
				RequiresHumanApproval: requiresHumanApproval,
				SafeToExecute:         safeToExecute,
				EvidenceFreshness:     freshness.State,
			})
		}
	}

	return candidates
}

// emitIncidentCandidate writes an incident candidate node into the graph.
// It does NOT auto-open an incident.
func emitIncidentCandidate(ctx context.Context, g *graph.Graph, c incidentCandidate, now time.Time) error {
	id := fmt.Sprintf("workflow_incident_candidate:%s:%d", sanitize(c.WorkflowName), now.Unix())
	expiresAt := now.Add(ttlFailedRun * time.Second)

	meta := liveNodeMeta("workflow_incident_candidate", now, expiresAt, ttlFailedRun, c.Confidence)
	meta["title"] = c.Title
	meta["severity"] = c.Severity
	meta["workflow_name"] = c.WorkflowName
	meta["evidence"] = strings.Join(c.Evidence, "; ")
	meta["matched_failure_modes"] = strings.Join(c.MatchedFailureModes, ", ")
	meta["threatened_invariants"] = strings.Join(c.ThreatenedInvariants, ", ")
	meta["recommended_next_action"] = c.RecommendedNextAction
	meta["suggested_incident_yaml"] = c.SuggestedIncidentYAML
	// Safety fields — always false/true/false, never overridable.
	meta["auto_opened"] = false
	meta["requires_human_approval"] = true
	meta["safe_to_execute"] = false
	meta["evidence_freshness"] = string(c.EvidenceFreshness)

	return g.AddNode(ctx, graph.Node{
		ID:      id,
		Type:    graph.NodeTypeIncident,
		Name:    c.Title,
		Summary: c.Title + " [candidate — requires human review]",
		Metadata: meta,
	})
}

func suggestedYAML(kind, wfName, runID string) string {
	runLine := ""
	if runID != "" {
		runLine = "\n  run_id: " + runID
	}
	return fmt.Sprintf(`# Incident candidate — requires human approval before opening
# kind: %s
# Generated by workflow_execution_extractor (NOT auto-approved)
id: "INC-DRAFT-%s"
workflow_name: "%s"%s
status: candidate
requires_approval: true`, kind, sanitize(wfName), wfName, runLine)
}

func sanitize(s string) string {
	return strings.NewReplacer(".", "-", "/", "-", " ", "-").Replace(s)
}

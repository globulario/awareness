package runtime_test

import (
	"testing"

	"github.com/globulario/awareness/runtime"
)

// TestFailedWorkflowMatchesFailureMode verifies that a FAILED workflow is
// keyword-matched against known failure mode IDs.
func TestFailedWorkflowMatchesFailureMode(t *testing.T) {
	snap := baseSnapshot()
	snap.WorkflowReceipts = []runtime.WorkflowReceipt{
		{
			WorkflowID:   "wf-001",
			WorkflowType: "package_install restart storm recovery",
			Status:       "FAILED",
			ErrorMsg:     "restart storm detected",
		},
	}

	knownFMs := []string{
		"failure.restart_storm",
		"failure.metadata_conflict",
	}

	result := snap.Match(nil, knownFMs)

	found := false
	for _, id := range result.MatchedFailureModes {
		if id == "failure.restart_storm" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected failure.restart_storm in MatchedFailureModes, got: %v", result.MatchedFailureModes)
	}
}

// TestTimedOutWorkflowMatchesFailureMode verifies that TIMED_OUT workflows
// are also considered for failure mode matching.
func TestTimedOutWorkflowMatchesFailureMode(t *testing.T) {
	snap := baseSnapshot()
	snap.WorkflowReceipts = []runtime.WorkflowReceipt{
		{
			WorkflowID:   "wf-002",
			WorkflowType: "metadata_conflict resolution",
			Status:       "TIMED_OUT",
			ErrorMsg:     "timeout waiting for quorum",
		},
	}

	knownFMs := []string{"failure.metadata_conflict"}

	result := snap.Match(nil, knownFMs)
	found := false
	for _, id := range result.MatchedFailureModes {
		if id == "failure.metadata_conflict" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected failure.metadata_conflict in MatchedFailureModes, got: %v", result.MatchedFailureModes)
	}
}

// TestSucceededWorkflowNotMatched verifies that successful workflows are not
// matched to any failure mode.
func TestSucceededWorkflowNotMatched(t *testing.T) {
	snap := baseSnapshot()
	snap.WorkflowReceipts = []runtime.WorkflowReceipt{
		{
			WorkflowID:   "wf-ok",
			WorkflowType: "restart storm recovery",
			Status:       "SUCCEEDED",
		},
	}

	knownFMs := []string{"failure.restart_storm"}

	result := snap.Match(nil, knownFMs)
	if len(result.MatchedFailureModes) != 0 {
		t.Errorf("succeeded workflow should not match failure modes, got: %v", result.MatchedFailureModes)
	}
}

// TestPendingWorkflowNotMatched verifies that PENDING workflows are not matched.
func TestPendingWorkflowNotMatched(t *testing.T) {
	snap := baseSnapshot()
	snap.WorkflowReceipts = []runtime.WorkflowReceipt{
		{
			WorkflowID:   "wf-pending",
			WorkflowType: "restart storm recovery",
			Status:       "PENDING",
		},
	}

	knownFMs := []string{"failure.restart_storm"}

	result := snap.Match(nil, knownFMs)
	if len(result.MatchedFailureModes) != 0 {
		t.Errorf("pending workflow should not match failure modes, got: %v", result.MatchedFailureModes)
	}
}

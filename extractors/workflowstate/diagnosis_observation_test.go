package workflowstate

// diagnosis_observation_test.go — P1-1: pins that the pattern → fm
// edge emitted by diagnoseRuns carries last_observed_at +
// observation_source="workflow". A workflow failure pattern firing
// IS an observation, so the edge must classify as ACTIVE the moment
// it's written.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/graph"
	workflowpb "github.com/globulario/awareness/workflowpb"
)

// writeFailureModesYAML stages a docs/awareness/failure_modes.yaml the
// diagnoseRuns helper can load.
func writeFailureModesYAML(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "failure_modes.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write failure_modes.yaml: %v", err)
	}
	return dir
}

func TestDiagnoseRuns_PatternToFailureModeEdgeIsStamped(t *testing.T) {
	g := newTestGraph(t)
	ctx := context.Background()

	// Seed a failure_modes.yaml that diagnoseRuns can match on. The
	// symptom keywords must overlap the failed run's ErrorMessage so
	// the matcher picks it up.
	dir := t.TempDir()
	writeFailureModesYAML(t, dir, `
failure_modes:
  - id: workflow.retry_storm
    title: workflow retry storm
    severity: warning
    symptoms:
      - "retry storm"
      - "timeout"
    related_invariants:
      - workflow_receipts_required
`)

	// Seed the failure_mode node so the pattern → fm AddEdge has a
	// dst that resolves.
	if err := g.AddNode(ctx, graph.Node{
		ID:   "failure_mode:workflow.retry_storm",
		Type: graph.NodeTypeFailureMode,
		Name: "workflow.retry_storm",
	}); err != nil {
		t.Fatalf("AddNode failure_mode: %v", err)
	}

	captured := time.Date(2026, 5, 13, 16, 0, 0, 0, time.UTC)
	runs := []*workflowpb.WorkflowRun{
		{
			Id:           "run-storm-1",
			WorkflowName: "package.install",
			Status:       workflowpb.RunStatus_RUN_STATUS_FAILED,
			ErrorMessage: "package install hit retry storm after timeout",
			RetryCount:   5,
		},
	}
	res := diagnoseRuns(ctx, g, runs, dir, captured)
	if res.nodesEmitted == 0 {
		t.Fatalf("diagnoseRuns emitted 0 nodes — pattern matching may have failed")
	}

	// Walk every edge in the graph and find the pattern → fm one.
	allEdges, err := g.AllEdges(ctx)
	if err != nil {
		t.Fatalf("AllEdges: %v", err)
	}
	var patternEdge *graph.Edge
	for i := range allEdges {
		e := &allEdges[i]
		if e.Kind == graph.EdgeWorkflowFailureIndicates &&
			e.Dst == "failure_mode:workflow.retry_storm" &&
			// pattern nodes are sources; skip the run→pattern edges (src
			// starts with workflow_run:) and only pick pattern→fm.
			len(e.Src) > 0 && e.Src[:len("workflow_run:")] != "workflow_run:" {
			patternEdge = e
			break
		}
	}
	if patternEdge == nil {
		t.Fatalf("pattern → failure_mode edge missing; edges=%+v", allEdges)
	}
	if !assurance.IsDetectorActive(*patternEdge, captured, assurance.DefaultDetectorActiveWindow) {
		t.Errorf("pattern → fm edge should classify as active immediately after diagnosis; metadata=%+v",
			patternEdge.Metadata)
	}
	if src, _ := patternEdge.Metadata[assurance.DetectorObservationSourceKey].(string); src != "workflow" {
		t.Errorf("observation_source = %q, want workflow", src)
	}
}

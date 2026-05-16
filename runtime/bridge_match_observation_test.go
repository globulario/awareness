package runtime_test

// bridge_match_observation_test.go — P1-1: pins that snapshot-level
// matches_failure_mode edges (from RuntimeBridge.WriteToGraph) carry
// the observation timestamp. A runtime match IS an observation; the
// edge must classify as ACTIVE the moment it's written so the
// failure_mode can climb back to well_covered from wired-only.

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/runtime"
)

// TestWriteToGraph_SnapshotMatchEdgeIsStamped pins that the
// snapshot:X → matches_failure_mode → failure_mode:Y edge carries
// last_observed_at = snap.CapturedAt and observation_source="runtime".
func TestWriteToGraph_SnapshotMatchEdgeIsStamped(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	if err := g.AddNode(ctx, graph.Node{
		ID: "failure_mode:scylla.write_latency_spike", Type: graph.NodeTypeFailureMode,
	}); err != nil {
		t.Fatalf("AddNode failure_mode: %v", err)
	}

	captured := time.Date(2026, 5, 13, 15, 0, 0, 0, time.UTC)
	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:                  "snap-match",
		CapturedAt:          captured,
		MatchedFailureModes: []string{"scylla.write_latency_spike"},
	}
	if err := bridge.WriteToGraph(ctx, snap, g); err != nil {
		t.Fatalf("WriteToGraph: %v", err)
	}

	// Pull the snapshot→fm edge and verify the stamp.
	out, err := g.OutgoingEdges(ctx, "runtime_snapshot:snap-match")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}
	var edge *graph.Edge
	for i := range out {
		if out[i].Kind == graph.EdgeMatchesFailureMode &&
			out[i].Dst == "failure_mode:scylla.write_latency_spike" {
			edge = &out[i]
			break
		}
	}
	if edge == nil {
		t.Fatalf("snapshot→fm matches_failure_mode edge missing; out=%+v", out)
	}
	if !assurance.IsDetectorActive(*edge, captured, assurance.DefaultDetectorActiveWindow) {
		t.Errorf("snapshot-level match edge should classify as active immediately")
	}
	if src, _ := edge.Metadata[assurance.DetectorObservationSourceKey].(string); src != "runtime" {
		t.Errorf("observation_source = %q, want runtime", src)
	}
	if ts, _ := edge.Metadata[assurance.DetectorObservedAtKey].(int64); ts != captured.Unix() {
		// JSON round-trip lands as float64 sometimes — try that too.
		if tf, _ := edge.Metadata[assurance.DetectorObservedAtKey].(float64); int64(tf) != captured.Unix() {
			t.Errorf("last_observed_at metadata = %v, want %d", edge.Metadata[assurance.DetectorObservedAtKey], captured.Unix())
		}
	}
}

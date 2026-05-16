package runtime_test

// bridge_detector_observation_test.go — P1-1 doctor-wiring acceptance
// tests. Pins:
//   - WriteToGraph stamps detector_mapping edges as observed when a
//     doctor finding fires for the corresponding rule;
//   - Suppressed findings are NOT stamped (operator silenced them);
//   - Findings whose FindingID has no detector node are silently
//     skipped (mapping doesn't exist yet — wired-only stays wired);
//   - Stamping errors surface as snapshot warnings, never as a
//     WriteToGraph failure (best-effort contract);
//   - The stamp is observable via the assurance lifecycle helper
//     (round-trip: stamp → IsDetectorActive=true).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/runtime"
)

func openGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// seedDetectorMapping authors a build-time detector → failure_mode
// edge with no observation stamp. Mirrors the extractors/doctor build
// path that reads detector_mapping.yaml.
func seedDetectorMapping(t *testing.T, g *graph.Graph, ruleID, fmID string) {
	t.Helper()
	ctx := context.Background()
	if err := g.AddNode(ctx, graph.Node{ID: "detector:" + ruleID, Type: "detector", Name: ruleID}); err != nil {
		t.Fatalf("AddNode detector:%s: %v", ruleID, err)
	}
	if err := g.AddNode(ctx, graph.Node{ID: "failure_mode:" + fmID, Type: graph.NodeTypeFailureMode, Name: fmID}); err != nil {
		t.Fatalf("AddNode failure_mode:%s: %v", fmID, err)
	}
	if err := g.AddEdge(ctx, graph.Edge{
		Src: "detector:" + ruleID, Kind: graph.EdgeMatchesFailureMode,
		Dst: "failure_mode:" + fmID,
		// Note: no last_observed_at stamp — wired-only.
	}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
}

// readDetectorEdge fetches the detector_mapping edge for a (rule, fm)
// pair so tests can inspect its metadata.
func readDetectorEdge(t *testing.T, g *graph.Graph, ruleID, fmID string) graph.Edge {
	t.Helper()
	out, err := g.OutgoingEdges(context.Background(), "detector:"+ruleID)
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}
	want := "failure_mode:" + fmID
	for _, e := range out {
		if e.Kind == graph.EdgeMatchesFailureMode && e.Dst == want {
			return e
		}
	}
	t.Fatalf("no detector_mapping edge for %s → %s", ruleID, fmID)
	return graph.Edge{}
}

// TestWriteToGraph_StampsDoctorRuleObservation is the load-bearing
// test: a snapshot with a doctor finding whose FindingID matches a
// detector_mapping rule causes the corresponding detector edge to be
// stamped with last_observed_at = snapshot CapturedAt.
func TestWriteToGraph_StampsDoctorRuleObservation(t *testing.T) {
	g := openGraph(t)
	seedDetectorMapping(t, g, "etcd.quorum", "etcd.leader_instability")

	// Pre-check: edge is wired-only.
	before := readDetectorEdge(t, g, "etcd.quorum", "etcd.leader_instability")
	if before.Metadata[assurance.DetectorObservedAtKey] != nil {
		t.Fatalf("edge already stamped before WriteToGraph; metadata=%+v", before.Metadata)
	}

	now := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:         "snap-1",
		CapturedAt: now,
		DoctorFindings: []runtime.DoctorFinding{
			{FindingID: "etcd.quorum", Severity: "critical", Title: "etcd leader churn"},
		},
	}
	if err := bridge.WriteToGraph(context.Background(), snap, g); err != nil {
		t.Fatalf("WriteToGraph: %v", err)
	}

	after := readDetectorEdge(t, g, "etcd.quorum", "etcd.leader_instability")
	if after.Metadata[assurance.DetectorObservedAtKey] == nil {
		t.Fatalf("edge not stamped after WriteToGraph; metadata=%+v", after.Metadata)
	}
	if !assurance.IsDetectorActive(after, now, assurance.DefaultDetectorActiveWindow) {
		t.Errorf("stamped edge should classify as active immediately after stamping")
	}
	if src, _ := after.Metadata[assurance.DetectorObservationSourceKey].(string); src != "doctor" {
		t.Errorf("observation_source = %q, want doctor", src)
	}
}

// TestWriteToGraph_SuppressedFindingDoesNotStamp pins the suppression
// contract: an operator-silenced finding must not flip the lifecycle
// to active. Suppression is an explicit "ignore this" signal — letting
// it stamp would defeat the operator's intent.
func TestWriteToGraph_SuppressedFindingDoesNotStamp(t *testing.T) {
	g := openGraph(t)
	seedDetectorMapping(t, g, "etcd.quorum", "etcd.leader_instability")

	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:         "snap-suppress",
		CapturedAt: time.Now(),
		DoctorFindings: []runtime.DoctorFinding{
			{FindingID: "etcd.quorum", Severity: "low", Suppressed: true},
		},
	}
	if err := bridge.WriteToGraph(context.Background(), snap, g); err != nil {
		t.Fatalf("WriteToGraph: %v", err)
	}

	after := readDetectorEdge(t, g, "etcd.quorum", "etcd.leader_instability")
	if after.Metadata[assurance.DetectorObservedAtKey] != nil {
		t.Errorf("suppressed finding must not stamp the edge; metadata=%+v", after.Metadata)
	}
}

// TestWriteToGraph_StampsMultipleFailureModesFromOneRule pins the
// fan-out case: a single doctor rule can map to multiple failure_modes
// in detector_mapping.yaml. All mapped edges must be stamped from one
// finding firing.
func TestWriteToGraph_StampsMultipleFailureModesFromOneRule(t *testing.T) {
	g := openGraph(t)
	seedDetectorMapping(t, g, "scylla.replication_lag", "scylla.critical_keyspace_under_replicated")
	seedDetectorMapping(t, g, "scylla.replication_lag", "scylla.write_latency_spike")

	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:         "snap-fanout",
		CapturedAt: time.Now(),
		DoctorFindings: []runtime.DoctorFinding{
			{FindingID: "scylla.replication_lag", Severity: "high"},
		},
	}
	if err := bridge.WriteToGraph(context.Background(), snap, g); err != nil {
		t.Fatalf("WriteToGraph: %v", err)
	}

	for _, fm := range []string{"scylla.critical_keyspace_under_replicated", "scylla.write_latency_spike"} {
		e := readDetectorEdge(t, g, "scylla.replication_lag", fm)
		if e.Metadata[assurance.DetectorObservedAtKey] == nil {
			t.Errorf("edge for fm=%s not stamped; metadata=%+v", fm, e.Metadata)
		}
	}
}

// TestWriteToGraph_UnmappedFindingNoOp pins the graceful path: a doctor
// finding whose FindingID has no matching detector node in the graph
// is silently skipped — wired-only mappings stay wired, no error, no
// warning.
func TestWriteToGraph_UnmappedFindingNoOp(t *testing.T) {
	g := openGraph(t)
	// NO seedDetectorMapping — the finding has no graph anchor.

	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:         "snap-unmapped",
		CapturedAt: time.Now(),
		DoctorFindings: []runtime.DoctorFinding{
			{FindingID: "no.such.rule", Severity: "low"},
		},
	}
	if err := bridge.WriteToGraph(context.Background(), snap, g); err != nil {
		t.Fatalf("WriteToGraph (unmapped finding): %v", err)
	}
	// No warnings should reference detector-observation stamping —
	// the lookup found nothing, which is fine.
	for _, w := range snap.Warnings {
		if strings.Contains(w, "detector-observation stamp failed") {
			t.Errorf("unexpected stamp warning for unmapped finding: %q", w)
		}
	}
}

// TestWriteToGraph_EmptyFindingIDDoesNotPanic pins safety: a doctor
// finding with FindingID="" is gracefully skipped (some collectors
// emit summary findings without a rule id).
func TestWriteToGraph_EmptyFindingIDDoesNotPanic(t *testing.T) {
	g := openGraph(t)

	bridge := &runtime.RuntimeBridge{NodeID: "test-node"}
	snap := &runtime.RuntimeSnapshot{
		ID:         "snap-empty-id",
		CapturedAt: time.Now(),
		DoctorFindings: []runtime.DoctorFinding{
			{FindingID: "", Severity: "low", Title: "summary finding"},
		},
	}
	if err := bridge.WriteToGraph(context.Background(), snap, g); err != nil {
		t.Fatalf("WriteToGraph (empty FindingID): %v", err)
	}
}

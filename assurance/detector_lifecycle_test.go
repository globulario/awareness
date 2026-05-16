package assurance_test

// detector_lifecycle_test.go — P1-1 acceptance tests for the
// active-vs-wired detector classification. The doc-level contract:
//
//   - A detector edge with NO last_observed_at stamp is wired only —
//     it must NOT count toward the "has detector" leg of well_covered.
//   - A detector edge with a stamp INSIDE the active window (default
//     30 days) is active — it counts toward the leg.
//   - A detector edge with a stamp OUTSIDE the active window is treated
//     as wired (the rule used to fire but hasn't recently).
//   - When a failure_mode has both wired-only AND active detector
//     edges, the active edges win — the failure_mode is well_covered.
//   - WiredDetectors is reported separately so operators see exactly
//     which mappings still need their first observation.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/graph"
)

// findCoverage returns the per-fm entry for id, or nil.
func findCoverage(r *assurance.CoverageReport, id string) *assurance.FailureModeCoverage {
	for i := range r.PerFailureMode {
		if r.PerFailureMode[i].ID == id {
			return &r.PerFailureMode[i]
		}
	}
	return nil
}

// TestDetectorLifecycle_WiredOnlyDoesNotCountAsActive pins the load-
// bearing rule: a detector edge with no observation stamp counts as
// wired-only, NOT as a "has detector" leg of well_covered.
func TestDetectorLifecycle_WiredOnlyDoesNotCountAsActive(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-wired", "wired-only failure")
	addNode(t, g, "DP-w", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "TEST-w", graph.NodeTypeTest, "test")
	addNode(t, g, "RT-w", graph.NodeTypeRuntimeState, "rt")
	addEdge(t, g, "DP-w", graph.EdgeMitigates, fmNode("FM-wired"))
	addEdge(t, g, "DP-w", graph.EdgeTestedBy, "TEST-w")
	// Detector edge present BUT no RecordDetectorObservation call —
	// the mapping is wired but the rule has never fired.
	addEdge(t, g, "RT-w", graph.EdgeMatchesFailureMode, fmNode("FM-wired"))

	r, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := findCoverage(r, "FM-wired")
	if fmc == nil {
		t.Fatalf("FM-wired not in report; per-fm=%+v", r.PerFailureMode)
	}
	if fmc.Detectors != 1 {
		t.Errorf("total Detectors = %d, want 1 (edge counted once)", fmc.Detectors)
	}
	if fmc.ActiveDetectors != 0 {
		t.Errorf("ActiveDetectors = %d, want 0 (no observation)", fmc.ActiveDetectors)
	}
	if fmc.WiredDetectors != 1 {
		t.Errorf("WiredDetectors = %d, want 1", fmc.WiredDetectors)
	}
	if fmc.Level == assurance.CoverageWellCovered {
		t.Errorf("Level = %s, want partial (wired-only must not be well_covered)", fmc.Level)
	}
	// Reasons should explain WHY the leg was missing.
	var found bool
	for _, reason := range fmc.Reasons {
		if strings.Contains(reason, "wired but never observed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reason mentioning 'wired but never observed'; got %+v", fmc.Reasons)
	}
}

// TestDetectorLifecycle_ActiveDetectorMakesWellCovered pins the happy
// path: a detector edge stamped with a fresh observation IS active and
// the failure_mode classifies as well_covered.
func TestDetectorLifecycle_ActiveDetectorMakesWellCovered(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-active", "active failure")
	addNode(t, g, "DP-a", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "TEST-a", graph.NodeTypeTest, "test")
	addNode(t, g, "RT-a", graph.NodeTypeRuntimeState, "rt")
	addEdge(t, g, "DP-a", graph.EdgeMitigates, fmNode("FM-active"))
	addEdge(t, g, "DP-a", graph.EdgeTestedBy, "TEST-a")
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"RT-a", "FM-active", "doctor",
		graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}

	r, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := findCoverage(r, "FM-active")
	if fmc == nil {
		t.Fatalf("FM-active not in report")
	}
	if fmc.ActiveDetectors != 1 {
		t.Errorf("ActiveDetectors = %d, want 1", fmc.ActiveDetectors)
	}
	if fmc.WiredDetectors != 0 {
		t.Errorf("WiredDetectors = %d, want 0", fmc.WiredDetectors)
	}
	if fmc.Level != assurance.CoverageWellCovered {
		t.Errorf("Level = %s, want well_covered", fmc.Level)
	}
}

// TestDetectorLifecycle_StaleObservationFallsBackToWired pins the
// 30-day window: an observation older than the window degrades to
// wired-only, just like the no-observation case.
func TestDetectorLifecycle_StaleObservationFallsBackToWired(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-stale", "stale observation")
	addNode(t, g, "DP-s", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "TEST-s", graph.NodeTypeTest, "test")
	addNode(t, g, "RT-s", graph.NodeTypeRuntimeState, "rt")
	addEdge(t, g, "DP-s", graph.EdgeMitigates, fmNode("FM-stale"))
	addEdge(t, g, "DP-s", graph.EdgeTestedBy, "TEST-s")
	// Observation 90 days old — well outside the 30-day window.
	staleObserved := time.Now().Add(-90 * 24 * time.Hour)
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"RT-s", "FM-stale", "doctor",
		graph.EdgeMatchesFailureMode, staleObserved); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}

	r, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := findCoverage(r, "FM-stale")
	if fmc == nil {
		t.Fatalf("FM-stale not in report")
	}
	if fmc.ActiveDetectors != 0 {
		t.Errorf("ActiveDetectors = %d, want 0 (observation is 90 days old, outside the window)",
			fmc.ActiveDetectors)
	}
	if fmc.WiredDetectors != 1 {
		t.Errorf("WiredDetectors = %d, want 1 (stale observation degrades to wired)",
			fmc.WiredDetectors)
	}
	if fmc.Level == assurance.CoverageWellCovered {
		t.Errorf("Level = %s, want partial (stale observation must not count as active)",
			fmc.Level)
	}
}

// TestDetectorLifecycle_MixedActiveAndWiredWinsActive pins that when a
// failure_mode has BOTH a fresh observation AND a wired-only mapping,
// the active edge wins — the failure_mode is well_covered. WiredDetectors
// should still be reported so operators see the unobserved mapping.
func TestDetectorLifecycle_MixedActiveAndWiredWinsActive(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-mixed", "mixed coverage")
	addNode(t, g, "DP-m", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "TEST-m", graph.NodeTypeTest, "test")
	addNode(t, g, "RT-active", graph.NodeTypeRuntimeState, "rt-active")
	addNode(t, g, "RT-wired", graph.NodeTypeRuntimeState, "rt-wired")
	addEdge(t, g, "DP-m", graph.EdgeMitigates, fmNode("FM-mixed"))
	addEdge(t, g, "DP-m", graph.EdgeTestedBy, "TEST-m")
	// Active detector.
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"RT-active", "FM-mixed", "doctor",
		graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}
	// Wired-only detector — separate edge, no observation.
	addEdge(t, g, "RT-wired", graph.EdgeMatchesFailureMode, fmNode("FM-mixed"))

	r, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := findCoverage(r, "FM-mixed")
	if fmc == nil {
		t.Fatalf("FM-mixed not in report")
	}
	if fmc.ActiveDetectors != 1 {
		t.Errorf("ActiveDetectors = %d, want 1", fmc.ActiveDetectors)
	}
	if fmc.WiredDetectors != 1 {
		t.Errorf("WiredDetectors = %d, want 1 (separate edge still surfaces)",
			fmc.WiredDetectors)
	}
	if fmc.Level != assurance.CoverageWellCovered {
		t.Errorf("Level = %s, want well_covered (any active detector wins)", fmc.Level)
	}
}

// TestRecordDetectorObservation_PreservesProvenance pins that the helper
// preserves existing edge metadata when stamping the observation —
// it MUST NOT clobber an unrelated metadata field the extractor wrote.
func TestRecordDetectorObservation_PreservesProvenance(t *testing.T) {
	ctx := context.Background()
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-prov", "provenance preservation")
	addNode(t, g, "RT-p", graph.NodeTypeRuntimeState, "rt")
	// Author the detector edge with a pre-existing metadata field
	// (simulating an extractor that records the YAML source path).
	if err := g.AddEdge(ctx, graph.Edge{
		Src:  "RT-p",
		Kind: graph.EdgeMatchesFailureMode,
		Dst:  fmNode("FM-prov"),
		Metadata: map[string]any{
			"reason":      "scylla retry rate spike",
			"source_yaml": "detector_mapping.yaml",
		},
	}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	// Now stamp the observation.
	if err := assurance.RecordDetectorObservation(ctx, g,
		"RT-p", "FM-prov", "doctor",
		graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}
	// Verify the original metadata survives + the new keys landed.
	out, err := g.OutgoingEdges(ctx, "RT-p")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}
	var found *graph.Edge
	for i := range out {
		if out[i].Kind == graph.EdgeMatchesFailureMode {
			found = &out[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("detector edge missing")
	}
	if v, _ := found.Metadata["reason"].(string); v != "scylla retry rate spike" {
		t.Errorf("original reason metadata clobbered: got %q", v)
	}
	if v, _ := found.Metadata["source_yaml"].(string); v != "detector_mapping.yaml" {
		t.Errorf("original source_yaml metadata clobbered: got %q", v)
	}
	if found.Metadata[assurance.DetectorObservedAtKey] == nil {
		t.Errorf("last_observed_at not stamped")
	}
	if found.Metadata[assurance.DetectorObservationSourceKey] != "doctor" {
		t.Errorf("observation_source = %v, want doctor", found.Metadata[assurance.DetectorObservationSourceKey])
	}
}

// TestRecordDetectorObservation_RejectsMissingArgs pins the validation
// contract: nil graph or empty ids return an error rather than silently
// no-oping (which would hide bugs in the doctor wiring).
func TestRecordDetectorObservation_RejectsMissingArgs(t *testing.T) {
	g := openSeededGraph(t)
	now := time.Now()
	if err := assurance.RecordDetectorObservation(context.Background(), nil,
		"d", "fm", "doctor", graph.EdgeMatchesFailureMode, now); err == nil {
		t.Error("expected error for nil graph")
	}
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"", "fm", "doctor", graph.EdgeMatchesFailureMode, now); err == nil {
		t.Error("expected error for empty detectorNodeID")
	}
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"d", "", "doctor", graph.EdgeMatchesFailureMode, now); err == nil {
		t.Error("expected error for empty failureModeID")
	}
}

// TestIsDetectorActive_HandlesAllMetadataValueShapes pins the value-
// coercion paths: int64, int, float64 (JSON round-trip), and
// string-encoded all parse correctly. Otherwise the observation written
// by one extractor (int64) could fail to be read by another after a
// JSON round-trip through the bundle (float64).
func TestIsDetectorActive_HandlesAllMetadataValueShapes(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-1 * time.Hour).Unix()
	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"int64", int64(fresh), true},
		{"int", int(fresh), true},
		{"float64", float64(fresh), true},
		{"string", "100000000", false}, // bogus old timestamp — outside window
		{"absent", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta := map[string]any{}
			if c.val != nil {
				meta[assurance.DetectorObservedAtKey] = c.val
			}
			edge := graph.Edge{Metadata: meta}
			got := assurance.IsDetectorActive(edge, now, assurance.DefaultDetectorActiveWindow)
			if got != c.want {
				t.Errorf("IsDetectorActive(%v) = %v, want %v", c.val, got, c.want)
			}
		})
	}
}

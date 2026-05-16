package assurance

// detector_lifecycle.go — P1-1: distinguishes "wired" detector edges
// (mapping exists in detector_mapping.yaml but the rule has never fired)
// from "active" detector edges (the doctor / metric / workflow collector
// has observed a finding within the active window).
//
// Why this matters: without lifecycle tracking, a failure_mode counts as
// `well_covered` the moment someone authors `detector_mapping.yaml`,
// even if the detector itself never runs in production. That overstates
// real coverage — it conflates "we wrote a mapping" with "we observe
// this failure". P1-1's contract is that only ACTIVE detectors count
// toward the "has detector" leg of the well_covered classification;
// WIRED-only detectors surface in the report as a distinct count so
// operators see exactly which mappings still need their first
// observation.
//
// Wiring side: a future doctor / metric / workflow extractor calls
// RecordDetectorObservation when a finding fires for a mapped
// failure_mode. The edge metadata gets stamped with last_observed_at +
// observation_source. The classifier reads those keys on each
// ComputeCoverage call — no separate state store.

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
)

// Edge metadata keys for detector lifecycle.
const (
	// DetectorObservedAtKey is the unix timestamp of the most recent
	// observation. Stored on the detector edge's metadata_json. Absent
	// (or zero) means the detector has never fired — the edge is wired
	// but not active.
	DetectorObservedAtKey = "last_observed_at"

	// DetectorObservationSourceKey identifies which collector recorded
	// the observation: "doctor" | "metrics" | "workflow" | "manual".
	// Useful for the operator-facing report; not consulted by the
	// classifier itself.
	DetectorObservationSourceKey = "observation_source"
)

// DefaultDetectorActiveWindow is how recent a detector observation must
// be before the edge is treated as "active" for coverage purposes.
// 30 days is the doc's suggested default — generous enough that a
// quarterly incident still counts, tight enough that a detector that
// hasn't fired in a year is treated as wired-only.
const DefaultDetectorActiveWindow = 30 * 24 * time.Hour

// IsDetectorActive returns true when the given detector edge has a
// last_observed_at within the active window from now. Edges with no
// observation timestamp are treated as WIRED, not active.
func IsDetectorActive(e graph.Edge, now time.Time, window time.Duration) bool {
	if window <= 0 {
		window = DefaultDetectorActiveWindow
	}
	ts := readObservedAtUnix(e.Metadata)
	if ts == 0 {
		return false
	}
	observed := time.Unix(ts, 0)
	return now.Sub(observed) <= window
}

// readObservedAtUnix accepts both int64 (the stable form on disk after
// JSON round-trip via float64) and any value the YAML decoder produces.
// Returns 0 when the field is absent or unparseable.
func readObservedAtUnix(meta map[string]any) int64 {
	if meta == nil {
		return 0
	}
	v, ok := meta[DetectorObservedAtKey]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		// JSON marshal/unmarshal of int64 lands here.
		return int64(n)
	case string:
		// Tolerate string-encoded unix seconds for hand-edited fixtures.
		var sec int64
		if _, err := fmt.Sscanf(n, "%d", &sec); err == nil {
			return sec
		}
	}
	return 0
}

// RecordDetectorObservation stamps a detector edge with the current
// observation timestamp + source label. Callers (cluster-doctor,
// metric-alert collectors, workflow integrity finding emitters) invoke
// this when they see a finding fire for a mapped failure_mode. The edge
// is upserted in place — existing metadata is preserved aside from the
// two lifecycle keys.
//
// detectorNodeID is the prefixed source id (e.g. "detector:etcd.quorum").
// failureModeID is the bare failure_mode id (no "failure_mode:" prefix);
// the helper handles the prefix conversion via graph.FailureModeNodeID.
// source is one of "doctor", "metrics", "workflow", "manual".
//
// edgeKind defaults to graph.EdgeMatchesFailureMode when "". Callers
// observing metric / workflow detectors should pass the correct edge
// kind explicitly so the upsert hits the right (src, kind, dst, phase)
// tuple.
func RecordDetectorObservation(ctx context.Context, g *graph.Graph, detectorNodeID, failureModeID, source, edgeKind string, observedAt time.Time) error {
	if g == nil {
		return fmt.Errorf("RecordDetectorObservation: nil graph")
	}
	if detectorNodeID == "" || failureModeID == "" {
		return fmt.Errorf("RecordDetectorObservation: detectorNodeID and failureModeID are required")
	}
	if edgeKind == "" {
		edgeKind = graph.EdgeMatchesFailureMode
	}
	dst := graph.FailureModeNodeID(failureModeID)

	// Load the existing edge so we preserve any provenance / extra
	// metadata the extractor wrote. Falls back to a fresh Edge if the
	// detector mapping wasn't extracted yet (the observation itself
	// creates the wiring).
	var base graph.Edge
	out, err := g.OutgoingEdges(ctx, detectorNodeID)
	if err == nil {
		for _, oe := range out {
			if oe.Kind == edgeKind && oe.Dst == dst {
				base = oe
				break
			}
		}
	}
	if base.Src == "" {
		base = graph.Edge{Src: detectorNodeID, Kind: edgeKind, Dst: dst}
	}
	if base.Metadata == nil {
		base.Metadata = map[string]any{}
	}
	base.Metadata[DetectorObservedAtKey] = observedAt.Unix()
	if source != "" {
		base.Metadata[DetectorObservationSourceKey] = source
	}
	return g.AddEdge(ctx, base)
}

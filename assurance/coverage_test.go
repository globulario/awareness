package assurance_test

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/assurance"
)

func openSeededGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// addFailureMode mirrors what the manual extractor does on a real build:
// inserts a row into the failure_modes table AND creates a graph node with
// the canonical "failure_mode:" prefix. Tests must use fmNode(id) when
// constructing edge destinations so the wiring matches production.
func addFailureMode(t *testing.T, g *graph.Graph, id, title string) {
	t.Helper()
	if err := g.UpsertFailureMode(context.Background(), graph.FailureMode{ID: id, Title: title}); err != nil {
		t.Fatalf("UpsertFailureMode %s: %v", id, err)
	}
	if err := g.AddNode(context.Background(), graph.Node{
		ID:   fmNode(id),
		Type: graph.NodeTypeFailureMode,
		Name: id,
	}); err != nil {
		t.Fatalf("AddNode %s: %v", fmNode(id), err)
	}
}

// fmNode returns the graph node id for a failure_mode, matching the
// manual extractor's prefix convention.
func fmNode(id string) string { return "failure_mode:" + id }

func addNode(t *testing.T, g *graph.Graph, id, ntype, name string) {
	t.Helper()
	err := g.AddNode(context.Background(), graph.Node{ID: id, Type: ntype, Name: name})
	if err != nil {
		t.Fatalf("AddNode %s: %v", id, err)
	}
}

func addEdge(t *testing.T, g *graph.Graph, src, kind, dst string) {
	t.Helper()
	err := g.AddEdge(context.Background(), graph.Edge{Src: src, Kind: kind, Dst: dst})
	if err != nil {
		t.Fatalf("AddEdge %s -%s-> %s: %v", src, kind, dst, err)
	}
}

// TestComputeCoverage_OrphanFailureMode: a failure_mode with zero inbound
// edges is classified as orphan, never as well_covered or partial.
func TestComputeCoverage_OrphanFailureMode(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-orphan", "Lonely failure")

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.FailureModesTotal != 1 {
		t.Fatalf("FailureModesTotal=%d, want 1", report.FailureModesTotal)
	}
	if report.OrphanCount != 1 {
		t.Errorf("OrphanCount=%d, want 1", report.OrphanCount)
	}
	if got := report.PerFailureMode[0].Level; got != assurance.CoverageOrphan {
		t.Errorf("orphan FM level=%s, want orphan", got)
	}
	if len(report.PerFailureMode[0].Reasons) == 0 {
		t.Error("expected at least one reason for orphan classification")
	}
}

// TestComputeCoverage_WellCovered: a failure_mode with mitigation + test +
// detector should classify as well_covered.
func TestComputeCoverage_WellCovered(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-good", "Well covered failure")
	addNode(t, g, "DP-1", graph.NodeTypeDesignPattern, "design pattern")
	addNode(t, g, "TEST-1", graph.NodeTypeTest, "test_case")
	addNode(t, g, "RT-1", graph.NodeTypeRuntimeState, "runtime observation")

	addEdge(t, g, "DP-1", graph.EdgeMitigates, fmNode("FM-good"))
	addEdge(t, g, "DP-1", graph.EdgeTestedBy, "TEST-1")
	// P1-1: stamp the detector edge as recently observed so it counts
	// toward the "has detector" leg. A wired-only detector mapping
	// (no observation) demotes the failure_mode to partial — see
	// TestComputeCoverage_WiredDetectorOnlyIsPartial.
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"RT-1", "FM-good", "doctor",
		graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.WellCoveredCount != 1 {
		t.Fatalf("WellCoveredCount=%d, want 1; per-fm=%+v", report.WellCoveredCount, report.PerFailureMode)
	}
	fmc := report.PerFailureMode[0]
	if fmc.Mitigations != 1 || fmc.Tests != 1 || fmc.Detectors != 1 {
		t.Errorf("counts wrong: mitigations=%d tests=%d detectors=%d",
			fmc.Mitigations, fmc.Tests, fmc.Detectors)
	}
}

// TestComputeCoverage_TheoreticalOnly: only learning entries / decision paths
// — no enforcement — must be classified as theoretical.
func TestComputeCoverage_TheoreticalOnly(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-theoretical", "Documented but unguarded")
	addNode(t, g, "FF-1", graph.NodeTypeForbiddenFix, "forbidden fix")

	addEdge(t, g, "FF-1", graph.EdgeForbids, fmNode("FM-theoretical"))

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.TheoreticalCount != 1 {
		t.Fatalf("TheoreticalCount=%d, want 1; per-fm=%+v", report.TheoreticalCount, report.PerFailureMode)
	}
	if report.PerFailureMode[0].Level != assurance.CoverageTheoretical {
		t.Errorf("level=%s, want theoretical", report.PerFailureMode[0].Level)
	}
}

// TestComputeCoverage_PartialMissingDetector: mitigation + test but no
// detector → partial, with a reason naming the missing leg.
func TestComputeCoverage_PartialMissingDetector(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-partial", "Partial coverage")
	addNode(t, g, "DP-2", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "TEST-2", graph.NodeTypeTest, "t")

	addEdge(t, g, "DP-2", graph.EdgeMitigates, fmNode("FM-partial"))
	addEdge(t, g, "DP-2", graph.EdgeTestedBy, "TEST-2")

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.PartialCount != 1 {
		t.Fatalf("PartialCount=%d, want 1", report.PartialCount)
	}
	fmc := report.PerFailureMode[0]
	if fmc.Level != assurance.CoveragePartial {
		t.Errorf("level=%s, want partial", fmc.Level)
	}
	foundDetectorReason := false
	for _, r := range fmc.Reasons {
		if r != "" && containsAny(r, []string{"detector"}) {
			foundDetectorReason = true
		}
	}
	if !foundDetectorReason {
		t.Errorf("expected a reason mentioning 'detector', got %v", fmc.Reasons)
	}
}

// TestComputeCoverage_DetectorOnly_OnUnseededFM: a detector edge pointing at a
// failure_mode that was never seeded should still be reported, so we don't
// hide signals from runtime that the YAML does not yet describe.
func TestComputeCoverage_DetectorOnly_OnUnseededFM(t *testing.T) {
	g := openSeededGraph(t)
	// No seeded failure_mode — only a runtime detector mentions FM-runtime.
	addNode(t, g, "RT-2", graph.NodeTypeRuntimeState, "rt")
	// Synthesised failure_mode (no UpsertFailureMode call). The detector edge
	// uses the canonical prefix so coverage's lazy-register path lands on the
	// same key it would for a seeded mode.
	addEdge(t, g, "RT-2", graph.EdgeMatchesFailureMode, fmNode("FM-runtime"))

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.FailureModesTotal != 1 {
		t.Fatalf("FailureModesTotal=%d, want 1 (synthesised from detector)", report.FailureModesTotal)
	}
	if report.PerFailureMode[0].ID != "FM-runtime" {
		t.Errorf("got id=%s, want FM-runtime", report.PerFailureMode[0].ID)
	}
}

// TestComputeCoverage_AggregatePercents: sanity-check the aggregate metrics.
func TestComputeCoverage_AggregatePercents(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "fm-1", "1")
	addFailureMode(t, g, "fm-2", "2")
	addFailureMode(t, g, "fm-3", "3")
	addFailureMode(t, g, "fm-4", "4")

	addNode(t, g, "dp-1", graph.NodeTypeDesignPattern, "dp")
	addNode(t, g, "test-1", graph.NodeTypeTest, "t")
	addNode(t, g, "rt-1", graph.NodeTypeRuntimeState, "rt")
	addEdge(t, g, "dp-1", graph.EdgeMitigates, fmNode("fm-1"))
	addEdge(t, g, "dp-1", graph.EdgeTestedBy, "test-1")
	// P1-1: well_covered requires an ACTIVE detector observation.
	if err := assurance.RecordDetectorObservation(context.Background(), g,
		"rt-1", "fm-1", "doctor",
		graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}

	addNode(t, g, "dp-2", graph.NodeTypeDesignPattern, "dp2")
	addEdge(t, g, "dp-2", graph.EdgeMitigates, fmNode("fm-2"))

	addNode(t, g, "ff-1", graph.NodeTypeForbiddenFix, "ff")
	addEdge(t, g, "ff-1", graph.EdgeForbids, fmNode("fm-3"))
	// fm-4 is orphan.

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if report.WellCoveredCount != 1 || report.PartialCount != 1 ||
		report.TheoreticalCount != 1 || report.OrphanCount != 1 {
		t.Errorf("counts wrong: well=%d partial=%d theoretical=%d orphan=%d (per-fm=%+v)",
			report.WellCoveredCount, report.PartialCount, report.TheoreticalCount,
			report.OrphanCount, report.PerFailureMode)
	}
	if report.WellCoveredPercent != 25.0 {
		t.Errorf("WellCoveredPercent=%.2f, want 25.0", report.WellCoveredPercent)
	}
	if report.CoveragePercent != 50.0 {
		t.Errorf("CoveragePercent=%.2f, want 50.0", report.CoveragePercent)
	}
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if n != "" && stringContains(s, n) {
			return true
		}
	}
	return false
}

// avoid pulling strings just for this single use.
func stringContains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestCoverageFor_LookupByBareAndPrefixedID exercises both calling forms a
// caller might use: the canonical un-prefixed failure_mode id (from a YAML
// row or a preflight match list) and the prefixed graph node id (from an
// edge endpoint). Both must resolve to the same entry.
func TestCoverageFor_LookupByBareAndPrefixedID(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-lookup", "Lookup test failure")

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	bare := report.CoverageFor("FM-lookup")
	if bare == nil {
		t.Fatal("CoverageFor(bare id) returned nil")
	}
	prefixed := report.CoverageFor("failure_mode:FM-lookup")
	if prefixed == nil {
		t.Fatal("CoverageFor(prefixed id) returned nil")
	}
	if bare != prefixed {
		t.Errorf("bare and prefixed lookups returned different entries: %p vs %p", bare, prefixed)
	}
	if bare.ID != "FM-lookup" {
		t.Errorf("entry.ID = %q, want FM-lookup", bare.ID)
	}
}

// TestCoverageFor_UnknownReturnsNil pins the "ask honestly, get an honest
// nil" contract — preflight relies on this to skip coverage influence when
// the matched failure_mode is not in the graph.
func TestCoverageFor_UnknownReturnsNil(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-known", "Present")

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	if got := report.CoverageFor("FM-not-in-graph"); got != nil {
		t.Errorf("CoverageFor(unknown) = %+v, want nil", got)
	}
	if got := report.CoverageFor(""); got != nil {
		t.Errorf("CoverageFor(empty) = %+v, want nil", got)
	}
}

// TestCoverageFor_NilReceiverSafe protects callers that may handle a nil
// report (e.g. when ComputeCoverage failed and the caller still chose to
// dispatch through the lookup helper).
func TestCoverageFor_NilReceiverSafe(t *testing.T) {
	var cov *assurance.CoverageReport
	if got := cov.CoverageFor("anything"); got != nil {
		t.Errorf("nil receiver CoverageFor = %+v, want nil", got)
	}
}

// TestComputeCoverage_PopulatesClosureLoopProvenance is the P1-4 acceptance
// test on the population side: when an incident_patterns row exists for a
// failure_mode AND a test node has a verifies-edge to it, ComputeCoverage
// surfaces the smallest incident_id and the smallest test name into the
// FailureModeCoverage record so Compose() can carry them through to the
// trust envelope.
func TestComputeCoverage_PopulatesClosureLoopProvenance(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-loop", "Closed loop fm")

	// Two incident_patterns rows — smallest incident_id wins for determinism.
	now := int64(1)
	for _, p := range []struct {
		id, incident string
	}{
		{"PAT-1", "INC-2026-0042"},
		{"PAT-2", "INC-2026-0007"}, // smaller, should win
	} {
		_, err := g.DB().Exec(
			`INSERT INTO incident_patterns
			 (id, incident_id, title, summary, severity, status, failure_mode, root_cause, lesson, created_at, updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			p.id, p.incident, "t", "s", "warning", "active", "FM-loop", "", "", now, now)
		if err != nil {
			t.Fatalf("insert pattern %s: %v", p.id, err)
		}
	}

	// Two tests verifying the failure_mode — smallest by id wins.
	addNode(t, g, "test:TestZebra", graph.NodeTypeTest, "TestZebra")
	addNode(t, g, "test:TestAlpha", graph.NodeTypeTest, "TestAlpha")
	addEdge(t, g, "test:TestZebra", graph.EdgeVerifies, fmNode("FM-loop"))
	addEdge(t, g, "test:TestAlpha", graph.EdgeVerifies, fmNode("FM-loop"))

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := report.CoverageFor("FM-loop")
	if fmc == nil {
		t.Fatal("CoverageFor returned nil for seeded failure_mode")
	}
	if fmc.LearnedFromIncident != "INC-2026-0007" {
		t.Errorf("LearnedFromIncident = %q, want INC-2026-0007 (smallest by id)", fmc.LearnedFromIncident)
	}
	if fmc.FirstVerifyingTest != "TestAlpha" {
		t.Errorf("FirstVerifyingTest = %q, want TestAlpha (smallest by id, prefix stripped)", fmc.FirstVerifyingTest)
	}
	if fmc.LearningEntries != 2 {
		t.Errorf("LearningEntries = %d, want 2", fmc.LearningEntries)
	}
	if fmc.Tests != 2 {
		t.Errorf("Tests = %d, want 2", fmc.Tests)
	}
}

// TestComputeCoverage_ClosureLoopEmptyWhenNoEvidence: a plain orphan
// failure_mode produces empty closure-loop fields so the envelope's
// omitempty JSON tag drops them. No fabrication when no evidence exists.
func TestComputeCoverage_ClosureLoopEmptyWhenNoEvidence(t *testing.T) {
	g := openSeededGraph(t)
	addFailureMode(t, g, "FM-empty", "No loop evidence")

	report, err := assurance.ComputeCoverage(context.Background(), g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}
	fmc := report.CoverageFor("FM-empty")
	if fmc == nil {
		t.Fatal("CoverageFor returned nil for seeded failure_mode")
	}
	if fmc.LearnedFromIncident != "" {
		t.Errorf("LearnedFromIncident = %q, want empty", fmc.LearnedFromIncident)
	}
	if fmc.FirstVerifyingTest != "" {
		t.Errorf("FirstVerifyingTest = %q, want empty", fmc.FirstVerifyingTest)
	}
}

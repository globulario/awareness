// Package assurance is the Awareness Assurance Layer. It measures the awareness system itself: how well each
// known failure_mode is covered by mitigations, tests, detectors, and learning
// entries, and whether the awareness graph + bundle are still trustworthy.
//
// The motivation is that adding more awareness subsystems without measuring
// coverage risks turning the system into a rubber stamp — NO_MATCH stops
// meaning "nothing applies" and starts meaning "we did not check the right
// thing." This package surfaces that risk as data instead of leaving it
// implicit.
package assurance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/globulario/awareness/graph"
)

// The failure_modes TABLE stores ids without the "failure_mode:" prefix; the
// graph stamps every node id with that prefix. Joins between the two must
// route through graph.FailureModeNodeID / graph.FailureModeIDFromNode so the
// prefix-bug class (TestComputeCoverage_RecognisesExtractorWiring; see
// docs/awareness/composed_path_failures.md Graph node id prefix) cannot
// recur. Do not introduce a local alias for the prefix here — the shared
// helper IS the contract.

// CoverageLevel classifies how well a single failure_mode is covered by the
// awareness graph.
type CoverageLevel string

const (
	// CoverageWellCovered: at least one mitigation, one test path, and one
	// detector edge — the failure_mode has prevention, proof, and observation.
	CoverageWellCovered CoverageLevel = "well_covered"
	// CoveragePartial: 1 or 2 of the three legs are present.
	CoveragePartial CoverageLevel = "partial"
	// CoverageTheoretical: documented but no enforcement — neither tests nor
	// detectors point at it. The failure_mode is a noun, not an actor.
	CoverageTheoretical CoverageLevel = "theoretical"
	// CoverageOrphan: zero inbound edges of any meaningful kind.
	CoverageOrphan CoverageLevel = "orphan"
)

// FailureModeCoverage describes the coverage tuple for a single failure_mode.
type FailureModeCoverage struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Severity string `json:"severity,omitempty"`

	// Inbound edge counts by category.
	Mitigations     int `json:"mitigations"`      // design_pattern → mitigates → fm
	Detectors       int `json:"detectors"`        // runtime/metric/workflow → matches/indicates → fm
	ActiveDetectors int `json:"active_detectors"` // P1-1: detectors observed within the active window
	WiredDetectors  int `json:"wired_detectors"`  // P1-1: detector edges that have never fired (mapping exists, no observation)
	Tests           int `json:"tests"`            // mitigation paths that lead to a test
	LearningEntries int `json:"learning_entries"` // incident_patterns + applied proposals naming this fm
	DecisionPaths   int `json:"decision_paths"`   // forbidden_fixes / patterns referencing this fm

	// Closure-loop provenance (P1-4). When the loop has produced a durable
	// artifact for this failure_mode, we surface a single identifier so the
	// trust envelope can explain WHY the verdict is partly load-bearing.
	// Deterministic choice (smallest incident_id / test name) means callers
	// don't see the field flicker across runs as new evidence appears.
	LearnedFromIncident string `json:"learned_from_incident,omitempty"` // incident_patterns.incident_id (smallest by id)
	FirstVerifyingTest  string `json:"first_verifying_test,omitempty"`  // test node id (no "test:" prefix) covering this fm

	Level   CoverageLevel `json:"level"`
	State   string        `json:"coverage_state,omitempty"`
	Reasons []string      `json:"reasons,omitempty"`
}

// CoverageReport is the aggregate per-failure_mode coverage report.
type CoverageReport struct {
	GeneratedAtUnix    int64                 `json:"generated_at_unix"`
	FailureModesTotal  int                   `json:"failure_modes_total"`
	WellCoveredCount   int                   `json:"well_covered_count"`
	PartialCount       int                   `json:"partial_count"`
	TheoreticalCount   int                   `json:"theoretical_count"`
	OrphanCount        int                   `json:"orphan_count"`
	CoveragePercent    float64               `json:"coverage_percent"` // (well + partial) / total
	WellCoveredPercent float64               `json:"well_covered_percent"`
	OrphanIDs          []string              `json:"orphan_ids,omitempty"`
	TheoreticalIDs     []string              `json:"theoretical_ids,omitempty"`
	PerFailureMode     []FailureModeCoverage `json:"per_failure_mode"`

	// byIDOnce / byID memoize the failure-mode lookup map. CoverageFor builds
	// it on first call so callers that only need one or two lookups don't pay
	// for it, but a preflight loop or repeated MCP-call pattern stays O(1)
	// per lookup. Skipped during JSON serialization (unexported).
	byIDOnce sync.Once
	byID     map[string]*FailureModeCoverage
}

// CoverageFor returns the coverage tuple for a single failure_mode by its
// canonical (un-prefixed) id, or nil if no entry exists. Idempotent on the
// id form: passing a graph node id with the "failure_mode:" prefix works too.
// Cheap on repeat calls — the index is built once per CoverageReport.
func (r *CoverageReport) CoverageFor(fmID string) *FailureModeCoverage {
	if r == nil || fmID == "" {
		return nil
	}
	r.byIDOnce.Do(func() {
		r.byID = make(map[string]*FailureModeCoverage, len(r.PerFailureMode))
		for i := range r.PerFailureMode {
			r.byID[r.PerFailureMode[i].ID] = &r.PerFailureMode[i]
		}
	})
	return r.byID[graph.FailureModeIDFromNode(fmID)]
}

// Edge kinds we treat as detectors (runtime / metric / workflow signals that
// point at a failure_mode). Pulled from graph/edges.go on purpose so a future
// rename trips the compiler.
var detectorEdgeKinds = []string{
	graph.EdgeMatchesFailureMode,
	graph.EdgeMetricWarningIndicatesFailureMode,
	graph.EdgeWorkflowFailureIndicates,
	graph.EdgeWorkflowErrorMatchesFailureMode,
}

// ComputeCoverage walks the graph once and returns a coverage tuple for every
// failure_mode in the failure_modes table. The traversal is bounded — we read
// every Edge and bucket it by destination. Reads are O(N+E).
func ComputeCoverage(ctx context.Context, g *graph.Graph) (*CoverageReport, error) {
	return ComputeCoverageAt(ctx, g, time.Now())
}

// ComputeCoverageAt is ComputeCoverage with an injectable clock. Test code
// that wants to assert detector-lifecycle behaviour (active vs wired) at a
// specific point in time passes a fixed `now`; production code always uses
// the wrapper above. The active-window check for detectors is the only
// place the clock is consulted today.
func ComputeCoverageAt(ctx context.Context, g *graph.Graph, now time.Time) (*CoverageReport, error) {
	if g == nil {
		return nil, fmt.Errorf("assurance: nil graph")
	}

	fms, err := g.AllFailureModes(ctx)
	if err != nil {
		return nil, fmt.Errorf("assurance: list failure modes: %w", err)
	}

	// Build the per-failure_mode entry up front so unreferenced failure_modes
	// still appear in the report. The bucket is keyed by the GRAPH NODE ID
	// (prefixed), not the failure_modes-table id (un-prefixed), so edge.Dst
	// lookups match without translation. The display ID (FailureModeCoverage.ID)
	// stays un-prefixed for callers.
	bucket := make(map[string]*FailureModeCoverage, len(fms))
	for _, fm := range fms {
		nodeID := graph.FailureModeNodeID(fm.ID)
		entry := &FailureModeCoverage{ID: fm.ID, Title: fm.Title}
		if n, err := g.FindNode(ctx, nodeID); err == nil && n != nil {
			entry.State = lifecycleHintFromNodeMeta(n.Metadata)
			if sev, _ := n.Metadata["severity"].(string); sev != "" {
				entry.Severity = sev
			}
		}
		bucket[nodeID] = entry
	}

	detectorSet := make(map[string]bool, len(detectorEdgeKinds))
	for _, k := range detectorEdgeKinds {
		detectorSet[k] = true
	}

	allEdges, err := g.AllEdges(ctx)
	if err != nil {
		return nil, fmt.Errorf("assurance: list edges: %w", err)
	}

	// Collect mitigation sources per failure_mode so we can later count tests
	// that mitigate via design_pattern → mitigates → failure_mode and have a
	// tested_by edge from the same design_pattern.
	mitigationSources := make(map[string]map[string]bool, len(fms))
	// Per-failure_mode test-id sets so a test linked via two paths
	// (direct required_tests AND via a design_pattern) is counted once.
	testSets := make(map[string]map[string]bool, len(fms))

	noteTest := func(fmKey, testID string) {
		if _, ok := bucket[fmKey]; !ok {
			return
		}
		if testSets[fmKey] == nil {
			testSets[fmKey] = make(map[string]bool)
		}
		testSets[fmKey][testID] = true
	}

	for i := range allEdges {
		e := &allEdges[i]

		// Outbound from a failure_mode: required_tests stamp a direct
		// failure_mode --tested_by--> test edge in the extractor.
		if entry, ok := bucket[e.Src]; ok {
			switch e.Kind {
			case graph.EdgeTestedBy, graph.EdgeRequiresTest, graph.EdgeValidatedBy:
				_ = entry
				noteTest(e.Src, e.Dst)
			}
		}

		entry, ok := bucket[e.Dst]
		if !ok {
			// Detector edges can land on a failure_mode by ID even when the
			// failure_mode was discovered at runtime and not seeded — register
			// it lazily so coverage still reports it. The bucket key stays the
			// graph node id (prefixed); the display id strips the prefix.
			if detectorSet[e.Kind] {
				entry = &FailureModeCoverage{
					ID:    graph.FailureModeIDFromNode(e.Dst),
					Title: "",
				}
				bucket[e.Dst] = entry
			} else {
				continue
			}
		}

		switch {
		case e.Kind == graph.EdgeMitigates:
			entry.Mitigations++
			if mitigationSources[e.Dst] == nil {
				mitigationSources[e.Dst] = make(map[string]bool)
			}
			mitigationSources[e.Dst][e.Src] = true
		case detectorSet[e.Kind]:
			entry.Detectors++
			// P1-1: split into active vs wired-only. A detector edge
			// without a last_observed_at stamp (or with a stale one)
			// is treated as wired-only — the mapping exists but the
			// rule has never fired in production. RecordDetectorObservation
			// is responsible for stamping the timestamp; the classifier
			// only reads it.
			if IsDetectorActive(*e, now, DefaultDetectorActiveWindow) {
				entry.ActiveDetectors++
			} else {
				entry.WiredDetectors++
			}
		case e.Kind == graph.EdgeForbids,
			e.Kind == graph.EdgeBlocksForbiddenAction,
			e.Kind == graph.EdgeCoversPattern:
			entry.DecisionPaths++
		case e.Kind == graph.EdgeFixedBy,
			e.Kind == graph.EdgeRemediatedBy,
			e.Kind == graph.EdgeRecords,
			e.Kind == graph.EdgeCausedBy:
			// caused_by from incident → failure_mode is real learning evidence;
			// fixed_by from failure_mode → resolution and remediated_by are
			// applied proposals or seed knowledge. They all signal that the
			// loop has produced something durable about this failure_mode.
			entry.LearningEntries++
		case e.Kind == graph.EdgeVerifies, e.Kind == graph.EdgeVerifiedBy:
			// Inbound verifies edge: test --verifies--> failure_mode.
			// The extractor stamps both directions for required_tests; record
			// the test once via the set so the pair doesn't double-count.
			noteTest(e.Dst, e.Src)
		}
	}

	// Second pass: tests that verify any mitigation source.
	// design_pattern --tested_by/verifies--> test counts as a test_for_fm if
	// the design_pattern is a known mitigation source. We merge into the
	// per-fm testSet so a test reachable via multiple paths is still 1.
	for fmID, sources := range mitigationSources {
		if _, ok := bucket[fmID]; !ok {
			continue
		}
		for src := range sources {
			out, err := g.OutgoingEdges(ctx, src)
			if err != nil {
				continue
			}
			for _, oe := range out {
				switch oe.Kind {
				case graph.EdgeTestedBy, graph.EdgeVerifies,
					graph.EdgeVerifiedBy, graph.EdgeValidatedBy,
					graph.EdgeRequiresTest:
					noteTest(fmID, oe.Dst)
				}
			}
		}
	}

	// Resolve the per-fm test sets into Tests counts, and record the
	// lexicographically-smallest test id as the regression-test pointer the
	// envelope surfaces. The "test:" prefix is stripped so callers see the
	// runnable test name (e.g. "TestResumeRequiresReceipt") not the graph
	// node id. Smallest-by-id is a deterministic choice — agents need a
	// stable pointer, not a ranked recommendation.
	for fmKey, set := range testSets {
		entry, ok := bucket[fmKey]
		if !ok {
			continue
		}
		entry.Tests = len(set)
		var first string
		for testID := range set {
			if first == "" || testID < first {
				first = testID
			}
		}
		if first != "" {
			entry.FirstVerifyingTest = strings.TrimPrefix(first, "test:")
		}
	}

	// Also count incident_patterns rows where failure_mode = fm.ID. The schema
	// stores those in a dedicated table outside the edges graph.
	if err := countIncidentPatterns(ctx, g, bucket); err != nil {
		return nil, err
	}

	// Classify each entry.
	report := &CoverageReport{
		FailureModesTotal: len(bucket),
		PerFailureMode:    make([]FailureModeCoverage, 0, len(bucket)),
	}
	for _, fmc := range bucket {
		fmc.Level, fmc.Reasons, fmc.State = classifyCoverage(*fmc)
		switch fmc.Level {
		case CoverageWellCovered:
			report.WellCoveredCount++
		case CoveragePartial:
			report.PartialCount++
		case CoverageTheoretical:
			report.TheoreticalCount++
			report.TheoreticalIDs = append(report.TheoreticalIDs, fmc.ID)
		case CoverageOrphan:
			report.OrphanCount++
			report.OrphanIDs = append(report.OrphanIDs, fmc.ID)
		}
		report.PerFailureMode = append(report.PerFailureMode, *fmc)
	}

	if total := report.FailureModesTotal; total > 0 {
		report.CoveragePercent = 100.0 * float64(report.WellCoveredCount+report.PartialCount) / float64(total)
		report.WellCoveredPercent = 100.0 * float64(report.WellCoveredCount) / float64(total)
	}

	sort.Strings(report.OrphanIDs)
	sort.Strings(report.TheoreticalIDs)
	sort.Slice(report.PerFailureMode, func(i, j int) bool {
		return report.PerFailureMode[i].ID < report.PerFailureMode[j].ID
	})

	return report, nil
}

// classifyCoverage assigns a coverage level using the three-leg rule:
// mitigations + tests + detectors. Zero of all of these → orphan.
func classifyCoverage(fmc FailureModeCoverage) (CoverageLevel, []string, string) {
	if fmc.State == "DEPRECATED" || fmc.State == "INTENTIONAL_GAP" {
		return CoverageTheoretical, []string{"lifecycle state marks this failure_mode as intentionally non-enforced"}, fmc.State
	}
	hasMitigation := fmc.Mitigations > 0
	hasTest := fmc.Tests > 0
	// P1-1: only ACTIVE detectors (observed within the active window)
	// count toward the well_covered leg. A failure_mode with only wired-
	// but-never-fired detector mappings is honest about its coverage
	// gap. WiredDetectors stays surfaced in the report so operators
	// can see "the mapping exists, we just haven't observed it yet."
	hasDetector := fmc.ActiveDetectors > 0
	hasWiredDetector := fmc.WiredDetectors > 0 && fmc.ActiveDetectors == 0

	legs := 0
	for _, leg := range []bool{hasMitigation, hasTest, hasDetector} {
		if leg {
			legs++
		}
	}

	totalEdges := fmc.Mitigations + fmc.Tests + fmc.Detectors + fmc.LearningEntries + fmc.DecisionPaths
	if totalEdges == 0 {
		return CoverageOrphan, []string{
			"no inbound mitigation, detector, learning, or decision_path edges — failure_mode is documented but unreferenced",
		}, "ORPHAN"
	}

	if legs == 3 {
		return CoverageWellCovered, nil, "ENFORCED"
	}
	if legs >= 1 {
		var reasons []string
		if !hasMitigation {
			reasons = append(reasons, "no design_pattern mitigates this failure_mode")
		}
		if !hasTest {
			reasons = append(reasons, "no test reachable through a mitigation source")
		}
		if !hasDetector {
			if hasWiredDetector {
				reasons = append(reasons, fmt.Sprintf(
					"%d detector mapping(s) wired but never observed — flip to active when cluster-doctor records the first finding",
					fmc.WiredDetectors))
			} else {
				reasons = append(reasons, "no runtime/metric/workflow detector edges target this failure_mode")
			}
		}
		if hasTest && hasDetector {
			return CoveragePartial, reasons, "DETECTED"
		}
		if hasTest {
			return CoveragePartial, reasons, "TESTED"
		}
		return CoveragePartial, reasons, "PARTIAL"
	}

	// legs == 0 but totalEdges > 0 — only learning entries or forbidden_fix
	// links, no active enforcement.
	return CoverageTheoretical, []string{
		"failure_mode has decision-path or learning entries but no live enforcement (no mitigation, no test, no detector)",
	}, "PARTIAL"
}

func lifecycleHintFromNodeMeta(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if d, _ := meta["deprecated"].(bool); d {
		return "DEPRECATED"
	}
	if ig, _ := meta["intentional_gap"].(bool); ig {
		return "INTENTIONAL_GAP"
	}
	if s, _ := meta["coverage_state"].(string); s != "" {
		return s
	}
	return ""
}

// countIncidentPatterns adds incident_patterns rows to the LearningEntries
// counter and records the smallest incident_id per failure_mode so the trust
// envelope can name the incident that originally closed the learning loop on
// this failure_mode. This table is queried directly because it is not
// represented as graph edges (it lives next to the failure-graph store).
//
// "Smallest" is a deterministic choice rather than a quality one: it
// stabilizes the envelope output across runs even as new incident_patterns
// rows are added. The agent-facing meaning is "this failure_mode was learned
// from at least one incident" — the specific id is provenance, not a ranking.
func countIncidentPatterns(ctx context.Context, g *graph.Graph, bucket map[string]*FailureModeCoverage) error {
	rows, err := g.DB().QueryContext(ctx,
		`SELECT failure_mode, COUNT(*), MIN(incident_id) FROM incident_patterns
		 WHERE failure_mode != '' AND incident_id != '' GROUP BY failure_mode`)
	if err != nil {
		return fmt.Errorf("assurance: count incident_patterns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fm string
		var count int
		var firstIncident string
		if err := rows.Scan(&fm, &count, &firstIncident); err != nil {
			return err
		}
		// incident_patterns.failure_mode stores the un-prefixed id; translate
		// to the graph node id (prefixed) for the bucket lookup.
		if entry, ok := bucket[graph.FailureModeNodeID(fm)]; ok {
			entry.LearningEntries += count
			if entry.LearnedFromIncident == "" && firstIncident != "" {
				entry.LearnedFromIncident = firstIncident
			}
		}
	}
	return rows.Err()
}

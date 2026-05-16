package assurance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

// TestComputeCoverage_RecognisesExtractorWiring is the regression test for the
// extraction-vs-conceptual orphan distinction. It loads a failure_modes.yaml
// through the same manual extractor the build pipeline uses, then asserts that
// ComputeCoverage classifies the failure_mode as well_covered — NOT orphan.
//
// History: the first version of coverage.go keyed its lookup by the
// failure_modes table's domain id ("foo.bar") while the manual extractor
// stamps edge destinations with the canonical graph node id
// ("failure_mode:foo.bar"). Edge lookup always missed, so 36/38 failure_modes
// in the live graph appeared orphan even though the extractor had wired
// every edge correctly. This test fails if that mismatch returns.
func TestComputeCoverage_RecognisesExtractorWiring(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "failure_modes.yaml")
	yaml := `failure_modes:
  - id: fm.coverage.integration
    title: coverage integration test
    severity: critical
    root_cause: extractor wires edges that coverage must read
    required_tests:
      - TestCoverageIntegration
    mitigates:
      - pattern.coverage_integration_guard
    detectors:
      - detector.coverage_integration
`
	if err := os.WriteFile(yamlPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := manual.LoadFailureModes(ctx, g, yamlPath); err != nil {
		t.Fatalf("LoadFailureModes: %v", err)
	}

	// P1-1: the failure_modes.yaml extractor only wires the detector edge —
	// the rule has never fired yet. Simulate cluster-doctor observing the
	// detector once so the edge is treated as ACTIVE and the failure_mode
	// classifies as well_covered (otherwise it stays partial, which is the
	// honest answer when no observation has occurred).
	if err := assurance.RecordDetectorObservation(ctx, g,
		"detector:detector.coverage_integration", "fm.coverage.integration",
		"doctor", graph.EdgeMatchesFailureMode, time.Now()); err != nil {
		t.Fatalf("RecordDetectorObservation: %v", err)
	}

	report, err := assurance.ComputeCoverage(ctx, g)
	if err != nil {
		t.Fatalf("ComputeCoverage: %v", err)
	}

	if report.OrphanCount > 0 {
		t.Errorf("expected 0 orphans, got %d. orphan_ids=%v",
			report.OrphanCount, report.OrphanIDs)
	}

	var fmc *assurance.FailureModeCoverage
	for i := range report.PerFailureMode {
		if report.PerFailureMode[i].ID == "fm.coverage.integration" {
			fmc = &report.PerFailureMode[i]
			break
		}
	}
	if fmc == nil {
		t.Fatalf("failure_mode fm.coverage.integration not found in report; per-fm=%+v",
			report.PerFailureMode)
	}
	if fmc.Mitigations == 0 {
		t.Errorf("Mitigations=0 — coverage missed the extractor's design_pattern→mitigates→failure_mode edge")
	}
	if fmc.Detectors == 0 {
		t.Errorf("Detectors=0 — coverage missed the detector→matches_failure_mode edge")
	}
	if fmc.Tests == 0 {
		t.Errorf("Tests=0 — coverage missed the test linked through the mitigation source. " +
			"This breaks the well_covered classification.")
	}
	if fmc.Level != assurance.CoverageWellCovered {
		t.Errorf("Level=%s, want well_covered. counts: mitigations=%d tests=%d detectors=%d. "+
			"This is the load-bearing case: extractor wired all three legs but coverage misclassified.",
			fmc.Level, fmc.Mitigations, fmc.Tests, fmc.Detectors)
	}
}

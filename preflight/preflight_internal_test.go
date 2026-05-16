package preflight

import (
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestComputeRiskTier_NilGraph_LocalCodeChangeNoFilesIsMedium ensures that when
// the graph is unavailable, zero file matches on a LocalCodeChange task is
// classified as medium risk — not low. Zero matches with no graph means
// coverage gap, not confirmed low impact.
func TestComputeRiskTier_NilGraph_LocalCodeChangeNoFilesIsMedium(t *testing.T) {
	r := &Report{
		Classification: []TaskClass{ClassLocalCodeChange},
		Files:          []string{},
	}
	got := computeRiskTier(r, nil)
	if got != RiskMedium {
		t.Errorf("computeRiskTier with nil graph and no files: got %q, want %q (coverage gap must not produce RiskLow)", got, RiskMedium)
	}
}

// TestComputeRiskTier_WithGraph_LocalCodeChangeNoFilesIsLow ensures that when
// the graph is available and confirmed no file impact, the task is low risk.
func TestComputeRiskTier_WithGraph_LocalCodeChangeNoFilesIsLow(t *testing.T) {
	g, _ := graph.OpenMemory()
	r := &Report{
		Classification: []TaskClass{ClassLocalCodeChange},
		Files:          []string{},
	}
	got := computeRiskTier(r, g)
	if got != RiskLow {
		t.Errorf("computeRiskTier with live graph and no files: got %q, want %q", got, RiskLow)
	}
}

// TestComputeRiskTier_HighRiskClassOverrides ensures architecture-sensitive
// tasks are always high risk regardless of graph presence.
func TestComputeRiskTier_HighRiskClassOverrides(t *testing.T) {
	g, _ := graph.OpenMemory()
	for _, cls := range []TaskClass{
		ClassArchitectureSensitive,
		ClassConvergenceRisk,
		ClassDependencyCycle,
		ClassPackageAdmission,
		ClassRuntimeIncident,
	} {
		r := &Report{Classification: []TaskClass{cls}}
		got := computeRiskTier(r, g)
		if got != RiskHigh {
			t.Errorf("class %q: got %q, want RiskHigh", cls, got)
		}
		got = computeRiskTier(r, nil)
		if got != RiskHigh {
			t.Errorf("class %q (nil graph): got %q, want RiskHigh", cls, got)
		}
	}
}

// TestApplyLowRiskFastPath_DoesNotMutateReport verifies that fast-path
// activation does not truncate any list on the Report. The flag is a signal
// only; data reduction belongs in the render layer.
func TestApplyLowRiskFastPath_DoesNotMutateReport(t *testing.T) {
	searches := make([]string, 20)
	tests := make([]string, 20)
	fixes := make([]string, 30)
	for i := range searches {
		searches[i] = "search"
	}
	for i := range tests {
		tests[i] = "test"
	}
	for i := range fixes {
		fixes[i] = "fix"
	}

	r := &Report{
		RiskTier:         RiskLow,
		Classification:   []TaskClass{ClassLocalCodeChange},
		RequiredSearches: searches,
		RequiredTests:    tests,
		ForbiddenFixes:   fixes,
		// No invariants/failure_modes/matched_aliases so fast path can fire.
	}

	applied := applyLowRiskFastPath(r)
	if !applied {
		t.Fatal("expected fast path to apply for RiskLow with no architecture facts")
	}
	if len(r.RequiredSearches) != 20 {
		t.Errorf("RequiredSearches was truncated: got %d, want 20", len(r.RequiredSearches))
	}
	if len(r.RequiredTests) != 20 {
		t.Errorf("RequiredTests was truncated: got %d, want 20", len(r.RequiredTests))
	}
	if len(r.ForbiddenFixes) != 30 {
		t.Errorf("ForbiddenFixes was truncated: got %d, want 30", len(r.ForbiddenFixes))
	}
}

// TestApplyLowRiskFastPath_BlockedByInvariants ensures fast path does not fire
// when awareness matched architecture facts.
func TestApplyLowRiskFastPath_BlockedByInvariants(t *testing.T) {
	r := &Report{
		RiskTier:       RiskLow,
		Classification: []TaskClass{ClassLocalCodeChange},
		Invariants:     []string{"some.invariant"},
	}
	if applyLowRiskFastPath(r) {
		t.Error("fast path must not apply when invariants are present")
	}
}

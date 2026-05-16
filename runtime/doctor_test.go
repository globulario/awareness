package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/runtime"
)

// TestDoctorFindingMatchesToInvariantByRef verifies that a finding with an
// InvariantRef is matched to that invariant ID directly.
func TestDoctorFindingMatchesToInvariantByRef(t *testing.T) {
	snap := baseSnapshot()
	snap.DoctorFindings = []runtime.DoctorFinding{
		{
			FindingID:    "f-001",
			Severity:     "high",
			Title:        "something is wrong",
			InvariantRef: "service.restart_singleflight",
		},
	}

	knownInvariants := []string{"service.restart_singleflight", "desired_hash.consistency"}
	result := snap.Match(knownInvariants, nil)

	if len(result.MatchedInvariants) != 1 {
		t.Fatalf("MatchedInvariants count = %d, want 1", len(result.MatchedInvariants))
	}
	if result.MatchedInvariants[0] != "service.restart_singleflight" {
		t.Errorf("MatchedInvariants[0] = %q, want %q",
			result.MatchedInvariants[0], "service.restart_singleflight")
	}
}

// TestDoctorFindingMatchesToInvariantByKeyword verifies that a finding without
// an InvariantRef is keyword-matched against known invariant IDs.
func TestDoctorFindingMatchesToInvariantByKeyword(t *testing.T) {
	snap := baseSnapshot()
	snap.DoctorFindings = []runtime.DoctorFinding{
		{
			FindingID: "f-002",
			Severity:  "medium",
			Title:     "metadata first policy violated in repository",
		},
	}

	knownInvariants := []string{"repository.metadata_first", "service.restart_singleflight"}
	result := snap.Match(knownInvariants, nil)

	found := false
	for _, id := range result.MatchedInvariants {
		if id == "repository.metadata_first" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected repository.metadata_first in MatchedInvariants, got: %v", result.MatchedInvariants)
	}
}

// TestSuppressedFindingNotMatched verifies that suppressed doctor findings
// do not contribute to MatchedInvariants.
func TestSuppressedFindingNotMatched(t *testing.T) {
	snap := baseSnapshot()
	snap.DoctorFindings = []runtime.DoctorFinding{
		{
			FindingID:    "f-003",
			Severity:     "critical",
			Title:        "restart singleflight violated",
			InvariantRef: "service.restart_singleflight",
			Suppressed:   true,
		},
	}

	knownInvariants := []string{"service.restart_singleflight"}
	result := snap.Match(knownInvariants, nil)

	if len(result.MatchedInvariants) != 0 {
		t.Errorf("expected no matched invariants for suppressed finding, got: %v", result.MatchedInvariants)
	}
}

// TestFakeDoctorSourceReturnsData verifies the FakeDoctorSource implementation.
func TestFakeDoctorSourceReturnsData(t *testing.T) {
	src := &runtime.FakeDoctorSource{
		Data: []runtime.DoctorFinding{
			{FindingID: "x", Severity: "low", Title: "low severity finding"},
		},
	}
	findings, err := src.Findings(context.Background())
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(findings))
	}
}

// TestNoopDoctorSourceReturnsEmpty verifies the NoopDoctorSource.
func TestNoopDoctorSourceReturnsEmpty(t *testing.T) {
	b := runtime.NewBridge("", "")
	snap, err := b.Snapshot(context.Background(), 15*time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.DoctorFindings) != 0 {
		t.Errorf("noop bridge: expected 0 doctor findings, got %d", len(snap.DoctorFindings))
	}
}

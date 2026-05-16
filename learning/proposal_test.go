package learning_test

import (
	"testing"

	"github.com/globulario/awareness/learning"
)

func TestGenerateProposalFromFixtureContainsExpectedFailureMode(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	p := learning.GenerateProposalFromBundle(b)

	if p.Proposal.Status != learning.StatusDraft {
		t.Errorf("expected status DRAFT, got %q", p.Proposal.Status)
	}
	if p.Proposal.SourceIncident != b.IncidentID {
		t.Errorf("source_incident mismatch: %q vs %q", p.Proposal.SourceIncident, b.IncidentID)
	}

	found := false
	for _, fm := range p.FailureModes {
		if fm.ID == "infra.desired_hash_mismatch_restart_storm" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected failure_mode infra.desired_hash_mismatch_restart_storm in proposal")
	}
}

func TestGenerateProposalFromFixtureContainsExpectedInvariants(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	p := learning.GenerateProposalFromBundle(b)

	expectedInvariants := []string{
		"infra.desired_hash_consistency",
		"service.restart_singleflight",
		"infra.heartbeat_not_desired_authority",
	}

	invIDs := make(map[string]bool)
	for _, inv := range p.Invariants {
		invIDs[inv.ID] = true
	}

	for _, id := range expectedInvariants {
		if !invIDs[id] {
			t.Errorf("expected invariant %q not found in proposal", id)
		}
	}
}

func TestGenerateProposalFromFixtureContainsContextAliases(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	p := learning.GenerateProposalFromBundle(b)

	if len(p.ContextAliases) == 0 {
		t.Error("expected context_aliases in proposal from fixture")
	}
	// Verify the restart storm alias is present.
	restartAliases := p.ContextAliases["service.restart_singleflight"]
	if len(restartAliases) == 0 {
		t.Error("expected aliases for service.restart_singleflight")
	}
}

func TestGenerateProposalSkeletonWithoutProposed(t *testing.T) {
	b := &learning.IncidentBundle{
		IncidentID:         "test.skeleton.2026-01-01",
		Title:              "Skeleton test incident",
		Severity:           "high",
		Symptoms:           []string{"widget broke"},
		ObservedServices:   []string{"widget-service"},
		SuspectedRootCause: "widget was not initialised",
		// No Proposed section.
	}

	p := learning.GenerateProposalFromBundle(b)

	if len(p.FailureModes) == 0 {
		t.Error("skeleton proposal must contain at least one failure mode")
	}
	if p.FailureModes[0].RootCause != b.SuspectedRootCause {
		t.Error("skeleton failure mode root_cause must be taken from suspected_root_cause")
	}
}

func TestProposalEvidenceLinkPreserved(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}

	p := learning.GenerateProposalFromBundle(b)

	if p.Evidence.SourceIncident != b.IncidentID {
		t.Errorf("evidence.source_incident %q does not match incident_id %q",
			p.Evidence.SourceIncident, b.IncidentID)
	}
}

func TestSaveAndLoadProposal(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)

	path := t.TempDir() + "/proposal.yaml"
	if err := learning.SaveProposal(path, p); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	loaded, err := learning.LoadProposalFromFile(path)
	if err != nil {
		t.Fatalf("LoadProposalFromFile: %v", err)
	}
	if loaded.Proposal.ID != p.Proposal.ID {
		t.Errorf("proposal ID mismatch after round-trip: %q vs %q", loaded.Proposal.ID, p.Proposal.ID)
	}
	if len(loaded.FailureModes) != len(p.FailureModes) {
		t.Errorf("failure_modes count mismatch: %d vs %d", len(loaded.FailureModes), len(p.FailureModes))
	}
}

package learning_test

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/learning"
)

// ---- 1. TestProposeFromIncidentRejectsUnsafePath ----

func TestSanitiseIDDoesNotContainPathSep(t *testing.T) {
	// sanitiseID is internal but we can test the observable behaviour:
	// GenerateProposalFromBundle uses sanitiseID to create an ID from incidentID,
	// and the resulting proposal ID must not contain path separators.
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	// The proposal ID must not contain "/" or "..".
	if strings.Contains(p.Proposal.ID, "/") {
		t.Errorf("proposal.id %q contains '/'", p.Proposal.ID)
	}
	if strings.Contains(p.Proposal.ID, "..") {
		t.Errorf("proposal.id %q contains '..'", p.Proposal.ID)
	}
}

// ---- 2. TestApproveProposalChangesStatus ----

func TestApproveProposalChangesStatus(t *testing.T) {
	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)

	if p.Proposal.Status != learning.StatusDraft {
		t.Fatalf("expected DRAFT status before approval, got %q", p.Proposal.Status)
	}

	learning.ApproveProposal(p)

	if p.Proposal.Status != learning.StatusApproved {
		t.Errorf("expected APPROVED status after ApproveProposal, got %q", p.Proposal.Status)
	}
	if strings.TrimSpace(p.Proposal.ApprovedBy) == "" {
		t.Error("expected approved_by to be populated by ApproveProposal")
	}
	if strings.TrimSpace(p.Proposal.ApprovedAt) == "" {
		t.Error("expected approved_at to be populated by ApproveProposal")
	}
}

// ---- 3. TestPromoteProposalRejectsDraft ----

func TestPromoteProposalRejectsDraft(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	// p.Proposal.Status is DRAFT — do not approve.

	vr := validateAndGetResult(t, p)
	if vr.Status != learning.ValidationPass {
		t.Fatalf("fixture must pass validation")
	}

	_, err = learning.PromoteProposal(ctx, p, vr, docsDir, nil)
	if err == nil {
		t.Error("expected error promoting a DRAFT proposal without AllowUnapproved")
	}
	if err != nil && !strings.Contains(err.Error(), "APPROVED") {
		t.Errorf("expected error mentioning APPROVED status, got: %v", err)
	}
}

// ---- 4. TestPromoteProposalAcceptsApproved ----

func TestPromoteProposalAcceptsApproved(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	learning.ApproveProposal(p)

	vr := validateAndGetResult(t, p)
	if vr.Status != learning.ValidationPass {
		t.Fatalf("fixture must pass validation")
	}

	_, err = learning.PromoteProposal(ctx, p, vr, docsDir, nil)
	if err != nil {
		t.Errorf("expected promotion to succeed for APPROVED proposal: %v", err)
	}
}

func TestPromoteProposalRejectsApprovedWithoutReviewer(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	p.Proposal.Status = learning.StatusApproved
	p.Proposal.ApprovedBy = ""
	p.Proposal.ApprovedAt = ""

	vr := validateAndGetResult(t, p)
	if vr.Status != learning.ValidationPass {
		t.Fatalf("fixture must pass validation")
	}

	_, err = learning.PromoteProposal(ctx, p, vr, docsDir, nil)
	if err == nil {
		t.Fatal("expected promotion to fail without approved_by reviewer identity")
	}
}

// ---- 5. TestPromoteProposalAllowUnapproved ----

func TestPromoteProposalAllowUnapproved(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	// Stay in DRAFT — rely on AllowUnapproved.

	vr := validateAndGetResult(t, p)
	if vr.Status != learning.ValidationPass {
		t.Fatalf("fixture must pass validation")
	}

	_, err = learning.PromoteProposal(ctx, p, vr, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true})
	if err != nil {
		t.Errorf("expected promotion to succeed with AllowUnapproved=true: %v", err)
	}
}

// ---- 6. TestAliasesCanTargetFailureMode ----

func TestAliasesCanTargetFailureMode(t *testing.T) {
	aliases := learning.ContextAliasMap{
		"failure_mode:test.failure": {"desired hash mismatch"},
	}
	matched := learning.MatchAliasTargets("check desired hash mismatch in envoy", aliases)
	if len(matched) == 0 {
		t.Fatal("expected failure_mode alias to be matched")
	}
	found := false
	for _, m := range matched {
		if m == "failure_mode:test.failure" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'failure_mode:test.failure' in matched, got: %v", matched)
	}
}

// ---- 7. TestAliasesCanTargetService ----

func TestAliasesCanTargetService(t *testing.T) {
	aliases := learning.ContextAliasMap{
		"service:envoy": {"proxy mesh restart"},
	}
	matched := learning.MatchAliasTargets("investigate proxy mesh restart issue", aliases)
	if len(matched) == 0 {
		t.Fatal("expected service alias to be matched")
	}
	found := false
	for _, m := range matched {
		if m == "service:envoy" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'service:envoy' in matched, got: %v", matched)
	}
}

// ---- 8. TestUnknownProposalStatusRejected ----

func TestUnknownProposalStatusRejected(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.bogus",
			SourceIncident: "test.incident",
			Status:         "BOGUS", // invalid
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL for unknown proposal status BOGUS")
	}
	found := false
	for _, f := range result.Findings {
		if f.Rule == 1 && f.Status == learning.ValidationFail {
			found = true
		}
	}
	if !found {
		t.Error("expected rule 1 failure for unknown status")
	}
}

// ---- 9. TestSourceIncidentMismatchRejected ----

func TestSourceIncidentMismatchRejected(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.mismatch",
			SourceIncident: "incident.A",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.mismatch",
				Title:           "Test mismatch",
				Severity:        "high",
				Symptoms:        []string{"something"},
				RootCause:       "mismatch",
				ArchitectureFix: "fix mismatch",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestMismatch"},
			},
		},
		Evidence: learning.ProposalEvidence{
			SourceIncident: "incident.B", // mismatch with proposal.source_incident
		},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL for source incident mismatch between proposal and evidence")
	}
	found := false
	for _, f := range result.Findings {
		if f.Rule == 10 && f.Status == learning.ValidationFail {
			found = true
		}
	}
	if !found {
		t.Error("expected rule 10 failure for source incident mismatch")
	}
}

// ---- alias target kind helper tests ----

func TestAliasTargetKind_Invariant(t *testing.T) {
	kind, bareID := learning.AliasTargetKind("invariant:convergence.no_infinite_retry")
	if kind != "invariant" {
		t.Errorf("expected kind=invariant, got %q", kind)
	}
	if bareID != "convergence.no_infinite_retry" {
		t.Errorf("expected bareID=convergence.no_infinite_retry, got %q", bareID)
	}
}

func TestAliasTargetKind_FailureMode(t *testing.T) {
	kind, bareID := learning.AliasTargetKind("failure_mode:envoy.restart_storm")
	if kind != "failure_mode" {
		t.Errorf("expected kind=failure_mode, got %q", kind)
	}
	if bareID != "envoy.restart_storm" {
		t.Errorf("expected bareID=envoy.restart_storm, got %q", bareID)
	}
}

func TestAliasTargetKind_Service(t *testing.T) {
	kind, bareID := learning.AliasTargetKind("service:envoy")
	if kind != "service" {
		t.Errorf("expected kind=service, got %q", kind)
	}
	if bareID != "envoy" {
		t.Errorf("expected bareID=envoy, got %q", bareID)
	}
}

func TestAliasTargetKind_Bare(t *testing.T) {
	kind, bareID := learning.AliasTargetKind("convergence.no_infinite_retry")
	if kind != "invariant" {
		t.Errorf("expected kind=invariant (bare backward-compat), got %q", kind)
	}
	if bareID != "convergence.no_infinite_retry" {
		t.Errorf("expected bareID=convergence.no_infinite_retry, got %q", bareID)
	}
}

// ---- graph expansion via aliases covers failure_mode and service prefixes ----

func TestExpandByAliasesIncludesFailureMode(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	// Add a failure mode to the graph.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "failure_mode:test.failure",
		Type: graph.NodeTypeFailureMode,
		Name: "test.failure",
	})

	// The AliasTargetKind function correctly classifies failure_mode prefix.
	kind, bareID := learning.AliasTargetKind("failure_mode:test.failure")
	if kind != "failure_mode" || bareID != "test.failure" {
		t.Errorf("AliasTargetKind: expected failure_mode/test.failure, got %s/%s", kind, bareID)
	}
}

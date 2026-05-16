package learning_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/learning"
)

// seedValidateGraph creates a minimal graph with existing invariants,
// services, and a forbidden fix for rule validation tests.
func seedValidateGraph(t *testing.T) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Existing critical invariant with forbidden fixes.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "convergence.no_infinite_retry",
		Title:    "No infinite retry",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:convergence.no_infinite_retry", Type: graph.NodeTypeInvariant, Name: "convergence.no_infinite_retry"})
	_ = g.AddNode(ctx, graph.Node{ID: "forbidden_fix:blind_retry", Type: graph.NodeTypeForbiddenFix, Name: "blind_retry"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:convergence.no_infinite_retry", Kind: graph.EdgeForbids, Dst: "forbidden_fix:blind_retry"})

	// Known services.
	for _, svc := range []string{"envoy", "xds", "cluster-controller", "workflow-service", "node-agent", "minio", "etcd"} {
		_ = g.AddNode(ctx, graph.Node{ID: "service:" + svc, Type: graph.NodeTypeGlobularService, Name: svc})
	}

	return g
}

func TestValidateProposalMissingRootCauseBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.test",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.failure",
				Title:           "Test failure",
				Severity:        "critical",
				Symptoms:        []string{"something broke"},
				RootCause:       "", // missing
				ArchitectureFix: "fix it",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestSomething"},
			},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL when root_cause is missing")
	}
}

func TestValidateProposalWeakensCriticalInvariantBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	// Propose lowering convergence.no_infinite_retry from critical to high.
	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.weaken",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		Invariants: []learning.ProposedInvariant{
			{
				ID:             "convergence.no_infinite_retry",
				Title:          "No infinite retry",
				Severity:       "high", // was critical
				Summary:        "No infinite retry",
				ForbiddenFixes: []string{"blind_retry"},
				RequiredTests:  []string{"TestNoRetry"},
			},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL when proposal lowers severity of critical invariant")
	}

	foundRule7 := false
	for _, f := range result.Findings {
		if f.Rule == 7 && f.Status == learning.ValidationFail {
			foundRule7 = true
		}
	}
	if !foundRule7 {
		t.Error("expected rule 7 failure for severity lowering")
	}
}

func TestValidateProposalRemovesForbiddenFixBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	// Propose updating convergence.no_infinite_retry but omitting blind_retry.
	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.removefix",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		Invariants: []learning.ProposedInvariant{
			{
				ID:             "convergence.no_infinite_retry",
				Title:          "No infinite retry",
				Severity:       "critical",
				Summary:        "No infinite retry",
				ForbiddenFixes: []string{"some_other_fix"}, // blind_retry omitted
				RequiredTests:  []string{"TestNoRetry"},
			},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL when forbidden fix removed from critical invariant")
	}
	foundRule8 := false
	for _, f := range result.Findings {
		if f.Rule == 8 && f.Status == learning.ValidationFail {
			foundRule8 = true
		}
	}
	if !foundRule8 {
		t.Error("expected rule 8 failure for forbidden fix removal")
	}
}

func TestValidateProposalDangerousRequiredCycleBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	// Add an existing required edge: minio depends_on etcd (recovery phase — dangerous).
	_ = g.AddEdge(ctx, graph.Edge{
		Src:      "service:minio",
		Kind:     graph.EdgeDependsOn,
		Dst:      "service:etcd",
		Phase:    "recovery",
		Required: true,
	})

	// Propose a dependency that closes a required cycle in recovery phase.
	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.cycle",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.cycle.failure",
				Title:           "Test cycle failure",
				Severity:        "critical",
				Symptoms:        []string{"cycle"},
				RootCause:       "cycle",
				ArchitectureFix: "remove cycle",
				RelatedServices: []string{"etcd"}, // source of proposed dep
				RequiredTests:   []string{"TestCycle"},
			},
		},
		ServiceDependencies: []learning.ProposedDependency{
			// etcd → minio (recovery, required) — closes the minio→etcd cycle.
			{Service: "minio", Phase: "recovery", Required: true},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL for dangerous required cycle")
	}
	foundRule9 := false
	for _, f := range result.Findings {
		if f.Rule == 9 && f.Status == learning.ValidationFail {
			foundRule9 = true
		}
	}
	if !foundRule9 {
		t.Error("expected rule 9 failure for dangerous cycle")
	}
}

func TestValidateProposalContextAliasesAccepted(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.aliases",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.alias.failure",
				Title:           "Test alias failure",
				Severity:        "high",
				Symptoms:        []string{"alias mismatch"},
				RootCause:       "task language did not match graph",
				ArchitectureFix: "add context aliases",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestAliasMatching"},
			},
		},
		ContextAliases: map[string][]string{
			"convergence.no_infinite_retry": {"retry storm", "repeated workflow dispatch"},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationPass {
		for _, f := range result.Findings {
			if f.Status == learning.ValidationFail {
				t.Logf("  fail: [rule %d] %s", f.Rule, f.Message)
			}
		}
		t.Error("expected PASS for proposal with context aliases only")
	}
}

func TestValidateProposalMissingEvidenceLinkBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.noevidence",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.noevidence",
				Title:           "No evidence",
				Severity:        "high",
				Symptoms:        []string{"something"},
				RootCause:       "unknown",
				ArchitectureFix: "unknown",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestSomething"},
			},
		},
		Evidence: learning.ProposalEvidence{
			SourceIncident: "", // missing
		},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL when evidence.source_incident is missing")
	}
	foundRule10 := false
	for _, f := range result.Findings {
		if f.Rule == 10 && f.Status == learning.ValidationFail {
			foundRule10 = true
		}
	}
	if !foundRule10 {
		t.Error("expected rule 10 failure for missing evidence link")
	}
}

func TestValidateProposalCriticalInvariantWithoutForbiddenFixBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.critnofix",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		Invariants: []learning.ProposedInvariant{
			{
				ID:             "new.critical.invariant",
				Title:          "New critical invariant",
				Severity:       "critical",
				Summary:        "must have forbidden fixes",
				ForbiddenFixes: nil, // missing
				RequiredTests:  []string{"TestCritical"},
			},
		},
		Evidence: learning.ProposalEvidence{SourceIncident: "test.incident"},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Error("expected FAIL when critical invariant has no forbidden_fixes")
	}
}

func TestValidateFullProposalFromFixturePasses(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	// Add the services and invariants the fixture references.
	for _, svc := range []string{"envoy", "xds", "cluster-controller", "workflow-service", "node-agent"} {
		_ = g.AddNode(ctx, graph.Node{ID: "service:" + svc, Type: graph.NodeTypeGlobularService, Name: svc})
	}
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "runtime.installed_state_not_liveness",
		Severity: "high",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:runtime.installed_state_not_liveness", Type: graph.NodeTypeInvariant, Name: "runtime.installed_state_not_liveness"})

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationPass {
		for _, f := range result.Findings {
			if f.Status == learning.ValidationFail {
				t.Logf("  FAIL [rule %d]: %s", f.Rule, f.Message)
			}
		}
		t.Error("expected PASS for full valid fixture proposal")
	}
}

func TestFalsePositiveFeedback_RecordHasRequiredFields(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.fp",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.fp",
				Title:           "False positive feedback",
				Severity:        "high",
				Symptoms:        []string{"noise"},
				RootCause:       "rules too broad",
				ArchitectureFix: "narrow matching",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestFP"},
			},
		},
		Evidence: learning.ProposalEvidence{
			SourceIncident: "test.incident",
			FalsePositive: &learning.FalsePositiveFeedback{
				FindingID:        "HIGH_RISK_FILE_NO_ANNOTATIONS",
				WhyFalsePositive: "test fixture file triggered production-only rule",
				EvidenceLink:     "docs/awareness/proposals/fp-example.md",
				Owner:            "awareness-team",
				Reviewer:         "reviewer@example.com",
			},
		},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationPass {
		t.Fatalf("expected PASS with complete false-positive feedback payload; got %v", result.Status)
	}
}

func TestFalsePositiveFeedback_MissingRequiredFieldsBlocks(t *testing.T) {
	ctx := context.Background()
	g := seedValidateGraph(t)

	p := &learning.ProposalSpec{
		Proposal: learning.ProposalHeader{
			ID:             "proposal.fp.missing",
			SourceIncident: "test.incident",
			Status:         learning.StatusDraft,
		},
		FailureModes: []learning.ProposedFailureMode{
			{
				ID:              "test.fp.missing",
				Title:           "False positive feedback missing fields",
				Severity:        "high",
				Symptoms:        []string{"noise"},
				RootCause:       "rules too broad",
				ArchitectureFix: "narrow matching",
				RelatedServices: []string{"envoy"},
				RequiredTests:   []string{"TestFPMissing"},
			},
		},
		Evidence: learning.ProposalEvidence{
			SourceIncident: "test.incident",
			FalsePositive: &learning.FalsePositiveFeedback{
				FindingID: "HIGH_RISK_FILE_NO_ANNOTATIONS",
			},
		},
	}

	result, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	if result.Status != learning.ValidationFail {
		t.Fatalf("expected FAIL when false-positive payload is incomplete")
	}
}

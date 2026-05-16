package contextnav

// falsifiers_test.go — Phase 6 acceptance tests for the falsifier
// template registry. Pins:
//   - templates produce the expected family-specific claims for known
//     failure_mode / invariant IDs;
//   - traces still carry at least one falsifier when no template matches
//     (generic fallback);
//   - no template suggests a destructive command (the registry's safety
//     contract);
//   - the rawKnowledge fallback's falsifier remains untouched (Phase 1
//     contract).

import (
	"strings"
	"testing"
)

// destructiveCommandTokens flags shell phrases that would mutate cluster
// state if executed. Used by TestFalsifiers_NoDestructiveCommands to
// scan every template Command string.
var destructiveCommandTokens = []string{
	"rm ", "rm -",
	"systemctl stop",
	"systemctl start",
	"systemctl restart",
	"systemctl disable",
	"systemctl enable",
	"etcdctl put",
	"etcdctl del",
	"etcdctl rm",
	"kubectl delete",
	"kubectl apply",
	"globular deploy",
	"globular install",
	"globular rollback",
	"globular cluster reset",
	" --force",
	" --apply",
	"reboot",
	"shutdown",
	"kill -",
	"pkill",
}

// TestFalsifiers_NoDestructiveCommands is the safety contract: every
// template Command string MUST be inspection-only. Anything that mutates
// cluster state belongs in NextActions with RequiresAck=true (Phase 7),
// not here.
func TestFalsifiers_NoDestructiveCommands(t *testing.T) {
	for _, tmpl := range failureFalsifierTemplates {
		for _, f := range tmpl.falsifiers {
			cmd := strings.ToLower(f.Command)
			for _, bad := range destructiveCommandTokens {
				if strings.Contains(cmd, bad) {
					t.Errorf("template %s: command %q contains destructive token %q",
						tmpl.family, f.Command, bad)
				}
			}
		}
	}
}

// TestFalsifiers_WorkflowFamily pins the workflow-receipt-resume template:
// a finding whose id mentions workflow.resume / receipt / retry must
// produce the workflow-specific falsifiers, not the generic.
func TestFalsifiers_WorkflowFamily(t *testing.T) {
	ids := []string{
		"workflow.resume_poisoning",
		"workflow_receipts_required",
		"workflow.retry_loop",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			f := falsifiersForFinding("failure_mode", id)
			if len(f) == 0 {
				t.Fatalf("workflow template missed id %q", id)
			}
			// First claim should reference workflow retry loop.
			if !containsCI(f[0].Claim, "workflow") && !containsCI(f[0].Claim, "retry") {
				t.Errorf("expected workflow-flavoured claim; got %q", f[0].Claim)
			}
		})
	}
}

// TestFalsifiers_RestartStormFamily pins the restart_storm template.
func TestFalsifiers_RestartStormFamily(t *testing.T) {
	f := falsifiersForFinding("failure_mode", "service.restart_singleflight")
	if len(f) == 0 {
		t.Fatalf("restart template missed")
	}
	found := false
	for _, x := range f {
		if containsCI(x.Claim, "restart") || containsCI(x.Claim, "systemd") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected restart/systemd claim; got %+v", f)
	}
}

// TestFalsifiers_DesiredInstalledFamily covers the desired/installed
// mismatch template — the most-asked-about family in practice.
func TestFalsifiers_DesiredInstalledFamily(t *testing.T) {
	ids := []string{
		"desired.build_id_immutable",
		"install.desired_state.invisible_service",
		"runtime.installed_state_not_liveness",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			f := falsifiersForFinding("failure_mode", id)
			if len(f) == 0 {
				t.Fatalf("desired/installed template missed id %q", id)
			}
		})
	}
}

// TestFalsifiers_PKIFamily pins the cert/SAN template.
func TestFalsifiers_PKIFamily(t *testing.T) {
	f := falsifiersForFinding("failure_mode", "pki.ca_not_published")
	if len(f) == 0 {
		t.Fatalf("pki template missed")
	}
	if !containsCI(f[0].Claim, "cert") && !containsCI(f[0].Claim, "san") && !containsCI(f[0].Claim, "ca") {
		t.Errorf("expected cert/SAN claim; got %q", f[0].Claim)
	}
}

// TestFalsifiers_DNSFamily pins the dns_endpoint template.
func TestFalsifiers_DNSFamily(t *testing.T) {
	f := falsifiersForFinding("invariant", "service.endpoint.etcd_address_reachability")
	if len(f) == 0 {
		t.Fatalf("dns template missed")
	}
}

// TestFalsifiers_ScyllaFamily pins the scylla_storage template — covers
// scylla / minio / objectstore / repository.minio.
func TestFalsifiers_ScyllaFamily(t *testing.T) {
	ids := []string{
		"scylla.critical_keyspace_under_replicated",
		"objectstore.topology_contract",
		"repository.minio.recovery_cycle",
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			f := falsifiersForFinding("failure_mode", id)
			if len(f) == 0 {
				t.Fatalf("scylla/objectstore template missed id %q", id)
			}
		})
	}
}

// TestFalsifiers_UnknownIDFallsThroughToGeneric pins the generic-fallback
// contract: when no template matches, falsifiersForFinding returns an
// empty slice and the caller falls back to genericFalsifier — so the
// trace still carries one falsifier.
func TestFalsifiers_UnknownIDFallsThroughToGeneric(t *testing.T) {
	got := falsifiersForFinding("failure_mode", "totally.unknown.failure")
	if got != nil {
		t.Errorf("expected nil for unknown id, got %+v", got)
	}
	// End-to-end: Build with the unknown id should still emit one falsifier.
	traces := Build(BuildInputs{
		FailureModes:        []string{"totally.unknown.failure"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
	})
	if len(traces[0].Falsifiers) == 0 {
		t.Error("trace must carry at least one falsifier (generic fallback)")
	}
}

// TestFalsifiers_TemplatedTraceUsesTemplateNotGeneric pins the precedence
// rule: a template match REPLACES the generic falsifier, not appends to
// it. Otherwise the trace would carry two falsifier sets of different
// specificity, confusing the agent.
func TestFalsifiers_TemplatedTraceUsesTemplateNotGeneric(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		Confidence:          ConfidenceHigh,
		GraphFreshnessKnown: true,
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	// Generic falsifier's Claim ("the graph path that produced this
	// finding is intact") must NOT appear when a template matched.
	for _, f := range traces[0].Falsifiers {
		if containsCI(f.Claim, "graph path that produced this finding") {
			t.Errorf("generic falsifier leaked into templated trace: %q", f.Claim)
		}
	}
	// At least one workflow-family falsifier should fire.
	hasWorkflowClaim := false
	for _, f := range traces[0].Falsifiers {
		if containsCI(f.Claim, "workflow") || containsCI(f.Claim, "receipt") {
			hasWorkflowClaim = true
			break
		}
	}
	if !hasWorkflowClaim {
		t.Errorf("expected workflow-family falsifier; got %+v", traces[0].Falsifiers)
	}
}

// TestFalsifiers_EveryTraceHasAtLeastOne is the doc-contract: every
// DecisionTrace, regardless of finding type or template availability,
// must carry at least one falsifier so the agent reads findings as
// falsifiable claims.
func TestFalsifiers_EveryTraceHasAtLeastOne(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:   []string{"workflow.resume_poisoning", "totally.unknown"},
		Invariants:     []string{"pki.ca_not_published", "no.template.match"},
		ForbiddenFixes: []string{"unmapped.forbidden"},
		RawKnowledge: []RawKnowledgeRef{{
			Source: "failure_modes.yaml",
			Kind:   "failure_mode",
			ID:     "raw.only.match",
		}},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
	})
	for i, tr := range traces {
		if len(tr.Falsifiers) == 0 {
			t.Errorf("trace[%d] (%s:%s) has no falsifier", i, tr.FindingType, tr.FindingID)
		}
	}
}

// containsCI is a case-insensitive contains so tests stay terse.
func containsCI(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

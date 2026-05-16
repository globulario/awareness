package preflight_test

// format_decision_trace_test.go — Phase 9 acceptance tests for the
// "Decision traces" section in agent format. Pins:
//   - section renders when DecisionTraces is non-empty;
//   - traces are sorted by risk (forbidden_fix > invariant > failure_mode);
//   - top-N caps trim pivots / actions / falsifiers per trace;
//   - agent output stays well under 200 lines for a normal preflight;
//   - empty DecisionTraces silently omits the section.

import (
	"strings"
	"testing"

	"github.com/globulario/awareness/preflight"
)

// makeReportWithTraces returns a minimal report carrying a curated set
// of decision traces for rendering tests.
func makeReportWithTraces() *preflight.Report {
	r := makeTestReport()
	r.DecisionTraces = []preflight.DecisionTrace{
		{
			FindingID:   "workflow.resume_poisoning",
			FindingType: preflight.FindingFailureMode,
			Confidence:  "high",
			Owner: preflight.OwnerContext{
				Layer:   "runtime",
				Service: "workflow",
				Package: "workflow-service",
			},
			MatchedBy: []preflight.EvidenceRef{
				{Source: "graph", PathSummary: "failure_mode:workflow.resume_poisoning --violates--> invariant:workflow_receipts_required", Confidence: 0.9, Freshness: "fresh"},
				{Source: "runtime", Reason: "systemd status shows repeated restart", Confidence: 0.95, Freshness: "fresh"},
				{Source: "trust_envelope", Reason: "overall trust verdict: usable", Confidence: 0.75, Freshness: "fresh"},
			},
			Pivots: []preflight.ContextPivot{
				{Kind: "source_invariant", ID: "invariant:workflow_receipts_required"},
				{Kind: "fix_case", ID: "fix_case:workflow_resume_receipt_gate"},
				{Kind: "incident", ID: "incident:INC-2026-0007"},
				{Kind: "forbidden_fix", ID: "resume_without_receipt"},
			},
			NextActions: []preflight.DiagnosticAction{
				{Kind: "rebuild", Command: "globular awareness build --clean", Reason: "graph stale", SafeToRun: true},
				{Kind: "inspect", Command: "globular awareness node-context --node failure_mode:workflow.resume_poisoning --zoom history --format agent", Reason: "inspect", SafeToRun: true},
				{Kind: "test", Command: "go test ./awareness/...", Reason: "required tests", SafeToRun: true},
			},
			Falsifiers: []preflight.Falsifier{
				{Claim: "a workflow retry loop is active", HowToCheck: "inspect runs"},
				{Claim: "the failed step has no terminal receipt", HowToCheck: "list outcomes"},
			},
		},
		{
			FindingID:   "use raw artifact digest as desired_hash",
			FindingType: preflight.FindingForbiddenFix,
			Confidence:  "medium",
			MatchedBy: []preflight.EvidenceRef{
				{Source: "graph", Confidence: 0.85, Freshness: "fresh"},
			},
			Pivots: []preflight.ContextPivot{},
			Falsifiers: []preflight.Falsifier{
				{Claim: "desired_hash uses canonical normalization", HowToCheck: "compare hash inputs"},
			},
		},
	}
	return r
}

// TestAgentFormat_DecisionTracesSectionRenders pins the section's
// existence in agent output when traces are present.
func TestAgentFormat_DecisionTracesSectionRenders(t *testing.T) {
	r := makeReportWithTraces()
	out, err := preflight.Render(r, preflight.FormatAgent)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "Decision traces:") {
		t.Errorf("agent output missing 'Decision traces:' section")
	}
	if !strings.Contains(out, "finding: failure_mode.workflow.resume_poisoning") {
		t.Errorf("agent output missing failure_mode trace finding line")
	}
	if !strings.Contains(out, "owner: runtime / workflow / workflow-service") {
		t.Errorf("agent output missing owner one-liner")
	}
}

// TestAgentFormat_DecisionTracesOrderedByRisk pins the doc's render
// rank: forbidden_fix appears before failure_mode even though the
// canonical JSON order puts FailureMode first.
func TestAgentFormat_DecisionTracesOrderedByRisk(t *testing.T) {
	r := makeReportWithTraces()
	out, _ := preflight.Render(r, preflight.FormatAgent)

	idxFF := strings.Index(out, "finding: forbidden_fix.")
	idxFM := strings.Index(out, "finding: failure_mode.")
	if idxFF < 0 || idxFM < 0 {
		t.Fatalf("expected both forbidden_fix and failure_mode trace lines; out=\n%s", out)
	}
	if idxFF > idxFM {
		t.Errorf("forbidden_fix trace should render before failure_mode (risk order); "+
			"got ff@%d fm@%d", idxFF, idxFM)
	}
}

// TestAgentFormat_DecisionTracesShowForbiddenSubsection pins that
// forbidden_fix pivots are broken out under their own "forbidden:"
// heading so the agent can scan do-not-dos quickly.
func TestAgentFormat_DecisionTracesShowForbiddenSubsection(t *testing.T) {
	r := makeReportWithTraces()
	out, _ := preflight.Render(r, preflight.FormatAgent)
	if !strings.Contains(out, "forbidden:") {
		t.Errorf("agent output missing 'forbidden:' subsection")
	}
	if !strings.Contains(out, "- resume_without_receipt") {
		t.Errorf("agent output missing the forbidden_fix entry; out=\n%s", out)
	}
}

// TestAgentFormat_DecisionTracesShowCompactPivotsAndActions pins the
// shape: pivots emit Kind: ID, next emits the Command, falsify emits
// the Claim.
func TestAgentFormat_DecisionTracesShowCompactPivotsAndActions(t *testing.T) {
	r := makeReportWithTraces()
	out, _ := preflight.Render(r, preflight.FormatAgent)

	wantSubstrings := []string{
		"pivots:",
		"source_invariant: invariant:workflow_receipts_required",
		"next:",
		"globular awareness build --clean",
		"falsify:",
		"a workflow retry loop is active",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("agent output missing %q\nout=\n%s", want, out)
		}
	}
}

// TestAgentFormat_DecisionTracesEmptyOmitsSection pins the inverse: a
// report with no DecisionTraces silently omits the section — agents
// shouldn't see an empty header.
func TestAgentFormat_DecisionTracesEmptyOmitsSection(t *testing.T) {
	r := makeTestReport()
	r.DecisionTraces = nil
	out, _ := preflight.Render(r, preflight.FormatAgent)
	if strings.Contains(out, "Decision traces:") {
		t.Errorf("agent output should not include 'Decision traces:' header when traces are empty")
	}
}

// TestAgentFormat_DecisionTracesLineBudget pins the doc's 200-line
// budget: with the test report (~3 traces and full sections) the agent
// output stays well under it. The number is conservative because
// renderAgent already emits ~50 lines of non-trace content.
func TestAgentFormat_DecisionTracesLineBudget(t *testing.T) {
	r := makeReportWithTraces()
	// Pad in some more invariants/failure modes to simulate a realistic preflight.
	r.Invariants = append(r.Invariants,
		"infra.heartbeat_not_desired_authority",
		"desired.build_id_immutable",
		"service.restart_singleflight")
	r.FailureModes = append(r.FailureModes,
		"workflow.backend_unavailable",
		"deterministic.install.failure.retry_loop")
	out, _ := preflight.Render(r, preflight.FormatAgent)
	lines := strings.Count(out, "\n")
	if lines >= 200 {
		t.Errorf("agent output exceeded 200-line budget: %d lines\n--- output ---\n%s",
			lines, out)
	}
}

// TestAgentFormat_DecisionTracesTopNCaps pins the cap behavior: a
// trace with many pivots/actions/falsifiers should be trimmed in agent
// format. JSON output still has the full list (tested in contextnav).
func TestAgentFormat_DecisionTracesTopNCaps(t *testing.T) {
	r := makeTestReport()
	// One trace with 6 pivots, 6 actions, 4 falsifiers.
	r.DecisionTraces = []preflight.DecisionTrace{
		{
			FindingID:   "fm.lots",
			FindingType: preflight.FindingFailureMode,
			Confidence:  "high",
			MatchedBy:   []preflight.EvidenceRef{{Source: "graph", Confidence: 0.85}},
			Pivots: []preflight.ContextPivot{
				{Kind: "required_test", ID: "T1"},
				{Kind: "required_test", ID: "T2"},
				{Kind: "required_test", ID: "T3"},
				{Kind: "required_test", ID: "T4"},
				{Kind: "required_test", ID: "T5"},
				{Kind: "required_test", ID: "T6"},
			},
			NextActions: []preflight.DiagnosticAction{
				{Kind: "rebuild", Command: "cmd1", SafeToRun: true},
				{Kind: "rebuild", Command: "cmd2", SafeToRun: true},
				{Kind: "rebuild", Command: "cmd3", SafeToRun: true},
				{Kind: "rebuild", Command: "cmd4", SafeToRun: true},
			},
			Falsifiers: []preflight.Falsifier{
				{Claim: "F1"}, {Claim: "F2"}, {Claim: "F3"}, {Claim: "F4"},
			},
		},
	}
	out, _ := preflight.Render(r, preflight.FormatAgent)
	// Top 3 pivots should appear; T4, T5, T6 should not.
	for _, want := range []string{"T1", "T2", "T3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing pivot %q within top-3 cap; out=\n%s", want, out)
		}
	}
	for _, dontWant := range []string{"T4", "T5", "T6"} {
		if strings.Contains(out, dontWant) {
			t.Errorf("pivot %q should be trimmed by top-3 cap; out=\n%s", dontWant, out)
		}
	}
	// Top 3 actions; cmd4 should not appear.
	for _, want := range []string{"cmd1", "cmd2", "cmd3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing action %q within top-3 cap; out=\n%s", want, out)
		}
	}
	if strings.Contains(out, "cmd4") {
		t.Errorf("action cmd4 should be trimmed by top-3 cap; out=\n%s", out)
	}
	// Top 2 falsifiers; F3 / F4 should not appear.
	for _, want := range []string{"F1", "F2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing falsifier %q within top-2 cap; out=\n%s", want, out)
		}
	}
	for _, dontWant := range []string{"F3", "F4"} {
		// One-letter strings would match too liberally — anchor with the "  - " bullet prefix.
		if strings.Contains(out, "  - "+dontWant+"\n") || strings.Contains(out, "    - "+dontWant+"\n") {
			t.Errorf("falsifier %q should be trimmed by top-2 cap; out=\n%s", dontWant, out)
		}
	}
}

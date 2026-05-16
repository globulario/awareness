package contextnav

// actions_test.go — Phase 7 acceptance tests for the DiagnosticAction
// emitter. Pins:
//   - stale graph emits a rebuild action;
//   - absent/stale live overlay emits a runtime_collect action, fresh
//     overlay does NOT;
//   - every trace gets an inspect action with the right anchor;
//   - RequiredTests produces a test action whose -run regex matches all
//     listed tests;
//   - raw-knowledge traces get only the rebuild action;
//   - all emitted commands are SafeToRun=true and free of destructive
//     tokens (the safety contract).

import (
	"strings"
	"testing"
)

// findAction returns the first DiagnosticAction with the matching Kind,
// or nil when none exists.
func findAction(actions []DiagnosticAction, kind string) *DiagnosticAction {
	for i := range actions {
		if actions[i].Kind == kind {
			return &actions[i]
		}
	}
	return nil
}

// TestActions_StaleGraphEmitsRebuild pins the doc rule: graph stale → a
// rebuild action with the canonical `globular awareness build --clean`
// command.
func TestActions_StaleGraphEmitsRebuild(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		GraphStale:          true,
	})
	a := findAction(traces[0].NextActions, ActionKindRebuild)
	if a == nil {
		t.Fatalf("expected rebuild action under stale graph; got %+v", traces[0].NextActions)
	}
	if !strings.Contains(a.Command, "awareness build --clean") {
		t.Errorf("rebuild command = %q, want it to call 'awareness build --clean'", a.Command)
	}
	if !a.SafeToRun || a.RequiresAck {
		t.Errorf("rebuild action must be SafeToRun=true RequiresAck=false; got %+v", a)
	}
}

// TestActions_AbsentOverlayEmitsLiveSnapshot pins: live overlay
// absent → runtime_collect with `globular awareness live-snapshot`.
// Also confirms the "stale" status produces the action.
func TestActions_AbsentOverlayEmitsLiveSnapshot(t *testing.T) {
	cases := []struct {
		name   string
		status string
	}{
		{"absent", "absent"},
		{"stale", "stale"},
		{"failed", "failed"},
		{"partial", "partial"},
		{"empty", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			traces := Build(BuildInputs{
				FailureModes:        []string{"workflow.resume_poisoning"},
				Confidence:          ConfidenceMedium,
				GraphFreshnessKnown: true,
				LiveOverlayStatus:   c.status,
			})
			a := findAction(traces[0].NextActions, ActionKindRuntimeCollect)
			if a == nil {
				t.Fatalf("expected runtime_collect action for status=%q; got %+v",
					c.status, traces[0].NextActions)
			}
			if !strings.Contains(a.Command, "live-snapshot") {
				t.Errorf("runtime_collect command = %q, want it to call live-snapshot", a.Command)
			}
		})
	}
}

// TestActions_FreshOverlaySuppressesLiveSnapshot pins the inverse: when
// the overlay is fresh, no runtime_collect action is suggested.
func TestActions_FreshOverlaySuppressesLiveSnapshot(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		LiveOverlayStatus:   "fresh",
	})
	if findAction(traces[0].NextActions, ActionKindRuntimeCollect) != nil {
		t.Errorf("fresh overlay must not produce runtime_collect; got %+v", traces[0].NextActions)
	}
}

// TestActions_InspectActionUsesFindingAnchor pins the inspect-command
// shape: the --node argument carries the prefixed graph node id for the
// finding, and --zoom history is set so the agent sees source invariant
// + incidents + fixes in one call (mirrors the doc example).
func TestActions_InspectActionUsesFindingAnchor(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		LiveOverlayStatus:   "fresh",
	})
	a := findAction(traces[0].NextActions, ActionKindInspect)
	if a == nil {
		t.Fatalf("expected inspect action; got %+v", traces[0].NextActions)
	}
	if !strings.Contains(a.Command, "--node failure_mode:workflow.resume_poisoning") {
		t.Errorf("inspect command should anchor on the finding; got %q", a.Command)
	}
	if !strings.Contains(a.Command, "--zoom history") {
		t.Errorf("inspect command should use --zoom history; got %q", a.Command)
	}
}

// TestActions_RequiredTestsEmitTestAction pins the test-emitter: when
// RequiredTests is non-empty, a test action with a -run regex matching
// every listed name fires.
func TestActions_RequiredTestsEmitTestAction(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		RequiredTests:       []string{"TestResumeRequiresReceipt", "TestBlockedReleaseRetryClassification"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		LiveOverlayStatus:   "fresh",
	})
	a := findAction(traces[0].NextActions, ActionKindTest)
	if a == nil {
		t.Fatalf("expected test action; got %+v", traces[0].NextActions)
	}
	if !strings.Contains(a.Command, "go test") {
		t.Errorf("test action should run go test; got %q", a.Command)
	}
	for _, name := range []string{"TestResumeRequiresReceipt", "TestBlockedReleaseRetryClassification"} {
		if !strings.Contains(a.Command, name) {
			t.Errorf("-run regex should include %q; got %q", name, a.Command)
		}
	}
}

// TestActions_RawKnowledgeTraceGetsOnlyRebuild pins the honesty
// contract: a raw-yaml fallback trace receives the rebuild action only.
// Layered inspect/test suggestions would imply the fallback match is
// load-bearing, which it explicitly is not.
func TestActions_RawKnowledgeTraceGetsOnlyRebuild(t *testing.T) {
	traces := Build(BuildInputs{
		RawKnowledge: []RawKnowledgeRef{{
			Source: "failure_modes.yaml",
			Kind:   "failure_mode",
			ID:     "etcd.leader_instability",
		}},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if len(traces[0].NextActions) != 1 {
		t.Errorf("raw trace should carry exactly one action, got %d: %+v",
			len(traces[0].NextActions), traces[0].NextActions)
	}
	if traces[0].NextActions[0].Kind != ActionKindRebuild {
		t.Errorf("raw trace action.Kind = %q, want rebuild", traces[0].NextActions[0].Kind)
	}
}

// TestActions_ActionsAreOrderedRebuildFirst pins the canonical order:
// rebuild → runtime_collect → inspect → test. Agents read the list as
// a prerequisite chain, so the order is part of the contract.
func TestActions_ActionsAreOrderedRebuildFirst(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		RequiredTests:       []string{"TestX"},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		GraphStale:          true,
		LiveOverlayStatus:   "stale",
	})
	want := []string{
		ActionKindRebuild,
		ActionKindRuntimeCollect,
		ActionKindInspect,
		ActionKindTest,
	}
	got := traces[0].NextActions
	if len(got) != len(want) {
		t.Fatalf("expected %d actions, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].Kind != w {
			t.Errorf("actions[%d].Kind = %q, want %q", i, got[i].Kind, w)
		}
	}
}

// TestActions_NoDestructiveCommands is the safety contract: every
// emitted Command MUST be inspection-only. Anything that mutates
// cluster state must set RequiresAck=true and be wired in elsewhere.
// This test scans every action emitted under a "kitchen sink" input
// and fails if any destructive token appears.
func TestActions_NoDestructiveCommands(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:        []string{"workflow.resume_poisoning"},
		Invariants:          []string{"pki.ca_not_published"},
		ForbiddenFixes:      []string{"resume_without_receipt"},
		RequiredTests:       []string{"TestResumeRequiresReceipt", "TestPKIPublication"},
		RawKnowledge: []RawKnowledgeRef{{
			Source: "failure_modes.yaml",
			Kind:   "failure_mode",
			ID:     "etcd.leader_instability",
		}},
		Confidence:          ConfidenceMedium,
		GraphFreshnessKnown: true,
		GraphStale:          true,
		LiveOverlayStatus:   "stale",
	})
	for ti, tr := range traces {
		for ai, a := range tr.NextActions {
			if a.RequiresAck {
				continue // mutating action with explicit ack — out of scope
			}
			if !a.SafeToRun {
				t.Errorf("trace[%d] action[%d] %s: SafeToRun=false without RequiresAck",
					ti, ai, a.Kind)
			}
			cmd := strings.ToLower(a.Command)
			for _, bad := range destructiveCommandTokens {
				if strings.Contains(cmd, bad) {
					t.Errorf("trace[%d] action[%d] %s: command %q contains destructive token %q",
						ti, ai, a.Kind, a.Command, bad)
				}
			}
		}
	}
}

// TestActions_BuildRunRegex_EscapesMetachars protects against a foot-gun
// in the -run regex builder: a test name that happens to contain regex
// metachars (unlikely but possible — e.g., "TestX.Y") must be quoted so
// the regex still matches it literally.
func TestActions_BuildRunRegex_EscapesMetachars(t *testing.T) {
	got := buildRunRegex([]string{"TestX.Y", "Test_Z"})
	if !strings.Contains(got, `TestX\.Y`) {
		t.Errorf("expected . to be escaped in %q", got)
	}
}

// TestActions_BuildRunRegex_EmptyReturnsImpossibleMatch pins that an
// empty RequiredTests list yields ^$ so the command stays well-formed
// but matches nothing — the caller would not normally emit a test
// action with no names, but the helper must still be safe.
func TestActions_BuildRunRegex_EmptyReturnsImpossibleMatch(t *testing.T) {
	if got := buildRunRegex(nil); got != "^$" {
		t.Errorf("buildRunRegex(nil) = %q, want ^$", got)
	}
}

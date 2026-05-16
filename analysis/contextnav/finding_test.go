package contextnav

// finding_test.go — Phase 10 acceptance tests for ParseFindingID and
// BuildForFinding. Pins the explicit-finding entry point used by the
// `awareness.finding_context` MCP tool and the CLI command.

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
)

func TestParseFindingID_Accepts(t *testing.T) {
	cases := []struct {
		in        string
		wantKind  string
		wantID    string
	}{
		{"failure_mode:workflow.resume_poisoning", "failure_mode", "workflow.resume_poisoning"},
		{"invariant:workflow_receipts_required", "invariant", "workflow_receipts_required"},
		{"forbidden_fix:resume_without_receipt", "forbidden_fix", "resume_without_receipt"},
		// Case-insensitive kind.
		{"FAILURE_MODE:x", "failure_mode", "x"},
		// Trimmed.
		{"  invariant:y  ", "invariant", "y"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			k, id, err := ParseFindingID(c.in)
			if err != nil {
				t.Fatalf("ParseFindingID(%q): %v", c.in, err)
			}
			if k != c.wantKind || id != c.wantID {
				t.Errorf("got (%q,%q), want (%q,%q)", k, id, c.wantKind, c.wantID)
			}
		})
	}
}

func TestParseFindingID_Rejects(t *testing.T) {
	cases := []string{
		"",
		"no_colon",
		":missing_kind",
		"missing_id:",
		"bogus_kind:x",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, _, err := ParseFindingID(c); err == nil {
				t.Errorf("expected error for %q, got nil", c)
			}
		})
	}
}

// TestBuildForFinding_SingleFailureModeTraceShape pins the shape: one
// trace, matching kind + id, with at least one falsifier (template-
// matched workflow family) and a non-empty NextActions list.
func TestBuildForFinding_SingleFailureModeTraceShape(t *testing.T) {
	g := newGraph(t)
	ctx := context.Background()
	seed(t, g, graph.Node{ID: "failure_mode:workflow.resume_poisoning", Type: graph.NodeTypeFailureMode})
	seed(t, g, graph.Node{ID: "invariant:workflow_receipts_required", Type: graph.NodeTypeInvariant})
	link(t, g, "failure_mode:workflow.resume_poisoning", "violates", "invariant:workflow_receipts_required")

	tr, err := BuildForFinding(ctx, FindingContextOptions{
		Kind:  "failure_mode",
		ID:    "workflow.resume_poisoning",
		Graph: g,
		Task:  "workflow retry loop",
	})
	if err != nil {
		t.Fatalf("BuildForFinding: %v", err)
	}
	if tr.FindingType != FindingFailureMode || tr.FindingID != "workflow.resume_poisoning" {
		t.Errorf("got (%q,%q), want (failure_mode, workflow.resume_poisoning)",
			tr.FindingType, tr.FindingID)
	}
	if len(tr.Falsifiers) == 0 {
		t.Error("expected template-matched falsifiers, got none")
	}
	if len(tr.NextActions) == 0 {
		t.Error("expected NextActions, got none")
	}
	// Pivot from Phase 4 graph walk should surface the source invariant.
	var hasSrcInv bool
	for _, p := range tr.Pivots {
		if p.Kind == PivotKindSourceInvariant {
			hasSrcInv = true
		}
	}
	if !hasSrcInv {
		t.Errorf("expected source_invariant pivot from graph walk; got %+v", tr.Pivots)
	}
}

// TestBuildForFinding_InvariantNoGraphIsSafe pins that Build still
// produces a usable trace when no graph is supplied — owner inference
// and graph-walked pivots simply skip, but the falsifiers + actions
// remain.
func TestBuildForFinding_InvariantNoGraphIsSafe(t *testing.T) {
	tr, err := BuildForFinding(context.Background(), FindingContextOptions{
		Kind: "invariant",
		ID:   "pki.ca_not_published",
	})
	if err != nil {
		t.Fatalf("BuildForFinding: %v", err)
	}
	if tr.FindingType != FindingInvariant {
		t.Errorf("FindingType = %q, want invariant", tr.FindingType)
	}
	// PKI family template should still match without a graph.
	if len(tr.Falsifiers) == 0 {
		t.Errorf("expected PKI-family falsifiers, got none")
	}
}

// TestBuildForFinding_MissingKindOrIDRejected pins the validation
// contract: explicit error rather than a synthetic empty trace.
func TestBuildForFinding_MissingKindOrIDRejected(t *testing.T) {
	if _, err := BuildForFinding(context.Background(), FindingContextOptions{Kind: "", ID: "x"}); err == nil {
		t.Error("expected error for empty Kind")
	}
	if _, err := BuildForFinding(context.Background(), FindingContextOptions{Kind: "failure_mode", ID: ""}); err == nil {
		t.Error("expected error for empty ID")
	}
	if _, err := BuildForFinding(context.Background(), FindingContextOptions{Kind: "bogus", ID: "x"}); err == nil {
		t.Error("expected error for unknown Kind")
	}
}

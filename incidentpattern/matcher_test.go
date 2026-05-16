package incidentpattern_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/incidentpattern"
)

func newTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("open memory graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func seedPattern(t *testing.T, g *graph.Graph, p incidentpattern.IncidentPattern) incidentpattern.IncidentPattern {
	t.Helper()
	st := incidentpattern.NewStore(g)
	stored, err := st.RecordPattern(context.Background(), p)
	if err != nil {
		t.Fatalf("seed pattern: %v", err)
	}
	return stored
}

var basePattern = incidentpattern.IncidentPattern{
	IncidentID:  "INC-2026-0001",
	Title:       "etcd cascade after partial install result promotion",
	Severity:    "critical",
	Summary:     "Leader failover between install result writes caused duplicate install dispatch.",
	FailureMode: "partial_authoritative_state_commit",
	RootCause:   "Install result promotion was split across multiple etcd writes.",
	Lesson:      "Installed-state, result promotion, and action cleanup must commit atomically.",
	Files: []incidentpattern.PatternFile{
		{Path: "golang/cluster_controller/reconcile.go", Role: "dispatch authority"},
		{Path: "golang/node_agent/apply.go", Role: "local execution authority"},
	},
	Symbols: []incidentpattern.PatternSymbol{
		{Symbol: "promoteInstallResult", Role: "failed fix site"},
	},
	Invariants: []incidentpattern.PatternInvariant{
		{InvariantID: "install_result_must_be_atomic", Relationship: "violated"},
	},
	EditShapes: []incidentpattern.EditShape{
		{ShapeKind: "split_authoritative_state_transition",
			Description: "State transition spread over more than one authoritative write.", Dangerous: true},
	},
	FailedFixes: []incidentpattern.FailedFix{
		{Description: "Promoted installed state before action cleanup.",
			Reverted: true, RevertReason: "Leader failover left partial authoritative state."},
	},
}

// Test 1: File overlap alone gives low/medium match but does NOT block.
func TestMatch_FileOverlapAloneDoesNotBlock(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	req := incidentpattern.IncidentMatchRequest{
		SessionID: "sess-t1",
		Task:      "update reconcile loop",
		Files:     []string{"golang/cluster_controller/reconcile.go"},
	}
	matches, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match for file overlap")
	}
	for _, m := range matches {
		if m.Block {
			t.Errorf("file overlap alone must not block, but got block=true for %s (score=%.2f signals=%v)",
				m.IncidentID, m.Score, m.MatchedSignals)
		}
	}
}

// Test 2: Dangerous edit shape produces high confidence.
func TestMatch_DangerousEditShapeHighConfidence(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	req := incidentpattern.IncidentMatchRequest{
		SessionID:     "sess-t2",
		Task:          "fix install dispatch",
		ProposedShape: []string{"split_authoritative_state_transition"},
	}
	matches, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected match for dangerous shape")
	}
	if matches[0].Confidence != "high" && matches[0].Confidence != "medium" {
		t.Errorf("expected high/medium confidence for dangerous shape, got %s (score=%.2f)",
			matches[0].Confidence, matches[0].Score)
	}
	// Shape weight alone is 0.35 → medium at least.
	if matches[0].Score < 0.30 {
		t.Errorf("score should be ≥ 0.35 for shape match, got %.2f", matches[0].Score)
	}
}

// Test 3: Reverted fix + file + shape match → block=true.
func TestMatch_RevertedFixWithSignalsBlocks(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	req := incidentpattern.IncidentMatchRequest{
		SessionID:     "sess-t3",
		Task:          "fix install dispatch after leader failover",
		Files:         []string{"golang/cluster_controller/reconcile.go"},
		Invariants:    []string{"install_result_must_be_atomic"},
		ProposedShape: []string{"split_authoritative_state_transition"},
	}
	matches, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected match")
	}
	m := matches[0]
	if !m.Block {
		t.Errorf("expected block=true for critical+high+reverted_fix, got block=false (score=%.2f confidence=%s signals=%v)",
			m.Score, m.Confidence, m.MatchedSignals)
	}
}

// Test 4: Task text similarity alone does not block.
func TestMatch_TaskTextAloneDoesNotBlock(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	req := incidentpattern.IncidentMatchRequest{
		SessionID: "sess-t4",
		Task:      "etcd cascade leader failover install",
		// No files, invariants, or shapes.
	}
	matches, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	for _, m := range matches {
		if m.Block {
			t.Errorf("task text alone must not block: %s score=%.2f signals=%v",
				m.IncidentID, m.Score, m.MatchedSignals)
		}
	}
}

// Test 5: Rejected proposal link is surfaced in the match.
func TestMatch_RejectedProposalSurfaced(t *testing.T) {
	g := newTestGraph(t)
	p := basePattern
	p.Proposals = []incidentpattern.PatternProposal{
		{ProposalID: "PROP-2026-0014", Relationship: "rejected",
			Reason: "Moved responsibility from workflow receipts to controller polling."},
	}
	stored := seedPattern(t, g, p)

	// Load the pattern back and verify proposal is stored.
	st := incidentpattern.NewStore(g)
	loaded, err := st.LoadPattern(context.Background(), stored.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Proposals) == 0 {
		t.Fatal("expected proposals to be stored and loaded")
	}
	if loaded.Proposals[0].ProposalID != "PROP-2026-0014" {
		t.Errorf("expected PROP-2026-0014, got %s", loaded.Proposals[0].ProposalID)
	}
	if loaded.Proposals[0].Reason == "" {
		t.Error("expected rejection reason to be stored")
	}
}

// Test 6: FormatAgentContextSection includes incident warnings when there are matches.
func TestFormatAgentContextSection_IncludesWarnings(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	req := incidentpattern.IncidentMatchRequest{
		SessionID:     "sess-t6",
		Task:          "fix install retry loop after leader failover",
		Files:         []string{"golang/cluster_controller/reconcile.go"},
		Invariants:    []string{"install_result_must_be_atomic"},
		ProposedShape: []string{"split_authoritative_state_transition"},
	}
	matches, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match for agent context test")
	}

	section := incidentpattern.FormatAgentContextSection(matches)
	if section == "" {
		t.Fatal("FormatAgentContextSection returned empty string for non-empty matches")
	}
	if !containsStr(section, "Relevant Incident Warnings") {
		t.Error("section should contain 'Relevant Incident Warnings' header")
	}
	if !containsStr(section, "INC-2026-0001") {
		t.Error("section should reference the matched incident ID")
	}
}

// Test 7: Acknowledged warning does not re-block in same session.
func TestMatch_AcknowledgedDoesNotReblock(t *testing.T) {
	g := newTestGraph(t)
	seedPattern(t, g, basePattern)

	ack := incidentpattern.NewAcknowledgementStore(g)
	sessionID := "sess-t7"

	req := incidentpattern.IncidentMatchRequest{
		SessionID:     sessionID,
		Task:          "fix install dispatch after leader failover",
		Files:         []string{"golang/cluster_controller/reconcile.go"},
		Invariants:    []string{"install_result_must_be_atomic"},
		ProposedShape: []string{"split_authoritative_state_transition"},
	}

	// First run — should block.
	matches1, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("first match: %v", err)
	}
	if len(matches1) == 0 || !matches1[0].Block {
		t.Fatal("expected block=true on first run")
	}

	// Acknowledge.
	if err := ack.AcknowledgeIncident(context.Background(), sessionID, "INC-2026-0001",
		"Read incident, changed strategy to use atomic etcd transaction."); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	// Second run — should NOT block.
	matches2, err := incidentpattern.Match(context.Background(), g, req)
	if err != nil {
		t.Fatalf("second match: %v", err)
	}
	if len(matches2) > 0 && matches2[0].Block {
		t.Error("expected block=false after acknowledgement in same session")
	}
}

func containsStr(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}

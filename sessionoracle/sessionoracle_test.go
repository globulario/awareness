package sessionoracle_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/sessionoracle"
)

func newTestOracle(t *testing.T) *sessionoracle.Oracle {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("open memory graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return sessionoracle.New(g)
}

func startTestSession(t *testing.T, o *sessionoracle.Oracle) *sessionoracle.AgentSession {
	t.Helper()
	sess, err := o.StartSession(context.Background(), sessionoracle.StartSessionRequest{
		Title:     "Test session",
		Objective: "Implement the thing",
		RepoRoot:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	return sess
}

// Test 1: Files recorded in order.
func TestRecordFileTouches_Order(t *testing.T) {
	o := newTestOracle(t)
	sess := startTestSession(t, o)
	ctx := context.Background()

	paths := []string{"a.go", "b.go", "c.go"}
	for _, p := range paths {
		if _, err := o.RecordFileTouch(ctx, sess.ID, p, "read", "test", 0); err != nil {
			t.Fatalf("record touch %s: %v", p, err)
		}
	}

	snap, err := o.BuildResumeSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if len(snap.FilesTouched) != 3 {
		t.Fatalf("expected 3 file touches, got %d", len(snap.FilesTouched))
	}
	for i, p := range paths {
		if snap.FilesTouched[i].Path != p {
			t.Errorf("touch[%d] expected %s, got %s", i, p, snap.FilesTouched[i].Path)
		}
		if snap.FilesTouched[i].Sequence != i+1 {
			t.Errorf("touch[%d] expected sequence %d, got %d", i, i+1, snap.FilesTouched[i].Sequence)
		}
	}
}

// Test 2: Decision with rationale and alternatives is preserved.
func TestRecordDecision_WithRationale(t *testing.T) {
	o := newTestOracle(t)
	sess := startTestSession(t, o)
	ctx := context.Background()

	_, err := o.RecordDecision(ctx, sessionoracle.RecordDecisionRequest{
		SessionID:              sess.ID,
		Title:                  "Auth authority",
		Decision:               "Controller owns token issuance.",
		Rationale:              "Node-agent must not contact auth service directly.",
		AlternativesConsidered: []string{"node-local tokens", "shared secret"},
		Confidence:             "high",
	})
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}

	snap, err := o.BuildResumeSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if len(snap.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(snap.Decisions))
	}
	d := snap.Decisions[0]
	if d.Title != "Auth authority" {
		t.Errorf("unexpected title: %s", d.Title)
	}
	if d.Rationale == "" {
		t.Error("rationale should not be empty")
	}
	if len(d.AlternativesConsidered) != 2 {
		t.Errorf("expected 2 alternatives, got %d", len(d.AlternativesConsidered))
	}
}

// Test 3: Unfinished work survives session close and appears in snapshot.
func TestUnfinishedWork_SurvivesClose(t *testing.T) {
	o := newTestOracle(t)
	sess := startTestSession(t, o)
	ctx := context.Background()

	_, err := o.RecordUnfinishedWork(ctx, sessionoracle.RecordUnfinishedWorkRequest{
		SessionID:  sess.ID,
		Title:      "Add failover test",
		Description: "Test leader crash between local completion and authoritative commit.",
		Priority:   "high",
		NextAction: "Write integration test before changing reconcile behavior.",
	})
	if err != nil {
		t.Fatalf("record unfinished: %v", err)
	}

	snap, err := o.CloseSession(ctx, sess.ID, false, sessionoracle.NoopBridge())
	if err != nil {
		t.Fatalf("close session: %v", err)
	}
	if len(snap.Unfinished) != 1 {
		t.Fatalf("expected 1 unfinished item, got %d", len(snap.Unfinished))
	}
	if snap.Unfinished[0].Title != "Add failover test" {
		t.Errorf("unexpected title: %s", snap.Unfinished[0].Title)
	}
	// Recommended next action should point to the high-priority unfinished work.
	if snap.RecommendedNextAction == "" {
		t.Error("recommended_next_action should not be empty")
	}
}

// Test 4: Resume latest session returns the most recent open session.
func TestResumeLatestOpenSession(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	repoRoot := t.TempDir()

	// Start two sessions for the same repo.
	first, err := o.StartSession(ctx, sessionoracle.StartSessionRequest{Title: "First", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // ensure ordered timestamps
	second, err := o.StartSession(ctx, sessionoracle.StartSessionRequest{Title: "Second", RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("start second: %v", err)
	}

	snap, err := o.ResumeLatestOpenSession(ctx, repoRoot)
	if err != nil {
		t.Fatalf("resume latest: %v", err)
	}
	if snap.SessionID != second.ID {
		t.Errorf("expected latest session %s, got %s", second.ID, snap.SessionID)
	}
	_ = first
}

// Test 5: Stale file warning appears on resume when file changed.
func TestResume_StaleFileWarning(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()

	// Write a temp file and record a read touch.
	f, err := os.CreateTemp(t.TempDir(), "stale*.go")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.WriteString("package main\n")
	f.Close()

	sess := startTestSession(t, o)

	if _, err := o.RecordFileTouch(ctx, sess.ID, f.Name(), "read", "initial read", 1); err != nil {
		t.Fatalf("record touch: %v", err)
	}

	// Build a snapshot (captures fingerprint).
	if _, err := o.BuildResumeSnapshot(ctx, sess.ID); err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	// Modify the file AFTER the snapshot.
	if err := os.WriteFile(f.Name(), []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Resume — should detect stale context.
	snap, err := o.ResumeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	hasStale := false
	for _, w := range snap.Warnings {
		if w.WarningType == "stale_context" && w.RelatedFile == f.Name() {
			hasStale = true
			break
		}
	}
	if !hasStale {
		t.Error("expected stale_context warning for modified file, got none")
	}
}

// Test 6: Incident warning stored during session appears in snapshot.
func TestIncidentWarning_SurvivesResume(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	sess := startTestSession(t, o)

	if _, err := o.RecordSessionWarning(ctx, sessionoracle.RecordSessionWarningRequest{
		SessionID:       sess.ID,
		WarningType:     "incident_pattern",
		Severity:        "critical",
		Message:         "INC-2026-0001: avoid split write",
		RelatedIncident: "INC-2026-0001",
	}); err != nil {
		t.Fatalf("record warning: %v", err)
	}

	snap, err := o.ResumeSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	found := false
	for _, w := range snap.Warnings {
		if w.WarningType == "incident_pattern" && w.RelatedIncident == "INC-2026-0001" {
			found = true
		}
	}
	if !found {
		t.Error("expected incident_pattern warning in resumed snapshot")
	}
}

// Test 7: AI Memory bridge receives compact summary (not event-level noise).
func TestClose_PushesToAIMemory(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	sess := startTestSession(t, o)

	// Record a few things.
	if _, err := o.RecordDecision(ctx, sessionoracle.RecordDecisionRequest{
		SessionID:  sess.ID,
		Title:      "Some decision",
		Decision:   "Keep it simple.",
		Rationale:  "Less complexity.",
		Confidence: "high",
	}); err != nil {
		t.Fatalf("record decision: %v", err)
	}
	if err := o.RecordSessionEvent(ctx, sess.ID, "note", "interim note", "body", nil, 1); err != nil {
		t.Fatalf("record event: %v", err)
	}

	var storedSummaries []sessionoracle.AIMemorySessionSummary
	var storedDecisions []sessionoracle.SessionDecision
	bridge := &captureBridge{
		onSummary:  func(s sessionoracle.AIMemorySessionSummary) { storedSummaries = append(storedSummaries, s) },
		onDecision: func(d sessionoracle.SessionDecision) { storedDecisions = append(storedDecisions, d) },
	}

	if _, err := o.CloseSession(ctx, sess.ID, true, bridge); err != nil {
		t.Fatalf("close: %v", err)
	}

	if len(storedSummaries) != 1 {
		t.Fatalf("expected 1 summary stored in ai-memory, got %d", len(storedSummaries))
	}
	if storedSummaries[0].SessionID != sess.ID {
		t.Errorf("summary session_id mismatch")
	}
	// High-confidence decisions should be pushed separately.
	if len(storedDecisions) != 1 {
		t.Errorf("expected 1 durable decision, got %d", len(storedDecisions))
	}
}

// Test 8: Session close with open high-priority work still records snapshot.
func TestClose_CarriesForwardUnfinishedWork(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	sess := startTestSession(t, o)

	if _, err := o.RecordUnfinishedWork(ctx, sessionoracle.RecordUnfinishedWorkRequest{
		SessionID:  sess.ID,
		Title:      "Critical blocker",
		Description: "Something must be done.",
		Priority:   "critical",
	}); err != nil {
		t.Fatalf("record unfinished: %v", err)
	}

	snap, err := o.CloseSession(ctx, sess.ID, false, nil)
	if err != nil {
		t.Fatalf("close session should not error even with open work: %v", err)
	}
	open := 0
	for _, w := range snap.Unfinished {
		if w.Status == "open" {
			open++
		}
	}
	if open == 0 {
		t.Error("snapshot should contain the open unfinished work item")
	}
}

// Test 9: Recommended next action prioritizes failed tests over low-priority work.
func TestRecommendedNextAction_FailedTestFirst(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	sess := startTestSession(t, o)

	if _, err := o.RecordUnfinishedWork(ctx, sessionoracle.RecordUnfinishedWorkRequest{
		SessionID:  sess.ID,
		Title:      "Low priority thing",
		Description: "Not urgent.",
		Priority:   "low",
	}); err != nil {
		t.Fatalf("unfinished: %v", err)
	}
	if _, err := o.RecordTestResult(ctx, sessionoracle.RecordTestResultRequest{
		SessionID: sess.ID,
		Command:   "go test ./...",
		Status:    "failed",
		Summary:   "3 tests failed",
	}); err != nil {
		t.Fatalf("test result: %v", err)
	}

	snap, err := o.BuildResumeSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snap.RecommendedNextAction == "" {
		t.Fatal("recommended_next_action is empty")
	}
	if !contains(snap.RecommendedNextAction, "test") && !contains(snap.RecommendedNextAction, "go test") {
		t.Errorf("expected recommended action to mention test, got: %s", snap.RecommendedNextAction)
	}
}

// Test 10: Unverified assumption remains visible in snapshot.
func TestAssumption_VisibleUntilResolved(t *testing.T) {
	o := newTestOracle(t)
	ctx := context.Background()
	sess := startTestSession(t, o)

	asm, err := o.RecordAssumption(ctx, sessionoracle.RecordAssumptionRequest{
		SessionID:      sess.ID,
		Assumption:     "etcd cluster is healthy",
		ValidationPlan: "Run doctor check.",
	})
	if err != nil {
		t.Fatalf("record assumption: %v", err)
	}

	snap, err := o.BuildResumeSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Assumptions) == 0 {
		t.Fatal("assumption should appear in snapshot")
	}
	if snap.Assumptions[0].Status != "unverified" {
		t.Errorf("expected unverified, got %s", snap.Assumptions[0].Status)
	}

	// Resolve it and verify it's no longer unverified.
	if err := o.ResolveAssumption(ctx, asm.ID, "verified", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	snap2, err := o.BuildResumeSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	for _, a := range snap2.Assumptions {
		if a.ID == asm.ID && a.Status == "unverified" {
			t.Error("assumption should be resolved in second snapshot")
		}
	}
}

// ── test helpers ──────────────────────────────────────────────────────────────

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

type captureBridge struct {
	onSummary  func(sessionoracle.AIMemorySessionSummary)
	onDecision func(sessionoracle.SessionDecision)
}

func (b *captureBridge) StoreSessionSummary(_ context.Context, s sessionoracle.AIMemorySessionSummary) error {
	if b.onSummary != nil {
		b.onSummary(s)
	}
	return nil
}
func (b *captureBridge) StoreDurableDecision(_ context.Context, d sessionoracle.SessionDecision) error {
	if b.onDecision != nil {
		b.onDecision(d)
	}
	return nil
}

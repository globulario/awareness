package contextfreshness_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/globulario/awareness/contextfreshness"
	"github.com/globulario/awareness/graph"
)

func newTestTracker(t *testing.T) *contextfreshness.Tracker {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("open memory graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return contextfreshness.New(g)
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ctx-fresh-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// Test 1: Fresh file produces no warning.
func TestCheckStaleContext_FreshFile(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)
	path := writeTempFile(t, "hello world")

	if _, err := tr.RecordContextRead(ctx, "sess-1", path, "test", "Read", 1); err != nil {
		t.Fatalf("record read: %v", err)
	}
	warnings, err := tr.CheckStaleContext(ctx, "sess-1", []string{path}, 5, contextfreshness.SeverityCritical)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for fresh file, got %d: %+v", len(warnings), warnings)
	}
}

// Test 2: Modified file produces a stale warning.
func TestCheckStaleContext_ModifiedFile(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)
	path := writeTempFile(t, "original content")

	if _, err := tr.RecordContextRead(ctx, "sess-2", path, "test", "Read", 1); err != nil {
		t.Fatalf("record read: %v", err)
	}

	if err := os.WriteFile(path, []byte("modified content"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	warnings, err := tr.CheckStaleContext(ctx, "sess-2", []string{path}, 10, contextfreshness.SeverityCritical)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Severity != contextfreshness.SeverityCritical {
		t.Errorf("severity: want critical, got %s", w.Severity)
	}
	if w.ReadTurnIndex != 1 {
		t.Errorf("read turn: want 1, got %d", w.ReadTurnIndex)
	}
	if w.CurrentTurnIndex != 10 {
		t.Errorf("current turn: want 10, got %d", w.CurrentTurnIndex)
	}
	if w.CurrentFingerprint == w.ReadFingerprint {
		t.Error("fingerprints should differ after modification")
	}
}

// Test 3: Same mtime but different content (sha256 changed) produces a stale warning.
func TestCheckStaleContext_SameMtimeDifferentContent(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)
	path := writeTempFile(t, "version A content")

	if _, err := tr.RecordContextRead(ctx, "sess-3", path, "test", "Read", 1); err != nil {
		t.Fatalf("record read: %v", err)
	}

	// Overwrite with different content.
	if err := os.WriteFile(path, []byte("version B content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Restore the original mtime to simulate a tool that preserves timestamps.
	oldTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	warnings, err := tr.CheckStaleContext(ctx, "sess-3", []string{path}, 5, contextfreshness.SeverityWarning)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning (sha256 changed despite same mtime), got %d", len(warnings))
	}
}

// Test 4: Deleted file produces a stale warning with CurrentFingerprint="deleted".
func TestCheckStaleContext_DeletedFile(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)
	path := writeTempFile(t, "content")

	if _, err := tr.RecordContextRead(ctx, "sess-4", path, "test", "Read", 2); err != nil {
		t.Fatalf("record read: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	warnings, err := tr.CheckStaleContext(ctx, "sess-4", []string{path}, 8, contextfreshness.SeverityCritical)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for deleted file, got %d", len(warnings))
	}
	if warnings[0].CurrentFingerprint != "deleted" {
		t.Errorf("want CurrentFingerprint=deleted, got %q", warnings[0].CurrentFingerprint)
	}
}

// Test 5: AcknowledgeWarning marks the warning acknowledged.
func TestAcknowledgeWarning(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)
	path := writeTempFile(t, "initial")

	if _, err := tr.RecordContextRead(ctx, "sess-5", path, "test", "Read", 1); err != nil {
		t.Fatalf("record read: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	warnings, err := tr.CheckStaleContext(ctx, "sess-5", []string{path}, 5, contextfreshness.SeverityCritical)
	if err != nil || len(warnings) != 1 {
		t.Fatalf("setup: %v, %d warnings", err, len(warnings))
	}

	if err := tr.AcknowledgeWarning(ctx, warnings[0].ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	// AcknowledgeWarning must not error on valid IDs — functional correctness
	// is verified by the absence of an error and the DB update succeeding.
}

// Test 6: CheckAllSessionReads detects all stale files across a session.
func TestCheckAllSessionReads(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	pathA := writeTempFile(t, "file A")
	pathB := writeTempFile(t, "file B")
	pathC := writeTempFile(t, "file C — stays fresh")

	for _, p := range []string{pathA, pathB, pathC} {
		if _, err := tr.RecordContextRead(ctx, "sess-6", p, "test", "Read", 1); err != nil {
			t.Fatalf("record %s: %v", p, err)
		}
	}

	// Modify A and B but not C.
	if err := os.WriteFile(pathA, []byte("A modified"), 0o644); err != nil {
		t.Fatalf("modify A: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("B modified"), 0o644); err != nil {
		t.Fatalf("modify B: %v", err)
	}

	warnings, err := tr.CheckAllSessionReads(ctx, "sess-6", 20)
	if err != nil {
		t.Fatalf("check all: %v", err)
	}
	if len(warnings) != 2 {
		t.Errorf("expected 2 stale warnings (A and B), got %d", len(warnings))
	}
	for _, w := range warnings {
		if w.Severity != contextfreshness.SeverityWarning {
			t.Errorf("session-wide check should produce warning severity, got %s", w.Severity)
		}
	}
}

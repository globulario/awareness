package graph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/graph"
)

// Regression tests for the writable-runtime-alongside-immutable-bundle
// architecture (consolidation of the prior "graph.Open always migrates
// against immutable bundles" composed-path failure).
//
// OpenComposite opens a writable runtime database and ATTACHes the bundle
// read-only. Unqualified reads of bundle-only tables (nodes, edges,
// invariants, failure_modes, graph_builds, context_aliases) resolve via
// the ATTACHed bundle; everything else lives in main and accepts writes.

func TestOpenComposite_ReadsBundleAndWritesRuntime(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	runtimePath := filepath.Join(dir, "runtime.db")

	// Seed a bundle with a node row, then close it.
	b, err := graph.Open(bundlePath)
	if err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	if _, err := b.DB().ExecContext(context.Background(),
		`INSERT INTO nodes (id, type, name) VALUES ('bundle-n1', 'service', 'from-bundle')`); err != nil {
		t.Fatalf("seed bundle insert: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("seed bundle close: %v", err)
	}

	// Open composite: writable runtime + read-only bundle ATTACH.
	g, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Unqualified read of a bundle-only table resolves through ATTACH.
	var name string
	if err := g.DB().QueryRowContext(context.Background(),
		`SELECT name FROM nodes WHERE id = 'bundle-n1'`).Scan(&name); err != nil {
		t.Fatalf("read bundle table via composite: %v", err)
	}
	if name != "from-bundle" {
		t.Errorf("bundle name = %q, want %q", name, "from-bundle")
	}

	// Write to a runtime-mutable table succeeds.
	if _, err := g.DB().ExecContext(context.Background(),
		`INSERT INTO session_events (id, session_id, event_type, created_at) VALUES ('e1', 's1', 'note', 1)`); err != nil {
		t.Fatalf("runtime write: %v", err)
	}
	var evtType string
	if err := g.DB().QueryRowContext(context.Background(),
		`SELECT event_type FROM session_events WHERE id = 'e1'`).Scan(&evtType); err != nil {
		t.Fatalf("read after runtime write: %v", err)
	}
	if evtType != "note" {
		t.Errorf("session_events event_type = %q, want %q", evtType, "note")
	}
}

func TestOpenComposite_RefusesWritesToBundleTables(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	runtimePath := filepath.Join(dir, "runtime.db")

	if b, err := graph.Open(bundlePath); err != nil {
		t.Fatalf("seed bundle: %v", err)
	} else {
		b.Close()
	}

	g, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Unqualified INSERT against nodes resolves to bundle.nodes and is
	// refused because bundle is attached read-only.
	_, err = g.DB().ExecContext(context.Background(),
		`INSERT INTO nodes (id, type, name) VALUES ('illegal', 'x', 'x')`)
	if err == nil {
		t.Fatal("writing a bundle-only table must fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "readonly") &&
		!strings.Contains(strings.ToLower(err.Error()), "read-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "read only") {
		t.Errorf("error should mention read-only; got: %v", err)
	}
}

func TestOpenComposite_CrossDatabaseJoin(t *testing.T) {
	// Cross-DB JOINs (experience_entries × nodes, livecluster snapshots ×
	// edges) are existing query shapes and must continue to work
	// transparently under composite.
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	runtimePath := filepath.Join(dir, "runtime.db")

	b, err := graph.Open(bundlePath)
	if err != nil {
		t.Fatalf("seed bundle: %v", err)
	}
	if _, err := b.DB().ExecContext(context.Background(),
		`INSERT INTO nodes (id, type, name) VALUES ('scorecard:exp1', 'scorecard', 'sc1')`); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	b.Close()

	g, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	if _, err := g.DB().ExecContext(context.Background(),
		`INSERT INTO experience_entries (id) VALUES ('exp1')`); err != nil {
		t.Fatalf("write runtime row: %v", err)
	}

	// Query crosses DBs: experience_entries (main) × nodes (bundle).
	row := g.DB().QueryRowContext(context.Background(), `
		SELECT n.name FROM experience_entries e
		LEFT JOIN nodes n ON n.id = ('scorecard:' || e.id)
		WHERE e.id = 'exp1'`)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("cross-DB JOIN: %v", err)
	}
	if name != "sc1" {
		t.Errorf("JOIN result = %q, want %q", name, "sc1")
	}
}

func TestOpenComposite_PersistsRuntimeDataAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	runtimePath := filepath.Join(dir, "runtime.db")

	if b, err := graph.Open(bundlePath); err != nil {
		t.Fatalf("seed bundle: %v", err)
	} else {
		b.Close()
	}

	// First open: write a runtime row.
	g1, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite #1: %v", err)
	}
	if _, err := g1.DB().ExecContext(context.Background(),
		`INSERT INTO session_events (id, session_id, event_type, created_at) VALUES ('persist1', 's1', 'note', 1)`); err != nil {
		t.Fatalf("write: %v", err)
	}
	g1.Close()

	// Re-open: row must still be there. Bundle ATTACH is re-established,
	// runtime tables must not be re-migrated destructively.
	g2, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite #2: %v", err)
	}
	t.Cleanup(func() { g2.Close() })

	var got string
	if err := g2.DB().QueryRowContext(context.Background(),
		`SELECT event_type FROM session_events WHERE id = 'persist1'`).Scan(&got); err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if got != "note" {
		t.Errorf("event_type = %q, want %q", got, "note")
	}
}

func TestOpenComposite_CreatesRuntimeParentDir(t *testing.T) {
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	if b, err := graph.Open(bundlePath); err != nil {
		t.Fatalf("seed bundle: %v", err)
	} else {
		b.Close()
	}

	// Runtime path under a directory that doesn't yet exist — must be
	// auto-created so Day-0 install doesn't have to pre-stage it.
	nested := filepath.Join(dir, "deep", "nested", "subdir")
	if _, err := os.Stat(nested); err == nil {
		t.Fatalf("precondition: %s should not exist", nested)
	}
	runtimePath := filepath.Join(nested, "runtime.db")

	g, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite: %v", err)
	}
	defer g.Close()

	if info, err := os.Stat(runtimePath); err != nil {
		t.Fatalf("runtime.db not created at %s: %v", runtimePath, err)
	} else if info.IsDir() {
		t.Errorf("%s is a directory, expected file", runtimePath)
	}
}

func TestOpenComposite_RejectsMissingBundle(t *testing.T) {
	dir := t.TempDir()
	_, err := graph.OpenComposite(
		filepath.Join(dir, "no-such-bundle.db"),
		filepath.Join(dir, "runtime.db"),
	)
	if err == nil {
		t.Fatal("OpenComposite with missing bundle must error")
	}
	if !strings.Contains(err.Error(), "stat bundle") {
		t.Errorf("error should mention bundle stat; got: %v", err)
	}
}

func TestOpenComposite_RejectsEmptyPaths(t *testing.T) {
	cases := []struct {
		name        string
		bundlePath  string
		runtimePath string
		wantSubstr  string
	}{
		{"empty bundle", "", "/tmp/r.db", "bundlePath is empty"},
		{"empty runtime", "/tmp/b.db", "", "runtimePath is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := graph.OpenComposite(tc.bundlePath, tc.runtimePath)
			if err == nil {
				t.Fatal("expected error for empty path")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error should contain %q; got: %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestOpenComposite_BundleOnlyTablesAbsentFromMain(t *testing.T) {
	// Direct verification of the partition: the bundle-only tables must
	// not exist in the main schema after OpenComposite. If they did,
	// SQLite would resolve unqualified reads to (empty) main copies,
	// silently shadowing bundle data.
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.db")
	runtimePath := filepath.Join(dir, "runtime.db")
	if b, err := graph.Open(bundlePath); err != nil {
		t.Fatalf("seed bundle: %v", err)
	} else {
		b.Close()
	}

	g, err := graph.OpenComposite(bundlePath, runtimePath)
	if err != nil {
		t.Fatalf("OpenComposite: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	for _, t1 := range []string{"nodes", "edges", "invariants", "failure_modes", "graph_builds", "context_aliases"} {
		var n int
		err := g.DB().QueryRowContext(context.Background(),
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, t1).Scan(&n)
		if err != nil {
			t.Fatalf("sqlite_master query for %s: %v", t1, err)
		}
		if n != 0 {
			t.Errorf("bundle-only table %q still present in main schema (count=%d) — would shadow bundle data", t1, n)
		}
	}

	// And the same names ARE present in the attached bundle schema.
	for _, t1 := range []string{"nodes", "edges", "invariants", "failure_modes", "graph_builds", "context_aliases"} {
		var n int
		err := g.DB().QueryRowContext(context.Background(),
			`SELECT count(*) FROM bundle.sqlite_master WHERE type='table' AND name=?`, t1).Scan(&n)
		if err != nil {
			t.Fatalf("bundle.sqlite_master query for %s: %v", t1, err)
		}
		if n != 1 {
			t.Errorf("bundle table %q missing from attached schema (count=%d)", t1, n)
		}
	}
}

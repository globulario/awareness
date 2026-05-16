package graph_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/graph"
)

// Regression tests for the composed-path failure recorded under
// "graph.Open always migrates against immutable bundles" in
// docs/awareness/composed_path_failures.md.
//
// The signed awareness bundle is content-addressed at
// /var/lib/globular/awareness/installed/<version>/<uuid>/, owned root:root.
// graph.Open always runs migrate() which executes DDL — a write — and
// fails when the service user can't open the file read-write. The fix is
// OpenReadOnly, which honours the bundle's immutability contract.

func TestOpenReadOnly_ReadsBundleWithoutWriting(t *testing.T) {
	// Build a graph with normal Open, close it. That simulates the bundle
	// being built and shipped. Then assert OpenReadOnly can query it.
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.db")
	g, err := graph.Open(path)
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	if _, err := g.DB().ExecContext(context.Background(),
		`INSERT INTO nodes (id, type, name) VALUES ('n1', 'service', 'test')`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("seed Close: %v", err)
	}

	ro, err := graph.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { ro.Close() })

	var name string
	if err := ro.DB().QueryRowContext(context.Background(),
		`SELECT name FROM nodes WHERE id = 'n1'`).Scan(&name); err != nil {
		t.Fatalf("read after OpenReadOnly: %v", err)
	}
	if name != "test" {
		t.Errorf("read value = %q, want %q", name, "test")
	}
}

func TestOpenReadOnly_RefusesWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.db")
	if g, err := graph.Open(path); err != nil {
		t.Fatalf("seed Open: %v", err)
	} else {
		g.Close()
	}

	ro, err := graph.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { ro.Close() })

	_, err = ro.DB().ExecContext(context.Background(),
		`INSERT INTO nodes (id, type, name) VALUES ('n2', 'service', 'illegal-write')`)
	if err == nil {
		t.Fatal("write through OpenReadOnly handle must fail; got nil error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "readonly") &&
		!strings.Contains(strings.ToLower(err.Error()), "read-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "read only") {
		t.Errorf("error should mention read-only; got: %v", err)
	}
}

// The bundle file is root-owned in production. We can't change uid in tests,
// but we can simulate the failure by removing write permission on the file
// and asserting OpenReadOnly still succeeds (proves no write attempt is made
// during open / migrate). This is the operational shape that prevented MCP
// from opening the bundle at all before this commit.
func TestOpenReadOnly_SucceedsWhenFileIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — chmod 0o444 doesn't restrict root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.db")
	if g, err := graph.Open(path); err != nil {
		t.Fatalf("seed Open: %v", err)
	} else {
		g.Close()
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	// Restore write perms on cleanup so t.TempDir's defer can remove the dir.
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		_ = os.Chmod(path, 0o644)
	})

	// graph.Open against this would fail with "attempt to write a readonly
	// database" — that was the exact production symptom on the bundle.
	openErr := func() error {
		g, err := graph.Open(path)
		if err != nil {
			return err
		}
		_ = g.Close()
		return nil
	}()
	if openErr == nil {
		t.Log("note: read-write Open unexpectedly succeeded on a read-only file; " +
			"the regression assertion below for OpenReadOnly is still meaningful")
	}

	ro, err := graph.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly must succeed against a read-only bundle file: %v", err)
	}
	defer ro.Close()

	// And queries must still work.
	if err := ro.DB().PingContext(context.Background()); err != nil {
		t.Errorf("read-only ping failed: %v", err)
	}
}

func TestOpenReadOnly_RejectsMissingFile(t *testing.T) {
	_, err := graph.OpenReadOnly(filepath.Join(t.TempDir(), "absent.db"))
	if err == nil {
		t.Fatal("OpenReadOnly on a missing path must return an error")
	}
}

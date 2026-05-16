package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

// Test 10: Source file node for existing file → no drift finding.
func TestAuditDriftNoStaleNodes(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)
	dir := t.TempDir()

	// Create the file on disk.
	relPath := "pkg/real.go"
	absPath := filepath.Join(dir, relPath)
	_ = os.MkdirAll(filepath.Dir(absPath), 0o755)
	_ = os.WriteFile(absPath, []byte("package pkg\n"), 0o644)

	// Add the node.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "source_file:" + relPath,
		Type: graph.NodeTypeSourceFile,
		Name: "real.go",
		Path: relPath,
	})

	findings := enforce.AuditDrift(ctx, g, dir)
	for _, f := range findings {
		if f.Code == enforce.CodeGraphSourceFileMissing {
			t.Errorf("unexpected stale-node finding for existing file: %v", f)
		}
	}
}

// Test 11: Source file node for deleted file → GRAPH_SOURCE_FILE_MISSING WARNING.
func TestAuditDriftStaleNode(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)
	dir := t.TempDir()

	relPath := "pkg/gone.go"

	// Do NOT create the file on disk — simulate deletion.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "source_file:" + relPath,
		Type: graph.NodeTypeSourceFile,
		Name: "gone.go",
		Path: relPath,
	})

	findings := enforce.AuditDrift(ctx, g, dir)
	found := false
	for _, f := range findings {
		if f.Code == enforce.CodeGraphSourceFileMissing && f.Severity == enforce.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s WARNING, got: %v", enforce.CodeGraphSourceFileMissing, findings)
	}
}

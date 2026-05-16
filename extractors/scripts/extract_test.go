package scripts_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/scripts"
	"github.com/globulario/awareness/graph"
)

func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMultiRepoCrawler_SkipsMissingRepoGracefully(t *testing.T) {
	g := openTestGraph(t)
	healths, err := scripts.Extract(context.Background(), g, []scripts.RepoRoot{
		{Path: "/nonexistent/repo"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(healths) == 0 {
		t.Fatal("expected at least one health entry")
	}
	if healths[0].Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", healths[0].Status)
	}
}

func TestMultiRepoCrawler_IndexesShellScripts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "scripts", "ensure-bootstrap-artifacts.sh"), `#!/bin/bash
# Bootstrap script
PACKAGE_NAME=minio

publish_package() {
    echo "Publishing $1"
}

install_packages() {
    echo "Installing..."
}
`)

	g := openTestGraph(t)
	healths, err := scripts.Extract(context.Background(), g, []scripts.RepoRoot{
		{Path: root, SourceTier: "installer_script"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if healths[0].Status != "ok" {
		t.Errorf("expected ok, got %q: %s", healths[0].Status, healths[0].Error)
	}
	if healths[0].NodesEmitted == 0 {
		t.Error("expected nodes to be emitted")
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "source_file")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.Metadata["source_tier"] == "installer_script" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no installer_script source_file node found")
	}
}

func TestMultiRepoCrawler_ExtractsScriptFunctions(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deploy.sh"), `#!/bin/bash

publish_artifact() {
    echo "publishing"
}

validate_package() {
    echo "validating"
}

sync_from_upstream() {
    echo "syncing"
}
`)

	g := openTestGraph(t)
	_, err := scripts.Extract(context.Background(), g, []scripts.RepoRoot{{Path: root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "source_file")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		t.Fatal("no nodes found")
	}
	fns, _ := nodes[0].Metadata["functions"].(string)
	for _, fn := range []string{"publish_artifact", "validate_package", "sync_from_upstream"} {
		if !contains(fns, fn) {
			t.Errorf("expected function %q in %q", fn, fns)
		}
	}
}

func TestMultiRepoCrawler_CrossRepoEdgeToPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "install.sh"), `#!/bin/bash
PACKAGE_NAME=workflow
PACKAGE_NAME=minio
`)

	g := openTestGraph(t)
	_, err := scripts.Extract(context.Background(), g, []scripts.RepoRoot{{Path: root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypePackage)
	if err != nil {
		t.Fatal(err)
	}
	nameSet := map[string]bool{}
	for _, n := range nodes {
		nameSet[n.Name] = true
	}
	for _, pkg := range []string{"workflow", "minio"} {
		if !nameSet[pkg] {
			t.Errorf("expected package node %q not found; nodes: %v", pkg, names(nodes))
		}
	}
}

func TestMultiRepoCrawler_SourceTierTagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "build.sh"), `#!/bin/bash
echo "building"
`)

	g := openTestGraph(t)
	_, err := scripts.Extract(context.Background(), g, []scripts.RepoRoot{
		{Path: root, SourceTier: "installer_script"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, "source_file")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Metadata["source_tier"] != "installer_script" {
			t.Errorf("expected source_tier=installer_script, got %v", n.Metadata["source_tier"])
		}
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && (s == sub || len(s) > len(sub) && (s[:len(sub)] == sub || containsAfterComma(s, sub)))
}

func containsAfterComma(s, sub string) bool {
	for _, part := range splitComma(s) {
		if part == sub {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	cur := ""
	for _, c := range s {
		if c == ',' {
			result = append(result, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		result = append(result, cur)
	}
	return result
}

func names(nodes []*graph.Node) []string {
	var ns []string
	for _, n := range nodes {
		ns = append(ns, n.Name)
	}
	return ns
}

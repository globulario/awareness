package clusterspec_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/clusterspec"
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

func TestPackageSpecIndexer_MissingPackagesRepoSkipsGracefully(t *testing.T) {
	g := openTestGraph(t)
	health, err := clusterspec.Extract(context.Background(), g, "/nonexistent/packages/metadata")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", health.Status)
	}
}

func TestPackageSpecIndexer_EmptyRootSkips(t *testing.T) {
	g := openTestGraph(t)
	health, err := clusterspec.Extract(context.Background(), g, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "skipped" {
		t.Errorf("expected status=skipped for empty root, got %q", health.Status)
	}
}

func TestPackageSpecIndexer_ParsePackageJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "workflow", "package.json"), `{
		"name": "workflow",
		"type": "service",
		"version": "1.2.0",
		"description": "Workflow engine",
		"profiles": ["core", "compute"],
		"systemd_unit": "globular-workflow.service"
	}`)

	g := openTestGraph(t)
	health, err := clusterspec.Extract(context.Background(), g, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("expected status=ok, got %q (error: %s)", health.Status, health.Error)
	}
	if health.NodesEmitted == 0 {
		t.Error("expected at least 1 node emitted")
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypePackage)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "package:workflow" {
			found = true
			if n.Metadata["kind"] != "service" {
				t.Errorf("expected kind=service, got %v", n.Metadata["kind"])
			}
			if n.Metadata["source_tier"] != "package_spec" {
				t.Errorf("expected source_tier=package_spec, got %v", n.Metadata["source_tier"])
			}
		}
	}
	if !found {
		t.Error("package:workflow node not found in graph")
	}
}


func TestPackageSpecIndexer_EmitsKindNode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "minio", "package.json"), `{
		"name": "minio",
		"type": "infrastructure",
		"version": "0.0.1",
		"systemd_unit": "globular-minio.service",
		"provides_capabilities": ["object-store"]
	}`)

	g := openTestGraph(t)
	_, err := clusterspec.Extract(context.Background(), g, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypePackage)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if n.ID == "package:minio" {
			found = true
			if n.Metadata["kind"] != "infrastructure" {
				t.Errorf("expected kind=infrastructure, got %v", n.Metadata["kind"])
			}
		}
	}
	if !found {
		t.Error("package:minio node not found")
	}
}

func TestPackageSpecIndexer_EmitsProfileEdges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "workflow", "package.json"), `{
		"name": "workflow",
		"type": "service",
		"profiles": ["core", "compute"]
	}`)

	g := openTestGraph(t)
	_, err := clusterspec.Extract(context.Background(), g, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// Check profile nodes exist.
	nodes, err := g.FindNodesByType(ctx, "node_profile")
	if err != nil {
		t.Fatal(err)
	}
	profileNames := map[string]bool{}
	for _, n := range nodes {
		profileNames[n.Name] = true
	}
	for _, p := range []string{"core", "compute"} {
		if !profileNames[p] {
			t.Errorf("expected profile node %q, not found", p)
		}
	}
}

func TestPackageSpecIndexer_ParseSystemdTemplateVars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "minio", "package.json"), `{"name": "minio", "type": "infrastructure"}`)
	writeFile(t, filepath.Join(root, "minio", "systemd", "globular-minio.service"), `
[Service]
ExecStart={{.Prefix}}/bin/minio server {{.MinioDataDir}} --address {{.NodeIP}}:9000
WorkingDirectory={{.StateDir}}
`)

	g := openTestGraph(t)
	_, err := clusterspec.Extract(context.Background(), g, root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSystemdUnit)
	if err != nil {
		t.Fatal(err)
	}

	var tmplNode *graph.Node
	for _, n := range nodes {
		if n.Metadata["is_template"] == true {
			tmplNode = n
			break
		}
	}
	if tmplNode == nil {
		t.Fatal("no template unit node found")
	}
	tvars, _ := tmplNode.Metadata["template_vars"].(string)
	for _, v := range []string{"Prefix", "NodeIP", "MinioDataDir", "StateDir"} {
		if !containsVar(tvars, v) {
			t.Errorf("expected template var %q in %q", v, tvars)
		}
	}
}

func containsVar(s, v string) bool {
	for _, part := range splitComma(s) {
		if part == v {
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

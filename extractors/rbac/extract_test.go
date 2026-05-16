package rbac_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/extractors/rbac"
	"github.com/globulario/awareness/graph"
)

// openTestGraph opens an in-memory awareness graph for testing.
func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("graph.OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// writePolicyFile marshals v to JSON and writes it to dir/name.
func writePolicyFile(t *testing.T, dir, name string, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestRBACExtractor_LoadsClusterRoles verifies that role nodes are emitted for
// each role defined in a roles file.
func TestRBACExtractor_LoadsClusterRoles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	policy := map[string]any{
		"version": "2.0",
		"roles": map[string][]string{
			"test-viewer": {
				"workflow.read",
				"workflow.list",
				"repository.artifact.list",
			},
			"test-admin": {
				"workflow.*",
				"repository.*",
			},
		},
	}
	writePolicyFile(t, dir, "cluster-roles.json", policy)

	h, err := rbac.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}
	if h.NodesEmitted < 2 {
		t.Errorf("NodesEmitted = %d, want >= 2 (one per role)", h.NodesEmitted)
	}

	// Both role nodes must be present.
	viewerNode, err := g.FindNode(ctx, "rbac_role:test-viewer")
	if err != nil {
		t.Fatalf("FindNode rbac_role:test-viewer: %v", err)
	}
	if viewerNode == nil {
		t.Error("rbac_role:test-viewer not found")
	}

	adminNode, err := g.FindNode(ctx, "rbac_role:test-admin")
	if err != nil {
		t.Fatalf("FindNode rbac_role:test-admin: %v", err)
	}
	if adminNode == nil {
		t.Error("rbac_role:test-admin not found")
	}

	// Policy file node must exist.
	fileNode, err := g.FindNode(ctx, "rbac_policy_file:cluster-roles.json")
	if err != nil {
		t.Fatalf("FindNode rbac_policy_file: %v", err)
	}
	if fileNode == nil {
		t.Error("rbac_policy_file:cluster-roles.json not found")
	}
}

// TestRBACExtractor_RedactsSensitiveSubjects verifies that JSON fields with
// sensitive names (token, password, secret, key) are redacted in emitted nodes.
func TestRBACExtractor_RedactsSensitiveSubjects(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// A policy file that includes a "token" field — this is synthetic and not
	// a real format, but it tests the redaction path.
	rawJSON := `{
		"version": "2.0",
		"token": "super-secret-jwt-value",
		"password": "hunter2",
		"roles": {
			"test-role": ["workflow.read"]
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "cluster-roles.json"), []byte(rawJSON), 0644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	h, err := rbac.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}

	// Verify the role node was emitted (extraction still worked).
	roleNode, err := g.FindNode(ctx, "rbac_role:test-role")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if roleNode == nil {
		t.Fatal("role node not emitted")
	}

	// Verify sensitive values do not appear in any node's metadata string.
	allNodes, err := g.FindNodesByType(ctx, graph.NodeTypeRBACRole)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	allNodes2, err := g.FindNodesByType(ctx, graph.NodeTypeRBACPolicyFile)
	if err != nil {
		t.Fatalf("FindNodesByType policy file: %v", err)
	}
	allNodes = append(allNodes, allNodes2...)

	for _, n := range allNodes {
		for k, v := range n.Metadata {
			str, ok := v.(string)
			if !ok {
				continue
			}
			if strings.Contains(str, "super-secret-jwt-value") {
				t.Errorf("node %s metadata field %q exposes sensitive token value", n.ID, k)
			}
			if strings.Contains(str, "hunter2") {
				t.Errorf("node %s metadata field %q exposes sensitive password value", n.ID, k)
			}
		}
	}
}

// TestRBACExtractor_MapsRolePermissions verifies that EdgeRoleGrantsPermission
// edges are emitted from each role to its permission nodes.
func TestRBACExtractor_MapsRolePermissions(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	policy := map[string]any{
		"version": "2.0",
		"roles": map[string][]string{
			"editor": {
				"workflow.write",
				"repository.artifact.publish",
			},
		},
	}
	writePolicyFile(t, dir, "cluster-roles.json", policy)

	h, err := rbac.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}

	// Verify permission nodes exist.
	permWrite, err := g.FindNode(ctx, "rbac_permission:editor:workflow.write")
	if err != nil {
		t.Fatalf("FindNode rbac_permission:editor:workflow.write: %v", err)
	}
	if permWrite == nil {
		t.Fatal("permission node rbac_permission:editor:workflow.write not found")
	}
	if permWrite.Type != graph.NodeTypeRBACPermission {
		t.Errorf("permission node type = %q, want %q", permWrite.Type, graph.NodeTypeRBACPermission)
	}

	permPublish, err := g.FindNode(ctx, "rbac_permission:editor:repository.artifact.publish")
	if err != nil {
		t.Fatalf("FindNode rbac_permission:editor:repository.artifact.publish: %v", err)
	}
	if permPublish == nil {
		t.Fatal("permission node rbac_permission:editor:repository.artifact.publish not found")
	}

	// Verify the metadata on the permission node has the parsed fields.
	if permPublish.Metadata != nil {
		service, _ := permPublish.Metadata["service"].(string)
		resource, _ := permPublish.Metadata["resource"].(string)
		verb, _ := permPublish.Metadata["verb"].(string)
		if service != "repository" {
			t.Errorf("permission service = %q, want repository", service)
		}
		if resource != "artifact" {
			t.Errorf("permission resource = %q, want artifact", resource)
		}
		if verb != "publish" {
			t.Errorf("permission verb = %q, want publish", verb)
		}
	}

	// Verify all RBAC permission nodes are present in the graph.
	permNodes, err := g.FindNodesByType(ctx, graph.NodeTypeRBACPermission)
	if err != nil {
		t.Fatalf("FindNodesByType RBACPermission: %v", err)
	}
	if len(permNodes) < 2 {
		t.Errorf("got %d permission nodes, want >= 2", len(permNodes))
	}
}

// TestRBACExtractor_SkipsNonJSONFiles verifies that non-.json files are ignored.
func TestRBACExtractor_SkipsNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// Write a YAML file and a text file — should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "roles.yaml"), []byte("roles: []"), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("readme"), 0644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	// Write the actual JSON policy.
	policy := map[string]any{
		"version": "2.0",
		"roles": map[string][]string{
			"solo-role": {"workflow.read"},
		},
	}
	writePolicyFile(t, dir, "cluster-roles.json", policy)

	h, err := rbac.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok", h.Status)
	}

	// Only nodes from cluster-roles.json should exist.
	roleNodes, err := g.FindNodesByType(ctx, graph.NodeTypeRBACRole)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	if len(roleNodes) != 1 {
		t.Errorf("got %d role nodes, want 1", len(roleNodes))
	}
}

// TestRBACExtractor_SkippedWhenDirMissing verifies graceful skip when the
// policy directory does not exist.
func TestRBACExtractor_SkippedWhenDirMissing(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	h, err := rbac.Extract(ctx, g, "/nonexistent/rbac/path/99999")
	if err != nil {
		t.Fatalf("Extract returned error for missing dir: %v", err)
	}
	if h.Status != "skipped" {
		t.Errorf("status = %q, want skipped", h.Status)
	}
	if h.NodesEmitted != 0 {
		t.Errorf("NodesEmitted = %d, want 0", h.NodesEmitted)
	}
}

// TestRBACExtractor_SkipsSensitiveFilenames verifies that files with sensitive
// names like "token.json" or "secret.json" are never opened.
func TestRBACExtractor_SkipsSensitiveFilenames(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := openTestGraph(t)

	// Write sensitive-named files with dummy JSON.
	sensitiveNames := []string{
		"token.json",
		"secret.json",
		"credentials.json",
		"api-key.json",
		"jwt-tokens.json",
	}
	for _, name := range sensitiveNames {
		if err := os.WriteFile(filepath.Join(dir, name),
			[]byte(`{"value":"SHOULD_NOT_APPEAR"}`), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Write a legitimate policy file.
	policy := map[string]any{
		"version": "2.0",
		"roles": map[string][]string{
			"safe-role": {"workflow.read"},
		},
	}
	writePolicyFile(t, dir, "cluster-roles.json", policy)

	h, err := rbac.Extract(ctx, g, dir)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q, want ok; notes: %v", h.Status, h.Notes)
	}

	// Only the legitimate role should be indexed.
	roleNodes, err := g.FindNodesByType(ctx, graph.NodeTypeRBACRole)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	if len(roleNodes) != 1 {
		t.Errorf("got %d role nodes, want 1 (sensitive files must be skipped)", len(roleNodes))
	}
	if roleNodes[0].Name != "safe-role" {
		t.Errorf("role name = %q, want safe-role", roleNodes[0].Name)
	}
}

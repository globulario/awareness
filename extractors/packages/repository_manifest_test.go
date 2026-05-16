package packages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/packages"
	"github.com/globulario/awareness/graph"
)

func openPkgGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

const sampleReleaseIndex = `{
  "platform_release": "1.1.5",
  "release_tag": "v1.1.5",
  "packages": [
    {
      "name": "workflow",
      "version": "1.0.93",
      "kind": "service",
      "build_number": 93,
      "build_id": "550e8400-e29b-41d4-a716-446655440000",
      "package_digest": "sha256:abc123",
      "changed_in_release": true
    },
    {
      "name": "minio",
      "version": "1.2.20",
      "kind": "infra",
      "build_number": 20,
      "build_id": "550e8400-e29b-41d4-a716-446655440001",
      "changed_in_release": false
    }
  ]
}`

// TestRepositoryManifestIndexer_ParseReleaseIndex verifies the release-index.json
// parser emits platform_release and artifact nodes.
func TestRepositoryManifestIndexer_ParseReleaseIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-index.json"), []byte(sampleReleaseIndex), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := openPkgGraph(t)
	if err := packages.Extract(context.Background(), g, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	n, err := g.FindNode(context.Background(), "platform:v1.1.5")
	if err != nil || n == nil {
		t.Error("expected platform:v1.1.5 node from release-index.json")
	}
}

// TestRepositoryManifestIndexer_ClassifiesCosmeticDrift verifies that packages
// with changed_in_release=false are indexed (non-changed packages still get artifact nodes).
func TestRepositoryManifestIndexer_ClassifiesCosmeticDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-index.json"), []byte(sampleReleaseIndex), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := openPkgGraph(t)
	if err := packages.Extract(context.Background(), g, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// minio has changed_in_release=false — it should still get an artifact node.
	n, err := g.FindNode(context.Background(), "artifact:minio@1.2.20")
	if err != nil || n == nil {
		t.Error("expected artifact:minio@1.2.20 node for unchanged package")
	}
}

// TestRepositoryManifestIndexer_ClassifiesDangerousDrift verifies that packages
// with changed_in_release=true are indexed with change metadata.
func TestRepositoryManifestIndexer_ClassifiesDangerousDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-index.json"), []byte(sampleReleaseIndex), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := openPkgGraph(t)
	if err := packages.Extract(context.Background(), g, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// workflow has changed_in_release=true — it should have an artifact node.
	n, err := g.FindNode(context.Background(), "artifact:workflow@1.0.93")
	if err != nil || n == nil {
		t.Error("expected artifact:workflow@1.0.93 node for changed package")
		return
	}

	// Verify the changed_in_release metadata is stored.
	if n.Metadata == nil {
		t.Error("expected artifact node to have metadata")
		return
	}
	changed, _ := n.Metadata["changed_in_release"].(bool)
	if !changed {
		t.Error("expected changed_in_release=true in artifact metadata")
	}
}

// TestRepositoryManifestIndexer_EmitsArtifactNodes verifies that each package
// in release-index.json produces an artifact node with build_id metadata.
func TestRepositoryManifestIndexer_EmitsArtifactNodes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-index.json"), []byte(sampleReleaseIndex), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g := openPkgGraph(t)
	if err := packages.Extract(context.Background(), g, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, artifactID := range []string{"artifact:workflow@1.0.93", "artifact:minio@1.2.20"} {
		n, err := g.FindNode(context.Background(), artifactID)
		if err != nil || n == nil {
			t.Errorf("expected artifact node %q", artifactID)
		}
	}
}

// TestRepositoryManifestIndexer_SkipsMissingManifest verifies that Extract
// completes without error when no release-index.json is present.
func TestRepositoryManifestIndexer_SkipsMissingManifest(t *testing.T) {
	dir := t.TempDir() // no release-index.json

	g := openPkgGraph(t)
	if err := packages.Extract(context.Background(), g, dir); err != nil {
		t.Fatalf("Extract should not fail when release-index.json is missing: %v", err)
	}
}

package enforce_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

func openIdentityGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func TestIdentityNormalization_PackagePrefix(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"minio", "package:minio"},
		{"workflow-service", "package:workflow-service"},
		{"package:minio", "package:minio"}, // already canonical
		{"package:already", "package:already"},
	}
	for _, c := range cases {
		got := enforce.NormalizeID(enforce.TierPackageSpec, c.raw)
		if got != c.want {
			t.Errorf("NormalizeID(TierPackageSpec, %q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestIdentityNormalization_UnitPrefix(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"globular-minio.service", "unit:globular-minio.service"},
		{"globular-workflow.service", "unit:globular-workflow.service"},
		// Without .service suffix, adds it
		{"globular-minio", "unit:globular-minio.service"},
		// Already canonical
		{"unit:globular-minio.service", "unit:globular-minio.service"},
	}
	for _, c := range cases {
		got := enforce.NormalizeID(enforce.TierSystemdRuntime, c.raw)
		if got != c.want {
			t.Errorf("NormalizeID(TierSystemdRuntime, %q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestIdentityNormalization_ArtifactPrefix(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"minio@1.2.20", "artifact:minio@1.2.20"},
		{"workflow-service@0.9.1", "artifact:workflow-service@0.9.1"},
		{"minio", "artifact:minio"},
		// Already canonical
		{"artifact:minio@1.2.20", "artifact:minio@1.2.20"},
	}
	for _, c := range cases {
		got := enforce.NormalizeID(enforce.TierRepositoryManifest, c.raw)
		if got != c.want {
			t.Errorf("NormalizeID(TierRepositoryManifest, %q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestIdentityNormalization_AliasResolution(t *testing.T) {
	// ResolveNode should find a node by raw name even if stored with canonical prefix.
	g := openIdentityGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{ID: "package:minio", Type: "package", Name: "minio"})

	// Resolve by canonical ID — direct hit.
	n, err := enforce.ResolveNode(ctx, g, "package:minio")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if n == nil || n.ID != "package:minio" {
		t.Errorf("expected package:minio, got %v", n)
	}

	// Resolve by bare name — should find via candidate expansion.
	n2, err := enforce.ResolveNode(ctx, g, "minio")
	if err != nil {
		t.Fatalf("ResolveNode bare: %v", err)
	}
	if n2 == nil || n2.ID != "package:minio" {
		t.Errorf("expected package:minio via bare name, got %v", n2)
	}
}

func TestIdentityNormalization_CrossLayerJoin(t *testing.T) {
	// NormalizeID must produce the same canonical ID when called from different tiers
	// for node types that must match across layers (e.g. a unit present in both
	// package_spec and systemd_runtime should share the same unit: prefixed ID).
	g := openIdentityGraph(t)
	ctx := context.Background()

	// Package spec emits a unit node for globular-minio.service.
	specID := enforce.NormalizeID(enforce.TierPackageSpec, "globular-minio.service")
	// systemd runtime also emits a node for the same unit.
	runtimeID := enforce.NormalizeID(enforce.TierSystemdRuntime, "globular-minio.service")

	if specID != runtimeID {
		t.Errorf("cross-layer ID mismatch: spec=%q runtime=%q", specID, runtimeID)
	}

	// Store one node, resolve via either ID — same node.
	_ = g.AddNode(ctx, graph.Node{ID: specID, Type: "systemd_unit", Name: "globular-minio.service"})

	n, err := enforce.ResolveNode(ctx, g, runtimeID)
	if err != nil {
		t.Fatalf("ResolveNode cross-layer: %v", err)
	}
	if n == nil || n.ID != specID {
		t.Errorf("expected %q, got %v", specID, n)
	}
}

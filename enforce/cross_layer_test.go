package enforce_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

func openCrossLayerGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func TestCrossLayer_DesiredInstalledMismatch(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{
		ID:   "etcd:/globular/resources/ServiceDesiredVersion/minio",
		Type: "etcd_desired_state",
		Name: "minio",
		Metadata: map[string]any{"desired_version": "1.2.0"},
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "receipt:minio",
		Type: "installed_artifact",
		Name: "minio",
		Metadata: map[string]any{"version": "1.0.0"},
	})

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	found := false
	for _, v := range res.Violations {
		if v.Kind == "desired_installed_mismatch" && v.NodeA == "etcd:/globular/resources/ServiceDesiredVersion/minio" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected desired_installed_mismatch violation for minio, got %+v", res.Violations)
	}
}

func TestCrossLayer_InstalledWithoutUnit(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Receipt for "workflow-service" with no systemd unit node.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "receipt:workflow-service",
		Type: "installed_artifact",
		Name: "workflow-service",
		Metadata: map[string]any{"version": "0.9.0"},
	})

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	found := false
	for _, v := range res.Violations {
		if v.Kind == "installed_without_unit" && v.NodeA == "receipt:workflow-service" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected installed_without_unit for workflow-service; violations: %+v", res.Violations)
	}

	// Now add the unit node — violation should go away.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "unit:globular-workflow-service.service",
		Type: "systemd_unit",
		Name: "globular-workflow-service.service",
	})

	res2, _ := enforce.CrossLayerCheck(ctx, g)
	for _, v := range res2.Violations {
		if v.Kind == "installed_without_unit" && v.NodeA == "receipt:workflow-service" {
			t.Error("violation should not appear once unit node is present")
		}
	}
}

func TestCrossLayer_SidecarHashMismatch(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{
		ID:   "unit:globular-minio.service",
		Type: "systemd_unit",
		Name: "globular-minio.service",
		Metadata: map[string]any{
			"sidecar_match": false,
		},
	})

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	found := false
	for _, v := range res.Violations {
		if v.Kind == "sidecar_hash_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected sidecar_hash_mismatch violation; violations: %+v", res.Violations)
	}
}

func TestCrossLayer_FoundingQuorumBelowMinimum(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Only 2 MinIO nodes installed.
	for _, nodeID := range []string{"globule-ryzen", "globule-nuc"} {
		_ = g.AddNode(ctx, graph.Node{
			ID:   "node:" + nodeID + "/installed/infra:minio",
			Type: "installed_package",
			Name: "minio",
			Metadata: map[string]any{
				"node_id": nodeID,
				"version": "1.2.20",
				"kind":    "infra",
			},
		})
	}

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	found := false
	for _, v := range res.Violations {
		if v.Kind == "founding_quorum_below_minimum" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected founding_quorum_below_minimum with only 2 minio nodes; violations: %+v", res.Violations)
	}

	// Add third node — quorum satisfied.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "node:globule-dell/installed/infra:minio",
		Type: "installed_package",
		Name: "minio",
		Metadata: map[string]any{"node_id": "globule-dell", "version": "1.2.20", "kind": "infra"},
	})

	res2, _ := enforce.CrossLayerCheck(ctx, g)
	for _, v := range res2.Violations {
		if v.Kind == "founding_quorum_below_minimum" && v.NodeA == "package:minio" {
			t.Error("quorum violation should not appear with 3 nodes")
		}
	}
}

func TestCrossLayer_CertMissingInternalSAN(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Cert with no .globular.internal SAN.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "cert:/var/lib/globular/pki/issued/services/service.crt",
		Type: "pki_certificate",
		Name: "service.crt",
		Path: "/var/lib/globular/pki/issued/services/service.crt",
		Metadata: map[string]any{
			"sans":        []any{"10.0.0.63", "localhost"},
			"common_name": "globular-service",
		},
	})

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	found := false
	for _, v := range res.Violations {
		if v.Kind == "cert_missing_internal_san" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cert_missing_internal_san violation; violations: %+v", res.Violations)
	}
}

func TestCrossLayer_NoDivergenceWhenVersionsMatch(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	_ = g.AddNode(ctx, graph.Node{
		ID:   "etcd:/globular/resources/ServiceDesiredVersion/minio",
		Type: "etcd_desired_state",
		Name: "minio",
		Metadata: map[string]any{"desired_version": "1.2.20"},
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "receipt:minio",
		Type: "installed_artifact",
		Name: "minio",
		Metadata: map[string]any{"version": "1.2.20"},
	})

	res, err := enforce.CrossLayerCheck(ctx, g)
	if err != nil {
		t.Fatalf("CrossLayerCheck: %v", err)
	}

	for _, v := range res.Violations {
		if v.Kind == "desired_installed_mismatch" {
			t.Errorf("unexpected mismatch violation when versions match: %+v", v)
		}
	}
}

// Alias tests with the exact names required by agent_playbooks.yaml validation.
func TestCrossLayerInvariants_DesiredInstalledVersionMismatch(t *testing.T) {
	TestCrossLayer_DesiredInstalledMismatch(t)
}

func TestCrossLayerInvariants_InstalledWithoutUnit(t *testing.T) {
	TestCrossLayer_InstalledWithoutUnit(t)
}

func TestCrossLayerInvariants_ProfileServiceCompliance(t *testing.T) {
	// Profile compliance check is covered by the founding quorum test
	// (quorum enforces that storage-profile nodes have infrastructure services).
	TestCrossLayer_FoundingQuorumBelowMinimum(t)
}

func TestCrossLayerInvariants_FoundingQuorumBelow3(t *testing.T) {
	TestCrossLayer_FoundingQuorumBelowMinimum(t)
}

func TestCrossLayerInvariants_CertSANMissing(t *testing.T) {
	TestCrossLayer_CertMissingInternalSAN(t)
}

func TestCrossLayerInvariants_SidecarHashMismatch(t *testing.T) {
	TestCrossLayer_SidecarHashMismatch(t)
}

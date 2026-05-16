package manual_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

func openGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// writeYAML writes content to a temp file and returns its path.
func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return p
}

// Test 3: manual invariant YAML loads objectstore.topology_contract.
func TestLoadInvariantsObjectstoreTopologyContract(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "invariants.yaml", `
invariants:
  - id: objectstore.topology_contract
    title: MinIO topology contract
    severity: critical
    status: active
    summary: MinIO may only run on nodes present in ObjectStoreDesiredState.
    protects:
      state:
        - /globular/objectstore/config
      systemd_units:
        - globular-minio.service
    forbidden_fixes:
      - start_minio_from_local_health_check
    required_tests:
      - TestMinioHeldWhenNodeNotInPool
`)

	if err := manual.LoadInvariants(ctx, g, filepath.Join(dir, "invariants.yaml")); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// Invariant node exists.
	node, err := g.FindNode(ctx, "invariant:objectstore.topology_contract")
	if err != nil {
		t.Fatal(err)
	}
	if node == nil {
		t.Fatal("invariant node not created")
	}
	if node.Type != graph.NodeTypeInvariant {
		t.Errorf("wrong type: %s", node.Type)
	}

	// Invariant record exists.
	invs, err := g.AllInvariants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, inv := range invs {
		if inv.ID == "objectstore.topology_contract" {
			found = true
			if inv.Severity != "critical" {
				t.Errorf("severity: got %s, want critical", inv.Severity)
			}
		}
	}
	if !found {
		t.Error("objectstore.topology_contract not in invariants table")
	}

	// Etcd key node exists.
	stateNode, err := g.FindNode(ctx, "etcd_key:/globular/objectstore/config")
	if err != nil {
		t.Fatal(err)
	}
	if stateNode == nil {
		t.Error("etcd_key node not created for protected state")
	}

	// Systemd unit node exists.
	unitNode, err := g.FindNode(ctx, "systemd_unit:globular-minio.service")
	if err != nil {
		t.Fatal(err)
	}
	if unitNode == nil {
		t.Error("systemd_unit node not created")
	}
}

// Test 4: forbidden fixes become forbidden_fix nodes and forbids edges.
func TestLoadInvariantsForbiddenFixesBecomNodesAndEdges(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "invariants.yaml", `
invariants:
  - id: convergence.no_infinite_retry
    title: No infinite deterministic retry
    severity: critical
    status: active
    summary: Deterministic failures must not retry forever.
    forbidden_fixes:
      - blind_reconcile_retry
      - treating_all_failures_as_transient
`)

	if err := manual.LoadInvariants(ctx, g, filepath.Join(dir, "invariants.yaml")); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	fixes := []string{"blind_reconcile_retry", "treating_all_failures_as_transient"}
	for _, fix := range fixes {
		fixNode, err := g.FindNode(ctx, "forbidden_fix:"+fix)
		if err != nil {
			t.Fatal(err)
		}
		if fixNode == nil {
			t.Errorf("forbidden_fix node %q not created", fix)
			continue
		}
		if fixNode.Type != graph.NodeTypeForbiddenFix {
			t.Errorf("wrong type for %s: %s", fix, fixNode.Type)
		}
	}

	// forbids edges exist.
	edges, err := g.EdgesByKind(ctx, graph.EdgeForbids)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 forbids edges, got %d", len(edges))
	}
	for _, e := range edges {
		if e.Src != "invariant:convergence.no_infinite_retry" {
			t.Errorf("unexpected forbids src: %s", e.Src)
		}
	}
}

// Test: missing file is silently skipped.
func TestLoadInvariantsMissingFileSkipped(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	err := manual.LoadInvariants(ctx, g, "/nonexistent/path/invariants.yaml")
	if err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
}

// Test: LoadAll with full docs directory.
func TestLoadAll(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()

	dir := t.TempDir()
	writeYAML(t, dir, "invariants.yaml", `
invariants:
  - id: inv.one
    title: Invariant One
    severity: high
    status: active
    summary: Test invariant.
    forbidden_fixes:
      - do_the_wrong_thing
`)
	writeYAML(t, dir, "failure_modes.yaml", `
failure_modes:
  - id: fm.one
    title: Failure Mode One
    symptoms:
      - something broke
    root_cause: Bad code.
    architecture_fix: Fix it.
`)
	writeYAML(t, dir, "services.yaml", `
services:
  - id: my-service
    name: my-service
    summary: Test service.
    systemd_unit: my-service.service
`)

	if err := manual.LoadAll(ctx, g, dir); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	stats, err := g.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Invariants != 1 {
		t.Errorf("invariants: got %d, want 1", stats.Invariants)
	}
	if stats.FailureModes != 1 {
		t.Errorf("failure_modes: got %d, want 1", stats.FailureModes)
	}
	// Nodes: invariant + forbidden_fix + failure_mode + service + systemd_unit = 5
	if stats.Nodes < 4 {
		t.Errorf("nodes: got %d, want >= 4", stats.Nodes)
	}
}

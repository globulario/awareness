package goast_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/goast"
	"github.com/globulario/awareness/graph"
)

func openEtcdTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

func writeNamedGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeNamedGoFile: %v", err)
	}
}

// TestEtcdEvidence_GetEmitsReadsAuthority verifies that an etcd client.Get() call
// in a function body produces a reads_authority edge from the symbol to an authority node.
func TestEtcdEvidence_GetEmitsReadsAuthority(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "auth.go", `package auth

func ReadKey(ctx context.Context, etcdClient KV) {
	resp, _ := etcdClient.Get(ctx, "/globular/auth/keys")
	_ = resp
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Find the symbol node.
	symNodes, err := g.FindNodesByType(ctx, graph.NodeTypeSymbol)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	var symID string
	for _, n := range symNodes {
		if n.Name == "ReadKey" {
			symID = n.ID
		}
	}
	if symID == "" {
		t.Fatal("ReadKey symbol not found")
	}

	outEdges, err := g.Neighbors(ctx, symID, "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range outEdges {
		if e.Kind == graph.EdgeReadsAuthority {
			found = true
		}
	}
	if !found {
		t.Error("expected reads_authority edge from ReadKey symbol")
	}
}

// TestEtcdEvidence_PutEmitsWritesState verifies that an etcd client.Put() call
// produces a writes_state edge.
func TestEtcdEvidence_PutEmitsWritesState(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "state.go", `package state

func WriteConfig(ctx context.Context, etcdKv KV) {
	etcdKv.Put(ctx, "/globular/config/key", "value")
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	symNodes, err := g.FindNodesByType(ctx, graph.NodeTypeSymbol)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	var symID string
	for _, n := range symNodes {
		if n.Name == "WriteConfig" {
			symID = n.ID
		}
	}
	if symID == "" {
		t.Fatal("WriteConfig symbol not found")
	}

	outEdges, err := g.Neighbors(ctx, symID, "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range outEdges {
		if e.Kind == graph.EdgeWritesState {
			found = true
		}
	}
	if !found {
		t.Error("expected writes_state edge from WriteConfig symbol")
	}
}

// TestEtcdEvidence_TxnEmitsGuardsAction verifies that etcd txn.Commit() produces
// a guards_action edge.
func TestEtcdEvidence_TxnEmitsGuardsAction(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "txn.go", `package txn

func CommitState(ctx context.Context, etcdKv KV) {
	txn := etcdKv.Txn(ctx)
	txn.Commit(ctx)
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	symNodes, err := g.FindNodesByType(ctx, graph.NodeTypeSymbol)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	var symID string
	for _, n := range symNodes {
		if n.Name == "CommitState" {
			symID = n.ID
		}
	}
	if symID == "" {
		t.Fatal("CommitState symbol not found")
	}

	outEdges, err := g.Neighbors(ctx, symID, "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range outEdges {
		if e.Kind == graph.EdgeGuardsAction {
			found = true
		}
	}
	if !found {
		t.Error("expected guards_action edge from CommitState symbol")
	}
}

// TestEtcdEvidence_NonEtcdReceiverIgnored verifies that calls on non-etcd receivers
// do not produce reads_authority edges (scope guard).
func TestEtcdEvidence_NonEtcdReceiverIgnored(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "httpget.go", `package httpget

import "net/http"

func FetchURL(req *http.Request) {
	req.Get(nil)
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	symNodes, err := g.FindNodesByType(ctx, graph.NodeTypeSymbol)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	for _, n := range symNodes {
		if n.Name == "FetchURL" {
			outEdges, _ := g.Neighbors(ctx, n.ID, "out")
			for _, e := range outEdges {
				if e.Kind == graph.EdgeReadsAuthority {
					t.Error("unexpected reads_authority edge from http.Request.Get — scope guard should filter this")
				}
			}
		}
	}
}

// TestEtcdEvidence_StringKeyInAuthorityNode verifies that a string literal key
// from client.Get() ends up as the authority node name.
func TestEtcdEvidence_StringKeyInAuthorityNode(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "keycheck.go", `package keycheck

func CheckKey(ctx context.Context, etcdCli KV) {
	etcdCli.Get(ctx, "/globular/nodes/status")
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Verify the authority node was created with the key literal.
	n, err := g.FindNode(ctx, "authority:/globular/nodes/status")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if n == nil {
		t.Error("expected authority:/globular/nodes/status node to be created from string literal key")
	}
}

// TestEtcdEvidence_NoEdgesOnMissingBody verifies that functions with no body
// (e.g., interface methods) don't panic and produce no edges.
func TestEtcdEvidence_NoEdgesOnMissingBody(t *testing.T) {
	dir := t.TempDir()
	writeNamedGoFile(t, dir, "iface.go", `package iface

type KV interface {
	Get(ctx interface{}, key string) (interface{}, error)
}
`)
	g := openEtcdTestGraph(t)
	ctx := context.Background()

	// Should not panic or return error.
	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}
}

package enforce_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// Test 5: Schema with producer and consumer → no findings.
func TestValidateContractsComplete(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "hash_schema:my_hash", Type: graph.NodeTypeHashSchema, Name: "my_hash"})
	_ = g.AddNode(ctx, graph.Node{ID: "symbol:a.Producer", Type: graph.NodeTypeSymbol, Name: "Producer"})
	_ = g.AddNode(ctx, graph.Node{ID: "symbol:b.Consumer", Type: graph.NodeTypeSymbol, Name: "Consumer"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:a.Producer", Kind: graph.EdgeProduces, Dst: "hash_schema:my_hash"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:b.Consumer", Kind: graph.EdgeRequires, Dst: "hash_schema:my_hash"})

	findings := enforce.ValidateContracts(ctx, g)
	if len(findings) != 0 {
		t.Errorf("expected no findings for complete schema, got: %v", findings)
	}
}

// Test 6: Schema with producer but no consumer → WARNING.
func TestValidateContractsMissingConsumer(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "hash_schema:orphan_hash", Type: graph.NodeTypeHashSchema, Name: "orphan_hash"})
	_ = g.AddNode(ctx, graph.Node{ID: "symbol:a.Producer", Type: graph.NodeTypeSymbol, Name: "Producer"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:a.Producer", Kind: graph.EdgeProduces, Dst: "hash_schema:orphan_hash"})

	findings := enforce.ValidateContracts(ctx, g)
	found := false
	for _, f := range findings {
		if f.Code == enforce.CodeHashSchemaNoConsumer && f.Severity == enforce.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s WARNING, got: %v", enforce.CodeHashSchemaNoConsumer, findings)
	}
}

// Test 7: Schema with consumer but no producer → ERROR.
func TestValidateContractsMissingProducer(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	_ = g.AddNode(ctx, graph.Node{ID: "hash_schema:broken_hash", Type: graph.NodeTypeHashSchema, Name: "broken_hash"})
	_ = g.AddNode(ctx, graph.Node{ID: "symbol:b.Consumer", Type: graph.NodeTypeSymbol, Name: "Consumer"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "symbol:b.Consumer", Kind: graph.EdgeRequires, Dst: "hash_schema:broken_hash"})

	findings := enforce.ValidateContracts(ctx, g)
	found := false
	for _, f := range findings {
		if f.Code == enforce.CodeHashSchemaNoProducer && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s ERROR, got: %v", enforce.CodeHashSchemaNoProducer, findings)
	}
}

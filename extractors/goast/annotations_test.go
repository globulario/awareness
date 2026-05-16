package goast_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/goast"
	"github.com/globulario/awareness/graph"
)

// openMemGraph opens an in-memory awareness graph for a test.
func openMemGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// writeGoFile writes a valid Go source file with the given body into dir and
// returns the relative path from dir to the file (always "pkg/file.go").
func writeGoFile(t *testing.T, dir, body string) string {
	t.Helper()
	pkgDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(pkgDir, "file.go")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filepath.Join("pkg", "file.go")
}

// Test 1: Go extractor parses globular:enforces on a function.
func TestGoExtractorParsesEnforcesAnnotation(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// DoSomething implements a critical step.
//globular:enforces my.invariant
func DoSomething() {}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Symbol node must exist.
	sym, err := g.FindNode(ctx, "symbol:pkg.DoSomething")
	if err != nil || sym == nil {
		t.Fatalf("symbol node not found: err=%v", err)
	}

	// invariant:my.invariant node must exist.
	invNode, err := g.FindNode(ctx, "invariant:my.invariant")
	if err != nil || invNode == nil {
		t.Fatalf("invariant node not found: err=%v", err)
	}

	// enforces edge must exist from symbol to invariant.
	edges, err := g.EdgesByKind(ctx, graph.EdgeEnforces)
	if err != nil {
		t.Fatalf("EdgesByKind: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.DoSomething" && e.Dst == "invariant:my.invariant" {
			found = true
			if !e.Required {
				t.Error("enforces edge must have Required=true")
			}
		}
	}
	if !found {
		t.Error("enforces edge from symbol:pkg.DoSomething to invariant:my.invariant not found")
	}
}

// Test 2: Go extractor parses globular:hash_schema (producer side).
func TestGoExtractorParsesHashSchema(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// ComputeHash computes the desired hash.
//globular:hash_schema release_desired_hash
func ComputeHash() string { return "" }
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// hash_schema node must exist.
	schemaNode, err := g.FindNode(ctx, "hash_schema:release_desired_hash")
	if err != nil || schemaNode == nil {
		t.Fatalf("hash_schema node not found: err=%v", err)
	}
	if schemaNode.Type != graph.NodeTypeHashSchema {
		t.Errorf("wrong type: got %s, want %s", schemaNode.Type, graph.NodeTypeHashSchema)
	}

	// produces edge must exist.
	edges, err := g.EdgesByKind(ctx, graph.EdgeProduces)
	if err != nil {
		t.Fatalf("EdgesByKind produces: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.ComputeHash" && e.Dst == "hash_schema:release_desired_hash" {
			found = true
		}
	}
	if !found {
		t.Error("produces edge from ComputeHash to hash_schema:release_desired_hash not found")
	}
}

// Test 3: Go extractor links a symbol to a forbidden fix.
func TestGoExtractorLinksSymbolToForbiddenFix(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// BadApproach implements a convergence shortcut.
//globular:forbids use_raw_digest_as_hash
func BadApproach() {}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	fixNode, err := g.FindNode(ctx, "forbidden_fix:use_raw_digest_as_hash")
	if err != nil || fixNode == nil {
		t.Fatalf("forbidden_fix node not found: err=%v", err)
	}
	if fixNode.Type != graph.NodeTypeForbiddenFix {
		t.Errorf("wrong type: got %s, want %s", fixNode.Type, graph.NodeTypeForbiddenFix)
	}

	edges, err := g.EdgesByKind(ctx, graph.EdgeForbids)
	if err != nil {
		t.Fatalf("EdgesByKind forbids: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.BadApproach" && e.Dst == "forbidden_fix:use_raw_digest_as_hash" {
			found = true
			if !e.Required {
				t.Error("forbids edge must have Required=true")
			}
		}
	}
	if !found {
		t.Error("forbids edge from BadApproach to forbidden_fix:use_raw_digest_as_hash not found")
	}
}

// Test 6: Hash schema mismatch fixture — producer and consumer both link through
// the same hash_schema node, making the contract queryable from the graph.
func TestHashSchemaMismatchLinksProducerAndConsumer(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	// Producer file.
	producerDir := filepath.Join(dir, "producer")
	if err := os.MkdirAll(producerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	producerSrc := `package producer

// ComputeInfraHash is the canonical hash producer.
//globular:hash_schema infra_desired_hash
//globular:enforces infra.desired_hash_consistency
func ComputeInfraHash() string { return "" }
`
	if err := os.WriteFile(filepath.Join(producerDir, "hash.go"), []byte(producerSrc), 0o644); err != nil {
		t.Fatalf("write producer: %v", err)
	}

	// Consumer file.
	consumerDir := filepath.Join(dir, "consumer")
	if err := os.MkdirAll(consumerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	consumerSrc := `package consumer

// ClassifyConvergence uses the infra hash to check convergence.
//globular:expects_hash_schema infra_desired_hash
//globular:enforces infra.desired_hash_consistency
func ClassifyConvergence() bool { return true }
`
	if err := os.WriteFile(filepath.Join(consumerDir, "conv.go"), []byte(consumerSrc), 0o644); err != nil {
		t.Fatalf("write consumer: %v", err)
	}

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Both should be connected to hash_schema:infra_desired_hash.
	schemaNode, err := g.FindNode(ctx, "hash_schema:infra_desired_hash")
	if err != nil || schemaNode == nil {
		t.Fatalf("hash_schema:infra_desired_hash not found: err=%v", err)
	}

	// Producer has EdgeProduces to the schema.
	producerEdges, _ := g.EdgesByKind(ctx, graph.EdgeProduces)
	hasProducer := false
	for _, e := range producerEdges {
		if e.Dst == "hash_schema:infra_desired_hash" {
			hasProducer = true
		}
	}
	if !hasProducer {
		t.Error("no produces edge to hash_schema:infra_desired_hash (producer not linked)")
	}

	// Consumer has EdgeRequires to the schema.
	requiresEdges, _ := g.EdgesByKind(ctx, graph.EdgeRequires)
	hasConsumer := false
	for _, e := range requiresEdges {
		if e.Dst == "hash_schema:infra_desired_hash" {
			hasConsumer = true
		}
	}
	if !hasConsumer {
		t.Error("no requires edge to hash_schema:infra_desired_hash (consumer not linked)")
	}
}

// Test: protects annotation creates EdgeProtects.
func TestGoExtractorParsesProtectsAnnotation(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// WriteState guards the etcd write path.
//globular:protects install.result.atomic_commit
func WriteState() {}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	edges, err := g.EdgesByKind(ctx, graph.EdgeProtects)
	if err != nil {
		t.Fatalf("EdgesByKind: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.WriteState" && e.Dst == "invariant:install.result.atomic_commit" {
			found = true
		}
	}
	if !found {
		t.Error("protects edge not found")
	}
}

// Test: state_transition annotation creates a state_transition node.
func TestGoExtractorParsesStateTransition(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// Commit advances installed->converged.
//globular:state_transition INSTALLED -> CONVERGED
func Commit() {}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Expect a state_transition node whose name contains both states.
	stNodes, err := g.FindNodesByType(ctx, graph.NodeTypeStateTransition)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	found := false
	for _, n := range stNodes {
		if n.Name == "INSTALLED -> CONVERGED" {
			found = true
		}
	}
	if !found {
		t.Errorf("state_transition node 'INSTALLED -> CONVERGED' not found, got: %v", stNodes)
	}
}

// Test: tested_by annotation creates a test node and EdgeTestedBy.
func TestGoExtractorParsesTestedBy(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// CriticalFunc does something critical.
//globular:tested_by TestCriticalFuncBehavior
func CriticalFunc() {}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	testNode, err := g.FindNode(ctx, "test:TestCriticalFuncBehavior")
	if err != nil || testNode == nil {
		t.Fatalf("test node not found: err=%v", err)
	}

	edges, err := g.EdgesByKind(ctx, graph.EdgeTestedBy)
	if err != nil {
		t.Fatalf("EdgesByKind tested_by: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.CriticalFunc" && e.Dst == "test:TestCriticalFuncBehavior" {
			found = true
		}
	}
	if !found {
		t.Error("tested_by edge not found")
	}
}

// Test: struct type annotations are processed.
func TestGoExtractorParsesStructAnnotations(t *testing.T) {
	g := openMemGraph(t)
	ctx := context.Background()
	dir := t.TempDir()

	writeGoFile(t, dir, `package pkg

// ConvergenceState holds state machine data.
//globular:enforces install.result.atomic_commit
type ConvergenceState struct {
	Version string
}
`)

	if err := goast.Extract(ctx, g, dir, dir); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	edges, err := g.EdgesByKind(ctx, graph.EdgeEnforces)
	if err != nil {
		t.Fatalf("EdgesByKind: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Src == "symbol:pkg.ConvergenceState" && e.Dst == "invariant:install.result.atomic_commit" {
			found = true
		}
	}
	if !found {
		t.Error("enforces edge from struct not found")
	}
}

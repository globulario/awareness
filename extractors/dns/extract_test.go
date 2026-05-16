package dns_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/dns"
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

func realDocsDir() string {
	// Package lives at golang/awareness/extractors/dns/ — 4 levels up to repo root.
	return filepath.Join("..", "..", "..", "..", "docs", "awareness")
}

// TestDNSExtract_EmptyDirSkipped verifies that an empty docsAwarenessDir causes
// Extract to return a skipped CollectorHealth without error.
func TestDNSExtract_EmptyDirSkipped(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	h, err := dns.Extract(ctx, g, "")
	if err != nil {
		t.Errorf("expected no error for empty dir, got: %v", err)
	}
	if h.Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", h.Status)
	}
}

// TestDNSExtract_MissingFileSkipped verifies that a valid dir without
// dns_zones.yaml returns a skipped collector health.
func TestDNSExtract_MissingFileSkipped(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	h, err := dns.Extract(ctx, g, t.TempDir())
	if err != nil {
		t.Errorf("expected no error when YAML missing, got: %v", err)
	}
	if h.Status != "skipped" {
		t.Errorf("expected status=skipped, got %q", h.Status)
	}
}

// TestDNSExtract_LoadsZoneNodes verifies that Extract produces dns_zone nodes
// for each zone defined in dns_zones.yaml.
func TestDNSExtract_LoadsZoneNodes(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	h, err := dns.Extract(ctx, g, realDocsDir())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if h.Status != "ok" {
		t.Fatalf("expected status=ok, got %q (error: %s)", h.Status, h.Error)
	}
	if h.NodesEmitted == 0 {
		t.Fatal("expected nodes emitted > 0")
	}

	// Verify the internal zone node exists.
	zoneNode, err := g.FindNode(ctx, "dns_zone:globular.internal")
	if err != nil {
		t.Fatalf("FindNode dns_zone:globular.internal: %v", err)
	}
	if zoneNode == nil {
		t.Fatal("expected dns_zone:globular.internal node, got nil")
	}
	if zoneNode.Type != graph.NodeTypeDNSZone {
		t.Errorf("expected NodeTypeDNSZone, got %q", zoneNode.Type)
	}
}

// TestDNSExtract_ServiceEndpointCoveredByCert verifies that service_endpoint
// nodes are linked to their certificate via EdgeServiceEndpointCoveredByCert.
func TestDNSExtract_ServiceEndpointCoveredByCert(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if _, err := dns.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	epID := "service_endpoint:cluster_controller.globular.internal"
	epNode, err := g.FindNode(ctx, epID)
	if err != nil {
		t.Fatalf("FindNode %s: %v", epID, err)
	}
	if epNode == nil {
		t.Fatalf("service_endpoint:cluster_controller.globular.internal not found")
	}

	// Check outbound edges for EdgeServiceEndpointCoveredByCert.
	edges, err := g.Neighbors(ctx, epID, "outbound")
	if err != nil {
		t.Fatalf("Neighbors(%s): %v", epID, err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeServiceEndpointCoveredByCert {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EdgeServiceEndpointCoveredByCert edge from %s, not found", epID)
	}
}

// TestDNSExtract_DomainSpecDeclaresRecord verifies that domain_spec nodes
// emit EdgeDomainSpecDeclaresRecord edges to their dns_record nodes.
func TestDNSExtract_DomainSpecDeclaresRecord(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if _, err := dns.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	dsID := "domain_spec:globular.internal"
	dsNode, err := g.FindNode(ctx, dsID)
	if err != nil {
		t.Fatalf("FindNode %s: %v", dsID, err)
	}
	if dsNode == nil {
		t.Fatalf("domain_spec:globular.internal not found")
	}

	edges, err := g.Neighbors(ctx, dsID, "outbound")
	if err != nil {
		t.Fatalf("Neighbors(%s): %v", dsID, err)
	}
	var declaredRecords int
	for _, e := range edges {
		if e.Kind == graph.EdgeDomainSpecDeclaresRecord {
			declaredRecords++
		}
	}
	if declaredRecords == 0 {
		t.Errorf("expected at least one EdgeDomainSpecDeclaresRecord from %s", dsID)
	}
}

// TestDNSExtract_RecordRisksInvariant verifies that dns_record nodes that list
// risks_invariants emit EdgeDNSRecordRisksInvariant edges.
func TestDNSExtract_RecordRisksInvariant(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if _, err := dns.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// globular.internal A record risks recovery.must_not_depend_on_dns_only.
	recID := "dns_record:globular.internal"
	recNode, err := g.FindNode(ctx, recID)
	if err != nil {
		t.Fatalf("FindNode %s: %v", recID, err)
	}
	if recNode == nil {
		t.Fatalf("dns_record:globular.internal not found")
	}

	edges, err := g.Neighbors(ctx, recID, "outbound")
	if err != nil {
		t.Fatalf("Neighbors(%s): %v", recID, err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeDNSRecordRisksInvariant &&
			e.Dst == "invariant:recovery.must_not_depend_on_dns_only" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EdgeDNSRecordRisksInvariant → invariant:recovery.must_not_depend_on_dns_only from %s", recID)
	}
}

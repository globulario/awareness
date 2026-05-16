package metrics_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/metrics"
	"github.com/globulario/awareness/graph"
)

// openGraph opens a fresh in-memory awareness graph for testing.
func openGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// realDocsDir returns the docs/awareness directory relative to this package.
// The metrics package lives at golang/awareness/extractors/metrics/ so we go
// up four levels to reach the repo root, then down into docs/awareness.
func realDocsDir() string {
	return filepath.Join("..", "..", "..", "..", "docs", "awareness")
}

// TestMetricKnowledgeIndexer_LoadsMetricQueries verifies that the extractor
// creates at least 5 metric_query nodes from the real metric_queries.yaml.
func TestMetricKnowledgeIndexer_LoadsMetricQueries(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeMetricQuery)
	if err != nil {
		t.Fatalf("FindNodesByType: %v", err)
	}
	if len(nodes) < 5 {
		t.Errorf("expected at least 5 metric_query nodes, got %d", len(nodes))
	}
}

// TestMetricKnowledgeIndexer_LoadsThresholds verifies that specific threshold
// nodes are created from metric_thresholds.yaml.
func TestMetricKnowledgeIndexer_LoadsThresholds(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	required := []string{
		"metric_threshold:etcd:disk_percent",
		"metric_threshold:scylla:disk_percent",
		"metric_threshold:minio:offline_disks",
	}
	for _, id := range required {
		n, err := g.FindNode(ctx, id)
		if err != nil {
			t.Errorf("FindNode(%s): %v", id, err)
			continue
		}
		if n == nil {
			t.Errorf("expected node %s to exist", id)
		}
	}
}

// TestMetricKnowledgeIndexer_LinksEtcdDiskToFailureMode verifies that the
// metric_warning_rule:etcd_disk_percent node exists and has the correct type.
func TestMetricKnowledgeIndexer_LinksEtcdDiskToFailureMode(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	n, err := g.FindNode(ctx, "metric_warning_rule:etcd_disk_percent")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if n == nil {
		t.Fatal("metric_warning_rule:etcd_disk_percent not found")
	}
	if n.Type != graph.NodeTypeMetricWarningRule {
		t.Errorf("type = %q, want %q", n.Type, graph.NodeTypeMetricWarningRule)
	}

	// Verify the metadata carries the expected query_id.
	if n.Metadata == nil {
		t.Fatal("metadata is nil")
	}
	qid, _ := n.Metadata["query_id"].(string)
	if qid != "etcd_disk_percent" {
		t.Errorf("query_id = %q, want %q", qid, "etcd_disk_percent")
	}
}

// TestMetricWarning_LinksToInvariant verifies that at least one warning rule
// node has an EdgeMetricWarningRisksInvariant edge when the target invariant
// nodes are seeded first.
func TestMetricWarning_LinksToInvariant(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	// Seed invariant nodes that the warning mappings reference, so that
	// best-effort linkage can form edges.
	invariantIDs := []string{
		"objectstore.topology_contract",
		"reconcile.global_work_must_not_starve_completion",
		"service.endpoint.etcd_address_reachability",
		"scylla.critical_keyspace_replication_policy",
	}
	for _, id := range invariantIDs {
		if err := g.AddNode(ctx, graph.Node{
			ID:   "invariant:" + id,
			Type: graph.NodeTypeInvariant,
			Name: id,
		}); err != nil {
			t.Fatalf("seed invariant %s: %v", id, err)
		}
	}

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// At least one EdgeMetricWarningRisksInvariant edge must exist.
	edges, err := g.EdgesByKind(ctx, graph.EdgeMetricWarningRisksInvariant)
	if err != nil {
		t.Fatalf("EdgesByKind: %v", err)
	}
	if len(edges) == 0 {
		t.Error("expected at least one EdgeMetricWarningRisksInvariant edge, got 0")
	}
}

// TestMetricWarning_UsesServiceSpecificThreshold verifies that the etcd
// threshold uses the etcd-specific warn value (70) rather than the default (90).
func TestMetricWarning_UsesServiceSpecificThreshold(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	n, err := g.FindNode(ctx, "metric_threshold:etcd:disk_percent")
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if n == nil {
		t.Fatal("metric_threshold:etcd:disk_percent not found")
	}

	if n.Metadata == nil {
		t.Fatal("metadata is nil")
	}
	warn, ok := n.Metadata["warn"].(float64)
	if !ok {
		t.Fatalf("warn field missing or wrong type: %v", n.Metadata["warn"])
	}
	const wantWarn = 70.0
	if warn != wantWarn {
		t.Errorf("etcd disk_percent warn = %v, want %v (etcd-specific, not default 90)", warn, wantWarn)
	}

	// Also verify this is distinct from the default threshold.
	defNode, err := g.FindNode(ctx, "metric_threshold:default:disk_percent")
	if err != nil {
		t.Fatalf("FindNode default: %v", err)
	}
	if defNode == nil {
		t.Skip("default disk_percent threshold not present — YAML may have changed")
	}
	defWarn, _ := defNode.Metadata["warn"].(float64)
	if defWarn == warn {
		t.Errorf("etcd and default disk_percent both have warn=%v — etcd-specific override not applied", warn)
	}
}

// TestExtract_EmptyDirReturnsNil verifies that an empty docsAwarenessDir
// causes Extract to return nil without error.
func TestExtract_EmptyDirReturnsNil(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, ""); err != nil {
		t.Errorf("expected nil error for empty dir, got: %v", err)
	}
}

// TestExtract_MissingFilesSkippedGracefully verifies that a valid directory
// without the YAML files causes Extract to return nil.
func TestExtract_MissingFilesSkippedGracefully(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	if err := metrics.Extract(ctx, g, t.TempDir()); err != nil {
		t.Errorf("expected nil error when YAML files missing, got: %v", err)
	}
}

// TestMetricWarning_ProducesRecommendedDiagnostic verifies that when a
// decision_rule node already exists in the graph, Extract emits an
// EdgeMetricWarningTriggerRule edge from the metric_warning_rule to the
// decision_rule. This ensures the metrics-to-invariant decision linkage is
// wired at extraction time, not just defined in the mapping struct.
func TestMetricWarning_ProducesRecommendedDiagnostic(t *testing.T) {
	ctx := context.Background()
	g := openGraph(t)

	// Pre-seed the decision rule node that minio_offline_disks links to.
	drID := "decision_rule:minio_topology_requires_three_storage_nodes"
	if err := g.AddNode(ctx, graph.Node{
		ID:   drID,
		Type: graph.NodeTypeDesignRule,
		Name: "minio_topology_requires_three_storage_nodes",
	}); err != nil {
		t.Fatalf("AddNode decision_rule: %v", err)
	}

	if err := metrics.Extract(ctx, g, realDocsDir()); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Verify the metric_warning_rule for minio_offline_disks was created.
	ruleID := "metric_warning_rule:minio_offline_disks"
	ruleNode, err := g.FindNode(ctx, ruleID)
	if err != nil {
		t.Fatalf("FindNode %s: %v", ruleID, err)
	}
	if ruleNode == nil {
		t.Fatalf("metric_warning_rule:minio_offline_disks not found — check YAML mappings")
	}

	// Verify EdgeMetricWarningTriggerRule edge exists from rule → decision rule.
	edges, err := g.Neighbors(ctx, ruleID, "outbound")
	if err != nil {
		t.Fatalf("Neighbors(%s): %v", ruleID, err)
	}
	found := false
	for _, e := range edges {
		if e.Dst == drID && e.Kind == graph.EdgeMetricWarningTriggerRule {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EdgeMetricWarningTriggerRule edge from %s → %s, not found in outbound edges", ruleID, drID)
	}

	// Verify metadata records the linked decision rule count.
	linkedDRs, _ := ruleNode.Metadata["linked_drs"].(float64)
	if linkedDRs < 1 {
		t.Errorf("expected linked_drs >= 1 in node metadata, got %v", ruleNode.Metadata["linked_drs"])
	}
}

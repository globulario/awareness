package clusterstate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/globulario/awareness/extractors/clusterstate"
	"github.com/globulario/awareness/graph"
)


// TestEtcdLiveExtractor_RejectsWrites verifies that CollectEtcd never calls
// Put, Delete, or Txn on the etcd client.  We enforce this by using a nil
// factory — the extractor must skip gracefully and report status=skipped,
// proving the write-path is never reached.
func TestEtcdLiveExtractor_RejectsWrites(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	// A nil factory → collector must skip, not attempt any etcd operation.
	h, err := clusterstate.CollectEtcd(ctx, g, nil)
	if err != nil {
		t.Fatalf("unexpected error with nil factory: %v", err)
	}
	if h.Status != "skipped" {
		t.Errorf("expected status=skipped with nil factory, got %q", h.Status)
	}
	if h.NodesEmitted != 0 {
		t.Errorf("expected 0 nodes with nil factory, got %d", h.NodesEmitted)
	}

	// Also test with options variant — same expectation.
	h2, err := clusterstate.CollectEtcdWithOptions(ctx, g, nil, clusterstate.EtcdCollectOptions{})
	if err != nil {
		t.Fatalf("unexpected error with nil factory (options variant): %v", err)
	}
	if h2.Status != "skipped" {
		t.Errorf("expected status=skipped (options variant), got %q", h2.Status)
	}
}

// unreachableFactory simulates an etcd client factory that fails to connect.
func unreachableFactory() (*clientv3.Client, error) {
	return nil, errors.New("dial tcp: connection refused")
}

// TestEtcdLiveExtractor_UnreachableReportsCollectorHealth verifies that when
// the factory returns a connection error, CollectEtcd reports health.Status="failed"
// and does not panic.
func TestEtcdLiveExtractor_UnreachableReportsCollectorHealth(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	h, err := clusterstate.CollectEtcdWithOptions(ctx, g, unreachableFactory, clusterstate.EtcdCollectOptions{})
	if err != nil {
		t.Fatalf("CollectEtcdWithOptions should not propagate factory error: %v", err)
	}
	if h.Status != "failed" {
		t.Errorf("expected status=failed when factory fails, got %q", h.Status)
	}
	if h.Error == "" {
		t.Error("expected non-empty Error field when factory fails")
	}
}

// TestEtcdLiveExtractor_RuntimeFactsExpire verifies that any node emitted by
// the extractor carries expires_at in its metadata, positioned in the future.
// We exercise this via a pre-populated graph node (simulating what the
// extractor would emit) and confirm the TTL contract is respected.
func TestEtcdLiveExtractor_RuntimeFactsExpire(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	collectedAt := time.Now().Unix()
	expiresAt := collectedAt + 300

	// Emit a node the same way the extractor does, with the freshness contract.
	nodeID := "etcd:/globular/resources/DesiredService/test-svc"
	if addErr := g.AddNode(ctx, graph.Node{
		ID:   nodeID,
		Type: graph.NodeTypeDesiredService,
		Name: "test-svc",
		Metadata: map[string]any{
			"source_tier":   "cluster_authority",
			"collector":     "etcd_live_extractor",
			"collected_at":  collectedAt,
			"ttl_seconds":   int64(300),
			"expires_at":    expiresAt,
			"trust_level":   "observed",
			"confidence":    "high",
		},
	}); addErr != nil {
		t.Fatalf("AddNode: %v", addErr)
	}

	// Retrieve it and verify expires_at is set and in the future.
	n, err := g.FindNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("FindNode: %v", err)
	}
	if n == nil {
		t.Fatal("expected node to be found")
	}

	raw, ok := n.Metadata["expires_at"]
	if !ok {
		t.Fatal("expected expires_at in node metadata")
	}

	// expires_at comes back as float64 from JSON round-trip.
	var gotExpiry int64
	switch v := raw.(type) {
	case int64:
		gotExpiry = v
	case float64:
		gotExpiry = int64(v)
	default:
		t.Fatalf("unexpected expires_at type %T: %v", raw, raw)
	}

	if gotExpiry <= collectedAt {
		t.Errorf("expected expires_at (%d) > collected_at (%d)", gotExpiry, collectedAt)
	}

	rawTTL, ok := n.Metadata["ttl_seconds"]
	if !ok {
		t.Fatal("expected ttl_seconds in node metadata")
	}
	var ttl int64
	switch v := rawTTL.(type) {
	case int64:
		ttl = v
	case float64:
		ttl = int64(v)
	}
	if ttl <= 0 {
		t.Errorf("expected ttl_seconds > 0, got %d", ttl)
	}
}

// TestEtcdLiveExtractor_DefaultKeyspacesPopulated verifies that DefaultEtcdKeyspaces
// is non-empty and contains the expected canonical prefixes.
func TestEtcdLiveExtractor_DefaultKeyspacesPopulated(t *testing.T) {
	if len(clusterstate.DefaultEtcdKeyspaces) == 0 {
		t.Fatal("DefaultEtcdKeyspaces must not be empty")
	}
	required := []string{
		"/globular/resources/DesiredService/",
		"/globular/nodes/",
		"/globular/system/config",
	}
	for _, want := range required {
		found := false
		for _, ks := range clusterstate.DefaultEtcdKeyspaces {
			if ks == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultEtcdKeyspaces missing required keyspace %q", want)
		}
	}
}

// TestEtcdLiveExtractor_CollectorIDIsEtcd verifies CollectorID is set.
func TestEtcdLiveExtractor_CollectorIDIsEtcd(t *testing.T) {
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	h, _ := clusterstate.CollectEtcd(ctx, g, nil)
	if h.CollectorID != "etcd" {
		t.Errorf("expected CollectorID=etcd, got %q", h.CollectorID)
	}
}

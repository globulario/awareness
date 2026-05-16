package bundlesync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── Phase C.3 discovery tests ─────────────────────────────────────────────────
//
// Coverage:
//   1. Empty inputs → empty result, no error.
//   2. Missing expected release fields → error.
//   3. Local cache match → highest priority, kind=local_cache.
//   4. Local cache version mismatch → not returned.
//   5. Local cache missing manifest → not returned.
//   6. Gateway URL → returned at gateway priority.
//   7. Peers filtered by build_id (only matching included).
//   8. Peers ordered by LastSeen descending.
//   9. Local node excluded from peers.
//   10. Cluster ID filter applied.
//   11. MaxAge filter excludes stale peers.
//   12. RequireRunningStatus excludes non-RUNNING peers.
//   13. Empty PeerURL excluded.
//   14. AwarenessBundleVersion mismatch excluded when published.
//   15. Combined: local + gateway + peers all returned in correct rank order.

// fakeRegistry is an in-memory NodeRegistry for tests.
type fakeRegistry struct {
	entries []NodeRegistryEntry
	err     error
}

func (f *fakeRegistry) ListNodes(ctx context.Context) ([]NodeRegistryEntry, error) {
	return f.entries, f.err
}

func defaultExpected() ReleaseIndex {
	return ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
}

func writeLocalCache(t *testing.T, dir string, m Manifest) string {
	t.Helper()
	curDir := filepath.Join(dir, "current")
	if err := os.MkdirAll(curDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(curDir, "manifest.json"), mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

// 1. Empty inputs return empty result, no error.
func TestDiscoverSourcesEmptyInputs(t *testing.T) {
	out, err := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: defaultExpected(),
	}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected 0 candidates, got %d: %+v", len(out), out)
	}
}

// 2. Missing ExpectedRelease fields → error.
func TestDiscoverSourcesRequiresExpectedRelease(t *testing.T) {
	_, err := DiscoverSources(context.Background(), DiscoveryOptions{}, nil)
	if err == nil {
		t.Fatal("expected error when ExpectedRelease is empty")
	}
	_, err = DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: ReleaseIndex{Version: "v1"}, // build_id missing
	}, nil)
	if err == nil {
		t.Fatal("expected error when build_id is empty")
	}
}

// 3. Local cache matches expected → highest-priority candidate.
func TestDiscoverSourcesLocalCacheMatches(t *testing.T) {
	bundleRoot := t.TempDir()
	exp := defaultExpected()
	writeLocalCache(t, bundleRoot, Manifest{
		Name: BundleName, Version: exp.Version, BuildID: exp.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	})

	out, err := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		LocalBundleDir:  bundleRoot,
	}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(out), out)
	}
	if out[0].Kind != SourceKindLocalCache {
		t.Errorf("kind=%s, want local_cache", out[0].Kind)
	}
	if out[0].Priority != priorityLocalCache {
		t.Errorf("priority=%d, want %d", out[0].Priority, priorityLocalCache)
	}
}

// 4. Local cache exists but version differs → not returned.
func TestDiscoverSourcesLocalCacheVersionMismatchNotReturned(t *testing.T) {
	bundleRoot := t.TempDir()
	writeLocalCache(t, bundleRoot, Manifest{
		Name: BundleName, Version: "v0.0.1", BuildID: "old",
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	})

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: defaultExpected(),
		LocalBundleDir:  bundleRoot,
	}, nil)
	for _, c := range out {
		if c.Kind == SourceKindLocalCache {
			t.Errorf("local_cache should not be returned for mismatched version")
		}
	}
}

// 5. Local bundle dir set but no manifest file → not returned.
func TestDiscoverSourcesLocalCacheMissingManifest(t *testing.T) {
	bundleRoot := t.TempDir() // empty
	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: defaultExpected(),
		LocalBundleDir:  bundleRoot,
	}, nil)
	for _, c := range out {
		if c.Kind == SourceKindLocalCache {
			t.Errorf("local_cache must not appear when current/manifest.json is missing")
		}
	}
}

// 6. Gateway URL returns a single SourceKindGateway candidate.
func TestDiscoverSourcesGateway(t *testing.T) {
	out, err := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: defaultExpected(),
		GatewayURL:      "https://gateway:10260",
	}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(out))
	}
	if out[0].Kind != SourceKindGateway {
		t.Errorf("kind=%s, want gateway", out[0].Kind)
	}
	if out[0].PeerURL != "https://gateway:10260" {
		t.Errorf("PeerURL=%s, want https://gateway:10260", out[0].PeerURL)
	}
}

// 7. Peers filtered by build_id — only matching included.
func TestDiscoverSourcesPeersFilteredByBuildID(t *testing.T) {
	exp := defaultExpected()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "match-1", PeerURL: "https://m1:10260", BuildID: exp.BuildID, LastSeen: now.Add(-1 * time.Minute)},
		{NodeID: "stale-build", PeerURL: "https://s1:10260", BuildID: "old999", LastSeen: now.Add(-1 * time.Minute)},
		{NodeID: "match-2", PeerURL: "https://m2:10260", BuildID: exp.BuildID, LastSeen: now.Add(-2 * time.Minute)},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		Now:             now,
	}, registry)

	if len(out) != 2 {
		t.Fatalf("expected 2 peers (matching build_id only), got %d: %+v", len(out), out)
	}
	for _, c := range out {
		if c.NodeID == "stale-build" {
			t.Errorf("stale-build peer should be excluded")
		}
	}
}

// 8. Peers ordered by LastSeen descending (most recent first).
func TestDiscoverSourcesPeersSortedByRecency(t *testing.T) {
	exp := defaultExpected()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "old", PeerURL: "https://old:10260", BuildID: exp.BuildID, LastSeen: now.Add(-10 * time.Minute)},
		{NodeID: "new", PeerURL: "https://new:10260", BuildID: exp.BuildID, LastSeen: now.Add(-1 * time.Minute)},
		{NodeID: "mid", PeerURL: "https://mid:10260", BuildID: exp.BuildID, LastSeen: now.Add(-5 * time.Minute)},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		Now:             now,
	}, registry)

	if len(out) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(out))
	}
	wantOrder := []string{"new", "mid", "old"}
	for i, want := range wantOrder {
		if out[i].NodeID != want {
			t.Errorf("position %d: got %s, want %s", i, out[i].NodeID, want)
		}
	}
}

// 9. Local node ID excluded.
func TestDiscoverSourcesExcludesLocalNode(t *testing.T) {
	exp := defaultExpected()
	now := time.Now()
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "self", PeerURL: "https://self:10260", BuildID: exp.BuildID, LastSeen: now},
		{NodeID: "peer", PeerURL: "https://peer:10260", BuildID: exp.BuildID, LastSeen: now},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		LocalNodeID:     "self",
	}, registry)

	if len(out) != 1 {
		t.Fatalf("expected 1 peer (local excluded), got %d", len(out))
	}
	if out[0].NodeID != "peer" {
		t.Errorf("got %s, want peer", out[0].NodeID)
	}
}

// 10. Cluster ID filter — peers from other clusters excluded.
func TestDiscoverSourcesFiltersByClusterID(t *testing.T) {
	exp := defaultExpected()
	now := time.Now()
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "same-cluster", PeerURL: "https://a:10260", BuildID: exp.BuildID, ClusterID: "cluster-a", LastSeen: now},
		{NodeID: "other-cluster", PeerURL: "https://b:10260", BuildID: exp.BuildID, ClusterID: "cluster-b", LastSeen: now},
		{NodeID: "no-cluster", PeerURL: "https://c:10260", BuildID: exp.BuildID, ClusterID: "", LastSeen: now},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		ClusterID:       "cluster-a",
	}, registry)

	if len(out) != 1 {
		t.Fatalf("expected 1 peer, got %d: %+v", len(out), out)
	}
	if out[0].NodeID != "same-cluster" {
		t.Errorf("got %s, want same-cluster", out[0].NodeID)
	}
}

// 11. MaxAge excludes peers older than the threshold.
func TestDiscoverSourcesMaxAgeFilter(t *testing.T) {
	exp := defaultExpected()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "fresh", PeerURL: "https://fresh:10260", BuildID: exp.BuildID, LastSeen: now.Add(-30 * time.Second)},
		{NodeID: "old", PeerURL: "https://old:10260", BuildID: exp.BuildID, LastSeen: now.Add(-10 * time.Minute)},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		MaxAge:          2 * time.Minute,
		Now:             now,
	}, registry)

	if len(out) != 1 {
		t.Fatalf("expected 1 peer (fresh only), got %d: %+v", len(out), out)
	}
	if out[0].NodeID != "fresh" {
		t.Errorf("got %s, want fresh", out[0].NodeID)
	}
}

// 12. RequireRunningStatus excludes non-RUNNING peers.
func TestDiscoverSourcesRequireRunningStatus(t *testing.T) {
	exp := defaultExpected()
	now := time.Now()
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "running", PeerURL: "https://r:10260", BuildID: exp.BuildID, Status: "RUNNING", LastSeen: now},
		{NodeID: "draining", PeerURL: "https://d:10260", BuildID: exp.BuildID, Status: "DRAINING", LastSeen: now},
		{NodeID: "no-status", PeerURL: "https://n:10260", BuildID: exp.BuildID, Status: "", LastSeen: now},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease:      exp,
		RequireRunningStatus: true,
	}, registry)

	// running + no-status (empty Status is tolerated). Draining excluded.
	gotIDs := map[string]bool{}
	for _, c := range out {
		gotIDs[c.NodeID] = true
	}
	if !gotIDs["running"] {
		t.Error("expected running peer to be included")
	}
	if gotIDs["draining"] {
		t.Error("expected draining peer to be excluded")
	}
	if !gotIDs["no-status"] {
		t.Error("expected no-status peer to be tolerated (registry may not write Status)")
	}
}

// 13. Empty PeerURL excludes the entry — un-pullable.
func TestDiscoverSourcesEmptyPeerURLExcluded(t *testing.T) {
	exp := defaultExpected()
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "good", PeerURL: "https://good:10260", BuildID: exp.BuildID},
		{NodeID: "no-url", PeerURL: "", BuildID: exp.BuildID},
	}}
	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
	}, registry)
	if len(out) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(out))
	}
	if out[0].NodeID != "good" {
		t.Errorf("got %s, want good", out[0].NodeID)
	}
}

// 14. Peer publishes a different awareness_bundle_version → excluded.
// (When the field is empty, the peer is tolerated — older publishers might not
// populate it.)
func TestDiscoverSourcesExcludesAwarenessBundleVersionMismatch(t *testing.T) {
	exp := defaultExpected()
	now := time.Now()
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "exact", PeerURL: "https://e:10260", BuildID: exp.BuildID, AwarenessBundleVersion: exp.Version, LastSeen: now},
		{NodeID: "wrong-bundle", PeerURL: "https://w:10260", BuildID: exp.BuildID, AwarenessBundleVersion: "v0.0.9", LastSeen: now},
		{NodeID: "no-bundle-info", PeerURL: "https://n:10260", BuildID: exp.BuildID, AwarenessBundleVersion: "", LastSeen: now},
	}}
	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
	}, registry)

	gotIDs := map[string]bool{}
	for _, c := range out {
		gotIDs[c.NodeID] = true
	}
	if !gotIDs["exact"] {
		t.Error("exact-bundle peer should be included")
	}
	if gotIDs["wrong-bundle"] {
		t.Error("peer with wrong AwarenessBundleVersion must be excluded")
	}
	if !gotIDs["no-bundle-info"] {
		t.Error("peer without AwarenessBundleVersion should be tolerated")
	}
}

// 15. Combined: local cache + gateway + peers — all returned, correct rank order.
func TestDiscoverSourcesCombinedRankOrder(t *testing.T) {
	exp := defaultExpected()
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	bundleRoot := t.TempDir()
	writeLocalCache(t, bundleRoot, Manifest{
		Name: BundleName, Version: exp.Version, BuildID: exp.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	})

	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{NodeID: "peer-recent", PeerURL: "https://r:10260", BuildID: exp.BuildID, LastSeen: now.Add(-1 * time.Minute)},
		{NodeID: "peer-older", PeerURL: "https://o:10260", BuildID: exp.BuildID, LastSeen: now.Add(-5 * time.Minute)},
	}}

	out, _ := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		LocalBundleDir:  bundleRoot,
		GatewayURL:      "https://gateway:10260",
		Now:             now,
	}, registry)

	if len(out) != 4 {
		t.Fatalf("expected 4 candidates, got %d: %+v", len(out), out)
	}
	wantKinds := []SourceKind{
		SourceKindLocalCache,
		SourceKindGateway,
		SourceKindPeer,
		SourceKindPeer,
	}
	for i, want := range wantKinds {
		if out[i].Kind != want {
			t.Errorf("position %d: kind=%s, want %s", i, out[i].Kind, want)
		}
	}
	// Within peers: peer-recent before peer-older.
	if out[2].NodeID != "peer-recent" {
		t.Errorf("first peer = %s, want peer-recent", out[2].NodeID)
	}
	if out[3].NodeID != "peer-older" {
		t.Errorf("second peer = %s, want peer-older", out[3].NodeID)
	}
}

// 16. Registry error degrades gracefully — local + gateway still returned.
func TestDiscoverSourcesRegistryErrorDoesNotFailDiscovery(t *testing.T) {
	exp := defaultExpected()
	bundleRoot := t.TempDir()
	writeLocalCache(t, bundleRoot, Manifest{
		Name: BundleName, Version: exp.Version, BuildID: exp.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	})

	registry := &fakeRegistry{err: errors.New("etcd down")}
	out, err := DiscoverSources(context.Background(), DiscoveryOptions{
		ExpectedRelease: exp,
		LocalBundleDir:  bundleRoot,
		GatewayURL:      "https://gateway:10260",
	}, registry)
	if err != nil {
		t.Fatalf("DiscoverSources should not fail when registry errors; got %v", err)
	}
	// Local cache + gateway should both come back.
	kinds := map[SourceKind]bool{}
	for _, c := range out {
		kinds[c.Kind] = true
	}
	if !kinds[SourceKindLocalCache] {
		t.Error("local_cache missing despite registry error")
	}
	if !kinds[SourceKindGateway] {
		t.Error("gateway missing despite registry error")
	}
}

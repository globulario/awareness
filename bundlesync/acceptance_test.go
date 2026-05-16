package bundlesync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Phase C.6: full acceptance suite ─────────────────────────────────────────
//
// Each test maps to a numbered scenario from the auto-bundle-sync spec. They
// are end-to-end: a real httptest TLS server stands in for the peer MCP, the
// real PullBundle runs, the real InstallBundle runs, the real symlink moves
// (or doesn't). No stubs except the peer registry, which has no real cluster
// to read from in tests.
//
// Tests assert whole-system behavior — what an operator running the released
// orchestrator on a node would actually observe.

// acceptancePeer wraps an httptest.NewTLSServer that mimics the Phase-B serve
// surface: GET /awareness/manifest → JSON, GET /awareness/bundle → octet-stream.
type acceptancePeer struct {
	server *httptest.Server
	bundle []byte
	man    Manifest

	// Per-test forced statuses (0 = serve normally).
	manifestStatus int
	bundleStatus   int
}

func newAcceptancePeer(t *testing.T, version, buildID string) *acceptancePeer {
	t.Helper()

	bundle, manifest := makeAcceptanceBundle(t, version, buildID)
	p := &acceptancePeer{bundle: bundle, man: manifest}

	mux := http.NewServeMux()
	mux.HandleFunc("/awareness/manifest", func(w http.ResponseWriter, r *http.Request) {
		if p.manifestStatus != 0 {
			http.Error(w, "test forced", p.manifestStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.man)
	})
	mux.HandleFunc("/awareness/bundle", func(w http.ResponseWriter, r *http.Request) {
		if p.bundleStatus != 0 {
			http.Error(w, "test forced", p.bundleStatus)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(p.bundle)
	})

	p.server = httptest.NewTLSServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *acceptancePeer) trustedPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.server.Certificate())
	return pool
}

func (p *acceptancePeer) registryEntry(now time.Time) NodeRegistryEntry {
	return NodeRegistryEntry{
		NodeID:                 "peer-acceptance",
		PeerURL:                p.server.URL,
		BuildID:                p.man.BuildID,
		AwarenessBundleVersion: p.man.Version,
		LastSeen:               now,
		Status:                 "RUNNING",
	}
}

// makeAcceptanceBundle builds a (gzip)tar with graph.db + a doc file, and the
// matching manifest. Returns the raw bundle bytes + manifest.
func makeAcceptanceBundle(t *testing.T, version, buildID string) ([]byte, Manifest) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	graph := []byte("graph for " + version + "/" + buildID)
	hdr := &tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(graph)), Typeflag: tar.TypeReg}
	tw.WriteHeader(hdr)
	tw.Write(graph)

	doc := []byte("# README\n")
	tw.WriteHeader(&tar.Header{Name: "docs/", Mode: 0755, Typeflag: tar.TypeDir})
	tw.WriteHeader(&tar.Header{Name: "docs/README.md", Mode: 0644, Size: int64(len(doc)), Typeflag: tar.TypeReg})
	tw.Write(doc)

	tw.Close()
	gz.Close()
	data := buf.Bytes()

	h := sha256.Sum256(data)
	m := Manifest{
		Name:          BundleName,
		Version:       version,
		BuildID:       buildID,
		SchemaVersion: "awareness.bundle.v1",
		SHA256:        hex.EncodeToString(h[:]),
		SizeBytes:     int64(len(data)),
		GraphHash:     "deadbeef",
		SourceCommit:  "git-acceptance",
	}
	return data, m
}

// preStageCurrentBundle places a fully-installed bundle under bundleRoot at
// version/buildID and points current → it. Returns the versioned dir and
// graph content so callers can prove the symlink/file weren't touched.
func preStageCurrentBundle(t *testing.T, bundleRoot, version, buildID, sentinel string) (string, []byte) {
	t.Helper()
	versionedDir := filepath.Join(bundleRoot, "installed", version, buildID)
	if err := os.MkdirAll(versionedDir, 0755); err != nil {
		t.Fatalf("mkdir versioned: %v", err)
	}
	graphContent := []byte(sentinel)
	if err := os.WriteFile(filepath.Join(versionedDir, "graph.db"), graphContent, 0644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	m := Manifest{
		Name: BundleName, Version: version, BuildID: buildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(versionedDir, "manifest.json"), mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.Symlink(versionedDir, filepath.Join(bundleRoot, "current")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return versionedDir, graphContent
}

// ── Spec test 1: missing bundle, auto-sync from trusted peer ─────────────────

func TestSpec1_MissingBundleAutoSyncFromTrustedPeer(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	peer := newAcceptancePeer(t, ri.Version, ri.BuildID)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	res, err := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("ensure error: %v reason=%s", err, res.Reason)
	}
	if !res.OK || res.State != StateAwarenessReady {
		t.Fatalf("expected AWARENESS_READY; got state=%s reason=%s", res.State, res.Reason)
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.Kind != SourceKindPeer {
		t.Errorf("expected installed_from.kind=peer_mcp; got %+v", res.InstalledFrom)
	}

	// current symlink must point at the new versioned dir.
	want := filepath.Join(bundleRoot, "installed", ri.Version, ri.BuildID)
	target, err := os.Readlink(filepath.Join(bundleRoot, "current"))
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	if target != want {
		t.Errorf("current → %s, want %s", target, want)
	}

	// graph.db is on disk after install.
	if _, err := os.Stat(filepath.Join(want, "graph.db")); err != nil {
		t.Errorf("graph.db missing after install: %v", err)
	}
}

// ── Spec test 2: peer serves wrong build_id; install must not happen ─────────

func TestSpec2_WrongBuildIDRejected(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	// Peer serves the WRONG build_id. Note: peer's registry entry advertises
	// the *correct* build_id (so discovery doesn't filter it out), but the
	// peer's actual served manifest carries old999 — exactly the lie the
	// puller must catch.
	peer := newAcceptancePeer(t, ri.Version, "old999")
	registry := &fakeRegistry{entries: []NodeRegistryEntry{
		{
			NodeID:                 "peer-liar",
			PeerURL:                peer.server.URL,
			BuildID:                ri.BuildID, // pretends to have the right build
			AwarenessBundleVersion: ri.Version,
			LastSeen:               now,
			Status:                 "RUNNING",
		},
	}}

	res, _ := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		Now:           now,
	})

	if res.OK {
		t.Fatal("expected OK=false for wrong build_id")
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SOURCE_UNAVAILABLE (after one rejected source)", res.State)
	}
	// Per-attempt detail must show the rejection reason was a build_id mismatch.
	if len(res.SourceTried) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(res.SourceTried))
	}
	att := res.SourceTried[0]
	if att.State != StateAwarenessBundleStale {
		t.Errorf("attempt state=%s, want AWARENESS_BUNDLE_STALE", att.State)
	}

	// current symlink must not exist.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current symlink must not exist after rejection")
	}
	// No versioned dir got created either.
	if _, err := os.Stat(filepath.Join(bundleRoot, "installed", ri.Version, ri.BuildID)); err == nil {
		t.Error("versioned dir must not exist after rejection")
	}
}

// ── Spec test 3: untrusted TLS — peer not in cluster CA pool ─────────────────

func TestSpec3_UntrustedTLSSourceRejected(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	peer := newAcceptancePeer(t, ri.Version, ri.BuildID)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	// EMPTY pool — peer's cert is not trusted.
	emptyPool := x509.NewCertPool()

	res, _ := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: emptyPool,
		Registry:      registry,
		Now:           now,
	})

	if res.OK {
		t.Fatal("OK=true despite untrusted TLS")
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want SOURCE_UNAVAILABLE", res.State)
	}

	// Attempt detail must show TLS rejection.
	if len(res.SourceTried) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(res.SourceTried))
	}
	att := res.SourceTried[0]
	if att.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("attempt state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", att.State)
	}
	if !strings.Contains(att.Err+att.Reason, "tls") &&
		!strings.Contains(att.Err+att.Reason, "certificate") &&
		!strings.Contains(att.Err+att.Reason, "x509") {
		t.Errorf("expected TLS-related rejection reason; got reason=%q err=%q", att.Reason, att.Err)
	}

	// Current symlink must not exist.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current must not exist after untrusted TLS rejection")
	}
}

// ── Spec test 4: "repository unavailable, peer available" ────────────────────
//
// Our orchestrator never knows about the repository — the source priority is
// local cache → gateway → peers (per Dave's preference, dropping the repository
// step). So this test is the symmetric proof: with NO local cache and NO
// gateway, a single trusted peer is enough to reach AWARENESS_READY.

func TestSpec4_PeerAvailableEvenWithoutOtherSources(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	peer := newAcceptancePeer(t, ri.Version, ri.BuildID)
	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	res, err := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		// No GatewayURL, no LocalBundleDir cache (bundleRoot is empty).
		Now: now,
	})
	if err != nil {
		t.Fatalf("ensure error: %v", err)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.Kind != SourceKindPeer {
		t.Errorf("expected source=peer_mcp; got %+v", res.InstalledFrom)
	}
}

// ── Spec test 5: no sources available; node stays MISSING, no retry storm ───

func TestSpec5_NoSourceAvailable(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	// Empty registry, no gateway, no local cache.
	emptyRegistry := &fakeRegistry{entries: nil}

	res, _ := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: x509.NewCertPool(),
		Registry:      emptyRegistry,
		Now:           now,
	})

	if res.OK {
		t.Fatal("OK=true with no sources")
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want SOURCE_UNAVAILABLE", res.State)
	}
	// No attempts should have run — there were no candidates to walk.
	if len(res.SourceTried) != 0 {
		t.Errorf("expected 0 attempts, got %d", len(res.SourceTried))
	}

	// The retry loop must respect MaxAttempts and exit cleanly — proving "no
	// retry storm" is just "the loop terminates at the cap with the same
	// SOURCE_UNAVAILABLE verdict."
	policy := RetryPolicy{Schedule: []time.Duration{0, 1 * time.Millisecond, 1 * time.Millisecond}, MaxAttempts: 3}
	loopRes := EnsureLoop(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: x509.NewCertPool(),
		Registry:      emptyRegistry,
		Now:           now,
	}, policy)
	if loopRes == nil || loopRes.OK {
		t.Fatalf("loop unexpectedly OK: %+v", loopRes)
	}
	if loopRes.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("loop final state=%s, want SOURCE_UNAVAILABLE", loopRes.State)
	}
}

// ── Spec test 6: peer serves a tar with unsafe entries — refused ─────────────

func TestSpec6_UnsafeTarRejected(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	// Build a peer that serves a bundle with both ../escape AND graph.db so
	// the manifest sha256 line up with the served bytes — the unsafe-tar
	// rejection has to come from the safety scan, not the hash check.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("malicious")
	tw.WriteHeader(&tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	data := buf.Bytes()
	h := sha256.Sum256(data)
	m := Manifest{
		Name: BundleName, Version: ri.Version, BuildID: ri.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: hex.EncodeToString(h[:]),
		SizeBytes: int64(len(data)),
	}

	peer := &acceptancePeer{bundle: data, man: m}
	mux := http.NewServeMux()
	mux.HandleFunc("/awareness/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(peer.man)
	})
	mux.HandleFunc("/awareness/bundle", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(peer.bundle)
	})
	peer.server = httptest.NewTLSServer(mux)
	defer peer.server.Close()

	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	res, _ := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		Now:           now,
	})

	if res.OK {
		t.Fatal("OK=true despite unsafe tar")
	}
	att := res.SourceTried[0]
	if att.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("attempt state=%s, want VERIFY_FAILED", att.State)
	}
	// current symlink must not exist; no extracted files outside staging/temp.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current must not exist after unsafe tar")
	}
	// Defense-in-depth: nothing should appear at /tmp/escape, /escape, etc.
	for _, leak := range []string{filepath.Join(bundleRoot, "escape"), filepath.Join(filepath.Dir(bundleRoot), "escape")} {
		if _, err := os.Lstat(leak); err == nil {
			t.Errorf("unsafe tar leaked outside staging at %s", leak)
		}
	}
}

// ── Spec test 7: install is atomic — failure leaves prior current intact ─────

func TestSpec7_InstallIsAtomic(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.31", BuildID: "new-build"}
	now := time.Now().UTC()

	// Pre-stage a perfectly good "v1.2.30/old" bundle as the active install.
	oldDir, oldGraph := preStageCurrentBundle(t, bundleRoot, "v1.2.30", "old-build", "PRE-EXISTING ACTIVE GRAPH")

	// Peer serves a bundle with an unsafe tar entry for the NEW release.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("evil")
	tw.WriteHeader(&tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	data := buf.Bytes()
	h := sha256.Sum256(data)
	m := Manifest{
		Name: BundleName, Version: ri.Version, BuildID: ri.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: hex.EncodeToString(h[:]),
	}
	peer := &acceptancePeer{bundle: data, man: m}
	mux := http.NewServeMux()
	mux.HandleFunc("/awareness/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(peer.man)
	})
	mux.HandleFunc("/awareness/bundle", func(w http.ResponseWriter, r *http.Request) {
		w.Write(peer.bundle)
	})
	peer.server = httptest.NewTLSServer(mux)
	defer peer.server.Close()

	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	res, _ := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		Now:           now,
	})

	if res.OK {
		t.Fatal("OK=true despite unsafe tar — atomic install must refuse")
	}

	// PRIOR active install must still be intact.
	target, err := os.Readlink(filepath.Join(bundleRoot, "current"))
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	if target != oldDir {
		t.Errorf("current moved to %s, want %s (atomic install must not switch on failure)", target, oldDir)
	}
	gotGraph, err := os.ReadFile(filepath.Join(oldDir, "graph.db"))
	if err != nil {
		t.Fatalf("read prior graph: %v", err)
	}
	if !bytes.Equal(gotGraph, oldGraph) {
		t.Errorf("prior bundle graph.db modified")
	}

	// New versioned dir must NOT exist (extract failed before rename).
	newDir := filepath.Join(bundleRoot, "installed", ri.Version, ri.BuildID)
	if _, err := os.Stat(newDir); err == nil {
		t.Errorf("new versioned dir exists at %s; install must not partially succeed", newDir)
	}
}

// ── Spec test 8: orchestrator end-to-end with all sources (local + peer) ─────
//
// Bonus integration check: when local_cache already matches expected release,
// orchestrator returns AWARENESS_READY without touching the network — the
// peer fixture's count of pull calls must stay at zero.

func TestSpec8_LocalCacheShortCircuitsNetwork(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now().UTC()

	// Pre-stage a matching active install.
	preStageCurrentBundle(t, bundleRoot, ri.Version, ri.BuildID, "ACTIVE GRAPH")

	// Peer is reachable but should never be contacted.
	calls := 0
	peer := newAcceptancePeer(t, ri.Version, ri.BuildID)
	mux := http.NewServeMux()
	mux.HandleFunc("/awareness/manifest", func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(peer.man)
	})
	mux.HandleFunc("/awareness/bundle", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write(peer.bundle)
	})
	peer.server.Close()
	peer.server = httptest.NewTLSServer(mux)
	defer peer.server.Close()

	registry := &fakeRegistry{entries: []NodeRegistryEntry{peer.registryEntry(now)}}

	res, err := EnsureAwarenessBundle(context.Background(), EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: peer.trustedPool(),
		Registry:      registry,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.Kind != SourceKindLocalCache {
		t.Errorf("expected source=local_cache; got %+v", res.InstalledFrom)
	}
	if calls != 0 {
		t.Errorf("peer was contacted %d time(s); local_cache fast path must skip the network", calls)
	}
}

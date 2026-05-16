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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── Phase C.2 pull tests ──────────────────────────────────────────────────────
//
// Acceptance:
//   1. Happy path — peer serves matching bundle, verified TLS, sha256 matches.
//   2. Wrong build_id from peer → rejected with AWARENESS_BUNDLE_STALE,
//      no bundle written.
//   3. Wrong version from peer → rejected with AWARENESS_BUNDLE_MISMATCH.
//   4. Untrusted TLS (cert not in caller's pool) → rejected with TLS error,
//      no bundle written.
//   5. Manifest sha256 ≠ bundle bytes → rejected with VERIFY_FAILED.
//   6. Missing ClusterCAPool → rejected immediately, no network call.
//   7. Peer returns 404 for manifest → SOURCE_UNAVAILABLE / clean error.
//   8. Unsafe tar entry in downloaded bundle → rejected, bundle file removed.

// peerFixture wraps a configurable test peer that can serve different
// (manifest, bundle) shapes per case.
type peerFixture struct {
	server   *httptest.Server
	manifest Manifest
	bundle   []byte
	// statusCode overrides (0 = use 200). Per-path control.
	manifestStatus int
	bundleStatus   int
}

func newPeerFixture(t *testing.T, m Manifest, bundle []byte) *peerFixture {
	t.Helper()
	p := &peerFixture{manifest: m, bundle: bundle}

	mux := http.NewServeMux()
	mux.HandleFunc(pullManifestPath, func(w http.ResponseWriter, r *http.Request) {
		if p.manifestStatus != 0 {
			http.Error(w, "test forced status", p.manifestStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.manifest)
	})
	mux.HandleFunc(pullBundlePath, func(w http.ResponseWriter, r *http.Request) {
		if p.bundleStatus != 0 {
			http.Error(w, "test forced status", p.bundleStatus)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(p.bundle)
	})

	p.server = httptest.NewTLSServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

// poolFromServer returns a CertPool that trusts the test server's cert.
// Use this for VERIFIED-TLS happy paths.
func poolFromServer(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

// makeBundleAndManifest produces a (gzip+)tar bundle containing graph.db
// plus the matching manifest. version/buildID let tests diverge from the
// caller's expectations to exercise mismatch paths.
func makeBundleAndManifest(t *testing.T, version, buildID string) ([]byte, Manifest) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("fake graph.db for " + version + "/" + buildID)
	hdr := &tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	tw.Write(body)
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
	}
	return data, m
}

// 1. Happy path: matching peer, verified TLS.
func TestPullBundleHappyPath(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "abc123")
	peer := newPeerFixture(t, manifest, bundle)

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: manifest.Version,
		ExpectedBuildID: manifest.BuildID,
		ClusterCAPool:   poolFromServer(peer.server),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("pull failed: %v (state=%s reason=%s)", err, res.State, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if res.State != StateAwarenessReady {
		t.Errorf("state=%s, want AWARENESS_READY", res.State)
	}
	if res.TLSTrust != tlsTrustVerified {
		t.Errorf("TLSTrust=%s, want VERIFIED", res.TLSTrust)
	}
	if res.SHA256 != manifest.SHA256 {
		t.Errorf("SHA256=%s, want %s", res.SHA256, manifest.SHA256)
	}

	// Bundle and manifest sidecar both on disk and match peer bytes.
	bundleData, err := os.ReadFile(res.BundlePath)
	if err != nil {
		t.Fatalf("read pulled bundle: %v", err)
	}
	if !bytes.Equal(bundleData, bundle) {
		t.Errorf("pulled bundle bytes differ from peer's")
	}
	if _, err := os.Stat(res.ManifestPath); err != nil {
		t.Errorf("manifest sidecar missing: %v", err)
	}
}

// 2. Peer serves a bundle with a different build_id than caller expects →
// rejected with AWARENESS_BUNDLE_STALE (same version, different build_id).
func TestPullBundleRejectsWrongBuildID(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "old-build")
	peer := newPeerFixture(t, manifest, bundle)

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: "v1.2.30",
		ExpectedBuildID: "new-build", // different from peer
		ClusterCAPool:   poolFromServer(peer.server),
		Timeout:         5 * time.Second,
	})
	if res.OK {
		t.Fatal("OK=true despite build_id mismatch")
	}
	if err == nil {
		t.Error("err should be non-nil for build_id mismatch")
	}
	if res.State != StateAwarenessBundleStale {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_STALE", res.State)
	}

	// Output dir must not contain a bundle.
	if _, err := os.Stat(filepath.Join(out, "bundle.tar.gz")); err == nil {
		t.Error("bundle.tar.gz must not be written when build_id mismatches")
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err == nil {
		t.Error("manifest.json must not be written when build_id mismatches")
	}
}

// 3. Wrong VERSION → AWARENESS_BUNDLE_MISMATCH (different release entirely).
func TestPullBundleRejectsWrongVersion(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v0.0.1", "abc123")
	peer := newPeerFixture(t, manifest, bundle)

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: "v1.2.30",
		ExpectedBuildID: "abc123",
		ClusterCAPool:   poolFromServer(peer.server),
		Timeout:         5 * time.Second,
	})
	if res.OK {
		t.Fatal("OK=true despite version mismatch")
	}
	if err == nil {
		t.Error("err should be non-nil")
	}
	if res.State != StateAwarenessBundleMismatch {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_MISMATCH", res.State)
	}
}

// 4. Untrusted TLS — pool does NOT include peer's cert. Pull must refuse,
// no bundle written, error wraps ErrPeerTLSUnverified.
func TestPullBundleRejectsUntrustedTLS(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "abc123")
	peer := newPeerFixture(t, manifest, bundle)

	// Pool with NO certs trusts nothing.
	emptyPool := x509.NewCertPool()

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: manifest.Version,
		ExpectedBuildID: manifest.BuildID,
		ClusterCAPool:   emptyPool,
		Timeout:         5 * time.Second,
	})
	if res.OK {
		t.Fatal("OK=true despite untrusted TLS")
	}
	if err == nil {
		t.Error("err should be non-nil")
	}
	if !errors.Is(err, ErrPeerTLSUnverified) {
		t.Errorf("err=%v, want wraps ErrPeerTLSUnverified", err)
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}
	// No bundle artifacts left behind.
	if _, err := os.Stat(filepath.Join(out, "bundle.tar.gz")); err == nil {
		t.Error("bundle.tar.gz must not exist when TLS verification fails")
	}
}

// 5. Manifest sha256 ≠ actual bundle bytes → VERIFY_FAILED, no install.
func TestPullBundleRejectsSHA256Mismatch(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "abc123")
	// Tamper the manifest so it claims a different hash than what the bundle
	// actually has. The peer serves both — the receiver must catch the lie.
	manifest.SHA256 = "deadbeef00000000000000000000000000000000000000000000000000000000"
	peer := newPeerFixture(t, manifest, bundle)

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: manifest.Version,
		ExpectedBuildID: manifest.BuildID,
		ClusterCAPool:   poolFromServer(peer.server),
		Timeout:         5 * time.Second,
	})
	if res.OK {
		t.Fatal("OK=true despite sha256 mismatch")
	}
	if err == nil {
		t.Error("err should be non-nil")
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}
	if res.SHA256 == manifest.SHA256 {
		t.Errorf("res.SHA256 should be the actual hash (not the lying manifest's): got %s", res.SHA256)
	}
	if _, err := os.Stat(filepath.Join(out, "bundle.tar.gz")); err == nil {
		t.Error("bundle.tar.gz must be removed on sha256 mismatch")
	}
}

// 6. ClusterCAPool=nil is a programmer error: pull rejects without making
// any network call.
func TestPullBundleRequiresClusterCAPool(t *testing.T) {
	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         "https://192.0.2.1:10260", // RFC5737 doc IP, never reachable
		OutDir:          out,
		ExpectedVersion: "v1.2.30",
		ExpectedBuildID: "abc123",
		ClusterCAPool:   nil,
	})
	if res.OK {
		t.Fatal("OK=true with nil pool")
	}
	if !errors.Is(err, ErrPeerTLSUnverified) {
		t.Errorf("err=%v, want ErrPeerTLSUnverified", err)
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}
}

// 7. Peer returns 404 for manifest → SOURCE_UNAVAILABLE-equivalent.
// We don't know the peer's own state — could be cold-bootstrap or wrong host —
// so the puller maps a 404 to a clean error the orchestrator can route on.
func TestPullBundleRejectsManifestNotFound(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "abc123")
	peer := newPeerFixture(t, manifest, bundle)
	peer.manifestStatus = http.StatusNotFound

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: manifest.Version,
		ExpectedBuildID: manifest.BuildID,
		ClusterCAPool:   poolFromServer(peer.server),
	})
	if res.OK {
		t.Fatal("OK=true with peer 404")
	}
	if err == nil {
		t.Error("err should be non-nil")
	}
	// No bundle artifacts.
	if _, err := os.Stat(filepath.Join(out, "bundle.tar.gz")); err == nil {
		t.Error("bundle.tar.gz must not exist when manifest fetch fails")
	}
}

// 8. Peer serves a bundle that contains an unsafe tar entry. Sha256 matches
// (since we computed it on the actual peer bytes), so the rejection comes
// from ValidateTarSafe, not the hash check.
func TestPullBundleRejectsUnsafeTar(t *testing.T) {
	// Build a bundle with a path-traversal entry.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("evil")
	tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	data := buf.Bytes()

	h := sha256.Sum256(data)
	m := Manifest{
		Name:          BundleName,
		Version:       "v1.2.30",
		BuildID:       "abc123",
		SchemaVersion: "awareness.bundle.v1",
		SHA256:        hex.EncodeToString(h[:]),
		SizeBytes:     int64(len(data)),
	}
	peer := newPeerFixture(t, m, data)

	out := t.TempDir()
	res, err := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: m.Version,
		ExpectedBuildID: m.BuildID,
		ClusterCAPool:   poolFromServer(peer.server),
	})
	if res.OK {
		t.Fatal("OK=true despite unsafe tar")
	}
	if !errors.Is(err, ErrTarUnsafe) {
		t.Errorf("err=%v, want wraps ErrTarUnsafe", err)
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}
	// On unsafe tar rejection, bundle file must be removed.
	if _, err := os.Stat(filepath.Join(out, "bundle.tar.gz")); err == nil {
		t.Error("bundle.tar.gz must be removed when tar safety fails")
	}
}

// 9. Schema unsupported at the peer → SCHEMA_UNSUPPORTED, no install.
func TestPullBundleRejectsSchemaUnsupported(t *testing.T) {
	bundle, manifest := makeBundleAndManifest(t, "v1.2.30", "abc123")
	manifest.SchemaVersion = "awareness.bundle.v99"
	peer := newPeerFixture(t, manifest, bundle)

	out := t.TempDir()
	res, _ := PullBundle(context.Background(), PullOptions{
		PeerURL:         peer.server.URL,
		OutDir:          out,
		ExpectedVersion: manifest.Version,
		ExpectedBuildID: manifest.BuildID,
		ClusterCAPool:   poolFromServer(peer.server),
	})
	if res.OK {
		t.Fatal("OK=true despite schema unsupported")
	}
	if res.State != StateAwarenessBundleSchemaUnsupported {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED", res.State)
	}
}

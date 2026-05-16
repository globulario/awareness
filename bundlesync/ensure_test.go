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
	mathrand "math/rand"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// ── Phase C.4 orchestrator tests ──────────────────────────────────────────────
//
// 1. Local cache fast path → AWARENESS_READY without invoking PullFunc.
// 2. Single peer succeeds on first try.
// 3. First peer fails, second succeeds (sequential fallback).
// 4. All peers fail → AWARENESS_BUNDLE_SOURCE_UNAVAILABLE.
// 5. No candidates → AWARENESS_BUNDLE_SOURCE_UNAVAILABLE.
// 6. ReleaseIndex / BundleRoot validation rejects bad input.
// 7. EnsureLoop retries on failure and returns on first success.
// 8. EnsureLoop honours ctx cancellation mid-backoff.
// 9. EnsureLoop respects MaxAttempts.
// 10. scheduleGap clamps to last entry; jitter stays bounded.

// stubPullSuccess produces a PullResult that succeeds and writes a real
// bundle + sidecar manifest into outDir so InstallBundle can run.
func stubPullSuccess(t *testing.T, version, buildID string) func(context.Context, PullOptions) (*PullResult, error) {
	t.Helper()
	return func(ctx context.Context, opts PullOptions) (*PullResult, error) {
		// Reject if expected doesn't match — orchestrator must hand correct
		// values to the puller.
		if opts.ExpectedVersion != version || opts.ExpectedBuildID != buildID {
			return nil, errors.New("stub: unexpected version/build_id")
		}

		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		body := []byte("graph for " + version + "/" + buildID)
		hdr := &tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		tw.WriteHeader(hdr)
		tw.Write(body)
		tw.Close()
		gz.Close()
		data := buf.Bytes()

		bundlePath := filepath.Join(opts.OutDir, "bundle.tar.gz")
		if err := os.WriteFile(bundlePath, data, 0644); err != nil {
			return nil, err
		}
		h := sha256.Sum256(data)
		m := Manifest{
			Name:          BundleName,
			Version:       version,
			BuildID:       buildID,
			SchemaVersion: "awareness.bundle.v1",
			SHA256:        hex.EncodeToString(h[:]),
			SizeBytes:     int64(len(data)),
		}
		mb, _ := json.MarshalIndent(m, "", "  ")
		manifestPath := filepath.Join(opts.OutDir, "manifest.json")
		if err := os.WriteFile(manifestPath, mb, 0644); err != nil {
			return nil, err
		}
		return &PullResult{
			OK:           true,
			State:        StateAwarenessReady,
			BundlePath:   bundlePath,
			ManifestPath: manifestPath,
			PeerManifest: &m,
			TLSTrust:     tlsTrustVerified,
			SizeBytes:    int64(len(data)),
			SHA256:       m.SHA256,
		}, nil
	}
}

// stubPullFail returns a PullResult with a synthesised failure.
func stubPullFail(state State, reason string) func(context.Context, PullOptions) (*PullResult, error) {
	return func(ctx context.Context, opts PullOptions) (*PullResult, error) {
		return &PullResult{
			OK:     false,
			State:  state,
			Reason: reason,
		}, errors.New(reason)
	}
}

// fakeRegistryFor builds a NodeRegistry returning the given peer entries.
func fakeRegistryFor(entries ...NodeRegistryEntry) *fakeRegistry {
	return &fakeRegistry{entries: entries}
}

// commonOpts builds an EnsureOptions with sensible defaults for a test.
// Caller fills BundleRoot / Registry / PullFunc / ReleaseIndex.
func commonOpts(bundleRoot string, ri *ReleaseIndex) EnsureOptions {
	pool := x509.NewCertPool() // empty pool is fine when PullFunc is stubbed
	return EnsureOptions{
		BundleRoot:    bundleRoot,
		ReleaseIndex:  ri,
		ClusterCAPool: pool,
		Now:           time.Now().UTC(),
	}
}

// 1. Local cache match → fast path, no pull invoked.
func TestEnsureLocalCacheFastPath(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}

	// Pre-stage the active install layout: current/ → installed/.../<...>/
	versionedDir := filepath.Join(bundleRoot, "installed", ri.Version, ri.BuildID)
	if err := os.MkdirAll(versionedDir, 0755); err != nil {
		t.Fatalf("mkdir versioned: %v", err)
	}
	m := Manifest{
		Name: BundleName, Version: ri.Version, BuildID: ri.BuildID,
		SchemaVersion: "awareness.bundle.v1", SHA256: "f00d",
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(versionedDir, "manifest.json"), mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionedDir, "graph.db"), []byte("g"), 0644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	if err := os.Symlink(versionedDir, filepath.Join(bundleRoot, "current")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	pullCalls := atomic.Int32{}
	opts := commonOpts(bundleRoot, ri)
	opts.PullFunc = func(context.Context, PullOptions) (*PullResult, error) {
		pullCalls.Add(1)
		return nil, errors.New("must not be called on local cache fast path")
	}

	res, err := EnsureAwarenessBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensure: %v (state=%s reason=%s)", err, res.State, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if res.State != StateAwarenessReady {
		t.Errorf("state=%s, want AWARENESS_READY", res.State)
	}
	if pullCalls.Load() != 0 {
		t.Errorf("PullFunc called %d times; expected 0 on local cache fast path", pullCalls.Load())
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.Kind != SourceKindLocalCache {
		t.Errorf("InstalledFrom kind = %v, want local_cache", res.InstalledFrom)
	}
}

// 2. Single peer succeeds on first try.
func TestEnsureSinglePeerSucceeds(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now()

	registry := fakeRegistryFor(NodeRegistryEntry{
		NodeID: "peer-a", PeerURL: "https://peer-a:10260",
		BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version,
		LastSeen: now, Status: "RUNNING",
	})

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = stubPullSuccess(t, ri.Version, ri.BuildID)

	res, err := EnsureAwarenessBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensure: %v reason=%s", err, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s tried=%+v", res.State, res.Reason, res.SourceTried)
	}
	if len(res.SourceTried) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(res.SourceTried))
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.NodeID != "peer-a" {
		t.Errorf("InstalledFrom = %+v, want peer-a", res.InstalledFrom)
	}

	// Active symlink points at the new versioned dir.
	target, err := os.Readlink(filepath.Join(bundleRoot, "current"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	want := filepath.Join(bundleRoot, "installed", ri.Version, ri.BuildID)
	if target != want {
		t.Errorf("current → %s, want %s", target, want)
	}
}

// 3. First peer fails, second succeeds — orchestrator falls through.
func TestEnsureSequentialFallback(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now()

	registry := fakeRegistryFor(
		NodeRegistryEntry{NodeID: "first", PeerURL: "https://first:10260", BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version, LastSeen: now, Status: "RUNNING"},
		NodeRegistryEntry{NodeID: "second", PeerURL: "https://second:10260", BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version, LastSeen: now.Add(-1 * time.Second), Status: "RUNNING"},
	)

	calls := atomic.Int32{}
	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = func(ctx context.Context, popts PullOptions) (*PullResult, error) {
		n := calls.Add(1)
		// First call: peer-url for "first" → fail. Second: succeed.
		if n == 1 {
			return &PullResult{OK: false, State: StateAwarenessBundleSourceUnavailable, Reason: "peer first down"},
				errors.New("peer first down")
		}
		return stubPullSuccess(t, ri.Version, ri.BuildID)(ctx, popts)
	}

	res, err := EnsureAwarenessBundle(context.Background(), opts)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s tried=%+v", res.State, res.SourceTried)
	}
	if len(res.SourceTried) != 2 {
		t.Errorf("expected 2 attempts, got %d: %+v", len(res.SourceTried), res.SourceTried)
	}
	if res.SourceTried[0].State == StateAwarenessReady {
		t.Errorf("first attempt should have failed; got %s", res.SourceTried[0].State)
	}
	if res.SourceTried[1].State != StateAwarenessReady {
		t.Errorf("second attempt should have succeeded; got %s", res.SourceTried[1].State)
	}
	if res.InstalledFrom == nil || res.InstalledFrom.Candidate.NodeID != "second" {
		t.Errorf("InstalledFrom = %+v, want second", res.InstalledFrom)
	}
}

// 4. All peers fail → AWARENESS_BUNDLE_SOURCE_UNAVAILABLE.
func TestEnsureAllPeersFail(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	now := time.Now()

	registry := fakeRegistryFor(
		NodeRegistryEntry{NodeID: "p1", PeerURL: "https://p1:10260", BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version, LastSeen: now, Status: "RUNNING"},
		NodeRegistryEntry{NodeID: "p2", PeerURL: "https://p2:10260", BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version, LastSeen: now, Status: "RUNNING"},
	)

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = stubPullFail(StateAwarenessBundleSourceUnavailable, "peer down")

	res, _ := EnsureAwarenessBundle(context.Background(), opts)
	if res.OK {
		t.Fatal("OK=true with all peers failing")
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SOURCE_UNAVAILABLE", res.State)
	}
	if len(res.SourceTried) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(res.SourceTried))
	}
}

// 5. No candidates discovered (no local cache, no gateway, empty registry) →
// AWARENESS_BUNDLE_SOURCE_UNAVAILABLE with "no candidate sources" reason.
func TestEnsureNoCandidates(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = fakeRegistryFor() // empty
	opts.PullFunc = stubPullFail(StateAwarenessBundleSourceUnavailable, "should not be called")

	res, _ := EnsureAwarenessBundle(context.Background(), opts)
	if res.OK {
		t.Fatal("OK=true with no candidates")
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SOURCE_UNAVAILABLE", res.State)
	}
	if len(res.SourceTried) != 0 {
		t.Errorf("no attempts should have been made; got %d", len(res.SourceTried))
	}
}

// 6. Validation: missing release index or bundle root → error before doing anything.
func TestEnsureValidation(t *testing.T) {
	cases := []struct {
		name string
		opts EnsureOptions
	}{
		{"no bundle root", EnsureOptions{ReleaseIndex: &ReleaseIndex{Version: "v1", BuildID: "b"}, ClusterCAPool: x509.NewCertPool()}},
		{"no release", EnsureOptions{BundleRoot: "/tmp", ClusterCAPool: x509.NewCertPool()}},
		{"empty version", EnsureOptions{BundleRoot: "/tmp", ReleaseIndex: &ReleaseIndex{BuildID: "b"}, ClusterCAPool: x509.NewCertPool()}},
		{"empty build_id", EnsureOptions{BundleRoot: "/tmp", ReleaseIndex: &ReleaseIndex{Version: "v"}, ClusterCAPool: x509.NewCertPool()}},
		{"no pool no stub", EnsureOptions{BundleRoot: "/tmp", ReleaseIndex: &ReleaseIndex{Version: "v", BuildID: "b"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := EnsureAwarenessBundle(context.Background(), c.opts)
			if err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
			if res.OK {
				t.Errorf("OK=true for invalid options")
			}
		})
	}
}

// 7. EnsureLoop returns success on first attempt when ensure succeeds.
func TestEnsureLoopReturnsOnFirstSuccess(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}

	registry := fakeRegistryFor(NodeRegistryEntry{
		NodeID: "p1", PeerURL: "https://p:10260",
		BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version,
		LastSeen: time.Now(), Status: "RUNNING",
	})

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = stubPullSuccess(t, ri.Version, ri.BuildID)

	policy := RetryPolicy{Schedule: []time.Duration{0}, MaxAttempts: 5}
	res := EnsureLoop(context.Background(), opts, policy)
	if !res.OK {
		t.Fatalf("loop OK=false; state=%s reason=%s", res.State, res.Reason)
	}
}

// 8. EnsureLoop succeeds after a couple of failures.
func TestEnsureLoopRetriesUntilSuccess(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}

	registry := fakeRegistryFor(NodeRegistryEntry{
		NodeID: "p1", PeerURL: "https://p:10260",
		BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version,
		LastSeen: time.Now(), Status: "RUNNING",
	})

	calls := atomic.Int32{}
	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = func(ctx context.Context, popts PullOptions) (*PullResult, error) {
		n := calls.Add(1)
		if n < 3 {
			return &PullResult{OK: false, State: StateAwarenessBundleSourceUnavailable, Reason: "still flaky"},
				errors.New("flaky")
		}
		return stubPullSuccess(t, ri.Version, ri.BuildID)(ctx, popts)
	}

	// Tight schedule for fast tests.
	policy := RetryPolicy{Schedule: []time.Duration{0, 1 * time.Millisecond, 1 * time.Millisecond}, MaxAttempts: 5}
	res := EnsureLoop(context.Background(), opts, policy)
	if !res.OK {
		t.Fatalf("loop OK=false after retries; state=%s", res.State)
	}
	if calls.Load() < 3 {
		t.Errorf("expected at least 3 pull calls; got %d", calls.Load())
	}
}

// 9. EnsureLoop respects MaxAttempts and returns the last failure result.
func TestEnsureLoopHonoursMaxAttempts(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	registry := fakeRegistryFor(NodeRegistryEntry{
		NodeID: "p1", PeerURL: "https://p:10260",
		BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version,
		LastSeen: time.Now(), Status: "RUNNING",
	})

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = stubPullFail(StateAwarenessBundleSourceUnavailable, "always down")

	policy := RetryPolicy{Schedule: []time.Duration{0}, MaxAttempts: 3}
	res := EnsureLoop(context.Background(), opts, policy)
	if res == nil || res.OK {
		t.Fatalf("expected failure result, got %+v", res)
	}
	if res.State != StateAwarenessBundleSourceUnavailable {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SOURCE_UNAVAILABLE", res.State)
	}
}

// 10. EnsureLoop honours ctx cancellation. Backoff sleep wakes early.
func TestEnsureLoopHonoursContextCancel(t *testing.T) {
	bundleRoot := t.TempDir()
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	registry := fakeRegistryFor(NodeRegistryEntry{
		NodeID: "p1", PeerURL: "https://p:10260",
		BuildID: ri.BuildID, AwarenessBundleVersion: ri.Version,
		LastSeen: time.Now(), Status: "RUNNING",
	})

	opts := commonOpts(bundleRoot, ri)
	opts.Registry = registry
	opts.PullFunc = stubPullFail(StateAwarenessBundleSourceUnavailable, "always down")

	// Long schedule so the loop is sleeping when ctx cancels.
	policy := RetryPolicy{Schedule: []time.Duration{0, 30 * time.Second, 30 * time.Second}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	res := EnsureLoop(ctx, opts, policy)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("loop did not wake on ctx cancel; elapsed=%v", elapsed)
	}
	// Last result should be the unavailable failure (we ran at least once).
	if res == nil {
		t.Fatal("res nil after cancel")
	}
}

// 11. scheduleGap stays within the policy's last value (no overflow), and
// jitter stays within ± jitter bounds.
func TestScheduleGapWithinBounds(t *testing.T) {
	policy := RetryPolicy{
		Schedule: []time.Duration{0, 1 * time.Second, 5 * time.Second},
		Jitter:   0.5,
	}
	rng := mathrand.New(mathrand.NewSource(1)) // deterministic for repeatable test

	// Past the schedule end the last value repeats. Sample many iterations
	// to exercise jitter across its range.
	for i := 0; i < 200; i++ {
		gap := scheduleGap(policy, i, rng)
		if gap < 0 {
			t.Errorf("attempt %d: negative gap %v", i, gap)
		}
		// Last entry repeats with jitter; gap must stay within base*(1±jitter).
		base := policy.Schedule[len(policy.Schedule)-1]
		if i >= len(policy.Schedule)-1 {
			ceiling := time.Duration(float64(base) * (1 + policy.Jitter + 0.001))
			floor := time.Duration(float64(base) * (1 - policy.Jitter - 0.001))
			if gap > ceiling || gap < floor {
				t.Errorf("attempt %d: gap %v outside [%v, %v]", i, gap, floor, ceiling)
			}
		}
	}
}

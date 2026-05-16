package bundlesync

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── Phase C.2: pull a bundle from a peer MCP over HTTPS ──────────────────────
//
// Contract:
//   - The peer is authoritative ONLY when its TLS cert chains to the cluster CA.
//     If verification fails, PullBundle refuses — there is no implicit fallback
//     to InsecureSkipVerify. The aggregator's evidence-collection path is the
//     place for unverified TLS, not the install authority path.
//   - The peer's manifest is verified against the caller-supplied expected
//     version+build_id BEFORE downloading the bundle. We never pull a wrong
//     bundle just to discard it later.
//   - The downloaded bundle is verified end-to-end (sha256 + tar safety) before
//     PullBundle returns OK. Callers can hand the result paths to InstallBundle
//     without re-running verification.
//
// What this function does NOT do:
//   - install: that's InstallBundle's job
//   - source discovery: callers (Phase C.3) pick the peer
//   - retry: callers (Phase C.4) drive the orchestration

// PullEndpoints exposes the URL paths the puller hits. Variables (not
// constants) so a future protocol bump can override without touching the
// MCP server constants — the two values must agree but their identities
// belong to different layers (server vs. client).
var (
	pullManifestPath = "/awareness/manifest"
	pullBundlePath   = "/awareness/bundle"
)

// Sentinel errors the puller can return.
var (
	ErrPeerUnreachable = errors.New("peer unreachable")
	ErrPeerTLSUnverified = errors.New("peer TLS not verified against cluster CA")
)

// PullOptions carries the inputs to PullBundle.
type PullOptions struct {
	// PeerURL is the peer's base URL, e.g. "https://10.0.0.8:10260".
	// PullBundle appends the manifest/bundle paths itself.
	PeerURL string

	// OutDir is where bundle.tar.gz and manifest.json get written. Created
	// if missing. Existing files are overwritten.
	OutDir string

	// ExpectedVersion / ExpectedBuildID come from the caller's release-index.
	// PullBundle refuses to write a bundle whose manifest doesn't match.
	ExpectedVersion string
	ExpectedBuildID string

	// ClusterCAPool is the cert pool the puller verifies the peer's TLS
	// certificate against. REQUIRED — passing nil is a programmer error and
	// returns a VerifyResult, not a panic, so server-side metrics can count it.
	ClusterCAPool *x509.CertPool

	// Timeout caps the entire pull (manifest fetch + bundle stream). Default
	// 60 seconds when zero.
	Timeout time.Duration
}

// PullResult describes what was pulled.
type PullResult struct {
	OK     bool
	State  State
	Reason string

	// Paths only populated on success.
	BundlePath   string
	ManifestPath string

	// Peer's manifest (parsed). Set even when OK=false to aid diagnosis.
	PeerManifest *Manifest

	// TLS trust at the moment the bundle was streamed. PullBundle never
	// reports anything other than VERIFIED on success — UNVERIFIED is a
	// failure condition, surfaced for telemetry.
	TLSTrust string

	// SizeBytes / SHA256 reflect the downloaded artifact. Useful for the
	// caller's audit log.
	SizeBytes int64
	SHA256    string
}

const (
	tlsTrustVerified   = "VERIFIED"
	tlsTrustUnverified = "UNVERIFIED"
	tlsTrustNone       = "NONE"
)

// PullBundle fetches a bundle from a peer MCP and writes the bundle + sidecar
// manifest into opts.OutDir. The downloaded artifacts are verified end-to-end
// before PullBundle returns OK.
//
// On any failure the OutDir contents are removed so callers can retry without
// orphaning a partial bundle.
func PullBundle(ctx context.Context, opts PullOptions) (*PullResult, error) {
	res := &PullResult{TLSTrust: tlsTrustNone}

	if opts.PeerURL == "" || opts.OutDir == "" || opts.ExpectedVersion == "" || opts.ExpectedBuildID == "" {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = "PullOptions: peer_url, out_dir, expected_version, and expected_build_id are required"
		return res, fmt.Errorf("invalid options")
	}

	// (1) Build TLS-verifying HTTP client. ClusterCAPool is mandatory; absent
	// pool is a security failure, not a graceful degradation.
	if opts.ClusterCAPool == nil {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = "cluster CA pool not provided — refusing to pull bundle without TLS verification"
		return res, ErrPeerTLSUnverified
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    opts.ClusterCAPool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	if err := os.MkdirAll(opts.OutDir, 0755); err != nil {
		res.State = StateAwarenessBundleInstallFailed
		res.Reason = fmt.Sprintf("mkdir out_dir: %v", err)
		return res, err
	}
	bundleOut := filepath.Join(opts.OutDir, "bundle.tar.gz")
	manifestOut := filepath.Join(opts.OutDir, "manifest.json")

	// Cleanup helper: on failure, remove anything we wrote so the caller
	// can retry from a clean state.
	cleanup := func() {
		_ = os.Remove(bundleOut)
		_ = os.Remove(manifestOut)
	}

	// (2) Fetch the manifest first. Verify identity BEFORE we spend time
	// streaming a bundle that might not match.
	peerManifest, manifestBytes, fetchErr := fetchPeerManifest(ctx, client, opts.PeerURL)
	if fetchErr != nil {
		res.State = mapPullError(fetchErr)
		res.Reason = fetchErr.Error()
		cleanup()
		return res, fetchErr
	}
	res.PeerManifest = peerManifest

	// (3) Verify the peer's manifest matches the caller's expected release.
	// This is a stricter check than VerifyManifest's release-index match
	// because the puller's contract is "give me exactly this build."
	if peerManifest.Name != BundleName {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = fmt.Sprintf("peer manifest.name = %q, want %q", peerManifest.Name, BundleName)
		cleanup()
		return res, fmt.Errorf("manifest name mismatch")
	}
	if peerManifest.Version != opts.ExpectedVersion {
		res.State = StateAwarenessBundleMismatch
		res.Reason = fmt.Sprintf("peer manifest.version = %q, expected %q", peerManifest.Version, opts.ExpectedVersion)
		cleanup()
		return res, fmt.Errorf("version mismatch")
	}
	if peerManifest.BuildID != opts.ExpectedBuildID {
		res.State = StateAwarenessBundleStale
		res.Reason = fmt.Sprintf("peer manifest.build_id = %q, expected %q", peerManifest.BuildID, opts.ExpectedBuildID)
		cleanup()
		return res, fmt.Errorf("build_id mismatch")
	}
	if !supportsSchema(peerManifest.SchemaVersion) {
		res.State = StateAwarenessBundleSchemaUnsupported
		res.Reason = fmt.Sprintf("peer manifest.schema_version = %q not supported", peerManifest.SchemaVersion)
		cleanup()
		return res, fmt.Errorf("schema unsupported")
	}
	if peerManifest.SHA256 == "" {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = "peer manifest.sha256 is empty"
		cleanup()
		return res, fmt.Errorf("manifest sha256 missing")
	}

	// (4) Stream the bundle, hashing as we go so we don't have to re-read
	// the file for verification.
	size, sha, dlErr := downloadBundle(ctx, client, opts.PeerURL, bundleOut)
	if dlErr != nil {
		res.State = mapPullError(dlErr)
		res.Reason = dlErr.Error()
		cleanup()
		return res, dlErr
	}
	res.SizeBytes = size
	res.SHA256 = sha

	if !strings.EqualFold(sha, peerManifest.SHA256) {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = fmt.Sprintf("downloaded sha256 = %s, manifest claims %s", sha, peerManifest.SHA256)
		cleanup()
		return res, ErrSHA256Mismatch
	}

	// (5) Tar-safety scan on the downloaded file. ValidateTarSafe is read-only;
	// if it rejects, we cleanup and refuse.
	f, err := os.Open(bundleOut)
	if err != nil {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = fmt.Sprintf("reopen downloaded bundle: %v", err)
		cleanup()
		return res, err
	}
	violations, tarErr := ValidateTarSafe(f)
	f.Close()
	if tarErr != nil || len(violations) > 0 {
		res.State = StateAwarenessBundleVerifyFailed
		if len(violations) > 0 {
			res.Reason = fmt.Sprintf("unsafe tar entry: %s (%s)", violations[0].Name, violations[0].Reason)
		} else {
			res.Reason = fmt.Sprintf("tar scan: %v", tarErr)
		}
		cleanup()
		return res, ErrTarUnsafe
	}

	// (6) Persist the manifest sidecar (using the bytes we already have so we
	// don't re-fetch and risk a manifest swap mid-flight).
	if err := os.WriteFile(manifestOut, manifestBytes, 0644); err != nil {
		res.State = StateAwarenessBundleInstallFailed
		res.Reason = fmt.Sprintf("write manifest sidecar: %v", err)
		cleanup()
		return res, err
	}

	res.OK = true
	res.State = StateAwarenessReady
	res.TLSTrust = tlsTrustVerified
	res.BundlePath = bundleOut
	res.ManifestPath = manifestOut
	return res, nil
}

// fetchPeerManifest GETs /awareness/manifest and parses the JSON. Returns the
// raw bytes too so the caller can write the exact representation to the
// sidecar without re-marshalling (manifests are version-sensitive).
func fetchPeerManifest(ctx context.Context, client *http.Client, peerURL string) (*Manifest, []byte, error) {
	endpoint := strings.TrimRight(peerURL, "/") + pullManifestPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, classifyHTTPError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap on manifest
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusNotFound:
		return nil, nil, fmt.Errorf("peer reports AWARENESS_BUNDLE_MISSING")
	default:
		return nil, nil, fmt.Errorf("peer manifest status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, nil, fmt.Errorf("%w: parse manifest: %v", ErrManifestInvalid, err)
	}
	return &m, body, nil
}

// downloadBundle streams /awareness/bundle to outPath, hashing and counting
// bytes as it copies. The output file is overwritten if present.
func downloadBundle(ctx context.Context, client *http.Client, peerURL, outPath string) (int64, string, error) {
	endpoint := strings.TrimRight(peerURL, "/") + pullBundlePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", classifyHTTPError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, "", fmt.Errorf("peer bundle status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	out, err := os.Create(outPath)
	if err != nil {
		return 0, "", fmt.Errorf("create bundle out: %w", err)
	}

	h := sha256.New()
	mw := io.MultiWriter(out, h)
	n, err := io.Copy(mw, resp.Body)
	closeErr := out.Close()
	if err != nil {
		return n, "", fmt.Errorf("stream bundle: %w", err)
	}
	if closeErr != nil {
		return n, "", fmt.Errorf("close bundle out: %w", closeErr)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// classifyHTTPError converts low-level HTTP errors into typed sentinels the
// puller can map to states cleanly.
func classifyHTTPError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls"):
		return fmt.Errorf("%w: %v", ErrPeerTLSUnverified, err)
	default:
		return fmt.Errorf("%w: %v", ErrPeerUnreachable, err)
	}
}

// mapPullError selects a State for a pull error.
func mapPullError(err error) State {
	if errors.Is(err, ErrPeerTLSUnverified) {
		return StateAwarenessBundleVerifyFailed
	}
	if errors.Is(err, ErrPeerUnreachable) {
		return StateAwarenessBundleSourceUnavailable
	}
	if errors.Is(err, ErrSHA256Mismatch) || errors.Is(err, ErrTarUnsafe) || errors.Is(err, ErrManifestInvalid) {
		return StateAwarenessBundleVerifyFailed
	}
	return StateAwarenessBundleVerifyFailed
}

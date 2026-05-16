package bundlesync

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"time"
)

// ── Phase C.4: EnsureAwarenessBundle orchestrator ────────────────────────────
//
// EnsureAwarenessBundle runs the full sequence the spec calls for:
//
//   1. Discover sources (local cache, gateway, peers from registry).
//   2. If local cache matches the expected release → fast-path AWARENESS_READY.
//   3. Otherwise, walk candidates in priority order:
//      a. Pull the bundle (Phase C.2)
//      b. Install it atomically (Phase C.1)
//      c. On success → return with the source recorded.
//   4. If no candidate succeeded → AWARENESS_BUNDLE_SOURCE_UNAVAILABLE.
//
// The orchestrator NEVER:
//   - re-extracts a local cache (we trust discovery's manifest match;
//     diagnosing a corrupted active install belongs in a separate phase)
//   - falls back to insecure TLS (PullBundle requires a verified pool)
//   - rebuilds graphs from source (per the freshness spec)
//   - mutates desired state
//
// Every attempt's outcome is recorded in EnsureResult.SourceTried so an
// operator can see WHY each candidate failed without re-running the orchestrator.

// EnsureOptions configures one ensure pass.
type EnsureOptions struct {
	// BundleRoot is /var/lib/globular/awareness in production. Local cache
	// lookup, pull staging, and install all happen under this root.
	BundleRoot string

	// ReleaseIndex pins the version + build_id we want active.
	ReleaseIndex *ReleaseIndex

	// Discovery scoping.
	LocalNodeID          string
	ClusterID            string
	GatewayURL           string
	PeerMaxAge           time.Duration
	RequireRunningStatus bool
	Registry             NodeRegistry

	// ClusterCAPool is required for any pull. Phase C.2 enforces this.
	ClusterCAPool *x509.CertPool

	// PullTimeout caps each pull attempt. Default 60s when zero.
	PullTimeout time.Duration

	// PullFunc lets tests replace PullBundle with a deterministic stub.
	// Production callers leave this nil and the real PullBundle is used.
	PullFunc func(ctx context.Context, opts PullOptions) (*PullResult, error)

	// Now is injected by tests; production callers leave it zero and
	// time.Now().UTC() is used.
	Now time.Time
}

// SourceAttempt is the per-candidate audit record.
type SourceAttempt struct {
	Candidate SourceCandidate `json:"candidate"`
	State     State           `json:"state"`
	Reason    string          `json:"reason"`
	Err       string          `json:"err,omitempty"`
	Elapsed   time.Duration   `json:"elapsed_ms"`
}

// EnsureResult describes the outcome of one orchestrator pass.
type EnsureResult struct {
	OK     bool   `json:"ok"`
	State  State  `json:"state"`
	Reason string `json:"reason"`

	// SourceTried lists every candidate the orchestrator visited, in order.
	// Includes both successes and failures; the successful one is also
	// reachable via InstalledFrom for convenience.
	SourceTried []SourceAttempt `json:"source_tried"`

	// InstalledFrom is set when OK==true.
	InstalledFrom *SourceAttempt `json:"installed_from,omitempty"`

	// InstalledPath is the versioned install dir (set when OK==true and the
	// install actually wrote — empty when the local cache was the fast path).
	InstalledPath string `json:"installed_path,omitempty"`

	// Manifest carries whatever manifest description the orchestrator ended
	// up with — from the local cache when fast-path, or from the pulled peer
	// otherwise. Useful for the operator log line.
	Manifest *Manifest `json:"manifest,omitempty"`
}

// EnsureAwarenessBundle runs ONE orchestrator pass. It does not retry on
// failure — that's EnsureLoop's job — but it can be called repeatedly by
// any retry strategy (cron, backoff, manual).
func EnsureAwarenessBundle(ctx context.Context, opts EnsureOptions) (*EnsureResult, error) {
	if err := validateEnsureOptions(opts); err != nil {
		return &EnsureResult{
			State:  StateAwarenessBundleVerifyFailed,
			Reason: err.Error(),
		}, err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	res := &EnsureResult{
		State: StateAwarenessBundleSyncing,
	}

	// (1) Discover candidates.
	candidates, derr := DiscoverSources(ctx, DiscoveryOptions{
		ExpectedRelease:      *opts.ReleaseIndex,
		LocalNodeID:          opts.LocalNodeID,
		ClusterID:            opts.ClusterID,
		MaxAge:               opts.PeerMaxAge,
		GatewayURL:           opts.GatewayURL,
		LocalBundleDir:       opts.BundleRoot,
		RequireRunningStatus: opts.RequireRunningStatus,
		Now:                  now,
	}, opts.Registry)
	if derr != nil {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = fmt.Sprintf("discovery: %v", derr)
		return res, derr
	}

	// (2) Local cache fast path. If discovery says the active install already
	// matches expected release, return AWARENESS_READY without pulling.
	for _, c := range candidates {
		if c.Kind != SourceKindLocalCache {
			continue
		}
		manifest := readActiveManifestSilently(opts.BundleRoot)
		attempt := SourceAttempt{
			Candidate: c,
			State:     StateAwarenessReady,
			Reason:    "active install already matches expected release",
		}
		res.SourceTried = []SourceAttempt{attempt}
		res.InstalledFrom = &attempt
		res.OK = true
		res.State = StateAwarenessReady
		res.Reason = attempt.Reason
		res.Manifest = manifest
		return res, nil
	}

	// (3) Walk non-local candidates in priority order.
	pullFn := opts.PullFunc
	if pullFn == nil {
		pullFn = PullBundle
	}

	for _, c := range candidates {
		if c.Kind == SourceKindLocalCache {
			continue
		}
		attempt := tryCandidate(ctx, c, opts, pullFn)
		res.SourceTried = append(res.SourceTried, attempt)
		if attempt.State == StateAwarenessReady {
			res.OK = true
			res.State = StateAwarenessReady
			res.Reason = fmt.Sprintf("installed from %s (%s)", c.Kind, c.NodeID)
			res.InstalledFrom = &res.SourceTried[len(res.SourceTried)-1]
			// InstalledPath/Manifest captured via candidate's pull staging area.
			// We re-read the active manifest after install so the result reflects
			// what's now on disk, not what we hoped to install.
			res.Manifest = readActiveManifestSilently(opts.BundleRoot)
			res.InstalledPath = filepath.Join(opts.BundleRoot, "installed", opts.ReleaseIndex.Version, opts.ReleaseIndex.BuildID)
			return res, nil
		}
	}

	// (4) Nothing worked.
	if len(candidates) == 0 {
		res.State = StateAwarenessBundleSourceUnavailable
		res.Reason = "no candidate sources discovered (no local cache, no gateway, no matching peers)"
		return res, nil
	}
	res.State = StateAwarenessBundleSourceUnavailable
	res.Reason = fmt.Sprintf("all %d source(s) failed; see source_tried for details", len(candidates))
	return res, nil
}

// tryCandidate runs PullBundle then InstallBundle for one candidate and
// returns a SourceAttempt with the final state.
func tryCandidate(ctx context.Context, c SourceCandidate, opts EnsureOptions, pullFn func(context.Context, PullOptions) (*PullResult, error)) SourceAttempt {
	start := time.Now()

	pullDir, err := os.MkdirTemp(filepath.Join(opts.BundleRoot, "staging"), "pull-")
	if err != nil {
		// Try MkdirAll for the staging parent first; this is the cold-bootstrap path.
		if mkErr := os.MkdirAll(filepath.Join(opts.BundleRoot, "staging"), 0755); mkErr == nil {
			pullDir, err = os.MkdirTemp(filepath.Join(opts.BundleRoot, "staging"), "pull-")
		}
	}
	if err != nil {
		return SourceAttempt{
			Candidate: c,
			State:     StateAwarenessBundleInstallFailed,
			Reason:    "could not create pull staging dir",
			Err:       err.Error(),
			Elapsed:   time.Since(start),
		}
	}
	// pullDir cleanup happens after install regardless of outcome.
	defer os.RemoveAll(pullDir)

	pullRes, pullErr := pullFn(ctx, PullOptions{
		PeerURL:         c.PeerURL,
		OutDir:          pullDir,
		ExpectedVersion: opts.ReleaseIndex.Version,
		ExpectedBuildID: opts.ReleaseIndex.BuildID,
		ClusterCAPool:   opts.ClusterCAPool,
		Timeout:         opts.PullTimeout,
	})
	if pullErr != nil || pullRes == nil || !pullRes.OK {
		st := StateAwarenessBundleVerifyFailed
		reason := "pull failed"
		errMsg := ""
		if pullRes != nil {
			st = pullRes.State
			reason = pullRes.Reason
		}
		if pullErr != nil {
			errMsg = pullErr.Error()
		}
		return SourceAttempt{
			Candidate: c,
			State:     st,
			Reason:    reason,
			Err:       errMsg,
			Elapsed:   time.Since(start),
		}
	}

	// Hand the verified pulled paths to the installer.
	installRes, installErr := InstallBundle(InstallOptions{
		BundlePath:   pullRes.BundlePath,
		ManifestPath: pullRes.ManifestPath,
		BundleRoot:   opts.BundleRoot,
		ReleaseIndex: opts.ReleaseIndex,
	})
	if installErr != nil || installRes == nil || !installRes.OK {
		st := StateAwarenessBundleInstallFailed
		reason := "install failed"
		errMsg := ""
		if installRes != nil {
			st = installRes.State
			reason = installRes.Reason
		}
		if installErr != nil {
			errMsg = installErr.Error()
		}
		return SourceAttempt{
			Candidate: c,
			State:     st,
			Reason:    reason,
			Err:       errMsg,
			Elapsed:   time.Since(start),
		}
	}

	return SourceAttempt{
		Candidate: c,
		State:     StateAwarenessReady,
		Reason:    fmt.Sprintf("installed from %s/%s", c.Kind, c.NodeID),
		Elapsed:   time.Since(start),
	}
}

func validateEnsureOptions(opts EnsureOptions) error {
	if opts.BundleRoot == "" {
		return errors.New("EnsureOptions: bundle_root required")
	}
	if opts.ReleaseIndex == nil || opts.ReleaseIndex.Version == "" || opts.ReleaseIndex.BuildID == "" {
		return errors.New("EnsureOptions: release_index.version + build_id required")
	}
	if opts.ClusterCAPool == nil && opts.PullFunc == nil {
		// Production path: real PullBundle requires a CA pool. Tests can
		// inject a PullFunc that ignores the pool.
		return errors.New("EnsureOptions: cluster_ca_pool required (no pull func injected)")
	}
	return nil
}

// readActiveManifestSilently returns the active manifest if present and
// parseable, else nil. Used purely to enrich EnsureResult — failures here
// must never propagate, since the orchestrator's verdict is independent of
// whether we could load the post-install manifest for display.
func readActiveManifestSilently(bundleRoot string) *Manifest {
	m, err := LoadManifest(filepath.Join(bundleRoot, "current", "manifest.json"))
	if err != nil {
		return nil
	}
	return m
}

// ── Retry / backoff ──────────────────────────────────────────────────────────

// RetryPolicy configures EnsureLoop.
type RetryPolicy struct {
	// Schedule is the gap before each attempt. Schedule[0] is the gap before
	// the first attempt (typically 0), Schedule[1] before the second, and so
	// on. After Schedule is exhausted, the last value repeats.
	//
	// Default per the spec: [0, 5s, 15s, 30s, 60s, 120s, 240s, 300s].
	Schedule []time.Duration

	// MaxAttempts caps the loop. 0 = unlimited (driven only by ctx).
	MaxAttempts int

	// Jitter (0..1) adds a random fraction of the gap to each delay so a
	// fleet of nodes doesn't all retry on the same tick.
	Jitter float64
}

// DefaultRetryPolicy mirrors the spec's recommended schedule: immediate, 5s,
// 15s, 30s, 60s, then every 2-5 minutes (capped at 300s) until ctx cancels.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Schedule: []time.Duration{
			0,
			5 * time.Second,
			15 * time.Second,
			30 * time.Second,
			60 * time.Second,
			120 * time.Second,
			240 * time.Second,
			300 * time.Second,
		},
		Jitter: 0.2, // ±20%
	}
}

// EnsureLoop runs EnsureAwarenessBundle in a backoff loop. It returns:
//
//   - the first successful EnsureResult, or
//   - the last EnsureResult when the context is cancelled, or
//   - the last EnsureResult after MaxAttempts.
//
// The loop publishes nothing externally — that's a job for the caller's
// logger. EnsureLoop's only side effect is the install on success.
func EnsureLoop(ctx context.Context, opts EnsureOptions, policy RetryPolicy) *EnsureResult {
	if len(policy.Schedule) == 0 {
		policy = DefaultRetryPolicy()
	}

	var last *EnsureResult
	rng := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))

	for attempt := 0; ; attempt++ {
		if policy.MaxAttempts > 0 && attempt >= policy.MaxAttempts {
			return last
		}

		gap := scheduleGap(policy, attempt, rng)
		if gap > 0 {
			t := time.NewTimer(gap)
			select {
			case <-ctx.Done():
				t.Stop()
				return last
			case <-t.C:
			}
		} else {
			// Even at gap=0 honor a cancelled ctx.
			select {
			case <-ctx.Done():
				return last
			default:
			}
		}

		res, _ := EnsureAwarenessBundle(ctx, opts)
		last = res
		if res != nil && res.OK {
			return res
		}
	}
}

// scheduleGap returns the gap before attempt n, with jitter applied. After
// Schedule is exhausted, the last value repeats.
func scheduleGap(policy RetryPolicy, attempt int, rng *mathrand.Rand) time.Duration {
	if len(policy.Schedule) == 0 {
		return 0
	}
	idx := attempt
	if idx >= len(policy.Schedule) {
		idx = len(policy.Schedule) - 1
	}
	base := policy.Schedule[idx]
	if base <= 0 || policy.Jitter <= 0 {
		return base
	}
	// jitter in [-Jitter, +Jitter] of base.
	delta := (rng.Float64()*2 - 1) * policy.Jitter
	scaled := time.Duration(float64(base) * (1 + delta))
	if scaled < 0 {
		scaled = 0
	}
	return scaled
}

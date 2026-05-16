package evidence

import (
	"testing"
	"time"
)

// ── Phase D acceptance tests ──────────────────────────────────────────────────
//
// Pin the contract that Day-1 readiness reflects bundlesync truth:
//
//   - Missing bundle  → AWARENESS_READY=false, Classification=AWARENESS_BUNDLE_MISSING
//   - Stale bundle    → AWARENESS_READY=false, Classification=AWARENESS_BUNDLE_STALE
//   - Mismatch bundle → AWARENESS_READY=false, Classification=AWARENESS_BUNDLE_MISMATCH
//   - DAY1_COMPLETE   → false in every non-READY case
//   - Architecture-sensitive failures (Scylla / Etcd / etc.) MUST NOT appear
//     as the verdict while the bundle is not READY — the bundle gate is
//     authoritative ahead of architecture gates.
//
// The freshness invariant
// (awareness.auto_install_requires_verification + .graph_freshness_matches_release)
// is what these tests defend.

// freshBundle returns a Day-1 healthy snapshot template with a fresh bundle.
// Helper to keep the per-case tests focused on the one thing they vary.
func freshBundle() (ReleaseInfo, AwarenessBundleStatus) {
	return ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "abc123"},
		AwarenessBundleStatus{
			Present: true, Status: "LOADED",
			Version: "1.2.30", BuildID: "abc123",
		}
}

// healthyServices/healthyPorts let architecture gates pass so the only
// failing axis is the bundle.
func healthyServices() []ServiceObservation {
	return []ServiceObservation{
		{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
		{Name: "minio", UnitName: "minio.service", ActiveState: "active", SubState: "running"},
		{Name: "envoy", UnitName: "envoy.service", ActiveState: "active", SubState: "running"},
		{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
	}
}
func healthyPorts() []PortObservation {
	return []PortObservation{
		{Port: 9042, Protocol: "tcp", Listening: true},
		{Port: 2379, Protocol: "tcp", Listening: true},
		{Port: 9000, Protocol: "tcp", Listening: true},
	}
}

// ── 1. Missing bundle blocks AWARENESS_READY and DAY1_COMPLETE ───────────────

func TestDay1RefusesReadyWhenBundleMissing(t *testing.T) {
	rel, _ := freshBundle()

	snap := makeSnap("node-a", PhaseDAY1, rel,
		AwarenessBundleStatus{Present: false, Status: "MISSING"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), healthyPorts(),
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle missing")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false when bundle missing")
	}
	if verdict.Verdict != "BLOCK" {
		t.Errorf("verdict = %s, want BLOCK", verdict.Verdict)
	}
	if verdict.Classification != ClassAwarenessBundleMissing {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISSING", verdict.Classification)
	}
}

// ── 2. Stale bundle (build_id drift on matching version) ─────────────────────

func TestDay1RefusesReadyWhenBundleStale(t *testing.T) {
	rel, _ := freshBundle()
	stale := AwarenessBundleStatus{
		Present: true, Status: "LOADED",
		Version: rel.Version, BuildID: "old-build", // version matches, build differs
	}

	snap := makeSnap("node-a", PhaseDAY1, rel, stale,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), healthyPorts(),
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false for stale bundle")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false for stale bundle")
	}
	if verdict.Classification != ClassAwarenessBundleStale {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_STALE", verdict.Classification)
	}

	// Forbidden actions must explicitly mention DAY1_COMPLETE block.
	foundForbid := false
	for _, a := range verdict.ForbiddenActions {
		if a == "mark node DAY1_COMPLETE" {
			foundForbid = true
		}
	}
	if !foundForbid {
		t.Errorf("forbidden_actions must include \"mark node DAY1_COMPLETE\" for stale bundle; got %v", verdict.ForbiddenActions)
	}
}

// ── 3. Mismatch bundle (different version) ───────────────────────────────────

func TestDay1RefusesReadyWhenBundleMismatched(t *testing.T) {
	rel, _ := freshBundle()
	wrong := AwarenessBundleStatus{
		Present: true, Status: "LOADED",
		Version: "1.0.0", BuildID: "anything", // version differs from release
	}

	snap := makeSnap("node-a", PhaseDAY1, rel, wrong,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), healthyPorts(),
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false for mismatched bundle")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false for mismatched bundle")
	}
	if verdict.Classification != ClassAwarenessBundleMismatch {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISMATCH", verdict.Classification)
	}
}

// ── 4. Architecture-sensitive classification blocked by stale bundle ─────────
//
// Even if Scylla is genuinely down, a stale bundle MUST be the verdict —
// you can't trust the graph's contracts to classify the runtime fault. The
// freshness spec calls this out: "classify architecture-sensitive failures
// using stale bundle" is a forbidden action.

func TestArchitectureClassificationBlockedByStaleBundle(t *testing.T) {
	rel, _ := freshBundle()
	stale := AwarenessBundleStatus{
		Present: true, Status: "LOADED",
		Version: rel.Version, BuildID: "old-build",
	}

	// Scylla port closed — would normally classify as SCYLLA_NOT_READY.
	scyllaDownServices := []ServiceObservation{
		{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
		{Name: "minio", UnitName: "minio.service", ActiveState: "active", SubState: "running"},
		{Name: "envoy", UnitName: "envoy.service", ActiveState: "active", SubState: "running"},
		{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
	}
	scyllaDownPorts := []PortObservation{
		{Port: 9042, Protocol: "tcp", Listening: false}, // Scylla down
		{Port: 2379, Protocol: "tcp", Listening: true},
		{Port: 9000, Protocol: "tcp", Listening: true},
	}

	snap := makeSnap("node-a", PhaseDAY1, rel, stale,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		scyllaDownServices, scyllaDownPorts,
	)
	verdict := classifySnap(t, snap)

	// Bundle gate wins.
	if verdict.Classification != ClassAwarenessBundleStale {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_STALE (bundle gate must win over Scylla)", verdict.Classification)
	}
	// Scylla-related classifications must NOT be the verdict.
	if verdict.Classification == ClassScyllaNotReady ||
		verdict.Classification == ClassJoinedButDependencyBlocked {
		t.Errorf("classification = %s; stale bundle must block architecture-sensitive classification", verdict.Classification)
	}

	// Forbidden actions must surface the rule.
	want := "classify architecture-sensitive failures using stale bundle"
	foundForbid := false
	for _, a := range verdict.ForbiddenActions {
		if a == want {
			foundForbid = true
		}
	}
	if !foundForbid {
		t.Errorf("forbidden_actions must include %q; got %v", want, verdict.ForbiddenActions)
	}
}

// ── 5. Mismatch bundle blocks architecture-sensitive classification too ──────

func TestArchitectureClassificationBlockedByMismatchedBundle(t *testing.T) {
	rel, _ := freshBundle()
	wrong := AwarenessBundleStatus{
		Present: true, Status: "LOADED",
		Version: "0.0.1", BuildID: "totally-different",
	}

	scyllaDownPorts := []PortObservation{
		{Port: 9042, Protocol: "tcp", Listening: false},
		{Port: 2379, Protocol: "tcp", Listening: true},
		{Port: 9000, Protocol: "tcp", Listening: true},
	}

	snap := makeSnap("node-a", PhaseDAY1, rel, wrong,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), scyllaDownPorts,
	)
	verdict := classifySnap(t, snap)

	if verdict.Classification != ClassAwarenessBundleMismatch {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISMATCH (bundle gate must win over runtime)", verdict.Classification)
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false")
	}
}

// ── 6. Bundle present but Status != LOADED also blocks readiness ─────────────

func TestDay1RefusesReadyWhenBundleStatusNotLoaded(t *testing.T) {
	rel, _ := freshBundle()
	corrupt := AwarenessBundleStatus{
		Present: true, Status: "CORRUPT", // any non-LOADED status
		Version: rel.Version, BuildID: rel.BuildID,
	}

	snap := makeSnap("node-a", PhaseDAY1, rel, corrupt,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), healthyPorts(),
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle Status != LOADED")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false")
	}
}

// ── 7. Sanity: Day-1 PASSES when bundle fresh AND infrastructure healthy ─────

func TestDay1PassesWhenBundleAndInfrastructureBothHealthy(t *testing.T) {
	rel, fresh := freshBundle()

	snap := makeSnap("node-a", PhaseDAY1, rel, fresh,
		pkiAllReadable(),
		ScyllaConfigObservation{},
		healthyServices(), healthyPorts(),
	)
	verdict := classifySnap(t, snap)

	if !verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be true with fresh bundle")
	}
	if !verdict.Readiness[LevelDay1Complete] {
		t.Errorf("DAY1_COMPLETE must be true; readiness=%v facts=%v", verdict.Readiness, snap.Facts)
	}
	if verdict.Verdict != "PASS" {
		t.Errorf("verdict = %s, want PASS", verdict.Verdict)
	}
}

// ── 8. The new fact is emitted for build_id-only drift, not Mismatch ─────────

func TestNormalizerEmitsStaleForBuildIDDriftOnly(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-a",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Release:     ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "abc123"},
		AwarenessBundle: AwarenessBundleStatus{
			Present: true, Status: "LOADED",
			Version: "1.2.30", BuildID: "old-build",
		},
		PKI: pkiAllReadable(),
	}
	facts := (&Normalizer{}).Normalize(snap)

	gotStale := false
	gotMismatch := false
	for _, f := range facts {
		if f.Kind == FactAwarenessBundleStale {
			gotStale = true
		}
		if f.Kind == FactAwarenessBundleMismatch {
			gotMismatch = true
		}
	}
	if !gotStale {
		t.Error("expected FactAwarenessBundleStale for build_id drift on matching version")
	}
	if gotMismatch {
		t.Error("FactAwarenessBundleMismatch must NOT be emitted when only build_id differs (that's STALE)")
	}
}

// ── 9. Version drift still emits FactAwarenessBundleMismatch (not Stale) ─────

func TestNormalizerEmitsMismatchForVersionDrift(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-a",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Release:     ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "abc123"},
		AwarenessBundle: AwarenessBundleStatus{
			Present: true, Status: "LOADED",
			Version: "0.0.1", BuildID: "anything",
		},
		PKI: pkiAllReadable(),
	}
	facts := (&Normalizer{}).Normalize(snap)

	gotMismatch := false
	for _, f := range facts {
		if f.Kind == FactAwarenessBundleMismatch {
			gotMismatch = true
		}
	}
	if !gotMismatch {
		t.Error("expected FactAwarenessBundleMismatch for version drift")
	}
}

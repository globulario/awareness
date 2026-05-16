package evidence

import (
	"testing"
	"time"
)

// ── Test 1: Awareness bundle missing → AWARENESS_BUNDLE_MISSING ──────────────

func TestAwarenessBundleMissingClassification(t *testing.T) {
	snap := makeSnap("node-b", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0"},
		AwarenessBundleStatus{Present: false, Status: "MISSING"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		[]PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: true},
			{Port: 2379, Protocol: "tcp", Listening: true},
		},
	)
	verdict := classifySnap(t, snap)

	if !verdict.Readiness[LevelMCPReachable] {
		t.Error("MCP_REACHABLE must be true when collector runs")
	}
	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle is missing")
	}
	if verdict.Classification != ClassAwarenessBundleMissing {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISSING", verdict.Classification)
	}
	if verdict.Verdict != "BLOCK" {
		t.Errorf("verdict = %s, want BLOCK", verdict.Verdict)
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false when bundle is missing")
	}
	assertAllowedContains(t, verdict.AllowedActions, "fetch awareness bundle from repository")
}

// ── Test 2: Scylla failed → JOINED_BUT_DEPENDENCY_BLOCKED ────────────────────

func TestScyllaFailedBlocksDay1(t *testing.T) {
	snap := makeSnap("node-b", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0"},
		AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.0", BuildID: "abc123"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "failed", SubState: "failed"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		[]PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: false},
			{Port: 2379, Protocol: "tcp", Listening: true},
		},
	)
	verdict := classifySnap(t, snap)

	if !verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be true when bundle is loaded and version matches")
	}
	if verdict.Readiness[LevelScyllaReady] {
		t.Error("SCYLLA_READY must be false when scylla-server is failed")
	}
	if verdict.Classification != ClassJoinedButDependencyBlocked {
		t.Errorf("classification = %s, want JOINED_BUT_DEPENDENCY_BLOCKED", verdict.Classification)
	}
	if verdict.Verdict != "BLOCK" {
		t.Errorf("verdict = %s, want BLOCK", verdict.Verdict)
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false when Scylla not ready")
	}
	assertForbiddenContains(t, verdict.ForbiddenActions, "mark node DAY1_COMPLETE")
}

// ── Test 3: Normalizer — failed Scylla unit → SCYLLA_CQL_UNREACHABLE ─────────

func TestNormalizerScyllaFailedUnit(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "10.0.0.8",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Services: []ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "failed", SubState: "failed"},
		},
		Ports: []PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: false},
		},
	}

	facts := (&Normalizer{}).Normalize(snap)

	found := false
	for _, f := range facts {
		if f.Kind == FactScyllaCQLUnreachable {
			found = true
			if f.NodeID != "10.0.0.8" {
				t.Errorf("node_id = %s, want 10.0.0.8", f.NodeID)
			}
			if f.Severity != SeverityCritical {
				t.Errorf("severity = %s, want CRITICAL", f.Severity)
			}
			if f.Phase != PhaseDAY1 {
				t.Errorf("phase = %s, want DAY1", f.Phase)
			}
		}
	}
	if !found {
		t.Errorf("expected SCYLLA_CQL_UNREACHABLE fact, got: %v", facts)
	}
}

// ── Test 4: start-limit-hit → START_LIMIT_HIT fact ───────────────────────────

func TestNormalizerStartLimitHit(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-c",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Services: []ServiceObservation{
			{Name: "globular-workflow", UnitName: "globular-workflow.service",
				ActiveState: "failed", SubState: "start-limit-hit"},
		},
	}

	facts := (&Normalizer{}).Normalize(snap)

	found := false
	for _, f := range facts {
		if (f.Kind == FactStartLimitHit || f.Kind == FactUnitStartLimitHit) &&
			f.Service == "globular-workflow" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected START_LIMIT_HIT for globular-workflow, got: %v", facts)
	}
}

// ── Test 5: Healthy node → PASS verdict ──────────────────────────────────────

func TestHealthyNodePassesDay1(t *testing.T) {
	snap := makeSnap("node-a", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0", BuildID: "abc"},
		AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.0", BuildID: "abc"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
			{Name: "minio", UnitName: "minio.service", ActiveState: "active", SubState: "running"},
			{Name: "envoy", UnitName: "envoy.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		[]PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: true},
			{Port: 2379, Protocol: "tcp", Listening: true},
			{Port: 9000, Protocol: "tcp", Listening: true},
		},
	)
	verdict := classifySnap(t, snap)

	if verdict.Verdict != "PASS" {
		t.Errorf("verdict = %s, want PASS (facts: %v)", verdict.Verdict, snap.Facts)
	}
	if !verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be true for a healthy node")
	}
}

// ── Test 6: Port closed without systemd failure still emits SCYLLA fact ───────

func TestPortClosedWithoutSystemdFailureEmitsFact(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-x",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Services: []ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
		},
		Ports: []PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: false},
		},
	}

	facts := (&Normalizer{}).Normalize(snap)

	// Expect SERVICE_ACTIVE_HEALTH_FAILED and/or SCYLLA_CQL_UNREACHABLE.
	found := false
	for _, f := range facts {
		if f.Kind == FactScyllaCQLUnreachable || f.Kind == FactServiceActiveHealthFailed {
			found = true
		}
	}
	if !found {
		t.Error("expected fact for closed port 9042 even when systemd says active")
	}
}

// ── Addendum Test 1: Workflow contract blocks unsafe remediation ───────────────

func TestWorkflowBlockedWhenScyllaDown(t *testing.T) {
	snap := makeSnap("node-b", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0"},
		AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.0"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "failed", SubState: "failed"},
			{Name: "globular-workflow", UnitName: "globular-workflow.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		[]PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: false},
			{Port: 2379, Protocol: "tcp", Listening: true},
			{Port: 10004, Protocol: "tcp", Listening: true},
		},
	)
	verdict := classifySnap(t, snap)

	// Scylla is the primary blocker, which makes workflow remediation unsafe.
	if verdict.Readiness[LevelWorkflowReady] {
		t.Error("WORKFLOW_READY must be false when Scylla is not ready")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false when workflow is unsafe")
	}
	// Should be JOINED_BUT_DEPENDENCY_BLOCKED (Scylla is the primary blocker before workflow).
	if verdict.Classification != ClassJoinedButDependencyBlocked {
		t.Errorf("classification = %s, want JOINED_BUT_DEPENDENCY_BLOCKED", verdict.Classification)
	}

	// Check WORKFLOW_REMEDIATION_UNSAFE fact was emitted.
	found := false
	for _, f := range snap.Facts {
		if f.Kind == FactWorkflowRemediationUnsafe {
			found = true
		}
	}
	if !found {
		t.Error("expected WORKFLOW_REMEDIATION_UNSAFE fact when Scylla is down and workflow is observed")
	}
}

// ── Addendum Test 2: Scylla config authority drift explains failure ────────────

func TestScyllaConfigAuthorityDrift(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-b",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.0"},
		PKI:         pkiAllReadable(),
		Release:     ReleaseInfo{Present: true, Version: "1.2.0"},
		ScyllaConfig: ScyllaConfigObservation{
			Present: true,
			// Seeds intentionally empty to simulate missing seed list.
			Seeds: nil,
		},
		Services: []ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "failed", SubState: "failed"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		Ports: []PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: false},
			{Port: 2379, Protocol: "tcp", Listening: true},
		},
	}

	facts := (&Normalizer{}).Normalize(snap)

	found := false
	for _, f := range facts {
		if f.Kind == FactScyllaConfigAuthorityDrift {
			found = true
		}
	}
	if !found {
		t.Error("expected SCYLLA_CONFIG_AUTHORITY_DRIFT when scylla.yaml has no seeds")
	}
}

// ── Addendum Test 3: MCP reachable but bundle missing ────────────────────────

func TestMCPReachableAwarenessBundleMissing(t *testing.T) {
	snap := makeSnap("node-b", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0"},
		AwarenessBundleStatus{Present: false, Status: "MISSING"},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{},
		[]PortObservation{{Port: 10260, Protocol: "tcp", Listening: true}},
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle missing")
	}
	if verdict.Readiness[LevelDay1Complete] {
		t.Error("DAY1_COMPLETE must be false when bundle missing")
	}
	if verdict.Classification != ClassAwarenessBundleMissing {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISSING", verdict.Classification)
	}
	assertForbiddenContains(t, verdict.ForbiddenActions, "mark node DAY1_COMPLETE")
}

// ── Addendum Test 4: PKI missing blocks Day-1 ────────────────────────────────

func TestPKIMissingBlocksDay1(t *testing.T) {
	snap := makeSnap("node-b", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.0"},
		AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.0"},
		// CA cert missing.
		PKIObservation{
			CACertPresent:    false,
			CACertReadable:   false, // missing file is implicitly unreadable
			NodeCertPresent:  true,
			NodeCertReadable: true,
			NodeKeyPresent:   true,
			NodeKeyReadable:  true,
		},
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		[]PortObservation{{Port: 2379, Protocol: "tcp", Listening: true}},
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelPKIReady] {
		t.Error("PKI_READY must be false when CA cert is missing")
	}
	if verdict.Classification != ClassPKIMissing {
		t.Errorf("classification = %s, want PKI_MISSING", verdict.Classification)
	}
	if verdict.Verdict != "BLOCK" {
		t.Errorf("verdict = %s, want BLOCK", verdict.Verdict)
	}
}

// ── Addendum Test 5: Service active but port closed → ACTIVE_HEALTH_FAILED ───

func TestServiceActivePortClosedEmitsHealthFailed(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-x",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Services: []ServiceObservation{
			{Name: "globular-workflow", UnitName: "globular-workflow.service",
				ActiveState: "active", SubState: "running"},
		},
		Ports: []PortObservation{
			{Port: 10004, Protocol: "tcp", Listening: false},
		},
	}

	facts := (&Normalizer{}).Normalize(snap)

	found := false
	for _, f := range facts {
		if f.Kind == FactServiceActiveHealthFailed && f.Service == "globular-workflow" {
			found = true
		}
	}
	if !found {
		t.Error("expected SERVICE_ACTIVE_HEALTH_FAILED for workflow when port 10004 closed")
	}
	// Also check RUNTIME_HEALTH_MISMATCH.
	found2 := false
	for _, f := range facts {
		if f.Kind == FactRuntimeHealthMismatch && f.Service == "globular-workflow" {
			found2 = true
		}
	}
	if !found2 {
		t.Error("expected RUNTIME_HEALTH_MISMATCH for workflow when port 10004 closed")
	}
}

// ── Addendum Test 6: Awareness bundle version mismatch ────────────────────────

func TestAwarenessBundleVersionMismatch(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:      "node-c",
		Phase:       PhaseDAY1,
		CollectedAt: time.Now().UTC(),
		Release:     ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "new-build"},
		AwarenessBundle: AwarenessBundleStatus{
			Present: true, Status: "LOADED", Version: "1.2.29", BuildID: "old-build",
		},
		PKI: pkiAllReadable(),
	}

	facts := (&Normalizer{}).Normalize(snap)

	found := false
	for _, f := range facts {
		if f.Kind == FactAwarenessBundleMismatch {
			found = true
		}
	}
	if !found {
		t.Error("expected AWARENESS_BUNDLE_MISMATCH when bundle v1.2.29 != release v1.2.30")
	}

	verdict := classifySnap(t, snap)
	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle version mismatches release-index")
	}
	if verdict.Classification != ClassAwarenessBundleMismatch {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_MISMATCH", verdict.Classification)
	}
}

// ── Phase B.1: bundle freshness vs runtime evidence orthogonality ────────────
//
// The freshness spec is explicit: "the graph is the map. Runtime evidence is
// the weather. Bad weather does not make the map obsolete."
//
// These tests pin both sides of that rule:
//
//   (a) Fresh bundle + Scylla port closed → SCYLLA_NOT_READY, NOT a bundle-stale verdict.
//       AWARENESS_READY stays true; the runtime failure does not "stale" the graph.
//
//   (b) Stale bundle + Scylla port open → AWARENESS_BUNDLE_MISMATCH classification.
//       The runtime is fine; the bundle is what's wrong, and we say so distinctly.
//
// Together they guarantee the classifier never collapses these axes — operators
// can trust the verdict to point at the actual fault, not a confused mixture.

// (a) Runtime port closed but bundle is fresh — verdict must be runtime-side.
func TestRuntimeFailureDoesNotStaleAFreshBundle(t *testing.T) {
	snap := makeSnap("node-a", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "abc123"},
		AwarenessBundleStatus{
			Present: true, Status: "LOADED",
			Version: "1.2.30", BuildID: "abc123",
		},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		// Scylla port closed even though service is "running" — runtime-side fault.
		[]PortObservation{
			{Port: 2379, Protocol: "tcp", Listening: true},
			{Port: 9042, Protocol: "tcp", Listening: false},
		},
	)
	verdict := classifySnap(t, snap)

	// Fresh bundle: AWARENESS_READY must stay true.
	if !verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be true for a fresh bundle, regardless of runtime port state")
	}

	// Verdict must point at Scylla, not at the bundle.
	if verdict.Classification == ClassAwarenessBundleMismatch ||
		verdict.Classification == ClassAwarenessBundleMissing {
		t.Errorf("classification = %s; runtime port failure must NOT classify as a bundle-staleness fault", verdict.Classification)
	}
	// Should land on Scylla (either ClassScyllaNotReady or ClassJoinedButDependencyBlocked).
	if verdict.Classification != ClassScyllaNotReady &&
		verdict.Classification != ClassJoinedButDependencyBlocked {
		t.Errorf("classification = %s; want SCYLLA_NOT_READY or JOINED_BUT_DEPENDENCY_BLOCKED", verdict.Classification)
	}

	// And no fact should claim the bundle mismatched the release.
	for _, f := range snap.Facts {
		if f.Kind == FactAwarenessBundleMismatch {
			t.Errorf("emitted FactAwarenessBundleMismatch despite fresh bundle: %+v", f)
		}
	}
}

// (b) Bundle stale but Scylla running fine — verdict must point at the bundle.
func TestStaleBundleClassifiesIndependentlyOfRuntime(t *testing.T) {
	snap := makeSnap("node-a", PhaseDAY1,
		ReleaseInfo{Present: true, Version: "1.2.30", BuildID: "abc123"},
		AwarenessBundleStatus{
			Present: true, Status: "LOADED",
			Version: "1.2.30", BuildID: "old999", // build_id drift on same version
		},
		pkiAllReadable(),
		ScyllaConfigObservation{},
		[]ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		// Runtime is healthy.
		[]PortObservation{
			{Port: 2379, Protocol: "tcp", Listening: true},
			{Port: 9042, Protocol: "tcp", Listening: true},
		},
	)
	verdict := classifySnap(t, snap)

	if verdict.Readiness[LevelAwarenessReady] {
		t.Error("AWARENESS_READY must be false when bundle build_id drifts from release-index")
	}
	// Build_id drift on a matching version is STALE per the freshness spec —
	// distinct from MISMATCH which means the version itself differs.
	if verdict.Classification != ClassAwarenessBundleStale {
		t.Errorf("classification = %s, want AWARENESS_BUNDLE_STALE", verdict.Classification)
	}
	// Runtime-side gate must NOT be the verdict — Scylla is healthy.
	if verdict.Classification == ClassScyllaNotReady ||
		verdict.Classification == ClassJoinedButDependencyBlocked {
		t.Errorf("classification = %s; bundle-staleness must not appear as a Scylla fault", verdict.Classification)
	}
}

// ── Test: Day1ReadinessLadder has all 14 levels in order ─────────────────────

func TestDay1ReadinessLadderCompleteness(t *testing.T) {
	if len(Day1ReadinessLadder) != 14 {
		t.Errorf("ladder has %d levels, want 14", len(Day1ReadinessLadder))
	}
	if Day1ReadinessLadder[0] != LevelNodeSeen {
		t.Errorf("first level = %s, want NODE_SEEN", Day1ReadinessLadder[0])
	}
	if Day1ReadinessLadder[len(Day1ReadinessLadder)-1] != LevelDay1Complete {
		t.Errorf("last level = %s, want DAY1_COMPLETE", Day1ReadinessLadder[len(Day1ReadinessLadder)-1])
	}
	// Verify MCP_TRUSTED, PKI_READY, and WORKFLOW_READY are present.
	levels := make(map[Day1ReadinessLevel]bool)
	for _, l := range Day1ReadinessLadder {
		levels[l] = true
	}
	for _, want := range []Day1ReadinessLevel{LevelMCPTrusted, LevelPKIReady, LevelWorkflowReady} {
		if !levels[want] {
			t.Errorf("ladder missing level %s", want)
		}
	}
}

// ── Test: HighestReachedLevel stops at first false ────────────────────────────

// ── portReady behavior tests ──────────────────────────────────────────────────

// Service expected on this node + port not observed → not ready.
// This used to silently pass before the portReady refactor.
func TestPortReadyMissingObservationWhenServiceExpected(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Services: []ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
		},
		// No port observations at all.
	}
	if portReady(snap, 9042, "scylla", "scylla-server") {
		t.Error("portReady must return false when scylla is expected on this node but port 9042 was not observed")
	}
}

// Service NOT expected on this node + port not observed → ready.
// Aggregator nodes without minio shouldn't fail Day-1 because port 9000 wasn't probed.
func TestPortReadyMissingObservationWhenServiceNotExpected(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Services: []ServiceObservation{
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
	}
	if !portReady(snap, 9000, "minio") {
		t.Error("portReady must return true when minio is NOT expected on this node and port 9000 was not observed")
	}
}

// Port observed listening → ready regardless of service expectation.
func TestPortReadyObservedListening(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Ports: []PortObservation{{Port: 2379, Protocol: "tcp", Listening: true}},
	}
	if !portReady(snap, 2379, "etcd") {
		t.Error("portReady must return true when port is observed listening")
	}
}

// Port observed not listening → not ready regardless of service expectation.
func TestPortReadyObservedNotListening(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Ports: []PortObservation{{Port: 2379, Protocol: "tcp", Listening: false}},
	}
	if portReady(snap, 2379, "etcd") {
		t.Error("portReady must return false when port is observed not listening")
	}
}

func TestHighestReachedLevelStopsAtFirstFalse(t *testing.T) {
	v := &Day1Verdict{
		Readiness: map[Day1ReadinessLevel]bool{
			LevelNodeSeen:        true,
			LevelMCPReachable:    true,
			LevelMCPTrusted:      false,
			LevelAwarenessReady:  true, // must not count past the first false
		},
	}
	got := v.HighestReachedLevel()
	if got != LevelMCPReachable {
		t.Errorf("HighestReachedLevel = %s, want MCP_REACHABLE", got)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// pkiAllReadable returns a PKIObservation where all three artifacts are
// present on disk AND readable by the collecting process — the common
// "healthy local PKI" fixture. Tests that need to vary one field can
// take the result and mutate it.
func pkiAllReadable() PKIObservation {
	return PKIObservation{
		CACertPresent:    true,
		CACertReadable:   true,
		NodeCertPresent:  true,
		NodeCertReadable: true,
		NodeKeyPresent:   true,
		NodeKeyReadable:  true,
	}
}

func makeSnap(
	nodeID string, phase Phase,
	rel ReleaseInfo, bundle AwarenessBundleStatus,
	pki PKIObservation, scyllaConf ScyllaConfigObservation,
	svcs []ServiceObservation, ports []PortObservation,
) *NodeRuntimeSnapshot {
	return &NodeRuntimeSnapshot{
		NodeID:          nodeID,
		Phase:           phase,
		CollectedAt:     time.Now().UTC(),
		Release:         rel,
		AwarenessBundle: bundle,
		PKI:             pki,
		ScyllaConfig:    scyllaConf,
		Services:        svcs,
		Ports:           ports,
	}
}

func classifySnap(t *testing.T, snap *NodeRuntimeSnapshot) *Day1Verdict {
	t.Helper()
	snap.Facts = (&Normalizer{}).Normalize(snap)
	return (&Classifier{}).Classify(snap)
}

func assertAllowedContains(t *testing.T, actions []string, want string) {
	t.Helper()
	for _, a := range actions {
		if a == want {
			return
		}
	}
	t.Errorf("allowed_actions does not contain %q; got: %v", want, actions)
}

func assertForbiddenContains(t *testing.T, actions []string, want string) {
	t.Helper()
	for _, a := range actions {
		if a == want {
			return
		}
	}
	t.Errorf("forbidden_actions does not contain %q; got: %v", want, actions)
}

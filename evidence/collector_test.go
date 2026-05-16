package evidence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── release-index parsing ─────────────────────────────────────────────────────
//
// Regression tests for the composed-path failure recorded under
// "release-index version field divergence" in
// docs/awareness/composed_path_failures.md.
//
// Before the fix, the collector read field "version" only. The release-index
// writer ships "platform_release" (post-2026-05) and leaves "version" null;
// the empty parse triggered a misleading RELEASE_INDEX_MISSING fact even when
// the file was present and well-formed.

func TestParseReleaseIndex_PlatformReleaseField(t *testing.T) {
	data := []byte(`{"platform_release": "1.2.44", "release_tag": "v1.2.44"}`)
	got := parseReleaseIndex(data)
	if !got.Present {
		t.Fatal("Present must be true when payload parses")
	}
	if got.Version != "1.2.44" {
		t.Errorf("Version = %q, want %q", got.Version, "1.2.44")
	}
}

func TestParseReleaseIndex_LegacyVersionField(t *testing.T) {
	data := []byte(`{"version": "1.1.0", "build_id": "abc"}`)
	got := parseReleaseIndex(data)
	if !got.Present || got.Version != "1.1.0" || got.BuildID != "abc" {
		t.Errorf("got %+v, want Present+Version=1.1.0+BuildID=abc", got)
	}
}

func TestParseReleaseIndex_PlatformReleaseWinsOverLegacyVersion(t *testing.T) {
	// platform_release is canonical; if both are populated, prefer it.
	data := []byte(`{"platform_release": "1.2.44", "version": "stale"}`)
	got := parseReleaseIndex(data)
	if got.Version != "1.2.44" {
		t.Errorf("Version = %q, want %q (platform_release must win)", got.Version, "1.2.44")
	}
}

func TestParseReleaseIndex_PresentButEmpty(t *testing.T) {
	// File parses but contains no version field — present, but version unknown.
	data := []byte(`{"unrelated": "stuff"}`)
	got := parseReleaseIndex(data)
	if !got.Present {
		t.Error("Present must be true when file parses, even without version")
	}
	if got.Version != "" {
		t.Errorf("Version = %q, want empty", got.Version)
	}
}

func TestParseReleaseIndex_Malformed(t *testing.T) {
	data := []byte(`not json`)
	got := parseReleaseIndex(data)
	if !got.Present {
		t.Error("Present must be true when the file exists, even if malformed")
	}
}

func TestReadReleaseIndexFrom_FileMissing(t *testing.T) {
	got := readReleaseIndexFrom(filepath.Join(t.TempDir(), "nope.json"))
	if got.Present {
		t.Error("Present must be false when file does not exist")
	}
}

func TestReadReleaseIndexFrom_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release-index.json")
	if err := os.WriteFile(path,
		[]byte(`{"platform_release": "1.2.44"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readReleaseIndexFrom(path)
	if !got.Present || got.Version != "1.2.44" {
		t.Errorf("got %+v, want Present+Version=1.2.44", got)
	}
}

// Normalizer keys the MISSING fact off Present, not Version. A real
// release-index.json with platform_release set must not produce a missing fact.
func TestNormalizer_NoMissingFactWhenFilePresent(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase:           PhaseDAY1,
		Release:         ReleaseInfo{Present: true, Version: "1.2.44"},
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.44"},
	}
	facts := (&Normalizer{}).Normalize(snap)
	for _, f := range facts {
		if f.Kind == FactReleaseIndexMissing {
			t.Errorf("must not emit RELEASE_INDEX_MISSING when release-index is present: %+v", f)
		}
	}
}

func TestNormalizer_MissingFactWhenFileAbsent(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase:           PhaseDAY1,
		Release:         ReleaseInfo{}, // Present=false
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.44"},
	}
	facts := (&Normalizer{}).Normalize(snap)
	found := false
	for _, f := range facts {
		if f.Kind == FactReleaseIndexMissing {
			found = true
		}
	}
	if !found {
		t.Error("must emit RELEASE_INDEX_MISSING when Release.Present is false")
	}
}

// Present-but-empty version must NOT emit MISSING — the file IS there.
// This is the exact bug shape that produced the false positive in production.
func TestNormalizer_NoMissingFactWhenPresentButVersionEmpty(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase:           PhaseDAY1,
		Release:         ReleaseInfo{Present: true, Version: ""},
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.44"},
	}
	facts := (&Normalizer{}).Normalize(snap)
	for _, f := range facts {
		if f.Kind == FactReleaseIndexMissing {
			t.Errorf("file is present, must not emit RELEASE_INDEX_MISSING: %+v", f)
		}
	}
}

// ── /proc/net/tcp listening-port parsing ──────────────────────────────────────
//
// Regression tests for the composed-path failure recorded under
// "collector probes loopback while services bind to node IP" in
// docs/awareness/composed_path_failures.md.
//
// Before the fix, the collector dialed 127.0.0.1:<port> to test listeners,
// which violates the cluster's no-localhost contract: Scylla, etcd, and MinIO
// bind to the node's primary IP per CLAUDE.md hard rule #3. The loopback dial
// reported every service as down, cascading into bogus
// FactWorkflowRemediationUnsafe.

// procNetTCPFixture mirrors a real /proc/net/tcp listing. State 0A is LISTEN.
// Local addresses are deliberately a mix of 0.0.0.0, 127.0.0.1, and a fake
// node IP (10.0.0.63 → hex little-endian = 3F00000A) to prove the parser
// keys on port alone, not on the bind address.
const procNetTCPFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 3F00000A:2362 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:9510 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12346 1 0000000000000000 100 0 0 10 0
   2: 00000000:094C 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12347 1 0000000000000000 100 0 0 10 0
   3: 00000000:1F90 0100007F:B842 01 00000000:00000000 00:00000000 00000000     0        0 12348 1 0000000000000000 100 0 0 10 0
`

func TestParseListeningPorts_FixtureWithMultipleBindAddresses(t *testing.T) {
	out := map[int]bool{}
	parseListeningPorts(strings.NewReader(procNetTCPFixture), out)

	// Hex 2362 = 9058? Actually 0x2362=9058 — but I want 9042 (CQL). Let me
	// hand-build expected ports from the fixture:
	//   0x2362 = 9058  (line 0 — node IP)
	//   0x9510 = 38160 (line 1 — loopback)
	//   0x094C = 2380  (line 2 — wildcard)
	//   0x1F90 = 8080  (line 3 — ESTABLISHED, must NOT be reported)
	want := map[int]bool{9058: true, 38160: true, 2380: true}
	if len(out) != len(want) {
		t.Fatalf("port count: got %d (%v), want %d (%v)", len(out), out, len(want), want)
	}
	for p := range want {
		if !out[p] {
			t.Errorf("port %d expected listening, got %v", p, out)
		}
	}
	if out[8080] {
		t.Error("port 8080 was ESTABLISHED (state 01), must not be reported as listening")
	}
}

func TestParseListeningPorts_IgnoresHeaderAndBlankLines(t *testing.T) {
	// Header alone — no listeners. Must not panic, must return empty.
	out := map[int]bool{}
	parseListeningPorts(strings.NewReader("  sl  local_address rem_address st\n"), out)
	if len(out) != 0 {
		t.Errorf("header-only input must produce no entries, got %v", out)
	}
}

func TestListeningTCPPortsFromPaths_TolerantOfMissingFiles(t *testing.T) {
	got := listeningTCPPortsFromPaths([]string{filepath.Join(t.TempDir(), "absent")})
	if len(got) != 0 {
		t.Errorf("missing file must yield empty map, got %v", got)
	}
}

// Composed: collectPorts uses procNetTCPPaths and reports per-knownPort
// state from the kernel table. A fake fixture file simulates a listener
// on Scylla CQL (9042 = 0x2352) bound to the node IP.
func TestCollectPorts_DetectsListenerBoundToNodeIP(t *testing.T) {
	dir := t.TempDir()
	procFile := filepath.Join(dir, "tcp")
	// 0x2352 = 9042; local_address 3F00000A is 10.0.0.63 in /proc/net/tcp's
	// host-byte-order hex (little-endian on x86_64).
	fixture := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 3F00000A:2352 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(procFile, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := procNetTCPPaths
	procNetTCPPaths = []string{procFile}
	defer func() { procNetTCPPaths = orig }()

	coll := &Collector{}
	obs := coll.collectPorts(nil)

	var scyllaPort *PortObservation
	for i, p := range obs {
		if p.Port == 9042 {
			scyllaPort = &obs[i]
			break
		}
	}
	if scyllaPort == nil {
		t.Fatal("knownPorts must include 9042")
	}
	if !scyllaPort.Listening {
		t.Error("Scylla CQL (9042) listener bound to node IP must be reported as listening — " +
			"a loopback-only probe would have missed this. Composed-path failure regression.")
	}
}

// The real-world consequence: with Scylla actually listening on the node IP,
// the normalizer must NOT emit FactScyllaCQLUnreachable, which means it must
// also NOT emit FactWorkflowRemediationUnsafe. That cascade was the user-facing
// HIGH-severity false positive that triggered this fix.
func TestNormalizer_NoFalseScyllaCascade_WhenPortListeningOnNodeIP(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase:           PhaseDAY1,
		Release:         ReleaseInfo{Present: true, Version: "1.2.44"},
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.44"},
		Services: []ServiceObservation{
			{Name: "scylla", UnitName: "scylla-server.service", ActiveState: "active", SubState: "running"},
			{Name: "workflow", UnitName: "globular-workflow.service", ActiveState: "active", SubState: "running"},
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		Ports: []PortObservation{
			{Port: 9042, Protocol: "tcp", Listening: true},
			{Port: 2379, Protocol: "tcp", Listening: true},
		},
	}
	facts := (&Normalizer{}).Normalize(snap)
	for _, f := range facts {
		switch f.Kind {
		case FactScyllaCQLUnreachable:
			t.Errorf("FactScyllaCQLUnreachable must not fire when port 9042 is listening: %+v", f)
		case FactWorkflowRemediationUnsafe:
			t.Errorf("FactWorkflowRemediationUnsafe must not cascade from a non-existent Scylla outage: %+v", f)
		}
	}
}

// ── PKI file-state splitting (missing vs unreadable) ─────────────────────────
//
// Regression tests for the composed-path failure recorded under
// "PKI fileReadable conflates missing with not-readable" in
// docs/awareness/composed_path_failures.md.
//
// Before the fix, observeFile didn't exist; the collector only asked "can I
// open this file?" That bool was wired straight through as `*Present`,
// collapsing two operationally distinct states ("file gone" vs "file there
// but I'm running as the wrong user") into one. Both rendered as
// FactPKIMissing, prescribing the wrong remediation (re-issue from CA) for
// the second case.

func TestObserveFile_PresentAndReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exists, readable := observeFile(path)
	if !exists || !readable {
		t.Errorf("got exists=%v readable=%v, want both true", exists, readable)
	}
}

func TestObserveFile_Absent(t *testing.T) {
	exists, readable := observeFile(filepath.Join(t.TempDir(), "nope"))
	if exists || readable {
		t.Errorf("got exists=%v readable=%v, want both false", exists, readable)
	}
}

func TestObserveFile_PresentButUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — cannot exercise unreadable-by-current-process branch")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "locked")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod 0o000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	exists, readable := observeFile(path)
	if !exists {
		t.Error("file with mode 0o000 must still be reported exists=true")
	}
	if readable {
		t.Error("file with mode 0o000 must not be readable by the current process")
	}
}

func TestNormalizer_PKIMissing_WhenFileAbsent(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase: PhaseDAY1,
		PKI: PKIObservation{
			CACertPresent: false, CACertReadable: false,
			NodeCertPresent: true, NodeCertReadable: true,
			NodeKeyPresent: true, NodeKeyReadable: true,
		},
	}
	facts := (&Normalizer{}).Normalize(snap)
	if findFact(facts, FactPKIMissing) == nil {
		t.Error("must emit FactPKIMissing when an artifact is absent")
	}
	if findFact(facts, FactPKIUnreadable) != nil {
		t.Error("must NOT emit FactPKIUnreadable when the cause is absence — that would double-count")
	}
}

func TestNormalizer_PKIUnreadable_WhenPresentButNotReadable(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		Phase: PhaseDAY1,
		PKI: PKIObservation{
			CACertPresent: true, CACertReadable: true,
			NodeCertPresent: true, NodeCertReadable: true,
			// Key is the most common case: mode 0400 owned by service user.
			NodeKeyPresent: true, NodeKeyReadable: false,
		},
	}
	facts := (&Normalizer{}).Normalize(snap)
	if findFact(facts, FactPKIMissing) != nil {
		t.Error("must NOT emit FactPKIMissing — file is present, just unreadable")
	}
	f := findFact(facts, FactPKIUnreadable)
	if f == nil {
		t.Fatal("must emit FactPKIUnreadable when an artifact is present but unreadable")
	}
	if !strings.Contains(f.Detail, "service user") {
		t.Errorf("detail must guide toward ownership / running-as-service-user check; got: %q", f.Detail)
	}
}

func TestNormalizer_PKIMissingWinsOverUnreadable(t *testing.T) {
	// When one artifact is absent and another is unreadable, MISSING wins:
	// the remediations don't overlap and re-issuance is the bigger fix.
	snap := &NodeRuntimeSnapshot{
		Phase: PhaseDAY1,
		PKI: PKIObservation{
			CACertPresent: false, CACertReadable: false,
			NodeCertPresent: true, NodeCertReadable: true,
			NodeKeyPresent: true, NodeKeyReadable: false,
		},
	}
	facts := (&Normalizer{}).Normalize(snap)
	if findFact(facts, FactPKIMissing) == nil {
		t.Error("MISSING must win when both conditions apply")
	}
	if findFact(facts, FactPKIUnreadable) != nil {
		t.Error("must not emit UNREADABLE when MISSING already covers the verdict")
	}
}

func TestClassifier_PKIUnreadableBlocksDay1AndPicksRightClass(t *testing.T) {
	snap := &NodeRuntimeSnapshot{
		NodeID:          "node-x",
		Phase:           PhaseDAY1,
		Release:         ReleaseInfo{Present: true, Version: "1.2.44"},
		AwarenessBundle: AwarenessBundleStatus{Present: true, Status: "LOADED", Version: "1.2.44"},
		PKI: PKIObservation{
			CACertPresent: true, CACertReadable: true,
			NodeCertPresent: true, NodeCertReadable: true,
			NodeKeyPresent: true, NodeKeyReadable: false, // the production bite shape
		},
		Services: []ServiceObservation{
			{Name: "etcd", UnitName: "etcd.service", ActiveState: "active", SubState: "running"},
		},
		Ports: []PortObservation{{Port: 2379, Listening: true}},
	}
	snap.Facts = (&Normalizer{}).Normalize(snap)
	verdict := (&Classifier{}).Classify(snap)

	if verdict.Readiness[LevelPKIReady] {
		t.Error("PKI_READY must be false when an artifact is unreadable")
	}
	if verdict.Classification != ClassPKIUnreadable {
		t.Errorf("classification = %s, want %s", verdict.Classification, ClassPKIUnreadable)
	}
	if verdict.Verdict != "BLOCK" {
		t.Errorf("verdict = %s, want BLOCK", verdict.Verdict)
	}
	// Forbidden actions must explicitly rule out re-issuance — that's the
	// wrong remediation, and the bug we're guarding against was prescribing
	// exactly that.
	foundForbidden := false
	for _, a := range verdict.ForbiddenActions {
		if strings.Contains(a, "re-issue") {
			foundForbidden = true
			break
		}
	}
	if !foundForbidden {
		t.Errorf("ForbiddenActions must rule out re-issue under UNREADABLE; got %v", verdict.ForbiddenActions)
	}
}

func findFact(facts []RuntimeFact, kind FactKind) *RuntimeFact {
	for i, f := range facts {
		if f.Kind == kind {
			return &facts[i]
		}
	}
	return nil
}

// ── Node identity defaulting ─────────────────────────────────────────────────
//
// Before the fix, NewCollector(nodeID="") produced a Collector with
// NodeID="", and every emitted fact carried node_id="". On a multi-node
// cluster, two facts about the same symptom from two different nodes
// were indistinguishable in the wire format. Cross-node correlation —
// "ryzen says X but nuc says not-X" — was structurally impossible.
// The fix: defaulting to os.Hostname() at construction time matches
// the name the rest of the system already knows the node by (cluster_
// controller resolves UUID from hostname).

func TestNewCollector_DefaultsNodeIDToLocalHostname(t *testing.T) {
	want := localHostname()
	if want == "" {
		t.Skip("os.Hostname() returned empty — cannot exercise default")
	}
	c := NewCollector("", "", PhaseDAY1)
	if c.NodeID != want {
		t.Errorf("NodeID = %q, want %q (local hostname)", c.NodeID, want)
	}
}

func TestNewCollector_RespectsExplicitNodeID(t *testing.T) {
	c := NewCollector("explicit-node", "", PhaseDAY1)
	if c.NodeID != "explicit-node" {
		t.Errorf("NodeID = %q, want %q (explicit caller-provided id)", c.NodeID, "explicit-node")
	}
}

func TestCollect_PropagatesNodeIDIntoSnapshot(t *testing.T) {
	// End-to-end: NewCollector("") → Collect() → snapshot.NodeID is non-empty.
	// Pins the wiring all the way through, so emitted facts can be attributed.
	if localHostname() == "" {
		t.Skip("os.Hostname() returned empty — cannot exercise propagation")
	}
	c := NewCollector("", "", PhaseDAY1)
	snap := c.Collect(context.Background())
	if snap.NodeID == "" {
		t.Error("snapshot NodeID must be populated when collector was constructed with empty id")
	}
	if snap.NodeID != c.NodeID {
		t.Errorf("snapshot.NodeID = %q, want %q (collector NodeID)", snap.NodeID, c.NodeID)
	}
}

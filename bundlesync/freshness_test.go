package bundlesync

import "testing"

// ── Phase B.1 freshness tests ────────────────────────────────────────────────
//
// Spec acceptance:
//   1. Fresh Bundle Passes
//   2. Stale Bundle Fails Readiness
//   (3. Runtime failure does not mark graph stale — lives in the evidence package)
//   (4. Runtime MCP does not generate graph — already enforced in Phase B serve tools)
//   (5. Dev profile build allowed only in dev profile — no build tools exist in mcp,
//        the negative test is structural and is asserted by the absence of tools)
//   (6. Sync fixes stale bundle — Phase C territory)
//
// The four tests below pin the in-package contract:
//   - happy path (no local binary)
//   - happy path (with matching local binary)
//   - STALE when build_id drifts on a matching version (matches the new rule)
//   - MISMATCH when version drifts (still distinguishable from STALE)
//   - SCHEMA_UNSUPPORTED stays SCHEMA_UNSUPPORTED through the orchestrator
//   - LocalBinary mismatch downgrades freshness even when bundle matches release
//   - Empty LocalBinary fields are NOT a hard failure (per spec)

func freshnessFixture() (*Manifest, *ReleaseIndex) {
	m := &Manifest{
		Name:          BundleName,
		Version:       "v1.2.30",
		BuildID:       "abc123",
		SchemaVersion: "awareness.bundle.v1",
		SHA256:        "f00d",
		GraphHash:     "deadbeef",
		SourceCommit:  "git-abc",
	}
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	return m, ri
}

// 1a. Fresh bundle (no local binary supplied) passes.
func TestFreshnessHappyPathNoLocalBinary(t *testing.T) {
	m, ri := freshnessFixture()
	r := CheckAwarenessFreshness(m, ri, nil)
	if !r.OK {
		t.Fatalf("OK=false; state=%s reason=%s", r.State, r.Reason)
	}
	if r.State != StateAwarenessReady {
		t.Errorf("state=%s, want AWARENESS_READY", r.State)
	}
	if !r.VersionMatchesRelease || !r.BuildIDMatchesRelease {
		t.Errorf("expected version/build matches; got %+v", r)
	}
	if !r.GraphHashPresent || !r.SourceCommitPresent {
		t.Errorf("optional fields should be flagged present: %+v", r)
	}
}

// 1b. Fresh bundle with matching local binary passes.
func TestFreshnessHappyPathLocalBinaryMatches(t *testing.T) {
	m, ri := freshnessFixture()
	lb := &LocalBinaryInfo{Version: m.Version, BuildID: m.BuildID}
	r := CheckAwarenessFreshness(m, ri, lb)
	if !r.OK {
		t.Fatalf("OK=false; state=%s", r.State)
	}
	if !r.LocalBinaryVersionMatch || !r.LocalBinaryBuildIDMatch {
		t.Errorf("expected local binary match flags true; got %+v", r)
	}
}

// 2. Stale bundle (build_id drift, version matches) fails with AWARENESS_BUNDLE_STALE.
// AWARENESS_READY must NOT be reported.
func TestFreshnessStaleOnBuildIDDrift(t *testing.T) {
	m, ri := freshnessFixture()
	ri.BuildID = "old999"

	r := CheckAwarenessFreshness(m, ri, nil)
	if r.OK {
		t.Fatal("OK=true for stale bundle")
	}
	if r.State != StateAwarenessBundleStale {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_STALE", r.State)
	}
	if r.VersionMatchesRelease != true {
		t.Error("VersionMatchesRelease should be true for build_id-only drift")
	}
	if r.BuildIDMatchesRelease != false {
		t.Error("BuildIDMatchesRelease should be false")
	}
}

// 2b. Mismatch (version drift) reports MISMATCH, not STALE.
func TestFreshnessMismatchOnVersionDrift(t *testing.T) {
	m, ri := freshnessFixture()
	ri.Version = "v9.9.99"

	r := CheckAwarenessFreshness(m, ri, nil)
	if r.OK {
		t.Fatal("OK=true for version mismatch")
	}
	if r.State != StateAwarenessBundleMismatch {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_MISMATCH", r.State)
	}
}

// SCHEMA_UNSUPPORTED is preserved through the orchestrator — it is not
// flattened into VERIFY_FAILED.
func TestFreshnessSchemaUnsupportedPreserved(t *testing.T) {
	m, ri := freshnessFixture()
	m.SchemaVersion = "awareness.bundle.v99"

	r := CheckAwarenessFreshness(m, ri, nil)
	if r.OK {
		t.Fatal("OK=true for unsupported schema")
	}
	if r.State != StateAwarenessBundleSchemaUnsupported {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED", r.State)
	}
	if r.SchemaSupported {
		t.Error("SchemaSupported should be false")
	}
}

// LocalBinary version drift fails freshness even when bundle matches release-index.
// This catches the "binary upgraded, bundle didn't" case.
func TestFreshnessLocalBinaryVersionDriftFails(t *testing.T) {
	m, ri := freshnessFixture()
	lb := &LocalBinaryInfo{Version: "v1.2.31", BuildID: m.BuildID}

	r := CheckAwarenessFreshness(m, ri, lb)
	if r.OK {
		t.Fatal("OK=true despite local binary version drift")
	}
	if r.State != StateAwarenessBundleMismatch {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_MISMATCH for binary/bundle version drift", r.State)
	}
	if r.LocalBinaryVersionMatch {
		t.Error("LocalBinaryVersionMatch should be false")
	}
}

// LocalBinary build_id drift (with matching version) reads as STALE.
func TestFreshnessLocalBinaryBuildIDDriftFails(t *testing.T) {
	m, ri := freshnessFixture()
	lb := &LocalBinaryInfo{Version: m.Version, BuildID: "old999"}

	r := CheckAwarenessFreshness(m, ri, lb)
	if r.OK {
		t.Fatal("OK=true despite local binary build_id drift")
	}
	if r.State != StateAwarenessBundleStale {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_STALE", r.State)
	}
	if r.LocalBinaryVersionMatch != true {
		t.Error("LocalBinaryVersionMatch should be true (versions matched)")
	}
	if r.LocalBinaryBuildIDMatch != false {
		t.Error("LocalBinaryBuildIDMatch should be false")
	}
}

// Empty LocalBinary fields are NOT a hard failure — the spec says missing
// build info should not block readiness.
func TestFreshnessEmptyLocalBinaryFieldsAreSoft(t *testing.T) {
	m, ri := freshnessFixture()

	cases := []struct {
		name string
		lb   *LocalBinaryInfo
	}{
		{"both empty", &LocalBinaryInfo{}},
		{"only version", &LocalBinaryInfo{Version: m.Version}},
		{"only build_id", &LocalBinaryInfo{BuildID: m.BuildID}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := CheckAwarenessFreshness(m, ri, c.lb)
			if !r.OK {
				t.Errorf("OK=false; missing local binary fields must be soft. state=%s reason=%s", r.State, r.Reason)
			}
		})
	}
}

// Nil release-index reports VERIFY_FAILED with a clear reason — we cannot
// decide freshness without authority.
func TestFreshnessNilReleaseIndex(t *testing.T) {
	m, _ := freshnessFixture()
	r := CheckAwarenessFreshness(m, nil, nil)
	if r.OK {
		t.Fatal("OK=true with nil release-index")
	}
	if r.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_VERIFY_FAILED", r.State)
	}
}

// Nil manifest reports MISSING. The cold-bootstrap state is the right one
// here: there is literally no manifest to evaluate.
func TestFreshnessNilManifest(t *testing.T) {
	_, ri := freshnessFixture()
	r := CheckAwarenessFreshness(nil, ri, nil)
	if r.OK {
		t.Fatal("OK=true with nil manifest")
	}
	if r.State != StateAwarenessBundleMissing {
		t.Errorf("state=%s, want AWARENESS_BUNDLE_MISSING", r.State)
	}
}

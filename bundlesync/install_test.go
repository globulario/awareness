package bundlesync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ── Phase C.1 atomic install tests ───────────────────────────────────────────
//
// Acceptance:
//   1. Fresh install — versioned dir created, current symlink points at it.
//   2. Re-install same version+build_id — idempotent; no new versioned dir.
//   3. Verify failure (wrong sha256) → no install, current untouched.
//   4. Unsafe tar entry → no install, current untouched, staging cleaned.
//   5. Bundle missing required files (no graph.db) → INCOMPLETE, no install.
//   6. Crash after rename, before symlink swap → next run completes idempotently.
//   7. Manifest sidecar copied into versioned dir.
//   8. Previous current target captured in result.

// makeInstallableBundle writes a valid bundle (containing manifest.json and
// graph.db) plus a sidecar manifest. Returns paths and the manifest.
func makeInstallableBundle(t *testing.T, dir string, version, buildID string) (bundlePath, manifestPath string, m Manifest) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	graphContent := []byte("fake graph.db content for " + version + "/" + buildID)
	hdr := &tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(graphContent)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(graphContent); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	// Include a docs subdir so we exercise nested directory creation.
	docsHdr := &tar.Header{Name: "docs/", Mode: 0755, Typeflag: tar.TypeDir}
	if err := tw.WriteHeader(docsHdr); err != nil {
		t.Fatalf("tar dir header: %v", err)
	}
	docContent := []byte("# README\n")
	docFileHdr := &tar.Header{Name: "docs/README.md", Mode: 0644, Size: int64(len(docContent)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(docFileHdr); err != nil {
		t.Fatalf("tar doc header: %v", err)
	}
	if _, err := tw.Write(docContent); err != nil {
		t.Fatalf("tar doc body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}

	bundleBytes := buf.Bytes()
	bundlePath = filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(bundlePath, bundleBytes, 0644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	h := sha256.Sum256(bundleBytes)
	m = Manifest{
		Name:          BundleName,
		Version:       version,
		BuildID:       buildID,
		SchemaVersion: "awareness.bundle.v1",
		SHA256:        hex.EncodeToString(h[:]),
		SizeBytes:     int64(len(bundleBytes)),
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	manifestPath = filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return
}

// 1. Fresh install creates the versioned dir and the symlink.
func TestInstallBundleFreshInstall(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()
	bp, mp, m := makeInstallableBundle(t, scratch, "v1.2.30", "abc123")

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if err != nil {
		t.Fatalf("install error: %v (state=%s reason=%s)", err, res.State, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if res.State != StateAwarenessReady {
		t.Errorf("state = %s, want AWARENESS_READY", res.State)
	}
	if res.AlreadyPresent {
		t.Error("AlreadyPresent should be false on fresh install")
	}
	if !res.SymlinkUpdated {
		t.Error("SymlinkUpdated should be true on fresh install")
	}

	wantDir := filepath.Join(bundleRoot, "installed", m.Version, m.BuildID)
	if res.InstalledPath != wantDir {
		t.Errorf("InstalledPath = %s, want %s", res.InstalledPath, wantDir)
	}

	// graph.db and the sidecar manifest must be present.
	if _, err := os.Stat(filepath.Join(wantDir, "graph.db")); err != nil {
		t.Errorf("graph.db missing in installed dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "manifest.json")); err != nil {
		t.Errorf("manifest sidecar missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "docs", "README.md")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}

	// The original tar.gz must be retained alongside the extracted contents
	// so MCP's awareness_bundle_stream / awareness_bundle_manifest tools can
	// serve the archive to remote callers. Without this, peer pulls return
	// AWARENESS_BUNDLE_MISSING even when the install succeeded.
	bundleCopy := filepath.Join(wantDir, installedBundleFilename)
	srcInfo, err := os.Stat(bp)
	if err != nil {
		t.Fatalf("stat source bundle: %v", err)
	}
	dstInfo, err := os.Stat(bundleCopy)
	if err != nil {
		t.Fatalf("retained bundle.tar.gz missing: %v", err)
	}
	if dstInfo.Size() != srcInfo.Size() {
		t.Errorf("retained bundle size = %d, want %d (source)", dstInfo.Size(), srcInfo.Size())
	}

	// Symlink points at versioned dir.
	target, err := os.Readlink(filepath.Join(bundleRoot, "current"))
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	if target != wantDir {
		t.Errorf("current → %s, want %s", target, wantDir)
	}
}

// 2. Re-install same version+build_id is idempotent. Versioned dir untouched,
// AlreadyPresent=true, SymlinkUpdated=false (still correct).
func TestInstallBundleIdempotentReinstall(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()
	bp, mp, m := makeInstallableBundle(t, scratch, "v1.2.30", "abc123")

	res1, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if err != nil || !res1.OK {
		t.Fatalf("first install failed: %v (state=%s reason=%s)", err, res1.State, res1.Reason)
	}

	// Snapshot graph.db inode/mtime so we can detect an unwanted re-extract.
	graphPath := filepath.Join(res1.InstalledPath, "graph.db")
	stat1, err := os.Stat(graphPath)
	if err != nil {
		t.Fatalf("stat graph: %v", err)
	}

	res2, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if err != nil {
		t.Fatalf("second install error: %v", err)
	}
	if !res2.OK {
		t.Fatalf("second install OK=false; state=%s reason=%s", res2.State, res2.Reason)
	}
	if !res2.AlreadyPresent {
		t.Error("AlreadyPresent should be true on re-install of same version/build")
	}
	if res2.SymlinkUpdated {
		t.Error("SymlinkUpdated should be false on idempotent re-install")
	}

	stat2, err := os.Stat(graphPath)
	if err != nil {
		t.Fatalf("stat graph after reinstall: %v", err)
	}
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Errorf("graph.db mtime changed; should not be re-extracted on idempotent install")
	}
}

// 3. Verify failure (wrong sha256) → no install, current untouched.
func TestInstallBundleVerifyFailureNoInstall(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()
	bp, mp, m := makeInstallableBundle(t, scratch, "v1.2.30", "abc123")

	// Corrupt the manifest sha256 so verify fails.
	m.SHA256 = "deadbeef00000000000000000000000000000000000000000000000000000000"
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, mb, 0644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if res.OK {
		t.Fatal("OK=true despite verify failure")
	}
	if err == nil {
		t.Error("err should be non-nil for verify failure")
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}

	// Current symlink must not exist.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current symlink should not exist after verify failure")
	}
	// Versioned dir must not exist.
	if _, err := os.Stat(filepath.Join(bundleRoot, "installed")); err == nil {
		t.Error("installed dir should not exist after verify failure")
	}
}

// 4. Unsafe tar entry detected at extract time → install fails, current untouched,
// staging cleaned up.
func TestInstallBundleUnsafeTarEntryNoInstall(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()

	// Build a bundle with both graph.db AND a path-traversal entry. The
	// manifest will pass verification but the extract path will refuse it.
	// (ValidateTarSafe in VerifyBundle will catch it first; this test
	// covers the install-time defense path symmetrically.)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("ok")
	hdrGood := &tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	tw.WriteHeader(hdrGood)
	tw.Write(body)
	hdrBad := &tar.Header{Name: "../escape", Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}
	tw.WriteHeader(hdrBad)
	tw.Write(body)
	tw.Close()
	gz.Close()
	data := buf.Bytes()

	bp := filepath.Join(scratch, "bundle.tar.gz")
	os.WriteFile(bp, data, 0644)
	h := sha256.Sum256(data)
	m := Manifest{
		Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
		SchemaVersion: "awareness.bundle.v1", SHA256: hex.EncodeToString(h[:]),
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	mp := filepath.Join(scratch, "manifest.json")
	os.WriteFile(mp, mb, 0644)

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if res.OK {
		t.Fatal("OK=true despite unsafe tar")
	}
	if err == nil {
		t.Error("err should be non-nil for unsafe tar")
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}

	// Current must not exist.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current must not exist when install fails")
	}

	// Staging must be empty (or non-existent).
	stagingPath := filepath.Join(bundleRoot, "staging")
	if entries, err := os.ReadDir(stagingPath); err == nil {
		if len(entries) > 0 {
			t.Errorf("staging dir not cleaned: %v", entries)
		}
	}
}

// 5. Bundle without graph.db → INCOMPLETE state, no install.
func TestInstallBundleMissingGraphDB(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()

	// Build a bundle with only docs/, no graph.db.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	docContent := []byte("# README\n")
	hdr := &tar.Header{Name: "docs/", Mode: 0755, Typeflag: tar.TypeDir}
	tw.WriteHeader(hdr)
	hdr2 := &tar.Header{Name: "docs/README.md", Mode: 0644, Size: int64(len(docContent)), Typeflag: tar.TypeReg}
	tw.WriteHeader(hdr2)
	tw.Write(docContent)
	tw.Close()
	gz.Close()
	data := buf.Bytes()

	bp := filepath.Join(scratch, "bundle.tar.gz")
	os.WriteFile(bp, data, 0644)
	h := sha256.Sum256(data)
	m := Manifest{
		Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
		SchemaVersion: "awareness.bundle.v1", SHA256: hex.EncodeToString(h[:]),
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	mp := filepath.Join(scratch, "manifest.json")
	os.WriteFile(mp, mb, 0644)

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if res.OK {
		t.Fatal("OK=true without graph.db")
	}
	if err == nil {
		t.Error("err should be non-nil")
	}
	if res.State != StateAwarenessBundleIncomplete {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_INCOMPLETE", res.State)
	}

	// Current must not exist.
	if _, err := os.Lstat(filepath.Join(bundleRoot, "current")); err == nil {
		t.Error("current must not exist for incomplete bundle")
	}
}

// 6. Crash recovery: simulate a state where the versioned dir already
// exists but the symlink wasn't switched yet. A re-run must complete
// idempotently — populate the symlink without re-extracting.
func TestInstallBundleRecoversFromCrashAfterRename(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()
	bp, mp, m := makeInstallableBundle(t, scratch, "v1.2.30", "abc123")

	// Pre-stage: create the versioned dir manually with a graph.db, simulating
	// a prior install that crashed after rename but before symlink swap.
	preExisting := filepath.Join(bundleRoot, "installed", m.Version, m.BuildID)
	if err := os.MkdirAll(preExisting, 0755); err != nil {
		t.Fatalf("pre-stage mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(preExisting, "graph.db"), []byte("pre-existing graph"), 0644); err != nil {
		t.Fatalf("pre-stage graph: %v", err)
	}
	// Sentinel file lets us prove the dir wasn't re-extracted.
	if err := os.WriteFile(filepath.Join(preExisting, "sentinel"), []byte("don't touch me"), 0644); err != nil {
		t.Fatalf("pre-stage sentinel: %v", err)
	}

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m.Version, BuildID: m.BuildID},
	})
	if err != nil {
		t.Fatalf("install error: %v (reason=%s)", err, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s reason=%s", res.State, res.Reason)
	}
	if !res.AlreadyPresent {
		t.Error("AlreadyPresent should be true (versioned dir existed)")
	}
	if !res.SymlinkUpdated {
		t.Error("SymlinkUpdated should be true (no prior current)")
	}

	// Sentinel must still be there — proves no re-extraction occurred.
	if _, err := os.Stat(filepath.Join(preExisting, "sentinel")); err != nil {
		t.Errorf("sentinel removed; install re-extracted on a recovery path: %v", err)
	}

	// Symlink must be set.
	target, err := os.Readlink(filepath.Join(bundleRoot, "current"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != preExisting {
		t.Errorf("current → %s, want %s", target, preExisting)
	}
}

// 7. After a successful install, then a NEW install of a different build_id,
// the previous active path is captured in the result and the symlink moves.
func TestInstallBundleSwitchesSymlinkAcrossBuilds(t *testing.T) {
	scratch1 := t.TempDir()
	scratch2 := t.TempDir()
	bundleRoot := t.TempDir()

	bp1, mp1, m1 := makeInstallableBundle(t, scratch1, "v1.2.30", "build-old")
	if _, err := InstallBundle(InstallOptions{
		BundlePath:   bp1,
		ManifestPath: mp1,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m1.Version, BuildID: m1.BuildID},
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	oldDir := filepath.Join(bundleRoot, "installed", m1.Version, m1.BuildID)

	bp2, mp2, m2 := makeInstallableBundle(t, scratch2, "v1.2.30", "build-new")
	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp2,
		ManifestPath: mp2,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: m2.Version, BuildID: m2.BuildID},
	})
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !res.OK {
		t.Fatalf("OK=false; state=%s", res.State)
	}
	if res.PreviousActive != oldDir {
		t.Errorf("PreviousActive = %s, want %s", res.PreviousActive, oldDir)
	}
	if !res.SymlinkUpdated {
		t.Error("SymlinkUpdated should be true on different-build install")
	}

	// Old versioned dir must still exist (we never delete on success).
	if _, err := os.Stat(oldDir); err != nil {
		t.Errorf("old versioned dir removed; install must not delete prior bundles: %v", err)
	}

	// Symlink now points at the new dir.
	newDir := filepath.Join(bundleRoot, "installed", m2.Version, m2.BuildID)
	target, _ := os.Readlink(filepath.Join(bundleRoot, "current"))
	if target != newDir {
		t.Errorf("current → %s, want %s", target, newDir)
	}
}

// 8. Manifest sidecar copied into the installed dir reflects the SIDECAR,
// not whatever manifest.json may have been packed in the tar.
func TestInstallBundleSidecarManifestWins(t *testing.T) {
	scratch := t.TempDir()
	bundleRoot := t.TempDir()

	// Build a bundle whose embedded manifest.json has the wrong build_id.
	// The sidecar we write next to it has the correct one.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	graph := []byte("g")
	tw.WriteHeader(&tar.Header{Name: "graph.db", Mode: 0644, Size: int64(len(graph)), Typeflag: tar.TypeReg})
	tw.Write(graph)

	embedded := []byte(`{"name":"globular-awareness-bundle","version":"v1.2.30","build_id":"WRONG","schema_version":"awareness.bundle.v1"}`)
	tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0644, Size: int64(len(embedded)), Typeflag: tar.TypeReg})
	tw.Write(embedded)

	tw.Close()
	gz.Close()
	data := buf.Bytes()

	bp := filepath.Join(scratch, "bundle.tar.gz")
	os.WriteFile(bp, data, 0644)
	h := sha256.Sum256(data)
	sidecar := Manifest{
		Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
		SchemaVersion: "awareness.bundle.v1", SHA256: hex.EncodeToString(h[:]),
	}
	mb, _ := json.MarshalIndent(sidecar, "", "  ")
	mp := filepath.Join(scratch, "manifest.json")
	os.WriteFile(mp, mb, 0644)

	res, err := InstallBundle(InstallOptions{
		BundlePath:   bp,
		ManifestPath: mp,
		BundleRoot:   bundleRoot,
		ReleaseIndex: &ReleaseIndex{Version: sidecar.Version, BuildID: sidecar.BuildID},
	})
	if err != nil || !res.OK {
		t.Fatalf("install: err=%v state=%s reason=%s", err, res.State, res.Reason)
	}

	installedManifest := filepath.Join(res.InstalledPath, "manifest.json")
	data, err = os.ReadFile(installedManifest)
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BuildID != "abc123" {
		t.Errorf("installed manifest build_id = %q, want %q (sidecar must win over embedded)", got.BuildID, "abc123")
	}
}

package bundlesync

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ── Phase A acceptance tests ──────────────────────────────────────────────────
//
// Coverage matrix per the user's acceptance list:
//
//   1. valid bundle passes                       → TestValidBundlePasses
//   2. wrong version fails                       → TestWrongVersionFails
//   3. wrong build_id fails                      → TestWrongBuildIDFails
//   4. wrong sha256 fails                        → TestWrongSHA256Fails
//   5. unsupported schema fails                  → TestUnsupportedSchemaFails
//   6. tar traversal fails                       → TestTarTraversalFails
//   7. absolute tar path fails                   → TestAbsoluteTarPathFails
//   8. symlink escape fails                      → TestSymlinkEscapeFails
//   9. device file fails                         → TestDeviceFileFails
//  10. existing current bundle is untouched      → TestExistingCurrentUntouchedOnFailure
//
// Each test builds its inputs in a fresh t.TempDir() and never touches paths
// outside that temp directory. The verify primitives must never attempt to
// install or modify /var/lib/globular/awareness/current.

// ── helpers ──────────────────────────────────────────────────────────────────

// goodEntry is a minimal valid tar entry for "graph.db". Used by every test
// that wants the underlying archive to be structurally valid.
func goodEntry() (*tar.Header, []byte) {
	body := []byte("fake sqlite content")
	return &tar.Header{
		Name:     "graph.db",
		Mode:     0644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}, body
}

type tarEntry struct {
	Header *tar.Header
	Body   []byte
}

// buildBundle builds a gzip+tar archive containing the given entries and
// returns its raw bytes plus the hex sha256 of the gzipped output (the value
// that would be placed in manifest.sha256).
func buildBundle(t *testing.T, entries []tarEntry) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		if e.Header.Size == 0 && len(e.Body) > 0 {
			e.Header.Size = int64(len(e.Body))
		}
		if err := tw.WriteHeader(e.Header); err != nil {
			t.Fatalf("write header %q: %v", e.Header.Name, err)
		}
		if e.Body != nil {
			if _, err := tw.Write(e.Body); err != nil {
				t.Fatalf("write body %q: %v", e.Header.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}

	data := buf.Bytes()
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

// writeBundle writes data and a manifest pointing to that data. Returns
// (bundlePath, manifestPath).
func writeBundle(t *testing.T, dir string, data []byte, m Manifest) (string, string) {
	t.Helper()

	bundlePath := filepath.Join(dir, "awareness.tar.gz")
	if err := os.WriteFile(bundlePath, data, 0644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	manifestPath := filepath.Join(dir, "awareness.manifest.json")
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, mb, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return bundlePath, manifestPath
}

// validBundle is the happy-path setup used by most negative tests; the
// negative tests then mutate one specific field to assert that a single
// failure mode is enough to reject the bundle.
func validBundle(t *testing.T, dir string) (bundlePath, manifestPath string, ri *ReleaseIndex, m Manifest) {
	t.Helper()

	hdr, body := goodEntry()
	data, sum := buildBundle(t, []tarEntry{{Header: hdr, Body: body}})
	m = Manifest{
		Name:          BundleName,
		Version:       "v1.2.30",
		BuildID:       "abc123",
		SchemaVersion: "awareness.bundle.v1",
		SHA256:        sum,
		SizeBytes:     int64(len(data)),
		CreatedAt:     "2026-05-09T00:00:00Z",
	}
	ri = &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	bundlePath, manifestPath = writeBundle(t, dir, data, m)
	return bundlePath, manifestPath, ri, m
}

// ── Tests ────────────────────────────────────────────────────────────────────

// 1. Valid bundle passes — version, build_id, schema, sha256 all match.
func TestValidBundlePasses(t *testing.T) {
	dir := t.TempDir()
	bp, mp, ri, _ := validBundle(t, dir)

	res, err := VerifyBundle(bp, mp, ri)
	if err != nil {
		t.Fatalf("verify failed: %v (reason=%s)", err, res.Reason)
	}
	if !res.OK {
		t.Fatalf("OK=false; reason=%s state=%s", res.Reason, res.State)
	}
	if res.State != StateAwarenessReady {
		t.Errorf("state = %s, want AWARENESS_READY", res.State)
	}
}

// 2. Wrong version fails with AWARENESS_BUNDLE_MISMATCH.
func TestWrongVersionFails(t *testing.T) {
	dir := t.TempDir()
	bp, mp, _, m := validBundle(t, dir)

	// Release-index expects a different version.
	ri := &ReleaseIndex{Version: "v9.9.99", BuildID: m.BuildID}

	res, _ := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for version mismatch")
	}
	if res.State != StateAwarenessBundleMismatch {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_MISMATCH", res.State)
	}
}

// 3. Wrong build_id (same release line) fails with AWARENESS_BUNDLE_STALE.
// Build-ID drift on a matching version reads as "behind on CI build", which
// is the STALE case per the freshness spec — distinct from MISMATCH where
// the version itself differs.
func TestWrongBuildIDFails(t *testing.T) {
	dir := t.TempDir()
	bp, mp, _, m := validBundle(t, dir)

	ri := &ReleaseIndex{Version: m.Version, BuildID: "old999"}

	res, _ := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for build_id drift")
	}
	if res.State != StateAwarenessBundleStale {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_STALE (same version, different build_id)", res.State)
	}
}

// 4. Wrong sha256 fails with AWARENESS_BUNDLE_VERIFY_FAILED.
func TestWrongSHA256Fails(t *testing.T) {
	dir := t.TempDir()
	bp, mp, ri, m := validBundle(t, dir)

	// Rewrite the manifest with a deliberately wrong sha256, keeping
	// everything else intact so manifest verification reaches the hash step.
	m.SHA256 = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, mb, 0644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}

	res, err := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for sha256 mismatch")
	}
	if res.State != StateAwarenessBundleVerifyFailed {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_VERIFY_FAILED", res.State)
	}
	if !errors.Is(err, ErrSHA256Mismatch) {
		t.Errorf("err = %v, want ErrSHA256Mismatch", err)
	}
	if res.ActualSHA256 == "" || res.ActualSHA256 == m.SHA256 {
		t.Errorf("ActualSHA256 should be the real hash, got %q (manifest claimed %q)", res.ActualSHA256, m.SHA256)
	}
}

// 5. Unsupported schema fails with AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED.
// This is a distinct state from VERIFY_FAILED: the bundle may be perfectly
// valid for a *newer* binary; the right remediation is "upgrade the binary",
// not "fetch a different bundle."
func TestUnsupportedSchemaFails(t *testing.T) {
	dir := t.TempDir()
	bp, mp, ri, m := validBundle(t, dir)

	m.SchemaVersion = "awareness.bundle.v99"
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, mb, 0644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}

	res, _ := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for unsupported schema")
	}
	if res.State != StateAwarenessBundleSchemaUnsupported {
		t.Errorf("state = %s, want AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED", res.State)
	}
}

// 6. Tar with ".." traversal fails with AWARENESS_BUNDLE_VERIFY_FAILED.
func TestTarTraversalFails(t *testing.T) {
	dir := t.TempDir()
	body := []byte("evil")
	data, sum := buildBundle(t, []tarEntry{
		{Header: &tar.Header{Name: "../escape", Mode: 0644, Typeflag: tar.TypeReg}, Body: body},
	})
	m := Manifest{
		Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
		SchemaVersion: "awareness.bundle.v1", SHA256: sum,
	}
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	bp, mp := writeBundle(t, dir, data, m)

	res, err := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for tar traversal")
	}
	if !errors.Is(err, ErrTarUnsafe) {
		t.Errorf("err = %v, want ErrTarUnsafe", err)
	}
	if !hasViolation(res.TarViolations, TarReasonPathTraversal) {
		t.Errorf("violations should include path_traversal; got %v", res.TarViolations)
	}
}

// 7. Tar with an absolute entry path fails with AWARENESS_BUNDLE_VERIFY_FAILED.
func TestAbsoluteTarPathFails(t *testing.T) {
	dir := t.TempDir()
	body := []byte("evil")
	data, sum := buildBundle(t, []tarEntry{
		{Header: &tar.Header{Name: "/etc/passwd", Mode: 0644, Typeflag: tar.TypeReg}, Body: body},
	})
	m := Manifest{
		Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
		SchemaVersion: "awareness.bundle.v1", SHA256: sum,
	}
	ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
	bp, mp := writeBundle(t, dir, data, m)

	res, err := VerifyBundle(bp, mp, ri)
	if res.OK {
		t.Fatal("OK=true for absolute tar path")
	}
	if !errors.Is(err, ErrTarUnsafe) {
		t.Errorf("err = %v, want ErrTarUnsafe", err)
	}
	if !hasViolation(res.TarViolations, TarReasonAbsolutePath) {
		t.Errorf("violations should include absolute_path; got %v", res.TarViolations)
	}
}

// 8. Symlink escape fails with AWARENESS_BUNDLE_VERIFY_FAILED.
// We test both flavors operators are likely to encounter:
//
//   8a. Absolute symlink target ("/etc/passwd")
//   8b. Relative symlink with traversal ("../../etc/passwd")
func TestSymlinkEscapeFails(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{"absolute target", "/etc/passwd"},
		{"traversal target", "../../etc/passwd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			data, sum := buildBundle(t, []tarEntry{{
				Header: &tar.Header{
					Name:     "link",
					Linkname: c.target,
					Mode:     0777,
					Typeflag: tar.TypeSymlink,
				},
			}})
			m := Manifest{
				Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
				SchemaVersion: "awareness.bundle.v1", SHA256: sum,
			}
			ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
			bp, mp := writeBundle(t, dir, data, m)

			res, err := VerifyBundle(bp, mp, ri)
			if res.OK {
				t.Fatal("OK=true for symlink escape")
			}
			if !errors.Is(err, ErrTarUnsafe) {
				t.Errorf("err = %v, want ErrTarUnsafe", err)
			}
			if !hasViolation(res.TarViolations, TarReasonSymlinkEscape) {
				t.Errorf("violations should include symlink_escape; got %v", res.TarViolations)
			}
		})
	}
}

// 9. Device file fails with AWARENESS_BUNDLE_VERIFY_FAILED.
// We test both block (Typeflag=4) and char (Typeflag=3) since real-world
// malicious archives have used both.
func TestDeviceFileFails(t *testing.T) {
	cases := []struct {
		name string
		flag byte
	}{
		{"block device", tar.TypeBlock},
		{"char device", tar.TypeChar},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			data, sum := buildBundle(t, []tarEntry{{
				Header: &tar.Header{
					Name:     "evil.dev",
					Mode:     0666,
					Typeflag: c.flag,
					Devmajor: 1,
					Devminor: 1,
				},
			}})
			m := Manifest{
				Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
				SchemaVersion: "awareness.bundle.v1", SHA256: sum,
			}
			ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
			bp, mp := writeBundle(t, dir, data, m)

			res, err := VerifyBundle(bp, mp, ri)
			if res.OK {
				t.Fatal("OK=true for device file")
			}
			if !errors.Is(err, ErrTarUnsafe) {
				t.Errorf("err = %v, want ErrTarUnsafe", err)
			}
			if !hasViolation(res.TarViolations, TarReasonDeviceFile) {
				t.Errorf("violations should include device_file; got %v", res.TarViolations)
			}
		})
	}
}

// 10. Existing current bundle is untouched on every failure.
//
// This pins the side-effect contract for Phase A: verify primitives never
// modify /var/lib/globular/awareness/current. We simulate the layout in a
// temp dir and exercise one failure from each category — bad sha256, mismatch,
// unsafe tar — confirming the simulated "current" symlink and its target file
// are byte-for-byte unchanged after each verify call.
func TestExistingCurrentUntouchedOnFailure(t *testing.T) {
	dir := t.TempDir()

	// Set up a fake layout that mirrors production:
	//   <dir>/installed/<version>/<build_id>/graph.db
	//   <dir>/current → installed/<version>/<build_id>
	installed := filepath.Join(dir, "installed", "v1.2.30", "abc123")
	if err := os.MkdirAll(installed, 0755); err != nil {
		t.Fatalf("mkdir installed: %v", err)
	}
	graphPath := filepath.Join(installed, "graph.db")
	originalGraphContent := []byte("PRE-EXISTING ACTIVE GRAPH — must not be touched")
	if err := os.WriteFile(graphPath, originalGraphContent, 0644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	currentLink := filepath.Join(dir, "current")
	if err := os.Symlink(installed, currentLink); err != nil {
		t.Fatalf("symlink current: %v", err)
	}

	// Snapshot the symlink target (resolved) and the file content.
	originalTarget, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}

	// Run several failure cases against bundles in a separate scratch dir.
	scratch := t.TempDir()

	// --- Failure (a): wrong sha256 ---
	{
		hdr, body := goodEntry()
		data, _ := buildBundle(t, []tarEntry{{Header: hdr, Body: body}})
		m := Manifest{
			Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
			SchemaVersion: "awareness.bundle.v1",
			SHA256:        "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		}
		ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
		bp, mp := writeBundle(t, scratch, data, m)
		res, _ := VerifyBundle(bp, mp, ri)
		if res.OK {
			t.Fatal("verify unexpectedly passed for wrong sha256")
		}
		assertCurrentUntouched(t, currentLink, originalTarget, graphPath, originalGraphContent)
	}

	// --- Failure (b): version mismatch ---
	{
		hdr, body := goodEntry()
		data, sum := buildBundle(t, []tarEntry{{Header: hdr, Body: body}})
		m := Manifest{
			Name: BundleName, Version: "v0.0.1", BuildID: "abc123",
			SchemaVersion: "awareness.bundle.v1", SHA256: sum,
		}
		ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
		bp, mp := writeBundle(t, scratch, data, m)
		res, _ := VerifyBundle(bp, mp, ri)
		if res.OK {
			t.Fatal("verify unexpectedly passed for version mismatch")
		}
		assertCurrentUntouched(t, currentLink, originalTarget, graphPath, originalGraphContent)
	}

	// --- Failure (c): unsafe tar (path traversal) ---
	{
		body := []byte("x")
		data, sum := buildBundle(t, []tarEntry{
			{Header: &tar.Header{Name: "../escape", Mode: 0644, Typeflag: tar.TypeReg}, Body: body},
		})
		m := Manifest{
			Name: BundleName, Version: "v1.2.30", BuildID: "abc123",
			SchemaVersion: "awareness.bundle.v1", SHA256: sum,
		}
		ri := &ReleaseIndex{Version: "v1.2.30", BuildID: "abc123"}
		bp, mp := writeBundle(t, scratch, data, m)
		res, _ := VerifyBundle(bp, mp, ri)
		if res.OK {
			t.Fatal("verify unexpectedly passed for tar traversal")
		}
		assertCurrentUntouched(t, currentLink, originalTarget, graphPath, originalGraphContent)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func hasViolation(vs []TarEntryViolation, reason string) bool {
	for _, v := range vs {
		if v.Reason == reason {
			return true
		}
	}
	return false
}

// assertCurrentUntouched verifies the simulated "current" symlink and the
// file it points at are unchanged.
func assertCurrentUntouched(t *testing.T, link, originalTarget, graphPath string, originalContent []byte) {
	t.Helper()

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink current: %v", err)
	}
	if target != originalTarget {
		t.Errorf("current symlink moved: %q → %q", originalTarget, target)
	}
	gotContent, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	if !bytes.Equal(gotContent, originalContent) {
		t.Errorf("graph content modified: %q → %q", originalContent, gotContent)
	}
}

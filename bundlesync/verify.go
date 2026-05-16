package bundlesync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyManifest checks a parsed manifest against the active release-index.
//
// Phase A invariants (must all hold for OK=true):
//  1. m.Name == BundleName
//  2. m.Version, m.BuildID, m.SchemaVersion, m.SHA256 are all non-empty
//  3. m.Version == ri.Version
//  4. m.BuildID == ri.BuildID
//  5. m.SchemaVersion is in SupportedSchemaVersions
//
// VerifyManifest never reads from disk; it operates on the parsed structs.
// This makes it cheap to call from MCP tool handlers that already have
// the manifest in memory.
func VerifyManifest(m *Manifest, ri *ReleaseIndex) *VerifyResult {
	r := &VerifyResult{
		ExpectedVersion: ri.Version,
		ExpectedBuildID: ri.BuildID,
		ActualVersion:   m.Version,
		ActualBuildID:   m.BuildID,
		ActualSchema:    m.SchemaVersion,
		ManifestSHA256:  m.SHA256,
	}

	// (1) Name check — silently rejecting an unknown bundle name is wrong;
	// we want callers to be able to log this.
	if m.Name != BundleName {
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = fmt.Sprintf("manifest.name = %q, want %q", m.Name, BundleName)
		return r
	}

	// (2) Required fields present.
	switch {
	case m.Version == "":
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = "manifest.version is empty"
		return r
	case m.BuildID == "":
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = "manifest.build_id is empty"
		return r
	case m.SchemaVersion == "":
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = "manifest.schema_version is empty"
		return r
	case m.SHA256 == "":
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = "manifest.sha256 is empty"
		return r
	}

	// (3) + (4) Release-index match. STALE vs MISMATCH split:
	//   - version differs           → AWARENESS_BUNDLE_MISMATCH
	//                                  (different release line entirely)
	//   - version equal, build_id differs → AWARENESS_BUNDLE_STALE
	//                                  (same release, behind on CI build)
	//
	// Operators read these differently: MISMATCH means "wrong release was
	// installed"; STALE means "the build pipeline moved on and we haven't."
	// Both block AWARENESS_READY, but the remediation paths can differ.
	if m.Version != ri.Version {
		r.State = StateAwarenessBundleMismatch
		r.Reason = fmt.Sprintf("manifest.version = %q, release-index.version = %q", m.Version, ri.Version)
		return r
	}
	if m.BuildID != ri.BuildID {
		r.State = StateAwarenessBundleStale
		r.Reason = fmt.Sprintf("manifest.build_id = %q, release-index.build_id = %q (same release line, older build)", m.BuildID, ri.BuildID)
		return r
	}

	// (5) Schema must be one we know how to load. Distinct from VERIFY_FAILED
	// because a schema we don't support may still be valid for a newer binary —
	// remediation is "upgrade the binary", not "fetch a different bundle."
	if !supportsSchema(m.SchemaVersion) {
		r.State = StateAwarenessBundleSchemaUnsupported
		r.Reason = fmt.Sprintf("manifest.schema_version = %q not in supported list %v", m.SchemaVersion, SupportedSchemaVersions)
		return r
	}

	r.OK = true
	r.State = StateAwarenessReady
	return r
}

// LoadManifest reads and parses a manifest file from disk.
// Returns ErrManifestInvalid for any parse or JSON decoding failure so callers
// can use errors.Is.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrManifestInvalid, path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrManifestInvalid, path, err)
	}
	return &m, nil
}

// VerifyBundleSHA256 hashes the bundle file at path and compares against
// expectedHex. It is purely read-only — no extraction, no install, no symlink
// changes — so it is safe to call against any candidate file regardless of
// what /var/lib/globular/awareness/current points at.
//
// The expected hash is matched case-insensitively; manifests written by
// different tools sometimes uppercase, sometimes lowercase the hex digest.
func VerifyBundleSHA256(path, expectedHex string) error {
	if expectedHex == "" {
		return fmt.Errorf("%w: expected sha256 is empty", ErrManifestInvalid)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open %s: %v", ErrBundleUnreadable, path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("%w: read %s: %v", ErrBundleUnreadable, path, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(actual, expectedHex) {
		return fmt.Errorf("%w: actual=%s expected=%s", ErrSHA256Mismatch, actual, expectedHex)
	}
	return nil
}

// VerifyBundle is the orchestrator that composes manifest verification, tar
// safety checks, and SHA-256 verification.
//
// It does NOT extract, install, or touch /var/lib/globular/awareness/current.
// It only reads the candidate bundle and manifest. A returned VerifyResult
// with OK=false guarantees no other path on the filesystem was modified.
//
// Order is deliberate:
//  1. VerifyManifest — cheapest; reject before opening the bundle file.
//  2. ValidateTarSafe — reject malicious archives before hashing them.
//  3. VerifyBundleSHA256 — last because it requires reading the whole file.
func VerifyBundle(bundlePath, manifestPath string, ri *ReleaseIndex) (*VerifyResult, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return &VerifyResult{
			State:  StateAwarenessBundleVerifyFailed,
			Reason: err.Error(),
		}, err
	}

	res := VerifyManifest(m, ri)
	if !res.OK {
		return res, nil
	}

	// Tar safety. Open and stream the file; ValidateTarSafe handles the
	// gzip layer when present.
	f, err := os.Open(bundlePath)
	if err != nil {
		return &VerifyResult{
			State:          StateAwarenessBundleVerifyFailed,
			Reason:         fmt.Sprintf("open bundle: %v", err),
			ManifestSHA256: m.SHA256,
		}, fmt.Errorf("%w: %v", ErrBundleUnreadable, err)
	}
	violations, tarErr := ValidateTarSafe(f)
	f.Close()
	if tarErr != nil {
		return &VerifyResult{
			State:          StateAwarenessBundleVerifyFailed,
			Reason:         fmt.Sprintf("tar scan: %v", tarErr),
			ManifestSHA256: m.SHA256,
		}, fmt.Errorf("%w: %v", ErrTarUnsafe, tarErr)
	}
	if len(violations) > 0 {
		res.OK = false
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = fmt.Sprintf("%d unsafe tar entries", len(violations))
		res.TarViolations = violations
		return res, fmt.Errorf("%w: %d entries", ErrTarUnsafe, len(violations))
	}

	// SHA-256 verification.
	if err := VerifyBundleSHA256(bundlePath, m.SHA256); err != nil {
		// Compute actual hash for the result even on mismatch so the
		// caller can log both.
		actual := hashFileBestEffort(bundlePath)
		res.OK = false
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = err.Error()
		res.ActualSHA256 = actual
		if errors.Is(err, ErrBundleUnreadable) {
			return res, err
		}
		return res, err
	}

	// All checks passed.
	res.ActualSHA256 = m.SHA256
	res.OK = true
	res.State = StateAwarenessReady
	return res, nil
}

// hashFileBestEffort returns hex(sha256) of path or "" on any read error.
// Used only to enrich VerifyResult on mismatch — never the primary check.
func hashFileBestEffort(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

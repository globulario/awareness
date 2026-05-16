package bundlesync

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// installedBundleFilename is the canonical filename used for the retained
// copy of the original tar.gz alongside the extracted contents. Centralized
// so MCP serve tools (mcp.awareness_bundle_stream,
// mcp.awareness_bundle_manifest) and the installer agree on one name.
//
// Lives in the bundlesync package so callers don't duplicate the literal
// across subsystems; MCP imports this constant via the
// activeBundleFilename binding in tools_awareness_bundle_serve.go.
const installedBundleFilename = "bundle.tar.gz"

// ── Atomic install primitives ────────────────────────────────────────────────
//
// Phase C.1 contract: install a verified bundle into the canonical layout
// without ever leaving the on-disk state half-applied. Every step that could
// fail is either:
//
//   1. before we touch the active symlink (so partial work cleans up itself), or
//   2. an atomic rename(2) on the same filesystem (so it either fully happens
//      or doesn't happen at all).
//
// Layout:
//
//   <BundleRoot>/
//     current                            -> installed/<version>/<build_id>/
//     installed/
//       <version>/
//         <build_id>/                    immutable, content-addressed
//           manifest.json
//           graph.db
//           ...
//     staging/
//       <random>/                        temp extraction; renamed-or-removed
//
// Crash safety:
//
//   - If we crash before the symlink is switched, the new versioned dir may
//     already exist (atomically renamed in) but `current` still points at
//     the old bundle. The next install will see the dir, skip extraction,
//     and complete the symlink switch idempotently.
//   - If we crash mid-extract, the staging tree is orphaned. Callers should
//     periodically prune <BundleRoot>/staging/.
//   - We NEVER delete the previous `current` target on success — that's
//     left for an explicit "compact" operation outside this primitive.

// InstallOptions carries the inputs to InstallBundle. All paths must be
// absolute. BundleRoot must exist (or be creatable) and live on the same
// filesystem as the staging dir for the atomic rename to be atomic.
type InstallOptions struct {
	BundlePath   string        // path to the .tar.gz on disk
	ManifestPath string        // path to manifest.json on disk (sidecar)
	BundleRoot   string        // e.g., /var/lib/globular/awareness
	ReleaseIndex *ReleaseIndex // for verification
}

// InstallResult describes what install did. OK=true means the active bundle
// is now the requested version+build_id and the symlink reflects that.
type InstallResult struct {
	OK             bool
	State          State
	Reason         string
	InstalledPath  string         // absolute path to the new versioned dir
	PreviousActive string         // prior current target (empty on first install)
	SymlinkUpdated bool           // true if the symlink target actually changed
	AlreadyPresent bool           // true if the versioned dir was already on disk
	Verify         *VerifyResult  // verification verdict
}

// InstallBundle is the atomic install entry point. It runs Phase A
// VerifyBundle first; on any verification failure it returns a result with
// OK=false, State set, and zero filesystem mutations under BundleRoot
// outside the staging area.
//
// On success:
//
//   - <BundleRoot>/installed/<version>/<build_id>/ is populated with the
//     bundle contents (or already existed and was reused);
//   - <BundleRoot>/current symlinks to that versioned dir.
//
// The sidecar manifest is also copied next to the extracted bundle so
// future inspectors don't need the original tarball:
//
//   <BundleRoot>/installed/<version>/<build_id>/manifest.json
func InstallBundle(opts InstallOptions) (*InstallResult, error) {
	if opts.BundlePath == "" || opts.ManifestPath == "" || opts.BundleRoot == "" {
		return &InstallResult{
			State:  StateAwarenessBundleVerifyFailed,
			Reason: "InstallOptions: bundle_path, manifest_path, and bundle_root are required",
		}, fmt.Errorf("invalid options")
	}

	res := &InstallResult{}

	// (1) Verify bundle (manifest + tar safety + sha256). On any failure we
	// MUST NOT touch BundleRoot/current. Phase A's VerifyBundle is read-only
	// against the input files, so this returns cleanly without side effects.
	verify, vErr := VerifyBundle(opts.BundlePath, opts.ManifestPath, opts.ReleaseIndex)
	res.Verify = verify
	if !verify.OK {
		res.State = verify.State
		res.Reason = verify.Reason
		return res, vErr
	}

	manifest, mErr := LoadManifest(opts.ManifestPath)
	if mErr != nil {
		res.State = StateAwarenessBundleVerifyFailed
		res.Reason = mErr.Error()
		return res, mErr
	}

	versionedDir := installedVersionDir(opts.BundleRoot, manifest.Version, manifest.BuildID)
	res.InstalledPath = versionedDir

	// (2) If the versioned dir already exists, we treat it as already-installed
	// and only ensure the symlink reflects it. This makes re-runs idempotent
	// and lets us recover from a crash that happened between rename and
	// symlink switch.
	if _, err := os.Stat(versionedDir); err == nil {
		res.AlreadyPresent = true
	} else if errors.Is(err, os.ErrNotExist) {
		// (3) Extract to a unique staging path, then rename atomically.
		stagingPath, err := makeStagingDir(opts.BundleRoot)
		if err != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = err.Error()
			return res, err
		}
		// Defense: if anything below fails, drop the staging tree. Successful
		// rename clears stagingPath and this becomes a no-op (RemoveAll on a
		// missing path is fine).
		defer os.RemoveAll(stagingPath)

		f, err := os.Open(opts.BundlePath)
		if err != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = fmt.Sprintf("open bundle: %v", err)
			return res, err
		}
		violations, exErr := ExtractTarSafe(f, stagingPath)
		f.Close()
		if exErr != nil {
			res.State = StateAwarenessBundleInstallFailed
			if len(violations) > 0 {
				verify.TarViolations = violations
				res.State = StateAwarenessBundleVerifyFailed
				res.Reason = fmt.Sprintf("unsafe tar entry: %v", violations[0].Reason)
				return res, exErr
			}
			res.Reason = fmt.Sprintf("extract: %v", exErr)
			return res, exErr
		}

		// (4) Drop the manifest sidecar inside the extracted tree so the
		// installed bundle is self-describing. We tolerate an existing
		// manifest.json in the tar — the sidecar overwrites it because the
		// sidecar is what the operator authoritatively pulled and verified.
		manifestData, mfErr := os.ReadFile(opts.ManifestPath)
		if mfErr != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = fmt.Sprintf("read manifest: %v", mfErr)
			return res, mfErr
		}
		if err := os.WriteFile(filepath.Join(stagingPath, "manifest.json"), manifestData, 0644); err != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = fmt.Sprintf("write manifest sidecar: %v", err)
			return res, err
		}

		// (4b) Retain the source tar.gz alongside the extracted contents so
		// MCP's awareness_bundle_stream / awareness_bundle_manifest tools
		// can serve the original archive to remote callers. Without this
		// copy, peer nodes that try to pull the bundle from this node see
		// AWARENESS_BUNDLE_MISSING even though the install succeeded.
		// Skipped if the source path equals the destination (in-place install
		// scenarios that some test setups exercise).
		dstBundle := filepath.Join(stagingPath, installedBundleFilename)
		if abs1, _ := filepath.Abs(opts.BundlePath); abs1 != dstBundle {
			if err := copyFileAtomic(opts.BundlePath, dstBundle); err != nil {
				res.State = StateAwarenessBundleInstallFailed
				res.Reason = fmt.Sprintf("retain bundle tarball: %v", err)
				return res, err
			}
		}

		// (5) Validate extracted contents. At minimum the bundle must contain
		// graph.db (the awareness graph itself); without it the bundle is
		// useless even if the tar verified.
		if err := validateExtracted(stagingPath); err != nil {
			res.State = StateAwarenessBundleIncomplete
			res.Reason = err.Error()
			return res, err
		}

		// (6) Atomic rename into the immutable versioned path.
		if err := os.MkdirAll(filepath.Dir(versionedDir), 0755); err != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = fmt.Sprintf("mkdir versioned parent: %v", err)
			return res, err
		}
		if err := os.Rename(stagingPath, versionedDir); err != nil {
			res.State = StateAwarenessBundleInstallFailed
			res.Reason = fmt.Sprintf("rename staging→versioned: %v", err)
			return res, err
		}
	} else {
		res.State = StateAwarenessBundleInstallFailed
		res.Reason = fmt.Sprintf("stat versioned dir: %v", err)
		return res, err
	}

	// (7) Atomically switch the current symlink. Capture the prior target for
	// the result so callers can decide whether to compact/cleanup later.
	currentLink := filepath.Join(opts.BundleRoot, "current")
	if existing, err := os.Readlink(currentLink); err == nil {
		// Stored as absolute when we created it; a previous tool may have
		// stored it relative. Resolve relative-to-BundleRoot for clarity.
		if filepath.IsAbs(existing) {
			res.PreviousActive = existing
		} else {
			res.PreviousActive = filepath.Join(opts.BundleRoot, existing)
		}
	}

	if res.PreviousActive == versionedDir {
		// Already pointing at us; nothing to do.
		res.OK = true
		res.State = StateAwarenessReady
		return res, nil
	}

	if err := atomicSymlinkSwap(currentLink, versionedDir); err != nil {
		res.State = StateAwarenessBundleInstallFailed
		res.Reason = fmt.Sprintf("symlink swap: %v", err)
		return res, err
	}

	res.SymlinkUpdated = true
	res.OK = true
	res.State = StateAwarenessReady
	return res, nil
}

// installedVersionDir is the canonical immutable path for a (version,build_id)
// pair. Centralized so callers and tests agree on the layout.
func installedVersionDir(bundleRoot, version, buildID string) string {
	return filepath.Join(bundleRoot, "installed", version, buildID)
}

// makeStagingDir creates a unique staging directory under <BundleRoot>/staging.
// Returned path does NOT yet exist on disk — ExtractTarSafe will create it.
// (We only create the parent so the same-filesystem rename invariant holds.)
func makeStagingDir(bundleRoot string) (string, error) {
	parent := filepath.Join(bundleRoot, "staging")
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", fmt.Errorf("mkdir staging: %w", err)
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return filepath.Join(parent, "stage-"+hex.EncodeToString(buf[:])), nil
}

// validateExtracted enforces the minimum contents an installed bundle must
// carry. Phase C.1 only requires graph.db; richer validation (signed manifest
// inside, contracts/, invariants/, etc.) belongs in a later phase.
func validateExtracted(dir string) error {
	required := []string{"graph.db"}
	for _, r := range required {
		p := filepath.Join(dir, r)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("required file missing in bundle: %s", r)
		}
	}
	return nil
}

// copyFileAtomic copies src to dst by writing to dst+".tmp" then renaming
// onto dst. Both paths must be on the same filesystem for the rename to be
// atomic; the caller arranges this by writing into the staging directory
// before the staging→versioned rename. Any partial write is removed.
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	if _, copyErr := io.Copy(out, in); copyErr != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy: %w", copyErr)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close dest: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// atomicSymlinkSwap replaces (or creates) linkPath so it points at target.
// On POSIX, rename(2) is atomic when both paths are on the same filesystem,
// which they always are here (both under BundleRoot).
//
// Implementation:
//   1. Create a sibling symlink at <linkPath>.tmp pointing at target.
//   2. Rename .tmp → linkPath. If linkPath exists, rename atomically replaces it.
//
// Cleanup of any leftover .tmp from a previous crashed run is best-effort.
func atomicSymlinkSwap(linkPath, target string) error {
	tmp := linkPath + ".tmp"
	// Best-effort cleanup of any orphan from a prior crash.
	_ = os.Remove(tmp)

	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("create tmp symlink: %w", err)
	}
	if err := os.Rename(tmp, linkPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename tmp→current: %w", err)
	}
	return nil
}

package bundlesync

import "fmt"

// CheckAwarenessFreshness composes manifest verification with release-index
// matching and (optional) local-binary correlation into a single verdict.
//
// This is the function the spec names "CheckAwarenessFreshness(releaseIndex,
// bundleManifest, localBinaryInfo)" — it answers the cluster-level question:
//
//	"Is this bundle still the right one for what's running here?"
//
// Compared to VerifyManifest, this function:
//
//   - returns FreshnessReport with diagnostic flags, not VerifyResult
//   - cross-checks against an optional LocalBinaryInfo (skipped when nil
//     or when its fields are empty — per the spec, missing build info is
//     not a hard failure)
//   - surfaces optional manifest fields (GraphHash, SourceCommit) so the
//     operator UI can render confidence badges
//
// What this function does NOT do:
//
//   - read from disk (callers pass parsed Manifest / ReleaseIndex)
//   - hash the bundle file (use VerifyBundleSHA256 for that)
//   - validate tar safety (use ValidateTarSafe)
//   - install or modify any path
//
// The orchestrator stays pure so it is cheap to call from MCP tool handlers
// that already have the manifest in memory.
func CheckAwarenessFreshness(m *Manifest, ri *ReleaseIndex, lb *LocalBinaryInfo) *FreshnessReport {
	r := &FreshnessReport{
		Manifest:    m,
		Release:     ri,
		LocalBinary: lb,
	}

	if m == nil {
		r.State = StateAwarenessBundleMissing
		r.Reason = "no manifest provided"
		return r
	}
	if ri == nil {
		r.State = StateAwarenessBundleVerifyFailed
		r.Reason = "release-index unavailable; cannot decide freshness"
		return r
	}

	// Reuse VerifyManifest for the bundle-vs-release comparison so the
	// STALE/MISMATCH/SCHEMA_UNSUPPORTED rules live in exactly one place.
	vr := VerifyManifest(m, ri)
	r.State = vr.State
	r.Reason = vr.Reason
	r.SchemaSupported = supportsSchema(m.SchemaVersion)
	r.VersionMatchesRelease = m.Version != "" && m.Version == ri.Version
	r.BuildIDMatchesRelease = m.BuildID != "" && m.BuildID == ri.BuildID
	r.GraphHashPresent = m.GraphHash != ""
	r.SourceCommitPresent = m.SourceCommit != ""

	if !vr.OK {
		// Bundle does not match release-index; freshness fails before we
		// even consider the local binary.
		return r
	}

	// Optional local-binary correlation. We only fail freshness on a
	// mismatch when LocalBinaryInfo carries non-empty fields — empty
	// fields mean "I don't know," and the spec says not to hard-fail
	// on missing build metadata.
	if lb != nil {
		if lb.Version != "" {
			r.LocalBinaryVersionMatch = lb.Version == m.Version
			if !r.LocalBinaryVersionMatch {
				r.OK = false
				r.State = StateAwarenessBundleMismatch
				r.Reason = fmt.Sprintf("local binary version = %q, bundle version = %q", lb.Version, m.Version)
				return r
			}
		}
		if lb.BuildID != "" {
			r.LocalBinaryBuildIDMatch = lb.BuildID == m.BuildID
			if !r.LocalBinaryBuildIDMatch {
				r.OK = false
				// Same-version, different-build correlation reads as STALE
				// for consistency with the manifest-vs-release rule.
				r.State = StateAwarenessBundleStale
				r.Reason = fmt.Sprintf("local binary build_id = %q, bundle build_id = %q", lb.BuildID, m.BuildID)
				return r
			}
		}
	}

	r.OK = true
	r.State = StateAwarenessReady
	return r
}

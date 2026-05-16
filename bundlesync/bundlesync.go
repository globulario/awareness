// Package bundlesync provides verification primitives for the Globular
// awareness bundle: manifest matching against the release-index, bundle
// SHA-256 verification, and tar-archive safety checks.
//
// Phase A scope (this file): pure, side-effect-free primitives. No network
// calls, no filesystem mutations beyond reading the candidate bundle/manifest,
// and no interaction with the active /var/lib/globular/awareness/current
// symlink. Phase C will compose these into EnsureAwarenessBundle().
//
// Cold-bootstrap rule: when no bundle is present locally and no source is
// reachable, the node MUST stay in AWARENESS_BUNDLE_MISSING. There is no
// embedded minimal bundle in Phase A — silent fallback would be a violation
// of "automatic ensure, not automatic trust."
package bundlesync

import "errors"

// State is a Day-1 readiness state for the awareness bundle. It is independent
// from the awareness/evidence Day1Verdict ladder; the bundle subsystem owns
// the lifecycle of these states and surfaces them to the classifier.
type State string

const (
	// StateAwarenessReady means a verified bundle matching the active
	// release-index is loaded.
	StateAwarenessReady State = "AWARENESS_READY"

	// StateAwarenessBundleMissing — no bundle at /var/lib/globular/awareness/current.
	// This is the cold-bootstrap safe state.
	StateAwarenessBundleMissing State = "AWARENESS_BUNDLE_MISSING"

	// StateAwarenessBundleStale — bundle present but older than the active release-index.
	StateAwarenessBundleStale State = "AWARENESS_BUNDLE_STALE"

	// StateAwarenessBundleMismatch — manifest version/build_id does not match release-index.
	StateAwarenessBundleMismatch State = "AWARENESS_BUNDLE_MISMATCH"

	// StateAwarenessBundleIncomplete — bundle present but required files missing.
	StateAwarenessBundleIncomplete State = "AWARENESS_BUNDLE_INCOMPLETE"

	// StateAwarenessBundleVerifyFailed — sha256 / tar-safety / structural check failed.
	// Schema-unsupport is reported separately as StateAwarenessBundleSchemaUnsupported
	// so operators can tell "I can't trust this bundle" from "I can't load this schema".
	StateAwarenessBundleVerifyFailed State = "AWARENESS_BUNDLE_VERIFY_FAILED"

	// StateAwarenessBundleSchemaUnsupported — manifest.schema_version is not in
	// SupportedSchemaVersions for this binary. Distinct from VERIFY_FAILED because
	// the bundle may be perfectly valid for a *newer* binary; the right action is
	// "upgrade the binary", not "fetch a different bundle."
	StateAwarenessBundleSchemaUnsupported State = "AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED"

	// StateAwarenessBundleSyncing — auto-sync flow currently fetching/installing.
	StateAwarenessBundleSyncing State = "AWARENESS_BUNDLE_SYNCING"

	// StateAwarenessBundleSourceUnavailable — every trusted source has been tried
	// and none yielded a matching, verified bundle. Backoff applies.
	StateAwarenessBundleSourceUnavailable State = "AWARENESS_BUNDLE_SOURCE_UNAVAILABLE"

	// StateAwarenessBundleInstallFailed — extract/install step failed; previous
	// bundle (if any) remains active.
	StateAwarenessBundleInstallFailed State = "AWARENESS_BUNDLE_INSTALL_FAILED"
)

// BundleName is the canonical manifest.name value. Any other name fails verification.
const BundleName = "globular-awareness-bundle"

// DefaultBundleRoot is the production install root for the awareness bundle.
// <DefaultBundleRoot>/current is a symlink to the active versioned dir, and
// <DefaultBundleRoot>/current/manifest.json is the default location preflight
// readers (CLI + MCP) consult to populate Staleness.BundlePresent.
const DefaultBundleRoot = "/var/lib/globular/awareness"

// DefaultManifestPath returns the canonical filesystem path of the active
// bundle manifest. Callers that don't override BundleManifestPath in
// preflight.Options should pass this so the joined freshness pipeline can
// resolve to "fresh" instead of "stale_unknown".
func DefaultManifestPath() string {
	return DefaultBundleRoot + "/current/manifest.json"
}

// CurrentBundleSchemaVersion is the schema string that newly built bundles
// stamp into manifest.json. Builders should write this value; consumers
// should accept anything in SupportedSchemaVersions. The pair lets the
// system roll forward one version ahead of the readers without breaking
// older binaries — bump SupportedSchemaVersions first, then
// CurrentBundleSchemaVersion.
const CurrentBundleSchemaVersion = "awareness.bundle.v1"

// SupportedSchemaVersions enumerates the bundle schemas this binary can load.
// Phase A supports only v1; bumping requires explicit code support for the
// new schema, never silent acceptance.
var SupportedSchemaVersions = []string{
	CurrentBundleSchemaVersion,
}

// IsSupportedSchemaVersion reports whether v is one of the schema strings
// this binary can load. Exposed so build/publish tools can validate a
// schema_version they're about to write or upload without reaching for the
// private supportsSchema helper.
func IsSupportedSchemaVersion(v string) bool { return supportsSchema(v) }

// Manifest describes a single awareness bundle as served by
// mcp.awareness_bundle_manifest or saved alongside a pulled tarball.
//
// Mandatory fields (must be non-empty for a verified bundle):
//
//	Name, Version, BuildID, SchemaVersion, SHA256.
//
// Optional fields may be empty without invalidating the manifest:
//
//	SizeBytes (when 0, length checks are skipped)
//	SourceNodeID, CreatedAt, Signature.
type Manifest struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	BuildID       string `json:"build_id"`
	SchemaVersion string `json:"schema_version"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	SourceNodeID  string `json:"source_node_id,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	Signature     string `json:"signature,omitempty"`

	// GraphHash is the hex digest of the compiled graph content (graph.db),
	// distinct from SHA256 which is the hash of the whole bundle archive.
	// Optional: when present, it gives operators a stable identity for the
	// graph itself across re-tarred/re-signed bundles.
	GraphHash string `json:"graph_hash,omitempty"`

	// SourceCommit is the git SHA the bundle was built from. Optional but
	// strongly recommended when the build pipeline provides it; it lets
	// `awareness verify` correlate a runtime bundle with a tree state.
	SourceCommit string `json:"source_commit,omitempty"`
}

// LocalBinaryInfo describes the running binary's release identity. Optional
// input to CheckAwarenessFreshness — when supplied, it lets the orchestrator
// catch the "binary upgraded but bundle didn't" or vice-versa cases that
// release-index alone can't see.
//
// Empty fields mean "I don't know." Per the spec, missing build info is not
// a hard failure; it just downgrades the freshness check to release-index-only.
type LocalBinaryInfo struct {
	Version string
	BuildID string
}

// ReleaseIndex is the subset of /var/lib/globular/release-index.json that
// the bundle subsystem must match against. Loading the full release-index is
// the caller's responsibility; this package only consumes the two fields that
// pin a bundle to a release.
type ReleaseIndex struct {
	Version string `json:"version"`
	BuildID string `json:"build_id"`
}

// VerifyResult describes the outcome of a verification call. It carries
// enough context for the classifier to choose a state and for an operator log
// line to be unambiguous about why a bundle was rejected.
type VerifyResult struct {
	OK     bool
	State  State
	Reason string

	// Mandatory fields the verifier observed.
	ExpectedVersion string
	ExpectedBuildID string
	ActualVersion   string
	ActualBuildID   string
	ActualSchema    string

	// SHA-256 hashes recorded for forensics. ManifestSHA256 is what the
	// manifest claims; ActualSHA256 is what the verifier hashed off disk
	// (empty when the verifier never reached the hashing step).
	ManifestSHA256 string
	ActualSHA256   string

	// TarViolations populated when the tar-safety check rejected entries.
	// A non-empty slice always coexists with OK=false and
	// State=AWARENESS_BUNDLE_VERIFY_FAILED.
	TarViolations []TarEntryViolation
}

// TarEntryViolation describes one unsafe entry found in a bundle archive.
type TarEntryViolation struct {
	Name   string `json:"name"`
	Reason string `json:"reason"` // one of the TarReason* constants below
}

// FreshnessReport is the output of CheckAwarenessFreshness. It composes the
// release-index/manifest comparison with optional local-binary correlation
// into a single verdict an operator or classifier can act on.
//
// Distinction from VerifyResult: VerifyResult is the bundle-level
// "is this bundle valid?" check; FreshnessReport is the cluster-level
// "does this bundle still match what we should be running?" check. A bundle
// can be VerifyResult.OK=true and FreshnessReport.OK=false (perfectly valid
// bundle, but stale relative to the active release-index).
type FreshnessReport struct {
	OK     bool
	State  State
	Reason string

	Manifest    *Manifest
	Release     *ReleaseIndex
	LocalBinary *LocalBinaryInfo

	// Diagnostic flags so callers can render a useful explanation without
	// re-running comparisons.
	VersionMatchesRelease bool
	BuildIDMatchesRelease bool
	SchemaSupported       bool

	// LocalBinary correlation. Both flags are false when LocalBinary is nil
	// (the check was skipped — not a failure).
	LocalBinaryVersionMatch bool
	LocalBinaryBuildIDMatch bool

	// Optional metadata surfaced for the operator UI.
	GraphHashPresent    bool
	SourceCommitPresent bool
}

// Tar reason codes — kept as constants so callers (CLI / MCP responses) can
// switch on a stable string set rather than parsing free-form text.
const (
	TarReasonAbsolutePath  = "absolute_path"
	TarReasonPathTraversal = "path_traversal"
	TarReasonSymlinkEscape = "symlink_escape"
	TarReasonDeviceFile    = "device_file"
	TarReasonHardlinkUnsafe = "hardlink_unsafe"
	TarReasonUnknownType   = "unknown_type"
)

// Sentinel errors. Verify functions wrap these so callers can use errors.Is
// without parsing reason strings.
var (
	// ErrManifestInvalid is returned when the manifest itself is malformed
	// or missing required fields.
	ErrManifestInvalid = errors.New("manifest invalid")

	// ErrVersionMismatch is returned when manifest.version != release.version.
	ErrVersionMismatch = errors.New("manifest version does not match release-index")

	// ErrBuildIDMismatch is returned when manifest.build_id != release.build_id.
	ErrBuildIDMismatch = errors.New("manifest build_id does not match release-index")

	// ErrSchemaUnsupported is returned when manifest.schema_version is not
	// in SupportedSchemaVersions for this binary.
	ErrSchemaUnsupported = errors.New("manifest schema_version unsupported by this binary")

	// ErrSHA256Mismatch is returned when the bundle's actual SHA-256 does
	// not match what the manifest declares.
	ErrSHA256Mismatch = errors.New("bundle SHA-256 does not match manifest")

	// ErrTarUnsafe is returned when one or more tar entries are unsafe.
	ErrTarUnsafe = errors.New("bundle archive contains unsafe entries")

	// ErrBundleUnreadable is returned when the bundle file cannot be opened
	// or streamed. Distinct from a verification failure.
	ErrBundleUnreadable = errors.New("bundle unreadable")
)

// supportsSchema reports whether v is in the supported set. Kept private so
// the supported list can grow without becoming public API.
func supportsSchema(v string) bool {
	for _, s := range SupportedSchemaVersions {
		if s == v {
			return true
		}
	}
	return false
}

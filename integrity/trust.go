// Package integrity provides graph integrity checking for the awareness system.
// It validates that the knowledge graph is descriptive, not aspirational.
package integrity

// Trust level constants classify how well an edge or claim is supported.
const (
	TrustStrictVerified = "strict_verified" // required test passed in CI or local check
	TrustVerified       = "verified"         // symbol/file exists, extractor confirmed
	TrustDeclared       = "declared"         // YAML declares it, no test proof
	TrustInferred       = "inferred"         // heuristic only, no source verification
	TrustProposal       = "proposal"         // pending proposal, not yet promoted
	TrustStale          = "stale"            // source changed or CI missing after changes
	TrustInvalid        = "invalid"          // referenced file/test/function missing
)

// Source type constants identify the provenance of a graph edge.
const (
	SourceYAML            = "yaml"
	SourceCodeExtractor   = "code_extractor"
	SourceTestDiscovery   = "test_discovery"
	SourceCIResult        = "ci_result"
	SourceProposal        = "proposal"
	SourceRuntimeSnapshot = "runtime_snapshot"
	SourceOfflineDiagnose = "offline_diagnose"
)

// StalePolicy constants name conditions under which an edge becomes stale.
const (
	StalePolicyTestMissing            = "test_missing"
	StalePolicyFileMissing            = "file_missing"
	StalePolicyFileChangedAfterVerify = "file_changed_after_verification"
	StalePolicyTestFailed             = "test_failed"
)

// EdgeProvenance records where a graph edge came from and its verification state.
// It is serialised to JSON and stored in edges.provenance_json.
type EdgeProvenance struct {
	SourceType        string   `json:"source_type,omitempty"`
	SourceFile        string   `json:"source_file,omitempty"`
	SourceCommit      string   `json:"source_commit,omitempty"`
	CreatedBy         string   `json:"created_by,omitempty"`
	LastVerifiedAt    int64    `json:"last_verified_at,omitempty"`
	LastVerifiedBy    string   `json:"last_verified_by,omitempty"`
	VerificationLevel string   `json:"verification_level,omitempty"`
	StalePolicy       []string `json:"stale_policy,omitempty"`
}

// TrustSummary counts edges by trust level.
type TrustSummary struct {
	StrictVerified int `json:"strict_verified"`
	Verified       int `json:"verified"`
	Declared       int `json:"declared"`
	Inferred       int `json:"inferred"`
	Proposal       int `json:"proposal"`
	Stale          int `json:"stale"`
	Invalid        int `json:"invalid"`
}

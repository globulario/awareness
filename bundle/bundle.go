// Package bundle defines the generic Awareness bundle format.
//
// An Awareness bundle is a portable snapshot of a project's knowledge graph,
// invariants, failure modes, and optional runtime overlay. The format is
// project-agnostic: Globular deploys it through its package/node-agent
// pipeline, but the bundle itself does not depend on Globular.
//
// Bundles support two usage modes:
//   - Cadence / local / CI: built with NullAdapter, no runtime signals, no
//     cluster required.
//   - Globular deployed: runtime_signals baked in from GlobularAdapter at
//     bundle build time.
package bundle

import (
	"errors"
	"time"
)

// CurrentSchemaVersion is the schema version for bundles produced by this
// module.
const CurrentSchemaVersion = "awareness.bundle.v1"

// BundleManifest is the metadata document inside every Awareness bundle.
// It is serialised as bundle.json at the bundle root.
//
// All *Path fields are relative to the bundle root directory.
// Optional fields are empty strings or false when not present.
type BundleManifest struct {
	// SchemaVersion identifies the bundle format version.
	SchemaVersion string `json:"schema_version"`

	// ProjectName is the value of .awareness.yaml project.name.
	ProjectName string `json:"project_name"`
	// ProjectKind is the value of .awareness.yaml project.kind.
	ProjectKind string `json:"project_kind"`
	// SourceRoot is the absolute path of the project source root at bundle
	// build time. Informational — not used to resolve relative paths.
	SourceRoot string `json:"source_root,omitempty"`
	// SourceRevision is the VCS revision (git SHA, tag) at bundle build time.
	// Empty when the source is not under version control or revision is unknown.
	SourceRevision string `json:"source_revision,omitempty"`

	// GeneratedAt is the time the bundle was built.
	GeneratedAt time.Time `json:"generated_at"`
	// GeneratorVersion is the version of the awareness tool that built the
	// bundle. Empty when built from an unversioned dev binary.
	GeneratorVersion string `json:"generator_version,omitempty"`

	// ProfilePath is the relative path to the serialised ProjectProfile
	// (profile.json) inside the bundle.
	ProfilePath string `json:"profile_path,omitempty"`
	// GraphPath is the relative path to the graph snapshot (graph.db or
	// graph.json) inside the bundle.
	GraphPath string `json:"graph_path,omitempty"`
	// FindingsPath is the relative path to the findings snapshot
	// (findings.json) inside the bundle.
	FindingsPath string `json:"findings_path,omitempty"`
	// EvidencePath is the relative path to the evidence snapshot
	// (evidence.json) inside the bundle.
	EvidencePath string `json:"evidence_path,omitempty"`

	// InvariantsPaths are the relative paths to the invariants YAML files
	// included in the bundle.
	InvariantsPaths []string `json:"invariants_paths,omitempty"`
	// FailureModesPaths are the relative paths to the failure_modes YAML
	// files included in the bundle.
	FailureModesPaths []string `json:"failure_modes_paths,omitempty"`
	// ForbiddenFixesPaths are the relative paths to the forbidden_fixes YAML
	// files included in the bundle.
	ForbiddenFixesPaths []string `json:"forbidden_fixes_paths,omitempty"`
	// DecisionsPath is the relative path to the decisions snapshot directory
	// inside the bundle.
	DecisionsPath string `json:"decisions_path,omitempty"`

	// RuntimeSignalsPath is the relative path to the optional runtime signals
	// overlay (runtime_signals.json) inside the bundle. Present only when the
	// bundle was built with a live runtime adapter (e.g. GlobularAdapter).
	RuntimeSignalsPath string `json:"runtime_signals_path,omitempty"`
	// RuntimeSignalsIncluded is true when runtime_signals.json is present in
	// the bundle. Equivalent to RuntimeSignalsPath != "".
	RuntimeSignalsIncluded bool `json:"runtime_signals_included"`

	// Deprecated: use RuntimeSignalsIncluded. Kept for bundle.v1 readers.
	IncludesRuntimeOverlay bool `json:"includes_runtime_overlay,omitempty"`
}

// Validate reports any structural problems with the manifest.
// A manifest is only valid when it has a non-empty SchemaVersion.
func (m *BundleManifest) Validate() error {
	if m.SchemaVersion == "" {
		return errors.New("bundle manifest is missing schema_version")
	}
	return nil
}

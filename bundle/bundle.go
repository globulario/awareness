// Package bundle defines the generic Awareness bundle format.
//
// An Awareness bundle is a portable snapshot of a project's knowledge graph,
// invariants, failure modes, and optional runtime overlay. The format is
// project-agnostic: Globular deploys it through its package/node-agent
// pipeline, but the bundle itself does not depend on Globular.
package bundle

import "time"

// BundleManifest is the metadata document inside every Awareness bundle.
// It is serialised as bundle.json at the bundle root.
type BundleManifest struct {
	// SchemaVersion identifies the bundle format version.
	SchemaVersion string `json:"schema_version"`
	// ProjectName is the value of .awareness.yaml project.name.
	ProjectName string `json:"project_name"`
	// ProjectKind is the value of .awareness.yaml project.kind.
	ProjectKind string `json:"project_kind"`
	// CreatedAt is the time the bundle was built.
	CreatedAt time.Time `json:"created_at"`
	// GraphPath is the relative path to the graph snapshot inside the bundle.
	GraphPath string `json:"graph_path"`
	// ProfilePath is the relative path to the profile snapshot.
	ProfilePath string `json:"profile_path"`
	// IncludesRuntimeOverlay indicates whether a runtime_overlay.json is
	// present in the bundle (Globular-specific runtime facts baked in at
	// build time).
	IncludesRuntimeOverlay bool `json:"includes_runtime_overlay"`
}

// CurrentSchemaVersion is the schema version for bundles produced by this
// module.
const CurrentSchemaVersion = "awareness.bundle.v1"

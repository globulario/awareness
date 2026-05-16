// Package runtime defines the boundary between the generic Awareness engine
// and project-specific live systems.
//
// Awareness core depends only on the Adapter interface defined here.
// The NullAdapter is the default for projects with runtime.enabled=false.
//
// The GlobularAdapter lives in the services repository and must not be
// imported by this module.
package runtime

import (
	"context"

	"github.com/globulario/awareness/project"
)

// Adapter is the contract between Awareness core and a project's live runtime
// system. Implement this interface to provide runtime context to Awareness
// without importing Globular internals into the core engine.
//
// Adapter identity is expressed by Name and Enabled. Runtime data is
// collected via Doctor, CollectFacts, and CollectEvidence. All methods
// receive the resolved ProjectProfile so the adapter can scope its queries
// to the correct project.
type Adapter interface {
	// Name returns the adapter identifier ("null", "globular", etc.).
	Name() string
	// Enabled returns true when the adapter has a live system to query.
	// NullAdapter always returns false.
	Enabled() bool

	// Doctor returns a health report from the runtime system.
	// NullAdapter returns a clean runtime_disabled report — not an error.
	Doctor(ctx context.Context, profile *project.ProjectProfile) (*DoctorReport, error)

	// CollectFacts gathers live operational facts from the runtime.
	// NullAdapter returns nil, nil.
	CollectFacts(ctx context.Context, profile *project.ProjectProfile) ([]Fact, error)

	// CollectEvidence returns evidence matching the query.
	// NullAdapter returns nil, nil.
	CollectEvidence(ctx context.Context, profile *project.ProjectProfile, query EvidenceQuery) ([]Evidence, error)
}

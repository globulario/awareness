// Package runtime defines the boundary between the generic Awareness engine
// and project-specific live systems.
//
// Awareness core depends only on the RuntimeAdapter interface defined here.
// The NullAdapter is the default for projects with runtime.enabled=false.
//
// The GlobularAdapter lives in the services repository and must not be
// imported by this module.
package runtime

import "context"

// RuntimeFact is a live fact collected from a running system.
type RuntimeFact struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// RuntimeFinding is a health observation from the runtime adapter.
type RuntimeFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// RuntimeRef identifies an object in the runtime system.
type RuntimeRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// RuntimeObject is a resolved runtime entity.
type RuntimeObject struct {
	Ref        RuntimeRef        `json:"ref"`
	Properties map[string]string `json:"properties,omitempty"`
}

// RuntimeAdapter is the contract between Awareness core and a project's live
// runtime system. Implement this interface to provide runtime context to
// Awareness without importing Globular internals into the core engine.
type RuntimeAdapter interface {
	// Name returns the adapter identifier ("null", "globular", etc.).
	Name() string
	// Enabled returns true when the adapter has a live system to query.
	Enabled() bool
	// CollectFacts gathers live operational facts from the runtime.
	CollectFacts(ctx context.Context) ([]RuntimeFact, error)
	// Health returns current health findings from the runtime.
	Health(ctx context.Context) ([]RuntimeFinding, error)
	// ResolveRuntimeObject looks up a specific runtime entity by reference.
	// Returns nil, nil when the object is not found (not an error).
	ResolveRuntimeObject(ctx context.Context, ref RuntimeRef) (*RuntimeObject, error)
}

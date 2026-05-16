// Package evidence defines generic evidence primitives.
//
// Evidence is collected by extractors (static analysis, runtime adapters,
// bundle snapshots) and attached to graph nodes, findings, and preflight
// reports. Globular-specific evidence collectors live in the services
// repository behind the GlobularAdapter.
package evidence

import "time"

// Evidence is a piece of supporting information attached to a finding or
// graph node.
type Evidence struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// Package graph defines generic graph primitives for the Awareness knowledge
// graph.
//
// The full production graph engine (SQLite-backed, with extractor pipeline,
// cycle detection, semantic scoring) lives in services/golang/awareness/graph
// during the migration period. Once the RuntimeAdapter boundary is clean,
// the engine moves here. This package establishes the canonical node/edge
// types that the engine will implement.
package graph

// Node is a vertex in the Awareness knowledge graph. Nodes represent files,
// packages, invariants, failure modes, tests, services, findings, and other
// entities that Awareness reasons about.
type Node struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Kind       string            `json:"kind"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Graph is an in-memory representation of the Awareness knowledge graph.
// The production engine uses SQLite for persistence; this struct is used
// for snapshots, tests, and the bundle format.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Package preflight defines the public API for Awareness preflight analysis.
//
// Full preflight behavior (graph traversal, alias matching, invariant
// evaluation, coverage checks, runtime integration) lives in
// services/golang/awareness/preflight during the migration period. Once
// the RuntimeAdapter boundary is clean, the engine moves here.
//
// This package establishes the canonical request/result types that callers
// (CLI and MCP tools) depend on.
package preflight

import "github.com/globulario/awareness/finding"

// Request describes what to run preflight analysis on.
type Request struct {
	// Task is the natural-language description of the work being done.
	Task string `json:"task"`
	// ProjectRoot is the absolute path to the project root.
	ProjectRoot string `json:"project_root"`
	// ChangedFiles is an optional set of files whose impact should be
	// evaluated. When empty, the full preflight runs without file scoping.
	ChangedFiles []string `json:"changed_files,omitempty"`
}

// Result contains the findings from a preflight run.
type Result struct {
	Findings []finding.Finding `json:"findings"`
}

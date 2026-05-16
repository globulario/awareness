package graph

// node_ids.go — typed helpers for the prefixed node-id convention.
//
// Graph nodes are stored with a "<kind>:" prefix on the id (e.g.
// "failure_mode:etcd.leader_instability"). The unprefixed form is the one
// used in YAML truth files and database tables (failure_modes.yaml row id,
// failure_modes scylla table key). Whenever a join crosses those two worlds,
// the prefix must be applied or stripped consistently.
//
// Hand-rolling the prefix at every call site is how the 2026-05-08 prefix
// bug shipped: one subsystem wrote "failure_mode:X", another looked up "X"
// directly, and the join silently produced an empty bucket. See
// docs/awareness/composed_path_failures.md (Graph node id prefix).
//
// Use these helpers anywhere code joins graph node ids to table/YAML ids.
// New helpers can be added below for other node types as the same join
// pattern surfaces (invariant, forbidden_fix, etc.).

import "strings"

// FailureModeNodePrefix is the canonical prefix the manual extractor stamps
// on every failure_mode node id. Keep all derivation through the helpers
// below — do not hand-concatenate this string at call sites.
const FailureModeNodePrefix = "failure_mode:"

// FailureModeNodeID returns the graph node id for a failure_mode whose
// canonical id (from failure_modes.yaml) is fmID. Idempotent: passing an
// already-prefixed id returns it unchanged so callers don't have to track
// which form their input is in.
func FailureModeNodeID(fmID string) string {
	if fmID == "" {
		return ""
	}
	if strings.HasPrefix(fmID, FailureModeNodePrefix) {
		return fmID
	}
	return FailureModeNodePrefix + fmID
}

// FailureModeIDFromNode returns the unprefixed failure_mode id for a graph
// node id. If the input has no prefix it is returned unchanged so callers
// can safely funnel mixed inputs through the helper.
func FailureModeIDFromNode(nodeID string) string {
	return strings.TrimPrefix(nodeID, FailureModeNodePrefix)
}

// IsFailureModeNode reports whether nodeID is in the failure_mode namespace.
func IsFailureModeNode(nodeID string) bool {
	return strings.HasPrefix(nodeID, FailureModeNodePrefix)
}

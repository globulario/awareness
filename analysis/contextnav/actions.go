package contextnav

// actions.go — Phase 7 of the context-navigation effort. Turns the
// trace's state (graph staleness, live overlay freshness, required
// tests, finding anchor) into a small list of read-only DiagnosticAction
// entries that an agent can run directly to advance the diagnosis.
//
// The doc's rules, made concrete:
//
//   - Stale graph → emit a rebuild action with `globular awareness
//     build --clean`. Reason quotes the freshness gap so the agent
//     understands why.
//   - Absent / stale live overlay → emit a runtime_collect action with
//     `globular awareness live-snapshot`. Skipped when the overlay was
//     reported as fresh.
//   - Always emit an inspect action: `globular awareness node-context
//     --node <anchor> --zoom history --format agent`. This is the
//     pivot point the agent uses to dig into source invariant,
//     incidents, fixes, and tests for the finding.
//   - When RequiredTests is non-empty → emit a test action listing the
//     test packages. Test packages are derived from RequiredTests names
//     conservatively (we only suggest `go test ./golang/awareness/...`
//     plus any explicit package paths the caller wired in).
//   - For raw-knowledge traces → emit the rebuild action only. The
//     trace's own Warnings already say "treat as a hint, not proof",
//     and additional inspect/test suggestions could imply the match is
//     load-bearing.
//
// Safety contract: every emitted command MUST be read-only or
// test/build only. SafeToRun=true is set unconditionally; RequiresAck is
// always false because nothing here mutates cluster state. A unit test
// scans every Command string for destructive tokens — adding a mutating
// command requires explicit RequiresAck=true and an updated safety scan.

import (
	"fmt"
	"sort"
	"strings"
)

// Action kinds. Match the design doc's vocabulary.
const (
	ActionKindRebuild        = "rebuild"
	ActionKindRuntimeCollect = "runtime_collect"
	ActionKindInspect        = "inspect"
	ActionKindTest           = "test"
	ActionKindGrep           = "grep"
)

// InferActions returns the ordered NextActions for a single trace. The
// order is fixed: rebuild → runtime_collect → inspect → test. This way
// agents read the list as a prerequisite chain (fix evidence quality
// first, then dig in, then run the tests).
//
// Raw-knowledge traces get only the rebuild action because the fallback
// match is by definition a graph miss — the agent should rebuild and
// re-run before treating any of the inspect/test suggestions as load-
// bearing for the fallback id.
func InferActions(t *DecisionTrace, in *BuildInputs) []DiagnosticAction {
	if t == nil || in == nil {
		return nil
	}
	if t.FindingType == FindingRawKnowledge {
		return []DiagnosticAction{rebuildAction(in)}
	}

	actions := make([]DiagnosticAction, 0, 4)

	if in.GraphStale || !in.GraphFreshnessKnown {
		actions = append(actions, rebuildAction(in))
	}
	if needsLiveSnapshot(in.LiveOverlayStatus) {
		actions = append(actions, runtimeCollectAction(in.LiveOverlayStatus))
	}
	if anchor := pivotAnchorID(t); anchor != "" {
		actions = append(actions, inspectAction(anchor, t))
	}
	if len(in.RequiredTests) > 0 {
		actions = append(actions, testAction(in.RequiredTests))
	}

	// Sort by action-kind rank for deterministic JSON output even when
	// callers append in a different order in the future.
	sort.SliceStable(actions, func(i, j int) bool {
		return actionKindRank(actions[i].Kind) < actionKindRank(actions[j].Kind)
	})
	return actions
}

// needsLiveSnapshot returns true when the overlay status calls for a
// fresh collect. Empty status means "no overlay was attached at all" —
// also a runtime_collect signal.
func needsLiveSnapshot(status string) bool {
	switch status {
	case "stale", "absent", "failed", "partial", "":
		return true
	}
	return false
}

func actionKindRank(k string) int {
	switch k {
	case ActionKindRebuild:
		return 0
	case ActionKindRuntimeCollect:
		return 1
	case ActionKindInspect:
		return 2
	case ActionKindTest:
		return 3
	case ActionKindGrep:
		return 4
	}
	return 99
}

func rebuildAction(in *BuildInputs) DiagnosticAction {
	reason := "graph is stale; rebuild before treating findings as load-bearing"
	if !in.GraphFreshnessKnown {
		reason = "no graph freshness report available; rebuild to establish a baseline"
	}
	return DiagnosticAction{
		Kind:      ActionKindRebuild,
		Command:   "globular awareness build --clean",
		Reason:    reason,
		SafeToRun: true,
	}
}

func runtimeCollectAction(status string) DiagnosticAction {
	reason := "live overlay is absent; collect a snapshot to ground runtime evidence"
	switch status {
	case "stale":
		reason = "live overlay is stale; collect a fresh snapshot before reading runtime claims"
	case "failed":
		reason = "previous live overlay collection failed; retry to recover runtime evidence"
	case "partial":
		reason = "live overlay is partial; collect a full snapshot to plug coverage gaps"
	}
	return DiagnosticAction{
		Kind:      ActionKindRuntimeCollect,
		Command:   "globular awareness live-snapshot",
		Reason:    reason,
		SafeToRun: true,
	}
}

func inspectAction(anchorID string, t *DecisionTrace) DiagnosticAction {
	// Use --zoom history so the agent sees source invariant + prior
	// incidents + fix cases in one call, mirroring the doc example.
	cmd := fmt.Sprintf("globular awareness node-context --node %s --zoom history --format agent", anchorID)
	reason := fmt.Sprintf("inspect source invariant, incidents, fixes, and tests for %s:%s",
		t.FindingType, t.FindingID)
	return DiagnosticAction{
		Kind:      ActionKindInspect,
		Command:   cmd,
		Reason:    reason,
		SafeToRun: true,
	}
}

func testAction(required []string) DiagnosticAction {
	// Required tests are bare test names (TestFoo). We don't know the
	// package paths, so suggest the cluster-wide awareness test root
	// plus a quoted match list. The agent can narrow down once it
	// reads the inspect output.
	tests := strings.Join(required, ", ")
	return DiagnosticAction{
		Kind:      ActionKindTest,
		Command:   "go test ./awareness/... -run '" + buildRunRegex(required) + "'",
		Reason:    fmt.Sprintf("run the required tests for this finding: %s", tests),
		SafeToRun: true,
	}
}

// buildRunRegex builds a -run regex matching every required test name
// exactly. Test names are conservatively escaped (replace regex
// metachars). The empty case returns "^$" so the command stays
// well-formed but matches nothing.
func buildRunRegex(names []string) string {
	if len(names) == 0 {
		return "^$"
	}
	escaped := make([]string, 0, len(names))
	for _, n := range names {
		escaped = append(escaped, regexEscape(n))
	}
	sort.Strings(escaped)
	return "^(" + strings.Join(escaped, "|") + ")$"
}

// regexEscape escapes the small set of regex metacharacters that show
// up in Go test names. Go's stdlib has regexp.QuoteMeta but we avoid
// importing regexp here to keep the action emitter pure-string.
var regexMetacharReplacer = strings.NewReplacer(
	`\`, `\\`,
	`.`, `\.`,
	`+`, `\+`,
	`*`, `\*`,
	`?`, `\?`,
	`(`, `\(`,
	`)`, `\)`,
	`[`, `\[`,
	`]`, `\]`,
	`{`, `\{`,
	`}`, `\}`,
	`^`, `\^`,
	`$`, `\$`,
	`|`, `\|`,
)

func regexEscape(s string) string { return regexMetacharReplacer.Replace(s) }

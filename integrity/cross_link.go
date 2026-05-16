package integrity

// cross_link.go — cross-link density audit. Counts per-type orphan
// findings the awareness graph currently carries: failure_modes /
// invariants with no edges at all, plus failure_modes missing the two
// load-bearing relationship classes (tested_by → test, violates →
// invariant).
//
// Why: the navigation layer (analysis/contextnav) ranks pivots and
// generates falsifiers from these very relationships. A failure_mode
// with no `violates` edge can't produce a graph-walked source_invariant
// pivot; a failure_mode with no `tested_by` edge can't surface a
// required_test pivot. Surfacing these gaps in the integrity report
// closes the feedback loop — operators see exactly which findings need
// edge work before the next graph build.
//
// The check is read-only and intentionally narrow: it only walks the
// two finding node types (failure_mode + invariant) and the two
// relationship edge kinds (tested_by + violates). Wider-coverage
// inferences belong in extractors, not the audit layer.

import (
	"context"
	"sort"

	"github.com/globulario/awareness/graph"
)

// edge kinds we treat as evidence of a tests-link / invariant-link.
// Multiple variants exist in the codebase ("tested_by" + "verifies" +
// "validates"); accept any of them so the audit doesn't fire on
// stylistic variations.
var testsLinkEdgeKinds = map[string]bool{
	"tested_by":   true,
	"verifies":    true,
	"validates":   true,
	"validated_by": true,
}

var invariantLinkEdgeKinds = map[string]bool{
	"violates":   true,
	"constrains": true,
	"implements": true,
}

// CrossLinkDensity counts cross-link gaps that block the navigation
// layer's pivot inference and falsifier-template anchoring. Zero counts
// mean every finding has the minimum edges contextnav can use.
//
// The IDs are sorted lists rather than counts-only so an operator can
// jump from "failure_modes_without_tests = 3" to the exact ids that
// need attention.
type CrossLinkDensity struct {
	OrphanFailureModes              int      `json:"orphan_failure_modes"`
	OrphanInvariants                int      `json:"orphan_invariants"`
	FailureModesWithoutTests        int      `json:"failure_modes_without_tests"`
	FailureModesWithoutInvariants   int      `json:"failure_modes_without_invariants"`
	OrphanFailureModeIDs            []string `json:"orphan_failure_mode_ids,omitempty"`
	OrphanInvariantIDs              []string `json:"orphan_invariant_ids,omitempty"`
	FailureModesWithoutTestsIDs     []string `json:"failure_modes_without_tests_ids,omitempty"`
	FailureModesWithoutInvariantIDs []string `json:"failure_modes_without_invariants_ids,omitempty"`
}

// computeCrossLinkDensity walks failure_mode and invariant nodes and
// counts the four gap classes. Returns a zero-value struct when the
// graph has no findings (rather than nil) so JSON output stays stable.
func computeCrossLinkDensity(ctx context.Context, g *graph.Graph) CrossLinkDensity {
	out := CrossLinkDensity{}
	if g == nil {
		return out
	}

	failureModes, err := g.FindNodesByType(ctx, graph.NodeTypeFailureMode)
	if err == nil {
		for _, fm := range failureModes {
			outEdges, _ := g.Neighbors(ctx, fm.ID, "out")
			inEdges, _ := g.Neighbors(ctx, fm.ID, "in")
			if len(outEdges) == 0 && len(inEdges) == 0 {
				out.OrphanFailureModes++
				out.OrphanFailureModeIDs = append(out.OrphanFailureModeIDs, fm.ID)
				continue
			}
			all := append([]graph.Edge{}, outEdges...)
			all = append(all, inEdges...)
			if !hasEdgeToType(ctx, g, fm.ID, all, testsLinkEdgeKinds, graph.NodeTypeTest) {
				out.FailureModesWithoutTests++
				out.FailureModesWithoutTestsIDs = append(out.FailureModesWithoutTestsIDs, fm.ID)
			}
			if !hasEdgeToType(ctx, g, fm.ID, all, invariantLinkEdgeKinds, graph.NodeTypeInvariant) {
				out.FailureModesWithoutInvariants++
				out.FailureModesWithoutInvariantIDs = append(out.FailureModesWithoutInvariantIDs, fm.ID)
			}
		}
	}

	invariants, err := g.FindNodesByType(ctx, graph.NodeTypeInvariant)
	if err == nil {
		for _, inv := range invariants {
			outEdges, _ := g.Neighbors(ctx, inv.ID, "out")
			inEdges, _ := g.Neighbors(ctx, inv.ID, "in")
			if len(outEdges) == 0 && len(inEdges) == 0 {
				out.OrphanInvariants++
				out.OrphanInvariantIDs = append(out.OrphanInvariantIDs, inv.ID)
			}
		}
	}

	// Sort ID lists so JSON output is byte-stable across runs.
	sort.Strings(out.OrphanFailureModeIDs)
	sort.Strings(out.OrphanInvariantIDs)
	sort.Strings(out.FailureModesWithoutTestsIDs)
	sort.Strings(out.FailureModesWithoutInvariantIDs)
	return out
}

// hasEdgeToType returns true when at least one of the given edges
// connects nodeID to a node of targetType via an edge whose kind is in
// kinds. Lookups go through g.FindNode so the type check uses the
// authoritative node table — edge dst strings alone aren't safe to
// pattern-match on.
func hasEdgeToType(ctx context.Context, g *graph.Graph, nodeID string, edges []graph.Edge, kinds map[string]bool, targetType string) bool {
	for _, e := range edges {
		if !kinds[e.Kind] {
			continue
		}
		other := e.Dst
		if e.Dst == nodeID {
			other = e.Src
		}
		n, err := g.FindNode(ctx, other)
		if err != nil || n == nil {
			continue
		}
		if n.Type == targetType {
			return true
		}
	}
	return false
}

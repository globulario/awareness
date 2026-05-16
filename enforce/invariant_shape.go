package enforce

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/graph"
)

// InvariantShapeResult holds all shape findings across every invariant.
type InvariantShapeResult struct {
	Findings      []Finding
	InvariantsOK  int
	InvariantsBad int
}

// InvariantShapeCheck validates structural invariants of every invariant node in g.
// It runs 10 checks per invariant and accumulates findings. Partial results are
// returned even if some invariants fail to query — missing graph nodes produce
// warnings rather than errors.
func InvariantShapeCheck(ctx context.Context, g *graph.Graph) InvariantShapeResult {
	var res InvariantShapeResult

	invs, err := g.AllInvariants(ctx)
	if err != nil {
		res.Findings = append(res.Findings, Finding{
			Code:     CodeNoGraph,
			Severity: SeverityWarning,
			Message:  "InvariantShapeCheck: cannot load invariants — " + err.Error(),
		})
		return res
	}

	for _, inv := range invs {
		nodeID := "invariant:" + inv.ID
		findings := checkInvariantShape(ctx, g, nodeID, inv.ID)
		if len(findings) == 0 {
			res.InvariantsOK++
		} else {
			res.InvariantsBad++
			res.Findings = append(res.Findings, findings...)
		}
	}
	return res
}

// checkInvariantShape runs all 10 shape rules for a single invariant node.
func checkInvariantShape(ctx context.Context, g *graph.Graph, nodeID, invID string) []Finding {
	var out []Finding

	inEdges, err := g.Neighbors(ctx, nodeID, "in")
	if err != nil {
		out = append(out, Finding{
			Code: CodeNoGraph, Severity: SeverityWarning,
			Message: fmt.Sprintf("invariant %s: cannot query in-edges: %v", invID, err),
		})
		return out
	}
	outEdges, err := g.Neighbors(ctx, nodeID, "out")
	if err != nil {
		out = append(out, Finding{
			Code: CodeNoGraph, Severity: SeverityWarning,
			Message: fmt.Sprintf("invariant %s: cannot query out-edges: %v", invID, err),
		})
		return out
	}

	// Index by kind for O(1) lookups.
	hasIn := edgeKindSet(inEdges)
	hasOut := edgeKindSet(outEdges)

	// ── Check 1: No implementation ────────────────────────────────────────────
	// At least one of: implements, partially_implements, enforces must point TO this invariant.
	if !hasIn[graph.EdgeImplements] && !hasIn[graph.EdgePartiallyImplements] && !hasIn[graph.EdgeEnforces] {
		out = append(out, Finding{
			Code:     CodeInvariantNoImplementation,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("invariant %s: no implementing source file (implements/partially_implements/enforces)", invID),
			File:     nodeID,
		})
	}

	// ── Check 2: No test coverage ─────────────────────────────────────────────
	// At least one of: tested_by (outgoing) or verifies (incoming) must exist.
	if !hasOut[graph.EdgeTestedBy] && !hasIn[graph.EdgeVerifies] {
		out = append(out, Finding{
			Code:     CodeInvariantNoTestCoverage,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("invariant %s: no test coverage (tested_by or verifies edge)", invID),
			File:     nodeID,
		})
	}

	// ── Check 3: No failure mode ──────────────────────────────────────────────
	// At least one of: affects (outgoing) or violates (incoming) must exist.
	if !hasOut[graph.EdgeAffects] && !hasIn[graph.EdgeViolates] {
		out = append(out, Finding{
			Code:     CodeInvariantNoFailureMode,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("invariant %s: no linked failure mode (affects/violates edge)", invID),
			File:     nodeID,
		})
	}

	// ── Check 4: No forbidden fix ─────────────────────────────────────────────
	if !hasOut[graph.EdgeForbids] {
		out = append(out, Finding{
			Code:     CodeInvariantNoForbiddenFix,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("invariant %s: no forbidden_fix defined (forbids edge)", invID),
			File:     nodeID,
		})
	}

	// ── Check 5: Orphan implementations ───────────────────────────────────────
	// Any source_file implementing this invariant should have a graph node.
	for _, e := range inEdges {
		if e.Kind != graph.EdgeImplements && e.Kind != graph.EdgePartiallyImplements {
			continue
		}
		n, err := g.FindNode(ctx, e.Src)
		if err != nil || n == nil {
			out = append(out, Finding{
				Code:     CodeInvariantOrphanImpl,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("invariant %s: implementing node %q not found in graph", invID, e.Src),
				File:     e.Src,
			})
		}
	}

	// ── Check 6: Missing authority ────────────────────────────────────────────
	// If the invariant has implementations but no authority source is declared
	// (either on the invariant node or any of its implementing files), flag it.
	hasImpl := hasIn[graph.EdgeImplements] || hasIn[graph.EdgePartiallyImplements]
	if hasImpl && !hasOut[graph.EdgeReadsAuthority] {
		// Also check if any implementing file has reads_authority edges.
		implHasAuthority := false
		for _, e := range inEdges {
			if e.Kind != graph.EdgeImplements && e.Kind != graph.EdgePartiallyImplements {
				continue
			}
			implOut, err2 := g.Neighbors(ctx, e.Src, "out")
			if err2 != nil {
				continue
			}
			for _, oe := range implOut {
				if oe.Kind == graph.EdgeReadsAuthority {
					implHasAuthority = true
					break
				}
			}
			if implHasAuthority {
				break
			}
		}
		if !implHasAuthority {
			out = append(out, Finding{
				Code:     CodeInvariantMissingAuthority,
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("invariant %s: has implementations but no authority source declared (reads_authority)", invID),
				File:     nodeID,
			})
		}
	}

	// ── Check 7: Unverified implementation ────────────────────────────────────
	// Has implementations (implements/partially_implements) but no test has a
	// verifies edge pointing here — only weaker tested_by edges exist.
	if hasImpl && !hasIn[graph.EdgeVerifies] {
		out = append(out, Finding{
			Code:     CodeInvariantUnverifiedImpl,
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("invariant %s: implementations present but no test has a verifies edge (only tested_by)", invID),
			File:     nodeID,
		})
	}

	// ── Check 8: guards_action unreachable ────────────────────────────────────
	// If implementing files declare guards_action edges, verify at least one
	// test verifies the invariant (the action guard is only meaningful if tested).
	hasGuards := false
	for _, e := range inEdges {
		if e.Kind != graph.EdgeImplements && e.Kind != graph.EdgePartiallyImplements {
			continue
		}
		implOut, err := g.Neighbors(ctx, e.Src, "out")
		if err != nil {
			continue
		}
		for _, oe := range implOut {
			if oe.Kind == graph.EdgeGuardsAction {
				hasGuards = true
				break
			}
		}
		if hasGuards {
			break
		}
	}
	if hasGuards && !hasIn[graph.EdgeVerifies] {
		out = append(out, Finding{
			Code:     CodeInvariantGuardsUnreachable,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("invariant %s: guards_action declared but no test verifies this invariant (action guard untested)", invID),
			File:     nodeID,
		})
	}

	// ── Check 9: Violated but no test ─────────────────────────────────────────
	// Has failure modes that violate this invariant but no test verifies it.
	if hasIn[graph.EdgeViolates] && !hasIn[graph.EdgeVerifies] && !hasOut[graph.EdgeTestedBy] {
		out = append(out, Finding{
			Code:     CodeInvariantViolatedNoTest,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("invariant %s: failure mode(s) violate this invariant but no test covers it", invID),
			File:     nodeID,
		})
	}

	// ── Check 10: Forbidden fix has no guard ──────────────────────────────────
	// Every forbidden_fix node targeting this invariant via blocks_forbidden_action
	// should have at least one test that verifies the invariant.
	for _, e := range inEdges {
		if e.Kind != graph.EdgeBlocksForbiddenAction {
			continue
		}
		// The forbidden_fix blocks this invariant — there must be a test.
		if !hasIn[graph.EdgeVerifies] && !hasOut[graph.EdgeTestedBy] {
			out = append(out, Finding{
				Code:     CodeInvariantForbiddenFixNoGuard,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("invariant %s: forbidden_fix %q has no test guard", invID, e.Src),
				File:     e.Src,
			})
			break // one finding per invariant is enough
		}
	}

	return out
}

// edgeKindSet builds a set of edge kinds from a slice of edges.
func edgeKindSet(edges []graph.Edge) map[string]bool {
	m := make(map[string]bool, len(edges))
	for _, e := range edges {
		m[e.Kind] = true
	}
	return m
}

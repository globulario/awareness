package contextnav

// pivots.go — Phase 4 of the context-navigation effort. Generates ranked
// next-hop pivots for a decision trace by walking the awareness graph with
// semantic.Related and converting each result into a typed ContextPivot.
//
// Why not deeper traversal: semantic.Related already does the costly part
// (priority-queue BFS with edge weights tuned per dimension). pivots.go is
// just the formatter — it asks semantic for related nodes, attaches a Kind
// label, and ranks the results by *usefulness* (what's most actionable for
// a fix) rather than raw distance.
//
// Composition: Build runs the Phase 2 Report-derived pivots first
// (source_invariant / required_test / forbidden_fix from data the Report
// already collected), then merges graph-walked pivots on top. Duplicates by
// (Kind, ID) are dropped — Report-derived entries win because their WhyRelevant
// describes a direct invariant/test/forbidden_fix relationship that came from
// the same source the agent already saw in the Report. The merged list is
// sorted by (usefulness_rank, ID) for deterministic output.

import (
	"context"
	"fmt"
	"sort"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/semantic"
)

// PivotKind labels the destination type of a pivot. The string values stay
// stable: they're already serialized to JSON and read by MCP/CLI consumers.
const (
	PivotKindRequiredTest    = "required_test"
	PivotKindForbiddenFix    = "forbidden_fix"
	PivotKindSourceInvariant = "source_invariant"
	PivotKindRuntimeEvidence = "runtime_evidence"
	PivotKindIncident        = "incident"
	PivotKindFixCase         = "fix_case"
	PivotKindExperience      = "experience"
	PivotKindRunbook         = "runbook"
	PivotKindFile            = "file"
	PivotKindSymbol          = "symbol"
	PivotKindPackage         = "package"
	PivotKindService         = "service"
	PivotKindFailureMode     = "failure_mode"
	PivotKindDocumentation   = "documentation"
)

// pivotKindRank scores each pivot kind for sort order. Lower = more useful.
// Order matches the design doc:
//
//	required_test > forbidden_fix > source_invariant > runtime_evidence >
//	incident > fix_case > experience > runbook > file > symbol > package >
//	service > failure_mode > documentation
var pivotKindRank = map[string]int{
	PivotKindRequiredTest:    0,
	PivotKindForbiddenFix:    1,
	PivotKindSourceInvariant: 2,
	PivotKindRuntimeEvidence: 3,
	PivotKindIncident:        4,
	PivotKindFixCase:         5,
	PivotKindExperience:      6,
	PivotKindRunbook:         7,
	PivotKindFile:            8,
	PivotKindSymbol:          9,
	PivotKindPackage:         10,
	PivotKindService:         11,
	PivotKindFailureMode:     12,
	PivotKindDocumentation:   13,
}

// nodeTypeToPivotKind maps a graph node type onto the pivot Kind the agent
// should see. Unmapped types skip pivot emission so we don't surface random
// nodes the agent has no context for.
var nodeTypeToPivotKind = map[string]string{
	graph.NodeTypeInvariant:           PivotKindSourceInvariant,
	graph.NodeTypeFailureMode:         PivotKindFailureMode,
	graph.NodeTypeForbiddenFix:        PivotKindForbiddenFix,
	graph.NodeTypeTest:                PivotKindRequiredTest,
	graph.NodeTypeFixCase:             PivotKindFixCase,
	graph.NodeTypeIncident:            PivotKindIncident,
	graph.NodeTypeIncidentReport:      PivotKindIncident,
	graph.NodeTypeIncidentBundle:      PivotKindIncident,
	graph.NodeTypeExperience:          PivotKindExperience,
	graph.NodeTypeNextTimeHint:        PivotKindExperience,
	graph.NodeTypeLesson:              PivotKindExperience,
	graph.NodeTypeRunbook:             PivotKindRunbook,
	graph.NodeTypeDebugPlaybook:       PivotKindRunbook,
	graph.NodeTypeRuntimeServiceStatus: PivotKindRuntimeEvidence,
	graph.NodeTypeWorkflowReceipt:     PivotKindRuntimeEvidence,
	graph.NodeTypeStateDelta:          PivotKindRuntimeEvidence,
	graph.NodeTypeDoctorFinding:       PivotKindRuntimeEvidence,
	graph.NodeTypeSystemdStatus:       PivotKindRuntimeEvidence,
	graph.NodeTypeSourceFile:          PivotKindFile,
	graph.NodeTypeSymbol:              PivotKindSymbol,
	graph.NodeTypePackage:             PivotKindPackage,
	graph.NodeTypeGlobularService:     PivotKindService,
	graph.NodeTypeDocumentationSection: PivotKindDocumentation,
}

// pivotTargetTypes is the TargetTypes list passed to semantic.Related — the
// graph types we want surfaced as pivots. Derived from nodeTypeToPivotKind.
var pivotTargetTypes = func() []string {
	out := make([]string, 0, len(nodeTypeToPivotKind))
	for t := range nodeTypeToPivotKind {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}()

// PivotOptions tunes pivot inference. Most callers can pass the zero value;
// MaxResults defaults to 12 (a working number balancing breadth vs. noise)
// and MaxDepth to 4 (matches semantic.Related's own default).
type PivotOptions struct {
	MaxResults int
	MaxDepth   int
	// IncludeRuntime gates runtime_state / metric / receipt nodes. When false
	// (the default for non-IncludeRuntime preflight runs), runtime nodes are
	// excluded so a graph-only preflight doesn't leak stale runtime hints.
	IncludeRuntime bool
}

// InferPivots returns ranked graph-walked pivots for the finding node. Empty
// slice when the graph is nil or has no relevant neighbors. Safe to call
// with a nil graph — used by Build's fallback path.
func InferPivots(ctx context.Context, g *graph.Graph, findingNodeID string, opts PivotOptions) []ContextPivot {
	if g == nil || findingNodeID == "" {
		return nil
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = 12
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 4
	}

	related, err := semantic.Related(ctx, g, findingNodeID, semantic.RelatedOptions{
		Dimension:         semantic.DimensionAll,
		TargetTypes:       pivotTargetTypes,
		MaxDepth:          opts.MaxDepth,
		MaxResults:        opts.MaxResults * 3, // overshoot — usefulness rank trims
		IncludeRuntime:    opts.IncludeRuntime,
		IncludeProvenance: true,
	})
	if err != nil || len(related) == 0 {
		return nil
	}

	pivots := make([]ContextPivot, 0, len(related))
	for _, r := range related {
		if r.Node == nil {
			continue
		}
		kind, ok := nodeTypeToPivotKind[r.Node.Type]
		if !ok {
			continue
		}
		pivot := ContextPivot{
			Kind:        kind,
			ID:          r.Node.ID,
			Title:       r.Node.Name,
			WhyRelevant: pivotWhyRelevant(kind, r),
			Confidence:  pivotConfidence(kind, r.Distance),
		}
		// Enrich fix_case pivots with status/remaining-gap text so the agent
		// sees "previous fix is partial" without a second hop.
		if kind == PivotKindFixCase && r.Node.Metadata != nil {
			if status, _ := r.Node.Metadata["status"].(string); status != "" {
				pivot.WhyRelevant = fmt.Sprintf("%s (fix status: %s)", pivot.WhyRelevant, status)
			}
		}
		pivots = append(pivots, pivot)
	}

	return rankAndCapPivots(pivots, opts.MaxResults)
}

// pivotWhyRelevant produces the human-readable WhyRelevant string for a
// pivot. The path summary from semantic.Related already encodes the edge
// chain ("failure_mode:X --violates--> invariant:Y"); we lift that into the
// pivot directly so the agent can see WHY this pivot was suggested without
// re-running the traversal.
func pivotWhyRelevant(kind string, r semantic.SemanticRelated) string {
	if r.PathSummary != "" {
		return r.PathSummary
	}
	if r.Reason != "" {
		return fmt.Sprintf("connected via %s", r.Reason)
	}
	return fmt.Sprintf("graph-related %s", kind)
}

// pivotConfidence converts semantic distance into a 0..1 confidence score.
// Closer = higher confidence. The mapping is intentionally generous (no
// pivot drops below 0.4) so the agent doesn't dismiss anything the graph
// surfaced.
func pivotConfidence(_ string, distance float64) float64 {
	switch {
	case distance <= 1.0:
		return 0.9
	case distance <= 2.0:
		return 0.75
	case distance <= 3.5:
		return 0.6
	default:
		return 0.4
	}
}

// rankAndCapPivots sorts by (usefulness rank, ID) for deterministic order
// and trims to maxResults.
func rankAndCapPivots(pivots []ContextPivot, maxResults int) []ContextPivot {
	sort.SliceStable(pivots, func(i, j int) bool {
		ri := pivotKindRank[pivots[i].Kind]
		rj := pivotKindRank[pivots[j].Kind]
		if ri != rj {
			return ri < rj
		}
		return pivots[i].ID < pivots[j].ID
	})
	if maxResults > 0 && len(pivots) > maxResults {
		pivots = pivots[:maxResults]
	}
	return pivots
}

// mergeAndRankPivots combines Report-derived pivots (Phase 2) with graph-
// walked pivots (Phase 4). Duplicates by (Kind, ID) collapse to one entry
// — the Report-derived entry wins because its WhyRelevant came from a
// direct edge the agent already saw in the Report's other sections.
func mergeAndRankPivots(report, graphWalked []ContextPivot, maxResults int) []ContextPivot {
	seen := make(map[string]bool, len(report)+len(graphWalked))
	key := func(p ContextPivot) string { return p.Kind + "|" + p.ID }
	merged := make([]ContextPivot, 0, len(report)+len(graphWalked))
	for _, p := range report {
		if seen[key(p)] {
			continue
		}
		seen[key(p)] = true
		merged = append(merged, p)
	}
	for _, p := range graphWalked {
		if seen[key(p)] {
			continue
		}
		seen[key(p)] = true
		merged = append(merged, p)
	}
	return rankAndCapPivots(merged, maxResults)
}

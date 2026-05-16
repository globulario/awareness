package integrity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/globulario/awareness/graph"
)

// IntegritySummary holds counts of integrity findings.
type IntegritySummary struct {
	Nodes                  int `json:"nodes"`
	Edges                  int `json:"edges"`
	InvalidShapes          int `json:"invalid_shapes"`
	StaleEdges             int `json:"stale_edges"`
	MissingTests           int `json:"missing_tests"`
	Contradictions         int `json:"contradictions"`
	OrphanNodes            int `json:"orphan_nodes"`
	EdgesWithoutProvenance int `json:"edges_without_provenance"`
}

// StaleEdge describes a graph edge that references a file or node that no longer exists.
type StaleEdge struct {
	Src    string `json:"src"`
	Kind   string `json:"kind"`
	Dst    string `json:"dst"`
	Reason string `json:"reason"`
}

// ProvenanceIssue describes an edge that lacks provenance metadata.
type ProvenanceIssue struct {
	Src   string `json:"src"`
	Kind  string `json:"kind"`
	Dst   string `json:"dst"`
	Issue string `json:"issue"`
}

// IntegrityResult is the full output of a graph integrity check.
type IntegrityResult struct {
	Status                 string            `json:"status"` // healthy | warning | critical
	Summary                IntegritySummary  `json:"summary"`
	InvalidShapes          []ShapeViolation  `json:"invalid_shapes"`
	StaleEdges             []StaleEdge       `json:"stale_edges,omitempty"`
	MissingTests           []TestIssue       `json:"missing_tests,omitempty"`
	Contradictions         []Contradiction   `json:"contradictions,omitempty"`
	OrphanNodes            []string          `json:"orphan_nodes,omitempty"`
	EdgesWithoutProvenance []ProvenanceIssue `json:"edges_without_provenance,omitempty"`
	CrossLinkDensity       CrossLinkDensity  `json:"cross_link_density"`
	TrustLevels            TrustSummary      `json:"trust_levels"`
	RecommendedActions     []string          `json:"recommended_actions"`
	ExitCode               int               `json:"exit_code"`
}

// Options configures a graph integrity check.
type Options struct {
	DocsDir         string // path to docs/awareness
	RepoRoot        string // repo root for test file scanning
	DBPath          string // path to graph.db (optional)
	Strict          bool   // if true, warnings also set exit code 1
	TestResultsFile string // optional path to .awareness/test-results.json
}

// Check runs a full graph integrity check.
//
// g may be nil; graph-dependent checks (orphan nodes, stale edges, edge provenance)
// are skipped gracefully. YAML-based checks (shapes, contradictions, test refs)
// always run when DocsDir is set.
//
// Exit codes:
//
//	0 = healthy
//	1 = warning
//	2 = critical
//	3 = tool/check failure (returned as error)
func Check(ctx context.Context, opts Options, g *graph.Graph) (*IntegrityResult, error) {
	result := &IntegrityResult{}

	// Load YAML knowledge base.
	fixCases, err := loadIntegrityFixCases(opts.DocsDir)
	if err != nil {
		return nil, fmt.Errorf("integrity check: %w", err)
	}
	forbiddenFixes, err := loadIntegrityForbiddenFixes(opts.DocsDir)
	if err != nil {
		return nil, fmt.Errorf("integrity check: %w", err)
	}
	failureModes, err := loadIntegrityFailureModes(opts.DocsDir)
	if err != nil {
		return nil, fmt.Errorf("integrity check: %w", err)
	}
	causalRules, err := loadIntegrityCausalRules(opts.DocsDir)
	if err != nil {
		return nil, fmt.Errorf("integrity check: %w", err)
	}

	// Load optional CI test results.
	var ci *CITestResults
	if opts.TestResultsFile != "" {
		ci, err = loadCITestResults(opts.TestResultsFile)
		if err != nil {
			return nil, fmt.Errorf("integrity check: load test results: %w", err)
		}
	}

	// ── P0: Shape validation ────────────────────────────────────────────────────

	ffIDSet := BuildForbiddenFixIDSet(forbiddenFixes)

	result.InvalidShapes = append(result.InvalidShapes, ValidateFixCaseShapes(fixCases)...)
	result.InvalidShapes = append(result.InvalidShapes, ValidateFailureModeShapes(failureModes, ffIDSet)...)
	result.InvalidShapes = append(result.InvalidShapes, ValidateForbiddenFixShapes(forbiddenFixes)...)
	result.InvalidShapes = append(result.InvalidShapes, ValidateCausalRuleShapes(causalRules)...)

	// ── P0: Contradiction detection ─────────────────────────────────────────────

	result.Contradictions = DetectContradictions(causalRules, forbiddenFixes)

	// ── P0: Test reference integrity ────────────────────────────────────────────

	result.MissingTests = CheckTestReferences(fixCases, opts.RepoRoot, ci)

	// ── P0+P1: Graph-dependent checks (skip when no graph) ──────────────────────

	if g != nil {
		stats, statsErr := g.Stats(ctx)
		if statsErr == nil {
			result.Summary.Nodes = stats.Nodes
			result.Summary.Edges = stats.Edges
		}

		// Stale edge check: edges pointing to nodes that no longer exist.
		result.StaleEdges = checkStaleEdges(ctx, g)

		// Edge provenance check for critical edge types.
		result.EdgesWithoutProvenance = checkEdgeProvenance(ctx, g)

		// Trust level summary.
		result.TrustLevels = computeTrustSummary(ctx, g)

		// Orphan node detection (nodes with no edges).
		result.OrphanNodes = findOrphanNodes(ctx, g)

		// Per-type cross-link density audit. Counts gaps that block
		// contextnav's pivot inference (failure_mode missing tests /
		// invariant links). Independent of the all-edges orphan check.
		result.CrossLinkDensity = computeCrossLinkDensity(ctx, g)
	}

	// ── Compute summary counts ───────────────────────────────────────────────────

	result.Summary.InvalidShapes = len(result.InvalidShapes)
	result.Summary.StaleEdges = len(result.StaleEdges)
	result.Summary.MissingTests = countCriticalTestIssues(result.MissingTests)
	result.Summary.Contradictions = len(result.Contradictions)
	result.Summary.OrphanNodes = len(result.OrphanNodes)
	result.Summary.EdgesWithoutProvenance = len(result.EdgesWithoutProvenance)

	// ── Determine status and exit code ───────────────────────────────────────────

	hasCritical := false
	hasWarning := false

	for _, v := range result.InvalidShapes {
		if v.Severity == "critical" {
			hasCritical = true
		} else {
			hasWarning = true
		}
	}
	if len(result.Contradictions) > 0 {
		hasCritical = true
	}
	for _, ti := range result.MissingTests {
		if ti.Severity == "critical" {
			hasCritical = true
		} else if ti.Severity == "warning" {
			hasWarning = true
		}
	}
	if len(result.StaleEdges) > 0 {
		hasWarning = true
	}
	if len(result.EdgesWithoutProvenance) > 0 {
		hasWarning = true
	}

	switch {
	case hasCritical:
		result.Status = "critical"
		result.ExitCode = 2
	case hasWarning:
		result.Status = "warning"
		result.ExitCode = 1
	default:
		result.Status = "healthy"
		result.ExitCode = 0
	}

	result.RecommendedActions = buildRecommendedActions(result)
	return result, nil
}

// ── graph-dependent helpers ──────────────────────────────────────────────────

func checkStaleEdges(ctx context.Context, g *graph.Graph) []StaleEdge {
	var stale []StaleEdge
	edges, err := g.AllEdges(ctx)
	if err != nil {
		return nil
	}
	for _, e := range edges {
		// Check if src node exists.
		src, _ := g.FindNode(ctx, e.Src)
		if src == nil {
			stale = append(stale, StaleEdge{
				Src:    e.Src,
				Kind:   e.Kind,
				Dst:    e.Dst,
				Reason: fmt.Sprintf("source node %q does not exist", e.Src),
			})
			continue
		}
		// Check if dst node exists.
		dst, _ := g.FindNode(ctx, e.Dst)
		if dst == nil {
			stale = append(stale, StaleEdge{
				Src:    e.Src,
				Kind:   e.Kind,
				Dst:    e.Dst,
				Reason: fmt.Sprintf("destination node %q does not exist", e.Dst),
			})
		}
	}
	return stale
}

// criticalEdgeTypes are edge types for which provenance is required.
var criticalEdgeTypes = map[string]bool{
	graph.EdgeVerifiedBy:  true,
	graph.EdgeRequiresTest: true,
	graph.EdgeImplements:  true,
	graph.EdgePromotedTo:  true,
}

func checkEdgeProvenance(ctx context.Context, g *graph.Graph) []ProvenanceIssue {
	var issues []ProvenanceIssue
	edges, err := g.AllEdges(ctx)
	if err != nil {
		return nil
	}
	for _, e := range edges {
		if !criticalEdgeTypes[e.Kind] {
			continue
		}
		// Provenance is canonically on e.Provenance (backed by the
		// edges.provenance_json column). Empty map means the writer
		// supplied no provenance — treat as missing for critical edges.
		// See docs/awareness/composed_path_failures.md (edge provenance home).
		if len(e.Provenance) == 0 {
			issues = append(issues, ProvenanceIssue{
				Src:   e.Src,
				Kind:  e.Kind,
				Dst:   e.Dst,
				Issue: fmt.Sprintf("critical edge %s -[%s]-> %s has no provenance metadata", e.Src, e.Kind, e.Dst),
			})
		}
	}
	return issues
}

func computeTrustSummary(_ context.Context, _ *graph.Graph) TrustSummary {
	// Trust summary is derived from edge provenance metadata.
	// For now, return empty summary — full trust scoring requires all edges
	// to carry provenance_json, which is populated incrementally.
	return TrustSummary{}
}

func findOrphanNodes(ctx context.Context, g *graph.Graph) []string {
	// Limit orphan detection to knowledge node types to avoid noise from
	// source_file and symbol nodes which legitimately have few connections.
	interestingTypes := []string{
		graph.NodeTypeInvariant,
		graph.NodeTypeFailureMode,
		graph.NodeTypeForbiddenFix,
		graph.NodeTypeFixCase,
		graph.NodeTypeGuardrail,
	}

	var orphans []string
	for _, nt := range interestingTypes {
		nodes, err := g.FindNodesByType(ctx, nt)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			edges, err := g.Neighbors(ctx, n.ID, "out")
			if err != nil {
				continue
			}
			inEdges, err := g.Neighbors(ctx, n.ID, "in")
			if err != nil {
				continue
			}
			if len(edges) == 0 && len(inEdges) == 0 {
				orphans = append(orphans, n.ID)
			}
		}
	}
	return orphans
}

// ── helpers ───────────────────────────────────────────────────────────────────

func countCriticalTestIssues(issues []TestIssue) int {
	n := 0
	for _, ti := range issues {
		if ti.Severity == "critical" {
			n++
		}
	}
	return n
}

func buildRecommendedActions(r *IntegrityResult) []string {
	var actions []string
	seen := map[string]bool{}
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			actions = append(actions, a)
		}
	}

	for _, v := range r.InvalidShapes {
		if v.NodeType == "fix_case" && v.Field == "required_tests" {
			add("Add required_tests to all DONE fix cases in docs/awareness/fix_cases.yaml")
		}
		if v.Field == "safe_alternative" {
			add("Add safe_alternative to forbidden fix entries in docs/awareness/forbidden_fixes.yaml")
		}
	}
	for _, ti := range r.MissingTests {
		if ti.Severity == "critical" && ti.Issue == "" {
			continue
		}
		switch {
		case len(ti.Issue) > 0 && ti.Issue[:len("REQUIRED_TEST_MISSING")] == "REQUIRED_TEST_MISSING":
			add(fmt.Sprintf("Implement missing test: %s", ti.TestName))
		case len(ti.Issue) > 0 && len(ti.Issue) >= len("REQUIRED_TEST_NO_PATH") && ti.Issue[:len("REQUIRED_TEST_NO_PATH")] == "REQUIRED_TEST_NO_PATH":
			add("Rebuild graph with test source path extractor: run 'globular awareness build'")
		}
	}
	for range r.Contradictions {
		add("Fix causal rule contradiction: ensure alarm disarm appears after compact+defrag+verify-disk in recommended_fix_order")
	}
	if len(r.StaleEdges) > 0 {
		add("Remove stale edges: run 'globular awareness build' to rebuild the graph")
	}
	if len(r.EdgesWithoutProvenance) > 0 {
		add("Add provenance metadata to critical edge types: rerun awareness build with provenance tracking enabled")
	}
	return actions
}

// loadCITestResults reads a CI test results JSON file.
func loadCITestResults(path string) (*CITestResults, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CI test results %s: %w", path, err)
	}
	var raw struct {
		Passed       bool     `json:"passed"`
		FailedTests  []string `json:"failed_tests"`
		SkippedTests []string `json:"skipped_tests"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse CI test results %s: %w", path, err)
	}
	return &CITestResults{
		Passed:       raw.Passed,
		FailedTests:  raw.FailedTests,
		SkippedTests: raw.SkippedTests,
	}, nil
}

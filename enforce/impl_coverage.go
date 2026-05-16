package enforce

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
)

// InvariantImplCoverageOptions controls the thresholds and exceptions for
// invariant implementation coverage enforcement.
type InvariantImplCoverageOptions struct {
	// MinPercent is the minimum allowed implemented_percent before a finding
	// is emitted. A value of 0 uses the internal default (39.0).
	MinPercent float64

	// Enforced controls whether breaching the threshold is a SeverityError.
	// When false, the threshold is advisory only (no Finding is emitted).
	Enforced bool

	// Exceptions lists invariants that are excluded from missing_implementation
	// reporting when their expiry has not passed (or they have no expiry).
	Exceptions []ImplCoverageException
}

// ImplCoverageException exempts a specific invariant from coverage enforcement.
type ImplCoverageException struct {
	InvariantID string `yaml:"invariant_id"`
	// Reason is one of: documentation_only | external_system | runtime_only | pending_design
	Reason    string `yaml:"reason"`
	Owner     string `yaml:"owner,omitempty"`
	// ExpiresAt is an optional RFC3339 or YYYY-MM-DD date. If set and in the
	// past, the exception no longer applies and the invariant reappears in
	// missing_implementation.
	ExpiresAt string `yaml:"expires_at,omitempty"`
}

// InvariantImplCoverageResult holds measured invariant implementation coverage.
type InvariantImplCoverageResult struct {
	Total              int     `json:"total"`
	Implemented        int     `json:"implemented"`        // has implements/partially_implements/enforces in-edge
	ImplementedPercent float64 `json:"implemented_percent"`
	VerifiedByTests    int     `json:"verified_by_tests"`  // has verifies in-edge or tested_by out-edge
	AuthorityMapped    int     `json:"authority_mapped"`   // has reads_authority out-edge
	HasForbiddenFixes  int     `json:"has_forbidden_fixes"` // has forbids out-edge
	HasDecisionGuidance int    `json:"has_decision_guidance"` // has metadata["decision_guidance"] non-empty

	// MissingImplementation lists invariant IDs with no implementing edge (after exceptions).
	MissingImplementation []string `json:"missing_implementation,omitempty"`

	Threshold struct {
		MinPercent float64 `json:"min_percent"`
		Enforced   bool    `json:"enforced"`
	} `json:"threshold"`

	Exceptions []ImplCoverageException `json:"exceptions,omitempty"`

	// Findings are non-nil only when the threshold is enforced and breached.
	// They are appended to the parent Audit result and not rendered separately.
	Findings []Finding `json:"-"`
}

// defaultImplCoverageMinPercent is the threshold set to the current
// approximate implementation level so the check passes on day-1.
const defaultImplCoverageMinPercent = 39.0

// InvariantImplementationCoverage measures how many invariants have at least
// one implementing edge (implements / partially_implements / enforces) pointing
// to their invariant node, and reports a finding when the percentage falls
// below the configured threshold.
//
// g must not be nil — the caller is responsible for the nil-graph guard.
func InvariantImplementationCoverage(ctx context.Context, g *graph.Graph, opts InvariantImplCoverageOptions) InvariantImplCoverageResult {
	var res InvariantImplCoverageResult

	// Resolve threshold.
	minPct := opts.MinPercent
	if minPct <= 0 {
		minPct = defaultImplCoverageMinPercent
	}
	res.Threshold.MinPercent = minPct
	res.Threshold.Enforced = opts.Enforced

	if len(opts.Exceptions) > 0 {
		res.Exceptions = opts.Exceptions
	}

	// Build exception lookup: invariantID → active (non-expired).
	exceptActive := buildActiveExceptions(opts.Exceptions)

	// Load all invariants.
	invs, err := g.AllInvariants(ctx)
	if err != nil {
		res.Findings = append(res.Findings, Finding{
			Code:     CodeNoGraph,
			Severity: SeverityWarning,
			Message:  "InvariantImplementationCoverage: cannot load invariants — " + err.Error(),
		})
		return res
	}

	res.Total = len(invs)

	for _, inv := range invs {
		nodeID := "invariant:" + inv.ID

		inEdges, err := g.Neighbors(ctx, nodeID, "in")
		if err != nil {
			// Log but continue — partial results are better than none.
			res.Findings = append(res.Findings, Finding{
				Code:     CodeNoGraph,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("InvariantImplementationCoverage: cannot query in-edges for %s: %v", inv.ID, err),
			})
			continue
		}
		outEdges, err := g.Neighbors(ctx, nodeID, "out")
		if err != nil {
			res.Findings = append(res.Findings, Finding{
				Code:     CodeNoGraph,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("InvariantImplementationCoverage: cannot query out-edges for %s: %v", inv.ID, err),
			})
			continue
		}

		hasIn := edgeKindSet(inEdges)
		hasOut := edgeKindSet(outEdges)

		// ── Implemented ────────────────────────────────────────────────────────
		isImpl := hasIn[graph.EdgeImplements] || hasIn[graph.EdgePartiallyImplements] || hasIn[graph.EdgeEnforces]
		if isImpl {
			res.Implemented++
		} else if !exceptActive[inv.ID] {
			res.MissingImplementation = append(res.MissingImplementation, inv.ID)
		}

		// ── Verified by tests ──────────────────────────────────────────────────
		if hasIn[graph.EdgeVerifies] || hasOut[graph.EdgeTestedBy] {
			res.VerifiedByTests++
		}

		// ── Authority mapped ───────────────────────────────────────────────────
		if hasOut[graph.EdgeReadsAuthority] {
			res.AuthorityMapped++
		}

		// ── Has forbidden fixes ────────────────────────────────────────────────
		if hasOut[graph.EdgeForbids] {
			res.HasForbiddenFixes++
		}

		// ── Has decision guidance ──────────────────────────────────────────────
		if hasDecisionGuidance(ctx, g, nodeID) {
			res.HasDecisionGuidance++
		}
	}

	// Compute percentage.
	if res.Total > 0 {
		res.ImplementedPercent = float64(res.Implemented) / float64(res.Total) * 100
	}
	if res.Total == 0 {
		res.Findings = append(res.Findings, Finding{
			Code:     CodeNoGraph,
			Severity: SeverityWarning,
			Message:  "invariant implementation coverage check could not run — graph has zero invariant nodes (rebuild awareness graph)",
		})
		return res
	}

	// Threshold check.
	if res.Threshold.Enforced && res.ImplementedPercent < res.Threshold.MinPercent {
		res.Findings = append(res.Findings, Finding{
			Code:     CodeInvariantCoverageBelowThreshold,
			Severity: SeverityError,
			Message: fmt.Sprintf(
				"invariant implementation coverage %.1f%% (%d/%d implemented) is below required threshold %.1f%%",
				res.ImplementedPercent, res.Implemented, res.Total, res.Threshold.MinPercent,
			),
		})
	}

	return res
}

// buildActiveExceptions returns a set of invariant IDs whose exceptions are
// currently active (not expired).
func buildActiveExceptions(exceptions []ImplCoverageException) map[string]bool {
	now := time.Now()
	active := make(map[string]bool, len(exceptions))
	for _, ex := range exceptions {
		if ex.ExpiresAt == "" {
			active[ex.InvariantID] = true
			continue
		}
		// Try RFC3339 first, then YYYY-MM-DD.
		t, err := time.Parse(time.RFC3339, ex.ExpiresAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", ex.ExpiresAt)
		}
		if err != nil {
			// Unparseable expiry — treat as not expired (safe default).
			active[ex.InvariantID] = true
			continue
		}
		if now.Before(t) {
			active[ex.InvariantID] = true
		}
		// If expired, do NOT add to active — invariant reappears in MissingImplementation.
	}
	return active
}

// hasDecisionGuidance returns true when the invariant node's metadata contains
// a non-empty "decision_guidance" key. The node is looked up by nodeID.
func hasDecisionGuidance(ctx context.Context, g *graph.Graph, nodeID string) bool {
	n, err := g.FindNode(ctx, nodeID)
	if err != nil || n == nil {
		return false
	}
	if n.Metadata == nil {
		return false
	}
	dg, ok := n.Metadata["decision_guidance"]
	if !ok {
		return false
	}
	// decision_guidance may be a slice or a string. Accept any non-zero value.
	switch v := dg.(type) {
	case []interface{}:
		return len(v) > 0
	case []string:
		return len(v) > 0
	case string:
		return v != ""
	default:
		return dg != nil
	}
}

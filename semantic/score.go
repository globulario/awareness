package semantic

import (
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
)

// PathScore is the weighted scoring result for one awareness path.
// Higher score = more operationally relevant. Negative score = avoid this path.
//
// Weights come from docs/awareness/knowledge/path_weights.yaml.
// They are operational heuristics, not ground truth.
type PathScore struct {
	Total             float64  `json:"score"`
	TrustWeight       float64  `json:"trust_weight"`
	DomainWeight      float64  `json:"domain_weight"`
	SeverityWeight    float64  `json:"severity_weight"`
	EvidenceWeight    float64  `json:"evidence_weight"`
	PenaltyWeight     float64  `json:"penalty_weight"`
	Rank              int      `json:"rank"`
	PathType          string   `json:"path_type"`
	TrustLevel        string   `json:"trust_level"`
	Domains           []string `json:"domains"`
	DecisionRelevant  bool     `json:"decision_relevant"`
	RiskRelevant      bool     `json:"risk_relevant"`
	ProofAvailable    bool     `json:"proof_available"`
	Explanation       []string `json:"score_explanation"`
}

// ScoringContext carries evidence gathered before scoring begins.
// Nil fields mean "not checked" — they do not add penalties by themselves.
type ScoringContext struct {
	// ChangedFilePaths is the set of file paths the user is editing.
	// An exact match adds ExactFileMatchBonus.
	ChangedFilePaths map[string]bool

	// GraphIsStale indicates the graph has not been rebuilt after recent code changes.
	GraphIsStale bool

	// RuntimeIsNoop means live cluster data was not collected.
	RuntimeIsNoop bool

	// RequiredTestPassed contains test names that passed in the last CI run.
	RequiredTestPassed map[string]bool

	// RequiredTestExists contains test names that exist in the codebase.
	RequiredTestExists map[string]bool

	// Severity is the worst severity found in paths being scored.
	// Values: "critical" | "high" | "medium" | "low" | ""
	Severity string
}

// ── Weight tables (mirrors path_weights.yaml) ────────────────────────────────

var trustWeights = map[string]float64{
	integrity.TrustStrictVerified: 40,
	integrity.TrustVerified:       30,
	integrity.TrustDeclared:       15,
	integrity.TrustInferred:       5,
	integrity.TrustProposal:       -10,
	integrity.TrustStale:          -25,
	integrity.TrustInvalid:        -100,
}

var domainWeights = map[graph.EdgeDomain]float64{
	graph.DomainDecision:    25,
	graph.DomainProof:       20,
	graph.DomainRisk:        20,
	graph.DomainInformation: 8,
	graph.DomainProposal:    -5,
}

var severityWeights = map[string]float64{
	"critical": 30,
	"high":     20,
	"medium":   10,
	"low":      3,
	"unknown":  0,
	"":         0,
}

const (
	ExactFileMatchBonus      = 30.0
	ExactFunctionMatchBonus  = 35.0
	RuntimeEvidenceBonus     = 25.0
	SymptomTextMatchBonus    = 20.0
	RequiredTestExistsBonus  = 15.0
	RequiredTestPassedBonus  = 25.0

	ContradictionPenalty        = -100.0
	MissingRequiredTestPenalty  = -50.0
	InvalidEdgePenalty          = -100.0
	GraphStalePenalty           = -20.0
	NoopRuntimePenalty          = -10.0
)

// ScoreImpactPath scores a single impact path from the integrity package.
// The returned PathScore explains why the path received its score.
func ScoreImpactPath(path integrity.ImpactPath, ctx ScoringContext) PathScore {
	ps := PathScore{
		PathType: classifyPathType(path),
	}

	// ── Trust weight ─────────────────────────────────────────────────────────
	minTrust := lowestTrustInChain(path.Steps)
	ps.TrustLevel = minTrust
	if w, ok := trustWeights[minTrust]; ok {
		ps.TrustWeight = w
		ps.Total += w
		ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f trust:%s", w, minTrust))
	}

	// ── Domain weights ────────────────────────────────────────────────────────
	domains := domainsInChain(path.Steps)
	ps.Domains = domainsAsStrings(domains)
	for d, present := range domains {
		if !present {
			continue
		}
		if d == graph.DomainDecision {
			ps.DecisionRelevant = true
		}
		if d == graph.DomainRisk {
			ps.RiskRelevant = true
		}
		if d == graph.DomainProof {
			ps.ProofAvailable = true
		}
		if w, ok := domainWeights[d]; ok {
			ps.DomainWeight += w
			ps.Total += w
			ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f domain:%s", w, d))
		}
	}

	// ── Severity weight ───────────────────────────────────────────────────────
	if w, ok := severityWeights[ctx.Severity]; ok && w > 0 {
		ps.SeverityWeight = w
		ps.Total += w
		ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f severity:%s", w, ctx.Severity))
	}

	// ── Evidence bonuses ──────────────────────────────────────────────────────
	for _, step := range path.Steps {
		if ctx.ChangedFilePaths[step.NodeID] || ctx.ChangedFilePaths[step.NodeName] {
			ps.EvidenceWeight += ExactFileMatchBonus
			ps.Total += ExactFileMatchBonus
			ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f exact file match: %s", ExactFileMatchBonus, step.NodeName))
		}
		if !ctx.RuntimeIsNoop && step.NodeType == graph.NodeTypeRuntimeState {
			ps.EvidenceWeight += RuntimeEvidenceBonus
			ps.Total += RuntimeEvidenceBonus
			ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f runtime evidence match: %s", RuntimeEvidenceBonus, step.NodeName))
		}
		if step.NodeType == graph.NodeTypeTest {
			if ctx.RequiredTestPassed[step.NodeName] {
				ps.EvidenceWeight += RequiredTestPassedBonus
				ps.Total += RequiredTestPassedBonus
				ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f required test passed: %s", RequiredTestPassedBonus, step.NodeName))
			} else if ctx.RequiredTestExists[step.NodeName] {
				ps.EvidenceWeight += RequiredTestExistsBonus
				ps.Total += RequiredTestExistsBonus
				ps.Explanation = append(ps.Explanation, fmt.Sprintf("%+.0f required test exists: %s", RequiredTestExistsBonus, step.NodeName))
			} else {
				ps.PenaltyWeight += MissingRequiredTestPenalty
				ps.Total += MissingRequiredTestPenalty
				ps.Explanation = append(ps.Explanation, fmt.Sprintf("%.0f missing required test: %s", MissingRequiredTestPenalty, step.NodeName))
			}
		}
	}

	// ── Penalties ─────────────────────────────────────────────────────────────
	if ctx.GraphIsStale {
		ps.PenaltyWeight += GraphStalePenalty
		ps.Total += GraphStalePenalty
		ps.Explanation = append(ps.Explanation, fmt.Sprintf("%.0f graph stale", GraphStalePenalty))
	}
	if ctx.RuntimeIsNoop {
		ps.PenaltyWeight += NoopRuntimePenalty
		ps.Total += NoopRuntimePenalty
		ps.Explanation = append(ps.Explanation, fmt.Sprintf("%.0f noop runtime", NoopRuntimePenalty))
	}
	if minTrust == integrity.TrustInvalid {
		ps.PenaltyWeight += InvalidEdgePenalty
		ps.Total += InvalidEdgePenalty
		ps.Explanation = append(ps.Explanation, fmt.Sprintf("%.0f invalid edge in path", InvalidEdgePenalty))
	}

	return ps
}

// RankPaths scores and sorts a slice of impact paths, returning them ranked
// highest-first. Paths with invalid/stale trust cannot outrank verified paths.
func RankPaths(paths []integrity.ImpactPath, ctx ScoringContext) []ScoredPath {
	scored := make([]ScoredPath, 0, len(paths))
	for _, p := range paths {
		ps := ScoreImpactPath(p, ctx)
		scored = append(scored, ScoredPath{ImpactPath: p, Score: ps})
	}
	// Sort descending by score.
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].Score.Total > scored[j-1].Score.Total; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}
	// Assign ranks.
	for i := range scored {
		scored[i].Score.Rank = i + 1
	}
	return scored
}

// ScoredPath pairs an impact path with its computed score.
type ScoredPath struct {
	integrity.ImpactPath
	Score PathScore `json:"score_detail"`
}

// StratifyByTrust groups scored paths into per-trust buckets.
// Buckets without any paths are omitted from the result.
func StratifyByTrust(paths []ScoredPath) map[string][]ScoredPath {
	out := map[string][]ScoredPath{}
	for _, sp := range paths {
		t := sp.Score.TrustLevel
		if t == "" {
			t = integrity.TrustDeclared
		}
		out[t] = append(out[t], sp)
	}
	return out
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// lowestTrustInChain returns the weakest trust level across all steps.
func lowestTrustInChain(steps []integrity.ImpactStep) string {
	// trust priority order (higher index = weaker)
	order := []string{
		integrity.TrustStrictVerified,
		integrity.TrustVerified,
		integrity.TrustDeclared,
		integrity.TrustInferred,
		integrity.TrustProposal,
		integrity.TrustStale,
		integrity.TrustInvalid,
	}
	rank := func(t string) int {
		for i, v := range order {
			if v == t {
				return i
			}
		}
		return len(order)
	}
	// Start at the best possible trust level; walk down to the actual minimum.
	// This allows strict_verified and verified paths to get their full weight
	// bonus, not just the declared baseline.
	worst := integrity.TrustStrictVerified
	if len(steps) == 0 {
		return integrity.TrustDeclared
	}
	worstRank := rank(worst)
	for _, s := range steps {
		if s.Trust == "" {
			continue
		}
		if r := rank(s.Trust); r > worstRank {
			worst = s.Trust
			worstRank = r
		}
	}
	return worst
}

// domainsInChain returns a set of domains present in the path steps.
func domainsInChain(steps []integrity.ImpactStep) map[graph.EdgeDomain]bool {
	out := map[graph.EdgeDomain]bool{}
	for _, s := range steps {
		if s.Predicate == "" {
			continue
		}
		out[graph.DomainForEdgeKind(s.Predicate)] = true
	}
	return out
}

// domainsAsStrings converts the domain set to a sorted string slice.
func domainsAsStrings(domains map[graph.EdgeDomain]bool) []string {
	order := []graph.EdgeDomain{
		graph.DomainInformation,
		graph.DomainDecision,
		graph.DomainProof,
		graph.DomainRisk,
		graph.DomainProposal,
	}
	var out []string
	for _, d := range order {
		if domains[d] {
			out = append(out, string(d))
		}
	}
	return out
}

// classifyPathType assigns a decision path type based on the path's terminal node type.
func classifyPathType(path integrity.ImpactPath) string {
	if len(path.Steps) == 0 {
		return "unknown"
	}
	last := path.Steps[len(path.Steps)-1]
	switch last.NodeType {
	case graph.NodeTypeTest, graph.NodeTypeFixCase:
		return "test_closure_path"
	case graph.NodeTypeForbiddenFix:
		return "pre_edit_path"
	case graph.NodeTypeInvariant:
		return "pre_edit_path"
	case graph.NodeTypeFailureMode:
		return "risk"
	case graph.NodeTypeRemediationWorkflow:
		return "runtime_remediation_path"
	case graph.NodeTypeAwarenessProposal:
		return "proposal_review_path"
	default:
		// Classify by predicates in the chain.
		for _, s := range path.Steps {
			if graph.IsDecisionEdge(s.Predicate) {
				return "pre_edit_path"
			}
		}
		return "information_path"
	}
}

// FormatPathSteps returns a compact string representation of path steps.
func FormatPathSteps(steps []integrity.ImpactStep) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if s.Predicate != "" {
			out = append(out, fmt.Sprintf("-%s->", s.Predicate))
		}
		label := s.NodeName
		if label == "" {
			label = s.NodeID
		}
		out = append(out, fmt.Sprintf("%s:%s", s.NodeType, label))
	}
	return out
}

// DescribePathTrust returns a human-readable trust summary for a path.
func DescribePathTrust(trust string) string {
	switch trust {
	case integrity.TrustStrictVerified:
		return "verified by CI test"
	case integrity.TrustVerified:
		return "verified by extractor"
	case integrity.TrustDeclared:
		return "declared in YAML, no test proof"
	case integrity.TrustInferred:
		return "inferred heuristic — treat as diagnostic only"
	case integrity.TrustProposal:
		return "pending proposal — not yet authoritative"
	case integrity.TrustStale:
		return "stale — source changed; rebuild graph before relying on this path"
	case integrity.TrustInvalid:
		return "invalid — referenced file or test is missing; do not use"
	default:
		return "unknown trust level"
	}
}

// BestTrustLabel returns a one-word confidence label suitable for API output.
func BestTrustLabel(trust string) string {
	switch trust {
	case integrity.TrustStrictVerified, integrity.TrustVerified:
		return "high"
	case integrity.TrustDeclared:
		return "medium"
	case integrity.TrustInferred, integrity.TrustProposal:
		return "low"
	default:
		return "unknown"
	}
}

// HasForbiddenAction returns true if any step reaches a forbidden_fix node.
func HasForbiddenAction(steps []integrity.ImpactStep) bool {
	for _, s := range steps {
		if s.NodeType == graph.NodeTypeForbiddenFix {
			return true
		}
	}
	return false
}

// RequiredTestNames returns all test names in the path steps.
func RequiredTestNames(steps []integrity.ImpactStep) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range steps {
		if s.NodeType == graph.NodeTypeTest && !seen[s.NodeName] {
			out = append(out, s.NodeName)
			seen[s.NodeName] = true
		}
	}
	return out
}

// ForbiddenActionNames returns all forbidden_fix node names in the path steps.
func ForbiddenActionNames(steps []integrity.ImpactStep) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range steps {
		if s.NodeType == graph.NodeTypeForbiddenFix && !seen[s.NodeName] {
			out = append(out, s.NodeName)
			seen[s.NodeName] = true
		}
	}
	return out
}

// InvariantNames returns all invariant node names in the path steps.
func InvariantNames(steps []integrity.ImpactStep) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range steps {
		if s.NodeType == graph.NodeTypeInvariant && !seen[s.NodeName] {
			out = append(out, s.NodeName)
			seen[s.NodeName] = true
		}
	}
	return out
}

// RequiredBehavior synthesizes a short list of required behaviors from the path.
// This is derived from invariant and forbidden_fix nodes.
func RequiredBehavior(steps []integrity.ImpactStep) []string {
	var out []string
	for _, s := range steps {
		switch s.NodeType {
		case graph.NodeTypeInvariant:
			label := s.NodeName
			if label == "" {
				label = s.NodeID
			}
			out = append(out, "Must satisfy invariant: "+label)
		case graph.NodeTypeForbiddenFix:
			label := s.NodeName
			if label == "" {
				label = s.NodeID
			}
			out = append(out, "Avoid forbidden action: "+strings.ReplaceAll(label, "_", " "))
		}
	}
	return out
}

package semantic_test

import (
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
	"github.com/globulario/awareness/semantic"
)

// ── PathScoring tests ────────────────────────────────────────────────────────

// TestPathScoring_VerifiedDecisionPathBeatsDeclaredInformationPath verifies that
// a verified decision path scores higher than a declared information-only path.
func TestPathScoring_VerifiedDecisionPathBeatsDeclaredInformationPath(t *testing.T) {
	ctx := semantic.ScoringContext{}

	verifiedDecision := integrity.ImpactPath{
		ChangedFile: "golang/cluster_controller/reconcile.go",
		Confidence:  "high",
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeForbiddenFix, NodeName: "restart_without_verification", Predicate: graph.EdgeForbids, Trust: integrity.TrustVerified},
		},
	}
	declaredInfo := integrity.ImpactPath{
		ChangedFile: "golang/cluster_controller/reconcile.go",
		Confidence:  "medium",
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeGoPackage, NodeName: "cluster_controller", Predicate: graph.EdgeImports, Trust: integrity.TrustDeclared},
		},
	}

	scoreA := semantic.ScoreImpactPath(verifiedDecision, ctx)
	scoreB := semantic.ScoreImpactPath(declaredInfo, ctx)

	if scoreA.Total <= scoreB.Total {
		t.Errorf("verified decision path (%.1f) must score higher than declared information path (%.1f)", scoreA.Total, scoreB.Total)
	}
}

// TestPathScoring_InvalidPathRanksLast verifies that an invalid path scores lower
// than any valid path regardless of domain.
func TestPathScoring_InvalidPathRanksLast(t *testing.T) {
	ctx := semantic.ScoringContext{}

	invalid := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeTest, NodeName: "TestMissing", Predicate: graph.EdgeTestedBy, Trust: integrity.TrustInvalid},
		},
	}
	declared := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeInvariant, NodeName: "basic.invariant", Predicate: graph.EdgeImplements, Trust: integrity.TrustDeclared},
		},
	}

	scoreInvalid := semantic.ScoreImpactPath(invalid, ctx)
	scoreDeclared := semantic.ScoreImpactPath(declared, ctx)

	if scoreInvalid.Total >= scoreDeclared.Total {
		t.Errorf("invalid path (%.1f) must score lower than declared path (%.1f)", scoreInvalid.Total, scoreDeclared.Total)
	}
}

// TestPathScoring_CriticalRiskBoostsPath verifies that critical severity adds
// a meaningful boost compared to low severity.
func TestPathScoring_CriticalRiskBoostsPath(t *testing.T) {
	ctxCritical := semantic.ScoringContext{Severity: "critical"}
	ctxLow := semantic.ScoringContext{Severity: "low"}

	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeFailureMode, NodeName: "leader_failover_loop", Predicate: graph.EdgeViolates, Trust: integrity.TrustDeclared},
		},
	}

	scoreCrit := semantic.ScoreImpactPath(path, ctxCritical)
	scoreLow := semantic.ScoreImpactPath(path, ctxLow)

	if scoreCrit.Total <= scoreLow.Total {
		t.Errorf("critical severity path (%.1f) must score higher than low severity (%.1f)", scoreCrit.Total, scoreLow.Total)
	}
}

// TestPathScoring_RequiredTestPassedBoostsPath verifies that a required test
// that has passed gives a higher score than one that merely exists.
func TestPathScoring_RequiredTestPassedBoostsPath(t *testing.T) {
	testName := "TestAtomicStateMutation"
	ctxPassed := semantic.ScoringContext{
		RequiredTestPassed: map[string]bool{testName: true},
		RequiredTestExists: map[string]bool{testName: true},
	}
	ctxExists := semantic.ScoringContext{
		RequiredTestExists: map[string]bool{testName: true},
	}

	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeTest, NodeName: testName, Predicate: graph.EdgeTestedBy, Trust: integrity.TrustVerified},
		},
	}

	scorePassed := semantic.ScoreImpactPath(path, ctxPassed)
	scoreExists := semantic.ScoreImpactPath(path, ctxExists)

	if scorePassed.Total <= scoreExists.Total {
		t.Errorf("test-passed path (%.1f) must score higher than test-exists path (%.1f)", scorePassed.Total, scoreExists.Total)
	}
}

// TestPathScoring_ProposalPathPenalizedOutsideProposalContext verifies that
// proposal-trust paths are penalized compared to declared paths.
func TestPathScoring_ProposalPathPenalizedOutsideProposalContext(t *testing.T) {
	ctx := semantic.ScoringContext{}

	proposal := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeInvariant, NodeName: "proposal.inv", Predicate: graph.EdgeImplements, Trust: integrity.TrustProposal},
		},
	}
	declared := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeInvariant, NodeName: "declared.inv", Predicate: graph.EdgeImplements, Trust: integrity.TrustDeclared},
		},
	}

	scoreProposal := semantic.ScoreImpactPath(proposal, ctx)
	scoreDeclared := semantic.ScoreImpactPath(declared, ctx)

	if scoreProposal.Total >= scoreDeclared.Total {
		t.Errorf("proposal path (%.1f) must score lower than declared path (%.1f) outside proposal context", scoreProposal.Total, scoreDeclared.Total)
	}
}

// TestPathScoring_ExplainsWeights verifies that the score explanation is non-empty
// and contains at least one signed entry for each contributor.
func TestPathScoring_ExplainsWeights(t *testing.T) {
	ctx := semantic.ScoringContext{Severity: "high", GraphIsStale: true}

	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeForbiddenFix, NodeName: "bad_fix", Predicate: graph.EdgeForbids, Trust: integrity.TrustVerified},
		},
	}

	score := semantic.ScoreImpactPath(path, ctx)

	if len(score.Explanation) == 0 {
		t.Error("score.Explanation must be non-empty")
	}

	// Must have at least trust, domain, severity, and penalty entries.
	hasPlus := false
	hasMinus := false
	for _, e := range score.Explanation {
		if len(e) > 0 && e[0] == '+' {
			hasPlus = true
		}
		if len(e) > 0 && e[0] == '-' {
			hasMinus = true
		}
	}
	if !hasPlus {
		t.Errorf("explanation missing positive weight entry; got: %v", score.Explanation)
	}
	if !hasMinus {
		t.Errorf("explanation missing negative penalty entry (graph_stale should appear); got: %v", score.Explanation)
	}
}

// ── Graph Domain tests ───────────────────────────────────────────────────────

// TestGraphDomains_FileImplementsInvariant_IsInformation verifies the implements
// edge kind is classified as information domain.
func TestGraphDomains_FileImplementsInvariant_IsInformation(t *testing.T) {
	domain := graph.DomainForEdgeKind(graph.EdgeImplements)
	if domain != graph.DomainInformation {
		t.Errorf("EdgeImplements domain = %q, want information", domain)
	}
}

// TestGraphDomains_ForbiddenFixForbidsAction_IsDecision verifies the forbids
// edge kind is classified as decision domain.
func TestGraphDomains_ForbiddenFixForbidsAction_IsDecision(t *testing.T) {
	domain := graph.DomainForEdgeKind(graph.EdgeForbids)
	if domain != graph.DomainDecision {
		t.Errorf("EdgeForbids domain = %q, want decision", domain)
	}
}

// TestGraphDomains_TestVerifiesFixCase_IsProof verifies the tested_by edge kind
// is classified as proof domain.
func TestGraphDomains_TestVerifiesFixCase_IsProof(t *testing.T) {
	domain := graph.DomainForEdgeKind(graph.EdgeTestedBy)
	if domain != graph.DomainProof {
		t.Errorf("EdgeTestedBy domain = %q, want proof", domain)
	}
}

// TestGraphDomains_FailureModeViolatesInvariant_IsRisk verifies the violates
// edge kind is classified as risk domain.
func TestGraphDomains_FailureModeViolatesInvariant_IsRisk(t *testing.T) {
	domain := graph.DomainForEdgeKind(graph.EdgeViolates)
	if domain != graph.DomainRisk {
		t.Errorf("EdgeViolates domain = %q, want risk", domain)
	}
}

// TestGraphDomains_ProposalPromotesToKnowledge_IsProposal verifies the proposes
// edge kind is classified as proposal domain.
func TestGraphDomains_ProposalPromotesToKnowledge_IsProposal(t *testing.T) {
	domain := graph.DomainForEdgeKind(graph.EdgeProposes)
	if domain != graph.DomainProposal {
		t.Errorf("EdgeProposes domain = %q, want proposal", domain)
	}
}

// TestGraphDomains_UnknownEdge_DefaultsToInformation verifies that unknown edge
// kinds safely default to information (not decision or risk).
func TestGraphDomains_UnknownEdge_DefaultsToInformation(t *testing.T) {
	domain := graph.DomainForEdgeKind("some_unknown_future_edge")
	if domain != graph.DomainInformation {
		t.Errorf("unknown edge domain = %q, want information (safe default)", domain)
	}
}

// ── Trust stratification tests ───────────────────────────────────────────────

// TestDecisionContext_DeclaredPathReturnedWithMediumConfidence verifies that
// declared-trust paths are stratified into their own bucket.
func TestDecisionContext_DeclaredPathReturnedWithMediumConfidence(t *testing.T) {
	paths := []semantic.ScoredPath{
		{ImpactPath: integrity.ImpactPath{Confidence: "medium", Steps: []integrity.ImpactStep{{Trust: integrity.TrustDeclared}}}, Score: semantic.PathScore{TrustLevel: integrity.TrustDeclared}},
	}
	strata := semantic.StratifyByTrust(paths)
	if _, ok := strata[integrity.TrustDeclared]; !ok {
		t.Error("declared path must appear in declared stratum")
	}
	if label := semantic.BestTrustLabel(integrity.TrustDeclared); label != "medium" {
		t.Errorf("BestTrustLabel(declared) = %q, want medium", label)
	}
}

// TestDecisionContext_InferredPathDiagnosticsOnly verifies that inferred-trust
// paths produce low confidence labels.
func TestDecisionContext_InferredPathDiagnosticsOnly(t *testing.T) {
	label := semantic.BestTrustLabel(integrity.TrustInferred)
	if label != "low" {
		t.Errorf("BestTrustLabel(inferred) = %q, want low", label)
	}
}

// TestDecisionContext_StalePathCannotDriveAction verifies stale paths score
// lower than declared paths.
func TestDecisionContext_StalePathCannotDriveAction(t *testing.T) {
	ctx := semantic.ScoringContext{}
	stale := integrity.ImpactPath{Steps: []integrity.ImpactStep{{NodeType: graph.NodeTypeInvariant, Predicate: graph.EdgeImplements, Trust: integrity.TrustStale}}}
	declared := integrity.ImpactPath{Steps: []integrity.ImpactStep{{NodeType: graph.NodeTypeInvariant, Predicate: graph.EdgeImplements, Trust: integrity.TrustDeclared}}}

	scoreStale := semantic.ScoreImpactPath(stale, ctx)
	scoreDeclared := semantic.ScoreImpactPath(declared, ctx)

	if scoreStale.Total >= scoreDeclared.Total {
		t.Errorf("stale path (%.1f) must score lower than declared path (%.1f)", scoreStale.Total, scoreDeclared.Total)
	}
}

// TestDecisionContext_InvalidPathCannotRecommendAction verifies invalid paths
// score below any valid path.
func TestDecisionContext_InvalidPathCannotRecommendAction(t *testing.T) {
	ctx := semantic.ScoringContext{}
	invalid := integrity.ImpactPath{Steps: []integrity.ImpactStep{{NodeType: graph.NodeTypeTest, Predicate: graph.EdgeTestedBy, Trust: integrity.TrustInvalid}}}
	declared := integrity.ImpactPath{Steps: []integrity.ImpactStep{{NodeType: graph.NodeTypeTest, Predicate: graph.EdgeTestedBy, Trust: integrity.TrustDeclared}}}

	scoreInvalid := semantic.ScoreImpactPath(invalid, ctx)
	scoreDeclared := semantic.ScoreImpactPath(declared, ctx)

	if scoreInvalid.Total >= scoreDeclared.Total {
		t.Errorf("invalid path (%.1f) must score below declared path (%.1f)", scoreInvalid.Total, scoreDeclared.Total)
	}
}

// TestDecisionContext_NoBareNoMatchWhenLowerTrustPathsExist verifies that
// StratifyByTrust returns at least the lower-trust bucket rather than empty.
func TestDecisionContext_NoBareNoMatchWhenLowerTrustPathsExist(t *testing.T) {
	paths := []semantic.ScoredPath{
		{ImpactPath: integrity.ImpactPath{Steps: []integrity.ImpactStep{{Trust: integrity.TrustInferred}}}, Score: semantic.PathScore{TrustLevel: integrity.TrustInferred}},
	}
	strata := semantic.StratifyByTrust(paths)
	if len(strata) == 0 {
		t.Error("StratifyByTrust must never return empty when paths exist — lower-trust paths must be surfaced, not hidden")
	}
}

// ── Decision path type tests ─────────────────────────────────────────────────

// TestDecisionPathType_PreEdit verifies that a path terminating at forbidden_fix
// is classified as pre_edit_path.
func TestDecisionPathType_PreEdit(t *testing.T) {
	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeForbiddenFix, NodeName: "bad_action", Predicate: graph.EdgeForbids, Trust: integrity.TrustDeclared},
		},
	}
	score := semantic.ScoreImpactPath(path, semantic.ScoringContext{})
	if score.PathType != "pre_edit_path" {
		t.Errorf("path terminating at forbidden_fix must be pre_edit_path, got %q", score.PathType)
	}
}

// TestDecisionPathType_RuntimeRemediation verifies paths to remediation_workflow
// nodes are classified correctly.
func TestDecisionPathType_RuntimeRemediation(t *testing.T) {
	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeRemediationWorkflow, NodeName: "restart_controller", Predicate: graph.EdgeRemediatedBy, Trust: integrity.TrustDeclared},
		},
	}
	score := semantic.ScoreImpactPath(path, semantic.ScoringContext{})
	if score.PathType != "runtime_remediation_path" {
		t.Errorf("path to remediation_workflow must be runtime_remediation_path, got %q", score.PathType)
	}
}

// TestDecisionPathType_TestClosure verifies paths to test nodes are classified
// as test_closure_path.
func TestDecisionPathType_TestClosure(t *testing.T) {
	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeTest, NodeName: "TestReconcileInvariant", Predicate: graph.EdgeTestedBy, Trust: integrity.TrustVerified},
		},
	}
	score := semantic.ScoreImpactPath(path, semantic.ScoringContext{})
	if score.PathType != "test_closure_path" {
		t.Errorf("path to test must be test_closure_path, got %q", score.PathType)
	}
}

// TestDecisionPathType_ProposalReview verifies paths to proposal nodes are
// classified as proposal_review_path.
func TestDecisionPathType_ProposalReview(t *testing.T) {
	path := integrity.ImpactPath{
		Steps: []integrity.ImpactStep{
			{NodeType: graph.NodeTypeAwarenessProposal, NodeName: "proposal.fix_001", Predicate: graph.EdgeProposes, Trust: integrity.TrustProposal},
		},
	}
	score := semantic.ScoreImpactPath(path, semantic.ScoringContext{})
	if score.PathType != "proposal_review_path" {
		t.Errorf("path to proposal must be proposal_review_path, got %q", score.PathType)
	}
}

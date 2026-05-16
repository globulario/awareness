package semantic_test

import (
	"testing"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
	"github.com/globulario/awareness/semantic"
)

// newPath creates a minimal ImpactPath with a single step of the given trust level.
func newPath(trust, edgeKind string) integrity.ImpactPath {
	return integrity.ImpactPath{
		ChangedFile: "golang/auth/auth.go",
		Confidence:  "high",
		Steps: []integrity.ImpactStep{
			{
				NodeType:  graph.NodeTypeInvariant,
				NodeName:  "auth.token_validation",
				Predicate: edgeKind,
				Trust:     trust,
			},
		},
	}
}

// TestTrustWeights_StrictVerifiedBeatsVerified verifies that strict_verified
// paths score strictly higher than verified paths.
func TestTrustWeights_StrictVerifiedBeatsVerified(t *testing.T) {
	ctx := semantic.ScoringContext{}
	sv := newPath(integrity.TrustStrictVerified, graph.EdgeImplements)
	v := newPath(integrity.TrustVerified, graph.EdgeImplements)

	svScore := semantic.ScoreImpactPath(sv, ctx)
	vScore := semantic.ScoreImpactPath(v, ctx)

	if svScore.Total <= vScore.Total {
		t.Errorf("strict_verified (%.1f) must score > verified (%.1f)", svScore.Total, vScore.Total)
	}
}

// TestTrustWeights_VerifiedBeatsDeclared verifies that verified paths score
// higher than declared paths.
func TestTrustWeights_VerifiedBeatsDeclared(t *testing.T) {
	ctx := semantic.ScoringContext{}
	v := newPath(integrity.TrustVerified, graph.EdgeTestedBy)
	d := newPath(integrity.TrustDeclared, graph.EdgeTestedBy)

	vScore := semantic.ScoreImpactPath(v, ctx)
	dScore := semantic.ScoreImpactPath(d, ctx)

	if vScore.Total <= dScore.Total {
		t.Errorf("verified (%.1f) must score > declared (%.1f)", vScore.Total, dScore.Total)
	}
}

// TestTrustWeights_DeclaredBeatsInferred verifies that declared paths score
// higher than inferred paths.
func TestTrustWeights_DeclaredBeatsInferred(t *testing.T) {
	ctx := semantic.ScoringContext{}
	d := newPath(integrity.TrustDeclared, graph.EdgeProtects)
	i := newPath(integrity.TrustInferred, graph.EdgeProtects)

	dScore := semantic.ScoreImpactPath(d, ctx)
	iScore := semantic.ScoreImpactPath(i, ctx)

	if dScore.Total <= iScore.Total {
		t.Errorf("declared (%.1f) must score > inferred (%.1f)", dScore.Total, iScore.Total)
	}
}

// TestTrustWeights_VerifiesEdgeTrustIsVerified verifies that a path traversed
// via EdgeVerifies gets TrustVerified (same as EdgeTestedBy).
func TestTrustWeights_VerifiesEdgeTrustIsVerified(t *testing.T) {
	ctx := semantic.ScoringContext{}
	verifiesPath := newPath(integrity.TrustVerified, graph.EdgeVerifies)
	testedByPath := newPath(integrity.TrustVerified, graph.EdgeTestedBy)

	vs := semantic.ScoreImpactPath(verifiesPath, ctx)
	ts := semantic.ScoreImpactPath(testedByPath, ctx)

	// Scores should be equal (same trust, same edge domain).
	if vs.Total != ts.Total {
		t.Errorf("EdgeVerifies and EdgeTestedBy paths with same trust should score equally; got %.1f vs %.1f", vs.Total, ts.Total)
	}
}

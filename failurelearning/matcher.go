package failurelearning

import (
	"context"
	"strings"

	"github.com/globulario/awareness/failuregraph"
)

// CompareToGraph compares a FailureLearningExtract against the existing Failure
// Graph and returns the best matching category + what kind of proposal to make.
func CompareToGraph(ctx context.Context, extract FailureLearningExtract, fg *failuregraph.Store) (*FailureLearningMatch, error) {
	// Try to match each raw error against the failure graph.
	var bestExp *failuregraph.FailureExplanation
	for _, raw := range extract.RawErrors {
		if raw == "" {
			continue
		}
		exp, err := failuregraph.MatchError(ctx, fg, failuregraph.MatchErrorRequest{
			RawError:      raw,
			SemanticAtoms: extract.SemanticAtoms,
			LiveSignals:   extract.LiveSignals,
		})
		if err != nil {
			return nil, err
		}
		if exp == nil {
			continue
		}
		if bestExp == nil || exp.Score > bestExp.Score {
			bestExp = exp
		}
	}

	// If no direct match, try FindSimilar for broader signal matching.
	if bestExp == nil && len(extract.RawErrors) > 0 {
		sims, err := failuregraph.FindSimilar(ctx, fg, failuregraph.SimilarFailureRequest{
			RawError:      strings.Join(extract.RawErrors, " "),
			SemanticAtoms: extract.SemanticAtoms,
			LiveSignals:   extract.LiveSignals,
			Limit:         1,
		})
		if err != nil {
			return nil, err
		}
		if len(sims) > 0 {
			bestExp = &sims[0]
		}
	}

	// No match in the graph.
	if bestExp == nil {
		if len(extract.RootCauses) > 0 && len(extract.Resolutions) > 0 {
			return &FailureLearningMatch{
				ProposalKind: KindCreateCategory,
				Confidence:   failuregraph.ConfidenceLow,
				MatchScore:   0.0,
			}, nil
		}
		return &FailureLearningMatch{
			ProposalKind: KindNoReusableKnowledge,
			Confidence:   failuregraph.ConfidenceNone,
			MatchScore:   0.0,
		}, nil
	}

	match := &FailureLearningMatch{
		CategoryID:   bestExp.Category.ID,
		CategoryName: bestExp.Category.Name,
		MatchScore:   bestExp.Score,
		Confidence:   bestExp.Confidence,
	}

	// Determine what kind of proposal to make by comparing the extract against
	// what the category already has.
	match.ProposalKind = determineProposalKind(ctx, extract, bestExp, fg)

	return match, nil
}

// determineProposalKind decides the most specific proposal kind given the match.
// When raw errors are present and a category match exists, the default is
// KindAddSignature — recording the new error observation is always useful.
// Only if there are no raw errors do we look at what other novel content to add.
func determineProposalKind(ctx context.Context, extract FailureLearningExtract, exp *failuregraph.FailureExplanation, fg *failuregraph.Store) string {
	// If we have raw errors, always propose adding a signature (the primary observation record).
	if len(extract.RawErrors) > 0 {
		return KindAddSignature
	}

	// No raw errors — check for other novel content in priority order.

	// New wrong fixes not in the category.
	existingWrong := nodeNames(exp.WrongFixes)
	for _, wf := range extract.WrongFixes {
		if !existingWrong[strings.ToLower(wf)] {
			return KindAddWrongFix
		}
	}

	// New regression tests.
	existingTests := nodeNames(exp.RequiredTests)
	for _, t := range extract.RegressionTests {
		if !existingTests[strings.ToLower(t)] {
			return KindAddRegressionTest
		}
	}

	// New causes.
	existingCauses := nodeNames(exp.LikelyCauses)
	for _, c := range extract.RootCauses {
		if !existingCauses[strings.ToLower(c)] {
			return KindAddCause
		}
	}

	// Default for high-confidence match — add signature.
	return KindAddSignature
}

// nodeNames builds a lowercase name set from a slice of FailureNodes.
func nodeNames(nodes []failuregraph.FailureNode) map[string]bool {
	m := make(map[string]bool)
	for _, n := range nodes {
		m[strings.ToLower(n.Name)] = true
		m[strings.ToLower(n.Summary)] = true
	}
	return m
}

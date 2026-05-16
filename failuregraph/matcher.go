package failuregraph

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// Scoring weights for each match signal.
const (
	scoreExactSignature    = 0.40
	scoreRegexSignature    = 0.30
	scoreKeywordSignature  = 0.15
	scoreComponentOverlap  = 0.15
	scoreSemanticAtom      = 0.25
	scoreWorkflowStage     = 0.20
	scoreInvariantOverlap  = 0.25
	scoreLiveSignal        = 0.20
)

// MatchError matches a raw error string against the failure knowledge graph
// and returns a full FailureExplanation for the best-scoring category.
// Returns nil explanation (no error) when nothing matches confidently.
func MatchError(ctx context.Context, s *Store, req MatchErrorRequest) (*FailureExplanation, error) {
	normalized := NormalizeErrorSignature(req.RawError)

	sigs, err := s.AllSignatures(ctx)
	if err != nil {
		return nil, err
	}

	scores := map[string]float64{}

	for _, sig := range sigs {
		if sig.CategoryID == "" {
			continue
		}
		var delta float64
		switch sig.MatcherKind {
		case MatcherKindExact:
			if sig.NormalizedSignature != "" && strings.Contains(normalized, sig.NormalizedSignature) {
				delta = scoreExactSignature
			} else if sig.NormalizedSignature != "" && strings.Contains(normalized, sig.Signature) {
				delta = scoreExactSignature
			}
		case MatcherKindRegex:
			if sig.MatcherPattern != "" {
				re, err := regexp.Compile("(?i)" + sig.MatcherPattern)
				if err == nil && re.MatchString(req.RawError) {
					delta = scoreRegexSignature
				}
			}
		case MatcherKindKeyword:
			keywords := strings.Fields(sig.MatcherPattern)
			if len(keywords) > 0 && ContainsKeywords(normalized, keywords) {
				// All keywords found in a template pattern — score like regex.
				delta = scoreRegexSignature
			}
		}
		if delta > 0 {
			scores[sig.CategoryID] += delta
		}
	}

	// Component overlap: if the category name contains a component keyword
	if req.Component != "" {
		compLower := strings.ToLower(req.Component)
		cats, _ := s.ListCategories(ctx)
		for _, cat := range cats {
			if strings.Contains(strings.ToLower(cat.Name), compLower) ||
				strings.Contains(strings.ToLower(cat.Summary), compLower) {
				scores[cat.ID] += scoreComponentOverlap
			}
		}
	}

	// Semantic atom overlap
	for _, atom := range req.SemanticAtoms {
		cats, _ := s.ListCategories(ctx)
		atomLower := strings.ToLower(atom)
		for _, cat := range cats {
			if strings.Contains(strings.ToLower(cat.Name), atomLower) ||
				strings.Contains(strings.ToLower(cat.Summary), atomLower) {
				scores[cat.ID] += scoreSemanticAtom
			}
		}
	}

	// Find the best candidate
	type scored struct {
		id    string
		score float64
	}
	var ranked []scored
	for id, sc := range scores {
		ranked = append(ranked, scored{id, sc})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	if len(ranked) == 0 || ranked[0].score < 0.30 {
		return nil, nil
	}

	best := ranked[0]
	cat, err := s.LoadNode(ctx, best.id)
	if err != nil {
		return nil, err
	}

	exp, err := ExplainCategory(ctx, s, best.id)
	if err != nil {
		return nil, err
	}

	exp.Score = best.score
	exp.Confidence = scoreToConfidence(best.score)
	exp.Category = *cat

	obs := FailureObservation{
		SessionID:           req.SessionID,
		IncidentID:          req.IncidentID,
		RunID:               req.RunID,
		Source:              "matcher",
		RawError:            req.RawError,
		NormalizedSignature: normalized,
		MatchedCategoryID:   best.id,
		Component:           req.Component,
		ServiceName:         req.ServiceName,
		FilePath:            req.FilePath,
		Symbol:              req.Symbol,
		Confidence:          exp.Confidence,
	}
	saved, err := s.RecordObservation(ctx, obs)
	if err == nil {
		exp.Observation = *saved
	}

	return exp, nil
}

// FindSimilar returns up to req.Limit categories matching the given signals.
func FindSimilar(ctx context.Context, s *Store, req SimilarFailureRequest) ([]FailureExplanation, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	matchReq := MatchErrorRequest{
		RawError:      req.RawError,
		Component:     req.Component,
		SemanticAtoms: req.SemanticAtoms,
		LiveSignals:   req.LiveSignals,
	}
	normalized := NormalizeErrorSignature(req.RawError)
	matchReq.RawError = normalized

	sigs, err := s.AllSignatures(ctx)
	if err != nil {
		return nil, err
	}
	_ = matchReq

	scores := map[string]float64{}
	for _, sig := range sigs {
		if sig.CategoryID == "" {
			continue
		}
		var delta float64
		switch sig.MatcherKind {
		case MatcherKindExact:
			if strings.Contains(normalized, sig.NormalizedSignature) {
				delta = scoreExactSignature
			}
		case MatcherKindRegex:
			re, err := regexp.Compile("(?i)" + sig.MatcherPattern)
			if err == nil && re.MatchString(req.RawError) {
				delta = scoreRegexSignature
			}
		case MatcherKindKeyword:
			keywords := strings.Fields(sig.MatcherPattern)
			if ContainsKeywords(normalized, keywords) {
				delta = scoreRegexSignature
			}
		}
		if delta > 0 {
			scores[sig.CategoryID] += delta
		}
	}

	// Semantic atoms
	cats, _ := s.ListCategories(ctx)
	for _, atom := range req.SemanticAtoms {
		atomLower := strings.ToLower(atom)
		for _, cat := range cats {
			if strings.Contains(strings.ToLower(cat.Name), atomLower) {
				scores[cat.ID] += scoreSemanticAtom
			}
		}
	}

	type sc struct {
		id    string
		score float64
	}
	var ranked []sc
	for id, v := range scores {
		if v >= 0.30 {
			ranked = append(ranked, sc{id, v})
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	var results []FailureExplanation
	for _, r := range ranked {
		exp, err := ExplainCategory(ctx, s, r.id)
		if err != nil {
			continue
		}
		exp.Score = r.score
		exp.Confidence = scoreToConfidence(r.score)
		results = append(results, *exp)
	}
	return results, nil
}

func scoreToConfidence(score float64) string {
	switch {
	case score >= 0.80:
		return ConfidenceHigh
	case score >= 0.55:
		return ConfidenceMedium
	case score >= 0.30:
		return ConfidenceLow
	default:
		return ConfidenceNone
	}
}

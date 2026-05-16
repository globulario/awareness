package failurelearning

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/failuregraph"
)

// ProposeUpdate is the main entry point. It extracts, matches, builds the
// proposal, and saves it. Deduplicates: if a non-rejected proposal already
// exists for the same source, returns the existing one.
func ProposeUpdate(ctx context.Context, req ProposeRequest, s *Store, fg *failuregraph.Store) (*FailureLearningProposal, error) {
	// Dedup: check if a non-rejected proposal already exists for this source.
	existing, err := s.ListBySource(ctx, req.SourceType, req.SourceID)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: dedup check: %w", err)
	}
	for i := range existing {
		if existing[i].Status != StatusRejected {
			return &existing[i], nil
		}
	}

	// Extract knowledge from the request.
	extract := ExtractFromRequest(req)

	// Quality check: must have ≥1 raw error OR ≥1 root cause.
	kind := KindNoReusableKnowledge
	var match *FailureLearningMatch
	if len(extract.RawErrors) == 0 && len(extract.RootCauses) == 0 {
		// Insufficient data — record a no-knowledge proposal.
		match = &FailureLearningMatch{
			ProposalKind: KindNoReusableKnowledge,
			Confidence:   failuregraph.ConfidenceNone,
		}
	} else {
		// Compare against the failure graph.
		m, err := CompareToGraph(ctx, extract, fg)
		if err != nil {
			return nil, fmt.Errorf("failurelearning: compare to graph: %w", err)
		}
		if m != nil {
			match = m
			kind = m.ProposalKind
		}
	}

	if match == nil {
		match = &FailureLearningMatch{
			ProposalKind: KindNoReusableKnowledge,
			Confidence:   failuregraph.ConfidenceNone,
		}
		kind = KindNoReusableKnowledge
	}

	patch := buildPatch(extract, match)
	title := buildTitle(extract, match)
	summary := buildSummary(extract, match)

	p := FailureLearningProposal{
		SourceType:         req.SourceType,
		SourceID:           req.SourceID,
		CreatedBy:          req.CreatedBy,
		ProposalKind:       kind,
		Status:             StatusProposed,
		TargetCategoryID:   match.CategoryID,
		ProposedCategoryID: match.CategoryID,
		Title:              title,
		Summary:            summary,
		Confidence:         match.Confidence,
		Rationale:          fmt.Sprintf("Extracted from %s %s. Match score: %.2f.", req.SourceType, req.SourceID, match.MatchScore),
		Extracted:          extract,
		Patch:              patch,
	}

	return s.SaveProposal(ctx, p)
}

// buildPatch creates a FailureGraphPatch from an extract and its match result.
func buildPatch(extract FailureLearningExtract, match *FailureLearningMatch) FailureGraphPatch {
	var patch FailureGraphPatch

	catID := match.CategoryID
	if catID == "" && match.ProposalKind == KindCreateCategory {
		// Derive a category ID from the first root cause or raw error.
		name := categoryNameFrom(extract)
		catID = "ERRCAT-" + name
	}

	switch match.ProposalKind {
	case KindAddSignature:
		for _, raw := range extract.RawErrors {
			if raw == "" {
				continue
			}
			norm := failuregraph.NormalizeErrorSignature(raw)
			patch.AddSignatures = append(patch.AddSignatures, FailureSignaturePatch{
				Signature:   norm,
				Normalized:  norm,
				CategoryID:  catID,
				Sample:      raw,
				MatcherKind: failuregraph.MatcherKindExact,
			})
		}

	case KindAddWrongFix:
		for i, wf := range extract.WrongFixes {
			nodeID := fmt.Sprintf("WRONG-%s-%d", slugify(wf), i)
			patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
				ID:      nodeID,
				Type:    failuregraph.NodeTypeWrongFix,
				Name:    nodeID,
				Summary: wf,
			})
			if catID != "" {
				patch.AddEdges = append(patch.AddEdges, FailureGraphEdgePatch{
					FromID:     catID,
					ToID:       nodeID,
					EdgeType:   failuregraph.EdgeAvoidFix,
					Confidence: failuregraph.ConfidenceMedium,
					Evidence:   "extracted from incident",
					Source:     "failure_learning",
				})
			}
		}

	case KindAddRegressionTest:
		for i, t := range extract.RegressionTests {
			nodeID := fmt.Sprintf("REGTEST-%s-%d", slugify(t), i)
			patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
				ID:      nodeID,
				Type:    failuregraph.NodeTypeRegressionTest,
				Name:    nodeID,
				Summary: t,
			})
			if catID != "" {
				patch.AddEdges = append(patch.AddEdges, FailureGraphEdgePatch{
					FromID:     catID,
					ToID:       nodeID,
					EdgeType:   failuregraph.EdgeClosureRequires,
					Confidence: failuregraph.ConfidenceMedium,
					Evidence:   "extracted from incident",
					Source:     "failure_learning",
				})
			}
		}

	case KindAddCause:
		for i, c := range extract.RootCauses {
			nodeID := fmt.Sprintf("CAUSE-%s-%d", slugify(c), i)
			patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
				ID:      nodeID,
				Type:    failuregraph.NodeTypeRootCause,
				Name:    nodeID,
				Summary: c,
			})
			if catID != "" {
				patch.AddEdges = append(patch.AddEdges, FailureGraphEdgePatch{
					FromID:     catID,
					ToID:       nodeID,
					EdgeType:   failuregraph.EdgeCommonlyCausedBy,
					Confidence: failuregraph.ConfidenceMedium,
					Evidence:   "extracted from incident",
					Source:     "failure_learning",
				})
			}
		}

	case KindCreateCategory:
		catName := categoryNameFrom(extract)
		patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
			ID:      catID,
			Type:    failuregraph.NodeTypeErrorCategory,
			Name:    catName,
			Summary: summarize(extract),
		})
		// Add all signatures for the new category.
		for _, raw := range extract.RawErrors {
			if raw == "" {
				continue
			}
			norm := failuregraph.NormalizeErrorSignature(raw)
			patch.AddSignatures = append(patch.AddSignatures, FailureSignaturePatch{
				Signature:  norm,
				Normalized: norm,
				CategoryID: catID,
				Sample:     raw,
				MatcherKind: failuregraph.MatcherKindExact,
			})
		}
		// Causes.
		for i, c := range extract.RootCauses {
			nodeID := fmt.Sprintf("CAUSE-%s-%d", slugify(c), i)
			patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
				ID:      nodeID,
				Type:    failuregraph.NodeTypeRootCause,
				Name:    nodeID,
				Summary: c,
			})
			patch.AddEdges = append(patch.AddEdges, FailureGraphEdgePatch{
				FromID:     catID,
				ToID:       nodeID,
				EdgeType:   failuregraph.EdgeCommonlyCausedBy,
				Confidence: failuregraph.ConfidenceLow,
				Evidence:   "newly created category",
				Source:     "failure_learning",
			})
			// Resolutions hang off the first cause.
			if i == 0 {
				for j, r := range extract.Resolutions {
					resID := fmt.Sprintf("RES-%s-%d", slugify(r), j)
					patch.AddNodes = append(patch.AddNodes, FailureGraphNodePatch{
						ID:      resID,
						Type:    failuregraph.NodeTypeResolution,
						Name:    resID,
						Summary: r,
					})
					patch.AddEdges = append(patch.AddEdges, FailureGraphEdgePatch{
						FromID:     nodeID,
						ToID:       resID,
						EdgeType:   failuregraph.EdgeFixedBy,
						Confidence: failuregraph.ConfidenceLow,
						Evidence:   "newly created category",
						Source:     "failure_learning",
					})
				}
			}
		}
	}

	return patch
}

// buildTitle returns a human-readable title for the proposal.
func buildTitle(extract FailureLearningExtract, match *FailureLearningMatch) string {
	switch match.ProposalKind {
	case KindCreateCategory:
		name := categoryNameFrom(extract)
		return fmt.Sprintf("Create new category: %s", name)
	case KindNoReusableKnowledge:
		return "No reusable knowledge extracted"
	default:
		topic := ""
		if len(extract.RawErrors) > 0 {
			norm := failuregraph.NormalizeErrorSignature(extract.RawErrors[0])
			// Trim to 60 chars for readability.
			if len(norm) > 60 {
				norm = norm[:60]
			}
			topic = norm
		} else if len(extract.RootCauses) > 0 {
			topic = extract.RootCauses[0]
		}
		target := match.CategoryName
		if target == "" {
			target = match.CategoryID
		}
		kindLabel := strings.ReplaceAll(match.ProposalKind, "_", " ")
		if topic != "" && target != "" {
			return fmt.Sprintf("%s %q to %s", kindLabel, topic, target)
		}
		return fmt.Sprintf("%s for %s", kindLabel, target)
	}
}

// buildSummary returns a one-sentence summary of what the proposal changes.
func buildSummary(extract FailureLearningExtract, match *FailureLearningMatch) string {
	switch match.ProposalKind {
	case KindNoReusableKnowledge:
		return "Insufficient data to propose a graph update."
	case KindCreateCategory:
		return fmt.Sprintf("New failure category with %d error(s), %d cause(s), %d resolution(s).",
			len(extract.RawErrors), len(extract.RootCauses), len(extract.Resolutions))
	default:
		return fmt.Sprintf("Proposal to %s in category %s (confidence: %s, score: %.2f).",
			strings.ReplaceAll(match.ProposalKind, "_", " "),
			match.CategoryName,
			match.Confidence,
			match.MatchScore,
		)
	}
}

// categoryNameFrom derives a safe category name from extract fields.
func categoryNameFrom(extract FailureLearningExtract) string {
	if len(extract.RootCauses) > 0 {
		return slugify(extract.RootCauses[0])
	}
	if len(extract.RawErrors) > 0 {
		return slugify(failuregraph.NormalizeErrorSignature(extract.RawErrors[0]))
	}
	return "unknown_category"
}

// summarize returns a short summary from extract fields.
func summarize(extract FailureLearningExtract) string {
	if len(extract.RootCauses) > 0 {
		return extract.RootCauses[0]
	}
	if len(extract.Symptoms) > 0 {
		return extract.Symptoms[0]
	}
	if len(extract.RawErrors) > 0 {
		return extract.RawErrors[0]
	}
	return ""
}

// slugify converts a string to a lowercase identifier safe for IDs and file names.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prev := '_'
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prev = r
		} else if prev != '_' {
			b.WriteRune('_')
			prev = '_'
		}
	}
	result := strings.Trim(b.String(), "_")
	if len(result) > 40 {
		result = result[:40]
	}
	if result == "" {
		result = "unknown"
	}
	return result
}

package failurelearning

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/awareness/failuregraph"
)

// ApplyProposal applies an approved proposal to the SQLite failure graph and
// writes/updates the YAML seed file under docsDir (typically <repoRoot>/docs/awareness).
// Returns error if proposal is not approved.
func ApplyProposal(ctx context.Context, proposalID string, s *Store, fg *failuregraph.Store, docsDir string) (*ApplyResult, error) {
	p, err := s.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: apply: %w", err)
	}

	if p.Status != StatusApproved {
		return nil, fmt.Errorf("failurelearning: proposal %s is %s, only approved proposals can be applied", proposalID, p.Status)
	}

	result := &ApplyResult{ProposalID: proposalID}

	// 1. Apply patch nodes.
	for _, n := range p.Patch.AddNodes {
		node := failuregraph.FailureNode{
			ID:       n.ID,
			NodeType: n.Type,
			Name:     n.Name,
			Summary:  n.Summary,
			Severity: n.Severity,
			Status:   failuregraph.StatusActive,
			Metadata: n.Metadata,
		}
		if node.Severity == "" {
			node.Severity = "warning"
		}
		if _, err := fg.RecordFailureNode(ctx, node); err != nil {
			return nil, fmt.Errorf("failurelearning: apply node %s: %w", n.ID, err)
		}
		result.CreatedNodes++
	}

	// 2. Apply patch edges.
	for _, e := range p.Patch.AddEdges {
		edge := failuregraph.FailureEdge{
			FromID:     e.FromID,
			ToID:       e.ToID,
			EdgeType:   e.EdgeType,
			Confidence: e.Confidence,
			Evidence:   e.Evidence,
			Source:     e.Source,
		}
		if _, err := fg.RecordFailureEdge(ctx, edge); err != nil {
			return nil, fmt.Errorf("failurelearning: apply edge %s→%s: %w", e.FromID, e.ToID, err)
		}
		result.CreatedEdges++
	}

	// 3. Apply patch signatures.
	for _, sig := range p.Patch.AddSignatures {
		errSig := failuregraph.ErrorSignature{
			Signature:           sig.Signature,
			NormalizedSignature: sig.Normalized,
			CategoryID:          sig.CategoryID,
			Severity:            sig.Severity,
			Sample:              sig.Sample,
			MatcherKind:         sig.MatcherKind,
			MatcherPattern:      sig.MatcherPattern,
		}
		if errSig.Severity == "" {
			errSig.Severity = "warning"
		}
		if _, err := fg.RecordErrorSignature(ctx, errSig); err != nil {
			return nil, fmt.Errorf("failurelearning: apply signature: %w", err)
		}
	}

	// 4. Write/update YAML seed file (best-effort — don't fail the apply for seed errors).
	categoryID := p.TargetCategoryID
	if categoryID == "" {
		categoryID = p.ProposedCategoryID
	}
	// For create_category proposals, the category comes from the patch.
	if categoryID == "" && len(p.Patch.AddNodes) > 0 {
		for _, n := range p.Patch.AddNodes {
			if n.Type == failuregraph.NodeTypeErrorCategory {
				categoryID = n.ID
				break
			}
		}
	}

	var seedPath, contentHash string
	if categoryID != "" && docsDir != "" {
		seedPath, contentHash, err = WriteOrUpdateSeedYAML(docsDir, categoryID, p.Patch, fg)
		if err != nil {
			// Record failure but don't block apply.
			_ = s.SaveSeedSync(ctx, SeedSyncStatus{
				ProposalID: proposalID,
				SeedPath:   seedPath,
				Status:     "failed",
				Message:    err.Error(),
			})
		} else {
			_ = s.SaveSeedSync(ctx, SeedSyncStatus{
				ProposalID:  proposalID,
				SeedPath:    seedPath,
				Status:      "synced",
				ContentHash: contentHash,
			})
		}
	}

	result.SeedPath = seedPath
	result.ContentHash = contentHash

	// 5. Mark proposal as applied.
	if err := s.MarkApplied(ctx, proposalID, time.Now().UnixMilli()); err != nil {
		return nil, fmt.Errorf("failurelearning: mark applied: %w", err)
	}

	return result, nil
}

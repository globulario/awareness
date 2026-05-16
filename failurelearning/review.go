package failurelearning

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ReviewProposal handles approve/approve_with_edits/reject/defer decisions.
// State machine: proposed → approved/rejected/deferred.
func ReviewProposal(ctx context.Context, proposalID, reviewer, decision, notes string, editedPatch *FailureGraphPatch, s *Store) (*FailureLearningProposal, error) {
	p, err := s.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: review: %w", err)
	}

	// Only proposed proposals can be reviewed in v1.
	if p.Status != StatusProposed {
		return nil, fmt.Errorf("failurelearning: proposal %s is %s, only proposed proposals can be reviewed", proposalID, p.Status)
	}

	// Map decision to new status.
	newStatus, err := decisionToStatus(decision)
	if err != nil {
		return nil, err
	}

	// If the reviewer supplied an edited patch, update it on the proposal.
	editedPatchJSON := ""
	if editedPatch != nil {
		b, jerr := json.Marshal(editedPatch)
		if jerr != nil {
			return nil, fmt.Errorf("failurelearning: marshal edited patch: %w", jerr)
		}
		editedPatchJSON = string(b)
		// Replace the patch on the proposal.
		p.Patch = *editedPatch
	}

	now := time.Now().UnixMilli()

	// Update status.
	if err := s.UpdateProposalStatus(ctx, proposalID, newStatus, reviewer, now); err != nil {
		return nil, err
	}

	// Save audit record.
	rev := FailureLearningReview{
		ProposalID:      proposalID,
		Reviewer:        reviewer,
		Decision:        decision,
		Notes:           notes,
		EditedPatchJSON: editedPatchJSON,
	}
	if _, err := s.SaveReview(ctx, rev); err != nil {
		return nil, fmt.Errorf("failurelearning: save review record: %w", err)
	}

	// If the patch was edited, re-save the proposal with the updated patch.
	if editedPatch != nil {
		p.Status = newStatus
		p.ReviewedBy = reviewer
		p.ReviewedAt = now
		if _, err := s.SaveProposal(ctx, *p); err != nil {
			return nil, fmt.Errorf("failurelearning: save edited proposal: %w", err)
		}
	}

	p.Status = newStatus
	p.ReviewedBy = reviewer
	p.ReviewedAt = now
	return p, nil
}

// RejectProposal is a shorthand for ReviewProposal with DecisionReject.
func RejectProposal(ctx context.Context, proposalID, reviewer, reason string, s *Store) error {
	_, err := ReviewProposal(ctx, proposalID, reviewer, DecisionReject, reason, nil, s)
	return err
}

// DeferProposal marks a proposal as deferred.
func DeferProposal(ctx context.Context, proposalID, reviewer, reason string, s *Store) error {
	_, err := ReviewProposal(ctx, proposalID, reviewer, DecisionDefer, reason, nil, s)
	return err
}

// decisionToStatus maps a review decision string to the target proposal status.
func decisionToStatus(decision string) (string, error) {
	switch decision {
	case DecisionApprove, DecisionApproveWithEdits, DecisionMerge:
		return StatusApproved, nil
	case DecisionReject:
		return StatusRejected, nil
	case DecisionDefer:
		return StatusDeferred, nil
	default:
		return "", fmt.Errorf("failurelearning: unknown review decision %q", decision)
	}
}

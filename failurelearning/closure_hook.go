package failurelearning

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/failuregraph"
)

// CheckClosure examines closure info and determines if a failure learning proposal
// is required. Returns "clean" if existing proposals cover it or data is insufficient;
// returns "closed_with_learning_pending" if a proposal should be created.
func CheckClosure(ctx context.Context, info ClosureInfo, s *Store, fg *failuregraph.Store) (*ClosureWithLearning, error) {
	// Check if proposals already exist for this closure source.
	existing, err := s.ListBySource(ctx, info.SourceType, info.ClosureID)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: check closure: %w", err)
	}

	for i := range existing {
		p := &existing[i]
		switch p.Status {
		case StatusApproved, StatusApplied, StatusDeferred:
			// A durable proposal already covers this closure — clean.
			return &ClosureWithLearning{
				Status:             "clean",
				ExistingProposalID: p.ID,
				RequiresLearning:   false,
				Reason:             fmt.Sprintf("existing %s proposal %s already covers this closure", p.Status, p.ID),
			}, nil
		}
	}

	// Determine if the closure has sufficient data to learn from.
	if !info.HasRootCause || !info.HasResolution {
		return &ClosureWithLearning{
			Status:           "clean",
			RequiresLearning: false,
			Reason:           "closure lacks root cause or resolution — insufficient data for learning",
		}, nil
	}

	// Closure has root cause + resolution but no durable proposal — learning required.
	return &ClosureWithLearning{
		Status:           "closed_with_learning_pending",
		RequiresLearning: true,
		Reason:           "closure has root cause and resolution but no approved/applied/deferred proposal",
	}, nil
}

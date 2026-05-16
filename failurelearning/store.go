package failurelearning

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
)

// Store wraps *sql.DB for the three failure_learning_* tables.
type Store struct {
	db *sql.DB
}

// New creates a Store from a *graph.Graph.
func New(g *graph.Graph) *Store {
	return &Store{db: g.DB()}
}

// randomHex returns n random hex bytes as a lowercase string.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newProposalID returns a unique proposal ID.
func newProposalID() string {
	return fmt.Sprintf("FLP-%d-%s", time.Now().UnixMilli(), randomHex(4))
}

// newReviewID returns a unique review ID.
func newReviewID() string {
	return fmt.Sprintf("FLREV-%d-%s", time.Now().UnixMilli(), randomHex(4))
}

// newSeedSyncID returns a unique seed sync ID.
func newSeedSyncID() string {
	return fmt.Sprintf("FLSEED-%d-%s", time.Now().UnixMilli(), randomHex(4))
}

// marshalExtract serialises a FailureLearningExtract to JSON.
func marshalExtract(e FailureLearningExtract) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// marshalPatch serialises a FailureGraphPatch to JSON.
func marshalPatch(p FailureGraphPatch) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// scanProposal scans one row from failure_learning_proposals.
func scanProposal(row interface {
	Scan(...any) error
}) (*FailureLearningProposal, error) {
	var p FailureLearningProposal
	var extractJSON, patchJSON string
	var targetCat, proposedCat, rationale, reviewedBy sql.NullString
	var reviewedAt, appliedAt sql.NullInt64

	err := row.Scan(
		&p.ID,
		&p.SourceType,
		&p.SourceID,
		&p.ProposalKind,
		&p.Status,
		&targetCat,
		&proposedCat,
		&p.Title,
		&p.Summary,
		&p.Confidence,
		&rationale,
		&extractJSON,
		&patchJSON,
		&p.CreatedBy,
		&reviewedBy,
		&p.CreatedAt,
		&reviewedAt,
		&appliedAt,
	)
	if err != nil {
		return nil, err
	}

	p.TargetCategoryID = targetCat.String
	p.ProposedCategoryID = proposedCat.String
	p.Rationale = rationale.String
	p.ReviewedBy = reviewedBy.String
	if reviewedAt.Valid {
		p.ReviewedAt = reviewedAt.Int64
	}
	if appliedAt.Valid {
		p.AppliedAt = appliedAt.Int64
	}

	if err := json.Unmarshal([]byte(extractJSON), &p.Extracted); err != nil {
		// tolerate corrupt JSON — return empty extract
		p.Extracted = FailureLearningExtract{}
	}
	if err := json.Unmarshal([]byte(patchJSON), &p.Patch); err != nil {
		p.Patch = FailureGraphPatch{}
	}
	return &p, nil
}

const proposalSelectCols = `id, source_type, source_id, proposal_kind, status,
    target_category_id, proposed_category_id, title, summary, confidence,
    rationale, extracted_json, patch_json, created_by, reviewed_by,
    created_at, reviewed_at, applied_at`

// SaveProposal upserts a FailureLearningProposal. Assigns a new ID if empty.
func (s *Store) SaveProposal(ctx context.Context, p FailureLearningProposal) (*FailureLearningProposal, error) {
	if p.ID == "" {
		p.ID = newProposalID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().UnixMilli()
	}

	extractJSON, err := marshalExtract(p.Extracted)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: marshal extract: %w", err)
	}
	patchJSON, err := marshalPatch(p.Patch)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: marshal patch: %w", err)
	}

	const q = `INSERT OR REPLACE INTO failure_learning_proposals
        (id, source_type, source_id, proposal_kind, status,
         target_category_id, proposed_category_id, title, summary, confidence,
         rationale, extracted_json, patch_json, created_by, reviewed_by,
         created_at, reviewed_at, applied_at)
        VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?)`

	_, err = s.db.ExecContext(ctx, q,
		p.ID, p.SourceType, p.SourceID, p.ProposalKind, p.Status,
		nullString(p.TargetCategoryID), nullString(p.ProposedCategoryID),
		p.Title, p.Summary, p.Confidence,
		nullString(p.Rationale), extractJSON, patchJSON,
		p.CreatedBy, nullString(p.ReviewedBy),
		p.CreatedAt, nullInt64(p.ReviewedAt), nullInt64(p.AppliedAt),
	)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: save proposal: %w", err)
	}
	return &p, nil
}

// GetProposal retrieves a proposal by ID.
func (s *Store) GetProposal(ctx context.Context, id string) (*FailureLearningProposal, error) {
	q := "SELECT " + proposalSelectCols + " FROM failure_learning_proposals WHERE id = ?"
	row := s.db.QueryRowContext(ctx, q, id)
	p, err := scanProposal(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("failurelearning: proposal %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failurelearning: get proposal %s: %w", id, err)
	}
	return p, nil
}

// UpdateProposalStatus updates the status, reviewedBy, and reviewedAt fields.
func (s *Store) UpdateProposalStatus(ctx context.Context, id, status, reviewedBy string, reviewedAt int64) error {
	const q = `UPDATE failure_learning_proposals
        SET status = ?, reviewed_by = ?, reviewed_at = ?
        WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, status, reviewedBy, reviewedAt, id)
	if err != nil {
		return fmt.Errorf("failurelearning: update status %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("failurelearning: proposal %s not found", id)
	}
	return nil
}

// MarkApplied stamps applied_at on the proposal and sets status=applied.
func (s *Store) MarkApplied(ctx context.Context, id string, appliedAt int64) error {
	const q = `UPDATE failure_learning_proposals SET status = 'applied', applied_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, q, appliedAt, id)
	if err != nil {
		return fmt.Errorf("failurelearning: mark applied %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("failurelearning: proposal %s not found", id)
	}
	return nil
}

// ListPending returns all proposals with status=proposed.
func (s *Store) ListPending(ctx context.Context) ([]FailureLearningProposal, error) {
	q := "SELECT " + proposalSelectCols + " FROM failure_learning_proposals WHERE status = 'proposed' ORDER BY created_at DESC"
	return s.queryProposals(ctx, q)
}

// ListBySource returns all proposals for the given source_type and source_id.
func (s *Store) ListBySource(ctx context.Context, sourceType, sourceID string) ([]FailureLearningProposal, error) {
	q := "SELECT " + proposalSelectCols + " FROM failure_learning_proposals WHERE source_type = ? AND source_id = ? ORDER BY created_at DESC"
	return s.queryProposals(ctx, q, sourceType, sourceID)
}

func (s *Store) queryProposals(ctx context.Context, q string, args ...any) ([]FailureLearningProposal, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: query proposals: %w", err)
	}
	defer rows.Close()

	var out []FailureLearningProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("failurelearning: scan proposal: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SaveReview upserts a FailureLearningReview, assigning an ID if empty.
func (s *Store) SaveReview(ctx context.Context, r FailureLearningReview) (*FailureLearningReview, error) {
	if r.ID == "" {
		r.ID = newReviewID()
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = time.Now().UnixMilli()
	}

	const q = `INSERT OR REPLACE INTO failure_learning_reviews
        (id, proposal_id, reviewer, decision, notes, edited_patch_json, created_at)
        VALUES (?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		r.ID, r.ProposalID, r.Reviewer, r.Decision,
		nullString(r.Notes), nullString(r.EditedPatchJSON), r.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failurelearning: save review: %w", err)
	}
	return &r, nil
}

// SaveSeedSync upserts a SeedSyncStatus record, assigning an ID if empty.
func (s *Store) SaveSeedSync(ctx context.Context, ss SeedSyncStatus) error {
	if ss.ID == "" {
		ss.ID = newSeedSyncID()
	}
	now := time.Now().UnixMilli()
	if ss.CreatedAt == 0 {
		ss.CreatedAt = now
	}
	ss.UpdatedAt = now

	const q = `INSERT OR REPLACE INTO failure_seed_sync
        (id, proposal_id, seed_path, status, content_hash, message, created_at, updated_at)
        VALUES (?,?,?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		ss.ID, ss.ProposalID, ss.SeedPath, ss.Status,
		nullString(ss.ContentHash), nullString(ss.Message), ss.CreatedAt, ss.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failurelearning: save seed sync: %w", err)
	}
	return nil
}

// --- SQL helpers ---

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: v != 0}
}

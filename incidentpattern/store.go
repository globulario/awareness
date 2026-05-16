package incidentpattern

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// Store provides persistence for incident patterns backed by the awareness graph DB.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by the given awareness graph.
func NewStore(g *graph.Graph) *Store {
	return &Store{db: g.DB()}
}

// RecordPattern inserts or replaces a pattern and all its relations.
// Returns the pattern with its assigned ID.
func (s *Store) RecordPattern(ctx context.Context, p IncidentPattern) (IncidentPattern, error) {
	if p.ID == "" {
		p.ID = "PAT-" + uuid.New().String()[:8]
	}
	now := time.Now().Unix()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = "active"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incident_patterns
		  (id, incident_id, title, summary, severity, status, failure_mode, root_cause, lesson, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  title=excluded.title, summary=excluded.summary, severity=excluded.severity,
		  status=excluded.status, failure_mode=excluded.failure_mode,
		  root_cause=excluded.root_cause, lesson=excluded.lesson, updated_at=excluded.updated_at`,
		p.ID, p.IncidentID, p.Title, p.Summary, p.Severity, p.Status,
		p.FailureMode, p.RootCause, p.Lesson, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return p, fmt.Errorf("incidentpattern: insert pattern: %w", err)
	}

	if err := s.replaceRelations(ctx, p); err != nil {
		return p, err
	}
	return p, nil
}

// replaceRelations deletes and re-inserts all relations for a pattern.
func (s *Store) replaceRelations(ctx context.Context, p IncidentPattern) error {
	tables := []string{
		"incident_pattern_files", "incident_pattern_symbols",
		"incident_pattern_invariants", "incident_pattern_failed_fixes",
		"incident_pattern_edit_shapes", "incident_pattern_proposals",
	}
	for _, t := range tables {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM "+t+" WHERE pattern_id=?", p.ID); err != nil {
			return fmt.Errorf("incidentpattern: delete %s: %w", t, err)
		}
	}

	for _, f := range p.Files {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO incident_pattern_files (pattern_id,path,role) VALUES (?,?,?)`,
			p.ID, f.Path, f.Role); err != nil {
			return fmt.Errorf("incidentpattern: insert file link: %w", err)
		}
	}
	for _, sym := range p.Symbols {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO incident_pattern_symbols (pattern_id,symbol,role) VALUES (?,?,?)`,
			p.ID, sym.Symbol, sym.Role); err != nil {
			return fmt.Errorf("incidentpattern: insert symbol link: %w", err)
		}
	}
	for _, inv := range p.Invariants {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO incident_pattern_invariants (pattern_id,invariant_id,relationship) VALUES (?,?,?)`,
			p.ID, inv.InvariantID, inv.Relationship); err != nil {
			return fmt.Errorf("incidentpattern: insert invariant link: %w", err)
		}
	}
	for _, ff := range p.FailedFixes {
		if ff.ID == "" {
			ff.ID = uuid.New().String()
		}
		reverted := 0
		if ff.Reverted {
			reverted = 1
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO incident_pattern_failed_fixes
			  (id,pattern_id,proposal_id,commit_hash,description,reverted,revert_reason,created_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			ff.ID, p.ID, ff.ProposalID, ff.CommitHash, ff.Description,
			reverted, ff.RevertReason, time.Now().Unix()); err != nil {
			return fmt.Errorf("incidentpattern: insert failed fix: %w", err)
		}
	}
	for _, es := range p.EditShapes {
		if es.ID == "" {
			es.ID = uuid.New().String()
		}
		dangerous := 1
		if !es.Dangerous {
			dangerous = 0
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO incident_pattern_edit_shapes
			  (id,pattern_id,shape_kind,description,dangerous)
			VALUES (?,?,?,?,?)`,
			es.ID, p.ID, es.ShapeKind, es.Description, dangerous); err != nil {
			return fmt.Errorf("incidentpattern: insert edit shape: %w", err)
		}
	}
	for _, pp := range p.Proposals {
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO incident_pattern_proposals
			  (pattern_id,proposal_id,relationship,reason)
			VALUES (?,?,?,?)`,
			p.ID, pp.ProposalID, pp.Relationship, pp.Reason); err != nil {
			return fmt.Errorf("incidentpattern: insert proposal link: %w", err)
		}
	}
	return nil
}

// LoadPattern loads a pattern by ID with all relations.
func (s *Store) LoadPattern(ctx context.Context, id string) (*IncidentPattern, error) {
	var p IncidentPattern
	err := s.db.QueryRowContext(ctx, `
		SELECT id,incident_id,title,summary,severity,status,failure_mode,root_cause,lesson,created_at,updated_at
		FROM incident_patterns WHERE id=?`, id).Scan(
		&p.ID, &p.IncidentID, &p.Title, &p.Summary, &p.Severity, &p.Status,
		&p.FailureMode, &p.RootCause, &p.Lesson, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("incidentpattern: load %s: %w", id, err)
	}
	if err := s.loadRelations(ctx, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// LoadPatternByIncident loads a pattern by incident ID.
func (s *Store) LoadPatternByIncident(ctx context.Context, incidentID string) (*IncidentPattern, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM incident_patterns WHERE incident_id=? ORDER BY created_at DESC LIMIT 1`,
		incidentID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("incidentpattern: no pattern for incident %s: %w", incidentID, err)
	}
	return s.LoadPattern(ctx, id)
}

// ListPatterns returns all active patterns with their relations.
func (s *Store) ListPatterns(ctx context.Context) ([]IncidentPattern, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,incident_id,title,summary,severity,status,failure_mode,root_cause,lesson,created_at,updated_at
		FROM incident_patterns WHERE status='active' ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("incidentpattern: list: %w", err)
	}

	// Drain rows before calling loadRelations — loadRelations opens its own cursors
	// and SQLite allows only one open connection (MaxOpenConns=1).
	var patterns []IncidentPattern
	for rows.Next() {
		var p IncidentPattern
		if err := rows.Scan(&p.ID, &p.IncidentID, &p.Title, &p.Summary, &p.Severity,
			&p.Status, &p.FailureMode, &p.RootCause, &p.Lesson, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		patterns = append(patterns, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for i := range patterns {
		if err := s.loadRelations(ctx, &patterns[i]); err != nil {
			return nil, err
		}
	}
	return patterns, nil
}

func (s *Store) loadRelations(ctx context.Context, p *IncidentPattern) error {
	// Files
	rows, err := s.db.QueryContext(ctx,
		`SELECT path,role FROM incident_pattern_files WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var f PatternFile
		f.PatternID = p.ID
		if err := rows.Scan(&f.Path, &f.Role); err != nil {
			return err
		}
		p.Files = append(p.Files, f)
	}
	rows.Close()

	// Symbols
	rows, err = s.db.QueryContext(ctx,
		`SELECT symbol,role FROM incident_pattern_symbols WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sym PatternSymbol
		sym.PatternID = p.ID
		if err := rows.Scan(&sym.Symbol, &sym.Role); err != nil {
			return err
		}
		p.Symbols = append(p.Symbols, sym)
	}
	rows.Close()

	// Invariants
	rows, err = s.db.QueryContext(ctx,
		`SELECT invariant_id,relationship FROM incident_pattern_invariants WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var inv PatternInvariant
		inv.PatternID = p.ID
		if err := rows.Scan(&inv.InvariantID, &inv.Relationship); err != nil {
			return err
		}
		p.Invariants = append(p.Invariants, inv)
	}
	rows.Close()

	// Failed fixes
	rows, err = s.db.QueryContext(ctx, `
		SELECT id,proposal_id,commit_hash,description,reverted,revert_reason,created_at
		FROM incident_pattern_failed_fixes WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ff FailedFix
		ff.PatternID = p.ID
		var reverted int
		if err := rows.Scan(&ff.ID, &ff.ProposalID, &ff.CommitHash, &ff.Description,
			&reverted, &ff.RevertReason, &ff.CreatedAt); err != nil {
			return err
		}
		ff.Reverted = reverted != 0
		p.FailedFixes = append(p.FailedFixes, ff)
	}
	rows.Close()

	// Edit shapes
	rows, err = s.db.QueryContext(ctx,
		`SELECT id,shape_kind,description,dangerous FROM incident_pattern_edit_shapes WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var es EditShape
		es.PatternID = p.ID
		var dangerous int
		if err := rows.Scan(&es.ID, &es.ShapeKind, &es.Description, &dangerous); err != nil {
			return err
		}
		es.Dangerous = dangerous != 0
		p.EditShapes = append(p.EditShapes, es)
	}
	rows.Close()

	// Proposals
	rows, err = s.db.QueryContext(ctx,
		`SELECT proposal_id,relationship,reason FROM incident_pattern_proposals WHERE pattern_id=?`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pp PatternProposal
		pp.PatternID = p.ID
		if err := rows.Scan(&pp.ProposalID, &pp.Relationship, &pp.Reason); err != nil {
			return err
		}
		p.Proposals = append(p.Proposals, pp)
	}
	return rows.Err()
}

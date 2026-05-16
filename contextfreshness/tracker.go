// Package contextfreshness tracks which files an agent session has read and
// detects when those files change, preventing the agent from acting on stale context.
//
// The freshness ledger answers: "Is the version of this file that the agent
// remembers still the current version?" — a question the awareness graph
// alone cannot answer.
package contextfreshness

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// Tracker records context reads and detects staleness.
// It is backed by the same SQLite database as the awareness graph.
type Tracker struct {
	db *sql.DB
}

// New returns a Tracker backed by the given awareness graph database.
func New(g *graph.Graph) *Tracker {
	return &Tracker{db: g.DB()}
}

// RecordContextRead records that the agent consumed path at its current fingerprint
// and updates the file_snapshots table so the current state is known.
func (t *Tracker) RecordContextRead(ctx context.Context, sessionID, path, readReason, readTool string, turnIndex int) (*ContextRead, error) {
	snap, err := Fingerprint(path)
	if err != nil {
		return nil, fmt.Errorf("contextfreshness: fingerprint %s: %w", path, err)
	}
	now := time.Now().Unix()
	cr := &ContextRead{
		ID:          uuid.New().String(),
		SessionID:   sessionID,
		Path:        path,
		Fingerprint: snap.Fingerprint,
		SizeBytes:   snap.SizeBytes,
		ModTimeUnix: snap.ModTimeUnix,
		GitCommit:   snap.GitCommit,
		ReadReason:  readReason,
		ReadTool:    readTool,
		TurnIndex:   turnIndex,
		CreatedAt:   now,
	}

	_, err = t.db.ExecContext(ctx, `
		INSERT INTO context_reads
		  (id, session_id, path, fingerprint, size_bytes, mod_time_unix,
		   git_commit, read_reason, read_tool, turn_index, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		cr.ID, cr.SessionID, cr.Path, cr.Fingerprint,
		cr.SizeBytes, cr.ModTimeUnix, cr.GitCommit,
		cr.ReadReason, cr.ReadTool, cr.TurnIndex, cr.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("contextfreshness: insert context_read: %w", err)
	}

	// Keep file_snapshots current so agent-context cache invalidation can use it.
	_, err = t.db.ExecContext(ctx, `
		INSERT INTO file_snapshots
		  (path, fingerprint, size_bytes, mod_time_unix, git_commit, updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET
		  fingerprint    = excluded.fingerprint,
		  size_bytes     = excluded.size_bytes,
		  mod_time_unix  = excluded.mod_time_unix,
		  git_commit     = excluded.git_commit,
		  updated_at     = excluded.updated_at`,
		path, snap.Fingerprint, snap.SizeBytes, snap.ModTimeUnix, snap.GitCommit, now)
	if err != nil {
		return nil, fmt.Errorf("contextfreshness: upsert file_snapshot: %w", err)
	}

	return cr, nil
}

// CheckStaleContext checks the given paths for staleness relative to what the
// session read earlier. Use SeverityCritical for pre-edit guards and
// SeverityWarning for background freshness scans.
func (t *Tracker) CheckStaleContext(ctx context.Context, sessionID string, paths []string, currentTurnIndex int, severity string) ([]StaleContextWarning, error) {
	if severity == "" {
		severity = SeverityCritical
	}
	var warnings []StaleContextWarning
	for _, path := range paths {
		w, err := t.checkPath(ctx, sessionID, path, currentTurnIndex, severity)
		if err != nil {
			return nil, err
		}
		if w != nil {
			warnings = append(warnings, *w)
		}
	}
	return warnings, nil
}

// checkPath checks a single path. Returns nil when the file is fresh or untracked.
func (t *Tracker) checkPath(ctx context.Context, sessionID, path string, currentTurnIndex int, severity string) (*StaleContextWarning, error) {
	var cr ContextRead
	err := t.db.QueryRowContext(ctx, `
		SELECT id, session_id, path, fingerprint, size_bytes, mod_time_unix,
		       git_commit, read_reason, read_tool, turn_index, created_at
		FROM context_reads
		WHERE session_id=? AND path=?
		ORDER BY created_at DESC LIMIT 1`,
		sessionID, path).Scan(
		&cr.ID, &cr.SessionID, &cr.Path, &cr.Fingerprint,
		&cr.SizeBytes, &cr.ModTimeUnix, &cr.GitCommit,
		&cr.ReadReason, &cr.ReadTool, &cr.TurnIndex, &cr.CreatedAt)
	if err != nil {
		// No read recorded for this path in this session → not stale, just untracked.
		return nil, nil
	}

	currentFP, deleted := currentFingerprintOrDeleted(path)
	if !deleted && currentFP == cr.Fingerprint {
		return nil, nil // still fresh
	}

	w := buildWarning(sessionID, path, &cr, currentFP, currentTurnIndex, severity)
	if err := t.persistWarning(ctx, w); err != nil {
		return nil, err
	}
	return &w, nil
}

// AcknowledgeWarning marks a stale_context_warning as acknowledged so the agent
// can signal it has re-read the file and is proceeding with fresh context.
func (t *Tracker) AcknowledgeWarning(ctx context.Context, warningID string) error {
	_, err := t.db.ExecContext(ctx,
		`UPDATE stale_context_warnings SET acknowledged_at=? WHERE id=?`,
		time.Now().Unix(), warningID)
	return err
}

func (t *Tracker) persistWarning(ctx context.Context, w StaleContextWarning) error {
	_, err := t.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stale_context_warnings
		  (id, session_id, path, read_fingerprint, current_fingerprint,
		   read_turn_index, current_turn_index, severity, message,
		   created_at, acknowledged_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.SessionID, w.Path, w.ReadFingerprint, w.CurrentFingerprint,
		w.ReadTurnIndex, w.CurrentTurnIndex, w.Severity, w.Message,
		w.CreatedAt, w.AcknowledgedAt)
	return err
}

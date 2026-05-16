package contextfreshness

import (
	"context"
	"fmt"
)

// CheckAllSessionReads checks every file this session has read for staleness.
// Uses SeverityWarning — this is a background scan, not a pre-edit guard.
// For pre-edit guards use CheckStaleContext with SeverityCritical.
func (t *Tracker) CheckAllSessionReads(ctx context.Context, sessionID string, currentTurnIndex int) ([]StaleContextWarning, error) {
	rows, err := t.db.QueryContext(ctx,
		`SELECT DISTINCT path FROM context_reads WHERE session_id=? ORDER BY path`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("contextfreshness: list session paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("contextfreshness: scan path: %w", err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contextfreshness: iterate paths: %w", err)
	}

	return t.CheckStaleContext(ctx, sessionID, paths, currentTurnIndex, SeverityWarning)
}

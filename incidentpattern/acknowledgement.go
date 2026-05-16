package incidentpattern

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// AcknowledgementStore persists per-session incident acknowledgements.
type AcknowledgementStore struct {
	db *sql.DB
}

// NewAcknowledgementStore returns an AcknowledgementStore backed by the awareness graph.
func NewAcknowledgementStore(g *graph.Graph) *AcknowledgementStore {
	return &AcknowledgementStore{db: g.DB()}
}

// AcknowledgeIncident records that the agent has read the incident and is proceeding
// with an adjusted plan. After acknowledgement, shouldBlock returns false for this
// session + incident pair (unless signals change).
func (a *AcknowledgementStore) AcknowledgeIncident(ctx context.Context, sessionID, incidentID, reason string) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO incident_pattern_acknowledgements
		  (id, session_id, incident_id, acknowledged_reason, created_at)
		VALUES (?,?,?,?,?)`,
		uuid.New().String(), sessionID, incidentID, reason, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("incidentpattern: acknowledge %s: %w", incidentID, err)
	}
	return nil
}

// IsAcknowledgedInSession returns true when the agent has already acknowledged
// this incident in the current session.
func (a *AcknowledgementStore) IsAcknowledgedInSession(ctx context.Context, sessionID, incidentID string) bool {
	var count int
	err := a.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM incident_pattern_acknowledgements
		WHERE session_id=? AND incident_id=?`, sessionID, incidentID).Scan(&count)
	return err == nil && count > 0
}

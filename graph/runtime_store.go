package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UpsertRuntimeSnapshot stores a serialized snapshot in the runtime_snapshots table.
func (g *Graph) UpsertRuntimeSnapshot(ctx context.Context, id string, capturedAt int64, nodeID, clusterID string, snapshotJSON []byte) error {
	now := time.Now().Unix()
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO runtime_snapshots (id, captured_at, node_id, cluster_id, snapshot_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			captured_at   = excluded.captured_at,
			node_id       = excluded.node_id,
			cluster_id    = excluded.cluster_id,
			snapshot_json = excluded.snapshot_json
	`, id, capturedAt, nodeID, clusterID, string(snapshotJSON), now)
	if err != nil {
		return fmt.Errorf("UpsertRuntimeSnapshot %s: %w", id, err)
	}
	return nil
}

// LatestRuntimeSnapshot returns the most-recent snapshot JSON, or nil if none.
func (g *Graph) LatestRuntimeSnapshot(ctx context.Context) ([]byte, error) {
	var jsonStr string
	err := g.db.QueryRowContext(ctx, `
		SELECT snapshot_json FROM runtime_snapshots ORDER BY captured_at DESC LIMIT 1
	`).Scan(&jsonStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LatestRuntimeSnapshot: %w", err)
	}
	return []byte(jsonStr), nil
}

// GetRuntimeSnapshotByID returns the stored snapshot JSON for the given snapshot ID.
// Returns (nil, nil) if no snapshot with that ID exists.
func (g *Graph) GetRuntimeSnapshotByID(ctx context.Context, id string) ([]byte, error) {
	var jsonStr string
	err := g.db.QueryRowContext(ctx,
		`SELECT snapshot_json FROM runtime_snapshots WHERE id = ?`, id).Scan(&jsonStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetRuntimeSnapshotByID: %w", err)
	}
	return []byte(jsonStr), nil
}

package livecluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// Store persists and retrieves live cluster signal data.
type Store struct {
	db *sql.DB
}

// NewStore returns a Store backed by the awareness graph.
func NewStore(g *graph.Graph) *Store {
	return &Store{db: g.DB()}
}

// StoreClusterSignalSnapshot persists a full snapshot and all its sub-records.
func (s *Store) StoreClusterSignalSnapshot(ctx context.Context, snap *ClusterSignalSnapshot) error {
	payload, _ := json.Marshal(snap.Sources)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO cluster_signal_snapshots
		  (id,cluster_id,node_id,collected_at,collector_version,status,summary,payload_json)
		VALUES (?,?,?,?,?,?,?,?)`,
		snap.ID, snap.ClusterID, snap.NodeID, snap.CollectedAt, snap.CollectorVersion,
		snap.Status, snap.Summary, string(payload))
	if err != nil {
		return fmt.Errorf("livecluster: store snapshot: %w", err)
	}

	for _, svc := range snap.Services {
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO service_live_states
			  (id,snapshot_id,service_name,component,node_id,status,health,
			   heartbeat_age_seconds,readiness,dependency_state,last_error,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), snap.ID, svc.ServiceName, svc.Component, svc.NodeID,
			svc.Status, svc.Health, svc.HeartbeatAgeSeconds, svc.Readiness,
			svc.DependencyState, svc.LastError, time.Now().Unix()); err != nil {
			return fmt.Errorf("livecluster: store service state: %w", err)
		}
	}

	for _, e := range snap.Errors {
		rf := strings.Join(e.RelatedFiles, "|")
		ri := strings.Join(e.RelatedInvariants, "|")
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO recent_error_signatures
			  (id,snapshot_id,service_name,component,node_id,signature,severity,
			   count,first_seen,last_seen,sample,related_files,related_invariants)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), snap.ID, e.ServiceName, e.Component, e.NodeID,
			e.Signature, e.Severity, e.Count, e.FirstSeen, e.LastSeen,
			e.Sample, rf, ri); err != nil {
			return fmt.Errorf("livecluster: store error sig: %w", err)
		}
	}

	for _, c := range snap.Convergence {
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO runtime_convergence_states
			  (id,snapshot_id,component,desired_state,installed_state,runtime_state,
			   convergence_status,blocked_reason,retry_count,age_seconds,related_key,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), snap.ID, c.Component, c.DesiredState,
			c.InstalledState, c.RuntimeState, c.ConvergenceStatus, c.BlockedReason,
			c.RetryCount, c.AgeSeconds, c.RelatedKey, time.Now().Unix()); err != nil {
			return fmt.Errorf("livecluster: store convergence: %w", err)
		}
	}

	for _, inc := range snap.Incidents {
		if _, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO active_cluster_incidents
			  (id,snapshot_id,incident_id,source,title,severity,status,component,
			   service_name,node_id,summary,started_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), snap.ID, inc.IncidentID, inc.Source, inc.Title,
			inc.Severity, inc.Status, inc.Component, inc.ServiceName, inc.NodeID,
			inc.Summary, inc.StartedAt, inc.UpdatedAt); err != nil {
			return fmt.Errorf("livecluster: store incident: %w", err)
		}
	}
	return nil
}

// GetLatestClusterSignalSnapshot loads the most recent snapshot for a cluster.
func (s *Store) GetLatestClusterSignalSnapshot(ctx context.Context, clusterID string) (*ClusterSignalSnapshot, error) {
	var snap ClusterSignalSnapshot
	var payloadJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT id,cluster_id,node_id,collected_at,collector_version,status,summary,payload_json
		FROM cluster_signal_snapshots
		WHERE cluster_id=? ORDER BY collected_at DESC LIMIT 1`, clusterID).Scan(
		&snap.ID, &snap.ClusterID, &snap.NodeID, &snap.CollectedAt,
		&snap.CollectorVersion, &snap.Status, &snap.Summary, &payloadJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no snapshot for cluster %s", clusterID)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(payloadJSON), &snap.Sources)
	snap.Services, _ = s.loadServices(ctx, snap.ID)
	snap.Errors, _ = s.loadErrors(ctx, snap.ID)
	snap.Convergence, _ = s.loadConvergence(ctx, snap.ID)
	snap.Incidents, _ = s.loadIncidents(ctx, snap.ID)
	return &snap, nil
}

func (s *Store) loadServices(ctx context.Context, snapID string) ([]ServiceLiveState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service_name,component,node_id,status,health,heartbeat_age_seconds,
		       readiness,dependency_state,last_error
		FROM service_live_states WHERE snapshot_id=?`, snapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceLiveState
	for rows.Next() {
		var svc ServiceLiveState
		var hbAge sql.NullInt64
		if err := rows.Scan(&svc.ServiceName, &svc.Component, &svc.NodeID, &svc.Status,
			&svc.Health, &hbAge, &svc.Readiness, &svc.DependencyState, &svc.LastError); err != nil {
			return nil, err
		}
		if hbAge.Valid {
			svc.HeartbeatAgeSeconds = hbAge.Int64
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *Store) loadErrors(ctx context.Context, snapID string) ([]RecentErrorSignature, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT service_name,component,node_id,signature,severity,count,first_seen,last_seen,sample,related_files
		FROM recent_error_signatures WHERE snapshot_id=?`, snapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentErrorSignature
	for rows.Next() {
		var e RecentErrorSignature
		var rf string
		var firstSeen, lastSeen sql.NullInt64
		if err := rows.Scan(&e.ServiceName, &e.Component, &e.NodeID, &e.Signature,
			&e.Severity, &e.Count, &firstSeen, &lastSeen, &e.Sample, &rf); err != nil {
			return nil, err
		}
		if firstSeen.Valid {
			e.FirstSeen = firstSeen.Int64
		}
		if lastSeen.Valid {
			e.LastSeen = lastSeen.Int64
		}
		if rf != "" {
			e.RelatedFiles = strings.Split(rf, "|")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) loadConvergence(ctx context.Context, snapID string) ([]RuntimeConvergenceState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT component,desired_state,installed_state,runtime_state,convergence_status,
		       blocked_reason,retry_count,age_seconds,related_key
		FROM runtime_convergence_states WHERE snapshot_id=?`, snapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuntimeConvergenceState
	for rows.Next() {
		var c RuntimeConvergenceState
		if err := rows.Scan(&c.Component, &c.DesiredState, &c.InstalledState, &c.RuntimeState,
			&c.ConvergenceStatus, &c.BlockedReason, &c.RetryCount, &c.AgeSeconds, &c.RelatedKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) loadIncidents(ctx context.Context, snapID string) ([]ActiveClusterIncident, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT incident_id,source,title,severity,status,component,service_name,node_id,summary,started_at,updated_at
		FROM active_cluster_incidents WHERE snapshot_id=?`, snapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActiveClusterIncident
	for rows.Next() {
		var inc ActiveClusterIncident
		var startedAt, updatedAt sql.NullInt64
		if err := rows.Scan(&inc.IncidentID, &inc.Source, &inc.Title, &inc.Severity,
			&inc.Status, &inc.Component, &inc.ServiceName, &inc.NodeID,
			&inc.Summary, &startedAt, &updatedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			inc.StartedAt = startedAt.Int64
		}
		if updatedAt.Valid {
			inc.UpdatedAt = updatedAt.Int64
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// StoreLivePreflightResult persists a live preflight verdict.
func (s *Store) StoreLivePreflightResult(ctx context.Context, r *LivePreflightResult) error {
	filesJSON, _ := json.Marshal(r.Files)
	compJSON, _ := json.Marshal(r.Components)
	blockJSON, _ := json.Marshal(r.Blockers)
	warnJSON, _ := json.Marshal(r.Warnings)
	confJSON, _ := json.Marshal(r.Confirmations)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_preflight_results
		  (id,session_id,task,files_json,components_json,static_result_id,signal_snapshot_id,
		   verdict,severity,summary,blockers_json,warnings_json,confirmations_json,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.SessionID, r.Task, string(filesJSON), string(compJSON),
		r.StaticResultID, r.SignalSnapshotID, r.Verdict, r.Severity, r.Summary,
		string(blockJSON), string(warnJSON), string(confJSON), time.Now().Unix())
	return err
}

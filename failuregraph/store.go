package failuregraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// Store provides persistence for the failure knowledge graph backed by the awareness graph DB.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by the given awareness graph.
func New(g *graph.Graph) *Store {
	return &Store{db: g.DB()}
}

// RecordFailureNode inserts or updates a failure node.
func (s *Store) RecordFailureNode(ctx context.Context, n FailureNode) (*FailureNode, error) {
	if n.ID == "" {
		n.ID = nodePrefix(n.NodeType) + uuid.New().String()[:8]
	}
	now := time.Now().Unix()
	if n.CreatedAt == 0 {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	if n.Status == "" {
		n.Status = StatusActive
	}
	meta := "{}"
	if n.Metadata != nil {
		b, _ := json.Marshal(n.Metadata)
		meta = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failure_nodes (id,node_type,name,summary,severity,status,metadata_json,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name=excluded.name, summary=excluded.summary, severity=excluded.severity,
		  status=excluded.status, metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		n.ID, n.NodeType, n.Name, n.Summary, n.Severity, n.Status, meta, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert node %s: %w", n.ID, err)
	}
	return &n, nil
}

// RecordFailureEdge inserts a directed typed edge between two nodes.
func (s *Store) RecordFailureEdge(ctx context.Context, e FailureEdge) (*FailureEdge, error) {
	if e.ID == "" {
		e.ID = "FEDGE-" + uuid.New().String()[:8]
	}
	e.CreatedAt = time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO failure_edges (id,from_id,to_id,edge_type,confidence,evidence,source,created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		e.ID, e.FromID, e.ToID, e.EdgeType, e.Confidence, e.Evidence, e.Source, e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert edge %s->%s (%s): %w", e.FromID, e.ToID, e.EdgeType, err)
	}
	return &e, nil
}

// RecordErrorSignature inserts or updates a normalized error signature.
func (s *Store) RecordErrorSignature(ctx context.Context, sig ErrorSignature) (*ErrorSignature, error) {
	if sig.ID == "" {
		sig.ID = "ERRSIG-" + uuid.New().String()[:8]
	}
	now := time.Now().Unix()
	if sig.CreatedAt == 0 {
		sig.CreatedAt = now
	}
	sig.UpdatedAt = now
	if sig.MatcherKind == "" {
		sig.MatcherKind = MatcherKindExact
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failure_error_signatures
		  (id,signature,normalized_signature,category_id,severity,sample,matcher_kind,matcher_pattern,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  normalized_signature=excluded.normalized_signature, category_id=excluded.category_id,
		  severity=excluded.severity, sample=excluded.sample, matcher_kind=excluded.matcher_kind,
		  matcher_pattern=excluded.matcher_pattern, updated_at=excluded.updated_at`,
		sig.ID, sig.Signature, sig.NormalizedSignature, sig.CategoryID,
		sig.Severity, sig.Sample, sig.MatcherKind, sig.MatcherPattern,
		sig.CreatedAt, sig.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert signature %s: %w", sig.ID, err)
	}
	return &sig, nil
}

// RecordObservation inserts a failure observation record.
func (s *Store) RecordObservation(ctx context.Context, obs FailureObservation) (*FailureObservation, error) {
	if obs.ID == "" {
		obs.ID = "FOBS-" + uuid.New().String()[:8]
	}
	obs.CreatedAt = time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failure_observations
		  (id,session_id,incident_id,run_id,source,raw_error,normalized_signature,
		   matched_signature_id,matched_category_id,component,service_name,file_path,symbol,confidence,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		obs.ID, obs.SessionID, obs.IncidentID, obs.RunID, obs.Source, obs.RawError,
		obs.NormalizedSignature, obs.MatchedSignatureID, obs.MatchedCategoryID,
		obs.Component, obs.ServiceName, obs.FilePath, obs.Symbol, obs.Confidence, obs.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert observation: %w", err)
	}
	return &obs, nil
}

// RecordResolutionRecipe inserts or updates a resolution recipe.
func (s *Store) RecordResolutionRecipe(ctx context.Context, r ResolutionRecipe) (*ResolutionRecipe, error) {
	if r.ID == "" {
		r.ID = "RECIPE-" + uuid.New().String()[:8]
	}
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	stepsJSON, _ := json.Marshal(r.Steps)
	forbiddenJSON, _ := json.Marshal(r.ForbiddenSteps)
	verifJSON, _ := json.Marshal(r.Verification)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO failure_resolution_recipes
		  (id,resolution_id,title,steps_json,forbidden_steps_json,verification_json,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  title=excluded.title, steps_json=excluded.steps_json,
		  forbidden_steps_json=excluded.forbidden_steps_json, verification_json=excluded.verification_json,
		  updated_at=excluded.updated_at`,
		r.ID, r.ResolutionID, r.Title, string(stepsJSON), string(forbiddenJSON), string(verifJSON),
		r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert recipe %s: %w", r.ID, err)
	}
	return &r, nil
}

// RecordWorkflowFailureMode inserts or updates a workflow failure mode.
func (s *Store) RecordWorkflowFailureMode(ctx context.Context, m WorkflowFailureMode) (*WorkflowFailureMode, error) {
	if m.ID == "" {
		m.ID = "WFMODE-" + uuid.New().String()[:8]
	}
	now := time.Now().Unix()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	meta := "{}"
	if m.Metadata != nil {
		b, _ := json.Marshal(m.Metadata)
		meta = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_failure_modes
		  (id,name,summary,workflow_stage,failure_phase,retry_semantics,closure_rule,metadata_json,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  summary=excluded.summary, workflow_stage=excluded.workflow_stage,
		  failure_phase=excluded.failure_phase, retry_semantics=excluded.retry_semantics,
		  closure_rule=excluded.closure_rule, metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		m.ID, m.Name, m.Summary, m.WorkflowStage, m.FailurePhase,
		m.RetrySemantics, m.ClosureRule, meta, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: insert workflow mode %s: %w", m.ID, err)
	}
	return &m, nil
}

// LoadNode loads a failure node by ID.
func (s *Store) LoadNode(ctx context.Context, id string) (*FailureNode, error) {
	var n FailureNode
	var metaJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,node_type,name,summary,severity,status,metadata_json,created_at,updated_at
		 FROM failure_nodes WHERE id=?`, id).
		Scan(&n.ID, &n.NodeType, &n.Name, &n.Summary, &n.Severity, &n.Status,
			&metaJSON, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: load node %s: %w", id, err)
	}
	_ = json.Unmarshal([]byte(metaJSON), &n.Metadata)
	return &n, nil
}

// ListCategories returns all active ErrorCategory nodes.
func (s *Store) ListCategories(ctx context.Context) ([]FailureNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,node_type,name,summary,severity,status,metadata_json,created_at,updated_at
		FROM failure_nodes WHERE node_type=? AND status=? ORDER BY name`,
		NodeTypeErrorCategory, StatusActive)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: list categories: %w", err)
	}
	return scanNodes(rows)
}

// NodesReachable returns all nodes reachable from fromID via the given edge type.
func (s *Store) NodesReachable(ctx context.Context, fromID, edgeType string) ([]FailureNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id,n.node_type,n.name,n.summary,n.severity,n.status,n.metadata_json,n.created_at,n.updated_at
		FROM failure_nodes n
		JOIN failure_edges e ON e.to_id=n.id
		WHERE e.from_id=? AND e.edge_type=? AND n.status=?
		ORDER BY n.name`, fromID, edgeType, StatusActive)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: reachable %s-[%s]->?: %w", fromID, edgeType, err)
	}
	return scanNodes(rows)
}

// LoadWorkflowModes returns all WorkflowFailureMode rows linked to a category.
func (s *Store) LoadWorkflowModes(ctx context.Context, categoryID string) ([]WorkflowFailureMode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id,w.name,w.summary,w.workflow_stage,w.failure_phase,w.retry_semantics,w.closure_rule,w.metadata_json,w.created_at,w.updated_at
		FROM workflow_failure_modes w
		JOIN failure_edges e ON e.to_id=w.id
		WHERE e.from_id=? AND e.edge_type=?`, categoryID, EdgeCommonlyCausedBy)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: workflow modes for %s: %w", categoryID, err)
	}
	defer rows.Close()
	var modes []WorkflowFailureMode
	for rows.Next() {
		var m WorkflowFailureMode
		var metaJSON string
		if err := rows.Scan(&m.ID, &m.Name, &m.Summary, &m.WorkflowStage, &m.FailurePhase,
			&m.RetrySemantics, &m.ClosureRule, &metaJSON, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metaJSON), &m.Metadata)
		modes = append(modes, m)
	}
	return modes, rows.Err()
}

// AllSignatures returns all active error signatures.
func (s *Store) AllSignatures(ctx context.Context) ([]ErrorSignature, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,signature,normalized_signature,category_id,severity,sample,matcher_kind,matcher_pattern,created_at,updated_at
		FROM failure_error_signatures ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("failuregraph: all signatures: %w", err)
	}
	defer rows.Close()
	var sigs []ErrorSignature
	for rows.Next() {
		var sig ErrorSignature
		if err := rows.Scan(&sig.ID, &sig.Signature, &sig.NormalizedSignature, &sig.CategoryID,
			&sig.Severity, &sig.Sample, &sig.MatcherKind, &sig.MatcherPattern,
			&sig.CreatedAt, &sig.UpdatedAt); err != nil {
			return nil, err
		}
		sigs = append(sigs, sig)
	}
	return sigs, rows.Err()
}

// scanNodes drains a *sql.Rows into []FailureNode, closing the cursor.
func scanNodes(rows *sql.Rows) ([]FailureNode, error) {
	defer rows.Close()
	var nodes []FailureNode
	for rows.Next() {
		var n FailureNode
		var metaJSON string
		if err := rows.Scan(&n.ID, &n.NodeType, &n.Name, &n.Summary, &n.Severity,
			&n.Status, &metaJSON, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metaJSON), &n.Metadata)
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// nodePrefix returns the ID prefix for a node type.
func nodePrefix(nodeType string) string {
	switch nodeType {
	case NodeTypeErrorCategory:
		return "ERRCAT-"
	case NodeTypeSymptom:
		return "SYM-"
	case NodeTypeRootCause:
		return "CAUSE-"
	case NodeTypeResolution:
		return "RES-"
	case NodeTypeWrongFix:
		return "WRONG-"
	case NodeTypeRegressionTest:
		return "REGTEST-"
	case NodeTypeInvariant:
		return "INV-"
	case NodeTypeWorkflowMode:
		return "WFMODE-"
	case NodeTypeIncidentExample:
		return "INCEX-"
	case NodeTypeSemanticAtom:
		return "SATOM-"
	case NodeTypeRuntimeSignal:
		return "RTSIG-"
	default:
		return "FN-"
	}
}

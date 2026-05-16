package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Invariant is the specialized invariant record stored in the invariants table.
type Invariant struct {
	ID       string
	Title    string
	Summary  string
	Severity string
	Status   string
}

// FailureMode is the specialized failure mode record stored in the failure_modes table.
type FailureMode struct {
	ID              string
	Title           string
	Summary         string
	Symptoms        []string
	RootCause       string
	ArchitectureFix string
}

// BuildStats holds graph build statistics.
type BuildStats struct {
	Nodes                int   `json:"nodes"`
	Edges                int   `json:"edges"`
	Invariants           int   `json:"invariants"`
	FailureModes         int   `json:"failure_modes"`
	FilesScanned         int   `json:"files_scanned,omitempty"`
	KnowledgeFilesScanned int  `json:"knowledge_files_scanned,omitempty"`
	DurationMs           int64 `json:"duration_ms,omitempty"`
}

// CollectorHealthItem records the outcome of a single collector pass.
// Stored as JSON array in graph_builds.collector_health_json.
type CollectorHealthItem struct {
	CollectorID  string `json:"collector_id"`
	SourceTier   string `json:"source_tier,omitempty"`
	Status       string `json:"status"`       // "ok" | "skipped" | "error"
	NodesEmitted int    `json:"nodes_emitted"`
	Error        string `json:"error,omitempty"`
	Priority     string `json:"priority,omitempty"` // "P0" | "P1"
}

// BuildRecord is a single row from the graph_builds table.
type BuildRecord struct {
	ID               string
	RepoRoot         string
	GitCommit        string
	ReleaseID        string
	CreatedAt        int64
	Stats            BuildStats
	CollectorHealth  []CollectorHealthItem
}

// LatestBuildRecord returns the most recent static graph build row (excludes live snapshots), or (nil, nil) if none.
func (g *Graph) LatestBuildRecord(ctx context.Context) (*BuildRecord, error) {
	row := g.db.QueryRowContext(ctx, `
		SELECT id, repo_root, git_commit, release_id, created_at, stats_json,
		       COALESCE(collector_health_json, '[]')
		FROM graph_builds WHERE id != 'live-snapshot' ORDER BY created_at DESC LIMIT 1
	`)
	return scanBuildRecord(row)
}

// LiveSnapshotBuildID is the fixed build ID used for live overlay refresh records.
// It is always overwritten (upserted) on each live-snapshot run.
const LiveSnapshotBuildID = "live-snapshot"

// LatestLiveSnapshotRecord returns the most recent live mirror refresh record,
// or (nil, nil) if no live-snapshot has been run.
func (g *Graph) LatestLiveSnapshotRecord(ctx context.Context) (*BuildRecord, error) {
	row := g.db.QueryRowContext(ctx, `
		SELECT id, repo_root, git_commit, release_id, created_at, stats_json,
		       COALESCE(collector_health_json, '[]')
		FROM graph_builds WHERE id = 'live-snapshot'
	`)
	return scanBuildRecord(row)
}

func scanBuildRecord(row *sql.Row) (*BuildRecord, error) {
	var r BuildRecord
	var statsJSON, healthJSON string
	err := row.Scan(&r.ID, &r.RepoRoot, &r.GitCommit, &r.ReleaseID, &r.CreatedAt, &statsJSON, &healthJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanBuildRecord: %w", err)
	}
	_ = json.Unmarshal([]byte(statsJSON), &r.Stats)
	_ = json.Unmarshal([]byte(healthJSON), &r.CollectorHealth)
	return &r, nil
}

// SetBuildCollectorHealth stores the collector health array for a build record.
func (g *Graph) SetBuildCollectorHealth(ctx context.Context, buildID string, items []CollectorHealthItem) error {
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("SetBuildCollectorHealth: encode: %w", err)
	}
	_, err = g.db.ExecContext(ctx,
		`UPDATE graph_builds SET collector_health_json = ? WHERE id = ?`,
		string(data), buildID)
	if err != nil {
		return fmt.Errorf("SetBuildCollectorHealth %s: %w", buildID, err)
	}
	return nil
}

// FindNode returns a node by ID, or (nil, nil) if not found.
func (g *Graph) FindNode(ctx context.Context, id string) (*Node, error) {
	row := g.db.QueryRowContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE id = ?
	`, id)
	return scanNode(row)
}

// FindNodesByType returns all nodes of the given type ordered by name.
func (g *Graph) FindNodesByType(ctx context.Context, nodeType string) ([]*Node, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE type = ? ORDER BY name
	`, nodeType)
	if err != nil {
		return nil, fmt.Errorf("FindNodesByType %s: %w", nodeType, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// FindNodesByPath returns nodes whose path exactly matches the given value.
func (g *Graph) FindNodesByPath(ctx context.Context, path string) ([]*Node, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE path = ? ORDER BY name
	`, path)
	if err != nil {
		return nil, fmt.Errorf("FindNodesByPath %q: %w", path, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// FindNodeByTypeAndName returns the first node matching type + exact name.
func (g *Graph) FindNodeByTypeAndName(ctx context.Context, nodeType, name string) (*Node, error) {
	row := g.db.QueryRowContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE type = ? AND name = ? LIMIT 1
	`, nodeType, name)
	return scanNode(row)
}

// FindNodesByNameLike returns nodes whose name contains the query string.
func (g *Graph) FindNodesByNameLike(ctx context.Context, query string) ([]*Node, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, type, name, path, summary, metadata_json, created_at, updated_at
		FROM nodes WHERE name LIKE ? ORDER BY name
	`, "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("FindNodesByNameLike %q: %w", query, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// Neighbors returns edges connected to id.
// direction is "out" (outgoing), "in" (incoming), or "both".
func (g *Graph) Neighbors(ctx context.Context, id, direction string) ([]Edge, error) {
	var (
		rows *sql.Rows
		err  error
	)
	switch direction {
	case "in":
		rows, err = g.db.QueryContext(ctx, `
			SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
			FROM edges WHERE dst = ?
		`, id)
	case "out":
		rows, err = g.db.QueryContext(ctx, `
			SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
			FROM edges WHERE src = ?
		`, id)
	default:
		rows, err = g.db.QueryContext(ctx, `
			SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
			FROM edges WHERE src = ? OR dst = ?
		`, id, id)
	}
	if err != nil {
		return nil, fmt.Errorf("Neighbors %s: %w", id, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// NeighborsByClass returns outgoing edges from id filtered by edge_class.
// Use EdgeClassDecision, EdgeClassStructural, or EdgeClassInformation.
// Falls back to all neighbors if edge_class column is missing (pre-migration DBs).
func (g *Graph) NeighborsByClass(ctx context.Context, id, class string) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
		FROM edges WHERE src = ? AND edge_class = ?
	`, id, class)
	if err != nil {
		// Fallback: column may not exist yet on pre-migration databases.
		return g.Neighbors(ctx, id, "out")
	}
	defer rows.Close()
	return scanEdges(rows)
}

// EdgesByClass returns all edges in the graph with the given edge_class.
func (g *Graph) EdgesByClass(ctx context.Context, class string) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
		FROM edges WHERE edge_class = ?
	`, class)
	if err != nil {
		return nil, fmt.Errorf("EdgesByClass %s: %w", class, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// TraverseDecision performs BFS from startID following only decision-class edges.
// This gives a clean causal path without information/context noise.
func (g *Graph) TraverseDecision(ctx context.Context, startID string, maxDepth int) (*TraversalResult, error) {
	visited := make(map[string]bool)
	result := &TraversalResult{}

	type item struct {
		id    string
		depth int
	}
	queue := []item{{startID, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur.id] {
			continue
		}
		visited[cur.id] = true

		node, err := g.FindNode(ctx, cur.id)
		if err != nil {
			return nil, err
		}
		if node != nil {
			result.Nodes = append(result.Nodes, node)
		}

		if cur.depth >= maxDepth {
			continue
		}

		edges, err := g.NeighborsByClass(ctx, cur.id, EdgeClassDecision)
		if err != nil {
			return nil, fmt.Errorf("TraverseDecision neighbors %s: %w", cur.id, err)
		}

		for _, e := range edges {
			result.Edges = append(result.Edges, e)
			if !visited[e.Dst] {
				queue = append(queue, item{e.Dst, cur.depth + 1})
			}
		}
	}

	return result, nil
}

// AllEdges returns every edge in the graph (used by cycle detection).
func (g *Graph) AllEdges(ctx context.Context) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json FROM edges
	`)
	if err != nil {
		return nil, fmt.Errorf("AllEdges: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// OutgoingEdges returns all edges where src == nodeID.
func (g *Graph) OutgoingEdges(ctx context.Context, nodeID string) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
		FROM edges WHERE src = ?
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("OutgoingEdges %s: %w", nodeID, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// EdgesByKind returns all edges of the given kind.
func (g *Graph) EdgesByKind(ctx context.Context, kind string) ([]Edge, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT src, kind, dst, phase, required, confidence, metadata_json, provenance_json
		FROM edges WHERE kind = ?
	`, kind)
	if err != nil {
		return nil, fmt.Errorf("EdgesByKind %s: %w", kind, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// AllInvariants returns all invariant records ordered by ID.
func (g *Graph) AllInvariants(ctx context.Context) ([]*Invariant, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, title, summary, severity, status, metadata_json
		FROM invariants ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("AllInvariants: %w", err)
	}
	defer rows.Close()
	var out []*Invariant
	for rows.Next() {
		var inv Invariant
		var meta string
		if err := rows.Scan(&inv.ID, &inv.Title, &inv.Summary, &inv.Severity, &inv.Status, &meta); err != nil {
			return nil, err
		}
		out = append(out, &inv)
	}
	return out, rows.Err()
}

// AllFailureModes returns all failure mode records ordered by ID.
func (g *Graph) AllFailureModes(ctx context.Context) ([]*FailureMode, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, title, summary, symptoms_json, root_cause, architecture_fix, metadata_json
		FROM failure_modes ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("AllFailureModes: %w", err)
	}
	defer rows.Close()
	var out []*FailureMode
	for rows.Next() {
		var fm FailureMode
		var sympJSON, metaJSON string
		if err := rows.Scan(&fm.ID, &fm.Title, &fm.Summary, &sympJSON, &fm.RootCause, &fm.ArchitectureFix, &metaJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(sympJSON), &fm.Symptoms)
		out = append(out, &fm)
	}
	return out, rows.Err()
}

// UpsertInvariant upserts an invariant record.
func (g *Graph) UpsertInvariant(ctx context.Context, inv Invariant) error {
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO invariants (id, title, summary, severity, status, metadata_json)
		VALUES (?, ?, ?, ?, ?, '{}')
		ON CONFLICT(id) DO UPDATE SET
			title    = excluded.title,
			summary  = excluded.summary,
			severity = excluded.severity,
			status   = excluded.status
	`, inv.ID, inv.Title, inv.Summary, inv.Severity, inv.Status)
	if err != nil {
		return fmt.Errorf("UpsertInvariant %s: %w", inv.ID, err)
	}
	return nil
}

// UpsertFailureMode upserts a failure mode record.
func (g *Graph) UpsertFailureMode(ctx context.Context, fm FailureMode) error {
	sympJSON, err := json.Marshal(fm.Symptoms)
	if err != nil {
		return err
	}
	_, err = g.db.ExecContext(ctx, `
		INSERT INTO failure_modes (id, title, summary, symptoms_json, root_cause, architecture_fix, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, '{}')
		ON CONFLICT(id) DO UPDATE SET
			title            = excluded.title,
			summary          = excluded.summary,
			symptoms_json    = excluded.symptoms_json,
			root_cause       = excluded.root_cause,
			architecture_fix = excluded.architecture_fix
	`, fm.ID, fm.Title, fm.Summary, string(sympJSON), fm.RootCause, fm.ArchitectureFix)
	if err != nil {
		return fmt.Errorf("UpsertFailureMode %s: %w", fm.ID, err)
	}
	return nil
}

// Stats returns current node/edge/invariant/failure-mode counts.
func (g *Graph) Stats(ctx context.Context) (BuildStats, error) {
	var s BuildStats
	for _, row := range []struct {
		dest *int
		q    string
	}{
		{&s.Nodes, `SELECT COUNT(*) FROM nodes`},
		{&s.Edges, `SELECT COUNT(*) FROM edges`},
		{&s.Invariants, `SELECT COUNT(*) FROM invariants`},
		{&s.FailureModes, `SELECT COUNT(*) FROM failure_modes`},
	} {
		if err := g.db.QueryRowContext(ctx, row.q).Scan(row.dest); err != nil {
			return s, err
		}
	}
	return s, nil
}

// UpsertBuildRecord records a completed graph build with its stats.
func (g *Graph) UpsertBuildRecord(ctx context.Context, id, repoRoot, gitCommit, releaseID string, stats BuildStats) error {
	statsJSON, _ := json.Marshal(stats)
	now := time.Now().Unix()
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO graph_builds (id, repo_root, git_commit, release_id, created_at, stats_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET stats_json = excluded.stats_json, created_at = excluded.created_at
	`, id, repoRoot, gitCommit, releaseID, now, string(statsJSON))
	if err != nil {
		return fmt.Errorf("UpsertBuildRecord: %w", err)
	}
	return nil
}

// ---- incident records ----

// IncidentRecord maps to the incidents table.
type IncidentRecord struct {
	ID           string
	Title        string
	Severity     string
	Status       string
	StartedAt    int64
	EndedAt      int64
	Summary      string
	EvidenceJSON string
	CreatedAt    int64
	UpdatedAt    int64
}

// ProposalStatus values for awareness_proposals.
const (
	ProposalStatusDraft      = "DRAFT"
	ProposalStatusValidated  = "VALIDATED"
	ProposalStatusNeedsReview = "NEEDS_REVIEW"
	ProposalStatusApproved   = "APPROVED"
	ProposalStatusRejected   = "REJECTED"
	ProposalStatusPromoted   = "PROMOTED"
	ProposalStatusSuperseded = "SUPERSEDED"
)

// ProposalRecord maps to the awareness_proposals table.
type ProposalRecord struct {
	ID             string
	IncidentID     string
	Status         string
	ProposalYAML   string
	ValidationJSON string
	CreatedBy      string
	CreatedAt      int64
	PromotedAt     int64
}

// ContextAliasRecord maps to the context_aliases table.
type ContextAliasRecord struct {
	ID         string
	TargetID   string
	Alias      string
	Confidence float64
	Source     string
	CreatedAt  int64
}

// UpsertIncident inserts or updates an incident record.
func (g *Graph) UpsertIncident(ctx context.Context, inc IncidentRecord) error {
	now := time.Now().Unix()
	if inc.CreatedAt == 0 {
		inc.CreatedAt = now
	}
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO incidents (id, title, severity, status, started_at, ended_at, summary, evidence_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title         = excluded.title,
			severity      = excluded.severity,
			status        = excluded.status,
			started_at    = excluded.started_at,
			ended_at      = excluded.ended_at,
			summary       = excluded.summary,
			evidence_json = excluded.evidence_json,
			updated_at    = excluded.updated_at
	`, inc.ID, inc.Title, inc.Severity, inc.Status, inc.StartedAt, inc.EndedAt,
		inc.Summary, inc.EvidenceJSON, inc.CreatedAt, now)
	if err != nil {
		return fmt.Errorf("UpsertIncident %s: %w", inc.ID, err)
	}
	return nil
}

// FindIncident returns an incident by ID, or (nil, nil) if not found.
func (g *Graph) FindIncident(ctx context.Context, id string) (*IncidentRecord, error) {
	var inc IncidentRecord
	err := g.db.QueryRowContext(ctx, `
		SELECT id, title, severity, status, started_at, ended_at, summary, evidence_json, created_at, updated_at
		FROM incidents WHERE id = ?
	`, id).Scan(&inc.ID, &inc.Title, &inc.Severity, &inc.Status, &inc.StartedAt,
		&inc.EndedAt, &inc.Summary, &inc.EvidenceJSON, &inc.CreatedAt, &inc.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindIncident %s: %w", id, err)
	}
	return &inc, nil
}

// AllIncidents returns all incident records ordered by created_at descending.
func (g *Graph) AllIncidents(ctx context.Context) ([]*IncidentRecord, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, title, severity, status, started_at, ended_at, summary, evidence_json, created_at, updated_at
		FROM incidents ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("AllIncidents: %w", err)
	}
	defer rows.Close()
	var out []*IncidentRecord
	for rows.Next() {
		var inc IncidentRecord
		if err := rows.Scan(&inc.ID, &inc.Title, &inc.Severity, &inc.Status, &inc.StartedAt,
			&inc.EndedAt, &inc.Summary, &inc.EvidenceJSON, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &inc)
	}
	return out, rows.Err()
}

// UpsertProposal inserts or updates a proposal record.
func (g *Graph) UpsertProposal(ctx context.Context, p ProposalRecord) error {
	now := time.Now().Unix()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO awareness_proposals (id, incident_id, status, proposal_yaml, validation_json, created_by, created_at, promoted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			incident_id     = excluded.incident_id,
			status          = excluded.status,
			proposal_yaml   = excluded.proposal_yaml,
			validation_json = excluded.validation_json,
			created_by      = excluded.created_by,
			promoted_at     = excluded.promoted_at
	`, p.ID, p.IncidentID, p.Status, p.ProposalYAML, p.ValidationJSON, p.CreatedBy, p.CreatedAt, p.PromotedAt)
	if err != nil {
		return fmt.Errorf("UpsertProposal %s: %w", p.ID, err)
	}
	return nil
}

// FindProposal returns a proposal by ID, or (nil, nil) if not found.
func (g *Graph) FindProposal(ctx context.Context, id string) (*ProposalRecord, error) {
	var p ProposalRecord
	err := g.db.QueryRowContext(ctx, `
		SELECT id, incident_id, status, proposal_yaml, validation_json, created_by, created_at, promoted_at
		FROM awareness_proposals WHERE id = ?
	`, id).Scan(&p.ID, &p.IncidentID, &p.Status, &p.ProposalYAML, &p.ValidationJSON,
		&p.CreatedBy, &p.CreatedAt, &p.PromotedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindProposal %s: %w", id, err)
	}
	return &p, nil
}

// AllProposals returns all proposal records ordered by created_at descending.
func (g *Graph) AllProposals(ctx context.Context) ([]*ProposalRecord, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, incident_id, status, proposal_yaml, validation_json, created_by, created_at, promoted_at
		FROM awareness_proposals ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("AllProposals: %w", err)
	}
	defer rows.Close()
	var out []*ProposalRecord
	for rows.Next() {
		var p ProposalRecord
		if err := rows.Scan(&p.ID, &p.IncidentID, &p.Status, &p.ProposalYAML,
			&p.ValidationJSON, &p.CreatedBy, &p.CreatedAt, &p.PromotedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// UpdateProposalStatus sets the status (and promoted_at if PROMOTED) of a proposal.
func (g *Graph) UpdateProposalStatus(ctx context.Context, id, status string) error {
	promotedAt := int64(0)
	if status == ProposalStatusPromoted {
		promotedAt = time.Now().Unix()
	}
	_, err := g.db.ExecContext(ctx, `
		UPDATE awareness_proposals SET status = ?, promoted_at = ? WHERE id = ?
	`, status, promotedAt, id)
	if err != nil {
		return fmt.Errorf("UpdateProposalStatus %s: %w", id, err)
	}
	return nil
}

// UpsertContextAlias inserts or replaces a context alias entry.
func (g *Graph) UpsertContextAlias(ctx context.Context, a ContextAliasRecord) error {
	now := time.Now().Unix()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	conf := a.Confidence
	if conf == 0 {
		conf = 1.0
	}
	_, err := g.db.ExecContext(ctx, `
		INSERT INTO context_aliases (id, target_id, alias, confidence, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			target_id  = excluded.target_id,
			alias      = excluded.alias,
			confidence = excluded.confidence,
			source     = excluded.source
	`, a.ID, a.TargetID, a.Alias, conf, a.Source, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("UpsertContextAlias %s: %w", a.ID, err)
	}
	return nil
}

// AllContextAliases returns all context alias records.
func (g *Graph) AllContextAliases(ctx context.Context) ([]*ContextAliasRecord, error) {
	rows, err := g.db.QueryContext(ctx, `
		SELECT id, target_id, alias, confidence, source, created_at
		FROM context_aliases ORDER BY target_id, alias
	`)
	if err != nil {
		return nil, fmt.Errorf("AllContextAliases: %w", err)
	}
	defer rows.Close()
	var out []*ContextAliasRecord
	for rows.Next() {
		var a ContextAliasRecord
		if err := rows.Scan(&a.ID, &a.TargetID, &a.Alias, &a.Confidence, &a.Source, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// FindInvariant returns an invariant record by ID, or (nil, nil) if not found.
func (g *Graph) FindInvariant(ctx context.Context, id string) (*Invariant, error) {
	var inv Invariant
	var meta string
	err := g.db.QueryRowContext(ctx, `
		SELECT id, title, summary, severity, status, metadata_json
		FROM invariants WHERE id = ?
	`, id).Scan(&inv.ID, &inv.Title, &inv.Summary, &inv.Severity, &inv.Status, &meta)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindInvariant %s: %w", id, err)
	}
	return &inv, nil
}

// ---- internal scan helpers ----

func scanNode(row *sql.Row) (*Node, error) {
	var n Node
	var meta string
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.Path, &n.Summary, &meta, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.Metadata = unmarshalMeta(meta)
	return &n, nil
}

func scanNodes(rows *sql.Rows) ([]*Node, error) {
	var out []*Node
	for rows.Next() {
		var n Node
		var meta string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.Path, &n.Summary, &meta, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Metadata = unmarshalMeta(meta)
		out = append(out, &n)
	}
	return out, rows.Err()
}

func scanEdges(rows *sql.Rows) ([]Edge, error) {
	var out []Edge
	for rows.Next() {
		var e Edge
		var req int
		var meta, prov string
		if err := rows.Scan(&e.Src, &e.Kind, &e.Dst, &e.Phase, &req, &e.Confidence, &meta, &prov); err != nil {
			return nil, err
		}
		e.Required = req == 1
		e.Metadata = unmarshalMeta(meta)
		e.Provenance = unmarshalMeta(prov)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- code smell helpers ----

// CodeSmellsForInvariants returns all code_smells from pattern nodes that
// have a "requires" edge targeting any of the given invariant node IDs.
// Results are deduplicated and sorted.
func (g *Graph) CodeSmellsForInvariants(ctx context.Context, invariantNodeIDs []string) ([]string, error) {
	if len(invariantNodeIDs) == 0 {
		return nil, nil
	}

	// Build query: find edges kind=requires whose dst is one of the invariant IDs.
	// Then fetch the src node when type = 'pattern'.
	rows, err := g.db.QueryContext(ctx, `
		SELECT DISTINCT n.metadata_json
		FROM edges e
		JOIN nodes n ON n.id = e.src
		WHERE e.kind = 'requires'
		  AND n.type = 'pattern'
		  AND e.dst IN (`+placeholders(len(invariantNodeIDs))+`)
	`, stringsToInterfaces(invariantNodeIDs)...)
	if err != nil {
		return nil, fmt.Errorf("CodeSmellsForInvariants: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var metaJSON string
		if err := rows.Scan(&metaJSON); err != nil {
			return nil, err
		}
		smells := extractCodeSmells(metaJSON)
		for _, s := range smells {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// PatternNamesForInvariants returns the IDs (names) of NodeTypePattern nodes
// that have an EdgeRequires edge to any of the given invariant node IDs.
// This is used by node-context to surface patterns.yaml pattern names alongside
// design_patterns.yaml patterns.
func (g *Graph) PatternNamesForInvariants(ctx context.Context, invariantNodeIDs []string) ([]string, error) {
	if len(invariantNodeIDs) == 0 {
		return nil, nil
	}
	rows, err := g.db.QueryContext(ctx, `
		SELECT DISTINCT n.name
		FROM edges e
		JOIN nodes n ON n.id = e.src
		WHERE e.kind = 'requires'
		  AND n.type = 'pattern'
		  AND e.dst IN (`+placeholders(len(invariantNodeIDs))+`)
		ORDER BY n.name
	`, stringsToInterfaces(invariantNodeIDs)...)
	if err != nil {
		return nil, fmt.Errorf("PatternNamesForInvariants: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// DesignContext is the set of design patterns, anti-patterns, and code smells
// linked to a given set of invariant node IDs.
type DesignContext struct {
	DesignPatterns []string
	AntiPatterns   []string
	CodeSmells     []string
}

// DesignContextForInvariants returns design patterns and anti-patterns that are
// linked to the given invariant node IDs via EdgeRequires (design_pattern) or
// EdgeViolates (anti_pattern). It also returns code_smell nodes attached to
// anti-patterns via EdgeSmellsLike, and code_smells embedded in metadata.
func (g *Graph) DesignContextForInvariants(ctx context.Context, invariantNodeIDs []string) (*DesignContext, error) {
	if len(invariantNodeIDs) == 0 {
		return &DesignContext{}, nil
	}

	dc := &DesignContext{}
	seen := map[string]bool{}
	args := stringsToInterfaces(invariantNodeIDs)
	ph := placeholders(len(invariantNodeIDs))

	// Design patterns: EdgeRequires or EdgeMitigates from design_pattern → invariant.
	dpRows, err := g.db.QueryContext(ctx, `
		SELECT DISTINCT n.name, n.summary
		FROM edges e
		JOIN nodes n ON n.id = e.src
		WHERE e.kind IN ('requires', 'mitigates')
		  AND n.type = 'design_pattern'
		  AND e.dst IN (`+ph+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("DesignContextForInvariants patterns: %w", err)
	}
	defer dpRows.Close()
	for dpRows.Next() {
		var name, summary string
		if err := dpRows.Scan(&name, &summary); err != nil {
			return nil, err
		}
		if !seen[name] {
			seen[name] = true
			dc.DesignPatterns = append(dc.DesignPatterns, name)
		}
	}
	if err := dpRows.Err(); err != nil {
		return nil, err
	}

	// Anti-patterns: EdgeViolates from anti_pattern → invariant.
	apRows, err := g.db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.name, n.metadata_json
		FROM edges e
		JOIN nodes n ON n.id = e.src
		WHERE e.kind = 'violates'
		  AND n.type = 'anti_pattern'
		  AND e.dst IN (`+ph+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("DesignContextForInvariants anti-patterns: %w", err)
	}
	defer apRows.Close()

	var antiPatternIDs []string
	for apRows.Next() {
		var id, name, metaJSON string
		if err := apRows.Scan(&id, &name, &metaJSON); err != nil {
			return nil, err
		}
		if !seen[name] {
			seen[name] = true
			dc.AntiPatterns = append(dc.AntiPatterns, name)
		}
		antiPatternIDs = append(antiPatternIDs, id)
		// Also extract code_smells embedded in anti-pattern metadata.
		for _, s := range extractCodeSmells(metaJSON) {
			if s != "" && !seen["smell:"+s] {
				seen["smell:"+s] = true
				dc.CodeSmells = append(dc.CodeSmells, s)
			}
		}
	}
	if err := apRows.Err(); err != nil {
		return nil, err
	}

	// Code smell nodes linked from anti-patterns via EdgeSmellsLike.
	if len(antiPatternIDs) > 0 {
		csRows, err := g.db.QueryContext(ctx, `
			SELECT DISTINCT n.name
			FROM edges e
			JOIN nodes n ON n.id = e.dst
			WHERE e.kind = 'smells_like'
			  AND n.type = 'code_smell'
			  AND e.src IN (`+placeholders(len(antiPatternIDs))+`)
		`, stringsToInterfaces(antiPatternIDs)...)
		if err != nil {
			return nil, fmt.Errorf("DesignContextForInvariants code smells: %w", err)
		}
		defer csRows.Close()
		for csRows.Next() {
			var name string
			if err := csRows.Scan(&name); err != nil {
				return nil, err
			}
			if name != "" && !seen["smell:"+name] {
				seen["smell:"+name] = true
				dc.CodeSmells = append(dc.CodeSmells, name)
			}
		}
		if err := csRows.Err(); err != nil {
			return nil, err
		}
	}

	sort.Strings(dc.DesignPatterns)
	sort.Strings(dc.AntiPatterns)
	sort.Strings(dc.CodeSmells)
	return dc, nil
}

// DesignContextForNode returns design patterns and anti-patterns that are
// directly linked to the given node ID via EdgeImplements, EdgeExhibits, or
// EdgeTouchesFile edges. This covers nodes (files, services) that have direct
// links in the design pattern layer even if they don't yet have invariant edges.
func (g *Graph) DesignContextForNode(ctx context.Context, nodeID string) (*DesignContext, error) {
	dc := &DesignContext{}
	seen := map[string]bool{}

	// Find design_pattern and anti_pattern nodes that link TO nodeID via
	// implements, exhibits, or touches_file edges.
	rows, err := g.db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.type, n.name, n.metadata_json
		FROM edges e
		JOIN nodes n ON n.id = e.src
		WHERE e.kind IN ('implements', 'exhibits', 'touches_file')
		  AND n.type IN ('design_pattern', 'anti_pattern')
		  AND e.dst = ?
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("DesignContextForNode: %w", err)
	}
	defer rows.Close()

	var antiPatternIDs []string
	for rows.Next() {
		var id, nodeType, name, metaJSON string
		if err := rows.Scan(&id, &nodeType, &name, &metaJSON); err != nil {
			return nil, err
		}
		switch nodeType {
		case NodeTypeDesignPattern:
			if !seen[name] {
				seen[name] = true
				dc.DesignPatterns = append(dc.DesignPatterns, name)
			}
		case NodeTypeAntiPattern:
			if !seen[name] {
				seen[name] = true
				dc.AntiPatterns = append(dc.AntiPatterns, name)
			}
			antiPatternIDs = append(antiPatternIDs, id)
			for _, s := range extractCodeSmells(metaJSON) {
				if s != "" && !seen["smell:"+s] {
					seen["smell:"+s] = true
					dc.CodeSmells = append(dc.CodeSmells, s)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Code smell nodes via EdgeSmellsLike from anti-patterns.
	if len(antiPatternIDs) > 0 {
		csRows, err := g.db.QueryContext(ctx, `
			SELECT DISTINCT n.name
			FROM edges e
			JOIN nodes n ON n.id = e.dst
			WHERE e.kind = 'smells_like'
			  AND n.type = 'code_smell'
			  AND e.src IN (`+placeholders(len(antiPatternIDs))+`)
		`, stringsToInterfaces(antiPatternIDs)...)
		if err != nil {
			return nil, fmt.Errorf("DesignContextForNode code smells: %w", err)
		}
		defer csRows.Close()
		for csRows.Next() {
			var name string
			if err := csRows.Scan(&name); err != nil {
				return nil, err
			}
			if name != "" && !seen["smell:"+name] {
				seen["smell:"+name] = true
				dc.CodeSmells = append(dc.CodeSmells, name)
			}
		}
		if err := csRows.Err(); err != nil {
			return nil, err
		}
	}

	sort.Strings(dc.DesignPatterns)
	sort.Strings(dc.AntiPatterns)
	sort.Strings(dc.CodeSmells)
	return dc, nil
}

// extractCodeSmells unpacks the code_smells array from a node's metadata_json blob.
func extractCodeSmells(metaJSON string) []string {
	var meta map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		return nil
	}
	raw, ok := meta["code_smells"]
	if !ok {
		return nil
	}
	var smells []string
	_ = json.Unmarshal(raw, &smells)
	return smells
}

// placeholders returns n comma-separated "?" for SQL IN clauses.
func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, n*2-1)
	for i := range b {
		if i%2 == 0 {
			b[i] = '?'
		} else {
			b[i] = ','
		}
	}
	return string(b)
}

// stringsToInterfaces converts []string to []interface{} for variadic SQL args.
func stringsToInterfaces(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range out {
		_ = s
		out[i] = ss[i]
	}
	return out
}

// ---- preflight audit ----

// PreflightAuditRecord is a durable record of one preflight run.
type PreflightAuditRecord struct {
	ID             string
	Task           string
	Timestamp      int64
	GitSHA         string
	Files          []string
	ForbiddenFixes []string
	Invariants     []string
	CodeSmells     []string
	CreatedAt      int64
}

// InsertPreflightAudit inserts a durable preflight audit record.
func (g *Graph) InsertPreflightAudit(ctx context.Context, r PreflightAuditRecord) error {
	if r.ID == "" {
		r.ID = fmt.Sprintf("preflight-audit-%d", time.Now().UnixNano())
	}
	now := time.Now().Unix()
	if r.Timestamp == 0 {
		r.Timestamp = now
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}

	filesJSON, _ := json.Marshal(r.Files)
	ffJSON, _ := json.Marshal(r.ForbiddenFixes)
	invJSON, _ := json.Marshal(r.Invariants)
	csJSON, _ := json.Marshal(r.CodeSmells)

	_, err := g.db.ExecContext(ctx, `
		INSERT INTO preflight_audits
			(id, task, timestamp, git_sha, files_json, forbidden_fixes_json, invariants_json, code_smells_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task                 = excluded.task,
			timestamp            = excluded.timestamp,
			git_sha              = excluded.git_sha,
			files_json           = excluded.files_json,
			forbidden_fixes_json = excluded.forbidden_fixes_json,
			invariants_json      = excluded.invariants_json,
			code_smells_json     = excluded.code_smells_json
	`, r.ID, r.Task, r.Timestamp, r.GitSHA,
		string(filesJSON), string(ffJSON), string(invJSON), string(csJSON), r.CreatedAt)
	if err != nil {
		return fmt.Errorf("InsertPreflightAudit: %w", err)
	}
	return nil
}

// QueryPreflightAudits returns audit records, optionally filtered by since (unix
// timestamp lower bound, 0 = no bound) and gitSHA (empty = no filter).
// Results are ordered by timestamp descending.
func (g *Graph) QueryPreflightAudits(ctx context.Context, since int64, gitSHA string) ([]*PreflightAuditRecord, error) {
	query := `
		SELECT id, task, timestamp, git_sha, files_json, forbidden_fixes_json, invariants_json, code_smells_json, created_at
		FROM preflight_audits
		WHERE timestamp >= ?`
	args := []interface{}{since}

	if gitSHA != "" {
		query += ` AND git_sha = ?`
		args = append(args, gitSHA)
	}
	query += ` ORDER BY timestamp DESC`

	rows, err := g.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("QueryPreflightAudits: %w", err)
	}
	defer rows.Close()

	var out []*PreflightAuditRecord
	for rows.Next() {
		var r PreflightAuditRecord
		var filesJSON, ffJSON, invJSON, csJSON string
		if err := rows.Scan(&r.ID, &r.Task, &r.Timestamp, &r.GitSHA,
			&filesJSON, &ffJSON, &invJSON, &csJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(filesJSON), &r.Files)
		_ = json.Unmarshal([]byte(ffJSON), &r.ForbiddenFixes)
		_ = json.Unmarshal([]byte(invJSON), &r.Invariants)
		_ = json.Unmarshal([]byte(csJSON), &r.CodeSmells)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// AgentUsageEvent is a single recorded preflight/agent-context call.
type AgentUsageEvent struct {
	ID                string
	EventTime         int64
	Agent             string
	SessionIDHash     string
	Repo              string
	Tool              string
	Operation         string // "called" | "skipped" | "failed"
	ResultStatus      string
	Confidence        string
	TaskType          string
	ChangedFilesCount int
}

// RecordAgentUsage inserts a usage event. Raw prompts are never stored.
func (g *Graph) RecordAgentUsage(ctx context.Context, e AgentUsageEvent) error {
	if e.ID == "" {
		return errors.New("RecordAgentUsage: id required")
	}
	if e.EventTime == 0 {
		e.EventTime = time.Now().Unix()
	}
	_, err := g.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO agent_usage_events
		(id, event_time, agent, session_id_hash, repo, tool, operation, result_status, confidence, task_type, changed_files_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.EventTime, e.Agent, e.SessionIDHash, e.Repo, e.Tool, e.Operation,
		e.ResultStatus, e.Confidence, e.TaskType, e.ChangedFilesCount)
	if err != nil {
		return fmt.Errorf("RecordAgentUsage: %w", err)
	}
	return nil
}

// AgentUsageSummary holds aggregate usage stats over a time window.
type AgentUsageSummary struct {
	WindowDays                       int     `json:"window_days"`
	SessionsTotal                    int     `json:"sessions_total"`
	PreflightCalls                   int     `json:"preflight_calls"`
	AgentContextCalls                int     `json:"agent_context_calls"`
	ScanViolationsCalls              int     `json:"scan_violations_calls"`
	PreEditContextCalls              int     `json:"pre_edit_context_calls"`
	CommitsWithoutIntegrityCheck     int     `json:"commits_without_integrity_check"`
	PreflightSkipRatePct             float64 `json:"preflight_skip_rate_pct"`
	Status                           string  `json:"status"`
	RecommendedAction                string  `json:"recommended_action,omitempty"`
}

// QueryAgentUsageSummary returns aggregate usage stats for a rolling window of
// windowDays days. Sessions are counted by distinct non-empty session_id_hash
// values. Skip rate = 1 - (preflight_calls / sessions).
func (g *Graph) QueryAgentUsageSummary(ctx context.Context, windowDays int) (*AgentUsageSummary, error) {
	since := time.Now().AddDate(0, 0, -windowDays).Unix()

	s := &AgentUsageSummary{WindowDays: windowDays}

	// Sessions = distinct session_id_hash values.
	row := g.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id_hash) FROM agent_usage_events WHERE event_time >= ? AND session_id_hash != ''`, since)
	_ = row.Scan(&s.SessionsTotal)

	countTool := func(tool string) int {
		var n int
		r := g.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM agent_usage_events WHERE event_time >= ? AND tool = ? AND operation = 'called'`, since, tool)
		_ = r.Scan(&n)
		return n
	}

	s.PreflightCalls = countTool("awareness.preflight")
	s.AgentContextCalls = countTool("awareness.agent_context")
	s.ScanViolationsCalls = countTool("awareness.scan_violations")
	s.PreEditContextCalls = countTool("awareness.pre_edit_context")

	// Commits without integrity check = events with tool "commit.graph_integrity" and operation "skipped".
	var commitSkips int
	r := g.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_usage_events WHERE event_time >= ? AND tool = 'commit.graph_integrity' AND operation = 'skipped'`, since)
	_ = r.Scan(&commitSkips)
	s.CommitsWithoutIntegrityCheck = commitSkips

	if s.SessionsTotal > 0 {
		s.PreflightSkipRatePct = (1 - float64(s.PreflightCalls)/float64(s.SessionsTotal)) * 100
		if s.PreflightSkipRatePct < 0 {
			s.PreflightSkipRatePct = 0
		}
	}

	switch {
	case s.SessionsTotal == 0:
		s.Status = "no_data"
		s.RecommendedAction = "Configure session-start hook to call awareness.agent_context"
	case s.CommitsWithoutIntegrityCheck > 0:
		s.Status = "warning"
		s.RecommendedAction = fmt.Sprintf("%d commits bypassed graph integrity check — run awareness.graph_integrity_check before committing", s.CommitsWithoutIntegrityCheck)
	case s.PreflightSkipRatePct > 50:
		s.Status = "warning"
		s.RecommendedAction = "Configure session-start hook to call awareness.agent_context — skip rate is high"
	default:
		s.Status = "ok"
	}

	return s, nil
}

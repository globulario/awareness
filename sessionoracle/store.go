package sessionoracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/globulario/awareness/contextfreshness"
	"github.com/globulario/awareness/graph"
	"github.com/google/uuid"
)

// Oracle is the session resumption store backed by the awareness graph DB.
type Oracle struct {
	db *sql.DB
	g  *graph.Graph
}

// New returns an Oracle backed by the given awareness graph.
func New(g *graph.Graph) *Oracle {
	return &Oracle{db: g.DB(), g: g}
}

// ── Session lifecycle ─────────────────────────────────────────────────────────

// StartSession creates a new agent session record.
func (o *Oracle) StartSession(ctx context.Context, req StartSessionRequest) (*AgentSession, error) {
	id := req.ID
	if id == "" {
		id = "SES-" + uuid.New().String()[:8]
	}
	actor := req.Actor
	if actor == "" {
		actor = "claude"
	}

	branch, commit := gitBranchAndCommit(req.RepoRoot)

	s := &AgentSession{
		ID:              id,
		Title:           req.Title,
		Objective:       req.Objective,
		Actor:           actor,
		Status:          "open",
		StartedAt:       time.Now().Unix(),
		ParentSessionID: req.ParentSessionID,
		RepoRoot:        req.RepoRoot,
		Branch:          branch,
		GitCommitStart:  commit,
	}

	_, err := o.db.ExecContext(ctx, `
		INSERT INTO agent_sessions
		  (id,title,objective,actor,status,started_at,parent_session_id,repo_root,branch,git_commit_start)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Title, s.Objective, s.Actor, s.Status, s.StartedAt,
		s.ParentSessionID, s.RepoRoot, s.Branch, s.GitCommitStart)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: start session: %w", err)
	}
	return s, nil
}

// GetSession loads a session by ID.
func (o *Oracle) GetSession(ctx context.Context, id string) (*AgentSession, error) {
	var s AgentSession
	var endedAt sql.NullInt64
	var parentID sql.NullString
	err := o.db.QueryRowContext(ctx, `
		SELECT id,title,objective,actor,status,started_at,ended_at,parent_session_id,
		       repo_root,branch,git_commit_start,git_commit_end
		FROM agent_sessions WHERE id=?`, id).Scan(
		&s.ID, &s.Title, &s.Objective, &s.Actor, &s.Status, &s.StartedAt,
		&endedAt, &parentID, &s.RepoRoot, &s.Branch, &s.GitCommitStart, &s.GitCommitEnd)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: get session %s: %w", id, err)
	}
	if endedAt.Valid {
		s.EndedAt = endedAt.Int64
	}
	if parentID.Valid {
		s.ParentSessionID = parentID.String
	}
	return &s, nil
}

// LatestOpenSession returns the most recent open session for the given repo root.
func (o *Oracle) LatestOpenSession(ctx context.Context, repoRoot string) (*AgentSession, error) {
	var id string
	err := o.db.QueryRowContext(ctx,
		`SELECT id FROM agent_sessions WHERE status='open' AND repo_root=? ORDER BY started_at DESC, rowid DESC LIMIT 1`,
		repoRoot).Scan(&id)
	if err == sql.ErrNoRows {
		// Fall back to any recent closed session.
		err = o.db.QueryRowContext(ctx,
			`SELECT id FROM agent_sessions WHERE repo_root=? ORDER BY started_at DESC, rowid DESC LIMIT 1`,
			repoRoot).Scan(&id)
	}
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: no session for repo %s: %w", repoRoot, err)
	}
	return o.GetSession(ctx, id)
}

// CloseSession marks a session closed, builds a resume snapshot, and optionally pushes to AI Memory.
func (o *Oracle) CloseSession(ctx context.Context, sessionID string, pushToAIMemory bool, bridge AIMemoryBridge) (*SessionResumeSnapshot, error) {
	_, commit := gitBranchAndCommit("")

	_, err := o.db.ExecContext(ctx,
		`UPDATE agent_sessions SET status='closed', ended_at=?, git_commit_end=? WHERE id=?`,
		time.Now().Unix(), commit, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: close session: %w", err)
	}

	snap, err := o.BuildResumeSnapshot(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if pushToAIMemory && bridge != nil {
		sess, _ := o.GetSession(ctx, sessionID)
		_ = bridge.StoreSessionSummary(ctx, sessionToMemorySummary(sess, snap))
		for _, d := range snap.Decisions {
			if d.Confidence == "high" {
				_ = bridge.StoreDurableDecision(ctx, d)
			}
		}
	}
	return snap, nil
}

// ── Events ────────────────────────────────────────────────────────────────────

// RecordSessionEvent appends a raw event to the session log.
func (o *Oracle) RecordSessionEvent(ctx context.Context, sessionID, eventType, title, body string, payload interface{}, turnIndex int) error {
	payloadJSON := ""
	if payload != nil {
		b, _ := json.Marshal(payload)
		payloadJSON = string(b)
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_events (id,session_id,turn_index,event_type,title,body,payload_json,created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New().String(), sessionID, turnIndex, eventType, title, body, payloadJSON, time.Now().Unix())
	return err
}

// ── File touches ──────────────────────────────────────────────────────────────

// RecordFileTouch persists a file access record and captures fingerprints.
func (o *Oracle) RecordFileTouch(ctx context.Context, sessionID, path, action, reason string, turnIndex int) (*SessionFileTouch, error) {
	// Determine sequence number (next for this session).
	var seq int
	_ = o.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence),0)+1 FROM session_file_touches WHERE session_id=?`,
		sessionID).Scan(&seq)

	fpBefore := fingerprint(path)
	fpAfter := ""
	if action == "edit" || action == "create" || action == "delete" || action == "rename" {
		fpAfter = fpBefore // will be updated by caller after the edit, or left empty
	}

	id := "FT-" + uuid.New().String()[:8]
	now := time.Now().Unix()
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_file_touches
		  (id,session_id,path,action,sequence,fingerprint_before,fingerprint_after,reason,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		id, sessionID, path, action, seq, fpBefore, fpAfter, reason, now)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record file touch: %w", err)
	}

	// Also cooperate with Stale Context Detection: record a context read for reads/inspects.
	if (action == "read" || action == "inspect") && o.g != nil {
		tracker := contextfreshness.New(o.g)
		_, _ = tracker.RecordContextRead(ctx, sessionID, path, reason, "session_oracle", turnIndex)
	}

	return &SessionFileTouch{
		ID:                id,
		SessionID:         sessionID,
		Path:              path,
		Action:            action,
		Sequence:          seq,
		FingerprintBefore: fpBefore,
		Reason:            reason,
		CreatedAt:         now,
	}, nil
}

// UpdateFileTouchAfter sets the fingerprint_after for a previously recorded touch.
func (o *Oracle) UpdateFileTouchAfter(ctx context.Context, touchID, fingerprintAfter string) error {
	_, err := o.db.ExecContext(ctx,
		`UPDATE session_file_touches SET fingerprint_after=? WHERE id=?`,
		fingerprintAfter, touchID)
	return err
}

// ListFileTouches returns all file touches for a session in sequence order.
func (o *Oracle) ListFileTouches(ctx context.Context, sessionID string) ([]SessionFileTouch, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,path,action,sequence,fingerprint_before,fingerprint_after,reason,created_at
		FROM session_file_touches WHERE session_id=? ORDER BY sequence ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionFileTouch
	for rows.Next() {
		var ft SessionFileTouch
		if err := rows.Scan(&ft.ID, &ft.SessionID, &ft.Path, &ft.Action, &ft.Sequence,
			&ft.FingerprintBefore, &ft.FingerprintAfter, &ft.Reason, &ft.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ft)
	}
	return out, rows.Err()
}

// ── Decisions ─────────────────────────────────────────────────────────────────

// RecordDecision persists an architectural decision.
func (o *Oracle) RecordDecision(ctx context.Context, req RecordDecisionRequest) (*SessionDecision, error) {
	if req.Confidence == "" {
		req.Confidence = "medium"
	}
	d := &SessionDecision{
		ID:                     "DEC-" + uuid.New().String()[:8],
		SessionID:              req.SessionID,
		Title:                  req.Title,
		Decision:               req.Decision,
		Rationale:              req.Rationale,
		AlternativesConsidered: req.AlternativesConsidered,
		RelatedFiles:           req.RelatedFiles,
		RelatedInvariants:      req.RelatedInvariants,
		RelatedIncidents:       req.RelatedIncidents,
		Confidence:             req.Confidence,
		CreatedAt:              time.Now().Unix(),
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_decisions
		  (id,session_id,title,decision,rationale,alternatives_considered,
		   related_files,related_invariants,related_incidents,confidence,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.SessionID, d.Title, d.Decision, d.Rationale,
		joinStrings(d.AlternativesConsidered), joinStrings(d.RelatedFiles),
		joinStrings(d.RelatedInvariants), joinStrings(d.RelatedIncidents),
		d.Confidence, d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record decision: %w", err)
	}
	return d, nil
}

// ListDecisions returns all decisions for a session.
func (o *Oracle) ListDecisions(ctx context.Context, sessionID string) ([]SessionDecision, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,title,decision,rationale,alternatives_considered,
		       related_files,related_invariants,related_incidents,confidence,created_at
		FROM session_decisions WHERE session_id=? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionDecision
	for rows.Next() {
		var d SessionDecision
		var alt, rf, ri, rinc string
		if err := rows.Scan(&d.ID, &d.SessionID, &d.Title, &d.Decision, &d.Rationale,
			&alt, &rf, &ri, &rinc, &d.Confidence, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.AlternativesConsidered = splitStrings(alt)
		d.RelatedFiles = splitStrings(rf)
		d.RelatedInvariants = splitStrings(ri)
		d.RelatedIncidents = splitStrings(rinc)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ── Assumptions ───────────────────────────────────────────────────────────────

// RecordAssumption persists an unverified assumption.
func (o *Oracle) RecordAssumption(ctx context.Context, req RecordAssumptionRequest) (*SessionAssumption, error) {
	a := &SessionAssumption{
		ID:             "ASM-" + uuid.New().String()[:8],
		SessionID:      req.SessionID,
		Assumption:     req.Assumption,
		Basis:          req.Basis,
		Status:         "unverified",
		ValidationPlan: req.ValidationPlan,
		RelatedFiles:   req.RelatedFiles,
		CreatedAt:      time.Now().Unix(),
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_assumptions
		  (id,session_id,assumption,basis,status,validation_plan,related_files,created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, a.SessionID, a.Assumption, a.Basis, a.Status, a.ValidationPlan, a.RelatedFiles, a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record assumption: %w", err)
	}
	return a, nil
}

// ResolveAssumption updates the status of an assumption.
func (o *Oracle) ResolveAssumption(ctx context.Context, assumptionID, status, _ string) error {
	_, err := o.db.ExecContext(ctx,
		`UPDATE session_assumptions SET status=?, resolved_at=? WHERE id=?`,
		status, time.Now().Unix(), assumptionID)
	return err
}

// ListAssumptions returns all assumptions for a session.
func (o *Oracle) ListAssumptions(ctx context.Context, sessionID string) ([]SessionAssumption, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,assumption,basis,status,validation_plan,related_files,created_at,resolved_at
		FROM session_assumptions WHERE session_id=? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionAssumption
	for rows.Next() {
		var a SessionAssumption
		var resolvedAt sql.NullInt64
		if err := rows.Scan(&a.ID, &a.SessionID, &a.Assumption, &a.Basis,
			&a.Status, &a.ValidationPlan, &a.RelatedFiles, &a.CreatedAt, &resolvedAt); err != nil {
			return nil, err
		}
		if resolvedAt.Valid {
			a.ResolvedAt = resolvedAt.Int64
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── Unfinished work ───────────────────────────────────────────────────────────

// RecordUnfinishedWork persists an incomplete task.
func (o *Oracle) RecordUnfinishedWork(ctx context.Context, req RecordUnfinishedWorkRequest) (*SessionUnfinishedWork, error) {
	if req.Priority == "" {
		req.Priority = "medium"
	}
	w := &SessionUnfinishedWork{
		ID:               "TODO-" + uuid.New().String()[:8],
		SessionID:        req.SessionID,
		Title:            req.Title,
		Description:      req.Description,
		Priority:         req.Priority,
		ReasonUnfinished: req.ReasonUnfinished,
		NextAction:       req.NextAction,
		RelatedFiles:     req.RelatedFiles,
		RelatedTests:     req.RelatedTests,
		RelatedIncidents: req.RelatedIncidents,
		Status:           "open",
		CreatedAt:        time.Now().Unix(),
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_unfinished_work
		  (id,session_id,title,description,priority,reason_unfinished,next_action,
		   related_files,related_tests,related_incidents,status,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.SessionID, w.Title, w.Description, w.Priority, w.ReasonUnfinished, w.NextAction,
		joinStrings(w.RelatedFiles), joinStrings(w.RelatedTests), joinStrings(w.RelatedIncidents),
		w.Status, w.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record unfinished work: %w", err)
	}
	return w, nil
}

// CloseUnfinishedWork marks a work item closed.
func (o *Oracle) CloseUnfinishedWork(ctx context.Context, workID, _ string) error {
	_, err := o.db.ExecContext(ctx,
		`UPDATE session_unfinished_work SET status='closed', closed_at=? WHERE id=?`,
		time.Now().Unix(), workID)
	return err
}

// ListUnfinishedWork returns all open work items for a session.
func (o *Oracle) ListUnfinishedWork(ctx context.Context, sessionID string) ([]SessionUnfinishedWork, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,title,description,priority,reason_unfinished,next_action,
		       related_files,related_tests,related_incidents,status,created_at,closed_at
		FROM session_unfinished_work WHERE session_id=? ORDER BY
		  CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END,
		  created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionUnfinishedWork
	for rows.Next() {
		var w SessionUnfinishedWork
		var rf, rt, ri string
		var closedAt sql.NullInt64
		if err := rows.Scan(&w.ID, &w.SessionID, &w.Title, &w.Description, &w.Priority,
			&w.ReasonUnfinished, &w.NextAction, &rf, &rt, &ri, &w.Status, &w.CreatedAt, &closedAt); err != nil {
			return nil, err
		}
		w.RelatedFiles = splitStrings(rf)
		w.RelatedTests = splitStrings(rt)
		w.RelatedIncidents = splitStrings(ri)
		if closedAt.Valid {
			w.ClosedAt = closedAt.Int64
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ── Warnings ──────────────────────────────────────────────────────────────────

// RecordSessionWarning persists a warning that was active during the session.
func (o *Oracle) RecordSessionWarning(ctx context.Context, req RecordSessionWarningRequest) (*SessionWarning, error) {
	w := &SessionWarning{
		ID:              "WARN-" + uuid.New().String()[:8],
		SessionID:       req.SessionID,
		WarningType:     req.WarningType,
		Severity:        req.Severity,
		Message:         req.Message,
		RelatedFile:     req.RelatedFile,
		RelatedIncident: req.RelatedIncident,
		CreatedAt:       time.Now().Unix(),
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_warnings
		  (id,session_id,warning_type,severity,message,related_file,related_incident,acknowledged,created_at)
		VALUES (?,?,?,?,?,?,?,0,?)`,
		w.ID, w.SessionID, w.WarningType, w.Severity, w.Message,
		w.RelatedFile, w.RelatedIncident, w.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record warning: %w", err)
	}
	return w, nil
}

// ListWarnings returns all warnings for a session.
func (o *Oracle) ListWarnings(ctx context.Context, sessionID string) ([]SessionWarning, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,warning_type,severity,message,related_file,related_incident,
		       acknowledged,created_at,acknowledged_at
		FROM session_warnings WHERE session_id=? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionWarning
	for rows.Next() {
		var w SessionWarning
		var ack int
		var ackedAt sql.NullInt64
		if err := rows.Scan(&w.ID, &w.SessionID, &w.WarningType, &w.Severity, &w.Message,
			&w.RelatedFile, &w.RelatedIncident, &ack, &w.CreatedAt, &ackedAt); err != nil {
			return nil, err
		}
		w.Acknowledged = ack != 0
		if ackedAt.Valid {
			w.AcknowledgedAt = ackedAt.Int64
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ── Test results ──────────────────────────────────────────────────────────────

// RecordTestResult persists the result of a test run.
func (o *Oracle) RecordTestResult(ctx context.Context, req RecordTestResultRequest) (*SessionTestResult, error) {
	r := &SessionTestResult{
		ID:            "TEST-" + uuid.New().String()[:8],
		SessionID:     req.SessionID,
		Command:       req.Command,
		Status:        req.Status,
		Summary:       req.Summary,
		OutputExcerpt: req.OutputExcerpt,
		RelatedFiles:  req.RelatedFiles,
		CreatedAt:     time.Now().Unix(),
	}
	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_test_results
		  (id,session_id,command,status,summary,output_excerpt,related_files,created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		r.ID, r.SessionID, r.Command, r.Status, r.Summary, r.OutputExcerpt,
		joinStrings(r.RelatedFiles), r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("sessionoracle: record test result: %w", err)
	}
	return r, nil
}

// ListTestResults returns all test results for a session.
func (o *Oracle) ListTestResults(ctx context.Context, sessionID string) ([]SessionTestResult, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT id,session_id,command,status,summary,output_excerpt,related_files,created_at
		FROM session_test_results WHERE session_id=? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionTestResult
	for rows.Next() {
		var r SessionTestResult
		var rf string
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Command, &r.Status,
			&r.Summary, &r.OutputExcerpt, &rf, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.RelatedFiles = splitStrings(rf)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func joinStrings(ss []string) string  { return strings.Join(ss, "|") }
func splitStrings(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

// fingerprint returns the sha256 of a file's content, or "" if unreadable.
func fingerprint(path string) string {
	snap, err := contextfreshness.Fingerprint(path)
	if err != nil {
		return ""
	}
	return snap.Fingerprint
}

// gitBranchAndCommit returns the current git branch and HEAD commit in the given dir.
func gitBranchAndCommit(repoRoot string) (branch, commit string) {
	args := []string{"rev-parse", "--abbrev-ref", "HEAD"}
	if repoRoot != "" {
		args = append([]string{"-C", repoRoot}, args...)
	}
	out, err := exec.Command("git", args...).Output()
	if err == nil {
		branch = strings.TrimSpace(string(out))
	}

	args2 := []string{"rev-parse", "HEAD"}
	if repoRoot != "" {
		args2 = append([]string{"-C", repoRoot}, args2...)
	}
	out2, err := exec.Command("git", args2...).Output()
	if err == nil {
		commit = strings.TrimSpace(string(out2))
	}
	return
}

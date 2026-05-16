package sessionoracle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/awareness/contextfreshness"
	"github.com/google/uuid"
)

// BuildResumeSnapshot synthesises a structured oracle snapshot from all session records.
func (o *Oracle) BuildResumeSnapshot(ctx context.Context, sessionID string) (*SessionResumeSnapshot, error) {
	sess, err := o.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	touches, err := o.ListFileTouches(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	decisions, err := o.ListDecisions(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	assumptions, err := o.ListAssumptions(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	work, err := o.ListUnfinishedWork(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	warnings, err := o.ListWarnings(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	tests, err := o.ListTestResults(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	snap := &SessionResumeSnapshot{
		ID:          "RESUME-" + uuid.New().String()[:8],
		SessionID:   sessionID,
		Objective:   sess.Objective,
		FilesTouched: touches,
		Decisions:   decisions,
		Assumptions: assumptions,
		Unfinished:  work,
		Warnings:    warnings,
		Tests:       tests,
		CreatedAt:   time.Now().Unix(),
	}
	snap.Summary = buildSummary(sess, snap)
	snap.RecommendedNextAction = buildRecommendedNextAction(snap)

	// Persist the snapshot for future resume calls.
	if err := o.persistSnapshot(ctx, snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// ResumeSession loads the latest snapshot for a session, refreshing stale context.
func (o *Oracle) ResumeSession(ctx context.Context, sessionID string) (*SessionResumeSnapshot, error) {
	snap, err := o.latestStoredSnapshot(ctx, sessionID)
	if err != nil {
		// Rebuild on the fly if no stored snapshot.
		return o.BuildResumeSnapshot(ctx, sessionID)
	}

	// Recheck stale context for all previously touched files.
	snap.Warnings = o.refreshStaleWarnings(ctx, sessionID, snap)
	snap.RecommendedNextAction = buildRecommendedNextAction(snap)
	return snap, nil
}

// ResumeLatestOpenSession finds the most recent open session and resumes it.
func (o *Oracle) ResumeLatestOpenSession(ctx context.Context, repoRoot string) (*SessionResumeSnapshot, error) {
	sess, err := o.LatestOpenSession(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	return o.ResumeSession(ctx, sess.ID)
}

// ── internals ─────────────────────────────────────────────────────────────────

func (o *Oracle) persistSnapshot(ctx context.Context, snap *SessionResumeSnapshot) error {
	filesJSON, _ := json.Marshal(snap.FilesTouched)
	decsJSON, _ := json.Marshal(snap.Decisions)
	unfinJSON, _ := json.Marshal(snap.Unfinished)
	warnJSON, _ := json.Marshal(snap.Warnings)
	testsJSON, _ := json.Marshal(snap.Tests)

	_, err := o.db.ExecContext(ctx, `
		INSERT INTO session_resume_snapshots
		  (id,session_id,summary,objective,files_touched_json,decisions_json,
		   unfinished_json,warnings_json,tests_json,recommended_next_action,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		snap.ID, snap.SessionID, snap.Summary, snap.Objective,
		string(filesJSON), string(decsJSON), string(unfinJSON),
		string(warnJSON), string(testsJSON), snap.RecommendedNextAction, snap.CreatedAt)
	return err
}

func (o *Oracle) latestStoredSnapshot(ctx context.Context, sessionID string) (*SessionResumeSnapshot, error) {
	var snap SessionResumeSnapshot
	var filesJSON, decsJSON, unfinJSON, warnJSON, testsJSON string
	err := o.db.QueryRowContext(ctx, `
		SELECT id,session_id,summary,objective,files_touched_json,decisions_json,
		       unfinished_json,warnings_json,tests_json,recommended_next_action,created_at
		FROM session_resume_snapshots WHERE session_id=? ORDER BY created_at DESC LIMIT 1`,
		sessionID).Scan(&snap.ID, &snap.SessionID, &snap.Summary, &snap.Objective,
		&filesJSON, &decsJSON, &unfinJSON, &warnJSON, &testsJSON,
		&snap.RecommendedNextAction, &snap.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no stored snapshot")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(filesJSON), &snap.FilesTouched)
	_ = json.Unmarshal([]byte(decsJSON), &snap.Decisions)
	_ = json.Unmarshal([]byte(unfinJSON), &snap.Unfinished)
	_ = json.Unmarshal([]byte(warnJSON), &snap.Warnings)
	_ = json.Unmarshal([]byte(testsJSON), &snap.Tests)
	return &snap, nil
}

// refreshStaleWarnings checks whether files touched in the session have changed since
// last read, injects stale-context warnings, and returns the merged warning list.
func (o *Oracle) refreshStaleWarnings(ctx context.Context, sessionID string, snap *SessionResumeSnapshot) []SessionWarning {
	existing := make(map[string]bool)
	for _, w := range snap.Warnings {
		if w.WarningType == "stale_context" {
			existing[w.RelatedFile] = true
		}
	}

	result := snap.Warnings
	if o.g == nil {
		return result
	}

	tracker := contextfreshness.New(o.g)
	var paths []string
	seen := map[string]bool{}
	for _, ft := range snap.FilesTouched {
		if !seen[ft.Path] {
			seen[ft.Path] = true
			paths = append(paths, ft.Path)
		}
	}
	if len(paths) == 0 {
		return result
	}

	staleWarnings, _ := tracker.CheckStaleContext(ctx, sessionID, paths, 0, contextfreshness.SeverityWarning)
	for _, sw := range staleWarnings {
		if existing[sw.Path] {
			continue
		}
		result = append(result, SessionWarning{
			ID:          "WARN-" + uuid.New().String()[:8],
			SessionID:   sessionID,
			WarningType: "stale_context",
			Severity:    sw.Severity,
			Message:     fmt.Sprintf("%s changed since last read (was %s, now %s)", sw.Path, sw.ReadFingerprint, sw.CurrentFingerprint),
			RelatedFile: sw.Path,
			CreatedAt:   time.Now().Unix(),
		})
	}
	return result
}

// buildSummary generates a human-readable summary of the session.
func buildSummary(sess *AgentSession, snap *SessionResumeSnapshot) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session %s", sess.ID)
	if sess.Title != "" {
		fmt.Fprintf(&sb, " — %s", sess.Title)
	}
	fmt.Fprintf(&sb, " (%s).", sess.Status)

	if len(snap.FilesTouched) > 0 {
		fmt.Fprintf(&sb, " Touched %d file(s).", len(snap.FilesTouched))
	}
	if len(snap.Decisions) > 0 {
		fmt.Fprintf(&sb, " %d decision(s) recorded.", len(snap.Decisions))
	}
	open := 0
	for _, w := range snap.Unfinished {
		if w.Status == "open" || w.Status == "in_progress" || w.Status == "blocked" {
			open++
		}
	}
	if open > 0 {
		fmt.Fprintf(&sb, " %d unfinished item(s) remain.", open)
	}
	failed := 0
	for _, t := range snap.Tests {
		if t.Status == "failed" || t.Status == "error" {
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintf(&sb, " %d test run(s) failed.", failed)
	}
	return sb.String()
}

// buildRecommendedNextAction derives the most important next step using priority ordering:
//  1. Critical stale context warnings
//  2. Critical incident warnings
//  3. Failed tests
//  4. Open high-priority unfinished work
//  5. Unverified assumptions
//  6. Objective (if nothing else)
func buildRecommendedNextAction(snap *SessionResumeSnapshot) string {
	for _, w := range snap.Warnings {
		if w.WarningType == "stale_context" && w.Severity == "critical" && !w.Acknowledged {
			return fmt.Sprintf("CRITICAL: Re-read %s — file changed since last session. Previous decisions may be stale.", w.RelatedFile)
		}
	}
	for _, w := range snap.Warnings {
		if w.WarningType == "incident_pattern" && w.Severity == "critical" && !w.Acknowledged {
			return fmt.Sprintf("CRITICAL: Active incident warning — %s. Acknowledge before proceeding.", w.Message)
		}
	}
	for _, w := range snap.Warnings {
		if w.WarningType == "stale_context" && !w.Acknowledged {
			return fmt.Sprintf("Re-read %s — file changed since last session.", w.RelatedFile)
		}
	}
	for _, t := range snap.Tests {
		if t.Status == "failed" || t.Status == "error" {
			return fmt.Sprintf("Fix failing test: %s", t.Command)
		}
	}
	for _, w := range snap.Unfinished {
		if (w.Status == "open" || w.Status == "in_progress") &&
			(w.Priority == "critical" || w.Priority == "high") {
			if w.NextAction != "" {
				return w.NextAction
			}
			return fmt.Sprintf("Complete: %s", w.Title)
		}
	}
	for _, a := range snap.Assumptions {
		if a.Status == "unverified" {
			if a.ValidationPlan != "" {
				return fmt.Sprintf("Verify assumption: %s — plan: %s", a.Assumption, a.ValidationPlan)
			}
			return fmt.Sprintf("Verify assumption: %s", a.Assumption)
		}
	}
	for _, w := range snap.Unfinished {
		if w.Status == "open" || w.Status == "in_progress" {
			if w.NextAction != "" {
				return w.NextAction
			}
			return fmt.Sprintf("Complete: %s", w.Title)
		}
	}
	if snap.Objective != "" {
		return fmt.Sprintf("Continue: %s", snap.Objective)
	}
	return "No specific next action recorded. Review session history."
}

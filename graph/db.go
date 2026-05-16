package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS nodes (
    id            TEXT PRIMARY KEY,
    type          TEXT NOT NULL,
    name          TEXT NOT NULL,
    path          TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS edges (
    src           TEXT NOT NULL,
    kind          TEXT NOT NULL,
    dst           TEXT NOT NULL,
    phase         TEXT NOT NULL DEFAULT '',
    required      INTEGER NOT NULL DEFAULT 0,
    confidence    REAL NOT NULL DEFAULT 1.0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (src, kind, dst, phase)
);

CREATE TABLE IF NOT EXISTS invariants (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS failure_modes (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',
    symptoms_json    TEXT NOT NULL DEFAULT '[]',
    root_cause       TEXT NOT NULL DEFAULT '',
    architecture_fix TEXT NOT NULL DEFAULT '',
    metadata_json    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS agent_context_cache (
    cache_key        TEXT PRIMARY KEY,
    task             TEXT NOT NULL,
    context_markdown TEXT NOT NULL,
    created_at       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS graph_builds (
    id          TEXT PRIMARY KEY,
    repo_root   TEXT NOT NULL,
    git_commit  TEXT NOT NULL DEFAULT '',
    release_id  TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL DEFAULT 0,
    stats_json  TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS incidents (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    severity      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    started_at    INTEGER NOT NULL DEFAULT 0,
    ended_at      INTEGER NOT NULL DEFAULT 0,
    summary       TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS awareness_proposals (
    id              TEXT PRIMARY KEY,
    incident_id     TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    proposal_yaml   TEXT NOT NULL,
    validation_json TEXT NOT NULL DEFAULT '{}',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL DEFAULT 0,
    promoted_at     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS context_aliases (
    id         TEXT PRIMARY KEY,
    target_id  TEXT NOT NULL,
    alias      TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 1.0,
    source     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS runtime_snapshots (
    id            TEXT PRIMARY KEY,
    captured_at   INTEGER NOT NULL,
    node_id       TEXT NOT NULL DEFAULT '',
    cluster_id    TEXT NOT NULL DEFAULT '',
    snapshot_json TEXT NOT NULL,
    created_at    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_runtime_snapshots_captured ON runtime_snapshots(captured_at DESC);

CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_edges_src  ON edges(src);
CREATE INDEX IF NOT EXISTS idx_edges_dst  ON edges(dst);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_phase ON edges(phase);
CREATE INDEX IF NOT EXISTS idx_context_aliases_target ON context_aliases(target_id);

CREATE TABLE IF NOT EXISTS preflight_audits (
    id                   TEXT PRIMARY KEY,
    task                 TEXT NOT NULL DEFAULT '',
    timestamp            INTEGER NOT NULL DEFAULT 0,
    git_sha              TEXT NOT NULL DEFAULT '',
    files_json           TEXT NOT NULL DEFAULT '[]',
    forbidden_fixes_json TEXT NOT NULL DEFAULT '[]',
    invariants_json      TEXT NOT NULL DEFAULT '[]',
    code_smells_json     TEXT NOT NULL DEFAULT '[]',
    created_at           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_preflight_audits_ts  ON preflight_audits(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_preflight_audits_sha ON preflight_audits(git_sha);

CREATE TABLE IF NOT EXISTS agent_usage_events (
    id                TEXT PRIMARY KEY,
    event_time        INTEGER NOT NULL DEFAULT 0,
    agent             TEXT NOT NULL DEFAULT 'unknown',
    session_id_hash   TEXT NOT NULL DEFAULT '',
    repo              TEXT NOT NULL DEFAULT '',
    tool              TEXT NOT NULL DEFAULT '',
    operation         TEXT NOT NULL DEFAULT 'called',
    result_status     TEXT NOT NULL DEFAULT '',
    confidence        TEXT NOT NULL DEFAULT '',
    task_type         TEXT NOT NULL DEFAULT '',
    changed_files_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_agent_usage_time ON agent_usage_events(event_time DESC);
CREATE INDEX IF NOT EXISTS idx_agent_usage_tool ON agent_usage_events(tool);

CREATE TABLE IF NOT EXISTS context_reads (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    path          TEXT NOT NULL,
    fingerprint   TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    mod_time_unix INTEGER NOT NULL DEFAULT 0,
    git_commit    TEXT NOT NULL DEFAULT '',
    read_reason   TEXT NOT NULL DEFAULT '',
    read_tool     TEXT NOT NULL DEFAULT '',
    turn_index    INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_context_reads_session     ON context_reads(session_id);
CREATE INDEX IF NOT EXISTS idx_context_reads_path        ON context_reads(path);
CREATE INDEX IF NOT EXISTS idx_context_reads_fingerprint ON context_reads(fingerprint);

CREATE TABLE IF NOT EXISTS file_snapshots (
    path          TEXT PRIMARY KEY,
    fingerprint   TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    mod_time_unix INTEGER NOT NULL DEFAULT 0,
    git_commit    TEXT NOT NULL DEFAULT '',
    updated_at    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_file_snapshots_fingerprint ON file_snapshots(fingerprint);

CREATE TABLE IF NOT EXISTS stale_context_warnings (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL,
    path                TEXT NOT NULL,
    read_fingerprint    TEXT NOT NULL,
    current_fingerprint TEXT NOT NULL,
    read_turn_index     INTEGER NOT NULL DEFAULT 0,
    current_turn_index  INTEGER NOT NULL DEFAULT 0,
    severity            TEXT NOT NULL,
    message             TEXT NOT NULL,
    created_at          INTEGER NOT NULL,
    acknowledged_at     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_stale_context_session ON stale_context_warnings(session_id);
CREATE INDEX IF NOT EXISTS idx_stale_context_path    ON stale_context_warnings(path);

-- Incident Pattern Matching tables
CREATE TABLE IF NOT EXISTS incident_patterns (
    id           TEXT PRIMARY KEY,
    incident_id  TEXT NOT NULL,
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    severity     TEXT NOT NULL DEFAULT 'warning',
    status       TEXT NOT NULL DEFAULT 'active',
    failure_mode TEXT NOT NULL DEFAULT '',
    root_cause   TEXT NOT NULL DEFAULT '',
    lesson       TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_incident_patterns_incident_id  ON incident_patterns(incident_id);
CREATE INDEX IF NOT EXISTS idx_incident_patterns_failure_mode ON incident_patterns(failure_mode);
CREATE INDEX IF NOT EXISTS idx_incident_patterns_status       ON incident_patterns(status);

CREATE TABLE IF NOT EXISTS incident_pattern_files (
    pattern_id TEXT NOT NULL,
    path       TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (pattern_id, path)
);
CREATE INDEX IF NOT EXISTS idx_incident_pattern_files_path ON incident_pattern_files(path);

CREATE TABLE IF NOT EXISTS incident_pattern_symbols (
    pattern_id TEXT NOT NULL,
    symbol     TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (pattern_id, symbol)
);
CREATE INDEX IF NOT EXISTS idx_incident_pattern_symbols_symbol ON incident_pattern_symbols(symbol);

CREATE TABLE IF NOT EXISTS incident_pattern_invariants (
    pattern_id   TEXT NOT NULL,
    invariant_id TEXT NOT NULL,
    relationship TEXT NOT NULL DEFAULT 'violated',
    PRIMARY KEY (pattern_id, invariant_id)
);

CREATE TABLE IF NOT EXISTS incident_pattern_failed_fixes (
    id            TEXT PRIMARY KEY,
    pattern_id    TEXT NOT NULL,
    proposal_id   TEXT NOT NULL DEFAULT '',
    commit_hash   TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL,
    reverted      INTEGER NOT NULL DEFAULT 0,
    revert_reason TEXT NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_incident_failed_fixes_pattern ON incident_pattern_failed_fixes(pattern_id);

CREATE TABLE IF NOT EXISTS incident_pattern_edit_shapes (
    id          TEXT PRIMARY KEY,
    pattern_id  TEXT NOT NULL,
    shape_kind  TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    dangerous   INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_incident_edit_shapes_pattern ON incident_pattern_edit_shapes(pattern_id);

CREATE TABLE IF NOT EXISTS incident_pattern_proposals (
    pattern_id   TEXT NOT NULL,
    proposal_id  TEXT NOT NULL,
    relationship TEXT NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (pattern_id, proposal_id)
);

CREATE TABLE IF NOT EXISTS incident_pattern_acknowledgements (
    id                  TEXT PRIMARY KEY,
    session_id          TEXT NOT NULL,
    incident_id         TEXT NOT NULL,
    acknowledged_reason TEXT NOT NULL DEFAULT '',
    created_at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_incident_ack_session  ON incident_pattern_acknowledgements(session_id);
CREATE INDEX IF NOT EXISTS idx_incident_ack_incident ON incident_pattern_acknowledgements(incident_id);

-- Session Resumption Oracle --------------------------------------------------
CREATE TABLE IF NOT EXISTS agent_sessions (
    id                TEXT PRIMARY KEY,
    title             TEXT NOT NULL DEFAULT '',
    objective         TEXT NOT NULL DEFAULT '',
    actor             TEXT NOT NULL DEFAULT 'claude',
    status            TEXT NOT NULL,
    started_at        INTEGER NOT NULL,
    ended_at          INTEGER,
    parent_session_id TEXT,
    repo_root         TEXT NOT NULL DEFAULT '',
    branch            TEXT NOT NULL DEFAULT '',
    git_commit_start  TEXT NOT NULL DEFAULT '',
    git_commit_end    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_status  ON agent_sessions(status);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_started ON agent_sessions(started_at);

CREATE TABLE IF NOT EXISTS session_events (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL,
    turn_index   INTEGER,
    event_type   TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_events_session ON session_events(session_id);
CREATE INDEX IF NOT EXISTS idx_session_events_type    ON session_events(event_type);

CREATE TABLE IF NOT EXISTS session_file_touches (
    id                 TEXT PRIMARY KEY,
    session_id         TEXT NOT NULL,
    path               TEXT NOT NULL,
    action             TEXT NOT NULL,
    sequence           INTEGER NOT NULL,
    fingerprint_before TEXT NOT NULL DEFAULT '',
    fingerprint_after  TEXT NOT NULL DEFAULT '',
    reason             TEXT NOT NULL DEFAULT '',
    created_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_file_touches_session ON session_file_touches(session_id);
CREATE INDEX IF NOT EXISTS idx_session_file_touches_path    ON session_file_touches(path);

CREATE TABLE IF NOT EXISTS session_decisions (
    id                      TEXT PRIMARY KEY,
    session_id              TEXT NOT NULL,
    title                   TEXT NOT NULL,
    decision                TEXT NOT NULL,
    rationale               TEXT NOT NULL,
    alternatives_considered TEXT NOT NULL DEFAULT '',
    related_files           TEXT NOT NULL DEFAULT '',
    related_invariants      TEXT NOT NULL DEFAULT '',
    related_incidents       TEXT NOT NULL DEFAULT '',
    confidence              TEXT NOT NULL DEFAULT 'medium',
    created_at              INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_decisions_session ON session_decisions(session_id);

CREATE TABLE IF NOT EXISTS session_assumptions (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    assumption      TEXT NOT NULL,
    basis           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'unverified',
    validation_plan TEXT NOT NULL DEFAULT '',
    related_files   TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    resolved_at     INTEGER
);
CREATE INDEX IF NOT EXISTS idx_session_assumptions_session ON session_assumptions(session_id);
CREATE INDEX IF NOT EXISTS idx_session_assumptions_status  ON session_assumptions(status);

CREATE TABLE IF NOT EXISTS session_unfinished_work (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL,
    title             TEXT NOT NULL,
    description       TEXT NOT NULL,
    priority          TEXT NOT NULL DEFAULT 'medium',
    reason_unfinished TEXT NOT NULL DEFAULT '',
    next_action       TEXT NOT NULL DEFAULT '',
    related_files     TEXT NOT NULL DEFAULT '',
    related_tests     TEXT NOT NULL DEFAULT '',
    related_incidents TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'open',
    created_at        INTEGER NOT NULL,
    closed_at         INTEGER
);
CREATE INDEX IF NOT EXISTS idx_session_unfinished_session ON session_unfinished_work(session_id);
CREATE INDEX IF NOT EXISTS idx_session_unfinished_status  ON session_unfinished_work(status);

CREATE TABLE IF NOT EXISTS session_warnings (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL,
    warning_type     TEXT NOT NULL,
    severity         TEXT NOT NULL,
    message          TEXT NOT NULL,
    related_file     TEXT NOT NULL DEFAULT '',
    related_incident TEXT NOT NULL DEFAULT '',
    acknowledged     INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL,
    acknowledged_at  INTEGER
);
CREATE INDEX IF NOT EXISTS idx_session_warnings_session ON session_warnings(session_id);

CREATE TABLE IF NOT EXISTS session_test_results (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL,
    command        TEXT NOT NULL,
    status         TEXT NOT NULL,
    summary        TEXT NOT NULL DEFAULT '',
    output_excerpt TEXT NOT NULL DEFAULT '',
    related_files  TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_tests_session ON session_test_results(session_id);

CREATE TABLE IF NOT EXISTS session_resume_snapshots (
    id                      TEXT PRIMARY KEY,
    session_id              TEXT NOT NULL,
    summary                 TEXT NOT NULL,
    objective               TEXT NOT NULL DEFAULT '',
    files_touched_json      TEXT NOT NULL DEFAULT '',
    decisions_json          TEXT NOT NULL DEFAULT '',
    unfinished_json         TEXT NOT NULL DEFAULT '',
    warnings_json           TEXT NOT NULL DEFAULT '',
    tests_json              TEXT NOT NULL DEFAULT '',
    recommended_next_action TEXT NOT NULL DEFAULT '',
    created_at              INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_resume_session ON session_resume_snapshots(session_id);

-- Live Cluster Signal Integration --------------------------------------------
CREATE TABLE IF NOT EXISTS cluster_signal_snapshots (
    id                TEXT PRIMARY KEY,
    cluster_id        TEXT NOT NULL DEFAULT '',
    node_id           TEXT NOT NULL DEFAULT '',
    collected_at      INTEGER NOT NULL,
    collector_version TEXT NOT NULL DEFAULT '1',
    status            TEXT NOT NULL,
    summary           TEXT NOT NULL DEFAULT '',
    payload_json      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_cluster_signal_cluster    ON cluster_signal_snapshots(cluster_id);
CREATE INDEX IF NOT EXISTS idx_cluster_signal_collected  ON cluster_signal_snapshots(collected_at);

CREATE TABLE IF NOT EXISTS service_live_states (
    id                   TEXT PRIMARY KEY,
    snapshot_id          TEXT NOT NULL,
    service_name         TEXT NOT NULL,
    component            TEXT NOT NULL DEFAULT '',
    node_id              TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL,
    health               TEXT NOT NULL,
    heartbeat_age_seconds INTEGER,
    readiness            TEXT NOT NULL DEFAULT '',
    dependency_state     TEXT NOT NULL DEFAULT '',
    last_error           TEXT NOT NULL DEFAULT '',
    updated_at           INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_service_live_snapshot ON service_live_states(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_service_live_service  ON service_live_states(service_name);
CREATE INDEX IF NOT EXISTS idx_service_live_health   ON service_live_states(health);

CREATE TABLE IF NOT EXISTS recent_error_signatures (
    id                TEXT PRIMARY KEY,
    snapshot_id       TEXT NOT NULL,
    service_name      TEXT NOT NULL DEFAULT '',
    component         TEXT NOT NULL DEFAULT '',
    node_id           TEXT NOT NULL DEFAULT '',
    signature         TEXT NOT NULL,
    severity          TEXT NOT NULL,
    count             INTEGER NOT NULL DEFAULT 1,
    first_seen        INTEGER,
    last_seen         INTEGER,
    sample            TEXT NOT NULL DEFAULT '',
    related_files     TEXT NOT NULL DEFAULT '',
    related_invariants TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_recent_error_snapshot   ON recent_error_signatures(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_recent_error_service    ON recent_error_signatures(service_name);
CREATE INDEX IF NOT EXISTS idx_recent_error_signature  ON recent_error_signatures(signature);

CREATE TABLE IF NOT EXISTS runtime_convergence_states (
    id                 TEXT PRIMARY KEY,
    snapshot_id        TEXT NOT NULL,
    component          TEXT NOT NULL,
    desired_state      TEXT NOT NULL DEFAULT '',
    installed_state    TEXT NOT NULL DEFAULT '',
    runtime_state      TEXT NOT NULL DEFAULT '',
    convergence_status TEXT NOT NULL,
    blocked_reason     TEXT NOT NULL DEFAULT '',
    retry_count        INTEGER NOT NULL DEFAULT 0,
    age_seconds        INTEGER NOT NULL DEFAULT 0,
    related_key        TEXT NOT NULL DEFAULT '',
    updated_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_convergence_snapshot   ON runtime_convergence_states(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_convergence_component  ON runtime_convergence_states(component);
CREATE INDEX IF NOT EXISTS idx_convergence_status     ON runtime_convergence_states(convergence_status);

CREATE TABLE IF NOT EXISTS active_cluster_incidents (
    id           TEXT PRIMARY KEY,
    snapshot_id  TEXT NOT NULL,
    incident_id  TEXT NOT NULL DEFAULT '',
    source       TEXT NOT NULL,
    title        TEXT NOT NULL,
    severity     TEXT NOT NULL,
    status       TEXT NOT NULL,
    component    TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    node_id      TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    started_at   INTEGER,
    updated_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_active_incidents_snapshot  ON active_cluster_incidents(snapshot_id);
CREATE INDEX IF NOT EXISTS idx_active_incidents_component ON active_cluster_incidents(component);
CREATE INDEX IF NOT EXISTS idx_active_incidents_service   ON active_cluster_incidents(service_name);

CREATE TABLE IF NOT EXISTS live_preflight_results (
    id                 TEXT PRIMARY KEY,
    session_id         TEXT NOT NULL DEFAULT '',
    task               TEXT NOT NULL DEFAULT '',
    files_json         TEXT NOT NULL DEFAULT '',
    components_json    TEXT NOT NULL DEFAULT '',
    static_result_id   TEXT NOT NULL DEFAULT '',
    signal_snapshot_id TEXT NOT NULL DEFAULT '',
    verdict            TEXT NOT NULL,
    severity           TEXT NOT NULL,
    summary            TEXT NOT NULL,
    blockers_json      TEXT NOT NULL DEFAULT '',
    warnings_json      TEXT NOT NULL DEFAULT '',
    confirmations_json TEXT NOT NULL DEFAULT '',
    created_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_live_preflight_session ON live_preflight_results(session_id);
CREATE INDEX IF NOT EXISTS idx_live_preflight_verdict ON live_preflight_results(verdict);

CREATE TABLE IF NOT EXISTS semantic_diff_reports (
    id               TEXT PRIMARY KEY,
    session_id       TEXT,
    diff_source      TEXT NOT NULL,
    git_base         TEXT,
    git_head         TEXT,
    task             TEXT,
    verdict          TEXT NOT NULL,
    severity         TEXT NOT NULL,
    summary          TEXT NOT NULL,
    diff_fingerprint TEXT,
    created_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_diff_session ON semantic_diff_reports(session_id);
CREATE INDEX IF NOT EXISTS idx_semantic_diff_verdict ON semantic_diff_reports(verdict);

CREATE TABLE IF NOT EXISTS semantic_diff_findings (
    id             TEXT PRIMARY KEY,
    report_id      TEXT NOT NULL,
    kind           TEXT NOT NULL,
    severity       TEXT NOT NULL,
    file_path      TEXT,
    symbol         TEXT,
    layer_from     TEXT,
    layer_to       TEXT,
    authority_from TEXT,
    authority_to   TEXT,
    invariant_id   TEXT,
    message        TEXT NOT NULL,
    evidence       TEXT,
    recommendation TEXT,
    created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_findings_report     ON semantic_diff_findings(report_id);
CREATE INDEX IF NOT EXISTS idx_semantic_findings_kind       ON semantic_diff_findings(kind);
CREATE INDEX IF NOT EXISTS idx_semantic_findings_file       ON semantic_diff_findings(file_path);
CREATE INDEX IF NOT EXISTS idx_semantic_findings_invariant  ON semantic_diff_findings(invariant_id);

CREATE TABLE IF NOT EXISTS semantic_diff_atoms (
    id             TEXT PRIMARY KEY,
    report_id      TEXT NOT NULL,
    file_path      TEXT NOT NULL,
    symbol         TEXT,
    atom_kind      TEXT NOT NULL,
    before_summary TEXT,
    after_summary  TEXT,
    confidence     TEXT NOT NULL,
    evidence       TEXT
);
CREATE INDEX IF NOT EXISTS idx_semantic_atoms_report ON semantic_diff_atoms(report_id);
CREATE INDEX IF NOT EXISTS idx_semantic_atoms_kind   ON semantic_diff_atoms(atom_kind);

CREATE TABLE IF NOT EXISTS semantic_layer_transitions (
    id              TEXT PRIMARY KEY,
    report_id       TEXT NOT NULL,
    file_path       TEXT,
    symbol          TEXT,
    layer_from      TEXT,
    layer_to        TEXT,
    transition_kind TEXT NOT NULL,
    allowed         INTEGER NOT NULL,
    reason          TEXT
);
CREATE INDEX IF NOT EXISTS idx_layer_transitions_report ON semantic_layer_transitions(report_id);

CREATE TABLE IF NOT EXISTS coordination_runs (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    objective        TEXT NOT NULL,
    status           TEXT NOT NULL,
    owner_agent_id   TEXT,
    repo_root        TEXT,
    branch           TEXT,
    git_commit_start TEXT,
    git_commit_end   TEXT,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    closed_at        INTEGER
);
CREATE INDEX IF NOT EXISTS idx_coordination_runs_status ON coordination_runs(status);
CREATE INDEX IF NOT EXISTS idx_coordination_runs_repo   ON coordination_runs(repo_root);

CREATE TABLE IF NOT EXISTS agent_participants (
    id           TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL,
    agent_name   TEXT NOT NULL,
    agent_kind   TEXT NOT NULL,
    session_id   TEXT,
    role         TEXT,
    status       TEXT NOT NULL,
    heartbeat_at INTEGER,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_participants_run    ON agent_participants(run_id);
CREATE INDEX IF NOT EXISTS idx_agent_participants_status ON agent_participants(status);

CREATE TABLE IF NOT EXISTS coordination_work_items (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL,
    title               TEXT NOT NULL,
    description         TEXT,
    status              TEXT NOT NULL,
    priority            TEXT NOT NULL,
    assigned_agent_id   TEXT,
    claimed_by_agent_id TEXT,
    related_files       TEXT,
    related_components  TEXT,
    related_invariants  TEXT,
    related_incidents   TEXT,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    closed_at           INTEGER
);
CREATE INDEX IF NOT EXISTS idx_coordination_work_run    ON coordination_work_items(run_id);
CREATE INDEX IF NOT EXISTS idx_coordination_work_status ON coordination_work_items(status);
CREATE INDEX IF NOT EXISTS idx_coordination_work_agent  ON coordination_work_items(assigned_agent_id);

CREATE TABLE IF NOT EXISTS coordination_file_claims (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    agent_id    TEXT NOT NULL,
    path        TEXT NOT NULL,
    claim_kind  TEXT NOT NULL,
    reason      TEXT,
    status      TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER,
    released_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_file_claims_run   ON coordination_file_claims(run_id);
CREATE INDEX IF NOT EXISTS idx_file_claims_path  ON coordination_file_claims(path);
CREATE INDEX IF NOT EXISTS idx_file_claims_agent ON coordination_file_claims(agent_id);

CREATE TABLE IF NOT EXISTS coordination_file_locks (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL,
    agent_id            TEXT NOT NULL,
    path                TEXT NOT NULL,
    lock_kind           TEXT NOT NULL,
    reason              TEXT NOT NULL,
    fingerprint_at_lock TEXT,
    status              TEXT NOT NULL,
    created_at          INTEGER NOT NULL,
    expires_at          INTEGER NOT NULL,
    released_at         INTEGER
);
CREATE INDEX IF NOT EXISTS idx_file_locks_run   ON coordination_file_locks(run_id, path);
CREATE INDEX IF NOT EXISTS idx_file_locks_agent ON coordination_file_locks(agent_id);
CREATE INDEX IF NOT EXISTS idx_file_locks_path  ON coordination_file_locks(path);

CREATE TABLE IF NOT EXISTS coordination_decisions (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL,
    agent_id           TEXT NOT NULL,
    title              TEXT NOT NULL,
    decision           TEXT NOT NULL,
    rationale          TEXT NOT NULL,
    scope              TEXT NOT NULL,
    related_files      TEXT,
    related_components TEXT,
    related_invariants TEXT,
    related_incidents  TEXT,
    binding            INTEGER NOT NULL DEFAULT 0,
    superseded_by      TEXT,
    created_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_coord_decisions_run   ON coordination_decisions(run_id);
CREATE INDEX IF NOT EXISTS idx_coord_decisions_scope ON coordination_decisions(scope);

CREATE TABLE IF NOT EXISTS coordination_assumptions (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    assumption      TEXT NOT NULL,
    basis           TEXT,
    status          TEXT NOT NULL,
    validation_plan TEXT,
    related_files   TEXT,
    created_at      INTEGER NOT NULL,
    resolved_at     INTEGER
);
CREATE INDEX IF NOT EXISTS idx_coord_assumptions_run    ON coordination_assumptions(run_id);
CREATE INDEX IF NOT EXISTS idx_coord_assumptions_status ON coordination_assumptions(status);

CREATE TABLE IF NOT EXISTS coordination_warnings (
    id                 TEXT PRIMARY KEY,
    run_id             TEXT NOT NULL,
    agent_id           TEXT,
    warning_type       TEXT NOT NULL,
    severity           TEXT NOT NULL,
    message            TEXT NOT NULL,
    related_file       TEXT,
    related_component  TEXT,
    related_incident   TEXT,
    status             TEXT NOT NULL,
    created_at         INTEGER NOT NULL,
    acknowledged_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_coord_warnings_run    ON coordination_warnings(run_id);
CREATE INDEX IF NOT EXISTS idx_coord_warnings_file   ON coordination_warnings(related_file);
CREATE INDEX IF NOT EXISTS idx_coord_warnings_status ON coordination_warnings(status);

CREATE TABLE IF NOT EXISTS coordination_handoff_notes (
    id            TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL,
    from_agent_id TEXT NOT NULL,
    to_agent_id   TEXT,
    work_item_id  TEXT,
    title         TEXT NOT NULL,
    body          TEXT NOT NULL,
    related_files TEXT,
    created_at    INTEGER NOT NULL,
    read_at       INTEGER
);
CREATE INDEX IF NOT EXISTS idx_handoff_run      ON coordination_handoff_notes(run_id);
CREATE INDEX IF NOT EXISTS idx_handoff_to_agent ON coordination_handoff_notes(to_agent_id);

CREATE TABLE IF NOT EXISTS coordination_conflicts (
    id            TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL,
    conflict_type TEXT NOT NULL,
    severity      TEXT NOT NULL,
    agent_a       TEXT,
    agent_b       TEXT,
    path          TEXT,
    symbol        TEXT,
    message       TEXT NOT NULL,
    resolution    TEXT,
    status        TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    resolved_at   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_coord_conflicts_run    ON coordination_conflicts(run_id);
CREATE INDEX IF NOT EXISTS idx_coord_conflicts_status ON coordination_conflicts(status);

-- Failure Knowledge Graph tables (Phase 13)

CREATE TABLE IF NOT EXISTS failure_nodes (
    id            TEXT PRIMARY KEY,
    node_type     TEXT NOT NULL,
    name          TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_failure_nodes_type   ON failure_nodes(node_type);
CREATE INDEX IF NOT EXISTS idx_failure_nodes_name   ON failure_nodes(name);
CREATE INDEX IF NOT EXISTS idx_failure_nodes_status ON failure_nodes(status);

CREATE TABLE IF NOT EXISTS failure_edges (
    id         TEXT PRIMARY KEY,
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    edge_type  TEXT NOT NULL,
    confidence TEXT NOT NULL DEFAULT '',
    evidence   TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_failure_edges_from ON failure_edges(from_id);
CREATE INDEX IF NOT EXISTS idx_failure_edges_to   ON failure_edges(to_id);
CREATE INDEX IF NOT EXISTS idx_failure_edges_type ON failure_edges(edge_type);

CREATE TABLE IF NOT EXISTS failure_error_signatures (
    id                   TEXT PRIMARY KEY,
    signature            TEXT NOT NULL,
    normalized_signature TEXT NOT NULL,
    category_id          TEXT NOT NULL DEFAULT '',
    severity             TEXT NOT NULL DEFAULT '',
    sample               TEXT NOT NULL DEFAULT '',
    matcher_kind         TEXT NOT NULL DEFAULT 'exact',
    matcher_pattern      TEXT NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL DEFAULT 0,
    updated_at           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_failure_sig_normalized ON failure_error_signatures(normalized_signature);
CREATE INDEX IF NOT EXISTS idx_failure_sig_category   ON failure_error_signatures(category_id);

CREATE TABLE IF NOT EXISTS failure_resolution_recipes (
    id                  TEXT PRIMARY KEY,
    resolution_id       TEXT NOT NULL,
    title               TEXT NOT NULL,
    steps_json          TEXT NOT NULL DEFAULT '[]',
    forbidden_steps_json TEXT NOT NULL DEFAULT '[]',
    verification_json   TEXT NOT NULL DEFAULT '[]',
    created_at          INTEGER NOT NULL DEFAULT 0,
    updated_at          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_resolution_recipes_resolution ON failure_resolution_recipes(resolution_id);

CREATE TABLE IF NOT EXISTS workflow_failure_modes (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    summary         TEXT NOT NULL DEFAULT '',
    workflow_stage  TEXT NOT NULL DEFAULT '',
    failure_phase   TEXT NOT NULL DEFAULT '',
    retry_semantics TEXT NOT NULL DEFAULT '',
    closure_rule    TEXT NOT NULL DEFAULT '',
    metadata_json   TEXT NOT NULL DEFAULT '{}',
    created_at      INTEGER NOT NULL DEFAULT 0,
    updated_at      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workflow_failure_modes_name  ON workflow_failure_modes(name);
CREATE INDEX IF NOT EXISTS idx_workflow_failure_modes_stage ON workflow_failure_modes(workflow_stage);

CREATE TABLE IF NOT EXISTS failure_observations (
    id                   TEXT PRIMARY KEY,
    session_id           TEXT NOT NULL DEFAULT '',
    incident_id          TEXT NOT NULL DEFAULT '',
    run_id               TEXT NOT NULL DEFAULT '',
    source               TEXT NOT NULL DEFAULT '',
    raw_error            TEXT NOT NULL DEFAULT '',
    normalized_signature TEXT NOT NULL DEFAULT '',
    matched_signature_id TEXT NOT NULL DEFAULT '',
    matched_category_id  TEXT NOT NULL DEFAULT '',
    component            TEXT NOT NULL DEFAULT '',
    service_name         TEXT NOT NULL DEFAULT '',
    file_path            TEXT NOT NULL DEFAULT '',
    symbol               TEXT NOT NULL DEFAULT '',
    confidence           TEXT NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_failure_obs_session  ON failure_observations(session_id);
CREATE INDEX IF NOT EXISTS idx_failure_obs_category ON failure_observations(matched_category_id);
CREATE INDEX IF NOT EXISTS idx_failure_obs_component ON failure_observations(component);

CREATE TABLE IF NOT EXISTS failure_learning_proposals (
    id                  TEXT PRIMARY KEY,
    source_type         TEXT NOT NULL,
    source_id           TEXT NOT NULL,
    proposal_kind       TEXT NOT NULL,
    status              TEXT NOT NULL,
    target_category_id  TEXT,
    proposed_category_id TEXT,
    title               TEXT NOT NULL,
    summary             TEXT NOT NULL,
    confidence          TEXT NOT NULL,
    rationale           TEXT,
    extracted_json      TEXT NOT NULL DEFAULT '{}',
    patch_json          TEXT NOT NULL DEFAULT '{}',
    created_by          TEXT,
    reviewed_by         TEXT,
    created_at          INTEGER NOT NULL,
    reviewed_at         INTEGER,
    applied_at          INTEGER
);
CREATE INDEX IF NOT EXISTS idx_failure_learning_source ON failure_learning_proposals(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_failure_learning_status ON failure_learning_proposals(status);
CREATE INDEX IF NOT EXISTS idx_failure_learning_target ON failure_learning_proposals(target_category_id);

CREATE TABLE IF NOT EXISTS failure_learning_reviews (
    id               TEXT PRIMARY KEY,
    proposal_id      TEXT NOT NULL,
    reviewer         TEXT NOT NULL,
    decision         TEXT NOT NULL,
    notes            TEXT,
    edited_patch_json TEXT,
    created_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_failure_learning_reviews_proposal ON failure_learning_reviews(proposal_id);

CREATE TABLE IF NOT EXISTS failure_seed_sync (
    id           TEXT PRIMARY KEY,
    proposal_id  TEXT NOT NULL,
    seed_path    TEXT NOT NULL,
    status       TEXT NOT NULL,
    content_hash TEXT,
    message      TEXT,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_failure_seed_sync_proposal ON failure_seed_sync(proposal_id);

CREATE TABLE IF NOT EXISTS experience_entries (
    id                TEXT PRIMARY KEY,
    kind              TEXT NOT NULL DEFAULT '',
    domain            TEXT NOT NULL DEFAULT '',
    capability        TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT '',
    summary           TEXT NOT NULL DEFAULT '',
    goal_original     TEXT NOT NULL DEFAULT '',
    goal_normalized   TEXT NOT NULL DEFAULT '',
    goal_verb         TEXT NOT NULL DEFAULT '',
    goal_object       TEXT NOT NULL DEFAULT '',
    strategy_id       TEXT NOT NULL DEFAULT '',
    lesson            TEXT NOT NULL DEFAULT '',
    next_time_hint    TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    reviewed_by       TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_experience_entries_domain ON experience_entries(domain);
CREATE INDEX IF NOT EXISTS idx_experience_entries_capability ON experience_entries(capability);
CREATE INDEX IF NOT EXISTS idx_experience_entries_status ON experience_entries(status);

CREATE TABLE IF NOT EXISTS experience_attempts (
    id             TEXT PRIMARY KEY,
    experience_id  TEXT NOT NULL,
    strategy_id    TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL DEFAULT '',
    rationale      TEXT NOT NULL DEFAULT '',
    outcome        TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_experience_attempts_exp ON experience_attempts(experience_id);

CREATE TABLE IF NOT EXISTS experience_observations (
    id             TEXT PRIMARY KEY,
    experience_id  TEXT NOT NULL,
    attempt_id     TEXT NOT NULL DEFAULT '',
    type           TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT '',
    confidence     REAL NOT NULL DEFAULT 0.0,
    created_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_experience_observations_exp ON experience_observations(experience_id);
`

// Graph is the central awareness graph handle backed by SQLite.
type Graph struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite awareness graph at path.
// The parent directory is created if it does not exist.
func Open(path string) (*Graph, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("awareness graph: mkdir %s: %w", filepath.Dir(path), err)
	}
	dsn := path + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("awareness graph: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL allows one writer
	g := &Graph{db: db}
	if err := g.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return g, nil
}

// OpenReadOnly opens an existing SQLite awareness graph at path for read-only
// access. It does NOT create the parent directory, does NOT migrate the
// schema, and the underlying connection refuses every write. This is the
// correct way to consume a signed, content-addressed bundle (e.g.
// /var/lib/globular/awareness/current/graph.db) whose contract is "installed
// once by root, never modified at runtime."
//
// The mode=ro&immutable=1 pragma tells SQLite the file will not change while
// open. This skips WAL setup entirely — Open's WAL pragma would otherwise
// require write access to create -wal and -shm sidecars. Combined with
// skipping migrate(), the read path is fully side-effect-free even when the
// caller has no write permission on the bundle directory.
//
// Callers that need to write to the graph (learn_from_fix, experience,
// session state) must use Open against a writable path instead. The MCP
// detects bundle paths and routes them here; runtime writes go to a
// separate writable database — see docs/awareness/composed_path_failures.md.
func OpenReadOnly(path string) (*Graph, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("awareness graph (read-only): stat %s: %w", path, err)
	}
	// SQLite URI form is required to honour mode=ro and immutable=1 as URI
	// parameters (the bare query-string form is interpreted as part of the
	// filename by the mattn/go-sqlite3 driver). Pragma-style options keep
	// their underscore prefix.
	dsn := "file:" + path + "?mode=ro&immutable=1&_foreign_keys=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("awareness graph (read-only): open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	// Validate the schema is accessible by running a no-op SELECT. We do not
	// run migrate() — a read-only handle must not write DDL. A read-only
	// open of a bundle whose schema is older than this binary expects will
	// surface as "no such column" on the first query that needs it, which
	// is the right place to detect drift: in a query that names the column.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("awareness graph (read-only): ping %s: %w", path, err)
	}
	return &Graph{db: db}, nil
}

// OpenMemory opens a fresh in-memory SQLite awareness graph.
// It is suitable for validation previews: changes are never persisted.
// Each call returns an independent, isolated in-memory database.
func OpenMemory() (*Graph, error) {
	// Each unique name gives an isolated in-memory DB within the process.
	// Using ?cache=shared with a unique name avoids cross-connection leakage.
	db, err := sql.Open("sqlite3", "file::memory:?mode=memory")
	if err != nil {
		return nil, fmt.Errorf("awareness graph (memory): open: %w", err)
	}
	db.SetMaxOpenConns(1)
	g := &Graph{db: db}
	if err := g.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return g, nil
}

// Close closes the underlying database.
func (g *Graph) Close() error { return g.db.Close() }

// DB returns the raw *sql.DB (for tests and bulk operations).
func (g *Graph) DB() *sql.DB { return g.db }

// migrate applies the schema DDL. Safe to call on an existing database.
func (g *Graph) migrate(_ context.Context) error {
	if _, err := g.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("awareness graph: migrate: %w", err)
	}
	if err := g.addMigrations(); err != nil {
		return fmt.Errorf("awareness graph: addMigrations: %w", err)
	}
	return nil
}

// addMigrations applies incremental schema changes to existing databases.
// Each statement is idempotent: "duplicate column name" errors are silently ignored.
func (g *Graph) addMigrations() error {
	migrations := []string{
		// Phase 11: edge provenance for trust tracking.
		`ALTER TABLE edges ADD COLUMN provenance_json TEXT NOT NULL DEFAULT '{}'`,
		// Phase 12: decision/information edge separation.
		// edge_class distinguishes causal/decision edges from contextual/information edges.
		// weight (0.0–1.0) controls traversal ranking: decision=1.0, structural=0.7, info=0.3.
		`ALTER TABLE edges ADD COLUMN edge_class TEXT NOT NULL DEFAULT 'information'`,
		`ALTER TABLE edges ADD COLUMN weight REAL NOT NULL DEFAULT 0.5`,
		// Phase 12: collector health tracking in graph_builds stats.
		`ALTER TABLE graph_builds ADD COLUMN collector_health_json TEXT NOT NULL DEFAULT '[]'`,
	}
	for _, m := range migrations {
		if _, err := g.db.Exec(m); err != nil {
			if isDuplicateColumnError(err) {
				continue // column already exists — idempotent
			}
			return fmt.Errorf("migration %q: %w", m, err)
		}
	}
	return nil
}

// isDuplicateColumnError returns true when SQLite rejects an ALTER TABLE ADD COLUMN
// because the column already exists (error message contains "duplicate column name").
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name") || strings.Contains(msg, "already exists")
}

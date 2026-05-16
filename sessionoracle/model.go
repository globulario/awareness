package sessionoracle

// AgentSession is a single work session tracked by the oracle.
type AgentSession struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Objective       string `json:"objective"`
	Actor           string `json:"actor"`
	Status          string `json:"status"` // open | closed
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	RepoRoot        string `json:"repo_root"`
	Branch          string `json:"branch"`
	GitCommitStart  string `json:"git_commit_start"`
	GitCommitEnd    string `json:"git_commit_end"`
}

// SessionEvent is a raw event emitted during a session.
type SessionEvent struct {
	ID          string      `json:"id"`
	SessionID   string      `json:"session_id"`
	TurnIndex   int         `json:"turn_index"`
	EventType   string      `json:"event_type"`
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	PayloadJSON string      `json:"payload_json,omitempty"`
	Payload     interface{} `json:"-"`
	CreatedAt   int64       `json:"created_at"`
}

// SessionFileTouch records when and how a file was accessed.
type SessionFileTouch struct {
	ID                string `json:"id"`
	SessionID         string `json:"session_id"`
	Path              string `json:"path"`
	Action            string `json:"action"` // read | edit | create | delete | rename | test | inspect
	Sequence          int    `json:"sequence"`
	FingerprintBefore string `json:"fingerprint_before,omitempty"`
	FingerprintAfter  string `json:"fingerprint_after,omitempty"`
	Reason            string `json:"reason,omitempty"`
	CreatedAt         int64  `json:"created_at"`
}

// SessionDecision records an architectural or engineering decision made during a session.
type SessionDecision struct {
	ID                     string   `json:"id"`
	SessionID              string   `json:"session_id"`
	Title                  string   `json:"title"`
	Decision               string   `json:"decision"`
	Rationale              string   `json:"rationale"`
	AlternativesConsidered []string `json:"alternatives_considered,omitempty"`
	RelatedFiles           []string `json:"related_files,omitempty"`
	RelatedInvariants      []string `json:"related_invariants,omitempty"`
	RelatedIncidents       []string `json:"related_incidents,omitempty"`
	Confidence             string   `json:"confidence"` // high | medium | low
	CreatedAt              int64    `json:"created_at"`
}

// SessionAssumption records an assumption made during a session, and its validation status.
type SessionAssumption struct {
	ID             string `json:"id"`
	SessionID      string `json:"session_id"`
	Assumption     string `json:"assumption"`
	Basis          string `json:"basis,omitempty"`
	Status         string `json:"status"` // unverified | verified | falsified | superseded
	ValidationPlan string `json:"validation_plan,omitempty"`
	RelatedFiles   string `json:"related_files,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	ResolvedAt     int64  `json:"resolved_at,omitempty"`
}

// SessionUnfinishedWork records a task that was not completed during a session.
type SessionUnfinishedWork struct {
	ID               string   `json:"id"`
	SessionID        string   `json:"session_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Priority         string   `json:"priority"` // critical | high | medium | low
	ReasonUnfinished string   `json:"reason_unfinished,omitempty"`
	NextAction       string   `json:"next_action,omitempty"`
	RelatedFiles     []string `json:"related_files,omitempty"`
	RelatedTests     []string `json:"related_tests,omitempty"`
	RelatedIncidents []string `json:"related_incidents,omitempty"`
	Status           string   `json:"status"` // open | in_progress | blocked | closed | superseded
	CreatedAt        int64    `json:"created_at"`
	ClosedAt         int64    `json:"closed_at,omitempty"`
}

// SessionWarning records an active warning during a session (stale context, incident pattern, etc).
type SessionWarning struct {
	ID              string `json:"id"`
	SessionID       string `json:"session_id"`
	WarningType     string `json:"warning_type"` // stale_context | incident_pattern | architecture | custom
	Severity        string `json:"severity"`     // info | warning | critical
	Message         string `json:"message"`
	RelatedFile     string `json:"related_file,omitempty"`
	RelatedIncident string `json:"related_incident,omitempty"`
	Acknowledged    bool   `json:"acknowledged"`
	CreatedAt       int64  `json:"created_at"`
	AcknowledgedAt  int64  `json:"acknowledged_at,omitempty"`
}

// SessionTestResult records the result of running tests during a session.
type SessionTestResult struct {
	ID            string   `json:"id"`
	SessionID     string   `json:"session_id"`
	Command       string   `json:"command"`
	Status        string   `json:"status"` // passed | failed | skipped | error
	Summary       string   `json:"summary,omitempty"`
	OutputExcerpt string   `json:"output_excerpt,omitempty"`
	RelatedFiles  []string `json:"related_files,omitempty"`
	CreatedAt     int64    `json:"created_at"`
}

// SessionResumeSnapshot is the structured oracle output for session resumption.
type SessionResumeSnapshot struct {
	ID                    string                  `json:"id"`
	SessionID             string                  `json:"session_id"`
	Summary               string                  `json:"summary"`
	Objective             string                  `json:"objective"`
	FilesTouched          []SessionFileTouch      `json:"files_touched"`
	Decisions             []SessionDecision       `json:"decisions"`
	Assumptions           []SessionAssumption     `json:"assumptions"`
	Unfinished            []SessionUnfinishedWork `json:"unfinished"`
	Warnings              []SessionWarning        `json:"warnings"`
	Tests                 []SessionTestResult     `json:"tests"`
	RecommendedNextAction string                  `json:"recommended_next_action"`
	CreatedAt             int64                   `json:"created_at"`
}

// StartSessionRequest carries arguments to StartSession.
type StartSessionRequest struct {
	ID              string
	Title           string
	Objective       string
	Actor           string
	RepoRoot        string
	ParentSessionID string
}

// RecordDecisionRequest carries arguments to RecordDecision.
type RecordDecisionRequest struct {
	SessionID              string
	Title                  string
	Decision               string
	Rationale              string
	AlternativesConsidered []string
	RelatedFiles           []string
	RelatedInvariants      []string
	RelatedIncidents       []string
	Confidence             string
}

// RecordAssumptionRequest carries arguments to RecordAssumption.
type RecordAssumptionRequest struct {
	SessionID      string
	Assumption     string
	Basis          string
	ValidationPlan string
	RelatedFiles   string
}

// RecordUnfinishedWorkRequest carries arguments to RecordUnfinishedWork.
type RecordUnfinishedWorkRequest struct {
	SessionID        string
	Title            string
	Description      string
	Priority         string
	ReasonUnfinished string
	NextAction       string
	RelatedFiles     []string
	RelatedTests     []string
	RelatedIncidents []string
}

// RecordSessionWarningRequest carries arguments to RecordSessionWarning.
type RecordSessionWarningRequest struct {
	SessionID       string
	WarningType     string
	Severity        string
	Message         string
	RelatedFile     string
	RelatedIncident string
}

// RecordTestResultRequest carries arguments to RecordTestResult.
type RecordTestResultRequest struct {
	SessionID     string
	Command       string
	Status        string
	Summary       string
	OutputExcerpt string
	RelatedFiles  []string
}

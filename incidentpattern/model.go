// Package incidentpattern implements Incident Pattern Matching for Globular Awareness.
//
// It turns the incident/proposal archive into a proactive oracle: before an agent
// edits a file, awareness checks whether the current task resembles a past incident,
// failed proposal, reverted fix, or known architectural trap — then surfaces an
// explainable, scored warning.
//
// Design rule: awareness warns, Claude reasons, tests prove.
package incidentpattern

// IncidentPattern is a reusable failure signature extracted from a real incident.
type IncidentPattern struct {
	ID          string
	IncidentID  string
	Title       string
	Summary     string
	Severity    string // critical | warning | info
	Status      string // active | archived
	FailureMode string
	RootCause   string
	Lesson      string
	CreatedAt   int64
	UpdatedAt   int64

	// Populated by LoadPattern / ListPatterns.
	Files       []PatternFile
	Symbols     []PatternSymbol
	Invariants  []PatternInvariant
	FailedFixes []FailedFix
	EditShapes  []EditShape
	Proposals   []PatternProposal
}

// PatternFile links a file path to a pattern with an optional role description.
type PatternFile struct {
	PatternID string
	Path      string
	Role      string
}

// PatternSymbol links a Go/proto symbol to a pattern.
type PatternSymbol struct {
	PatternID string
	Symbol    string
	Role      string
}

// PatternInvariant links an invariant to a pattern and describes the relationship.
type PatternInvariant struct {
	PatternID    string
	InvariantID  string
	Relationship string // violated | protects | related
}

// FailedFix records a past fix attempt that failed or was reverted.
type FailedFix struct {
	ID           string
	PatternID    string
	ProposalID   string
	CommitHash   string
	Description  string
	Reverted     bool
	RevertReason string
	CreatedAt    int64
}

// EditShape captures an architectural movement pattern — the shape of a dangerous
// edit, not its exact diff. The same bug can reappear with different code but the
// same shape (e.g. split_authoritative_state_transition).
type EditShape struct {
	ID          string
	PatternID   string
	ShapeKind   string
	Description string
	Dangerous   bool
}

// PatternProposal links a rejected/reverted/unsafe proposal to a pattern.
type PatternProposal struct {
	PatternID    string
	ProposalID   string
	Relationship string // rejected | reverted | superseded | unsafe
	Reason       string
}

// IncidentMatchRequest is the input to the pattern matcher.
// Only SessionID, Task, and at least one of Files/Invariants/ProposedShape are required.
// More signals = more accurate matching.
type IncidentMatchRequest struct {
	SessionID     string
	Task          string
	Intent        string   // edit | review | diagnose
	Files         []string
	Symbols       []string
	Components    []string
	Invariants    []string
	ProposedShape []string
	DiffPreview   string
}

// IncidentPatternMatch is one matching result with its full explanation.
type IncidentPatternMatch struct {
	PatternID      string
	IncidentID     string
	Title          string
	Severity       string
	Score          float64
	Confidence     string // high | medium | low
	Block          bool
	MatchedSignals []MatchedSignal
	FailedFixes    []FailedFix
	Lesson         string
	Warning        string
	RecommendedNext []string
}

// MatchedSignal explains why a specific pattern matched.
type MatchedSignal struct {
	Kind        string  // file | symbol | component | invariant | shape | failure_mode | task_text | reverted_fix
	Value       string
	Weight      float64
	Explanation string
}

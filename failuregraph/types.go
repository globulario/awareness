// Package failuregraph implements the Failure Knowledge Graph for Globular Awareness.
//
// It stores typed failure causality chains — error signature → category → cause →
// resolution → wrong fix → regression test → invariant — so that when an agent
// encounters an error it does not start from zero.
//
// Design rule: boring, explainable, and useful in under one second.
// No ML classifier. No black-box inference. Only reusable, proven failure knowledge.
package failuregraph

// Node type identifiers.
const (
	NodeTypeErrorCategory  = "ErrorCategory"
	NodeTypeSymptom        = "Symptom"
	NodeTypeRootCause      = "RootCause"
	NodeTypeResolution     = "Resolution"
	NodeTypeWrongFix       = "WrongFix"
	NodeTypeRegressionTest = "RegressionTest"
	NodeTypeInvariant      = "Invariant"
	NodeTypeIncidentExample = "IncidentExample"
	NodeTypeSemanticAtom   = "SemanticAtom"
	NodeTypeRuntimeSignal  = "RuntimeSignal"
	NodeTypeWorkflowMode   = "WorkflowFailureMode"
)

// Edge type identifiers.
const (
	EdgeBelongsTo        = "belongs_to"
	EdgeObservedAs       = "observed_as"
	EdgeCommonlyCausedBy = "commonly_caused_by"
	EdgeFixedBy          = "fixed_by"
	EdgeViolates         = "violates"
	EdgeRequiresTest     = "requires_test"
	EdgeReintroduces     = "reintroduces"
	EdgeAvoidFix         = "avoid_fix"
	EdgeClosureRequires  = "closure_requires"
	EdgeIndicates        = "indicates"
	EdgeObservedIn       = "observed_in"
	EdgeReinforces       = "reinforces"
	EdgeCanFailAs        = "can_fail_as"
)

// Confidence tier constants.
const (
	ConfidenceHigh   = "high"   // score 0.80–1.00
	ConfidenceMedium = "medium" // score 0.55–0.79
	ConfidenceLow    = "low"    // score 0.30–0.54
	ConfidenceNone   = "none"   // below 0.30
)

// Status values for nodes.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// MatcherKind values.
const (
	MatcherKindExact  = "exact"
	MatcherKindRegex  = "regex"
	MatcherKindKeyword = "keyword"
)

// FailureNode is a typed vertex in the failure knowledge graph.
type FailureNode struct {
	ID        string
	NodeType  string
	Name      string
	Summary   string
	Severity  string // critical | warning | info
	Status    string // active | archived
	Metadata  map[string]any
	CreatedAt int64
	UpdatedAt int64
}

// FailureEdge is a typed directed edge between two FailureNodes.
type FailureEdge struct {
	ID         string
	FromID     string
	ToID       string
	EdgeType   string
	Confidence string // high | medium | low
	Evidence   string
	Source     string
	CreatedAt  int64
}

// ErrorSignature is a normalized error string linked to a failure category.
// One category can have many signatures; one signature belongs to one category.
type ErrorSignature struct {
	ID                  string
	Signature           string // human-readable canonical name
	NormalizedSignature string // deterministically normalized, used for matching
	CategoryID          string
	Severity            string
	Sample              string // real example before normalization
	MatcherKind         string // exact | regex | keyword
	MatcherPattern      string // regex or keyword list if not exact
	CreatedAt           int64
	UpdatedAt           int64
}

// FailureObservation records one observed error linked to a matched category.
type FailureObservation struct {
	ID                  string
	SessionID           string
	IncidentID          string
	RunID               string
	Source              string // live_signal | session | manual | semantic_diff
	RawError            string
	NormalizedSignature string
	MatchedSignatureID  string
	MatchedCategoryID   string
	Component           string
	ServiceName         string
	FilePath            string
	Symbol              string
	Confidence          string
	CreatedAt           int64
}

// FailureExplanation is the full diagnostic output for a matched or requested category.
type FailureExplanation struct {
	Observation        FailureObservation
	Category           FailureNode
	Symptoms           []FailureNode
	LikelyCauses       []FailureNode
	Resolutions        []FailureNode
	WrongFixes         []FailureNode
	RequiredTests      []FailureNode
	RelatedInvariants  []FailureNode
	RelatedIncidents   []FailureNode
	WorkflowModes      []WorkflowFailureMode
	RecommendedAction  string
	Score              float64
	Confidence         string
}

// ResolutionRecipe provides step-by-step instructions for applying a resolution.
type ResolutionRecipe struct {
	ID             string
	ResolutionID   string
	Title          string
	Steps          []string
	ForbiddenSteps []string
	Verification   []string
	CreatedAt      int64
	UpdatedAt      int64
}

// WorkflowFailureMode describes a typed failure state in the workflow engine.
type WorkflowFailureMode struct {
	ID             string
	Name           string
	Summary        string
	WorkflowStage  string
	FailurePhase   string
	RetrySemantics string
	ClosureRule    string
	Metadata       map[string]any
	CreatedAt      int64
	UpdatedAt      int64
}

// MatchErrorRequest is the input to the error matcher.
type MatchErrorRequest struct {
	SessionID    string
	IncidentID   string
	RunID        string
	RawError     string
	Component    string
	ServiceName  string
	FilePath     string
	Symbol       string
	SemanticAtoms []string
	LiveSignals  []string
	WorkflowStage string
}

// SimilarFailureRequest finds failure categories similar to an observed error.
type SimilarFailureRequest struct {
	RawError      string
	Component     string
	SemanticAtoms []string
	LiveSignals   []string
	Limit         int
}

// CategorySeed is the intermediate representation used when loading seed YAML files.
type CategorySeed struct {
	ID       string         `yaml:"id"`
	Type     string         `yaml:"type"`
	Name     string         `yaml:"name"`
	Severity string         `yaml:"severity"`
	Summary  string         `yaml:"summary"`

	Signatures []string     `yaml:"signatures"`
	Symptoms   []SeedItem   `yaml:"symptoms"`

	Causes     []SeedItem   `yaml:"causes"`
	Resolutions []SeedItem  `yaml:"resolutions"`
	WrongFixes []SeedItem   `yaml:"wrong_fixes"`
	Tests      []SeedItem   `yaml:"tests"`
}

// SeedItem is a sub-node referenced from a CategorySeed.
type SeedItem struct {
	ID      string `yaml:"id"`
	Summary string `yaml:"summary"`
}

// Package failurelearning implements the Failure Graph Learning Loop for Globular Awareness.
//
// It turns closed incidents and sessions into structured proposals to update the
// Failure Knowledge Graph. The pipeline is: extract → match → propose → review → apply.
//
// Design rule: proposals are auditable, durable, and human-reviewable before any mutation.
package failurelearning

// Proposal kinds — what kind of update the proposal will make to the failure graph.
const (
	KindAddSignature        = "add_signature"
	KindAddSymptom          = "add_symptom"
	KindAddCause            = "add_cause"
	KindAddResolution       = "add_resolution"
	KindAddWrongFix         = "add_wrong_fix"
	KindAddRegressionTest   = "add_regression_test"
	KindAddInvariantEdge    = "add_invariant_edge"
	KindCreateCategory      = "create_category"
	KindMergeCategories     = "merge_categories"
	KindNoReusableKnowledge = "no_reusable_knowledge"
)

// Proposal statuses.
const (
	StatusProposed   = "proposed"
	StatusApproved   = "approved"
	StatusRejected   = "rejected"
	StatusDeferred   = "deferred"
	StatusApplied    = "applied"
	StatusSuperseded = "superseded"
)

// Review decisions.
const (
	DecisionApprove          = "approve"
	DecisionApproveWithEdits = "approve_with_edits"
	DecisionReject           = "reject"
	DecisionDefer            = "defer"
	DecisionMerge            = "merge"
)

// Source types.
const (
	SourceIncident = "incident"
	SourceSession  = "session"
	SourceClosure  = "closure"
)

// FailureLearningProposal is a structured proposal to update the Failure Knowledge Graph.
// It records the source (incident/session/closure), the extracted knowledge, the
// proposed graph patch, and the review lifecycle.
type FailureLearningProposal struct {
	ID                 string
	SourceType         string
	SourceID           string
	ProposalKind       string
	Status             string
	TargetCategoryID   string
	ProposedCategoryID string
	Title              string
	Summary            string
	Confidence         string
	Rationale          string
	Extracted          FailureLearningExtract
	Patch              FailureGraphPatch
	CreatedBy          string
	ReviewedBy         string
	CreatedAt          int64
	ReviewedAt         int64
	AppliedAt          int64
}

// FailureLearningExtract holds all structured knowledge extracted from a source.
type FailureLearningExtract struct {
	RawErrors         []string
	NormalizedErrors  []string
	Symptoms          []string
	RootCauses        []string
	Resolutions       []string
	WrongFixes        []string
	RegressionTests   []string
	RelatedFiles      []string
	RelatedComponents []string
	RelatedInvariants []string
	RelatedIncidents  []string
	SemanticAtoms     []string
	LiveSignals       []string
	ClosureEvidence   []string
}

// FailureGraphPatch describes the set of mutations to apply to the failure graph.
type FailureGraphPatch struct {
	AddNodes      []FailureGraphNodePatch
	AddEdges      []FailureGraphEdgePatch
	AddSignatures []FailureSignaturePatch
	AddRecipes    []FailureResolutionRecipePatch
	SeedFilePath  string
	SeedYAML      string
}

// FailureGraphNodePatch describes a node to add.
type FailureGraphNodePatch struct {
	ID       string
	Type     string
	Name     string
	Summary  string
	Severity string
	Metadata map[string]any
}

// FailureGraphEdgePatch describes an edge to add.
type FailureGraphEdgePatch struct {
	FromID     string
	ToID       string
	EdgeType   string
	Confidence string
	Evidence   string
	Source     string
}

// FailureSignaturePatch describes an error signature to add.
type FailureSignaturePatch struct {
	Signature      string
	Normalized     string
	CategoryID     string
	Severity       string
	MatcherKind    string
	MatcherPattern string
	Sample         string
}

// FailureResolutionRecipePatch describes a resolution recipe to add.
type FailureResolutionRecipePatch struct {
	Title          string
	Steps          []string
	ForbiddenSteps []string
	Verification   []string
}

// FailureLearningReview is an audit record for a review decision on a proposal.
type FailureLearningReview struct {
	ID              string
	ProposalID      string
	Reviewer        string
	Decision        string
	Notes           string
	EditedPatchJSON string
	CreatedAt       int64
}

// SeedSyncStatus tracks whether a YAML seed file was successfully written.
type SeedSyncStatus struct {
	ID          string
	ProposalID  string
	SeedPath    string
	Status      string // synced | failed | pending
	ContentHash string
	Message     string
	CreatedAt   int64
	UpdatedAt   int64
}

// ProposeRequest is the input to ProposeUpdate, supplying raw knowledge from any source.
type ProposeRequest struct {
	SourceType      string
	SourceID        string
	CreatedBy       string
	RawErrors       []string
	Symptoms        []string
	RootCauses      []string
	Resolutions     []string
	WrongFixes      []string
	Tests           []string
	Files           []string
	Components      []string
	Invariants      []string
	Incidents       []string
	SemanticAtoms   []string
	LiveSignals     []string
	ClosureEvidence []string
}

// FailureLearningMatch is the result of comparing an extract against the failure graph.
type FailureLearningMatch struct {
	CategoryID    string
	CategoryName  string
	MatchScore    float64
	MatchedFields []string
	ProposalKind  string
	Confidence    string
}

// ApplyResult summarises what was created when a proposal was applied.
type ApplyResult struct {
	ProposalID   string
	CreatedNodes int
	CreatedEdges int
	SeedPath     string
	ContentHash  string
}

// ClosureInfo carries the closure fields that CheckClosure evaluates.
type ClosureInfo struct {
	ClosureID     string
	SourceType    string
	HasRootCause  bool
	HasResolution bool
	HasProof      bool
	RawErrors     []string
	RootCauses    []string
	Resolutions   []string
	WrongFixes    []string
	Tests         []string
}

// ClosureWithLearning is the verdict returned by CheckClosure.
type ClosureWithLearning struct {
	Status             string // "clean" | "closed_with_learning_pending"
	ExistingProposalID string
	RequiresLearning   bool
	Reason             string
}

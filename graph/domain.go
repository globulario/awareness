package graph

// EdgeDomain classifies an edge's operational role in the awareness graph.
// A relation is not automatically a decision — domains separate descriptive
// knowledge from operational guidance.
type EdgeDomain string

const (
	// DomainInformation describes structure, references, implementation,
	// ownership, dependencies, definitions. These edges answer "what exists?"
	DomainInformation EdgeDomain = "information"

	// DomainDecision describes allowed actions, forbidden actions, fix order,
	// stop rules, verification requirements. These edges answer "what to do?"
	DomainDecision EdgeDomain = "decision"

	// DomainProof describes tests, CI evidence, runtime verification, closure
	// conditions. These edges answer "is this verified?"
	DomainProof EdgeDomain = "proof"

	// DomainRisk describes failure modes, drift, symptoms, hazards, stale edges,
	// contradictions. These edges answer "what can go wrong?"
	DomainRisk EdgeDomain = "risk"

	// DomainProposal describes pending knowledge changes that are not yet
	// authoritative. These edges answer "what is being learned?"
	DomainProposal EdgeDomain = "proposal"
)

// EdgeRole describes the semantic relationship expressed by an edge.
type EdgeRole string

const (
	EdgeRoleImplements   EdgeRole = "implements"    // source implements an abstraction
	EdgeRoleVerifies     EdgeRole = "verifies"       // source proves a claim
	EdgeRoleForbids      EdgeRole = "forbids"        // source prohibits an action
	EdgeRoleRecommends   EdgeRole = "recommends"     // source recommends an action
	EdgeRoleViolates     EdgeRole = "violates"       // source breaks an invariant
	EdgeRoleObserves     EdgeRole = "observes"       // source captures runtime evidence
	EdgeRoleRequires     EdgeRole = "requires"       // source depends on a precondition
	EdgeRolePromotesTo   EdgeRole = "promotes_to"    // source becomes authoritative entry
	EdgeRoleDetects      EdgeRole = "detects"        // source identifies a pattern
	EdgeRoleProduces     EdgeRole = "produces"       // source creates an output
	EdgeRoleDepends      EdgeRole = "depends"        // source relies on a component
	EdgeRoleCauses       EdgeRole = "causes"         // source triggers an effect
	EdgeRoleProtects     EdgeRole = "protects"       // source guards an invariant
	EdgeRoleSupersedes   EdgeRole = "supersedes"     // source replaces a prior entry
	EdgeRoleContradicts  EdgeRole = "contradicts"    // source conflicts with another claim
)

// edgeDomains maps each edge kind constant to its primary operational domain.
// Edges can participate in multiple domains; only the most actionable is listed.
// Use DomainForEdgeKind to look up the domain; use DomainsForEdgeKind for multi-domain.
var edgeDomains = map[string]EdgeDomain{
	// ── Information ──────────────────────────────────────────────────────────
	EdgeImplements:       DomainInformation,
	EdgeDefines:          DomainInformation,
	EdgeCalls:            DomainInformation,
	EdgeImports:          DomainInformation,
	EdgeReads:            DomainInformation,
	EdgeWrites:           DomainInformation,
	EdgeOwns:             DomainInformation,
	EdgeDependsOn:        DomainInformation,
	EdgeProduces:         DomainInformation,
	EdgeRunsAs:           DomainInformation,
	EdgeEmits:            DomainInformation,
	EdgeSubscribes:       DomainInformation,
	EdgeEnforces:         DomainDecision,
	EdgeProtects:         DomainInformation,
	EdgeRecords:          DomainInformation,
	EdgeRecalls:          DomainInformation,
	EdgeAffects:          DomainInformation,
	EdgeTouchesFile:      DomainInformation,
	EdgeTouchesSymbol:    DomainInformation,
	EdgeCoversPattern:    DomainInformation,
	EdgeDocuments:        DomainInformation,
	EdgeMentionedIn:      DomainInformation,
	EdgeExplains:         DomainInformation,
	EdgeCapturedIn:       DomainInformation,
	EdgeReports:          DomainInformation,
	EdgeEvidences:        DomainInformation,
	EdgeCurrentStatusOf:  DomainInformation,
	EdgeHasStateDelta:    DomainInformation,
	EdgeRuntimeDependsOn: DomainInformation,
	EdgeControls:         DomainInformation,

	// ── Decision ─────────────────────────────────────────────────────────────
	EdgeForbids:          DomainDecision,
	EdgeRequires:         DomainDecision,
	EdgeSafeWhen:         DomainDecision,
	EdgeUnsafeWhen:       DomainDecision,
	EdgeBlocks:           DomainDecision,
	EdgeUnblocks:         DomainDecision,
	EdgeDecides:          DomainDecision,
	EdgeRationalizes:     DomainDecision,
	EdgeRemediatedBy:     DomainDecision,
	EdgeFixedBy:          DomainDecision,
	EdgeFixes:            DomainDecision,
	EdgePartiallyFixes:   DomainDecision,
	EdgeDerivedFrom:      DomainDecision,
	EdgeGeneralizesTo:    DomainDecision,
	EdgeSpecializes:      DomainDecision,
	EdgeContradicts:      DomainDecision,

	// ── Proof ────────────────────────────────────────────────────────────────
	EdgeTestedBy:          DomainProof,
	EdgeValidatedBy:       DomainProof,
	EdgeVerifiedBy:        DomainProof,
	EdgeVerifies:          DomainProof, // test → invariant direct proof
	EdgeRequiresTest:      DomainProof,
	EdgeSupportedBy:       DomainProof,
	EdgeApprovedBy:        DomainProof,
	EdgeImplementsGuardrail: DomainProof,

	// ── Risk ─────────────────────────────────────────────────────────────────
	EdgeViolates:             DomainRisk,
	EdgeCausedBy:             DomainRisk,
	EdgeMatchesInvariant:     DomainRisk,
	EdgeMatchesFailureMode:   DomainRisk,
	EdgeSmellsLike:           DomainRisk,
	EdgeExhibits:             DomainRisk,
	EdgeMitigates:            DomainRisk,
	EdgePreventedBy:          DomainRisk,
	EdgeStillMissing:         DomainRisk,
	EdgeRegressedBy:          DomainRisk,
	EdgeDuplicates:           DomainRisk,

	// ── Proposal ─────────────────────────────────────────────────────────────
	EdgeProposes:         DomainProposal,
	EdgeObservedDuring:   DomainProposal,
	EdgeNeedsReview:      DomainProposal,
	EdgeRejectedBy:       DomainProposal,
	EdgeAliases:          DomainProposal,
	EdgeSupersedes:       DomainProposal,
	EdgePromotedTo:       DomainProposal,

	// ── Phase 3: precise file→invariant edges ────────────────────────────────
	EdgeObserves:    DomainInformation, // detection/reporting — information only
	EdgeConfigures:  DomainDecision,    // configuring policy IS decision-domain
	EdgeMayAffect:   DomainInformation, // weak/indirect — information only

	// ── Service design graph edges (Phase 2-8) ────────────────────────────────
	EdgeHasAuthz:         DomainDecision,    // rpc → authz constraint IS decision
	EdgeHasStreamingMode: DomainInformation, // structural metadata
	EdgeImplementedBy:    DomainInformation, // rpc → go implementation
	EdgeGovernedBy:       DomainDecision,    // rpc → invariant — decision constraint
	EdgeProvidesService:  DomainInformation, // package → service — structural

	// ── Invariant implementation graph edges ──────────────────────────────────
	EdgePartiallyImplements:   DomainInformation, // weaker than implements
	EdgeReadsAuthority:        DomainProof,        // evidence: what authority is read
	EdgeWritesState:           DomainRisk,         // evidence: what state is mutated
	EdgeGuardsAction:          DomainDecision,     // function guards an action via txn
	EdgeBlocksForbiddenAction: DomainDecision,     // forbidden fix blocks this invariant
	EdgeConstrainsActionFor:   DomainDecision,     // invariant constrains an action
	EdgeHasEvidence:           DomainInformation,  // link to any evidence artifact
}

// edgeRoles maps each edge kind to its semantic role.
var edgeRoles = map[string]EdgeRole{
	EdgeImplements:      EdgeRoleImplements,
	EdgeTestedBy:        EdgeRoleVerifies,
	EdgeValidatedBy:     EdgeRoleVerifies,
	EdgeVerifiedBy:      EdgeRoleVerifies,
	EdgeRequiresTest:    EdgeRoleVerifies,
	EdgeForbids:         EdgeRoleForbids,
	EdgeSafeWhen:        EdgeRoleForbids,
	EdgeUnsafeWhen:      EdgeRoleForbids,
	EdgeRemediatedBy:    EdgeRoleRecommends,
	EdgeFixedBy:         EdgeRoleRecommends,
	EdgeFixes:           EdgeRoleRecommends,
	EdgeViolates:        EdgeRoleViolates,
	EdgeCausedBy:        EdgeRoleCauses,
	EdgeCapturedIn:      EdgeRoleObserves,
	EdgeEvidences:       EdgeRoleObserves,
	EdgeMatchesInvariant:    EdgeRoleObserves,
	EdgeMatchesFailureMode:  EdgeRoleObserves,
	EdgeRequires:        EdgeRoleRequires,
	EdgeDependsOn:       EdgeRoleDepends,
	EdgePromotedTo:      EdgeRolePromotesTo,
	EdgeProposes:        EdgeRoleProduces,
	EdgeProduces:        EdgeRoleProduces,
	EdgeProtects:        EdgeRoleProtects,
	EdgeSupersedes:      EdgeRoleSupersedes,
	EdgeContradicts:     EdgeRoleContradicts,

	// Phase 3: precise file→invariant edges.
	EdgeEnforces:   EdgeRoleProtects,   // enforces = active protection
	EdgeConfigures: EdgeRoleRecommends, // configures = defines the policy
	EdgeObserves:   EdgeRoleObserves,   // observes = detection/reporting
	EdgeMayAffect:  EdgeRoleObserves,   // may_affect = weak indirect link

	// Service design graph edges.
	EdgeHasAuthz:         EdgeRoleRequires,   // rpc requires authz
	EdgeGovernedBy:       EdgeRoleProtects,   // governed by invariant
	EdgeImplementedBy:    EdgeRoleImplements, // rpc implemented by method
	EdgeProvidesService:  EdgeRoleProduces,   // package provides service
}

// DomainForEdgeKind returns the primary operational domain for a given edge kind.
// Returns DomainInformation for unknown edge kinds (safe default).
func DomainForEdgeKind(kind string) EdgeDomain {
	if d, ok := edgeDomains[kind]; ok {
		return d
	}
	return DomainInformation
}

// RoleForEdgeKind returns the semantic role for a given edge kind.
// Returns EdgeRoleProduces for unknown edge kinds (safe default).
func RoleForEdgeKind(kind string) EdgeRole {
	if r, ok := edgeRoles[kind]; ok {
		return r
	}
	return EdgeRoleProduces
}

// IsDecisionEdge returns true if the edge kind belongs to the decision domain.
func IsDecisionEdge(kind string) bool {
	return DomainForEdgeKind(kind) == DomainDecision
}

// IsProofEdge returns true if the edge kind belongs to the proof domain.
func IsProofEdge(kind string) bool {
	return DomainForEdgeKind(kind) == DomainProof
}

// IsRiskEdge returns true if the edge kind belongs to the risk domain.
func IsRiskEdge(kind string) bool {
	return DomainForEdgeKind(kind) == DomainRisk
}

// IsProposalEdge returns true if the edge kind belongs to the proposal domain.
func IsProposalEdge(kind string) bool {
	return DomainForEdgeKind(kind) == DomainProposal
}

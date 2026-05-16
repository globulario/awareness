package graph

import (
	"context"
	"fmt"
)

// Edge kinds supported in V1.
const (
	EdgeDefines      = "defines"
	EdgeCalls        = "calls"
	EdgeImports      = "imports"
	EdgeReads        = "reads"
	EdgeWrites       = "writes"
	EdgeOwns         = "owns"
	EdgeDependsOn    = "depends_on"
	EdgeProduces     = "produces"
	EdgeRunsAs       = "runs_as"
	EdgeEmits        = "emits"
	EdgeSubscribes   = "subscribes"
	EdgeProtects     = "protects"
	EdgeEnforces     = "enforces"
	EdgeViolates     = "violates"
	EdgeTestedBy     = "tested_by"
	EdgeRemediatedBy = "remediated_by"
	EdgeRecords      = "records"
	EdgeRecalls      = "recalls"
	EdgeAffects      = "affects"
	EdgeBlocks       = "blocks"
	EdgeUnblocks     = "unblocks"
	EdgeRequires     = "requires"
	EdgeForbids      = "forbids"
	EdgeSafeWhen     = "safe_when"
	EdgeUnsafeWhen   = "unsafe_when"

	// Learning edge kinds (Task 3).
	EdgeObservedDuring = "observed_during"
	EdgeProposes       = "proposes"
	EdgeDerivedFrom    = "derived_from"
	EdgeSupportedBy    = "supported_by"
	EdgePromotedTo     = "promoted_to"
	EdgeSupersedes     = "supersedes"
	EdgeAliases        = "aliases"
	EdgeNeedsReview    = "needs_review"
	EdgeApprovedBy     = "approved_by"
	EdgeRejectedBy     = "rejected_by"

	// Fix ledger edge kinds (Task 4).
	EdgeFixes               = "fixes"
	EdgePartiallyFixes      = "partially_fixes"
	EdgeVerifiedBy          = "verified_by"
	EdgeStillMissing        = "still_missing"
	EdgeDuplicates          = "duplicates"
	EdgeRegressedBy         = "regressed_by"
	EdgeImplementsGuardrail = "implements_guardrail"
	EdgeRequiresTest        = "requires_test"
	EdgeTouchesFile         = "touches_file"
	EdgeTouchesSymbol       = "touches_symbol"
	EdgeCoversPattern       = "covers_pattern"

	// Protocol annotation edge kinds (Task 8).
	EdgeControls = "controls"

	// Design pattern layer edge kinds.
	EdgeImplements  = "implements"   // source_file implements a design_pattern
	EdgeExhibits    = "exhibits"     // source_file exhibits an anti_pattern
	EdgeSmellsLike  = "smells_like"  // anti_pattern → code_smell
	EdgeMitigates   = "mitigates"    // design_pattern mitigates failure_mode / anti_pattern
	EdgePreventedBy = "prevented_by" // anti_pattern prevented_by design_pattern

	// Design / documentation edge kinds (Task 12).
	EdgeExplains      = "explains"
	EdgeDecides       = "decides"
	EdgeRationalizes  = "rationalizes"
	EdgeContradicts   = "contradicts"
	EdgeDocuments     = "documents"
	EdgeMentionedIn   = "mentioned_in"
	EdgeCausedBy      = "caused_by"
	EdgeFixedBy       = "fixed_by"
	EdgeValidatedBy   = "validated_by"
	EdgeGeneralizesTo = "generalizes_to"
	EdgeSpecializes   = "specializes"

	// Experience ledger edge kinds.
	EdgePursuedGoal                   = "pursued_goal"
	EdgeUsedStrategy                  = "used_strategy"
	EdgeHasAttempt                    = "has_attempt"
	EdgeFailedBecause                 = "failed_because"
	EdgeSucceededBecause              = "succeeded_because"
	EdgeProducedLesson                = "produced_lesson"
	EdgeWarnsAgainst                  = "warns_against"
	EdgeSuggestsNext                  = "suggests_next"
	EdgeRelatedToCapability           = "related_to_capability"
	EdgeChangedSymbol                 = "changed_symbol"
	EdgeAvoidedForbiddenFix           = "avoided_forbidden_fix"
	EdgeProducedForbiddenFixCandidate = "produced_forbidden_fix_candidate"
	EdgeProducedInvariantCandidate    = "produced_invariant_candidate"
	EdgeClosedBy                      = "closed_by"
	EdgeSimilarTo                     = "similar_to"
	EdgeContradictedBy                = "contradicted_by"

	// Runtime bridge edge kinds (Task 6).
	EdgeCapturedIn         = "captured_in"
	EdgeReports            = "reports"
	EdgeEvidences          = "evidences"
	EdgeMatchesInvariant   = "matches_invariant"
	EdgeMatchesFailureMode = "matches_failure_mode"
	EdgeHasStateDelta      = "has_state_delta"
	EdgeCurrentStatusOf    = "current_status_of"
	EdgeRuntimeDependsOn   = "runtime_depends_on"

	// Observation edge kinds.
	EdgeObserves = "observes" // source_file observes/detects an invariant (diagnostic/reporting)

	// Precise file→invariant relationship edges (Phase 3).
	EdgeConfigures = "configures" // source_file configures/defines data for invariant
	EdgeMayAffect  = "may_affect" // source_file may indirectly affect invariant

	// Service design graph edge kinds (Phase 2-8).
	EdgeHasAuthz         = "has_authz"          // rpc_method → authz_annotation
	EdgeHasStreamingMode = "has_streaming_mode" // rpc_method → streaming_mode
	EdgeImplementedBy    = "implemented_by"     // rpc_method → symbol (Go method)
	EdgeGovernedBy       = "governed_by"        // rpc_method → invariant
	EdgeProvidesService  = "provides_service"   // package → proto_service

	// Invariant implementation graph edge kinds.
	// These edges wire source code, tests, and authority data to invariant nodes,
	// forming an "invariant implementation graph" that agents can traverse to
	// understand what enforces an invariant, how it is tested, and what breaks it.
	EdgePartiallyImplements   = "partially_implements"    // source_file → invariant (weaker than implements)
	EdgeReadsAuthority        = "reads_authority"         // function/file → invariant authority source
	EdgeWritesState           = "writes_state"            // function/file → state artifact it mutates
	EdgeGuardsAction          = "guards_action"           // function/file → action it gates/transacts
	EdgeBlocksForbiddenAction = "blocks_forbidden_action" // forbidden_fix → invariant it guards
	EdgeVerifies              = "verifies"                // test → invariant (direct test proof)
	EdgeConstrainsActionFor   = "constrains_action_for"   // invariant → action it constrains at runtime
	EdgeHasEvidence           = "has_evidence"            // any node → evidence artifact

	// Live etcd / cluster state edge kinds.
	EdgeEtcdSnapshotContainsKey     = "etcd_snapshot_contains_key"
	EdgeKeyDeclaresResource         = "key_declares_resource"
	EdgeDesiredTargetsService       = "desired_targets_service"
	EdgeDesiredTargetsNode          = "desired_targets_node"
	EdgeReleaseHasBuildID           = "release_has_build_id"
	EdgeNodeReportsInstalledPackage = "node_reports_installed_package"
	EdgeNodeReportsRuntimeStatus    = "node_reports_runtime_status"
	EdgeServiceConfigDeclEndpoint   = "service_config_declares_endpoint"

	// Convergence delta edge kinds.
	EdgeDesiredComparesToInstalled = "desired_compares_to_installed"
	EdgeInstalledComparesToRuntime = "installed_compares_to_runtime"
	EdgeActionTargetsPackage       = "action_targets_package"
	EdgeDriftDetectedBetween       = "drift_detected_between"

	// Metrics edge kinds.
	EdgeMetricQueryObservesService        = "metric_query_observes_service"
	EdgeMetricThresholdAppliesToService   = "metric_threshold_applies_to_service"
	EdgeMetricWarningIndicatesFailureMode = "metric_warning_indicates_failure_mode"
	EdgeMetricWarningRisksInvariant       = "metric_warning_risks_invariant"
	EdgeMetricWarningTriggerRule          = "metric_warning_triggers_decision_rule"

	// PKI / certificate edge kinds.
	EdgeCertIssuedBy       = "certificate_issued_by"
	EdgeCertHasSAN         = "certificate_has_san"
	EdgeCertUsedByService  = "certificate_used_by_service"
	EdgeCertCoversEndpoint = "certificate_covers_endpoint"
	EdgeCertRisksInvariant = "certificate_risks_invariant"

	// RBAC edge kinds.
	EdgeRoleGrantsPermission      = "role_grants_permission"
	EdgeSubjectBoundToRole        = "subject_bound_to_role"
	EdgeServiceRequiresPermission = "service_requires_permission"
	EdgeServiceHasIdentity        = "service_has_identity"
	EdgePermissionAllowsAction    = "permission_allows_action"
	EdgePermissionRisksInvariant  = "permission_risks_invariant"

	// Workflow execution edge kinds.
	EdgeWorkflowRunInstantiates       = "workflow_run_instantiates_definition"
	EdgeWorkflowRunTargetsService     = "workflow_run_targets_service"
	EdgeWorkflowRunTargetsNode        = "workflow_run_targets_node"
	EdgeWorkflowRunTargetsPackage     = "workflow_run_targets_package"
	EdgeWorkflowRunFailedAtStep       = "workflow_run_failed_at_step"
	EdgeWorkflowFailureIndicates      = "workflow_failure_indicates_failure_mode"
	EdgeWorkflowFailureRisksInvariant = "workflow_failure_risks_invariant"

	// Typed workflow execution proof edges (precise semantics, preferred over owns/depends_on).
	EdgeWorkflowRunHasStepRun            = "workflow_run_has_step_run"
	EdgeWorkflowStepRunInstantiatesStep  = "workflow_step_run_instantiates_step"
	EdgeWorkflowStepVerifiesInvariant    = "workflow_step_verifies_invariant"
	EdgeWorkflowStepTargetsState         = "workflow_step_targets_state"
	EdgeWorkflowStepRunFailedWithError   = "workflow_step_run_failed_with_error"
	EdgeWorkflowErrorMatchesFailureMode  = "workflow_error_matches_failure_mode"
	EdgeWorkflowRunForbidsAction         = "workflow_run_forbids_action"
	EdgeWorkflowRunRecommendsDiagnostic  = "workflow_run_recommends_diagnostic"
	EdgeWorkflowReceiptProvesStepEffect  = "workflow_receipt_proves_step_effect"
	EdgeWorkflowStepRunEmittedReceipt    = "workflow_step_run_emitted_receipt"
	EdgeWorkflowReceiptVerifiesInvariant = "workflow_receipt_verifies_invariant"
	EdgeWorkflowReceiptRecordsError      = "workflow_receipt_records_error"
	EdgeWorkflowReceiptVerifiesAction    = "workflow_receipt_verifies_action"

	// DNS / network edge kinds.
	EdgeDNSRecordResolvesTo          = "dns_record_resolves_to"
	EdgeServiceEndpointAdvertisedBy  = "service_endpoint_advertised_by"
	EdgeDomainSpecDeclaresRecord     = "domain_spec_declares_record"
	EdgeDNSRecordRisksInvariant      = "dns_record_risks_invariant"
	EdgeServiceEndpointCoveredByCert = "service_endpoint_covered_by_cert"
)

// EdgeClass distinguishes decision-relevant edges from contextual information edges.
const (
	// EdgeClassDecision marks edges that directly drive decisions: causal rules,
	// forbidden actions, required tests, blocks relationships. Weight=1.0.
	EdgeClassDecision = "decision"
	// EdgeClassStructural marks architectural structure edges: owns, depends_on,
	// calls, reads, writes. Weight=0.7.
	EdgeClassStructural = "structural"
	// EdgeClassInformation marks low-signal context edges: references, mentions,
	// similar_to, documents. Weight=0.3.
	EdgeClassInformation = "information"
)

// decisionEdgeKinds lists edge kinds that are always classified as decision-class.
var decisionEdgeKinds = map[string]bool{
	EdgeBlocks: true, EdgeRequires: true, EdgeForbids: true, EdgeSafeWhen: true,
	EdgeUnsafeWhen: true, EdgeViolates: true, EdgeEnforces: true,
	EdgeRequiresTest: true, EdgeGovernedBy: true, EdgeCausedBy: true,
	EdgeFixedBy: true, EdgeRemediatedBy: true, EdgeUnblocks: true,
	// Invariant implementation graph — decision-class.
	EdgeGuardsAction: true, EdgeBlocksForbiddenAction: true, EdgeConstrainsActionFor: true,
}

// structuralEdgeKinds lists edge kinds classified as structural-class.
var structuralEdgeKinds = map[string]bool{
	EdgeOwns: true, EdgeDependsOn: true, EdgeCalls: true, EdgeImports: true,
	EdgeReads: true, EdgeWrites: true, EdgeDefines: true, EdgeProduces: true,
	EdgeEmits: true, EdgeTestedBy: true, EdgeImplements: true, EdgeControls: true,
	EdgeImplementedBy: true, EdgeCurrentStatusOf: true, EdgeRuntimeDependsOn: true,
	// Invariant implementation graph — structural-class.
	EdgePartiallyImplements: true, EdgeReadsAuthority: true, EdgeWritesState: true, EdgeVerifies: true,
}

// classifyEdge returns the edge_class and weight for an edge kind.
func classifyEdge(kind string) (string, float64) {
	if decisionEdgeKinds[kind] {
		return EdgeClassDecision, 1.0
	}
	if structuralEdgeKinds[kind] {
		return EdgeClassStructural, 0.7
	}
	return EdgeClassInformation, 0.3
}

// Edge is a directed relationship between two graph nodes.
type Edge struct {
	Src        string
	Kind       string
	Dst        string
	Phase      string
	Required   bool
	Confidence float64
	// Class is the edge classification: decision, structural, or information.
	// Auto-classified from Kind if empty.
	Class string
	// Weight is the traversal weight (0.0–1.0). Auto-set from Class if 0.
	Weight   float64
	Metadata map[string]any
	// Provenance describes where this edge came from (source_type, source_file,
	// created_by, verification_level, …). Stored in the dedicated
	// edges.provenance_json column — distinct from Metadata, which is for
	// caller-supplied edge attributes. The column is the canonical home of
	// provenance; do not write or read provenance via Metadata. See
	// docs/awareness/composed_path_failures.md (edge provenance home).
	Provenance map[string]any
}

// ProvenanceEdge is an Edge that carries full provenance metadata describing
// where the edge came from and how well it is verified.
// Use AddEdgeWithProvenance to write provenance into the graph.
type ProvenanceEdge struct {
	Edge
	// SourceType is one of the SourceXxx constants in the integrity package.
	SourceType string
	// SourceFile is the YAML or source file from which this edge was extracted.
	SourceFile string
	// SourceCommit is the git SHA when the edge was last written.
	SourceCommit string
	// CreatedBy identifies the extractor or tool that created this edge.
	CreatedBy string
	// LastVerifiedAt is the Unix timestamp of the last verification.
	LastVerifiedAt int64
	// LastVerifiedBy identifies the verifier (e.g., "ci-check", "test-discovery").
	LastVerifiedBy string
	// VerificationLevel is one of the TrustXxx constants in the integrity package.
	VerificationLevel string
	// StalePolicy lists the conditions under which this edge becomes stale.
	StalePolicy []string
}

// AddEdgeWithProvenance writes an edge with full provenance metadata.
// Provenance lands in the dedicated edges.provenance_json column, NOT in
// metadata_json. See docs/awareness/composed_path_failures.md (edge
// provenance home) for why these two homes were consolidated to one.
func (g *Graph) AddEdgeWithProvenance(ctx context.Context, pe ProvenanceEdge) error {
	pe.Edge.Provenance = map[string]any{
		"source_type":        pe.SourceType,
		"source_file":        pe.SourceFile,
		"source_commit":      pe.SourceCommit,
		"created_by":         pe.CreatedBy,
		"last_verified_at":   pe.LastVerifiedAt,
		"last_verified_by":   pe.LastVerifiedBy,
		"verification_level": pe.VerificationLevel,
		"stale_policy":       pe.StalePolicy,
	}
	return g.AddEdge(ctx, pe.Edge)
}

// AddEdge upserts an edge. The (src, kind, dst, phase) tuple is the primary key.
// edge_class and weight are auto-classified from Kind when not explicitly set.
//
// Provenance, when supplied via e.Provenance, is written to the canonical
// edges.provenance_json column. Empty Provenance writes "{}" (matching the
// schema default), so existing rows are not mutated by callers that don't
// touch provenance.
func (g *Graph) AddEdge(ctx context.Context, e Edge) error {
	meta, err := marshalMeta(e.Metadata)
	if err != nil {
		return fmt.Errorf("AddEdge %s -[%s]-> %s: %w", e.Src, e.Kind, e.Dst, err)
	}
	prov, err := marshalMeta(e.Provenance)
	if err != nil {
		return fmt.Errorf("AddEdge %s -[%s]-> %s: encode provenance: %w", e.Src, e.Kind, e.Dst, err)
	}
	conf := e.Confidence
	if conf == 0 {
		conf = 1.0
	}
	req := 0
	if e.Required {
		req = 1
	}
	class := e.Class
	weight := e.Weight
	if class == "" || weight == 0 {
		c, w := classifyEdge(e.Kind)
		if class == "" {
			class = c
		}
		if weight == 0 {
			weight = w
		}
	}
	// On UPSERT, only overwrite provenance_json when the caller actually
	// supplied one. This preserves provenance written by an earlier
	// AddEdgeWithProvenance call when a later AddEdge (without provenance)
	// touches the same (src,kind,dst,phase) tuple.
	providedProv := len(e.Provenance) > 0
	if providedProv {
		_, err = g.db.ExecContext(ctx, `
			INSERT INTO edges (src, kind, dst, phase, required, confidence, metadata_json, edge_class, weight, provenance_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(src, kind, dst, phase) DO UPDATE SET
				required        = excluded.required,
				confidence      = excluded.confidence,
				metadata_json   = excluded.metadata_json,
				edge_class      = excluded.edge_class,
				weight          = excluded.weight,
				provenance_json = excluded.provenance_json
		`, e.Src, e.Kind, e.Dst, e.Phase, req, conf, meta, class, weight, prov)
	} else {
		_, err = g.db.ExecContext(ctx, `
			INSERT INTO edges (src, kind, dst, phase, required, confidence, metadata_json, edge_class, weight, provenance_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '{}')
			ON CONFLICT(src, kind, dst, phase) DO UPDATE SET
				required      = excluded.required,
				confidence    = excluded.confidence,
				metadata_json = excluded.metadata_json,
				edge_class    = excluded.edge_class,
				weight        = excluded.weight
		`, e.Src, e.Kind, e.Dst, e.Phase, req, conf, meta, class, weight)
	}
	if err != nil {
		return fmt.Errorf("AddEdge %s -[%s]-> %s: %w", e.Src, e.Kind, e.Dst, err)
	}
	return nil
}

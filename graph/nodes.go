package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Node types supported in V1.
const (
	NodeTypeSourceFile          = "source_file"
	NodeTypeSymbol              = "symbol"
	NodeTypeGoPackage           = "go_package"
	NodeTypeProtoService        = "proto_service"
	NodeTypeProtoMessage        = "proto_message"
	NodeTypeRPCMethod           = "rpc_method"
	NodeTypeGlobularService     = "globular_service"
	NodeTypePackage             = "package"
	NodeTypeWorkflow            = "workflow"
	NodeTypeWorkflowStep        = "workflow_step"
	NodeTypeSystemdUnit         = "systemd_unit"
	NodeTypeEtcdKey             = "etcd_key"
	NodeTypeScyllaTable         = "scylla_table"
	NodeTypeMinioBucket         = "minio_bucket"
	NodeTypeEventType           = "event_type"
	NodeTypeDoctorFinding       = "doctor_finding"
	NodeTypeInvariant           = "invariant"
	NodeTypeFailureMode         = "failure_mode"
	NodeTypeForbiddenFix        = "forbidden_fix"
	NodeTypeTest                = "test"
	NodeTypeRuntimeState        = "runtime_state"
	NodeTypeRemediationWorkflow = "remediation_workflow"
	NodeTypeDependencyPhase     = "dependency_phase"

	// Learning node types (Task 3).
	NodeTypeIncident          = "incident"
	NodeTypeIncidentBundle    = "incident_bundle"
	NodeTypeAwarenessProposal = "awareness_proposal"
	NodeTypeProposalPatch     = "proposal_patch"
	NodeTypeContextAlias      = "context_alias"
	NodeTypeLearningRule      = "learning_rule"
	NodeTypeEvidence          = "evidence"
	NodeTypeManualRepair      = "manual_repair"

	// Experience ledger node types.
	NodeTypeExperience     = "experience"
	NodeTypeGoalPattern    = "goal_pattern"
	NodeTypeStrategy       = "strategy"
	NodeTypeAttempt        = "attempt"
	NodeTypeObservation    = "observation"
	NodeTypeLesson         = "lesson"
	NodeTypeNextTimeHint   = "next_time_hint"
	NodeTypeEvidenceBundle = "evidence_bundle"
	NodeTypeScorecard      = "scorecard"

	// Fix ledger node types (Task 4).
	NodeTypeFixCase              = "fix_case"
	NodeTypeGuardrail            = "guardrail"
	NodeTypeImplementationStatus = "implementation_status"
	NodeTypeRemainingGap         = "remaining_gap"
	NodeTypeTestObligation       = "test_obligation"
	NodeTypeRegressionRisk       = "regression_risk"
	NodeTypeCodeFix              = "code_fix"
	NodeTypeAuditItem            = "audit_item"

	// Protocol annotation node types (Task 8).
	NodeTypeHashSchema       = "hash_schema"
	NodeTypeStateTransition  = "state_transition"
	NodeTypeRiskSurface      = "risk_surface"
	NodeTypeProtocolContract = "protocol_contract"

	// Pattern node type (Task 15).
	NodeTypePattern = "pattern"

	// Design pattern layer node types.
	NodeTypeDesignPattern = "design_pattern"
	NodeTypeAntiPattern   = "anti_pattern"
	NodeTypeCodeSmell     = "code_smell"

	// Design / documentation node types (Task 12).
	NodeTypeArchitectureDecision = "architecture_decision"
	NodeTypeDesignRule           = "design_rule"
	NodeTypeRationale            = "rationale"
	NodeTypeTradeoff             = "tradeoff"
	NodeTypeOperationalPrinciple = "operational_principle"
	NodeTypeRunbook              = "runbook"
	NodeTypeDebugPlaybook        = "debug_playbook"
	NodeTypeDocumentationSection = "documentation_section"
	NodeTypeErrorReport          = "error_report"
	NodeTypeIncidentReport       = "incident_report"
	NodeTypeCommitFix            = "commit_fix"

	// Service design graph node types (Phase 2-8).
	NodeTypeAuthzAnnotation = "authz_annotation" // proto RPC authz option
	NodeTypeStreamingMode   = "streaming_mode"   // client_streaming / server_streaming / bidirectional

	// Runtime bridge node types (Task 6).
	NodeTypeRuntimeSnapshot      = "runtime_snapshot"
	NodeTypeRuntimeServiceStatus = "runtime_service_status"
	NodeTypeWorkflowReceipt      = "workflow_receipt"
	NodeTypeStateDelta           = "state_delta"
	NodeTypeRepositoryStatus     = "repository_status"
	NodeTypeObjectstoreStatus    = "objectstore_status"
	NodeTypeXDSStatus            = "xds_status"
	NodeTypeSystemdStatus        = "systemd_status"
	NodeTypeDoctorEvidence       = "doctor_evidence"

	// Live etcd cluster state node types.
	// These nodes are collected at runtime from etcd and carry TTL metadata.
	NodeTypeEtcdSnapshot          = "etcd_snapshot"
	NodeTypeDesiredService        = "desired_service"
	NodeTypeDesiredInfrastructure = "desired_infrastructure"
	NodeTypeServiceRelease        = "service_release"
	NodeTypeInfrastructureRelease = "infrastructure_release"
	NodeTypeNodeHeartbeat         = "node_heartbeat"
	NodeTypeNodeConvergenceState  = "node_convergence_state"
	NodeTypeNodeInstalledPackage  = "node_installed_package"
	NodeTypeObjectstoreDesired    = "objectstore_desired_state"
	NodeTypeServiceRuntimeConfig  = "service_runtime_config"
	NodeTypeClusterSystemConfig   = "cluster_system_config"

	// Convergence delta node types.
	NodeTypeConvergenceRecord    = "convergence_record"
	NodeTypeDesiredStateRecord   = "desired_state_record"
	NodeTypeInstalledStateRecord = "installed_state_record"
	NodeTypeRuntimeStateRecord   = "runtime_state_record"
	NodeTypeDriftRecord          = "drift_record"
	NodeTypeReleaseAction        = "release_action"
	NodeTypeVerificationRecord   = "verification_record"

	// Metrics node types.
	NodeTypeMetricQuery       = "metric_query"
	NodeTypeMetricThreshold   = "metric_threshold"
	NodeTypeMetricWarningRule = "metric_warning_rule"
	NodeTypeMetricSample      = "metric_sample"
	NodeTypeMetricWarning     = "metric_warning"

	// PKI / certificate node types.
	NodeTypeCertificate          = "certificate"
	NodeTypeCertificateAuthority = "certificate_authority"
	NodeTypeCertSAN              = "certificate_san"
	NodeTypeCertExpiryWarning    = "certificate_expiry_warning"

	// RBAC policy node types.
	NodeTypeRBACPolicyFile  = "rbac_policy_file"
	NodeTypeRBACRole        = "rbac_role"
	NodeTypeRBACPermission  = "rbac_permission"
	NodeTypeRBACSubject     = "rbac_subject"
	NodeTypeRBACBinding     = "rbac_binding"
	NodeTypeServiceIdentity = "service_identity"

	// Workflow execution node types.
	NodeTypeWorkflowRun                = "workflow_run"
	NodeTypeWorkflowStepRun            = "workflow_step_run"
	NodeTypeWorkflowBlockedReason      = "workflow_blocked_reason"
	NodeTypeWorkflowRetryRecord        = "workflow_retry_record"
	NodeTypeWorkflowError              = "workflow_error"
	NodeTypeWorkflowStepEffect         = "workflow_step_effect"
	NodeTypeWorkflowVerificationRecord = "workflow_verification_record"
	NodeTypeWorkflowIntegrityFinding   = "workflow_integrity_finding"

	// DNS / network node types.
	NodeTypeDNSZone         = "dns_zone"
	NodeTypeDNSRecord       = "dns_record"
	NodeTypeServiceEndpoint = "service_endpoint"
	NodeTypeDomainSpec      = "domain_spec"
)

// Node is a vertex in the awareness graph.
type Node struct {
	ID        string
	Type      string
	Name      string
	Path      string
	Summary   string
	Metadata  map[string]any
	CreatedAt int64
	UpdatedAt int64
}

// AddNode upserts a node by ID. Existing nodes are updated in-place.
//
// AddNode is destructive: the upsert replaces every column including
// metadata_json. Callers that only want to STUB a node (so an edge has a
// valid target) and must NOT clobber a richer node already written by
// another extractor MUST use EnsureNode instead. See
// docs/awareness/composed_path_failures.md (2026-05-10 lifecycle metadata
// loss) for the incident that produced this distinction.
func (g *Graph) AddNode(ctx context.Context, n Node) error {
	meta, err := marshalMeta(n.Metadata)
	if err != nil {
		return fmt.Errorf("AddNode %s: %w", n.ID, err)
	}
	now := time.Now().Unix()
	_, err = g.db.ExecContext(ctx, `
		INSERT INTO nodes (id, type, name, path, summary, metadata_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type          = excluded.type,
			name          = excluded.name,
			path          = excluded.path,
			summary       = excluded.summary,
			metadata_json = excluded.metadata_json,
			updated_at    = excluded.updated_at
	`, n.ID, n.Type, n.Name, n.Path, n.Summary, meta, now, now)
	if err != nil {
		return fmt.Errorf("AddNode %s: %w", n.ID, err)
	}
	return nil
}

// EnsureNode inserts a node only if no node with the same ID exists.
// If the node already exists, EnsureNode is a no-op — the existing row's
// metadata, summary, type, and name are preserved.
//
// Use EnsureNode when an extractor needs to make sure an edge target is
// present but does NOT itself own the node's authoritative content. The
// canonical example is "I'm the design_patterns loader and my YAML
// references a failure_mode by id; I want the failure_mode loader's
// metadata to win." Calling AddNode in that situation silently clobbers
// lifecycle hints (deprecated, intentional_gap), as documented in
// docs/awareness/composed_path_failures.md.
//
// If you DO own the node's content (you're the canonical loader), use
// AddNode. The two functions are intentionally distinct so the call site
// expresses the intent.
func (g *Graph) EnsureNode(ctx context.Context, n Node) error {
	meta, err := marshalMeta(n.Metadata)
	if err != nil {
		return fmt.Errorf("EnsureNode %s: %w", n.ID, err)
	}
	now := time.Now().Unix()
	_, err = g.db.ExecContext(ctx, `
		INSERT INTO nodes (id, type, name, path, summary, metadata_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, n.ID, n.Type, n.Name, n.Path, n.Summary, meta, now, now)
	if err != nil {
		return fmt.Errorf("EnsureNode %s: %w", n.ID, err)
	}
	return nil
}

// DeleteNode removes a node and all its edges from the graph.
func (g *Graph) DeleteNode(ctx context.Context, id string) error {
	if _, err := g.db.ExecContext(ctx, `DELETE FROM edges WHERE src = ? OR dst = ?`, id, id); err != nil {
		return fmt.Errorf("DeleteNode edges %s: %w", id, err)
	}
	if _, err := g.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("DeleteNode node %s: %w", id, err)
	}
	return nil
}

// PruneStaleSourceFileNodes removes source_file nodes whose paths no longer
// exist on disk. srcDir is the repo root used to resolve repo-relative paths.
func (g *Graph) PruneStaleSourceFileNodes(ctx context.Context, srcDir string) (int, error) {
	nodes, err := g.FindNodesByType(ctx, NodeTypeSourceFile)
	if err != nil {
		return 0, fmt.Errorf("PruneStale: list nodes: %w", err)
	}
	pruned := 0
	for _, n := range nodes {
		if n.Path == "" {
			continue
		}
		absPath := filepath.Join(srcDir, n.Path)
		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			if err := g.DeleteNode(ctx, n.ID); err != nil {
				return pruned, err
			}
			pruned++
		}
	}
	return pruned, nil
}

// marshalMeta encodes a metadata map to JSON. Returns "{}" for nil.
func marshalMeta(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalMeta decodes a JSON metadata string. Returns nil on empty/invalid input.
func unmarshalMeta(s string) map[string]any {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

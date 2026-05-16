package evidence

import "time"

// FactKind is a stable token identifying the category of a normalized runtime fact.
// Facts are grouped by domain. Classifiers match facts against graph contracts.
type FactKind string

// ── Service runtime ──────────────────────────────────────────────────────────
const (
	FactServiceFailed         FactKind = "SERVICE_FAILED"
	FactPortClosed            FactKind = "PORT_CLOSED"
	FactDependencyUnreachable FactKind = "DEPENDENCY_UNREACHABLE"
	FactStartLimitHit         FactKind = "START_LIMIT_HIT"
)

// ── Systemd runtime proof ────────────────────────────────────────────────────
const (
	FactUnitMissing                  FactKind = "UNIT_MISSING"
	FactUnitRenderingDrift           FactKind = "UNIT_RENDERING_DRIFT"
	FactUnitStartLimitHit            FactKind = "UNIT_START_LIMIT_HIT"
	FactUnitWorkingDirectoryMissing  FactKind = "UNIT_WORKING_DIRECTORY_MISSING"
	FactUnitEnvFileMissing           FactKind = "UNIT_ENV_FILE_MISSING"
	FactUnitExecMissing              FactKind = "UNIT_EXEC_MISSING"
	FactServiceActiveHealthFailed    FactKind = "SERVICE_ACTIVE_HEALTH_FAILED"
	FactServiceProcessRunningPortClosed FactKind = "SERVICE_PROCESS_RUNNING_PORT_CLOSED"
	FactRuntimeHealthMismatch        FactKind = "RUNTIME_HEALTH_MISMATCH"
)

// ── PKI / identity / trust ───────────────────────────────────────────────────
const (
	FactPKIMissing               FactKind = "PKI_MISSING"
	// FactPKIUnreadable: file exists on disk but the collecting process cannot
	// read it. Different remediation from MISSING — either fix ownership/perms
	// or accept that this collector is running as the wrong user (e.g. a CLI
	// invoked by a developer hitting a service-user-only key file).
	FactPKIUnreadable            FactKind = "PKI_UNREADABLE"
	FactPKIExpired               FactKind = "PKI_EXPIRED"
	FactPKISANMismatch           FactKind = "PKI_SAN_MISMATCH"
	FactPKIClusterTrustMismatch  FactKind = "PKI_CLUSTER_TRUST_MISMATCH"
	FactCertificateMissing       FactKind = "CERTIFICATE_MISSING"
	FactCertificateAuthorityDrift FactKind = "CERTIFICATE_AUTHORITY_DRIFT"
	FactMCPReachableButUntrusted FactKind = "MCP_REACHABLE_BUT_UNTRUSTED"
	FactGRPCReachableButUntrusted FactKind = "GRPC_REACHABLE_BUT_UNTRUSTED"
)

// ── Local config ─────────────────────────────────────────────────────────────
const (
	FactConfigMismatch                    FactKind = "CONFIG_MISMATCH"
	FactLocalConfigMissing                FactKind = "LOCAL_CONFIG_MISSING"
	FactLocalConfigStale                  FactKind = "LOCAL_CONFIG_STALE"
	FactLocalConfigDrift                  FactKind = "LOCAL_CONFIG_DRIFT"
	FactLocalConfigConflictsWithEtcd      FactKind = "LOCAL_CONFIG_CONFLICTS_WITH_ETCD"
	FactLocalConfigConflictsWithRelIndex  FactKind = "LOCAL_CONFIG_CONFLICTS_WITH_RELEASE_INDEX"
	FactLocalConfigValidButRuntimeFailed  FactKind = "LOCAL_CONFIG_VALID_BUT_RUNTIME_FAILED"
	FactAuthorityDrift                    FactKind = "AUTHORITY_DRIFT"
)

// ── Scylla topology and readiness ────────────────────────────────────────────
const (
	FactScyllaCQLUnreachable      FactKind = "SCYLLA_CQL_UNREACHABLE"
	FactScyllaServiceFailed       FactKind = "SCYLLA_SERVICE_FAILED"
	FactScyllaSeedConfigDrift     FactKind = "SCYLLA_SEED_CONFIG_DRIFT"
	FactScyllaRPCAddressDrift     FactKind = "SCYLLA_RPC_ADDRESS_DRIFT"
	FactScyllaListenAddressDrift  FactKind = "SCYLLA_LISTEN_ADDRESS_DRIFT"
	FactScyllaClusterNameMismatch FactKind = "SCYLLA_CLUSTER_NAME_MISMATCH"
	FactScyllaDataDirClusterMismatch FactKind = "SCYLLA_DATA_DIR_CLUSTER_MISMATCH"
	FactScyllaConfigAuthorityDrift FactKind = "SCYLLA_CONFIG_AUTHORITY_DRIFT"
	FactScyllaDependencyGateBlocked FactKind = "SCYLLA_DEPENDENCY_GATE_BLOCKED"
	// Legacy alias kept for normalizer compatibility.
	FactDataDirClusterIDMismatch FactKind = "DATA_DIR_CLUSTER_ID_MISMATCH"
	FactChecksumMismatch         FactKind = "CHECKSUM_MISMATCH"
)

// ── MinIO / objectstore topology ─────────────────────────────────────────────
const (
	FactObjectstoreTopologyMissing   FactKind = "OBJECTSTORE_TOPOLOGY_CONTRACT_MISSING"
	FactMinIORunningWithoutContract  FactKind = "MINIO_RUNNING_WITHOUT_CONTRACT"
	FactMinIORunningOutsidePool      FactKind = "MINIO_RUNNING_OUTSIDE_DESIRED_POOL"
	FactMinIORenderedGenerationDrift FactKind = "MINIO_RENDERED_GENERATION_DRIFT"
	FactMinIODistributedConfDrift    FactKind = "MINIO_DISTRIBUTED_CONF_DRIFT"
)

// ── Repository ───────────────────────────────────────────────────────────────
const (
	FactRepoMetadataBackendUnavail    FactKind = "REPOSITORY_METADATA_BACKEND_UNAVAILABLE"
	FactRepoBlobBackendUnavailable    FactKind = "REPOSITORY_BLOB_BACKEND_UNAVAILABLE"
	FactRepoArtifactLedgerMissing     FactKind = "REPOSITORY_ARTIFACT_LEDGER_MISSING"
	FactRepoAwarenessBundleUnavailable FactKind = "REPOSITORY_AWARENESS_BUNDLE_UNAVAILABLE"
	FactRepoDegraded                  FactKind = "REPOSITORY_DEGRADED"
	FactRepoReadOnly                  FactKind = "REPOSITORY_READ_ONLY"
	FactRepoLocalOnly                 FactKind = "REPOSITORY_LOCAL_ONLY"
)

// ── Workflow ─────────────────────────────────────────────────────────────────
const (
	FactWorkflowBackendUnavailable   FactKind = "WORKFLOW_BACKEND_UNAVAILABLE"
	FactWorkflowReceiptsUnwritable   FactKind = "WORKFLOW_RECEIPTS_UNWRITABLE"
	FactWorkflowDefinitionsMissing   FactKind = "WORKFLOW_DEFINITIONS_MISSING"
	FactWorkflowRemediationUnsafe    FactKind = "WORKFLOW_REMEDIATION_UNSAFE"
	FactWorkflowDependencyBlocked    FactKind = "WORKFLOW_DEPENDENCY_BLOCKED"
)

// ── Etcd key authority ───────────────────────────────────────────────────────
const (
	FactEtcdUnreachable                   FactKind = "ETCD_UNREACHABLE"
	FactEtcdAuthorityEmpty                FactKind = "ETCD_AUTHORITY_EMPTY"
	FactEtcdAuthorityPartial              FactKind = "ETCD_AUTHORITY_PARTIAL"
	FactDesiredStateMissing               FactKind = "DESIRED_STATE_MISSING"
	FactDesiredInstalledDivergence        FactKind = "DESIRED_INSTALLED_DIVERGENCE"
	FactDesiredInstalledRuntimeDivergence FactKind = "DESIRED_INSTALLED_RUNTIME_DIVERGENCE"
	FactTopologyContractMissing           FactKind = "TOPOLOGY_CONTRACT_MISSING"
	FactNodeMembershipContradiction       FactKind = "NODE_MEMBERSHIP_CONTRADICTION"
	FactAuthorityLostLocalRuntimeSurvived FactKind = "AUTHORITY_LOST_WITH_LOCAL_RUNTIME_SURVIVED"
)

// ── Installed-state ──────────────────────────────────────────────────────────
const (
	FactInstalledStateMissing           FactKind = "INSTALLED_STATE_MISSING"
	FactInstalledStateStale             FactKind = "INSTALLED_STATE_STALE"
	FactInstalledStateBuildMismatch     FactKind = "INSTALLED_STATE_BUILD_MISMATCH"
	FactInstalledStatePresentRuntimeDead FactKind = "INSTALLED_STATE_PRESENT_RUNTIME_DEAD"
	FactInstalledStatePresentUnitMissing FactKind = "INSTALLED_STATE_PRESENT_UNIT_MISSING"
	FactInstalledStatePresentBinaryMissing FactKind = "INSTALLED_STATE_PRESENT_BINARY_MISSING"
)

// ── Release-index / BOM ──────────────────────────────────────────────────────
const (
	FactReleaseIndexMissing        FactKind = "RELEASE_INDEX_MISSING"
	FactReleaseIndexStale          FactKind = "RELEASE_INDEX_STALE"
	FactReleaseIndexAuthorityLost  FactKind = "RELEASE_INDEX_AUTHORITY_LOST"
	FactPackageBuildMismatch       FactKind = "PACKAGE_BUILD_MISMATCH"
	FactAwarenessBundleMissing     FactKind = "AWARENESS_BUNDLE_MISSING"
	FactAwarenessBundleStale       FactKind = "AWARENESS_BUNDLE_STALE"
	FactAwarenessBundleMismatch    FactKind = "AWARENESS_BUNDLE_MISMATCH"
	FactBinaryChecksumMismatch     FactKind = "BINARY_CHECKSUM_MISMATCH"
)

// ── xDS / gateway routing ────────────────────────────────────────────────────
const (
	FactGatewayBootstrapMissing       FactKind = "GATEWAY_BOOTSTRAP_MISSING"
	FactGatewayProcessRunningRouteBroken FactKind = "GATEWAY_PROCESS_RUNNING_ROUTE_BROKEN"
	FactXDSNotApplied                 FactKind = "XDS_NOT_APPLIED"
	FactXDSGenerationStale            FactKind = "XDS_GENERATION_STALE"
	FactRouteEndpointUnhealthy        FactKind = "ROUTE_ENDPOINT_UNHEALTHY"
	FactRouteTLSTrustFailed           FactKind = "ROUTE_TLS_TRUST_FAILED"
)

// ── MCP reachability and trust ───────────────────────────────────────────────
const (
	FactMCPUnreachable                    FactKind = "MCP_UNREACHABLE"
	FactMCPReachableUntrusted             FactKind = "MCP_REACHABLE_UNTRUSTED"
	FactMCPReachableAwarenessBundleMissing FactKind = "MCP_REACHABLE_AWARENESS_BUNDLE_MISSING"
	FactMCPReachableAwarenessReady        FactKind = "MCP_REACHABLE_AWARENESS_READY"
	FactMCPRuntimeSnapshotFailed          FactKind = "MCP_RUNTIME_SNAPSHOT_FAILED"
)

// ── Severity ─────────────────────────────────────────────────────────────────

// Severity of a runtime fact.
type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
)

// ── Phase ─────────────────────────────────────────────────────────────────────

// Phase during which a fact was observed.
type Phase string

const (
	PhaseDAY0 Phase = "DAY0"
	PhaseDAY1 Phase = "DAY1"
)

// ── RuntimeFact ───────────────────────────────────────────────────────────────

// RuntimeFact is a normalized, structured observation derived from raw runtime evidence.
// Raw logs and systemd state are inputs; RuntimeFact is the output Awareness reasons about.
//
// The fact pipeline: collect (raw) → normalize (fact) → classify (verdict) → expose (MCP/CLI)
type RuntimeFact struct {
	Kind        FactKind  `json:"kind"`
	NodeID      string    `json:"node_id"`
	Service     string    `json:"service,omitempty"`
	Port        int       `json:"port,omitempty"`
	Phase       Phase     `json:"phase"`
	Severity    Severity  `json:"severity"`
	Blocks      []string  `json:"blocks,omitempty"`
	Confidence  float64   `json:"confidence"` // 0.0–1.0
	Timestamp   time.Time `json:"timestamp"`
	EvidenceRef string    `json:"evidence_ref,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

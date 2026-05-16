package evidence

// Day1ReadinessLevel is a step in the 14-level Day-1 readiness ladder.
// Each level must be satisfied before the next can be reached.
type Day1ReadinessLevel string

const (
	LevelNodeSeen             Day1ReadinessLevel = "NODE_SEEN"
	LevelMCPReachable         Day1ReadinessLevel = "MCP_REACHABLE"
	LevelMCPTrusted           Day1ReadinessLevel = "MCP_TRUSTED"
	LevelAwarenessReady       Day1ReadinessLevel = "AWARENESS_READY"
	LevelPackageBOMReady      Day1ReadinessLevel = "PACKAGE_BOM_READY"
	LevelEtcdMemberReady      Day1ReadinessLevel = "ETCD_MEMBER_READY"
	LevelLocalRuntimeObserved Day1ReadinessLevel = "LOCAL_RUNTIME_OBSERVED"
	LevelPKIReady             Day1ReadinessLevel = "PKI_READY"
	LevelScyllaReady          Day1ReadinessLevel = "SCYLLA_READY"
	LevelObjectstoreReady     Day1ReadinessLevel = "OBJECTSTORE_READY"
	LevelGatewayReady         Day1ReadinessLevel = "GATEWAY_READY"
	LevelWorkflowReady        Day1ReadinessLevel = "WORKFLOW_READY"
	LevelWorkloadReady        Day1ReadinessLevel = "WORKLOAD_READY"
	LevelDay1Complete         Day1ReadinessLevel = "DAY1_COMPLETE"
)

// Day1ReadinessLadder is the canonical ordered sequence of readiness levels.
var Day1ReadinessLadder = []Day1ReadinessLevel{
	LevelNodeSeen,
	LevelMCPReachable,
	LevelMCPTrusted,
	LevelAwarenessReady,
	LevelPackageBOMReady,
	LevelEtcdMemberReady,
	LevelLocalRuntimeObserved,
	LevelPKIReady,
	LevelScyllaReady,
	LevelObjectstoreReady,
	LevelGatewayReady,
	LevelWorkflowReady,
	LevelWorkloadReady,
	LevelDay1Complete,
}

// Day1Classification describes why a node is blocked or how it failed.
type Day1Classification string

const (
	ClassHealthy                    Day1Classification = "HEALTHY"
	ClassJoinedButDependencyBlocked Day1Classification = "JOINED_BUT_DEPENDENCY_BLOCKED"
	ClassAwarenessBundleMissing            Day1Classification = "AWARENESS_BUNDLE_MISSING"
	ClassAwarenessBundleMismatch           Day1Classification = "AWARENESS_BUNDLE_MISMATCH"
	ClassAwarenessBundleStale              Day1Classification = "AWARENESS_BUNDLE_STALE"
	ClassAwarenessBundleSchemaUnsupported  Day1Classification = "AWARENESS_BUNDLE_SCHEMA_UNSUPPORTED"
	ClassAwarenessBundleVerifyFailed       Day1Classification = "AWARENESS_BUNDLE_VERIFY_FAILED"
	ClassAwarenessBundleSourceUnavailable  Day1Classification = "AWARENESS_BUNDLE_SOURCE_UNAVAILABLE"
	ClassScyllaNotReady             Day1Classification = "SCYLLA_NOT_READY"
	ClassScyllaConfigAuthorityDrift Day1Classification = "SCYLLA_CONFIG_AUTHORITY_DRIFT"
	ClassObjectstoreNotReady        Day1Classification = "OBJECTSTORE_NOT_READY"
	ClassGatewayNotReady            Day1Classification = "GATEWAY_NOT_READY"
	ClassWorkflowRemediationUnsafe  Day1Classification = "WORKFLOW_REMEDIATION_UNSAFE"
	ClassPKIMissing                 Day1Classification = "PKI_MISSING"
	ClassPKIUnreadable              Day1Classification = "PKI_UNREADABLE"
	ClassMCPReachableButUntrusted   Day1Classification = "MCP_REACHABLE_BUT_UNTRUSTED"
	ClassServiceFailed              Day1Classification = "SERVICE_FAILED"
	ClassEtcdNotReady               Day1Classification = "ETCD_NOT_READY"
	ClassAuthorityLostRuntimeSurvived Day1Classification = "AUTHORITY_LOST_WITH_LOCAL_RUNTIME_SURVIVED"
	ClassUnknown                    Day1Classification = "UNKNOWN"
)

// Day1Verdict is the classifier output: what the node's current state means, what is safe to do,
// and what is forbidden.
type Day1Verdict struct {
	NodeID           string                      `json:"node_id"`
	Phase            Phase                       `json:"phase"`
	// Verdict is PASS, BLOCK, WARN, or UNKNOWN.
	Verdict          string                      `json:"verdict"`
	Classification   Day1Classification          `json:"classification"`
	PrimaryBlocker   string                      `json:"primary_blocker,omitempty"`
	Readiness        map[Day1ReadinessLevel]bool `json:"readiness"`
	BlockedServices  []string                    `json:"blocked_services,omitempty"`
	AllowedActions   []string                    `json:"allowed_actions,omitempty"`
	ForbiddenActions []string                    `json:"forbidden_actions,omitempty"`
	Evidence         []RuntimeFact               `json:"evidence,omitempty"`
}

// HighestReachedLevel returns the highest readiness level that is true in the verdict,
// stopping at the first false level in ladder order.
func (v *Day1Verdict) HighestReachedLevel() Day1ReadinessLevel {
	highest := Day1ReadinessLevel("")
	for _, level := range Day1ReadinessLadder {
		if v.Readiness[level] {
			highest = level
		} else {
			break
		}
	}
	return highest
}

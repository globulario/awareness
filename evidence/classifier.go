package evidence

// Classifier matches normalized RuntimeFacts against graph contracts
// to produce a Day1Verdict. It is read-only — it never mutates state.
//
// Pattern: Read graph contract → collect runtime evidence → compare → identify broken relationship
//          → emit precise classification → emit allowed and forbidden actions.
type Classifier struct{}

// Classify produces a Day1Verdict from a normalized NodeRuntimeSnapshot.
// The snapshot's Facts field must already be populated by the Normalizer.
func (c *Classifier) Classify(snap *NodeRuntimeSnapshot) *Day1Verdict {
	v := &Day1Verdict{
		NodeID:   snap.NodeID,
		Phase:    snap.Phase,
		Readiness: make(map[Day1ReadinessLevel]bool),
		Evidence:  snap.Facts,
	}

	// Walk the ladder in order, setting each level.
	// Level 1: NODE_SEEN — always true if we can produce a snapshot.
	v.Readiness[LevelNodeSeen] = true

	// Level 2: MCP_REACHABLE — we can't test this locally; assume true if collector ran.
	v.Readiness[LevelMCPReachable] = true

	// Level 3: MCP_TRUSTED — no MCP trust facts detected (PKI present implies local TLS is possible).
	mcpUntrusted := hasFact(snap.Facts, FactMCPReachableButUntrusted, "") ||
		hasFact(snap.Facts, FactMCPReachableUntrusted, "")
	v.Readiness[LevelMCPTrusted] = !mcpUntrusted

	// Level 4: AWARENESS_READY — bundle present, loaded, and exactly matches
	// the release-index. Any of MISMATCH (version drift), STALE (build_id
	// drift on matching version), or non-LOADED status fails readiness here.
	// This is the gate the freshness spec calls out: a node may not report
	// AWARENESS_READY unless the bundle matches what the cluster expects to
	// be running, and architecture-sensitive classification (Scylla, etcd,
	// etc.) below depends on this gate to be honest.
	bundleOK := snap.AwarenessBundle.Present &&
		snap.AwarenessBundle.Status == "LOADED" &&
		!hasFact(snap.Facts, FactAwarenessBundleMismatch, "awareness") &&
		!hasFact(snap.Facts, FactAwarenessBundleStale, "awareness")
	v.Readiness[LevelAwarenessReady] = bundleOK

	// Level 5: PACKAGE_BOM_READY — release index must exist.
	v.Readiness[LevelPackageBOMReady] = snap.Release.Version != "" &&
		!hasFact(snap.Facts, FactReleaseIndexMissing, "")

	// Level 6: ETCD_MEMBER_READY — etcd active and port 2379 reachable.
	etcdReady := !hasFact(snap.Facts, FactEtcdUnreachable, "etcd") &&
		portReady(snap, 2379, "etcd")
	v.Readiness[LevelEtcdMemberReady] = etcdReady

	// Level 7: LOCAL_RUNTIME_OBSERVED — collector produced service observations.
	v.Readiness[LevelLocalRuntimeObserved] = len(snap.Services) > 0

	// Level 8: PKI_READY — neither PKI_MISSING nor PKI_UNREADABLE. Both block
	// the service from completing mTLS; the verdict distinguishes them so the
	// remediation hint differs (re-issue vs check ownership / run as service user).
	pkiReady := !hasFact(snap.Facts, FactPKIMissing, "pki") &&
		!hasFact(snap.Facts, FactPKIUnreadable, "pki")
	v.Readiness[LevelPKIReady] = pkiReady

	// Level 9: SCYLLA_READY — no Scylla CQL unreachable fact AND port 9042 listening.
	// Graph contract: workflow, event, resource, repository depend on Scylla.
	scyllaFailed := hasFact(snap.Facts, FactScyllaCQLUnreachable, "scylla") ||
		hasFact(snap.Facts, FactScyllaCQLUnreachable, "scylla-server") ||
		hasFact(snap.Facts, FactScyllaServiceFailed, "scylla") ||
		hasFact(snap.Facts, FactServiceFailed, "scylla") ||
		hasFact(snap.Facts, FactServiceFailed, "scylla-server")
	scyllaPortOK := portReady(snap, 9042, "scylla", "scylla-server")
	scyllaReady := !scyllaFailed && scyllaPortOK
	v.Readiness[LevelScyllaReady] = scyllaReady

	// Level 10: OBJECTSTORE_READY — no topology-missing or service-failed fact for minio.
	objectstoreFailed := hasFact(snap.Facts, FactObjectstoreTopologyMissing, "minio") ||
		hasFact(snap.Facts, FactServiceFailed, "minio")
	objectstorePortOK := portReady(snap, 9000, "minio")
	objectstoreReady := !objectstoreFailed && objectstorePortOK
	v.Readiness[LevelObjectstoreReady] = objectstoreReady

	// Level 11: GATEWAY_READY — envoy not failed.
	gatewayFailed := hasFact(snap.Facts, FactServiceFailed, "envoy") ||
		hasFact(snap.Facts, FactGatewayBootstrapMissing, "envoy")
	v.Readiness[LevelGatewayReady] = !gatewayFailed

	// Level 12: WORKFLOW_READY — workflow process not in WORKFLOW_REMEDIATION_UNSAFE or
	// WORKFLOW_DEPENDENCY_BLOCKED state. Workflow requires Scylla.
	// If Scylla is not ready, workflow remediation is unsafe (graph contract enforced).
	workflowUnsafe := hasFact(snap.Facts, FactWorkflowRemediationUnsafe, "globular-workflow") ||
		hasFact(snap.Facts, FactWorkflowDependencyBlocked, "globular-workflow") ||
		!scyllaReady
	v.Readiness[LevelWorkflowReady] = !workflowUnsafe

	// Level 13: WORKLOAD_READY — all infra services ready.
	workloadReady := scyllaReady && objectstoreReady && !gatewayFailed &&
		etcdReady && bundleOK && pkiReady
	v.Readiness[LevelWorkloadReady] = workloadReady

	// Level 14: DAY1_COMPLETE.
	v.Readiness[LevelDay1Complete] = workloadReady

	// Determine primary classification and verdict.
	v.Classification, v.PrimaryBlocker = c.classify(snap, v)

	if v.Readiness[LevelDay1Complete] {
		v.Verdict = "PASS"
	} else if v.Classification == ClassUnknown {
		v.Verdict = "UNKNOWN"
	} else {
		v.Verdict = "BLOCK"
	}

	c.populateActions(v)

	// Populate blocked services from critical/high facts.
	for _, f := range snap.Facts {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			v.BlockedServices = append(v.BlockedServices, f.Blocks...)
		}
	}
	v.BlockedServices = dedup(v.BlockedServices)

	return v
}

func (c *Classifier) classify(snap *NodeRuntimeSnapshot, v *Day1Verdict) (Day1Classification, string) {
	// Check in ladder order — first failing gate wins. The bundle-related
	// gates come BEFORE every architecture-sensitive gate (Scylla / Etcd /
	// Workflow / Objectstore / Gateway): a stale or missing graph cannot be
	// trusted to classify those failures, so we surface the bundle problem
	// first and block the architecture verdicts entirely.
	if !v.Readiness[LevelMCPTrusted] {
		return ClassMCPReachableButUntrusted, "MCP reachable but certificate not trusted"
	}
	// Missing bundle takes precedence over mismatch/stale: there's nothing to
	// even compare against the release-index until a bundle is installed.
	if !snap.AwarenessBundle.Present {
		return ClassAwarenessBundleMissing, "awareness bundle not installed"
	}
	if hasFact(snap.Facts, FactAwarenessBundleMismatch, "awareness") {
		return ClassAwarenessBundleMismatch, "awareness bundle version does not match release-index"
	}
	if hasFact(snap.Facts, FactAwarenessBundleStale, "awareness") {
		return ClassAwarenessBundleStale, "awareness bundle build_id drifted from release-index (same release line, older build)"
	}
	if !v.Readiness[LevelPKIReady] {
		// Missing is strictly worse than unreadable; missing wins when both
		// would apply, in line with the normalizer's emission order.
		if hasFact(snap.Facts, FactPKIMissing, "pki") {
			return ClassPKIMissing, "PKI artifacts missing (CA cert, node cert, or private key)"
		}
		return ClassPKIUnreadable, "PKI artifacts present but unreadable by collecting process " +
			"(check file ownership / verify collector is running as the service user)"
	}
	if !v.Readiness[LevelEtcdMemberReady] {
		// Check if local runtime survived despite missing etcd authority.
		if len(snap.Services) > 0 {
			return ClassAuthorityLostRuntimeSurvived, "etcd authority unavailable but local runtime observed"
		}
		return ClassEtcdNotReady, "etcd not reachable on port 2379"
	}
	if hasFact(snap.Facts, FactScyllaConfigAuthorityDrift, "scylla") {
		return ClassScyllaConfigAuthorityDrift, "scylla.yaml seed list conflicts with expected topology"
	}
	if !v.Readiness[LevelScyllaReady] {
		return ClassJoinedButDependencyBlocked, "SCYLLA_NOT_READY"
	}
	if hasFact(snap.Facts, FactWorkflowRemediationUnsafe, "globular-workflow") {
		return ClassWorkflowRemediationUnsafe, "workflow depends on Scylla; Scylla not ready"
	}
	if !v.Readiness[LevelObjectstoreReady] {
		return ClassObjectstoreNotReady, "minio not ready on port 9000"
	}
	if !v.Readiness[LevelGatewayReady] {
		return ClassGatewayNotReady, "envoy not active"
	}
	for _, f := range snap.Facts {
		if f.Kind == FactServiceFailed || f.Kind == FactStartLimitHit || f.Kind == FactUnitStartLimitHit {
			return ClassServiceFailed, f.Service + " " + string(f.Kind)
		}
	}
	if v.Readiness[LevelDay1Complete] {
		return ClassHealthy, ""
	}
	return ClassUnknown, "unknown blocker"
}

func (c *Classifier) populateActions(v *Day1Verdict) {
	switch v.Classification {
	case ClassAwarenessBundleMissing, ClassMCPReachableButUntrusted:
		v.AllowedActions = []string{
			"fetch awareness bundle from repository",
			"fetch awareness bundle from gateway fallback",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"dispatch workloads",
		}

	case ClassAwarenessBundleMismatch:
		v.AllowedActions = []string{
			"fetch updated awareness bundle matching release-index version",
		}
		v.ForbiddenActions = []string{
			"mark node AWARENESS_READY with mismatched bundle",
			"mark node DAY1_COMPLETE",
			"classify architecture-sensitive failures using mismatched bundle",
		}

	case ClassAwarenessBundleStale:
		// Same release line, older build_id. The fix is the same as MISMATCH —
		// re-sync from a trusted source — but the framing for operators is
		// different ("CI moved on, you didn't") so the actions surface it.
		v.AllowedActions = []string{
			"sync awareness bundle from trusted source",
			"globular awareness sync --from <trusted peer>",
		}
		v.ForbiddenActions = []string{
			"mark node AWARENESS_READY with stale bundle",
			"mark node DAY1_COMPLETE",
			"classify architecture-sensitive failures using stale bundle",
		}

	case ClassAwarenessBundleSchemaUnsupported:
		// Bundle is for a newer schema than this binary supports. Remediation
		// is "upgrade the binary," not "fetch a different bundle."
		v.AllowedActions = []string{
			"upgrade awareness/MCP binary to a version supporting the bundle schema",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"install bundle whose schema is not supported",
			"mark node AWARENESS_READY",
			"mark node DAY1_COMPLETE",
		}

	case ClassAwarenessBundleVerifyFailed:
		v.AllowedActions = []string{
			"sync awareness bundle from a trusted source",
			"verify bundle integrity (sha256, signature, tar safety)",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"install unverified bundle",
			"mark node AWARENESS_READY after failed verification",
			"mark node DAY1_COMPLETE",
		}

	case ClassAwarenessBundleSourceUnavailable:
		v.AllowedActions = []string{
			"retry trusted sources with bounded backoff",
			"escalate to operator if no source becomes available",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"pull bundle from untrusted source",
			"mark node AWARENESS_READY without a verified bundle",
			"mark node DAY1_COMPLETE",
		}

	case ClassPKIMissing:
		v.AllowedActions = []string{
			"request certificate issuance from cluster CA",
			"collect PKI diagnostics",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"accept gRPC connections without mTLS",
		}

	case ClassPKIUnreadable:
		// Files exist; remediation is permissions/ownership or running context,
		// NOT re-issuance. Misdiagnosing this as MISSING wastes the CA and can
		// rotate a healthy cert for no reason.
		v.AllowedActions = []string{
			"verify PKI file ownership (expect globular:globular)",
			"verify collector is running as the service user",
			"collect PKI diagnostics",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"re-issue PKI artifacts (files exist; rotation would be unnecessary)",
		}

	case ClassJoinedButDependencyBlocked, ClassScyllaNotReady:
		v.AllowedActions = []string{
			"collect Scylla diagnostics",
			"run bounded Scylla diagnosis workflow",
			"hold dependent workloads",
			"emit finding",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"dispatch Scylla-backed workloads",
			"retry dependent services indefinitely",
			"wipe Scylla data automatically",
		}

	case ClassScyllaConfigAuthorityDrift:
		v.AllowedActions = []string{
			"read etcd topology to determine correct seed list",
			"re-render scylla.yaml from etcd topology",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"restart Scylla with local config authority (may propagate wrong seed list)",
			"mark node DAY1_COMPLETE",
		}

	case ClassWorkflowRemediationUnsafe:
		v.AllowedActions = []string{
			"run workflow-backed repair only if it does NOT require Scylla durability",
			"collect diagnostics",
			"wait for Scylla to become ready",
		}
		v.ForbiddenActions = []string{
			"run workflow-backed destructive repair while workflow backend is unhealthy",
			"mark workflow-ready based only on process status",
			"retry workflow indefinitely when dependency is blocked",
		}

	case ClassEtcdNotReady:
		v.AllowedActions = []string{
			"collect etcd diagnostics",
			"check etcd cluster health",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"modify etcd data directly",
		}

	case ClassAuthorityLostRuntimeSurvived:
		v.AllowedActions = []string{
			"preserve current local evidence",
			"propose recovery plan for operator review",
			"collect diagnostics",
		}
		v.ForbiddenActions = []string{
			"reinstall blindly without operator approval",
			"wipe local runtime state",
			"mark node DAY1_COMPLETE",
		}

	case ClassObjectstoreNotReady:
		v.AllowedActions = []string{
			"collect MinIO diagnostics",
			"run bounded MinIO diagnosis workflow",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
			"dispatch package distribution without MinIO topology contract",
		}

	case ClassGatewayNotReady:
		v.AllowedActions = []string{
			"collect xDS/gateway diagnostics",
			"verify xDS bootstrap config exists",
		}
		v.ForbiddenActions = []string{
			"mark gateway ready based only on process status",
			"mark node DAY1_COMPLETE",
		}

	case ClassHealthy:
		v.AllowedActions = []string{"all"}

	default:
		v.AllowedActions = []string{
			"collect diagnostics",
			"escalate to operator",
		}
		v.ForbiddenActions = []string{
			"mark node DAY1_COMPLETE",
		}
	}
}

// portListening returns true if port p appears in obs with Listening=true.
// When the port was not observed at all, it returns false (unknown is NOT ready).
// Use portReady when you also want to allow "service not expected on this node"
// to pass without a port observation.
func portListening(obs []PortObservation, port int) bool {
	for _, o := range obs {
		if o.Port == port {
			return o.Listening
		}
	}
	return false
}

// portReady decides whether a port should be treated as ready for a Day-1 gate.
//
// Decision rules:
//  1. Port observed with Listening=true  → ready.
//  2. Port observed with Listening=false → not ready.
//  3. Port NOT observed AND any of expectedServices appears in snap.Services
//     → not ready (we expected the service here; absence of observation is
//     treated as "not ready", not "fine").
//  4. Port NOT observed AND none of expectedServices is on this node
//     → ready (the service is not expected to run here, so the missing
//     observation is correct, not a blocker).
//
// This closes the previous false-ready hole where unobserved ports always
// returned true and silently passed Day-1 readiness gates.
func portReady(snap *NodeRuntimeSnapshot, port int, expectedServices ...string) bool {
	for _, o := range snap.Ports {
		if o.Port == port {
			return o.Listening
		}
	}
	// Port not observed — decide based on whether the service is expected here.
	if serviceExpected(snap.Services, expectedServices...) {
		return false
	}
	return true
}

// serviceExpected returns true when any of names appears in obs by Name or UnitName.
func serviceExpected(obs []ServiceObservation, names ...string) bool {
	if len(obs) == 0 || len(names) == 0 {
		return false
	}
	for _, o := range obs {
		for _, n := range names {
			if n == "" {
				continue
			}
			if o.Name == n || o.UnitName == n {
				return true
			}
		}
	}
	return false
}

// dedup returns a deduplicated copy of strings.
func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

package evidence

import (
	"strings"
	"time"
)

// Normalizer converts raw ServiceObservations and PortObservations into structured RuntimeFacts.
// The normalizer is stateless and deterministic.
//
// Pipeline: collector (raw) → Normalizer (facts) → Classifier (verdict) → MCP/CLI (output)
type Normalizer struct{}

// Normalize derives RuntimeFacts from a NodeRuntimeSnapshot's raw observations.
// The returned facts slice should be assigned to snap.Facts by the caller.
func (n *Normalizer) Normalize(snap *NodeRuntimeSnapshot) []RuntimeFact {
	var facts []RuntimeFact
	now := time.Now().UTC()
	nodeID := snap.NodeID

	facts = append(facts, n.normalizeBundleStatus(snap, nodeID, now)...)
	facts = append(facts, n.normalizeReleaseIndex(snap, nodeID, now)...)
	facts = append(facts, n.normalizeBundleVersionMatch(snap, nodeID, now)...)
	facts = append(facts, n.normalizePKI(snap, nodeID, now)...)
	facts = append(facts, n.normalizeScyllaConfig(snap, nodeID, now)...)
	facts = append(facts, n.normalizeServices(snap, nodeID, now)...)
	facts = append(facts, n.normalizePorts(snap, nodeID, now)...)
	// Workflow normalization must run after services+ports so it can see Scylla facts.
	facts = append(facts, n.normalizeWorkflowWithFacts(snap, facts, nodeID, now)...)

	return facts
}

// ── bundle status ────────────────────────────────────────────────────────────

func (n *Normalizer) normalizeBundleStatus(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	var facts []RuntimeFact
	b := snap.AwarenessBundle
	if !b.Present || b.Status == "MISSING" {
		facts = append(facts, RuntimeFact{
			Kind:       FactAwarenessBundleMissing,
			NodeID:     nodeID,
			Service:    "awareness",
			Phase:      snap.Phase,
			Severity:   SeverityHigh,
			Blocks:     []string{"mcp", "day1"},
			Confidence: 1.0,
			Timestamp:  now,
			Detail:     "awareness bundle not installed at " + awarenessCurrentLink,
		})
	}
	if b.Status == "STALE" {
		facts = append(facts, RuntimeFact{
			Kind:      FactAwarenessBundleStale,
			NodeID:    nodeID,
			Service:   "awareness",
			Phase:     snap.Phase,
			Severity:  SeverityMedium,
			Confidence: 0.9,
			Timestamp: now,
			Detail:    "awareness bundle present but marked stale",
		})
	}
	return facts
}

// ── release-index ────────────────────────────────────────────────────────────

func (n *Normalizer) normalizeReleaseIndex(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	// Only emit MISSING when the file truly isn't there. A present-but-empty
	// payload (e.g. version field renamed upstream) is a different shape and
	// produces no fact here — silence is honest until we add a dedicated kind.
	if snap.Release.Present {
		return nil
	}
	return []RuntimeFact{{
		Kind:        FactReleaseIndexMissing,
		NodeID:      nodeID,
		Phase:       snap.Phase,
		Severity:    SeverityHigh,
		Confidence:  1.0,
		Timestamp:   now,
		EvidenceRef: "file:" + releaseIndexPath,
		Detail:      "release-index.json not found",
	}}
}

// ── bundle ↔ release-index version match ────────────────────────────────────

func (n *Normalizer) normalizeBundleVersionMatch(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	b := snap.AwarenessBundle
	r := snap.Release
	if !b.Present || r.Version == "" {
		return nil // can't compare
	}
	if b.Version != "" && r.Version != "" && b.Version != r.Version {
		return []RuntimeFact{{
			Kind:        FactAwarenessBundleMismatch,
			NodeID:      nodeID,
			Service:     "awareness",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Confidence:  1.0,
			Timestamp:   now,
			EvidenceRef: "manifest+release-index",
			Detail:      "bundle.version=" + b.Version + " != release-index.version=" + r.Version,
		}}
	}
	// Build_id drift on a matching version is STALE (same release line, behind
	// on CI build) — distinct from MISMATCH where the version itself differs.
	// The freshness spec calls this out explicitly: operators read STALE as
	// "the build pipeline moved on and we haven't" while MISMATCH means
	// "wrong release was installed."
	if b.BuildID != "" && r.BuildID != "" && b.BuildID != r.BuildID {
		return []RuntimeFact{{
			Kind:        FactAwarenessBundleStale,
			NodeID:      nodeID,
			Service:     "awareness",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Confidence:  1.0,
			Timestamp:   now,
			EvidenceRef: "manifest+release-index",
			Detail:      "bundle.build_id=" + b.BuildID + " != release-index.build_id=" + r.BuildID + " (same version, older build)",
		}}
	}
	return nil
}

// ── PKI ──────────────────────────────────────────────────────────────────────

func (n *Normalizer) normalizePKI(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	pki := snap.PKI

	// Missing wins over unreadable: a vanished file is a strictly worse
	// state than a permission problem, and the remediations don't overlap.
	if missingPath := firstMissingPKIPath(pki); missingPath != "" {
		return []RuntimeFact{{
			Kind:        FactPKIMissing,
			NodeID:      nodeID,
			Service:     "pki",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Blocks:      []string{"mcp", "grpc", "mesh", "node-agent"},
			Confidence:  1.0,
			Timestamp:   now,
			EvidenceRef: "file:" + missingPath,
			Detail:      "PKI artifact missing: " + missingPath,
		}}
	}
	if unreadablePath := firstUnreadablePKIPath(pki); unreadablePath != "" {
		return []RuntimeFact{{
			Kind:        FactPKIUnreadable,
			NodeID:      nodeID,
			Service:     "pki",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Blocks:      []string{"mcp", "grpc", "mesh", "node-agent"},
			Confidence:  1.0,
			Timestamp:   now,
			EvidenceRef: "file:" + unreadablePath,
			Detail: "PKI artifact present but not readable by collecting process: " +
				unreadablePath +
				" (check file ownership or verify collector is running as the service user)",
		}}
	}
	return nil
}

// firstMissingPKIPath returns the path of the first PKI artifact that is
// absent from disk, or "" if all three exist. Stable order: CA → cert → key.
func firstMissingPKIPath(pki PKIObservation) string {
	switch {
	case !pki.CACertPresent:
		return pkiCACertPath
	case !pki.NodeCertPresent:
		return pkiNodeCertPath
	case !pki.NodeKeyPresent:
		return pkiNodeKeyPath
	}
	return ""
}

// firstUnreadablePKIPath returns the path of the first PKI artifact that is
// present on disk but not readable by the collecting process. Only called
// after firstMissingPKIPath has confirmed all three are present.
func firstUnreadablePKIPath(pki PKIObservation) string {
	switch {
	case !pki.CACertReadable:
		return pkiCACertPath
	case !pki.NodeCertReadable:
		return pkiNodeCertPath
	case !pki.NodeKeyReadable:
		return pkiNodeKeyPath
	}
	return ""
}

// ── Scylla config authority ───────────────────────────────────────────────────

func (n *Normalizer) normalizeScyllaConfig(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	if !snap.ScyllaConfig.Present {
		return nil // No scylla.yaml — Scylla may not be expected on this node.
	}
	// If Scylla is running but config has no seeds, emit AUTHORITY_DRIFT.
	if len(snap.ScyllaConfig.Seeds) == 0 {
		return []RuntimeFact{{
			Kind:        FactScyllaConfigAuthorityDrift,
			NodeID:      nodeID,
			Service:     "scylla",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Confidence:  0.7,
			Timestamp:   now,
			EvidenceRef: "file:" + scyllaConfigPath,
			Detail:      "scylla.yaml has no seed_provider seeds",
		}}
	}
	return nil
}

// ── Service-level facts from systemd ─────────────────────────────────────────

func (n *Normalizer) normalizeServices(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	portMap := buildPortMap(snap.Ports)
	var facts []RuntimeFact

	for _, svc := range snap.Services {
		switch svc.ActiveState {
		case "failed":
			kind, severity, blocks := serviceFailKind(svc.UnitName, portMap)
			facts = append(facts, RuntimeFact{
				Kind:        kind,
				NodeID:      nodeID,
				Service:     svc.Name,
				Phase:       snap.Phase,
				Severity:    severity,
				Blocks:      blocks,
				Confidence:  1.0,
				Timestamp:   now,
				EvidenceRef: "systemd:" + svc.UnitName,
				Detail:      "ActiveState=failed SubState=" + svc.SubState,
			})

		case "inactive":
			if isCriticalUnit(svc.UnitName) {
				facts = append(facts, RuntimeFact{
					Kind:        FactServiceFailed,
					NodeID:      nodeID,
					Service:     svc.Name,
					Phase:       snap.Phase,
					Severity:    SeverityMedium,
					Confidence:  0.8,
					Timestamp:   now,
					EvidenceRef: "systemd:" + svc.UnitName,
					Detail:      "expected active but is inactive",
				})
			}
		}

		// start-limit-hit from SubState (even when ActiveState is "failed").
		if svc.SubState == "start-limit-hit" || svc.SubState == "auto-restart-queue" {
			if !hasFact(facts, FactUnitStartLimitHit, svc.Name) {
				facts = append(facts, RuntimeFact{
					Kind:        FactUnitStartLimitHit,
					NodeID:      nodeID,
					Service:     svc.Name,
					Phase:       snap.Phase,
					Severity:    SeverityHigh,
					Blocks:      serviceBlocks(svc.Name),
					Confidence:  1.0,
					Timestamp:   now,
					EvidenceRef: "systemd:" + svc.UnitName,
					Detail:      "SubState=" + svc.SubState,
				})
			}
			// Legacy alias for compatibility.
			if !hasFact(facts, FactStartLimitHit, svc.Name) {
				facts = append(facts, RuntimeFact{
					Kind:        FactStartLimitHit,
					NodeID:      nodeID,
					Service:     svc.Name,
					Phase:       snap.Phase,
					Severity:    SeverityHigh,
					Blocks:      serviceBlocks(svc.Name),
					Confidence:  1.0,
					Timestamp:   now,
					EvidenceRef: "systemd:" + svc.UnitName,
					Detail:      "SubState=" + svc.SubState,
				})
			}
		}

		// SERVICE_ACTIVE_HEALTH_FAILED: systemd says active but expected port is closed.
		if svc.ActiveState == "active" {
			if port := expectedPort(svc.Name); port > 0 {
				listening, observed := portMap[port]
				if observed && !listening {
					facts = append(facts, RuntimeFact{
						Kind:        FactServiceActiveHealthFailed,
						NodeID:      nodeID,
						Service:     svc.Name,
						Port:        port,
						Phase:       snap.Phase,
						Severity:    SeverityHigh,
						Confidence:  0.9,
						Timestamp:   now,
						EvidenceRef: "systemd:" + svc.UnitName + " port:" + itoa(port),
						Detail:      "systemd active but port " + itoa(port) + " not listening",
					})
					facts = append(facts, RuntimeFact{
						Kind:        FactRuntimeHealthMismatch,
						NodeID:      nodeID,
						Service:     svc.Name,
						Phase:       snap.Phase,
						Severity:    SeverityHigh,
						Confidence:  0.9,
						Timestamp:   now,
						EvidenceRef: "systemd:" + svc.UnitName + " port:" + itoa(port),
						Detail:      "systemd and port state disagree",
					})
				}
			}
		}
	}
	return facts
}

// ── Port-level facts ──────────────────────────────────────────────────────────

func (n *Normalizer) normalizePorts(snap *NodeRuntimeSnapshot, nodeID string, now time.Time) []RuntimeFact {
	portMap := buildPortMap(snap.Ports)
	var facts []RuntimeFact

	type portRule struct {
		port     int
		service  string
		factKind FactKind
		severity Severity
		blocks   []string
	}
	rules := []portRule{
		{9042, "scylla", FactScyllaCQLUnreachable, SeverityCritical,
			[]string{"workflow", "event", "resource", "repository"}},
		{2379, "etcd", FactEtcdUnreachable, SeverityCritical,
			[]string{"controller", "node-agent", "all"}},
		{9000, "minio", FactObjectstoreTopologyMissing, SeverityHigh,
			[]string{"repository", "package-distribution"}},
	}

	for _, rule := range rules {
		listening, observed := portMap[rule.port]
		if !observed || listening {
			continue
		}
		if !hasFact(facts, rule.factKind, rule.service) {
			facts = append(facts, RuntimeFact{
				Kind:        rule.factKind,
				NodeID:      nodeID,
				Service:     rule.service,
				Port:        rule.port,
				Phase:       snap.Phase,
				Severity:    rule.severity,
				Blocks:      rule.blocks,
				Confidence:  0.9,
				Timestamp:   now,
				EvidenceRef: "port:" + itoa(rule.port),
				Detail:      "port " + itoa(rule.port) + " not listening",
			})
		}
	}
	return facts
}

// ── Workflow safety ───────────────────────────────────────────────────────────

// normalizeWorkflowWithFacts emits WORKFLOW_REMEDIATION_UNSAFE when Scylla is not ready.
// Workflow depends on Scylla; if Scylla is blocked, workflow-backed remediation
// that requires Scylla durability is unsafe, even if the workflow process is running.
// accumulated contains all facts produced so far in the same Normalize() call.
func (n *Normalizer) normalizeWorkflowWithFacts(snap *NodeRuntimeSnapshot, accumulated []RuntimeFact, nodeID string, now time.Time) []RuntimeFact {
	scyllaDown := hasFact(accumulated, FactScyllaCQLUnreachable, "scylla") ||
		hasFact(accumulated, FactScyllaServiceFailed, "scylla") ||
		hasFact(accumulated, FactServiceFailed, "scylla")

	// Only emit if Scylla is known to be down AND workflow process is observed.
	workflowSeen := false
	for _, svc := range snap.Services {
		if svc.Name == "globular-workflow" {
			workflowSeen = true
			break
		}
	}

	if scyllaDown && workflowSeen {
		return []RuntimeFact{{
			Kind:        FactWorkflowRemediationUnsafe,
			NodeID:      nodeID,
			Service:     "globular-workflow",
			Phase:       snap.Phase,
			Severity:    SeverityHigh,
			Blocks:      []string{"workflow-remediation"},
			Confidence:  0.95,
			Timestamp:   now,
			EvidenceRef: "contract:workflow-depends_on-scylla",
			Detail:      "workflow depends on Scylla; Scylla not ready → workflow-backed remediation unsafe",
		}}
	}
	return nil
}

// ── domain helpers ────────────────────────────────────────────────────────────

// serviceFailKind returns the appropriate FactKind for a failed systemd unit.
//
// Globular installs its managed infrastructure units under the canonical
// `globular-<name>.service` prefix (e.g. globular-minio.service). Non-Globular
// distros run the upstream names (minio.service, etcd.service, ...). We
// canonicalize to the bare component name so both forms classify the same way
// — this is the only place that mapping should live.
func serviceFailKind(unit string, portMap map[int]bool) (FactKind, Severity, []string) {
	name := canonicalUnitBase(unit)
	_ = portMap
	switch name {
	case "scylla-server", "scylla":
		return FactScyllaCQLUnreachable, SeverityCritical,
			[]string{"workflow", "event", "resource", "repository"}
	case "minio":
		return FactObjectstoreTopologyMissing, SeverityHigh,
			[]string{"repository", "package-distribution"}
	case "etcd":
		return FactEtcdUnreachable, SeverityCritical,
			[]string{"controller", "node-agent", "all"}
	case "envoy":
		return FactGatewayBootstrapMissing, SeverityHigh,
			[]string{"mesh", "gateway"}
	default:
		return FactServiceFailed, SeverityHigh, serviceBlocks(name)
	}
}

// canonicalUnitBase strips the `.service` suffix and the optional Globular
// `globular-` prefix so callers can switch on the bare component name.
// "globular-minio.service" → "minio", "scylla-server.service" → "scylla-server".
func canonicalUnitBase(unit string) string {
	name := strings.TrimSuffix(unit, ".service")
	return strings.TrimPrefix(name, "globular-")
}

// expectedPort returns the primary listening port for a service, or 0 if unknown.
func expectedPort(name string) int {
	switch name {
	case "scylla", "scylla-server":
		return 9042
	case "etcd":
		return 2379
	case "minio":
		return 9000
	case "globular-mcp":
		return 10260
	case "globular-workflow":
		return 10004
	case "globular-cluster-controller":
		return 12000
	case "globular-node-agent":
		return 11000
	}
	return 0
}

// isCriticalUnit returns true for units that should always be active on a Globular node.
func isCriticalUnit(unit string) bool {
	switch canonicalUnitBase(unit) {
	case "node-agent", "etcd":
		return true
	}
	return false
}

// serviceBlocks returns the list of subsystems blocked when a service fails.
func serviceBlocks(name string) []string {
	switch name {
	case "scylla", "scylla-server":
		return []string{"workflow", "event", "resource", "repository"}
	case "minio":
		return []string{"repository", "package-distribution"}
	case "etcd":
		return []string{"controller", "node-agent", "all"}
	case "envoy":
		return []string{"mesh", "gateway"}
	case "workflow":
		return []string{"automation"}
	}
	return nil
}

// buildPortMap returns a map[port]listening from observations.
func buildPortMap(ports []PortObservation) map[int]bool {
	m := make(map[int]bool, len(ports))
	for _, p := range ports {
		m[p.Port] = p.Listening
	}
	return m
}

// hasFact reports whether any fact with the given kind and service already exists.
func hasFact(facts []RuntimeFact, kind FactKind, service string) bool {
	for _, f := range facts {
		if f.Kind == kind && f.Service == service {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

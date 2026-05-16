package contextnav

// falsifiers.go — Phase 6 of the context-navigation effort. Generates
// per-finding-family "what would prove this diagnosis wrong?" claims by
// matching finding IDs against a curated template registry. When no
// template matches, callers fall back to the generic falsifier shipped
// in Phase 2 — every DecisionTrace MUST carry at least one falsifier so
// the agent reads in falsifiable claims, not just match output.
//
// Rules from the design doc:
//   1. Generate from matched failure_mode / invariant IDs using
//      DETERMINISTIC templates. No LLM or fuzzy matching in awareness
//      core logic.
//   2. Commands MUST be safe to execute or inspect — read-only or
//      diagnosis-only. Anything cluster-mutating belongs in NextActions
//      with RequiresAck=true (Phase 7), never here.
//   3. The matchers are substring-based so a family like
//      "workflow.resume_poisoning" picks up the workflow template, and
//      "service.restart_singleflight" picks up the restart template.
//
// How to add a template: append to failureFalsifierTemplates with a
// matcher predicate and the falsifier set. The first matching template
// wins. Keep matchers narrow (specific words) so families don't bleed.

import "strings"

// falsifierTemplate is one rule in the registry: a predicate that decides
// whether a finding id belongs to this template's family, plus the
// falsifier claims to emit when matched.
type falsifierTemplate struct {
	// family is a short label used in test failure messages. Not exposed
	// to callers.
	family string
	// matches returns true when this template applies to the given
	// (lowercase) finding id.
	matches func(idLower string) bool
	// falsifiers is the list to emit. Commands MUST be read-only.
	falsifiers []Falsifier
}

// matchersAny returns a predicate that hits when any of the given
// substrings appears in the id. Keep it small — narrower matchers prevent
// family bleed.
func matchersAny(needles ...string) func(string) bool {
	return func(id string) bool {
		for _, n := range needles {
			if strings.Contains(id, n) {
				return true
			}
		}
		return false
	}
}

// failureFalsifierTemplates is the ordered registry of failure-family
// templates. First match wins; keep the order from most-specific
// (narrowest matchers) to most-general.
//
// IMPORTANT: every Command here must be read-only. If you need to
// suggest a mutating operation, add it as a DiagnosticAction in Phase 7
// with RequiresAck=true.
var failureFalsifierTemplates = []falsifierTemplate{
	{
		family:  "workflow_receipt_resume",
		matches: matchersAny("workflow.resume", "workflow.retry", "receipt", "resume_without_receipt", "workflow_receipts"),
		falsifiers: []Falsifier{
			{
				Claim:      "a workflow retry loop is active",
				HowToCheck: "inspect recent workflow runs for repeated same-target/same-package failures across a short window",
				Command:    `globular awareness preflight --task "workflow retry loop" --include-runtime --format agent`,
			},
			{
				Claim:      "the failed step has no terminal receipt",
				HowToCheck: "list step outcomes for the failing run and verify a terminal receipt (success/failed/aborted) exists for every step",
			},
		},
	},
	{
		family:  "restart_storm",
		matches: matchersAny("restart_storm", "restart_singleflight", "systemd_restart", "cgroup_escape", "port_squatting"),
		falsifiers: []Falsifier{
			{
				Claim:      "systemd is restarting the unit at a sustained rate",
				HowToCheck: "check systemd unit RestartCount/ActiveEnterTimestamp; a restart storm shows >1 restart per minute over the live overlay window",
				Command:    `globular awareness live-snapshot && globular awareness preflight --task "restart storm" --include-runtime --format agent`,
			},
			{
				Claim:      "another process is squatting on the unit's listening port",
				HowToCheck: "match the unit's expected listening port against the actual port owner; cgroup escape and port-squat surface as a different cgroup holding the port",
			},
		},
	},
	{
		family:  "desired_installed_mismatch",
		matches: matchersAny("desired_state", "installed_state", "drift", "convergence", "build_id_immutable", "bom_completeness"),
		falsifiers: []Falsifier{
			{
				Claim:      "desired and installed build_id differ for this service",
				HowToCheck: "compare DesiredService.BuildID for the service to the NodeInstalledPackage.BuildID on each affected node; equality refutes the drift hypothesis",
			},
			{
				Claim:      "installed state is stale (node-agent hasn't reported recently)",
				HowToCheck: "verify NodeHeartbeat timestamp for each affected node is within the configured staleness window",
			},
		},
	},
	{
		family:  "install_pipeline",
		matches: matchersAny("install.result", "install.desired_state", "release.failed", "partial_commit", "atomic_commit", "package_admission"),
		falsifiers: []Falsifier{
			{
				Claim:      "the install workflow did not reach a terminal verification step",
				HowToCheck: "list WorkflowStepRun rows for the install run and verify the final verification step produced a receipt with success",
			},
			{
				Claim:      "the partial-commit was actually atomic but mis-reported",
				HowToCheck: "compare the installed_state_record's atomicity stamp to the workflow receipt's commit phase; matched stamps refute the partial-commit claim",
			},
		},
	},
	{
		family:  "pki_cert_san",
		matches: matchersAny("pki.", "cert", "san ", "x509", "tls", "ca_not_published"),
		falsifiers: []Falsifier{
			{
				Claim:      "the endpoint is not covered by the certificate's SAN list",
				HowToCheck: "inspect the certificate's CertSAN node set and confirm at least one SAN matches the endpoint's advertised host/IP",
			},
			{
				Claim:      "the CA certificate has not been distributed to non-CA nodes",
				HowToCheck: "verify CertificateAuthority node presence + provenance on each affected node; absence proves the publication gap",
			},
		},
	},
	{
		family:  "dns_endpoint",
		matches: matchersAny("dns", "endpoint.advertised", "service.endpoint.etcd_address"),
		falsifiers: []Falsifier{
			{
				Claim:      "the advertised endpoint resolves to a different host than the desired one",
				HowToCheck: "compare DesiredService.AdvertisedEndpoint to the resolved IP of the corresponding DNSRecord/ServiceEndpoint node",
			},
		},
	},
	{
		family:  "scylla_storage",
		matches: matchersAny("scylla", "critical_keyspace", "replication", "minio", "objectstore", "repository.minio"),
		falsifiers: []Falsifier{
			{
				Claim:      "the keyspace has fewer than the configured replicas live",
				HowToCheck: "query the ScyllaTable replication factor against the count of live ScyllaNode entries; equality with the configured RF refutes the under-replication claim",
			},
			{
				Claim:      "MinIO erasure quorum is intact despite the warning",
				HowToCheck: "count live MinIO drives across nodes; a count >= configured erasure-set size refutes the quorum-loss claim",
			},
		},
	},
	{
		family:  "etcd_quorum",
		matches: matchersAny("etcd", "quorum", "leader", "raft", "lkg_corrupt"),
		falsifiers: []Falsifier{
			{
				Claim:      "etcd lost quorum within the live-overlay window",
				HowToCheck: "compare etcd_endpoints.healthy_count to the configured quorum size at the live overlay's captured_at timestamp",
			},
			{
				Claim:      "the affected node never had a stable etcd member identity",
				HowToCheck: "look up the node's etcd member id history; an absent or churning id refutes the stable-cluster hypothesis",
			},
		},
	},
	{
		family:  "critical_state",
		matches: matchersAny("critical_state", "critical_queries", "absent_key_interpreted_as_stop", "unbounded_hang"),
		falsifiers: []Falsifier{
			{
				Claim:      "the absent etcd key was actually a deliberate clear, not an outage",
				HowToCheck: "inspect the etcd revision history for the key; an authored deletion at a known revision refutes the outage hypothesis",
			},
		},
	},
}

// invariantFalsifierTemplates is a thin alias of the failure-mode
// templates today — invariant IDs share the same vocabulary. Kept as a
// separate variable so we can specialize later without breaking the
// failure_mode path.
var invariantFalsifierTemplates = failureFalsifierTemplates

// falsifiersForFinding returns the templated falsifiers that match the
// given finding id, or an empty slice when no template matches. The
// caller is responsible for falling back to genericFalsifier when this
// returns empty.
//
// findingKind is "failure_mode", "invariant", or "forbidden_fix". Today
// only failure_mode and invariant carry templates; forbidden_fix falls
// through to the generic falsifier in every case.
func falsifiersForFinding(findingKind, findingID string) []Falsifier {
	id := strings.ToLower(findingID)
	var templates []falsifierTemplate
	switch findingKind {
	case "failure_mode":
		templates = failureFalsifierTemplates
	case "invariant":
		templates = invariantFalsifierTemplates
	default:
		return nil
	}
	for _, tmpl := range templates {
		if tmpl.matches(id) {
			// Defensive copy so callers can't mutate the registry.
			out := make([]Falsifier, len(tmpl.falsifiers))
			copy(out, tmpl.falsifiers)
			return out
		}
	}
	return nil
}

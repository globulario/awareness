package contextnav

// owner.go — Phase 3 of the context-navigation effort. Resolves the
// "which layer owns this finding?" question by walking one hop of the
// awareness graph from a finding node and counting neighbors by their
// 4-layer Globular role: repository / desired / installed / runtime /
// workflow / pki / dns / rbac.
//
// Resolution order (per the design doc):
//
//  1. Prefer explicit graph ownership: count outgoing+incoming neighbors
//     of the finding, bucketing each into a layer by its node type.
//  2. If the task description hints at a class (incident / install / state
//     mismatch / etc.), use it as a tiebreaker among layers that scored
//     equally.
//  3. Pull Service/Package/Files/Symbols from the same neighbor walk so
//     OwnerContext carries actionable handles, not just a label.
//  4. If no graph neighbor maps to a layer AND no task hint resolves,
//     return Layer="unknown" — callers should attach a warning to the
//     enclosing DecisionTrace.
//
// Why one hop and not a deeper BFS: at depth 1, ownership signals are
// strong and the cost is bounded. Deep traversal would re-discover the
// same layer signal via long chains (e.g. invariant → failure_mode →
// runtime_state_record) and inflate the dominant layer artificially.

import (
	"context"
	"sort"
	"strings"

	"github.com/globulario/awareness/graph"
)

// Layer labels for the OwnerContext.Layer field. Match the doc's table.
const (
	LayerRepository = "repository"
	LayerDesired    = "desired"
	LayerInstalled  = "installed"
	LayerRuntime    = "runtime"
	LayerWorkflow   = "workflow"
	LayerPKI        = "pki"
	LayerDNS        = "dns"
	LayerRBAC       = "rbac"
	LayerUnknown    = "unknown"
)

// nodeTypeToLayer maps graph node types to the layer that owns them.
// Source: docs/operators/* layering + claude_codex_awareness_context_navigation_improvement.md Phase 3.
// When a node type can legitimately belong to multiple layers (e.g.
// service_endpoint may be DNS-flavoured or routing-flavoured), pick the
// one most actionable for ownership purposes — the agent uses Layer to
// decide which subsystem's people/code own the fix, not to render the
// node's category.
var nodeTypeToLayer = map[string]string{
	// repository — release admission, artifact storage, publish-state
	graph.NodeTypePackage:               LayerRepository,
	graph.NodeTypeServiceRelease:        LayerRepository,
	graph.NodeTypeInfrastructureRelease: LayerRepository,
	graph.NodeTypeRepositoryStatus:      LayerRepository,
	graph.NodeTypeObjectstoreStatus:     LayerRepository,

	// desired — what the cluster SHOULD have running (etcd-backed)
	graph.NodeTypeDesiredService:        LayerDesired,
	graph.NodeTypeDesiredInfrastructure: LayerDesired,
	graph.NodeTypeDesiredStateRecord:    LayerDesired,
	graph.NodeTypeObjectstoreDesired:    LayerDesired,
	graph.NodeTypeServiceRuntimeConfig:  LayerDesired,
	graph.NodeTypeClusterSystemConfig:   LayerDesired,
	graph.NodeTypeEtcdKey:               LayerDesired,
	graph.NodeTypeEtcdSnapshot:          LayerDesired,

	// installed — what node-agents report as on-disk
	graph.NodeTypeNodeInstalledPackage: LayerInstalled,
	graph.NodeTypeInstalledStateRecord: LayerInstalled,
	graph.NodeTypeNodeConvergenceState: LayerInstalled,
	graph.NodeTypeNodeHeartbeat:        LayerInstalled,

	// runtime — what is observably executing right now
	graph.NodeTypeRuntimeStateRecord:   LayerRuntime,
	graph.NodeTypeRuntimeServiceStatus: LayerRuntime,
	graph.NodeTypeRuntimeSnapshot:      LayerRuntime,
	graph.NodeTypeSystemdStatus:        LayerRuntime,
	graph.NodeTypeSystemdUnit:          LayerRuntime,
	graph.NodeTypeDoctorFinding:        LayerRuntime,
	graph.NodeTypeDoctorEvidence:       LayerRuntime,
	graph.NodeTypeMetricSample:         LayerRuntime,
	graph.NodeTypeMetricWarning:        LayerRuntime,
	graph.NodeTypeRuntimeState:         LayerRuntime,
	graph.NodeTypeStateDelta:           LayerRuntime,
	graph.NodeTypeDriftRecord:          LayerRuntime,
	graph.NodeTypeConvergenceRecord:    LayerRuntime,
	graph.NodeTypeXDSStatus:            LayerRuntime,

	// workflow — orchestration receipts/runs/effects
	graph.NodeTypeWorkflow:                  LayerWorkflow,
	graph.NodeTypeWorkflowStep:              LayerWorkflow,
	graph.NodeTypeWorkflowRun:               LayerWorkflow,
	graph.NodeTypeWorkflowStepRun:           LayerWorkflow,
	graph.NodeTypeWorkflowReceipt:           LayerWorkflow,
	graph.NodeTypeWorkflowRetryRecord:       LayerWorkflow,
	graph.NodeTypeWorkflowBlockedReason:     LayerWorkflow,
	graph.NodeTypeWorkflowError:             LayerWorkflow,
	graph.NodeTypeWorkflowStepEffect:        LayerWorkflow,
	graph.NodeTypeWorkflowVerificationRecord: LayerWorkflow,
	graph.NodeTypeWorkflowIntegrityFinding:  LayerWorkflow,
	graph.NodeTypeReleaseAction:             LayerWorkflow,

	// pki — TLS / cert authority / SAN coverage
	graph.NodeTypeCertificate:          LayerPKI,
	graph.NodeTypeCertificateAuthority: LayerPKI,
	graph.NodeTypeCertSAN:              LayerPKI,
	graph.NodeTypeCertExpiryWarning:    LayerPKI,

	// dns — zone / record / endpoint routing
	graph.NodeTypeDNSZone:         LayerDNS,
	graph.NodeTypeDNSRecord:       LayerDNS,
	graph.NodeTypeServiceEndpoint: LayerDNS,
	graph.NodeTypeDomainSpec:      LayerDNS,

	// rbac — policy file / role / permission / subject / binding
	graph.NodeTypeRBACPolicyFile:  LayerRBAC,
	graph.NodeTypeRBACRole:        LayerRBAC,
	graph.NodeTypeRBACPermission:  LayerRBAC,
	graph.NodeTypeRBACSubject:     LayerRBAC,
	graph.NodeTypeRBACBinding:     LayerRBAC,
	graph.NodeTypeServiceIdentity: LayerRBAC,
}

// taskClassLayerOrder ranks layers when the task description suggests a
// class. Lower index = higher preference. Used only as a tiebreaker when
// two layers tie on neighbor count.
var taskClassLayerOrder = map[string][]string{
	"runtime_incident":  {LayerRuntime, LayerInstalled, LayerDesired, LayerRepository},
	"state_mismatch":    {LayerDesired, LayerInstalled, LayerRuntime, LayerRepository},
	"package_admission": {LayerRepository, LayerDesired, LayerInstalled, LayerRuntime},
	"workflow_failure":  {LayerWorkflow, LayerRuntime, LayerInstalled, LayerDesired},
	"cert_dns":          {LayerPKI, LayerDNS, LayerRuntime},
	"rbac_failure":      {LayerRBAC, LayerRuntime},
}

// inferTaskClass returns a coarse class string for the task description,
// or "" if no class hint applies. Pure keyword matching — deliberately
// dumb so the function stays predictable and easy to extend.
func inferTaskClass(task string) string {
	t := strings.ToLower(task)
	switch {
	case containsAny(t, "incident", "outage", "crash", "restart storm", "retry loop", "panic"):
		return "runtime_incident"
	case containsAny(t, "desired ", "installed ", "state mismatch", "drift", "convergence"):
		return "state_mismatch"
	case containsAny(t, "publish", "admit", "release", "build_id", "artifact", "package admission"):
		return "package_admission"
	case containsAny(t, "workflow", "receipt", "orchestrat"):
		return "workflow_failure"
	case containsAny(t, "cert", "tls", "san ", "acme", "x509", "dns"):
		return "cert_dns"
	case containsAny(t, "rbac", "permission", "authz", "authorization"):
		return "rbac_failure"
	}
	return ""
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// InferOwner resolves the OwnerContext for a finding node by walking one
// hop of the graph and counting neighbor layers. Returns Layer="unknown"
// when no neighbor's type maps to a known layer and no task hint applies.
//
// findingNodeID must be a graph node id (e.g. "failure_mode:X" or
// "invariant:Y"). Passing an empty id or an id with no edges is safe —
// the function returns a Layer="unknown" OwnerContext.
func InferOwner(ctx context.Context, g *graph.Graph, findingNodeID, task string, files []string) OwnerContext {
	out := OwnerContext{Layer: LayerUnknown}
	if g == nil || findingNodeID == "" {
		return enrichWithFileHint(out, files)
	}

	edges, err := g.Neighbors(ctx, findingNodeID, "both")
	if err != nil || len(edges) == 0 {
		return enrichWithFileHint(out, files)
	}

	// Collect unique neighbor ids (the OTHER endpoint of each edge).
	neighborIDs := make(map[string]bool, len(edges))
	for _, e := range edges {
		if e.Src != findingNodeID {
			neighborIDs[e.Src] = true
		}
		if e.Dst != findingNodeID {
			neighborIDs[e.Dst] = true
		}
	}

	layerScores := make(map[string]int, 8)
	for id := range neighborIDs {
		n, err := g.FindNode(ctx, id)
		if err != nil || n == nil {
			continue
		}
		switch n.Type {
		case graph.NodeTypeGlobularService:
			if out.Service == "" {
				out.Service = n.Name
			}
		case graph.NodeTypePackage:
			if out.Package == "" {
				out.Package = n.Name
			}
		case graph.NodeTypeSourceFile:
			if n.Path != "" {
				out.Files = appendUnique(out.Files, n.Path)
			}
		case graph.NodeTypeSymbol:
			if n.Name != "" {
				out.Symbols = appendUnique(out.Symbols, n.Name)
			}
		}
		if layer, ok := nodeTypeToLayer[n.Type]; ok {
			layerScores[layer]++
			// stateIDs: record the underlying node id so callers can pivot
			// from the trace directly to the layer's record.
			out.StateIDs = appendUnique(out.StateIDs, n.ID)
		}
	}

	out.Layer = chooseLayer(layerScores, inferTaskClass(task))
	out = enrichWithFileHint(out, files)
	return out
}

// chooseLayer picks the winning layer by neighbor count, breaking ties
// using the task-class preference order when available.
func chooseLayer(scores map[string]int, taskClass string) string {
	if len(scores) == 0 {
		return LayerUnknown
	}
	// Sort layers by (score desc, then taskClass-preferred-order, then name).
	type entry struct {
		layer string
		score int
	}
	ranked := make([]entry, 0, len(scores))
	for l, s := range scores {
		ranked = append(ranked, entry{l, s})
	}
	order := taskClassLayerOrder[taskClass]
	rank := func(l string) int {
		for i, o := range order {
			if o == l {
				return i
			}
		}
		return len(order) + 1
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ri, rj := rank(ranked[i].layer), rank(ranked[j].layer); ri != rj {
			return ri < rj
		}
		return ranked[i].layer < ranked[j].layer
	})
	return ranked[0].layer
}

// enrichWithFileHint merges caller-supplied target files into OwnerContext.
// File hints are additive — they don't override a graph-derived Service
// when one was found, but they DO populate Files so the agent has handles
// to inspect even when graph ownership was sparse.
func enrichWithFileHint(out OwnerContext, files []string) OwnerContext {
	for _, f := range files {
		if f != "" {
			out.Files = appendUnique(out.Files, f)
		}
	}
	return out
}

func appendUnique(dst []string, v string) []string {
	for _, x := range dst {
		if x == v {
			return dst
		}
	}
	return append(dst, v)
}

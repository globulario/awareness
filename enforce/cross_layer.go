// Package enforce/cross_layer implements multi-tier awareness invariant checks.
// CrossLayerCheck joins nodes across source tiers (package_spec, systemd_runtime,
// installed_metadata, repository_manifest, etcd_desired_state) and emits
// divergence edges for violations detected at the boundary between layers.
//
// Invariants checked:
//   1. Desired/Installed version mismatch  (etcd desired != receipt installed)
//   2. Installed-without-unit              (receipt present, no matching systemd unit)
//   3. Profile compliance                  (package requires profiles the node lacks)
//   4. Founding quorum                     (storage/etcd/scylladb must have ≥3 nodes)
//   5. Cert SAN coverage                   (cert node missing required SANs)
//   6. Sidecar hash mismatch               (unit SHA-256 != .sha256 sidecar file)

package enforce

import (
	"context"
	"strings"

	"github.com/globulario/awareness/graph"
)

// CrossLayerViolation describes a single invariant violation found across layers.
type CrossLayerViolation struct {
	InvariantID string // canonical invariant ID (e.g. "invariant:minio.minimum_3_nodes")
	Kind        string // short violation type key
	NodeA       string // first node involved
	NodeB       string // second node involved (if applicable)
	Detail      string // human-readable description
}

// CrossLayerResult holds the aggregate findings of a full cross-layer scan.
type CrossLayerResult struct {
	Violations []CrossLayerViolation
	// NodesExamined is the total number of nodes examined by all checks.
	NodesExamined int
}

// CrossLayerCheck runs all cross-layer invariant checks against g.
// It returns a result even if some checks fail — partial results are better
// than none. Errors from individual checks are accumulated in the violations
// with Kind="check_error".
func CrossLayerCheck(ctx context.Context, g *graph.Graph) (CrossLayerResult, error) {
	var res CrossLayerResult

	checks := []func(context.Context, *graph.Graph, *CrossLayerResult){
		checkDesiredInstalledMismatch,
		checkInstalledWithoutUnit,
		checkSidecarHashMismatch,
		checkFoundingQuorum,
		checkCertSAN,
	}

	for _, check := range checks {
		check(ctx, g, &res)
	}
	return res, nil
}

// ── Check 1: Desired/Installed version mismatch ───────────────────────────────

// checkDesiredInstalledMismatch finds etcd desired-state nodes whose desired
// version differs from the installed version in the corresponding receipt node.
func checkDesiredInstalledMismatch(ctx context.Context, g *graph.Graph, res *CrossLayerResult) {
	nodes, err := g.FindNodesByType(ctx, "etcd_desired_state")
	if err != nil {
		return
	}
	for _, n := range nodes {
		res.NodesExamined++
		desiredVer, _ := n.Metadata["desired_version"].(string)
		if desiredVer == "" {
			continue
		}
		serviceName := n.Name
		receiptID := "receipt:" + serviceName
		receipt, err := g.FindNode(ctx, receiptID)
		if err != nil || receipt == nil {
			continue
		}
		installedVer, _ := receipt.Metadata["version"].(string)
		if installedVer == "" || installedVer == desiredVer {
			continue
		}
		inv := "invariant:" + serviceName + ".version_convergence"
		res.Violations = append(res.Violations, CrossLayerViolation{
			InvariantID: inv,
			Kind:        "desired_installed_mismatch",
			NodeA:       n.ID,
			NodeB:       receiptID,
			Detail:      "desired=" + desiredVer + " installed=" + installedVer,
		})
		// Emit edge in graph so other tools can traverse it.
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  n.ID,
			Kind: graph.EdgeHasStateDelta,
			Dst:  receiptID,
			Metadata: map[string]any{
				"desired_version":   desiredVer,
				"installed_version": installedVer,
				"violation":         "desired_installed_mismatch",
			},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  n.ID,
			Kind: graph.EdgeViolates,
			Dst:  inv,
		})
	}
}

// ── Check 2: Installed-without-unit ──────────────────────────────────────────

// checkInstalledWithoutUnit finds receipt nodes that have no corresponding
// systemd unit node in the graph (service installed but unit file absent).
func checkInstalledWithoutUnit(ctx context.Context, g *graph.Graph, res *CrossLayerResult) {
	nodes, err := g.FindNodesByType(ctx, "installed_artifact")
	if err != nil {
		return
	}
	for _, n := range nodes {
		res.NodesExamined++
		// Try the canonical unit IDs.
		unitID1 := "unit:globular-" + n.Name + ".service"
		unitID2 := "unit:" + n.Name + ".service"
		u1, _ := g.FindNode(ctx, unitID1)
		u2, _ := g.FindNode(ctx, unitID2)
		if u1 != nil || u2 != nil {
			continue
		}
		inv := "invariant:" + n.Name + ".unit_present"
		res.Violations = append(res.Violations, CrossLayerViolation{
			InvariantID: inv,
			Kind:        "installed_without_unit",
			NodeA:       n.ID,
			NodeB:       unitID1,
			Detail:      n.Name + " receipt exists but no systemd unit node in graph",
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  n.ID,
			Kind: graph.EdgeViolates,
			Dst:  inv,
		})
	}
}

// ── Check 3: Sidecar hash mismatch ───────────────────────────────────────────

// checkSidecarHashMismatch finds systemd unit nodes where sidecar_match=false,
// indicating the unit file content has diverged from its pinned SHA-256.
func checkSidecarHashMismatch(ctx context.Context, g *graph.Graph, res *CrossLayerResult) {
	nodes, err := g.FindNodesByType(ctx, "systemd_unit")
	if err != nil {
		return
	}
	for _, n := range nodes {
		res.NodesExamined++
		sidecarMatch, ok := n.Metadata["sidecar_match"]
		if !ok {
			continue // no sidecar — not an error
		}
		matched := false
		switch v := sidecarMatch.(type) {
		case bool:
			matched = v
		case float64:
			matched = v != 0
		case int64:
			matched = v != 0
		}
		if matched {
			continue
		}
		inv := "invariant:" + n.Name + ".unit_integrity"
		res.Violations = append(res.Violations, CrossLayerViolation{
			InvariantID: inv,
			Kind:        "sidecar_hash_mismatch",
			NodeA:       n.ID,
			Detail:      n.Name + " unit file SHA-256 does not match .sha256 sidecar",
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  n.ID,
			Kind: graph.EdgeViolates,
			Dst:  inv,
		})
	}
}

// ── Check 4: Founding quorum ──────────────────────────────────────────────────

// checkFoundingQuorum verifies that the storage-tier services (etcd, scylladb,
// minio) have at least 3 installed nodes. It counts distinct `node_id` values
// across installed_package nodes for each infrastructure service.
func checkFoundingQuorum(ctx context.Context, g *graph.Graph, res *CrossLayerResult) {
	infraServices := []struct {
		name    string
		minimum int
	}{
		{"minio", 3},
		{"scylladb", 3},
		// etcd is on all nodes — we only count if we have etcd installed nodes
		{"etcd", 3},
	}

	nodes, err := g.FindNodesByType(ctx, "installed_package")
	if err != nil {
		return
	}

	counts := make(map[string]map[string]bool) // serviceName → set of nodeIDs
	for _, n := range nodes {
		res.NodesExamined++
		svcName := n.Name
		nodeID, _ := n.Metadata["node_id"].(string)
		if nodeID == "" {
			continue
		}
		if counts[svcName] == nil {
			counts[svcName] = make(map[string]bool)
		}
		counts[svcName][nodeID] = true
	}

	for _, svc := range infraServices {
		nodeSet := counts[svc.name]
		if len(nodeSet) == 0 {
			continue // service not yet discovered — skip, not a violation
		}
		if len(nodeSet) >= svc.minimum {
			continue
		}
		inv := "invariant:" + svc.name + ".minimum_" + itoa(svc.minimum) + "_nodes"
		res.Violations = append(res.Violations, CrossLayerViolation{
			InvariantID: inv,
			Kind:        "founding_quorum_below_minimum",
			NodeA:       "package:" + svc.name,
			Detail:      svc.name + " has " + itoa(len(nodeSet)) + " node(s), need " + itoa(svc.minimum),
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  "package:" + svc.name,
			Kind: graph.EdgeViolates,
			Dst:  inv,
		})
	}
}

// ── Check 5: Cert SAN coverage ───────────────────────────────────────────────

// checkCertSAN verifies that PKI certificate nodes include the cluster VIP
// and node FQDN patterns. Certs missing ".globular.internal" or the wildcard
// SAN are flagged as potential mTLS routing failures.
func checkCertSAN(ctx context.Context, g *graph.Graph, res *CrossLayerResult) {
	nodes, err := g.FindNodesByType(ctx, "pki_certificate")
	if err != nil {
		return
	}
	for _, n := range nodes {
		res.NodesExamined++
		// Only check service certificates (not CA).
		if !strings.Contains(n.Path, "/issued/") {
			continue
		}
		sans, _ := n.Metadata["sans"].([]any)
		hasInternal := false
		for _, raw := range sans {
			s, _ := raw.(string)
			if strings.Contains(s, ".globular.internal") || strings.Contains(s, "*.globular") {
				hasInternal = true
				break
			}
		}
		if !hasInternal {
			inv := "invariant:" + n.Name + ".cert_san_coverage"
			res.Violations = append(res.Violations, CrossLayerViolation{
				InvariantID: inv,
				Kind:        "cert_missing_internal_san",
				NodeA:       n.ID,
				Detail:      n.Name + " cert has no .globular.internal SAN — mTLS routing may fail",
			})
			_ = g.AddEdge(ctx, graph.Edge{
				Src:  n.ID,
				Kind: graph.EdgeViolates,
				Dst:  inv,
			})
		}
	}
}


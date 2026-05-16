// convergence.go — reads etcd convergence records and emits
// Desired→Installed→Runtime delta nodes into the awareness graph.
//
// Convergence records live at:
//   /globular/convergence/nodes/{node_id}/packages/{pkg}/latest
//
// Each record captures the last reconcile action for a (node, package) pair,
// including desired vs installed versions, outcome, and attempt count.
//
// Design:
//   - Read-only: never calls cli.Put/Delete/Txn.
//   - Graceful skip when factory is nil or returns nil client.
//   - All emitted nodes carry ttl_seconds and expires_at.

package clusterstate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/globulario/awareness/graph"
)

const convergencePrefix = "/globular/convergence/nodes/"

// convergenceRecord mirrors the JSON stored at
// /globular/convergence/nodes/{node_id}/packages/{pkg}/latest.
type convergenceRecord struct {
	ActionID        string         `json:"action_id"`
	WorkflowID      string         `json:"workflow_id"`
	Package         string         `json:"package"`
	NodeID          string         `json:"node_id"`
	DesiredVersion  string         `json:"desired_version"`
	DesiredBuildID  string         `json:"desired_build_id"`
	DesiredHash     string         `json:"desired_hash"`
	LocalVersion    string         `json:"local_version"`
	LocalBuildID    string         `json:"local_build_id"`
	LocalHash       string         `json:"local_hash"`
	Outcome         string         `json:"outcome"`
	ReasonCode      string         `json:"reason_code"`
	CommittedAt     int64          `json:"committed_at"`
	LastAttemptAt   int64          `json:"last_attempt_at"`
	AttemptCount    int            `json:"attempt_count"`
	SourceComponent string         `json:"source_component"`
	Evidence        map[string]any `json:"evidence"`
}

// classifyDrift returns a drift_class string for a convergence record.
func classifyDrift(rec convergenceRecord) string {
	switch {
	case rec.Outcome == "FAILED":
		return "runtime_dead"
	case rec.Outcome == "BLOCKED":
		return "release_phase_stuck"
	case rec.AttemptCount > 5:
		return "release_phase_stuck"
	case rec.LocalVersion == "" && rec.DesiredVersion != "":
		return "installed_missing"
	case rec.DesiredVersion == "" && rec.LocalVersion == "":
		return "desired_missing"
	case rec.DesiredVersion != rec.LocalVersion:
		return "version_mismatch"
	case rec.DesiredVersion == rec.LocalVersion && rec.Outcome == "SUCCESS_COMMITTED":
		return "aligned"
	default:
		return "unknown"
	}
}

// CollectConvergence reads convergence records from etcd and emits
// Desired→Installed→Runtime delta nodes into g.
// A nil factory skips gracefully with status="skipped".
func CollectConvergence(ctx context.Context, g *graph.Graph, factory EtcdClientFactory) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "convergence",
		SourceTier:  "cluster_authority",
		Status:      "skipped",
	}

	if factory == nil {
		return health, nil
	}

	cli, err := factory()
	if err != nil || cli == nil {
		if err != nil {
			health.Status = "failed"
			health.Error = err.Error()
			health.Notes = append(health.Notes,
				fmt.Sprintf("factory error: %v", err))
			emitConvergenceFailureNode(ctx, g, err)
		}
		return health, nil
	}
	defer cli.Close()

	scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := cli.Get(scanCtx, convergencePrefix,
		clientv3.WithPrefix(),
		clientv3.WithLimit(2000),
	)
	if err != nil {
		health.Status = "failed"
		health.Error = err.Error()
		health.Notes = append(health.Notes, fmt.Sprintf("convergence prefix get error: %v", err))
		emitConvergenceFailureNode(ctx, g, err)
		return health, nil
	}

	collectedAt := time.Now().Unix()

	if len(resp.Kvs) == 0 {
		health.Status = "ok"
		health.Notes = append(health.Notes, "convergence prefix returned 0 keys — no reconcile records found")
		return health, nil
	}

	for _, kv := range resp.Kvs {
		key := string(kv.Key)

		// key shape: /globular/convergence/nodes/{node_id}/packages/{pkg}/latest
		nodeID, pkg, ok := parseConvergenceKey(key)
		if !ok {
			continue
		}

		var rec convergenceRecord
		if err := json.Unmarshal(kv.Value, &rec); err != nil {
			health.Notes = append(health.Notes, fmt.Sprintf("skip %s: json decode: %v", key, err))
			continue
		}

		// Fill in IDs from the key if the record fields are empty.
		if rec.NodeID == "" {
			rec.NodeID = nodeID
		}
		if rec.Package == "" {
			rec.Package = pkg
		}

		driftClass := classifyDrift(rec)
		convID := fmt.Sprintf("convergence:%s:%s", rec.NodeID, rec.Package)

		meta := map[string]any{
			"package":          rec.Package,
			"node_id":          rec.NodeID,
			"desired_version":  rec.DesiredVersion,
			"desired_build_id": rec.DesiredBuildID,
			"local_version":    rec.LocalVersion,
			"local_build_id":   rec.LocalBuildID,
			"outcome":          rec.Outcome,
			"drift_class":      driftClass,
			"attempt_count":    rec.AttemptCount,
			"committed_at":     rec.CommittedAt,
			"collected_at":     collectedAt,
			"ttl_seconds":      int64(300),
			"expires_at":       collectedAt + 300,
			"source_tier":      "cluster_authority",
			"trust_level":      "observed",
			"etcd_key":         key,
			"source_component": rec.SourceComponent,
		}
		if rec.ReasonCode != "" {
			meta["reason_code"] = rec.ReasonCode
		}

		summary := fmt.Sprintf("%s@%s → %s [%s] on %s",
			rec.Package, rec.DesiredVersion, rec.LocalVersion, driftClass, rec.NodeID)

		convNode := graph.Node{
			ID:       convID,
			Type:     graph.NodeTypeConvergenceRecord,
			Name:     fmt.Sprintf("%s/%s", rec.NodeID, rec.Package),
			Summary:  summary,
			Metadata: meta,
		}
		if err := g.AddNode(ctx, convNode); err != nil {
			continue
		}
		health.NodesEmitted++

		// Edge: convergence record → installed package node (if it exists).
		installedID := fmt.Sprintf("node:%s/installed/:%s", rec.NodeID, rec.Package)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  convID,
			Kind: graph.EdgeDesiredComparesToInstalled,
			Dst:  installedID,
		})

		// Emit drift record when not aligned.
		if driftClass != "aligned" {
			driftID := fmt.Sprintf("drift:%s:%s", rec.NodeID, rec.Package)
			driftMeta := map[string]any{
				"package":         rec.Package,
				"node_id":         rec.NodeID,
				"drift_class":     driftClass,
				"desired_version": rec.DesiredVersion,
				"local_version":   rec.LocalVersion,
				"outcome":         rec.Outcome,
				"attempt_count":   rec.AttemptCount,
				"collected_at":    collectedAt,
				"ttl_seconds":     int64(300),
				"expires_at":      collectedAt + 300,
				"source_tier":     "cluster_authority",
				"trust_level":     "observed",
			}
			if rec.ReasonCode != "" {
				driftMeta["reason_code"] = rec.ReasonCode
			}

			driftNode := graph.Node{
				ID:      driftID,
				Type:    graph.NodeTypeDriftRecord,
				Name:    fmt.Sprintf("drift:%s/%s", rec.NodeID, rec.Package),
				Summary: fmt.Sprintf("drift %s on %s: %s", rec.Package, rec.NodeID, driftClass),
				Metadata: driftMeta,
			}
			if addErr := g.AddNode(ctx, driftNode); addErr == nil {
				health.NodesEmitted++
				_ = g.AddEdge(ctx, graph.Edge{
					Src:  convID,
					Kind: graph.EdgeDriftDetectedBetween,
					Dst:  driftID,
				})
			}
		}
	}

	health.Status = "ok"
	return health, nil
}

// parseConvergenceKey extracts node_id and pkg from a convergence etcd key.
// Expected format: /globular/convergence/nodes/{node_id}/packages/{pkg}/latest
// Returns ok=false for keys that don't match.
func parseConvergenceKey(key string) (nodeID, pkg string, ok bool) {
	rest := strings.TrimPrefix(key, convergencePrefix)
	if rest == key {
		return "", "", false
	}
	// rest = {node_id}/packages/{pkg}/latest
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || parts[1] != "packages" {
		return "", "", false
	}
	nodeID = parts[0]
	// parts[2] = {pkg}, parts[3] = "latest"
	pkg = parts[2]
	if pkg == "" || parts[3] != "latest" {
		return "", "", false
	}
	return nodeID, pkg, true
}

// emitConvergenceFailureNode emits a ConvergenceRecord node with failed status so
// the graph can surface that convergence data was unavailable this collection run.
func emitConvergenceFailureNode(ctx context.Context, g *graph.Graph, cause error) {
	collectedAt := time.Now().Unix()
	_ = g.AddNode(ctx, graph.Node{
		ID:      "convergence:collector:failure",
		Type:    graph.NodeTypeConvergenceRecord,
		Name:    "convergence_collector_failure",
		Summary: "convergence extractor failed to reach etcd",
		Metadata: map[string]any{
			"status":            "failed",
			"last_error":        cause.Error(),
			"coverage":          "failed",
			"confidence_impact": "lowers_runtime_confidence",
			"collected_at":      collectedAt,
			"ttl_seconds":       int64(300),
			"expires_at":        collectedAt + 300,
			"source_tier":       "cluster_authority",
			"trust_level":       "unverified",
		},
	})
}

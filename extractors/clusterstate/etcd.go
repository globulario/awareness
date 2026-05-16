// Package clusterstate/etcd provides a read-only etcd snapshot collector.
// It reads desired-service state from etcd and emits divergence edges when
// the desired version in etcd differs from the installed version already
// recorded in the awareness graph (from the varlib/receipt collector).
//
// Design:
//   - Accepts a ClientFactory function for dependency injection and testability.
//   - A nil factory (or factory returning error) causes graceful skip.
//   - Does not modify cluster state — read-only.
//   - Key namespaces read: configurable via EtcdCollectOptions (DefaultEtcdKeyspaces).

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

// EtcdClientFactory returns a connected etcd client.
// Return (nil, nil) to skip the collector gracefully.
type EtcdClientFactory func() (*clientv3.Client, error)

// EtcdConnectError is returned by a ClientFactory when the etcd connection
// cannot be established (e.g. TLS certificates not found, no endpoints configured).
// CollectEtcd treats this as a graceful skip, not an error.
type EtcdConnectError struct {
	Reason string
}

func (e *EtcdConnectError) Error() string { return "etcd connect: " + e.Reason }

// EtcdCollectOptions controls which keyspaces the etcd collector reads.
// Zero value uses DefaultEtcdKeyspaces.
type EtcdCollectOptions struct {
	// Keyspaces to collect. Defaults to DefaultEtcdKeyspaces if empty.
	Keyspaces []string
}

// DefaultEtcdKeyspaces is the canonical set of etcd key prefixes the collector
// reads when no explicit keyspaces are configured.
var DefaultEtcdKeyspaces = []string{
	"/globular/resources/DesiredService/",
	"/globular/resources/InfrastructureRelease/",
	"/globular/resources/ServiceRelease/",
	"/globular/nodes/",
	"/globular/objectstore/",
	"/globular/services/",
	"/globular/system/config",
}

// desiredVersionRecord is the etcd JSON structure for ServiceDesiredVersion.
type desiredVersionRecord struct {
	Spec *struct {
		ServiceName string `json:"service_name"`
		Version     string `json:"version"`
		BuildID     string `json:"build_id"`
	} `json:"spec"`
}

// installedPackageRecord is the etcd JSON structure for an installed package.
type installedPackageRecord struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// CollectEtcd reads desired-service state from etcd and records divergence
// relative to the installed state already in the graph.
// A nil factory or a factory error skips gracefully with status="skipped".
// This is the backward-compatible three-argument form; it uses DefaultEtcdKeyspaces.
func CollectEtcd(ctx context.Context, g *graph.Graph, factory EtcdClientFactory) (CollectorHealth, error) {
	h, err := CollectEtcdWithOptions(ctx, g, factory, EtcdCollectOptions{})
	// Backward compat: map "failed" back to "skipped" for factory errors.
	// CollectEtcd has historically treated all factory errors as graceful skips.
	if h.Status == "failed" {
		h.Status = "skipped"
	}
	return h, err
}

// CollectEtcdWithOptions is the full form of CollectEtcd that accepts explicit
// keyspace options. Pass an empty EtcdCollectOptions{} to use DefaultEtcdKeyspaces.
// Unlike CollectEtcd, this function distinguishes factory errors (status="failed")
// from true skips (status="skipped").
func CollectEtcdWithOptions(ctx context.Context, g *graph.Graph, factory EtcdClientFactory, opts EtcdCollectOptions) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "etcd",
		Status:      "skipped",
	}

	keyspaces := opts.Keyspaces
	if len(keyspaces) == 0 {
		keyspaces = DefaultEtcdKeyspaces
	}

	if factory == nil {
		return health, nil
	}

	cli, err := factory()
	if err != nil || cli == nil {
		if err != nil {
			health.Status = "failed"
			health.Error = err.Error()
			health.Notes = append(health.Notes, fmt.Sprintf("factory error: %v", err))
		}
		return health, nil
	}
	defer cli.Close()

	scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	collectedAt := time.Now().Unix()

	for _, keyspace := range keyspaces {
		if err := collectKeyspace(scanCtx, g, cli, keyspace, collectedAt, &health); err != nil {
			// Non-fatal per keyspace; already recorded in health.
			_ = err
		}
	}

	if health.Status == "skipped" {
		// At least one keyspace was attempted.
		health.Status = "ok"
	}

	return health, nil
}

// freshnessMeta returns the standard freshness metadata block for an etcd node.
func freshnessMeta(rev int64, collectedAt int64) map[string]any {
	return map[string]any{
		"source_tier":        "cluster_authority",
		"collector":          "etcd_live_extractor",
		"etcd_revision":      rev,
		"cluster_scope":      "cluster_wide",
		"collected_at":       collectedAt,
		"ttl_seconds":        int64(300),
		"expires_at":         collectedAt + 300,
		"trust_level":        "observed",
		"confidence":         "high",
	}
}

// mergeMeta merges extra k/v pairs into a base metadata map.
func mergeMeta(base map[string]any, extra map[string]any) map[string]any {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// collectKeyspace dispatches collection for a single key prefix/key.
func collectKeyspace(ctx context.Context, g *graph.Graph, cli *clientv3.Client, keyspace string, collectedAt int64, health *CollectorHealth) error {
	switch {
	case keyspace == "/globular/system/config":
		return collectSingleKey(ctx, g, cli, keyspace, graph.NodeTypeClusterSystemConfig, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/resources/DesiredService/"):
		return collectDesiredServices(ctx, g, cli, keyspace, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/resources/ServiceRelease/"):
		return collectPrefixNodes(ctx, g, cli, keyspace, graph.NodeTypeServiceRelease, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/resources/InfrastructureRelease/"):
		return collectPrefixNodes(ctx, g, cli, keyspace, graph.NodeTypeInfrastructureRelease, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/nodes/"):
		return collectInstalledPackages(ctx, g, cli, keyspace, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/objectstore/"):
		return collectPrefixNodes(ctx, g, cli, keyspace, graph.NodeTypeObjectstoreDesired, collectedAt, health)

	case strings.HasPrefix(keyspace, "/globular/services/"):
		return collectPrefixNodes(ctx, g, cli, keyspace, graph.NodeTypeServiceRuntimeConfig, collectedAt, health)

	default:
		// Generic fallback: emit raw etcd key nodes.
		return collectPrefixNodes(ctx, g, cli, keyspace, graph.NodeTypeEtcdKey, collectedAt, health)
	}
}

// collectSingleKey fetches a single etcd key and emits a node of the given type.
func collectSingleKey(ctx context.Context, g *graph.Graph, cli *clientv3.Client, key string, nodeType string, collectedAt int64, health *CollectorHealth) error {
	resp, err := cli.Get(ctx, key)
	if err != nil {
		health.Status = "partial"
		health.Notes = append(health.Notes, fmt.Sprintf("key %s: get error: %v", key, err))
		return err
	}
	if len(resp.Kvs) == 0 {
		health.Notes = append(health.Notes, fmt.Sprintf("key %s: unexpectedly empty", key))
		return nil
	}
	kv := resp.Kvs[0]
	meta := freshnessMeta(resp.Header.GetRevision(), collectedAt)
	meta["etcd_key"] = key
	meta["value_size"] = len(kv.Value)

	// Try to decode JSON value into metadata fields.
	var raw map[string]any
	if json.Unmarshal(kv.Value, &raw) == nil {
		for k, v := range raw {
			// Don't overwrite freshness fields.
			if _, exists := meta[k]; !exists {
				meta[k] = v
			}
		}
	}

	n := graph.Node{
		ID:       "etcd:" + key,
		Type:     nodeType,
		Name:     key,
		Summary:  fmt.Sprintf("%s (rev=%d)", key, kv.ModRevision),
		Metadata: meta,
	}
	if err := g.AddNode(ctx, n); err != nil {
		return err
	}
	health.NodesEmitted++
	return nil
}

// collectPrefixNodes fetches all keys under a prefix and emits one node per key.
func collectPrefixNodes(ctx context.Context, g *graph.Graph, cli *clientv3.Client, prefix string, nodeType string, collectedAt int64, health *CollectorHealth) error {
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(1000))
	if err != nil {
		health.Status = "partial"
		health.Notes = append(health.Notes, fmt.Sprintf("prefix %s: get error: %v", prefix, err))
		return err
	}
	if len(resp.Kvs) == 0 {
		health.Notes = append(health.Notes, fmt.Sprintf("prefix %s: returned 0 keys (unexpected for live cluster)", prefix))
		return nil
	}
	rev := resp.Header.GetRevision()
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			name = key
		}
		meta := freshnessMeta(rev, collectedAt)
		meta["etcd_key"] = key

		// Attempt JSON decode for richer metadata.
		var raw map[string]any
		if json.Unmarshal(kv.Value, &raw) == nil {
			for k, v := range raw {
				if _, exists := meta[k]; !exists {
					meta[k] = v
				}
			}
		}

		n := graph.Node{
			ID:       "etcd:" + key,
			Type:     nodeType,
			Name:     name,
			Summary:  fmt.Sprintf("%s (rev=%d)", key, kv.ModRevision),
			Metadata: meta,
		}
		if err := g.AddNode(ctx, n); err != nil {
			continue
		}
		health.NodesEmitted++

		// Emit EtcdSnapshotContainsKey if there's a snapshot root node.
		snapshotID := "etcd_snapshot:latest"
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  snapshotID,
			Kind: graph.EdgeEtcdSnapshotContainsKey,
			Dst:  "etcd:" + key,
		})
	}
	return nil
}

// collectDesiredServices fetches /globular/resources/DesiredService/* and emits
// NodeTypeDesiredService nodes with edges to service nodes.
func collectDesiredServices(ctx context.Context, g *graph.Graph, cli *clientv3.Client, prefix string, collectedAt int64, health *CollectorHealth) error {
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(500))
	if err != nil {
		health.Status = "partial"
		health.Notes = append(health.Notes, fmt.Sprintf("desired services: get error: %v", err))
		return err
	}
	if len(resp.Kvs) == 0 {
		health.Notes = append(health.Notes, fmt.Sprintf("prefix %s: 0 keys returned", prefix))
		return nil
	}
	rev := resp.Header.GetRevision()
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		serviceName := strings.TrimPrefix(key, prefix)
		if serviceName == "" {
			continue
		}

		meta := freshnessMeta(rev, collectedAt)
		meta["etcd_key"] = key
		meta["service_name"] = serviceName

		// Decode the record for version/build_id.
		var raw map[string]any
		if json.Unmarshal(kv.Value, &raw) == nil {
			for k, v := range raw {
				if _, exists := meta[k]; !exists {
					meta[k] = v
				}
			}
		}

		// Also try the typed desiredVersionRecord form for backward compat.
		var rec desiredVersionRecord
		if json.Unmarshal(kv.Value, &rec) == nil && rec.Spec != nil {
			if rec.Spec.Version != "" {
				meta["desired_version"] = rec.Spec.Version
			}
			if rec.Spec.BuildID != "" {
				meta["desired_build_id"] = rec.Spec.BuildID
			}
		}

		nodeID := "etcd:" + key
		n := graph.Node{
			ID:       nodeID,
			Type:     graph.NodeTypeDesiredService,
			Name:     serviceName,
			Summary:  fmt.Sprintf("desired service %s (rev=%d)", serviceName, kv.ModRevision),
			Metadata: meta,
		}
		if err := g.AddNode(ctx, n); err != nil {
			continue
		}
		health.NodesEmitted++

		// Edge: desired service → existing service/package node (if present).
		serviceID := "package:" + serviceName
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  nodeID,
			Kind: graph.EdgeDesiredTargetsService,
			Dst:  serviceID,
		})

		// Maintain backward-compat drift detection (compares against receipt node).
		version, _ := meta["desired_version"].(string)
		if version != "" {
			detectDesiredInstalledDrift(ctx, g, serviceName, version, key)
		}
	}
	return nil
}

// collectInstalledPackages fetches /globular/nodes/*/packages/* and emits
// NodeTypeNodeInstalledPackage nodes.
func collectInstalledPackages(ctx context.Context, g *graph.Graph, cli *clientv3.Client, prefix string, collectedAt int64, health *CollectorHealth) error {
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(2000))
	if err != nil {
		health.Status = "partial"
		health.Notes = append(health.Notes, fmt.Sprintf("installed packages: get error: %v", err))
		return err
	}
	if len(resp.Kvs) == 0 {
		health.Notes = append(health.Notes, fmt.Sprintf("prefix %s: 0 keys returned", prefix))
		return nil
	}
	rev := resp.Header.GetRevision()
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		// Expect: /globular/nodes/<nodeID>/packages/<kind>/<name>
		parts := strings.SplitN(strings.TrimPrefix(key, prefix), "/", 4)
		if len(parts) != 4 || parts[1] != "packages" {
			continue
		}
		nodeID := parts[0]
		kind := parts[2]
		name := parts[3]
		if name == "" {
			continue
		}

		var rec installedPackageRecord
		_ = json.Unmarshal(kv.Value, &rec)
		if rec.Name == "" {
			rec.Name = name
		}
		if rec.Kind == "" {
			rec.Kind = kind
		}

		meta := freshnessMeta(rev, collectedAt)
		meta["etcd_key"] = key
		meta["node_id"] = nodeID
		meta["kind"] = rec.Kind
		meta["version"] = rec.Version

		graphNodeID := fmt.Sprintf("node:%s/installed/%s:%s", nodeID, rec.Kind, rec.Name)
		n := graph.Node{
			ID:       graphNodeID,
			Type:     graph.NodeTypeNodeInstalledPackage,
			Name:     rec.Name,
			Summary:  fmt.Sprintf("installed %s@%s on %s", rec.Name, rec.Version, nodeID),
			Metadata: meta,
		}
		if err := g.AddNode(ctx, n); err != nil {
			continue
		}
		health.NodesEmitted++

		// Link to canonical package node.
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  graphNodeID,
			Kind: graph.EdgeNodeReportsInstalledPackage,
			Dst:  "package:" + rec.Name,
		})
	}
	return nil
}

// readDesiredServices fetches all ServiceDesiredVersion keys from etcd.
// Kept for backward compat with internal callers if any.
func readDesiredServices(ctx context.Context, cli *clientv3.Client) (map[string]desiredVersionRecord, error) {
	prefix := "/globular/resources/ServiceDesiredVersion/"
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(500))
	if err != nil {
		return nil, err
	}
	out := make(map[string]desiredVersionRecord, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			continue
		}
		var rec desiredVersionRecord
		if err := json.Unmarshal(kv.Value, &rec); err != nil {
			continue
		}
		out[name] = rec
	}
	return out, nil
}

// readInstalledPackages fetches installed package entries from etcd node records.
// Kept for backward compat.
func readInstalledPackages(ctx context.Context, cli *clientv3.Client) (map[string][]installedPackageRecord, error) {
	prefix := "/globular/nodes/"
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(2000))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]installedPackageRecord)
	for _, kv := range resp.Kvs {
		key := string(kv.Key)
		// Expect: /globular/nodes/<nodeID>/packages/<kind>/<name>
		parts := strings.SplitN(strings.TrimPrefix(key, prefix), "/", 4)
		if len(parts) != 4 || parts[1] != "packages" {
			continue
		}
		nodeID := parts[0]
		kind := parts[2]
		name := parts[3]
		if name == "" {
			continue
		}
		var rec installedPackageRecord
		_ = json.Unmarshal(kv.Value, &rec)
		if rec.Name == "" {
			rec.Name = name
		}
		if rec.Kind == "" {
			rec.Kind = kind
		}
		out[nodeID] = append(out[nodeID], rec)
	}
	return out, nil
}

// detectDesiredInstalledDrift looks up the installed version of serviceName
// in the graph (as emitted by the receipt/varlib collector) and emits a
// HasStateDelta edge when the versions differ.
func detectDesiredInstalledDrift(ctx context.Context, g *graph.Graph, serviceName, desiredVersion, etcdKey string) {
	receiptID := "receipt:" + serviceName
	receiptNode, err := g.FindNode(ctx, receiptID)
	if err != nil || receiptNode == nil {
		return
	}
	installedVersion, _ := receiptNode.Metadata["version"].(string)
	if installedVersion == "" || installedVersion == desiredVersion {
		return
	}
	// Emit a state-delta edge marking the divergence.
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "etcd:" + etcdKey,
		Kind: graph.EdgeHasStateDelta,
		Dst:  receiptID,
		Metadata: map[string]any{
			"desired_version":   desiredVersion,
			"installed_version": installedVersion,
			"drift":             true,
		},
	})
}

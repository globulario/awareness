package bundlesync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ── Phase C.3: source discovery ──────────────────────────────────────────────
//
// DiscoverSources is the orchestrator's "where might a matching bundle live?"
// query. It is decoupled from etcd / MCP / cluster controller — callers
// implement NodeRegistry to plug in whichever registry they have. The MCP
// package provides an etcd-backed implementation; tests use an in-memory one.
//
// Ranking (highest priority first):
//
//   1. local_cache  — already-installed bundle in <LocalBundleDir>/current
//                     whose manifest matches the expected release. If present,
//                     this is by far the cheapest source and should be tried
//                     first; it short-circuits the orchestrator entirely.
//
//   2. gateway      — caller-supplied gateway/control URL. Single, well-known,
//                     trusted by definition.
//
//   3. peer_mcp     — entries from NodeRegistry filtered to ones that publish
//                     the exact build_id we want, sorted by LastSeen descending.
//                     Peers that lie about version/build_id are caught by
//                     PullBundle (manifest verification), so discovery is
//                     allowed to be optimistic.
//
// What this function does NOT do:
//
//   - reach out to peers (that's PullBundle)
//   - decide TLS trust (PullBundle's ClusterCAPool handles that)
//   - install or modify any path
//
// The returned candidates carry a Reason string for log/audit; the orchestrator
// (Phase C.4) is responsible for trying them in order and reporting which one
// produced the install.

// SourceKind classifies a candidate.
type SourceKind string

const (
	SourceKindLocalCache SourceKind = "local_cache"
	SourceKindGateway    SourceKind = "gateway"
	SourceKindPeer       SourceKind = "peer_mcp"
)

// SourceCandidate is a place a matching bundle might be obtained from.
type SourceCandidate struct {
	Kind     SourceKind `json:"kind"`
	NodeID   string     `json:"node_id,omitempty"`  // empty for non-peer kinds
	PeerURL  string     `json:"peer_url,omitempty"` // base URL for the puller
	Priority int        `json:"priority"`           // higher = tried first
	Reason   string     `json:"reason"`             // why this candidate was selected
	LastSeen time.Time  `json:"last_seen,omitempty"`
}

// NodeRegistryEntry is the projection of a peer record needed for discovery.
// It intentionally mirrors the relevant fields from the MCP node registry
// without dragging in MCP/etcd types.
type NodeRegistryEntry struct {
	NodeID                 string
	PeerURL                string
	ClusterID              string
	ReleaseVersion         string
	BuildID                string
	AwarenessBundleVersion string
	LastSeen               time.Time
	Status                 string // e.g. "RUNNING"
}

// NodeRegistry is the source of peer entries. Callers provide an
// implementation; tests use a fake; the MCP package provides one that reads
// /globular/mcp/nodes/* from etcd.
type NodeRegistry interface {
	ListNodes(ctx context.Context) ([]NodeRegistryEntry, error)
}

// DiscoveryOptions narrows discovery to the caller's release, cluster, and
// freshness constraints. Empty fields disable the corresponding check.
type DiscoveryOptions struct {
	// ExpectedRelease is the version+build_id we want to install. Peers that
	// don't publish a matching pair are excluded.
	ExpectedRelease ReleaseIndex

	// LocalNodeID excludes this node from the peer list. Pulling from
	// yourself doesn't help.
	LocalNodeID string

	// ClusterID, when non-empty, excludes peers that publish a different
	// cluster_id (or none). Defends against cross-cluster leakage in shared
	// dev environments.
	ClusterID string

	// MaxAge excludes peers whose LastSeen is older than this (relative to
	// the time this call is made). Zero disables the check.
	MaxAge time.Duration

	// GatewayURL, when non-empty, is added as a single SourceKindGateway
	// candidate at priority above peers but below local cache.
	GatewayURL string

	// LocalBundleDir is the path that contains the active bundle layout
	// (e.g. /var/lib/globular/awareness). When the manifest under
	// <dir>/current/manifest.json matches ExpectedRelease, we add a
	// local_cache candidate at the highest priority.
	LocalBundleDir string

	// RequireRunningStatus, when true, excludes peers whose Status is not
	// "RUNNING". Default false (some registries don't write Status; tolerate).
	RequireRunningStatus bool

	// Now lets tests inject a deterministic clock for the MaxAge check.
	// Production callers leave this zero and time.Now() is used.
	Now time.Time
}

// Priorities are coarse so a quality tie within a kind preserves the
// LastSeen-based sort. Operators reading the response can also infer the kind
// at a glance.
const (
	priorityLocalCache = 300
	priorityGateway    = 200
	priorityPeerBase   = 100 // refined by recency rank
)

// DiscoverSources returns a ranked candidate list. An empty result is not an
// error — it just means no source is currently usable, and the orchestrator
// should publish AWARENESS_BUNDLE_SOURCE_UNAVAILABLE.
func DiscoverSources(ctx context.Context, opts DiscoveryOptions, registry NodeRegistry) ([]SourceCandidate, error) {
	if opts.ExpectedRelease.Version == "" || opts.ExpectedRelease.BuildID == "" {
		return nil, fmt.Errorf("DiscoverSources: expected_release.version and build_id are required")
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var out []SourceCandidate

	// (1) Local cache. We trust the manifest sidecar's claim AT discovery time;
	// the orchestrator will run a real verification before treating this
	// source as usable. If verification fails the orchestrator falls through
	// to the next candidate.
	if opts.LocalBundleDir != "" {
		if c, ok := localCacheCandidate(opts.LocalBundleDir, opts.ExpectedRelease); ok {
			out = append(out, c)
		}
	}

	// (2) Gateway. Single candidate, fixed priority.
	if opts.GatewayURL != "" {
		out = append(out, SourceCandidate{
			Kind:     SourceKindGateway,
			PeerURL:  opts.GatewayURL,
			Priority: priorityGateway,
			Reason:   "gateway URL configured",
		})
	}

	// (3) Peers from registry.
	if registry != nil {
		peers, err := registry.ListNodes(ctx)
		if err != nil {
			// We intentionally don't fail the whole discovery just because
			// the peer registry is down. Local cache + gateway may still
			// suffice. Caller logs the error via the returned slice's
			// completeness.
			peers = nil
		}
		out = append(out, rankPeers(peers, opts, now)...)
	}

	// Stable sort by priority descending; LastSeen ties broken by NodeID.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].NodeID < out[j].NodeID
	})

	return out, nil
}

// localCacheCandidate returns a candidate when <dir>/current/manifest.json
// exists and its version/build_id match the expected release.
//
// We read the manifest with bundlesync.LoadManifest so the parsing rules stay
// consistent. We do NOT verify the bundle's sha256 here — the orchestrator
// is responsible for re-running the full verify before using the local cache.
// Discovery's job is just to nominate it.
func localCacheCandidate(bundleRoot string, expected ReleaseIndex) (SourceCandidate, bool) {
	manifestPath := filepath.Join(bundleRoot, "current", "manifest.json")
	if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
		return SourceCandidate{}, false
	}
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return SourceCandidate{}, false
	}
	if m.Version != expected.Version || m.BuildID != expected.BuildID {
		return SourceCandidate{}, false
	}
	return SourceCandidate{
		Kind:     SourceKindLocalCache,
		Priority: priorityLocalCache,
		Reason:   fmt.Sprintf("local current/manifest.json matches expected %s/%s", expected.Version, expected.BuildID),
	}, true
}

// rankPeers applies the filter + sort rules to a flat list of registry
// entries. Returns candidates with Priority offset by their recency rank
// so the global stable sort preserves intra-kind order.
func rankPeers(peers []NodeRegistryEntry, opts DiscoveryOptions, now time.Time) []SourceCandidate {
	if len(peers) == 0 {
		return nil
	}

	filtered := make([]NodeRegistryEntry, 0, len(peers))
	for _, p := range peers {
		if !peerMatchesFilter(p, opts, now) {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 0 {
		return nil
	}

	// Sort by LastSeen descending so the most recently seen peer ranks first.
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].LastSeen.Equal(filtered[j].LastSeen) {
			return filtered[i].LastSeen.After(filtered[j].LastSeen)
		}
		return filtered[i].NodeID < filtered[j].NodeID
	})

	out := make([]SourceCandidate, 0, len(filtered))
	for i, p := range filtered {
		out = append(out, SourceCandidate{
			Kind:     SourceKindPeer,
			NodeID:   p.NodeID,
			PeerURL:  p.PeerURL,
			Priority: priorityPeerBase - i, // -i preserves recency order via stable sort
			Reason:   peerSelectionReason(p),
			LastSeen: p.LastSeen,
		})
	}
	return out
}

// peerMatchesFilter returns true when a registry entry passes every filter
// the caller configured. Empty/zero fields on opts disable that filter.
func peerMatchesFilter(p NodeRegistryEntry, opts DiscoveryOptions, now time.Time) bool {
	// Self-exclusion.
	if opts.LocalNodeID != "" && p.NodeID == opts.LocalNodeID {
		return false
	}
	// Empty PeerURL is not pullable.
	if p.PeerURL == "" {
		return false
	}
	// Cluster scoping — defend against cross-cluster bundle leakage.
	if opts.ClusterID != "" && p.ClusterID != opts.ClusterID {
		return false
	}
	// Release pinning — peer must publish the build_id we want. The
	// awareness_bundle_version is checked too when the peer publishes one,
	// but if it's empty we trust the build_id alone (older publishers may
	// not have populated AwarenessBundleVersion).
	if p.BuildID != opts.ExpectedRelease.BuildID {
		return false
	}
	if p.AwarenessBundleVersion != "" && p.AwarenessBundleVersion != opts.ExpectedRelease.Version {
		return false
	}
	// Recency.
	if opts.MaxAge > 0 && !p.LastSeen.IsZero() {
		if now.Sub(p.LastSeen) > opts.MaxAge {
			return false
		}
	}
	// Status filter.
	if opts.RequireRunningStatus && p.Status != "" && p.Status != "RUNNING" {
		return false
	}
	return true
}

// peerSelectionReason returns a short human-readable reason string for a
// peer candidate. Surfaces the LastSeen so log readers can tell stale-but-
// included peers from fresh ones.
func peerSelectionReason(p NodeRegistryEntry) string {
	if p.LastSeen.IsZero() {
		return fmt.Sprintf("peer %s publishes build_id=%s", p.NodeID, p.BuildID)
	}
	return fmt.Sprintf("peer %s publishes build_id=%s (last_seen=%s)", p.NodeID, p.BuildID, p.LastSeen.UTC().Format(time.RFC3339))
}

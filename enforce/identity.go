// Package enforce/identity provides canonical ID normalization for the awareness graph.
// Every cross-layer node type gets a canonical prefix so that graph joins between
// package specs, systemd units, etcd state, and artifacts resolve correctly.
//
// Canonical prefixes:
//
//	package:<name>           Globular package (e.g. package:minio)
//	service:<name>           gRPC/globular service (e.g. service:workflow-service)
//	unit:<name>.service      systemd unit (e.g. unit:globular-minio.service)
//	artifact:<name>@<ver>    BOM artifact identity (e.g. artifact:minio@1.2.20)
//	node:<id>                Cluster node (e.g. node:globule-ryzen)
//	build:<build_id>         Build UUID identity (e.g. build:23bf5442-...)
//	platform:<tag>           Platform release tag (e.g. platform:v1.2.20)
//	profile:<name>           Node profile (e.g. profile:storage)
//	etcd:<key>               etcd key path (e.g. etcd:/globular/resources/...)
//	cert:<path>              PKI certificate file (e.g. cert:/var/lib/globular/...)
//	receipt:<name>           Installed artifact receipt (e.g. receipt:minio)
//	config:<name>            Configuration file or env file (e.g. config:minio.env)
//	script:<rel_path>        Script file (e.g. script:scripts/ensure-bootstrap.sh)
//	failure_mode:<id>        Known failure mode (e.g. failure_mode:minio.quorum_loss)
//	invariant:<id>           Invariant ID (e.g. invariant:minio.minimum_3_nodes)
//	workflow:<name>          Workflow name (e.g. workflow:minio-topology-repair)

package enforce

import (
	"context"
	"strings"

	"github.com/globulario/awareness/graph"
)

// SourceTier identifies which collector produced a node.
type SourceTier string

const (
	TierPackageSpec       SourceTier = "package_spec"
	TierInstallerScript   SourceTier = "installer_script"
	TierSystemdRuntime    SourceTier = "systemd_runtime"
	TierInstalledMetadata SourceTier = "installed_metadata"
	TierRepositoryManifest SourceTier = "repository_manifest"
	TierEtcdDesiredState  SourceTier = "etcd_desired_state"
	TierGoSource          SourceTier = "go_source"
)

// NormalizeID returns the canonical graph node ID for a given tier and raw name.
// If raw already has a canonical prefix it is returned unchanged.
func NormalizeID(tier SourceTier, raw string) string {
	// Already canonical — leave alone.
	for _, pfx := range []string{
		"package:", "service:", "unit:", "artifact:", "node:",
		"build:", "platform:", "profile:", "etcd:", "cert:",
		"receipt:", "config:", "script:", "failure_mode:", "invariant:",
		"workflow:", "unit_template:",
	} {
		if strings.HasPrefix(raw, pfx) {
			return raw
		}
	}

	switch tier {
	case TierPackageSpec:
		if strings.HasSuffix(raw, ".service") {
			return "unit:" + raw
		}
		return "package:" + raw
	case TierSystemdRuntime:
		if strings.HasSuffix(raw, ".service") {
			return "unit:" + raw
		}
		return "unit:" + raw + ".service"
	case TierInstalledMetadata:
		if strings.HasSuffix(raw, ".crt") || strings.HasSuffix(raw, ".pem") {
			return "cert:" + raw
		}
		if strings.HasSuffix(raw, ".json") {
			return "receipt:" + strings.TrimSuffix(raw, ".json")
		}
		return "receipt:" + raw
	case TierRepositoryManifest:
		if strings.Contains(raw, "@") {
			return "artifact:" + raw
		}
		return "artifact:" + raw
	case TierEtcdDesiredState:
		if strings.HasPrefix(raw, "/globular/") {
			return "etcd:" + raw
		}
		return "node:" + raw
	case TierInstallerScript:
		return "script:" + raw
	default:
		return raw
	}
}

// ResolveNode tries multiple canonical prefixes to find a node in the graph.
// It returns the first node found, or nil if none match.
func ResolveNode(ctx context.Context, g *graph.Graph, raw string) (*graph.Node, error) {
	// Try exact ID first.
	if n, err := g.FindNode(ctx, raw); err == nil && n != nil {
		return n, nil
	}

	// Try common canonical forms.
	candidates := canonicalCandidates(raw)
	for _, id := range candidates {
		if id == raw {
			continue
		}
		if n, err := g.FindNode(ctx, id); err == nil && n != nil {
			return n, nil
		}
	}
	return nil, nil
}

// canonicalCandidates returns all plausible canonical IDs for a raw name.
func canonicalCandidates(raw string) []string {
	candidates := []string{raw}
	base := raw

	// Strip known prefixes for variant generation.
	for _, pfx := range []string{
		"package:", "service:", "unit:", "artifact:", "node:",
		"build:", "platform:", "profile:", "etcd:", "cert:",
		"receipt:", "config:", "script:", "failure_mode:", "invariant:",
		"workflow:",
	} {
		if strings.HasPrefix(raw, pfx) {
			base = strings.TrimPrefix(raw, pfx)
			break
		}
	}

	// Generate all prefix variants for the base name.
	candidates = append(candidates,
		"package:"+base,
		"service:"+base,
		"unit:"+base,
		"receipt:"+base,
		"artifact:"+base,
		"invariant:"+base,
		"workflow:"+base,
	)

	// systemd unit variants.
	if strings.HasSuffix(base, ".service") {
		candidates = append(candidates, "unit:"+base)
	} else {
		candidates = append(candidates,
			"unit:globular-"+base+".service",
			"unit:"+base+".service",
		)
	}

	return candidates
}

// CollectorResult records the outcome of a single collector pass.
// This is the unified health type used across all clusterstate collectors.
type CollectorResult struct {
	CollectorID  string `json:"collector_id"`
	SourceTier   string `json:"source_tier"`
	Status       string `json:"status"`        // "ok" | "skipped" | "error"
	NodesEmitted int    `json:"nodes_emitted"`
	Error        string `json:"error,omitempty"`
	Priority     string `json:"priority"`      // "P0" | "P1"
}

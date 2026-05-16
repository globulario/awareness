// Package clusterstate collects live cluster-state metadata from the local
// node: systemd unit files, /var/lib/globular metadata, and PKI certificates.
// These collectors produce source_tier=systemd_runtime and
// source_tier=installed_metadata graph nodes that join Layer 3 (Installed) to
// Layer 4 (Runtime) in the 4-layer state model.
//
// Safety rules:
//   - Never write to etcd
//   - Never read private keys, tokens, or credentials
//   - All collection is read-only
//   - Missing paths are silently skipped (CollectorHealth.Status = "skipped")
package clusterstate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/graph"
)

const (
	sourceTierSystemd   = "systemd_runtime"
	sourceTierInstalled = "installed_metadata"
)

// CollectorHealth reports the result of a collection pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "skipped" | "partial" | "failed" | "error"
	NodesEmitted int
	Error        string
	Notes        []string // advisory notes (empty keyspace, partial coverage, etc.)
}

// SystemdDir is the default location for systemd unit files.
// Override for testing.
var SystemdDir = "/etc/systemd/system"

// CollectSystemd walks SystemdDir for globular-*.service files, reads each
// unit file, compares it against the .sha256 sidecar, reads drop-in
// overrides, and emits SystemdUnit nodes into the graph.
//
// Missing SystemdDir returns CollectorHealth{Status:"skipped"}.
func CollectSystemd(ctx context.Context, g *graph.Graph) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "systemd",
		SourceTier:  sourceTierSystemd,
	}

	if _, err := os.Stat(SystemdDir); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("systemd dir not found: %s", SystemdDir)
		return health, nil
	}

	entries, err := os.ReadDir(SystemdDir)
	if err != nil {
		health.Status = "error"
		health.Error = err.Error()
		return health, nil
	}

	collectedAt := time.Now().Unix()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "globular-") || !strings.HasSuffix(name, ".service") {
			continue
		}

		unitPath := filepath.Join(SystemdDir, name)
		n, err := indexSystemdUnit(ctx, g, name, unitPath, collectedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clusterstate/systemd: skip %s: %v\n", name, err)
			continue
		}
		health.NodesEmitted += n
	}

	health.Status = "ok"
	return health, nil
}

func indexSystemdUnit(ctx context.Context, g *graph.Graph, name, unitPath string, collectedAt int64) (int, error) {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return 0, err
	}

	// Compute SHA-256 of the unit file.
	sum := sha256.Sum256(data)
	fileHash := hex.EncodeToString(sum[:])

	// Compare with .sha256 sidecar.
	sidecarPath := unitPath + ".sha256"
	sidecarMatch := false
	if sidecarData, err := os.ReadFile(sidecarPath); err == nil {
		sidecarHash := strings.TrimSpace(string(sidecarData))
		sidecarMatch = (fileHash == sidecarHash)
	}

	// Parse ExecStart and ExecStartPre lines.
	execStart := extractDirective(data, "ExecStart=")
	execStartPre := extractDirective(data, "ExecStartPre=")

	// Count drop-in overrides.
	dropinDir := unitPath + ".d"
	dropinCount := 0
	if dropinEntries, err := os.ReadDir(dropinDir); err == nil {
		for _, de := range dropinEntries {
			if !de.IsDir() && strings.HasSuffix(de.Name(), ".conf") {
				dropinCount++
			}
		}
	}

	// Package name from unit name: "globular-minio.service" → "minio"
	pkgName := strings.TrimPrefix(name, "globular-")
	pkgName = strings.TrimSuffix(pkgName, ".service")

	unitID := "unit:" + name
	meta := map[string]any{
		"source_tier":   sourceTierSystemd,
		"collected_at":  collectedAt,
		"file_hash":     fileHash,
		"sidecar_match": sidecarMatch,
		"dropin_count":  dropinCount,
		"package":       pkgName,
	}
	if execStart != "" {
		meta["exec_start"] = execStart
	}
	if execStartPre != "" {
		meta["exec_start_pre"] = execStartPre
	}

	if err := g.AddNode(ctx, graph.Node{
		ID:      unitID,
		Type:    graph.NodeTypeSystemdUnit,
		Name:    name,
		Path:    unitPath,
		Summary: fmt.Sprintf("systemd unit: sidecar_match=%v dropin_count=%d", sidecarMatch, dropinCount),
		Metadata: meta,
	}); err != nil {
		return 0, err
	}

	// Link unit → package if package node exists.
	pkgID := "package:" + pkgName
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  unitID,
		Kind: graph.EdgeCurrentStatusOf,
		Dst:  pkgID,
		Metadata: map[string]any{
			"source_tier": sourceTierSystemd,
			"edge_note":   "runtime_unit_to_package",
		},
	})

	return 1, nil
}

// extractDirective finds the first line starting with the given directive key
// and returns the value after the = sign.
func extractDirective(data []byte, key string) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, key) {
			return strings.TrimPrefix(line, key)
		}
	}
	return ""
}

// WalkDropIns reads all .conf files in the drop-in directory for a unit and
// returns their contents as a map of filename → content.
func WalkDropIns(unitName string) (map[string]string, error) {
	dropinDir := filepath.Join(SystemdDir, unitName+".d")
	result := map[string]string{}

	err := filepath.WalkDir(dropinDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".conf") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		result[d.Name()] = string(data)
		return nil
	})
	return result, err
}

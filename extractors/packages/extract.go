// Package packages extracts package nodes from Globular package.json manifests
// and release-index.json files.
package packages

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
)

// packageManifest mirrors the relevant fields of a Globular package.json.
type packageManifest struct {
	Name         string   `json:"name"`
	ServiceName  string   `json:"service_name"`
	Version      string   `json:"version"`
	Kind         string   `json:"kind"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
}

// releaseIndex mirrors the top level of release-index.json.
type releaseIndex struct {
	PlatformRelease string `json:"platform_release"`
	ReleaseTag      string `json:"release_tag"`
	Packages        []struct {
		Name               string `json:"name"`
		Version            string `json:"version"`
		Kind               string `json:"kind"`
		BuildNumber        int    `json:"build_number"`
		BuildID            string `json:"build_id"`
		PackageDigest      string `json:"package_digest"`
		EntrypointChecksum string `json:"entrypoint_checksum"`
		Profiles           []string `json:"profiles"`
		ChangedInRelease   bool   `json:"changed_in_release"`
	} `json:"packages"`
}

// Extract walks repoRoot for package.json files and the release-index.json.
func Extract(ctx context.Context, g *graph.Graph, repoRoot string) error {
	// Walk for package.json manifests.
	if err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "package.json" {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		return extractPackageManifest(ctx, g, path, rel)
	}); err != nil {
		return err
	}

	// Try release-index.json in common locations.
	for _, candidate := range []string{
		filepath.Join(repoRoot, "release-index.json"),
		filepath.Join(repoRoot, "packages", "release-index.json"),
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			rel, _ := filepath.Rel(repoRoot, candidate)
			_ = extractReleaseIndex(ctx, g, data, rel)
			break
		}
	}

	return nil
}

func extractPackageManifest(ctx context.Context, g *graph.Graph, absPath, relPath string) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}

	var m packageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	if m.Name == "" {
		return nil
	}

	pkgID := "package:" + m.Name
	if err := g.AddNode(ctx, graph.Node{
		ID:      pkgID,
		Type:    graph.NodeTypePackage,
		Name:    m.Name,
		Path:    relPath,
		Summary: m.Description,
		Metadata: map[string]any{
			"version": m.Version,
			"kind":    m.Kind,
		},
	}); err != nil {
		return err
	}

	// Link package to its globular service.
	if m.ServiceName != "" {
		svcID := "service:" + m.ServiceName
		_ = g.AddNode(ctx, graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: m.ServiceName})
		_ = g.AddEdge(ctx, graph.Edge{Src: pkgID, Kind: graph.EdgeOwns, Dst: svcID})
	}

	// Dependencies.
	for _, dep := range m.Dependencies {
		depID := "package:" + dep
		_ = g.AddNode(ctx, graph.Node{ID: depID, Type: graph.NodeTypePackage, Name: dep})
		_ = g.AddEdge(ctx, graph.Edge{Src: pkgID, Kind: graph.EdgeDependsOn, Dst: depID})
	}

	return nil
}

func extractReleaseIndex(ctx context.Context, g *graph.Graph, data []byte, relPath string) error {
	var idx releaseIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil
	}

	// Emit a platform release node.
	if idx.ReleaseTag != "" {
		platformID := "platform:" + idx.ReleaseTag
		_ = g.AddNode(ctx, graph.Node{
			ID:   platformID,
			Type: "platform_release",
			Name: idx.ReleaseTag,
			Path: relPath,
			Metadata: map[string]any{
				"platform_release": idx.PlatformRelease,
				"source_tier":      "repository_manifest",
			},
		})
	}

	for _, p := range idx.Packages {
		if p.Name == "" {
			continue
		}
		pkgID := "package:" + p.Name
		artifactID := "artifact:" + p.Name + "@" + p.Version
		meta := map[string]any{
			"version":              p.Version,
			"kind":                 p.Kind,
			"build_number":         p.BuildNumber,
			"build_id":             p.BuildID,
			"package_digest":       p.PackageDigest,
			"entrypoint_checksum":  p.EntrypointChecksum,
			"changed_in_release":   p.ChangedInRelease,
			"source_tier":          "repository_manifest",
		}
		if len(p.Profiles) > 0 {
			var ps strings.Builder
			for i, prof := range p.Profiles {
				if i > 0 {
					ps.WriteByte(',')
				}
				ps.WriteString(prof)
			}
			meta["profiles"] = ps.String()
		}

		// Package node — upsert with extended manifest data.
		_ = g.AddNode(ctx, graph.Node{
			ID:   pkgID,
			Type: graph.NodeTypePackage,
			Name: p.Name,
			Path: relPath,
			Metadata: map[string]any{
				"version":     p.Version,
				"kind":        p.Kind,
				"source_tier": "repository_manifest",
			},
		})

		// Artifact node — identity-level (name@version with build_id, checksum).
		_ = g.AddNode(ctx, graph.Node{
			ID:       artifactID,
			Type:     "artifact",
			Name:     p.Name + "@" + p.Version,
			Path:     relPath,
			Summary:  p.Name + " v" + p.Version + " build#" + itoa(p.BuildNumber),
			Metadata: meta,
		})

		// Artifact → Package edge.
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  artifactID,
			Kind: graph.EdgeDependsOn,
			Dst:  pkgID,
			Metadata: map[string]any{"source_tier": "repository_manifest"},
		})

		// Artifact → Platform edge.
		if idx.ReleaseTag != "" {
			platformID := "platform:" + idx.ReleaseTag
			_ = g.AddEdge(ctx, graph.Edge{
				Src:  artifactID,
				Kind: graph.EdgeDependsOn,
				Dst:  platformID,
				Metadata: map[string]any{"source_tier": "repository_manifest"},
			})
		}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Package clusterspec extracts package spec metadata from the Globular packages
// metadata repository. It reads package.json, awareness.yaml, and systemd
// service unit templates to populate the awareness graph with cluster-level
// package identity, profiles, invariants, and failure modes.
//
// Source tier: package_spec
// This extractor covers coverage that the Go AST extractor cannot — it indexes
// declarative package specs that define what gets installed where and what health
// invariants apply.
package clusterspec

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// CollectorHealth reports the result of a single collection pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "skipped" | "error"
	NodesEmitted int
	Error        string
}

const sourceTierPackageSpec = "package_spec"

// Extract walks the packages metadata repository at metaRoot (e.g.
// /path/to/packages/metadata) and indexes every package it finds into the
// awareness graph.
//
// A missing or inaccessible metaRoot returns CollectorHealth{Status:"skipped"}
// rather than an error — the graph degrades gracefully.
func Extract(ctx context.Context, g *graph.Graph, metaRoot string) (CollectorHealth, error) {
	health := CollectorHealth{
		CollectorID: "clusterspec",
		SourceTier:  sourceTierPackageSpec,
	}

	if metaRoot == "" {
		health.Status = "skipped"
		health.Error = "no packages metadata repo configured"
		return health, nil
	}

	if _, err := os.Stat(metaRoot); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("packages metadata repo not found: %s", metaRoot)
		return health, nil
	}

	entries, err := os.ReadDir(metaRoot)
	if err != nil {
		health.Status = "error"
		health.Error = err.Error()
		return health, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkgDir := filepath.Join(metaRoot, entry.Name())
		n, err := indexPackageDir(ctx, g, pkgDir, entry.Name())
		if err != nil {
			fmt.Fprintf(os.Stderr, "clusterspec: skip %s: %v\n", entry.Name(), err)
			continue
		}
		health.NodesEmitted += n
	}

	health.Status = "ok"
	return health, nil
}

// packageJSON mirrors the fields relevant to awareness from a Globular package.json.
type packageJSON struct {
	Name                 string   `json:"name"`
	Type                 string   `json:"type"`
	Version              string   `json:"version"`
	Description          string   `json:"description"`
	Profiles             []string `json:"profiles"`
	SystemdUnit          string   `json:"systemd_unit"`
	ProvidesCapabilities []string `json:"provides_capabilities"`
	EntrypointChecksum   string   `json:"entrypoint_checksum"`
	HealthCheckUnit      string   `json:"health_check_unit"`
	HealthCheckPort      int      `json:"health_check_port"`
}

// awarenessYAML mirrors the relevant fields of an awareness.yaml contract file.
type awarenessYAML struct {
	Service     string `yaml:"service"`
	Package     string `yaml:"package"`
	PackageKind string `yaml:"package_kind"`
	Summary     string `yaml:"summary"`
	Owns        struct {
		SystemdUnits    []string `yaml:"systemd_units"`
		EtcdKeys        []string `yaml:"etcd_keys"`
		FilesystemPaths []string `yaml:"filesystem_paths"`
	} `yaml:"owns"`
	Reads struct {
		EtcdKeys []string `yaml:"etcd_keys"`
	} `yaml:"reads"`
	DependsOn            []string           `yaml:"depends_on"`
	Invariants           []string           `yaml:"invariants"`
	ForbiddenFixes       []string           `yaml:"forbidden_fixes"`
	KnownFailureModes    []knownFailureMode `yaml:"known_failure_modes"`
	RemediationWorkflows []string           `yaml:"remediation_workflows"`
	RequiredTests        []string           `yaml:"required_tests"`
}

type knownFailureMode struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Diagnosis   string `yaml:"diagnosis"`
	Remedy      string `yaml:"remedy"`
}

// templateVarRE matches Go template expressions like {{.Prefix}} or {{.NodeIP}}.
var templateVarRE = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

func indexPackageDir(ctx context.Context, g *graph.Graph, pkgDir, dirName string) (int, error) {
	emitted := 0

	// Read package.json.
	var pj packageJSON
	pjPath := filepath.Join(pkgDir, "package.json")
	if data, err := os.ReadFile(pjPath); err == nil {
		_ = json.Unmarshal(data, &pj)
	}

	pkgName := pj.Name
	if pkgName == "" {
		pkgName = dirName // fallback to directory name
	}

	// Read awareness.yaml (optional).
	var aw awarenessYAML
	awPath := filepath.Join(pkgDir, "awareness.yaml")
	if data, err := os.ReadFile(awPath); err == nil {
		_ = yaml.Unmarshal(data, &aw)
	}

	kind := pj.Type
	if kind == "" {
		kind = aw.PackageKind
	}
	summary := pj.Description
	if summary == "" {
		summary = aw.Summary
	}

	meta := map[string]any{
		"source_tier": sourceTierPackageSpec,
		"kind":        kind,
		"version":     pj.Version,
	}
	if pj.SystemdUnit != "" {
		meta["systemd_unit"] = pj.SystemdUnit
	}
	if pj.EntrypointChecksum != "" {
		meta["entrypoint_checksum"] = pj.EntrypointChecksum
	}
	if pj.HealthCheckPort != 0 {
		meta["health_check_port"] = pj.HealthCheckPort
	}
	if len(pj.ProvidesCapabilities) > 0 {
		meta["provides_capabilities"] = strings.Join(pj.ProvidesCapabilities, ",")
	}
	if aw.PackageKind == "infrastructure" && len(pj.Profiles) == 0 {
		meta["profile_note"] = "infrastructure: all founding nodes"
	}

	pkgID := "package:" + pkgName
	if err := g.AddNode(ctx, graph.Node{
		ID:       pkgID,
		Type:     graph.NodeTypePackage,
		Name:     pkgName,
		Path:     pkgDir,
		Summary:  summary,
		Metadata: meta,
	}); err != nil {
		return 0, err
	}
	emitted++

	// Profile edges: Package → profile (requires).
	for _, profile := range pj.Profiles {
		profileID := "profile:" + profile
		_ = g.AddNode(ctx, graph.Node{
			ID:   profileID,
			Type: "node_profile",
			Name: profile,
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeRequires,
			Dst:      profileID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
	}

	// Systemd unit node + owns edge.
	unitName := pj.SystemdUnit
	if unitName == "" && len(aw.Owns.SystemdUnits) > 0 {
		unitName = aw.Owns.SystemdUnits[0]
	}
	if unitName != "" {
		unitID := "unit:" + unitName
		_ = g.AddNode(ctx, graph.Node{
			ID:   unitID,
			Type: graph.NodeTypeSystemdUnit,
			Name: unitName,
			Metadata: map[string]any{
				"source_tier": sourceTierPackageSpec,
				"package":     pkgName,
			},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeOwns,
			Dst:      unitID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		emitted++
	}

	// Additional units from awareness.yaml owns.
	for _, u := range aw.Owns.SystemdUnits {
		if u == unitName {
			continue
		}
		uID := "unit:" + u
		_ = g.AddNode(ctx, graph.Node{
			ID:   uID,
			Type: graph.NodeTypeSystemdUnit,
			Name: u,
			Metadata: map[string]any{
				"source_tier": sourceTierPackageSpec,
				"package":     pkgName,
			},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeOwns,
			Dst:      uID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		emitted++
	}

	// etcd keys written (owned).
	for _, k := range aw.Owns.EtcdKeys {
		keyID := "etcd:" + k
		_ = g.AddNode(ctx, graph.Node{
			ID:   keyID,
			Type: graph.NodeTypeEtcdKey,
			Name: k,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeWrites,
			Dst:      keyID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		emitted++
	}

	// etcd keys read.
	for _, k := range aw.Reads.EtcdKeys {
		keyID := "etcd:" + k
		_ = g.AddNode(ctx, graph.Node{
			ID:   keyID,
			Type: graph.NodeTypeEtcdKey,
			Name: k,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeReads,
			Dst:      keyID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
	}

	// Dependency edges.
	for _, dep := range aw.DependsOn {
		depID := "package:" + dep
		_ = g.AddNode(ctx, graph.Node{ID: depID, Type: graph.NodeTypePackage, Name: dep})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeDependsOn,
			Dst:      depID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
	}

	// Known failure mode nodes.
	for _, fm := range aw.KnownFailureModes {
		if fm.ID == "" {
			continue
		}
		fmID := "failure_mode:" + pkgName + "." + fm.ID
		_ = g.AddNode(ctx, graph.Node{
			ID:      fmID,
			Type:    graph.NodeTypeFailureMode,
			Name:    fm.ID,
			Summary: fm.Description,
			Metadata: map[string]any{
				"source_tier": sourceTierPackageSpec,
				"diagnosis":   fm.Diagnosis,
				"remedy":      fm.Remedy,
				"package":     pkgName,
			},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeProduces,
			Dst:      fmID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
		emitted++
	}

	// Invariant links.
	for _, inv := range aw.Invariants {
		invID := "invariant:" + inv
		_ = g.AddNode(ctx, graph.Node{ID: invID, Type: graph.NodeTypeInvariant, Name: inv})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeEnforces,
			Dst:      invID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
	}

	// Remediation workflow links.
	for _, wf := range aw.RemediationWorkflows {
		wfID := "workflow:" + wf
		_ = g.AddNode(ctx, graph.Node{ID: wfID, Type: graph.NodeTypeWorkflow, Name: wf})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:      pkgID,
			Kind:     graph.EdgeRemediatedBy,
			Dst:      wfID,
			Metadata: map[string]any{"source_tier": sourceTierPackageSpec},
		})
	}

	// Systemd unit template vars from systemd/*.service template files.
	templateVars, tmplUnitFile := extractTemplateVars(pkgDir)
	if tmplUnitFile != "" {
		tmplID := "unit_template:" + pkgName
		_ = g.AddNode(ctx, graph.Node{
			ID:   tmplID,
			Type: graph.NodeTypeSystemdUnit,
			Name: tmplUnitFile,
			Path: filepath.Join(pkgDir, "systemd", tmplUnitFile),
			Metadata: map[string]any{
				"source_tier":   sourceTierPackageSpec,
				"template_vars": strings.Join(templateVars, ","),
				"is_template":   true,
				"package":       pkgName,
			},
		})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  pkgID,
			Kind: graph.EdgeOwns,
			Dst:  tmplID,
			Metadata: map[string]any{
				"source_tier": sourceTierPackageSpec,
				"edge_note":   "unit_template",
			},
		})
		emitted++
	}

	return emitted, nil
}

// extractTemplateVars reads systemd/*.service template files from a package
// directory and returns the unique template variable names and the first unit
// file name found.
func extractTemplateVars(pkgDir string) (vars []string, unitFile string) {
	systemdDir := filepath.Join(pkgDir, "systemd")
	seen := map[string]bool{}

	_ = filepath.WalkDir(systemdDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".service") {
			return nil
		}
		if unitFile == "" {
			unitFile = d.Name()
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range templateVarRE.FindAllStringSubmatch(string(data), -1) {
			v := m[1]
			if !seen[v] {
				seen[v] = true
				vars = append(vars, v)
			}
		}
		return nil
	})
	return vars, unitFile
}

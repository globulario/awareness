package packages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// Package kind values — aligned with the Globular registry (lowercase canonical form).
// The validator accepts both lowercase and uppercase spellings.
const (
	KindInfrastructure = "infrastructure"
	KindService        = "service"
	KindSubsystem      = "subsystem"
	KindPlatform       = "platform"       // conceptual alias for core service tier
	KindApplication    = "application"
	KindAgent          = "agent"
	KindCommand        = "command"
	KindExperimental   = "experimental"
)

// RequiresAwarenessContract returns true for package kinds that must supply awareness.yaml.
// Missing awareness.yaml for these kinds results in BLOCK during admission.
func RequiresAwarenessContract(kind string) bool {
	switch strings.ToLower(kind) {
	case KindInfrastructure, KindService, KindSubsystem, KindPlatform:
		return true
	}
	return false
}

// isKnownKind returns true if the kind is one the validator recognises.
func isKnownKind(kind string) bool {
	switch strings.ToLower(kind) {
	case KindInfrastructure, KindService, KindSubsystem, KindPlatform,
		KindApplication, KindAgent, KindCommand, KindExperimental:
		return true
	}
	return false
}

// AwarenessContract is the parsed content of a package awareness.yaml file.
// It is the operational passport of a package — describing what it owns,
// what it depends on, and what constraints it must respect.
type AwarenessContract struct {
	// Kubernetes-style header (optional, ignored by validator).
	APIVersion string `yaml:"apiVersion"`
	KindHeader string `yaml:"kind"` // "AwarenessContract" — not to be confused with package_kind

	// Package identity.
	Service     string `yaml:"service"`
	Package     string `yaml:"package"`
	PackageKind string `yaml:"package_kind"`
	Summary     string `yaml:"summary"`

	// State ownership.
	Owns ContractOwns `yaml:"owns"`

	// State this package reads.
	Reads ContractState `yaml:"reads"`

	// State this package writes (used for write-protection checks).
	Writes ContractState `yaml:"writes"`

	// Runtime dependencies with phase and required flag.
	DependsOn []ContractDependency `yaml:"depends_on"`

	// Event relationships.
	Emits      []string `yaml:"emits"`
	Subscribes []string `yaml:"subscribes"`

	// Declared invariants this package is responsible for enforcing.
	Invariants []string `yaml:"invariants"`

	// Fixes this package explicitly disallows (self-imposed and declared).
	ForbiddenFixes []string `yaml:"forbidden_fixes"`

	// Conditions under which the package can safely degrade.
	SafeDegradedModes []string `yaml:"safe_degraded_modes"`

	// Workflow IDs this package may use for remediation.
	RemediationWorkflows []string `yaml:"remediation_workflows"`

	// Tests that must pass before this package is promoted.
	RequiredTests []string `yaml:"required_tests"`

	// RBAC permissions this package requires.
	RequiredPermissions []string `yaml:"required_permissions"`

	// Admission overrides (package-level).
	Admission struct {
		Strict                    bool `yaml:"strict"`
		AllowUnknownDependencies  bool `yaml:"allow_unknown_dependencies"`
		AllowPrivilegedStateWrites bool `yaml:"allow_privileged_state_writes"`
	} `yaml:"admission"`

	// sourcePath is set by the loader — the directory this contract was found in.
	sourcePath string
}

// ContractOwns declares what cluster resources a package owns.
type ContractOwns struct {
	EtcdKeys        []string `yaml:"etcd_keys"`
	ScyllaTables    []string `yaml:"scylla_tables"`
	MinioBuckets    []string `yaml:"minio_buckets"`
	FilesystemPaths []string `yaml:"filesystem_paths"`
	EventTypes      []string `yaml:"event_types"`
	SystemdUnits    []string `yaml:"systemd_units"`
	DNSZones        []string `yaml:"dns_zones"`
}

// ContractState declares state a package reads or writes.
type ContractState struct {
	EtcdKeys        []string `yaml:"etcd_keys"`
	ScyllaTables    []string `yaml:"scylla_tables"`
	MinioBuckets    []string `yaml:"minio_buckets"`
	FilesystemPaths []string `yaml:"filesystem_paths"`
}

// ContractDependency is a typed service dependency with phase and required flag.
type ContractDependency struct {
	Service         string `yaml:"service"`
	Phase           string `yaml:"phase"`
	Required        bool   `yaml:"required"`
	Reason          string `yaml:"reason"`
	SafeEscapeHatch string `yaml:"safe_escape_hatch"`
}

// SourcePath returns the directory the contract was loaded from.
func (c *AwarenessContract) SourcePath() string { return c.sourcePath }

// NormalisedKind returns the package_kind in lower-case for comparison.
func (c *AwarenessContract) NormalisedKind() string {
	return strings.ToLower(c.PackageKind)
}

// AllWrittenEtcdKeys returns the union of owns.etcd_keys and writes.etcd_keys.
func (c *AwarenessContract) AllWrittenEtcdKeys() []string {
	seen := make(map[string]bool)
	var out []string
	for _, k := range c.Owns.EtcdKeys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, k := range c.Writes.EtcdKeys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// LoadAwarenessContract searches for awareness.yaml starting from packagePath.
// Search order:
//  1. <packagePath>/awareness.yaml
//  2. packages/metadata/<package-name>/awareness.yaml (if package.json gives the name)
//
// Returns (nil, nil) when no awareness.yaml is found at either location.
func LoadAwarenessContract(packagePath string) (*AwarenessContract, error) {
	candidates := []string{
		filepath.Join(packagePath, "awareness.yaml"),
	}

	// Derive package name from package.json if present.
	if data, err := os.ReadFile(filepath.Join(packagePath, "package.json")); err == nil {
		var m packageManifest
		if json.Unmarshal(data, &m) == nil && m.Name != "" {
			candidates = append(candidates,
				filepath.Join(packagePath, "packages", "metadata", m.Name, "awareness.yaml"),
			)
		}
	}

	for _, p := range candidates {
		c, err := tryLoadFile(p)
		if err != nil {
			return nil, err
		}
		if c != nil {
			c.sourcePath = packagePath
			return c, nil
		}
	}

	return nil, nil
}

// LoadAwarenessContractFromFile loads the awareness.yaml at the given path directly.
func LoadAwarenessContractFromFile(path string) (*AwarenessContract, error) {
	c, err := tryLoadFile(path)
	if err != nil {
		return nil, err
	}
	if c != nil {
		c.sourcePath = filepath.Dir(path)
	}
	return c, nil
}

func tryLoadFile(path string) (*AwarenessContract, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("awareness_contract: read %s: %w", path, err)
	}
	var c AwarenessContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("awareness_contract: parse %s: %w", path, err)
	}
	return &c, nil
}

// ContractGraphPreview returns the nodes and edges a contract would add to the graph
// without writing anything. It is safe to call repeatedly — no side effects.
func ContractGraphPreview(c *AwarenessContract) ([]graph.Node, []graph.Edge) {
	var nodes []graph.Node
	var edges []graph.Edge

	addNode := func(n graph.Node) { nodes = append(nodes, n) }
	addEdge := func(e graph.Edge) { edges = append(edges, e) }

	pkgID := "package:" + c.Package
	svcID := "service:" + c.Service

	// Package node.
	addNode(graph.Node{
		ID:      pkgID,
		Type:    graph.NodeTypePackage,
		Name:    c.Package,
		Summary: c.Summary,
		Metadata: map[string]any{
			"kind":    c.PackageKind,
			"service": c.Service,
		},
	})

	// Service node.
	if c.Service != "" {
		addNode(graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: c.Service})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeOwns, Dst: svcID})
	}

	addOwnedResource := func(nodeType, prefix, name string) {
		nid := prefix + name
		addNode(graph.Node{ID: nid, Type: nodeType, Name: name})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeOwns, Dst: nid})
	}

	for _, k := range c.Owns.EtcdKeys {
		addOwnedResource(graph.NodeTypeEtcdKey, "etcd_key:", k)
	}
	for _, t := range c.Owns.ScyllaTables {
		addOwnedResource(graph.NodeTypeScyllaTable, "scylla_table:", t)
	}
	for _, b := range c.Owns.MinioBuckets {
		addOwnedResource(graph.NodeTypeMinioBucket, "minio_bucket:", b)
	}
	for _, p := range c.Owns.FilesystemPaths {
		addOwnedResource(graph.NodeTypeRuntimeState, "runtime_state:fs:", p)
	}
	for _, u := range c.Owns.SystemdUnits {
		addOwnedResource(graph.NodeTypeSystemdUnit, "systemd_unit:", u)
	}
	for _, ev := range c.Owns.EventTypes {
		addOwnedResource(graph.NodeTypeEventType, "event_type:", ev)
	}

	// Emits / subscribes.
	for _, ev := range c.Emits {
		nid := "event_type:" + ev
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeEventType, Name: ev})
		addEdge(graph.Edge{Src: svcID, Kind: graph.EdgeEmits, Dst: nid})
	}
	for _, ev := range c.Subscribes {
		nid := "event_type:" + ev
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeEventType, Name: ev})
		addEdge(graph.Edge{Src: svcID, Kind: graph.EdgeSubscribes, Dst: nid})
	}

	// Dependency edges — from service node, not package node.
	for _, dep := range c.DependsOn {
		dstID := "service:" + dep.Service
		addNode(graph.Node{ID: dstID, Type: graph.NodeTypeGlobularService, Name: dep.Service})
		addEdge(graph.Edge{
			Src:      svcID,
			Kind:     graph.EdgeDependsOn,
			Dst:      dstID,
			Phase:    dep.Phase,
			Required: dep.Required,
		})
	}

	// Invariants enforced by this package.
	for _, inv := range c.Invariants {
		nid := "invariant:" + inv
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeInvariant, Name: inv})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeEnforces, Dst: nid})
	}

	// Self-declared forbidden fixes.
	for _, fix := range c.ForbiddenFixes {
		nid := "forbidden_fix:" + fix
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeForbiddenFix, Name: fix})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeForbids, Dst: nid})
	}

	// Remediation workflows.
	for _, wf := range c.RemediationWorkflows {
		nid := "workflow:" + wf
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeWorkflow, Name: wf})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeRemediatedBy, Dst: nid})
	}

	// Required tests.
	for _, t := range c.RequiredTests {
		nid := "test:" + t
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeTest, Name: t})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeTestedBy, Dst: nid})
	}

	// Written state (creates write edges — used for protection check).
	for _, k := range c.Writes.EtcdKeys {
		nid := "etcd_key:" + k
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeEtcdKey, Name: k})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeWrites, Dst: nid})
	}
	for _, k := range c.Reads.EtcdKeys {
		nid := "etcd_key:" + k
		addNode(graph.Node{ID: nid, Type: graph.NodeTypeEtcdKey, Name: k})
		addEdge(graph.Edge{Src: pkgID, Kind: graph.EdgeReads, Dst: nid})
	}

	return nodes, edges
}

// AddContractToGraph commits the contract's nodes and edges to g.
func AddContractToGraph(ctx context.Context, g *graph.Graph, c *AwarenessContract) error {
	nodes, edges := ContractGraphPreview(c)
	for _, n := range nodes {
		if err := g.AddNode(ctx, n); err != nil {
			return fmt.Errorf("AddContractToGraph node %s: %w", n.ID, err)
		}
	}
	for _, e := range edges {
		if err := g.AddEdge(ctx, e); err != nil {
			return fmt.Errorf("AddContractToGraph edge %s->%s: %w", e.Src, e.Dst, err)
		}
	}
	return nil
}

// ExtractAwarenessContracts walks a packages metadata directory and loads
// all awareness.yaml files into g.
// metadataDir should be e.g. packages/metadata/.
func ExtractAwarenessContracts(ctx context.Context, g *graph.Graph, metadataDir string) error {
	entries, err := os.ReadDir(metadataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ExtractAwarenessContracts: readdir %s: %w", metadataDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgDir := filepath.Join(metadataDir, e.Name())
		contract, err := LoadAwarenessContract(pkgDir)
		if err != nil {
			return fmt.Errorf("ExtractAwarenessContracts: %s: %w", e.Name(), err)
		}
		if contract == nil {
			continue
		}
		if err := AddContractToGraph(ctx, g, contract); err != nil {
			return err
		}
	}
	return nil
}

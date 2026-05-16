// Package project defines how Awareness discovers and loads project identity.
//
// Each repository that uses Awareness carries a .awareness.yaml file at its
// root. This package resolves that file into a ProjectProfile — the single
// struct that all Awareness commands and MCP tools consume.
//
// No Globular runtime packages are imported here. The GlobularAdapter lives
// in the services repository. This package is runtime-agnostic.
package project

import "time"

// ProjectKind classifies the type of project for awareness reasoning.
type ProjectKind string

const (
	ProjectKindUnknown             ProjectKind = "unknown"
	ProjectKindDistributedPlatform ProjectKind = "distributed-platform"
	ProjectKindApplication         ProjectKind = "application"
	ProjectKindLibrary             ProjectKind = "library"
)

// RuntimeConfig describes whether a runtime adapter is active.
type RuntimeConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Adapter string `yaml:"adapter" json:"adapter"`
}

// LanguageConfig describes which languages are present in the project.
type LanguageConfig struct {
	Go         bool `yaml:"go" json:"go"`
	TypeScript bool `yaml:"typescript" json:"typescript"`
	Proto      bool `yaml:"proto" json:"proto"`
	YAML       bool `yaml:"yaml" json:"yaml"`
	Markdown   bool `yaml:"markdown" json:"markdown"`
}

// GraphConfig holds graph cache configuration.
type GraphConfig struct {
	CacheDir     string        `yaml:"cache_dir" json:"cache_dir"`
	FreshnessTTL time.Duration `yaml:"-" json:"freshness_ttl"`
	FreshnessTTLRaw string    `yaml:"freshness_ttl" json:"-"`
	InvalidateOn []string      `yaml:"invalidate_on" json:"invalidate_on"`
}

// MCPConfig controls how the MCP server resolves project context.
type MCPConfig struct {
	ProjectAware       bool   `yaml:"project_aware" json:"project_aware"`
	ResolveProfileFrom string `yaml:"resolve_profile_from" json:"resolve_profile_from"`
}

// AwarenessPaths holds resolved absolute paths to the project's Awareness
// knowledge files. All paths are absolute after ResolveProfile returns.
type AwarenessPaths struct {
	Root           string   `yaml:"root" json:"root"`
	Invariants     []string `yaml:"invariants" json:"invariants"`
	FailureModes   []string `yaml:"failure_modes" json:"failure_modes"`
	ForbiddenFixes []string `yaml:"forbidden_fixes" json:"forbidden_fixes"`
	CausalRules    []string `yaml:"causal_rules" json:"causal_rules"`
	ContextAliases []string `yaml:"context_aliases" json:"context_aliases"`
	DecisionsDir   string   `yaml:"decisions_dir" json:"decisions_dir"`
	ProposalsDir   string   `yaml:"proposals_dir" json:"proposals_dir"`
}

// ProjectProfile is the fully resolved project identity.
// All path fields are absolute. Obtained via ResolveProfile — do not
// construct directly.
type ProjectProfile struct {
	// Name is the project identifier from .awareness.yaml.
	Name string `yaml:"name" json:"name"`
	// Kind classifies the project type.
	Kind ProjectKind `yaml:"kind" json:"kind"`
	// Root is the absolute path to the project root (directory containing .awareness.yaml).
	Root string `yaml:"root" json:"root"`
	// ConfigPath is the absolute path to the .awareness.yaml that produced this profile.
	ConfigPath string `yaml:"-" json:"config_path"`
	// RootMarkers are file/directory names that signal the project root
	// (used as supplementary signals alongside .git).
	RootMarkers []string `yaml:"root_markers" json:"root_markers"`
	// SourceRoots are the directories (absolute) that contain project source code.
	SourceRoots []string `yaml:"source_roots" json:"source_roots"`

	Languages LanguageConfig `yaml:"languages" json:"languages"`
	Awareness AwarenessPaths `yaml:"awareness" json:"awareness"`
	Runtime   RuntimeConfig  `yaml:"runtime" json:"runtime"`
	Graph     GraphConfig    `yaml:"graph" json:"graph"`
	MCP       MCPConfig      `yaml:"mcp" json:"mcp"`
}

// IsRuntimeEnabled returns true when a non-null runtime adapter is configured.
func (p *ProjectProfile) IsRuntimeEnabled() bool {
	return p.Runtime.Enabled && p.Runtime.Adapter != "" && p.Runtime.Adapter != "null"
}

// AdapterName returns the runtime adapter name, defaulting to "null".
func (p *ProjectProfile) AdapterName() string {
	if p.Runtime.Adapter == "" {
		return "null"
	}
	return p.Runtime.Adapter
}

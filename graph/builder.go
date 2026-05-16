package graph

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ─── Build options ────────────────────────────────────────────────────────────

// BuildOptions controls which graph components are built.
type BuildOptions struct {
	// IncludeSourceFiles walks SourceRoots and adds source_file nodes.
	// Disabled by default to keep the graph small on first use.
	IncludeSourceFiles bool

	// MaxSourceFiles caps the number of source_file nodes to avoid
	// overwhelming small graph.json files. Default 500.
	MaxSourceFiles int
}

// BuildResult is returned by Build.
type BuildResult struct {
	Graph         *GraphFile
	NodeCount     int
	EdgeCount     int
	InvariantCount int
	FailureModeCount int
	ForbiddenFixCount int
	SourceFileCount int
}

// ─── Knowledge YAML shapes (private — only for reading) ──────────────────────

type yamlInvariant struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Severity    string   `yaml:"severity"`
	Tags        []string `yaml:"tags"`
}

type yamlFailureMode struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Symptoms    []string `yaml:"symptoms"`
	WrongFixes  []string `yaml:"wrong_fixes"`
	Severity    string   `yaml:"severity"`
	Tags        []string `yaml:"tags"`
}

type yamlForbiddenFix struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	AppliesWhen string   `yaml:"applies_when"`
	Tags        []string `yaml:"tags"`
}

type yamlInvariantsFile struct {
	Invariants []yamlInvariant `yaml:"invariants"`
}

type yamlFailureModesFile struct {
	FailureModes []yamlFailureMode `yaml:"failure_modes"`
}

type yamlForbiddenFixesFile struct {
	ForbiddenFixes []yamlForbiddenFix `yaml:"forbidden_fixes"`
}

// ─── Build ────────────────────────────────────────────────────────────────────

// BuildInput describes the project data needed to build a graph.
// This lets callers pass pre-loaded data or supply resolved paths
// without depending on the project package directly.
type BuildInput struct {
	ProjectName    string
	ProjectKind    string
	ProjectRoot    string
	InvariantPaths []string
	FailureModePaths []string
	ForbiddenFixPaths []string
	SourceRoots    []string
	SourceExtensions []string
}

// Build constructs an in-memory awareness knowledge graph from the provided
// project data. The resulting GraphFile can be serialised to graph.json.
//
// Build is pure: it reads YAML files but does not write anything.
func Build(input BuildInput, opts BuildOptions) (*BuildResult, error) {
	if opts.MaxSourceFiles <= 0 {
		opts.MaxSourceFiles = 500
	}

	result := &BuildResult{}
	gf := &GraphFile{
		SchemaVersion: CurrentGraphSchemaVersion,
		Project:       input.ProjectName,
		GeneratedAt:   time.Now().UTC(),
	}

	var nodes []Node
	var edges []Edge

	// Project node.
	projectID := "project:" + sanitizeID(input.ProjectName)
	nodes = append(nodes, Node{
		ID:   projectID,
		Kind: "project",
		Label: input.ProjectName,
		Properties: map[string]string{
			"kind": string(input.ProjectKind),
			"root": input.ProjectRoot,
		},
	})

	// ── Load invariants ──────────────────────────────────────────────────────
	var invariants []yamlInvariant
	for _, p := range input.InvariantPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f yamlInvariantsFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.Invariants {
			f.Invariants[i].Tags = append(f.Invariants[i].Tags, "_source:"+filepath.Base(p))
		}
		invariants = append(invariants, f.Invariants...)
	}

	for _, inv := range invariants {
		if inv.ID == "" {
			continue
		}
		nodeID := "invariant:" + inv.ID
		props := map[string]string{"severity": inv.Severity}
		if inv.Title != "" {
			props["title"] = inv.Title
		}
		if len(inv.Tags) > 0 {
			props["tags"] = strings.Join(inv.Tags, ",")
		}
		nodes = append(nodes, Node{
			ID:         nodeID,
			Kind:       "invariant",
			Label:      coalesce(inv.Title, inv.ID),
			Properties: props,
		})
		edges = append(edges, Edge{
			From: projectID,
			To:   nodeID,
			Kind: "defines",
		})
		result.InvariantCount++
	}

	// ── Load failure modes ───────────────────────────────────────────────────
	var failureModes []yamlFailureMode
	for _, p := range input.FailureModePaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f yamlFailureModesFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.FailureModes {
			f.FailureModes[i].Tags = append(f.FailureModes[i].Tags, "_source:"+filepath.Base(p))
		}
		failureModes = append(failureModes, f.FailureModes...)
	}

	for _, fm := range failureModes {
		if fm.ID == "" {
			continue
		}
		nodeID := "failure_mode:" + fm.ID
		props := map[string]string{"severity": fm.Severity}
		if fm.Title != "" {
			props["title"] = fm.Title
		}
		if len(fm.Tags) > 0 {
			props["tags"] = strings.Join(fm.Tags, ",")
		}
		if len(fm.Symptoms) > 0 {
			props["symptoms"] = strings.Join(fm.Symptoms, "|")
		}
		nodes = append(nodes, Node{
			ID:         nodeID,
			Kind:       "failure_mode",
			Label:      coalesce(fm.Title, fm.ID),
			Properties: props,
		})
		edges = append(edges, Edge{
			From: projectID,
			To:   nodeID,
			Kind: "knows_failure_mode",
		})
		result.FailureModeCount++
	}

	// ── Load forbidden fixes ─────────────────────────────────────────────────
	var forbiddenFixes []yamlForbiddenFix
	for _, p := range input.ForbiddenFixPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f yamlForbiddenFixesFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.ForbiddenFixes {
			f.ForbiddenFixes[i].Tags = append(f.ForbiddenFixes[i].Tags, "_source:"+filepath.Base(p))
		}
		forbiddenFixes = append(forbiddenFixes, f.ForbiddenFixes...)
	}

	for _, ff := range forbiddenFixes {
		if ff.ID == "" {
			continue
		}
		nodeID := "forbidden_fix:" + ff.ID
		props := map[string]string{}
		if ff.Title != "" {
			props["title"] = ff.Title
		}
		if len(ff.Tags) > 0 {
			props["tags"] = strings.Join(ff.Tags, ",")
		}
		nodes = append(nodes, Node{
			ID:         nodeID,
			Kind:       "forbidden_fix",
			Label:      coalesce(ff.Title, ff.ID),
			Properties: props,
		})
		edges = append(edges, Edge{
			From: projectID,
			To:   nodeID,
			Kind: "forbids_fix",
		})
		result.ForbiddenFixCount++
	}

	// ── Cross-links: failure_mode → related_to → invariant ──────────────────
	for _, fm := range failureModes {
		if fm.ID == "" {
			continue
		}
		fmBlob := strings.ToLower(fm.ID + " " + fm.Title + " " + fm.Description + " " + strings.Join(fm.Symptoms, " ") + " " + strings.Join(fm.Tags, " "))
		for _, inv := range invariants {
			if inv.ID == "" {
				continue
			}
			invTerms := idTerms(inv.ID)
			matched := false
			for _, t := range invTerms {
				if len(t) > 3 && strings.Contains(fmBlob, t) {
					matched = true
					break
				}
			}
			if matched {
				edges = append(edges, Edge{
					From: "failure_mode:" + fm.ID,
					To:   "invariant:" + inv.ID,
					Kind: "related_to",
				})
			}
		}
	}

	// ── Cross-links: forbidden_fix → protects → invariant ───────────────────
	for _, ff := range forbiddenFixes {
		if ff.ID == "" {
			continue
		}
		ffBlob := strings.ToLower(ff.ID + " " + ff.Title + " " + ff.Description + " " + strings.Join(ff.Tags, " "))
		for _, inv := range invariants {
			if inv.ID == "" {
				continue
			}
			invTerms := idTerms(inv.ID)
			matched := false
			for _, t := range invTerms {
				if len(t) > 3 && strings.Contains(ffBlob, t) {
					matched = true
					break
				}
			}
			if matched {
				edges = append(edges, Edge{
					From: "forbidden_fix:" + ff.ID,
					To:   "invariant:" + inv.ID,
					Kind: "protects",
				})
			}
		}
	}

	// ── Source file nodes (optional) ─────────────────────────────────────────
	if opts.IncludeSourceFiles && len(input.SourceRoots) > 0 {
		exts := input.SourceExtensions
		if len(exts) == 0 {
			exts = []string{".go", ".ts", ".tsx", ".js", ".py", ".java", ".rs", ".proto", ".yaml", ".yml"}
		}
		extSet := make(map[string]bool, len(exts))
		for _, e := range exts {
			extSet[e] = true
		}

		var sourceFiles []string
		for _, root := range input.SourceRoots {
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if extSet[filepath.Ext(path)] {
					rel, relErr := filepath.Rel(input.ProjectRoot, path)
					if relErr != nil {
						rel = path
					}
					sourceFiles = append(sourceFiles, rel)
				}
				if len(sourceFiles) >= opts.MaxSourceFiles {
					return filepath.SkipAll
				}
				return nil
			})
			if len(sourceFiles) >= opts.MaxSourceFiles {
				break
			}
		}

		for _, rel := range sourceFiles {
			nodeID := "source_file:" + rel
			nodes = append(nodes, Node{
				ID:    nodeID,
				Kind:  "source_file",
				Label: filepath.Base(rel),
				Properties: map[string]string{
					"path": rel,
					"ext":  filepath.Ext(rel),
				},
			})
			edges = append(edges, Edge{
				From: projectID,
				To:   nodeID,
				Kind: "contains",
			})
			result.SourceFileCount++

			// Keyword-match source files against invariants.
			fileBlob := strings.ToLower(rel)
			for _, inv := range invariants {
				if inv.ID == "" {
					continue
				}
				for _, t := range idTerms(inv.ID) {
					if len(t) > 4 && strings.Contains(fileBlob, t) {
						edges = append(edges, Edge{
							From: nodeID,
							To:   "invariant:" + inv.ID,
							Kind: "mentions",
						})
						break
					}
				}
			}
		}
	}

	gf.Nodes = nodes
	gf.Edges = edges
	result.Graph = gf
	result.NodeCount = len(nodes)
	result.EdgeCount = len(edges)
	return result, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// sanitizeID returns a graph-safe node ID component (lowercase, hyphens).
func sanitizeID(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// idTerms splits a dotted ID like "process.state.determinism" into its parts.
func idTerms(id string) []string {
	return strings.FieldsFunc(strings.ToLower(id), func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ':'
	})
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

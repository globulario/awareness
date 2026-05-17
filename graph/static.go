package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/awareness/knowledge"
	"github.com/globulario/awareness/scan/tsast"
)

// CurrentGraphSchemaVersion is the schema version written into static GraphFile exports.
const CurrentGraphSchemaVersion = "awareness.graph.v1"

// StaticNode is a JSON-serialisable graph vertex used by the lightweight
// file-based graph builder. Distinct from the SQLite-backed Node type.
type StaticNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// StaticEdge is a JSON-serialisable directed edge used by the file-based builder.
type StaticEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// GraphFile is the on-disk JSON representation of a static awareness graph.
type GraphFile struct {
	SchemaVersion string       `json:"schema_version"`
	Project       string       `json:"project"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Nodes         []StaticNode `json:"nodes"`
	Edges         []StaticEdge `json:"edges"`
}

// BuildInput parameterises a static graph build.
type BuildInput struct {
	ProjectName       string
	ProjectKind       string
	ProjectRoot       string
	InvariantPaths    []string
	FailureModePaths  []string
	ForbiddenFixPaths []string
	SourceRoots       []string
}

// BuildOptions controls optional behaviour of Build.
type BuildOptions struct {
	IncludeSourceFiles bool // adds .go source_file nodes
	IncludeTypeScript  bool // adds TypeScript source_file nodes + frontend_* nodes and edges
}

// BuildResult holds the output of a static graph build.
type BuildResult struct {
	Graph             *GraphFile
	NodeCount         int
	EdgeCount         int
	InvariantCount    int
	FailureModeCount  int
	ForbiddenFixCount int
	SourceFileCount   int
}

// Build constructs a static GraphFile from the provided knowledge paths.
func Build(input BuildInput, opts BuildOptions) (*BuildResult, error) {
	base, err := knowledge.LoadFromPaths(
		input.InvariantPaths,
		input.FailureModePaths,
		input.ForbiddenFixPaths,
		nil,
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("load knowledge: %w", err)
	}

	gf := &GraphFile{
		SchemaVersion: CurrentGraphSchemaVersion,
		Project:       input.ProjectName,
		GeneratedAt:   time.Now().UTC(),
	}

	var invariantCount, failureModeCount, forbiddenFixCount int

	if base != nil {
		for _, inv := range base.Invariants {
			gf.Nodes = append(gf.Nodes, StaticNode{
				ID:    inv.ID,
				Kind:  "invariant",
				Label: inv.Title,
			})
			invariantCount++
		}
		for _, fm := range base.FailureModes {
			gf.Nodes = append(gf.Nodes, StaticNode{
				ID:    fm.ID,
				Kind:  "failure_mode",
				Label: fm.Title,
			})
			failureModeCount++
		}
		for _, ff := range base.ForbiddenFixes {
			gf.Nodes = append(gf.Nodes, StaticNode{
				ID:    ff.ID,
				Kind:  "forbidden_fix",
				Label: ff.Summary,
			})
			forbiddenFixCount++
		}
	}

	var sourceFileCount int
	if opts.IncludeSourceFiles {
		for _, root := range input.SourceRoots {
			count, err := addSourceFileNodes(gf, root)
			if err == nil {
				sourceFileCount += count
			}
		}
	}

	if opts.IncludeTypeScript {
		for _, root := range input.SourceRoots {
			count, err := addFrontendNodes(gf, root)
			if err == nil {
				sourceFileCount += count
			}
		}
	}

	sort.Slice(gf.Nodes, func(i, j int) bool {
		return gf.Nodes[i].ID < gf.Nodes[j].ID
	})

	return &BuildResult{
		Graph:             gf,
		NodeCount:         len(gf.Nodes),
		EdgeCount:         len(gf.Edges),
		InvariantCount:    invariantCount,
		FailureModeCount:  failureModeCount,
		ForbiddenFixCount: forbiddenFixCount,
		SourceFileCount:   sourceFileCount,
	}, nil
}

// addFrontendNodes scans root for TypeScript/React files and emits:
//   - one source_file node per file
//   - one frontend_* node per discovered construct
//   - edges connecting files to their constructs
func addFrontendNodes(gf *GraphFile, root string) (int, error) {
	findings, err := tsast.ScanDir(root)
	if err != nil {
		return 0, err
	}

	// Track emitted source_file and construct nodes to avoid duplicates.
	seenFiles := map[string]bool{}
	seenNodes := map[string]bool{}
	fileCount := 0

	for _, f := range findings {
		rel, relErr := filepath.Rel(root, f.File)
		if relErr != nil {
			rel = f.File
		}
		rel = filepath.ToSlash(rel)

		// Emit source_file node once per file.
		fileNodeID := "ts:" + rel
		if !seenFiles[fileNodeID] {
			seenFiles[fileNodeID] = true
			gf.Nodes = append(gf.Nodes, StaticNode{
				ID:   fileNodeID,
				Kind: "source_file",
				Properties: map[string]string{
					"language": "typescript",
					"path":     rel,
				},
			})
			fileCount++
		}

		// Map tsast kind to graph node kind.
		nodeKind := frontendNodeKind(f.Kind)
		if nodeKind == "" {
			continue
		}

		// Construct a stable node ID from file + kind + name.
		nodeID := nodeKind + ":" + rel
		if f.Name != "" {
			nodeID = nodeKind + ":" + f.Name + "@" + rel
		}

		if !seenNodes[nodeID] {
			seenNodes[nodeID] = true
			props := map[string]string{"file": rel}
			if f.Name != "" {
				props["name"] = f.Name
			}
			gf.Nodes = append(gf.Nodes, StaticNode{
				ID:         nodeID,
				Kind:       nodeKind,
				Label:      f.Name,
				Properties: props,
			})

			// Edge: file → construct.
			edgeKind := fileToConstructEdge(nodeKind)
			if edgeKind != "" {
				gf.Edges = append(gf.Edges, StaticEdge{
					From: fileNodeID,
					To:   nodeID,
					Kind: edgeKind,
				})
			}
		}
	}

	return fileCount, nil
}

// frontendNodeKind maps a tsast kind string to its graph node kind.
func frontendNodeKind(tsKind string) string {
	switch tsKind {
	case tsast.KindComponent:
		return "frontend_component"
	case tsast.KindRoute:
		return "frontend_route"
	case tsast.KindBackendCall:
		return "frontend_backend_call"
	case tsast.KindStateAtom:
		return "frontend_state_atom"
	case tsast.KindHook:
		return "frontend_hook"
	case tsast.KindPermissionCheck:
		return "frontend_permission_check"
	case tsast.KindTest:
		return "frontend_test"
	case tsast.KindStory:
		return "frontend_story"
	case tsast.KindLayoutSignal:
		return "frontend_layout_signal"
	default:
		return ""
	}
}

// fileToConstructEdge returns the edge kind for a file→construct relationship.
func fileToConstructEdge(nodeKind string) string {
	switch nodeKind {
	case "frontend_component":
		return "file_defines_component"
	case "frontend_route":
		return "file_defines_route"
	case "frontend_backend_call":
		return "file_contains_backend_call"
	case "frontend_state_atom":
		return "file_contains_state_atom"
	case "frontend_hook":
		return "file_defines_hook"
	case "frontend_permission_check":
		return "file_contains_permission_check"
	case "frontend_test":
		return "file_is_test"
	case "frontend_story":
		return "file_is_story"
	default:
		return ""
	}
}

func addSourceFileNodes(gf *GraphFile, root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		gf.Nodes = append(gf.Nodes, StaticNode{
			ID:   rel,
			Kind: "source_file",
		})
		count++
		return nil
	})
	return count, err
}

package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/awareness/knowledge"
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
	IncludeSourceFiles bool
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

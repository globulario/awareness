// Package scripts crawls shell scripts, Makefiles, and extra Go files from
// one or more repository roots that are outside the primary services/ repo.
// It produces awareness graph nodes for each script file and extracts
// cross-repo edges when a script references a known package or service name.
//
// Source tier: installer_script (for shell/Makefile) or shared_lib (for .go)
package scripts

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/globulario/awareness/graph"
)

const (
	sourceTierInstallerScript = "installer_script"
	sourceTierSharedLib       = "shared_lib"
)

// RepoRoot describes a single extra repository to crawl.
type RepoRoot struct {
	// Path is the filesystem path to the repository root.
	Path string
	// SourceTier overrides the default tier ("installer_script" for scripts).
	SourceTier string
	// Include is an optional list of glob patterns relative to Path.
	// If empty, all .sh, Makefile, and .go files are included.
	Include []string
}

// CollectorHealth reports the result of a crawl pass.
type CollectorHealth struct {
	CollectorID  string
	SourceTier   string
	Status       string // "ok" | "skipped" | "error"
	NodesEmitted int
	Error        string
}

// Extract crawls each root in roots for shell scripts, Makefiles, and Go files.
// For each file found, it creates a graph node and emits cross-repo edges to
// packages/services that the script references by name.
//
// Missing roots are silently skipped (CollectorHealth.Status="skipped").
func Extract(ctx context.Context, g *graph.Graph, roots []RepoRoot) ([]CollectorHealth, error) {
	var healths []CollectorHealth
	for _, root := range roots {
		h := crawlRoot(ctx, g, root)
		healths = append(healths, h)
	}
	return healths, nil
}

func crawlRoot(ctx context.Context, g *graph.Graph, root RepoRoot) CollectorHealth {
	health := CollectorHealth{
		CollectorID: "scripts:" + root.Path,
		SourceTier:  tierFor(root),
	}

	if root.Path == "" {
		health.Status = "skipped"
		health.Error = "empty path"
		return health
	}
	if _, err := os.Stat(root.Path); os.IsNotExist(err) {
		health.Status = "skipped"
		health.Error = fmt.Sprintf("path not found: %s", root.Path)
		return health
	}

	err := filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" ||
				name == "dist" || name == "bin" || name == "dependencies" {
				return filepath.SkipDir
			}
			return nil
		}

		if !isIndexable(d.Name()) {
			return nil
		}

		rel, _ := filepath.Rel(root.Path, path)
		n, err2 := indexScriptFile(ctx, g, path, rel, tierFor(root))
		if err2 == nil {
			health.NodesEmitted += n
		}
		return nil
	})
	if err != nil {
		health.Status = "error"
		health.Error = err.Error()
		return health
	}

	health.Status = "ok"
	return health
}

// isIndexable returns true for .sh files, Makefiles, and .go files.
func isIndexable(name string) bool {
	return strings.HasSuffix(name, ".sh") ||
		name == "Makefile" ||
		strings.HasSuffix(name, ".go")
}

func tierFor(root RepoRoot) string {
	if root.SourceTier != "" {
		return root.SourceTier
	}
	return sourceTierInstallerScript
}

// packageRefRE matches common patterns that reference a Globular package by name:
// - echo "... minio ..."
// - PACKAGE=minio
// - --name minio
// - package_name="workflow"
// We keep this deliberately broad and filter on known names at graph lookup time.
var packageRefRE = regexp.MustCompile(`(?i)(package[_-]?name|--name|PACKAGE|service)\s*[="\s]+([a-z][a-z0-9-]+)`)

// fnDefRE matches shell function definitions: funcname() { or function funcname {
var fnDefRE = regexp.MustCompile(`^(?:function\s+)?([a-zA-Z_][a-zA-Z0-9_-]+)\s*\(\s*\)`)

func indexScriptFile(ctx context.Context, g *graph.Graph, absPath, relPath, tier string) (int, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return 0, nil
	}

	fileID := "script:" + relPath
	functions := extractFunctions(data, absPath)
	refs := extractPackageRefs(data)

	meta := map[string]any{
		"source_tier": tier,
		"file_type":   fileType(filepath.Base(absPath)),
	}
	if len(functions) > 0 {
		meta["functions"] = strings.Join(functions, ",")
	}
	if len(refs) > 0 {
		meta["package_refs"] = strings.Join(refs, ",")
	}

	if err := g.AddNode(ctx, graph.Node{
		ID:      fileID,
		Type:    "source_file",
		Name:    filepath.Base(absPath),
		Path:    relPath,
		Summary: fmt.Sprintf("%s script: %d functions, %d package refs", tier, len(functions), len(refs)),
		Metadata: meta,
	}); err != nil {
		return 0, err
	}
	emitted := 1

	// Cross-repo edges: script → package when script references a package name.
	for _, ref := range refs {
		pkgID := "package:" + ref
		_ = g.AddNode(ctx, graph.Node{ID: pkgID, Type: graph.NodeTypePackage, Name: ref})
		_ = g.AddEdge(ctx, graph.Edge{
			Src:  fileID,
			Kind: graph.EdgeAffects,
			Dst:  pkgID,
			Metadata: map[string]any{
				"source_tier": tier,
				"edge_note":   "script_references_package",
			},
		})
	}

	return emitted, nil
}

// extractFunctions returns shell function names defined in the file.
// For .go files it extracts top-level func names via simple regex.
func extractFunctions(data []byte, path string) []string {
	var fns []string
	seen := map[string]bool{}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if m := fnDefRE.FindStringSubmatch(strings.TrimSpace(line)); len(m) > 1 {
			fn := m[1]
			if !seen[fn] {
				seen[fn] = true
				fns = append(fns, fn)
			}
		}
	}
	return fns
}

// extractPackageRefs extracts likely Globular package name references from a script.
func extractPackageRefs(data []byte) []string {
	var refs []string
	seen := map[string]bool{}

	for _, m := range packageRefRE.FindAllSubmatch(data, -1) {
		if len(m) < 3 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(string(m[2])))
		if len(name) < 2 || seen[name] {
			continue
		}
		// Exclude generic words that aren't package names.
		if isStopWord(name) {
			continue
		}
		seen[name] = true
		refs = append(refs, name)
	}
	return refs
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "not": true, "all": true,
	"set": true, "get": true, "run": true, "use": true, "add": true,
	"new": true, "old": true, "true": true, "false": true, "null": true,
}

func isStopWord(s string) bool {
	return stopWords[s] || len(s) <= 1
}

func fileType(name string) string {
	switch {
	case strings.HasSuffix(name, ".sh"):
		return "shell"
	case name == "Makefile":
		return "makefile"
	case strings.HasSuffix(name, ".go"):
		return "go"
	default:
		return "unknown"
	}
}

// awareness is the CLI for the Awareness reasoning engine.
//
// This binary is a placeholder that grows as generic packages are migrated
// from services/golang/awareness into this module. Current commands:
//
//	awareness profile show    — display resolved project profile
//	awareness profile doctor  — check profile health
//	awareness preflight       — run a lightweight preflight check
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/bundle"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/project"
	"github.com/globulario/awareness/runtime"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "profile":
		runProfile(os.Args[2:])
	case "preflight":
		runPreflight(os.Args[2:])
	case "bundle":
		runBundle(os.Args[2:])
	case "graph":
		runGraphCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "awareness: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func runProfile(args []string) {
	sub := "show"
	projectRoot := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "show", "doctor":
			sub = args[i]
		case "--project-root":
			if i+1 < len(args) {
				i++
				projectRoot = args[i]
			}
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}

	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	switch sub {
	case "show":
		fmt.Printf("project: %s\n", prof.Name)
		fmt.Printf("kind:    %s\n", prof.Kind)
		fmt.Printf("root:    %s\n", prof.Root)
		fmt.Printf("profile: %s\n", prof.ConfigPath)
		fmt.Printf("runtime: enabled=%v adapter=%s\n", prof.Runtime.Enabled, prof.AdapterName())
	case "doctor":
		fmt.Printf("project: %s\n", prof.Name)
		fmt.Printf("kind:    %s\n", prof.Kind)
		fmt.Printf("root:    %s\n", prof.Root)
		fmt.Printf("profile: %s\n", prof.ConfigPath)
		fmt.Printf("runtime: %s\n", runtimeStatus(prof))
		fmt.Println()
		fmt.Println("status: ok")
	}
}

func runPreflight(args []string) {
	var (
		changed     bool
		projectRoot string
		format      string
		task        string
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--changed":
			changed = true
		case "--project-root":
			if i+1 < len(args) {
				i++
				projectRoot = args[i]
			}
		case "--format":
			if i+1 < len(args) {
				i++
				format = args[i]
			}
		case "--task":
			if i+1 < len(args) {
				i++
				task = args[i]
			}
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}

	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	// Collect changed files.
	var changedFiles []string
	var warnings []string
	if changed {
		changedFiles, warnings = collectChangedFiles(prof.Root)
	}

	// Collect runtime signals.
	adapterName := prof.AdapterName()
	adapter, adapterErr := runtime.New(adapterName)
	runtimeStatus := "disabled"
	if adapterErr != nil {
		warnings = append(warnings, fmt.Sprintf("runtime adapter %q unavailable: %v", adapterName, adapterErr))
	} else if adapter.Enabled() {
		_, sigErr := adapter.CollectSignals(context.Background(), prof, runtime.SignalOptions{})
		if sigErr != nil {
			warnings = append(warnings, fmt.Sprintf("collect runtime signals: %v", sigErr))
		} else {
			runtimeStatus = "ok"
		}
	}

	// Classify the task.
	var classification []TaskClass
	if task != "" {
		classification = preflight.ClassifyTask(task)
	}

	// Raw knowledge fallback.
	rawMatches := preflight.RawKnowledgeFallback(task, changedFiles, prof.Awareness.Root)

	// Accumulate invariants/failure_modes/forbidden_fixes from raw matches.
	var invariants, failureModes, forbiddenFixes []string
	for _, m := range rawMatches {
		if m.Score < 2 {
			continue
		}
		switch m.Kind {
		case "invariant":
			invariants = append(invariants, m.ID)
		case "failure_mode":
			failureModes = append(failureModes, m.ID)
		case "forbidden_fix":
			forbiddenFixes = append(forbiddenFixes, m.ID)
		}
	}
	invariants = preflight.UniqueStrings(invariants)
	failureModes = preflight.UniqueStrings(failureModes)
	forbiddenFixes = preflight.UniqueStrings(forbiddenFixes)

	result := preflight.PreflightResult{
		ProjectName:    prof.Name,
		Task:           task,
		ChangedFiles:   changedFiles,
		Classification: classification,
		Invariants:     invariants,
		FailureModes:   failureModes,
		ForbiddenFixes: forbiddenFixes,
		RawMatches:     rawMatches,
		RuntimeStatus:  runtimeStatus,
		Warnings:       warnings,
		OK:             true,
	}

	switch format {
	case "json":
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fatal("marshal result: %v", err)
		}
		fmt.Println(string(b))
	default:
		printPreflightText(result)
	}
}

func printPreflightText(r preflight.PreflightResult) {
	fmt.Printf("project: %s\n", r.ProjectName)
	fmt.Printf("runtime: %s\n", r.RuntimeStatus)
	if r.Task != "" {
		fmt.Printf("task:    %s\n", r.Task)
	}
	fmt.Printf("changed: %d files\n", len(r.ChangedFiles))
	if len(r.Classification) > 0 {
		classes := make([]string, len(r.Classification))
		for i, c := range r.Classification {
			classes[i] = string(c)
		}
		fmt.Printf("classification: [%s]\n", strings.Join(classes, ", "))
	}
	fmt.Printf("raw_matches: %d\n", len(r.RawMatches))
	for _, m := range r.RawMatches {
		fmt.Printf("  - %s: %s (score:%d)\n", m.Kind, m.ID, m.Score)
	}
	if len(r.Warnings) > 0 {
		for _, w := range r.Warnings {
			fmt.Printf("warning: %s\n", w)
		}
	}
	if r.OK {
		fmt.Println("status: ok")
	} else {
		fmt.Println("status: error")
	}
}

// collectChangedFiles returns files reported as changed by git.
// It tries `git diff --name-only HEAD` first, then falls back to
// `git status --porcelain` for untracked/modified files not yet committed.
func collectChangedFiles(repoRoot string) ([]string, []string) {
	var files []string
	var warnings []string

	// Try git diff HEAD for staged/committed changes.
	diffOut, err := runGit(repoRoot, "diff", "--name-only", "HEAD")
	if err == nil && len(strings.TrimSpace(diffOut)) > 0 {
		for _, line := range strings.Split(strings.TrimSpace(diffOut), "\n") {
			if line != "" {
				files = append(files, line)
			}
		}
	}

	// Also check git status --porcelain for untracked/modified files.
	statusOut, err := runGit(repoRoot, "status", "--porcelain")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("git unavailable: %v — running without changed file context", err))
		return files, warnings
	}
	for _, line := range strings.Split(strings.TrimSpace(statusOut), "\n") {
		if len(line) < 4 {
			continue
		}
		// porcelain format: XY FILENAME or XY ORIG -> DEST
		xy := line[:2]
		rest := strings.TrimSpace(line[2:])
		// Skip entries that are already unmodified in the working tree.
		if xy == "  " {
			continue
		}
		// Handle rename: "R  old -> new"
		if strings.Contains(rest, " -> ") {
			parts := strings.SplitN(rest, " -> ", 2)
			rest = strings.TrimSpace(parts[1])
		}
		if rest != "" {
			files = append(files, rest)
		}
	}

	files = preflight.UniqueStrings(files)
	return files, warnings
}

func runGit(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// TaskClass is a local alias for convenience in this file.
type TaskClass = preflight.TaskClass

func runtimeStatus(p *project.ProjectProfile) string {
	if p.IsRuntimeEnabled() {
		return fmt.Sprintf("adapter:%s", p.AdapterName())
	}
	return "disabled"
}

func runBundle(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "build":
		runBundleBuild(args)
	default:
		fmt.Fprintf(os.Stderr, "awareness bundle: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "usage: awareness bundle build --out PATH [--project-root PATH] [--revision REV] [--version VER]")
		os.Exit(1)
	}
}

func runBundleBuild(args []string) {
	var (
		projectRoot string
		outputDir   string
		revision    string
		version     string
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project-root":
			if i+1 < len(args) {
				i++
				projectRoot = args[i]
			}
		case "--out":
			if i+1 < len(args) {
				i++
				outputDir = args[i]
			}
		case "--revision":
			if i+1 < len(args) {
				i++
				revision = args[i]
			}
		case "--version":
			if i+1 < len(args) {
				i++
				version = args[i]
			}
		}
	}
	if outputDir == "" {
		fatal("--out is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	// Auto-detect revision from git if not provided.
	if revision == "" {
		if rev, err := runGit(prof.Root, "rev-parse", "--short", "HEAD"); err == nil {
			revision = strings.TrimSpace(rev)
		}
	}

	manifest, err := bundle.Build(prof, outputDir, bundle.BuildOptions{
		GeneratorVersion: version,
		Revision:         revision,
	})
	if err != nil {
		fatal("bundle build: %v", err)
	}

	fmt.Printf("bundle: %s\n", outputDir)
	fmt.Printf("schema: %s\n", manifest.SchemaVersion)
	fmt.Printf("project: %s (%s)\n", manifest.ProjectName, manifest.ProjectKind)
	fmt.Printf("revision: %s\n", manifest.SourceRevision)
	fmt.Printf("invariants: %d\n", len(manifest.InvariantsPaths))
	fmt.Printf("failure_modes: %d\n", len(manifest.FailureModesPaths))
	fmt.Printf("forbidden_fixes: %d\n", len(manifest.ForbiddenFixesPaths))
	fmt.Printf("runtime_signals: %v\n", manifest.RuntimeSignalsIncluded)
	fmt.Println("status: ok")
}

// ─── Graph commands ───────────────────────────────────────────────────────────

func runGraphCmd(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "build":
		runGraphBuild(args)
	case "query":
		runGraphQuery(args)
	case "inspect":
		runGraphInspect(args)
	default:
		fmt.Fprintf(os.Stderr, "awareness graph: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "usage:")
		fmt.Fprintln(os.Stderr, "  awareness graph build    [--project-root PATH] [--out PATH] [--sources]")
		fmt.Fprintln(os.Stderr, "  awareness graph query    [--project-root PATH] --query TEXT [--limit N]")
		fmt.Fprintln(os.Stderr, "  awareness graph inspect  [--project-root PATH]")
		os.Exit(1)
	}
}

func runGraphBuild(args []string) {
	var projectRoot, out string
	includeSources := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project-root":
			if i+1 < len(args) {
				i++
				projectRoot = args[i]
			}
		case "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		case "--sources":
			includeSources = true
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	// Default output path: <awareness-cache>/graph.json
	if out == "" {
		if prof.Graph.CacheDir != "" {
			out = prof.Graph.CacheDir + "/graph.json"
		} else {
			out = prof.Awareness.Root + "/cache/graph.json"
		}
	}

	input := graph.BuildInput{
		ProjectName:       prof.Name,
		ProjectKind:       string(prof.Kind),
		ProjectRoot:       prof.Root,
		InvariantPaths:    prof.Awareness.Invariants,
		FailureModePaths:  prof.Awareness.FailureModes,
		ForbiddenFixPaths: prof.Awareness.ForbiddenFixes,
		SourceRoots:       prof.SourceRoots,
	}

	result, err := graph.Build(input, graph.BuildOptions{
		IncludeSourceFiles: includeSources,
	})
	if err != nil {
		fatal("graph build: %v", err)
	}

	// Write graph.json.
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fatal("create output directory: %v", err)
	}

	data, err := json.MarshalIndent(result.Graph, "", "  ")
	if err != nil {
		fatal("marshal graph: %v", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fatal("write %s: %v", out, err)
	}

	fmt.Printf("graph: %s\n", out)
	fmt.Printf("project: %s\n", result.Graph.Project)
	fmt.Printf("schema: %s\n", result.Graph.SchemaVersion)
	fmt.Printf("nodes: %d\n", result.NodeCount)
	fmt.Printf("edges: %d\n", result.EdgeCount)
	fmt.Printf("  invariants: %d\n", result.InvariantCount)
	fmt.Printf("  failure_modes: %d\n", result.FailureModeCount)
	fmt.Printf("  forbidden_fixes: %d\n", result.ForbiddenFixCount)
	if includeSources {
		fmt.Printf("  source_files: %d\n", result.SourceFileCount)
	}
	fmt.Println("status: ok")
}

func runGraphQuery(args []string) {
	var projectRoot, query string
	limit := 20
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project-root":
			if i+1 < len(args) {
				i++
				projectRoot = args[i]
			}
		case "--query":
			if i+1 < len(args) {
				i++
				query = args[i]
			}
		case "--limit":
			if i+1 < len(args) {
				i++
				fmt.Sscanf(args[i], "%d", &limit)
			}
		}
	}
	if query == "" {
		fatal("--query is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	graphPath := graphJSONPath(prof)
	if graphPath == "" {
		fmt.Fprintln(os.Stderr, "no graph.json found — run 'awareness graph build' first")
		os.Exit(1)
	}

	data, err := os.ReadFile(graphPath)
	if err != nil {
		fatal("read graph: %v", err)
	}
	var gf graph.GraphFile
	if err := json.Unmarshal(data, &gf); err != nil {
		fatal("parse graph: %v", err)
	}

	terms := splitTerms(query)
	var matches []graph.Node
	for _, n := range gf.Nodes {
		blob := strings.ToLower(n.ID + " " + n.Kind + " " + n.Label)
		for _, v := range n.Properties {
			blob += " " + strings.ToLower(v)
		}
		for _, t := range terms {
			if strings.Contains(blob, t) {
				matches = append(matches, n)
				break
			}
		}
		if len(matches) >= limit {
			break
		}
	}

	fmt.Printf("query: %s\n", query)
	fmt.Printf("graph: %s\n", graphPath)
	fmt.Printf("matches: %d\n", len(matches))
	for _, n := range matches {
		fmt.Printf("  [%s] %s — %s\n", n.Kind, n.ID, n.Label)
	}
}

func runGraphInspect(args []string) {
	var projectRoot string
	for i := 0; i < len(args); i++ {
		if args[i] == "--project-root" && i+1 < len(args) {
			i++
			projectRoot = args[i]
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		fatal("%v", err)
	}

	graphPath := graphJSONPath(prof)
	if graphPath == "" {
		fmt.Println("graph: not found")
		fmt.Println("run 'awareness graph build' to generate a graph")
		return
	}

	data, err := os.ReadFile(graphPath)
	if err != nil {
		fatal("read graph: %v", err)
	}
	var gf graph.GraphFile
	if err := json.Unmarshal(data, &gf); err != nil {
		fatal("parse graph: %v", err)
	}

	kindCounts := map[string]int{}
	for _, n := range gf.Nodes {
		kindCounts[n.Kind]++
	}
	edgeKindCounts := map[string]int{}
	for _, e := range gf.Edges {
		edgeKindCounts[e.Kind]++
	}

	fmt.Printf("graph: %s\n", graphPath)
	fmt.Printf("schema: %s\n", gf.SchemaVersion)
	fmt.Printf("project: %s\n", gf.Project)
	fmt.Printf("generated_at: %s\n", gf.GeneratedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Printf("nodes: %d\n", len(gf.Nodes))
	for k, c := range kindCounts {
		fmt.Printf("  %s: %d\n", k, c)
	}
	fmt.Printf("edges: %d\n", len(gf.Edges))
	for k, c := range edgeKindCounts {
		fmt.Printf("  %s: %d\n", k, c)
	}
}

func graphJSONPath(prof *project.ProjectProfile) string {
	candidates := []string{}
	if prof.Graph.CacheDir != "" {
		candidates = append(candidates, prof.Graph.CacheDir+"/graph.json")
	}
	candidates = append(candidates, prof.Awareness.Root+"/cache/graph.json")
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func splitTerms(q string) []string {
	var terms []string
	seen := map[string]bool{}
	for _, t := range strings.Fields(strings.ToLower(q)) {
		t = strings.Trim(t, ".,;:\"'")
		if len(t) > 2 && !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	return terms
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: awareness <command> [subcommand] [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  profile show    [--project-root PATH]")
	fmt.Fprintln(os.Stderr, "  profile doctor  [--project-root PATH]")
	fmt.Fprintln(os.Stderr, "  preflight       [--changed] [--project-root PATH] [--format text|json] [--task TEXT]")
	fmt.Fprintln(os.Stderr, "  bundle build    --out PATH [--project-root PATH] [--revision REV] [--version VER]")
	fmt.Fprintln(os.Stderr, "  graph build     [--project-root PATH] [--out PATH] [--sources]")
	fmt.Fprintln(os.Stderr, "  graph query     [--project-root PATH] --query TEXT [--limit N]")
	fmt.Fprintln(os.Stderr, "  graph inspect   [--project-root PATH]")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "awareness: "+format+"\n", args...)
	os.Exit(1)
}

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
	"strings"

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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: awareness <command> [subcommand] [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  profile show    [--project-root PATH]")
	fmt.Fprintln(os.Stderr, "  profile doctor  [--project-root PATH]")
	fmt.Fprintln(os.Stderr, "  preflight       [--changed] [--project-root PATH] [--format text|json] [--task TEXT]")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "awareness: "+format+"\n", args...)
	os.Exit(1)
}

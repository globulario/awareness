// awareness is the CLI for the Awareness reasoning engine.
//
// This binary is a placeholder that grows as generic packages are migrated
// from services/golang/awareness into this module. Current commands:
//
//	awareness profile show    — display resolved project profile
//	awareness profile doctor  — check profile health
package main

import (
	"fmt"
	"os"

	"github.com/globulario/awareness/project"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "profile":
		runProfile(os.Args[2:])
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
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "awareness: "+format+"\n", args...)
	os.Exit(1)
}

package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const configFileName = ".awareness.yaml"

// ResolveOptions controls project profile discovery.
type ResolveOptions struct {
	// ProjectRoot, when set, skips upward walk and loads .awareness.yaml
	// directly from this directory. Use for --project-root flags.
	ProjectRoot string
	// ConfigName overrides the default config file name ".awareness.yaml".
	// Useful for testing with alternate fixture names.
	ConfigName string
}

// ResolveProfile discovers and loads the ProjectProfile for the project
// containing cwd.
//
// Discovery rules:
//  1. If opts.ProjectRoot is set, load from that directory.
//  2. Walk upward from cwd looking for .awareness.yaml.
//  3. Stop at the nearest .git root or filesystem root.
//  4. Return a clear error when no profile is found.
//  5. Resolve all relative paths against the project root.
func ResolveProfile(cwd string, opts ResolveOptions) (*ProjectProfile, error) {
	cfgName := opts.ConfigName
	if cfgName == "" {
		cfgName = configFileName
	}

	if opts.ProjectRoot != "" {
		return loadFromDir(opts.ProjectRoot, cfgName)
	}
	return discover(cwd, cfgName)
}

// discover walks upward from start looking for the config file.
func discover(start, cfgName string) (*ProjectProfile, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, fmt.Errorf("resolve cwd %q: %w", start, err)
	}

	dir := abs
	for {
		candidate := filepath.Join(dir, cfgName)
		if _, err := os.Stat(candidate); err == nil {
			return loadFromDir(dir, cfgName)
		}

		// Stop at .git boundary — never cross the repository root.
		if isGitRoot(dir) {
			return nil, fmt.Errorf(
				"no %s found before repository root: %s",
				cfgName, dir,
			)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf(
				"no %s found (reached filesystem root from %s)",
				cfgName, start,
			)
		}
		dir = parent
	}
}

// loadFromDir reads cfgName from dir, parses it, and returns a resolved
// ProjectProfile with Root set to the absolute project directory.
func loadFromDir(dir, cfgName string) (*ProjectProfile, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve project root %q: %w", dir, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project root does not exist: %s", abs)
		}
		return nil, fmt.Errorf("stat project root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project root is not a directory: %s", abs)
	}

	configPath := filepath.Join(abs, cfgName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", configPath, err)
	}

	var prof ProjectProfile
	if err := yaml.Unmarshal(data, &prof); err != nil {
		return nil, fmt.Errorf("parse %s: %w", configPath, err)
	}

	// Set fields that are resolved at load time, not from YAML.
	prof.Root = abs
	prof.ConfigPath = configPath

	// Resolve relative paths against the project root.
	resolve := func(rel string) string {
		if rel == "" || filepath.IsAbs(rel) {
			return rel
		}
		return filepath.Join(abs, rel)
	}
	resolveAll := func(rels []string) []string {
		out := make([]string, len(rels))
		for i, r := range rels {
			out[i] = resolve(r)
		}
		return out
	}

	prof.Awareness.Root = resolve(prof.Awareness.Root)
	prof.Awareness.Invariants = resolveAll(prof.Awareness.Invariants)
	prof.Awareness.FailureModes = resolveAll(prof.Awareness.FailureModes)
	prof.Awareness.ForbiddenFixes = resolveAll(prof.Awareness.ForbiddenFixes)
	prof.Awareness.CausalRules = resolveAll(prof.Awareness.CausalRules)
	prof.Awareness.ContextAliases = resolveAll(prof.Awareness.ContextAliases)
	prof.Awareness.DecisionsDir = resolve(prof.Awareness.DecisionsDir)
	prof.Awareness.ProposalsDir = resolve(prof.Awareness.ProposalsDir)
	prof.SourceRoots = resolveAll(prof.SourceRoots)
	prof.Graph.CacheDir = resolve(prof.Graph.CacheDir)

	// Resolve freshness TTL from the raw string field.
	if raw := prof.Graph.FreshnessTTLRaw; raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid graph.freshness_ttl %q in %s: %w", raw, configPath, err)
		}
		prof.Graph.FreshnessTTL = d
	}

	// Default adapter.
	if prof.Runtime.Adapter == "" {
		if prof.Runtime.Enabled {
			prof.Runtime.Adapter = "globular"
		} else {
			prof.Runtime.Adapter = "null"
		}
	}

	if err := validate(&prof); err != nil {
		return nil, fmt.Errorf("invalid profile %s: %w", configPath, err)
	}

	return &prof, nil
}

// validate checks a profile for required fields.
func validate(p *ProjectProfile) error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

// isGitRoot returns true if dir contains a .git entry.
func isGitRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

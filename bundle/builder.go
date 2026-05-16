package bundle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/globulario/awareness/project"
)

// BuildOptions configures a bundle build operation.
type BuildOptions struct {
	// GeneratorVersion is the version string injected into the manifest
	// (e.g. via ldflags at build time). Empty for dev builds.
	GeneratorVersion string
	// Revision is the VCS revision (git SHA / tag) at build time.
	Revision string
	// RuntimeSignalsIncluded must be true when the caller has written
	// runtime_signals.json into OutputDir before calling Build.
	RuntimeSignalsIncluded bool
}

// Build constructs a project Awareness bundle in outputDir.
//
// It copies the invariants, failure_modes, and forbidden_fixes files from the
// project's awareness root, writes a manifest.json at the bundle root, and
// returns the completed manifest.
//
// For NullAdapter projects RuntimeSignalsIncluded must be false. GlobularAdapter
// callers in the services repository may set it to true after writing
// runtime_signals.json themselves.
func Build(prof *project.ProjectProfile, outputDir string, opts BuildOptions) (*BundleManifest, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	manifest := &BundleManifest{
		SchemaVersion:    CurrentSchemaVersion,
		ProjectName:      prof.Name,
		ProjectKind:      string(prof.Kind),
		SourceRoot:       prof.Root,
		SourceRevision:   opts.Revision,
		GeneratedAt:      time.Now().UTC(),
		GeneratorVersion: opts.GeneratorVersion,
		ProfilePath:      "profile.json",
	}

	// Write profile.json.
	if profData, err := json.MarshalIndent(prof, "", "  "); err == nil {
		os.WriteFile(filepath.Join(outputDir, "profile.json"), profData, 0o644)
	}

	// Copy invariants.
	for _, src := range prof.Awareness.Invariants {
		dst, err := copyKnowledgeFile(src, outputDir)
		if err != nil {
			return nil, fmt.Errorf("copy invariants: %w", err)
		}
		manifest.InvariantsPaths = append(manifest.InvariantsPaths, dst)
	}

	// Copy failure modes.
	for _, src := range prof.Awareness.FailureModes {
		dst, err := copyKnowledgeFile(src, outputDir)
		if err != nil {
			return nil, fmt.Errorf("copy failure_modes: %w", err)
		}
		manifest.FailureModesPaths = append(manifest.FailureModesPaths, dst)
	}

	// Copy forbidden fixes.
	for _, src := range prof.Awareness.ForbiddenFixes {
		dst, err := copyKnowledgeFile(src, outputDir)
		if err != nil {
			return nil, fmt.Errorf("copy forbidden_fixes: %w", err)
		}
		manifest.ForbiddenFixesPaths = append(manifest.ForbiddenFixesPaths, dst)
	}

	// Copy optional extended knowledge files when present in the awareness root.
	if prof.Awareness.Root != "" {
		optionalFiles := []struct {
			name    string
			setPath func(string)
		}{
			{"decisions.yaml", func(p string) { manifest.DecisionsPath = p }},
			{"forbidden_assumptions.yaml", func(p string) { manifest.ForbiddenAssumptionsPath = p }},
			{"required_tests.yaml", func(p string) { manifest.RequiredTestsPath = p }},
			{"subsystem_boundaries.yaml", func(p string) { manifest.SubsystemBoundariesPath = p }},
			{"authority_rules.yaml", func(p string) { manifest.AuthorityRulesPath = p }},
			{"preflight_questions.yaml", func(p string) { manifest.PreflightQuestionsPath = p }},
			{"remediation_contracts.yaml", func(p string) { manifest.RemediationContractsPath = p }},
		}
		for _, f := range optionalFiles {
			src := filepath.Join(prof.Awareness.Root, f.name)
			if _, err := os.Stat(src); err == nil {
				if dst, err := copyKnowledgeFile(src, outputDir); err == nil {
					f.setPath(dst)
				}
			}
		}
	}

	// Runtime signals — set both canonical and v1 alias together.
	if opts.RuntimeSignalsIncluded {
		manifest.RuntimeSignalsPath = "runtime_signals.json"
		manifest.RuntimeSignalsIncluded = true
		manifest.IncludesRuntimeOverlay = true
	}

	// Write manifest.
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "bundle.json"), manifestData, 0o644); err != nil {
		return nil, fmt.Errorf("write bundle.json: %w", err)
	}

	return manifest, nil
}

// copyKnowledgeFile copies src to outputDir/basename(src) and returns the
// relative destination path ("basename").
func copyKnowledgeFile(src, outputDir string) (string, error) {
	base := filepath.Base(src)
	dst := filepath.Join(outputDir, base)

	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	return base, nil
}

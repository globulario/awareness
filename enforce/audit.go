package enforce

import (
	"context"

	"github.com/globulario/awareness/graph"
)

// AuditOptions controls which checks are run.
type AuditOptions struct {
	// RepoRoot is the repository root used for optional filesystem-backed checks.
	// When set, required-test validation may resolve test targets from *_test.go
	// even when graph test nodes are missing path metadata.
	RepoRoot string

	// SrcDir is the root directory to walk for annotation validation.
	// Leave empty to skip file-system annotation checks.
	SrcDir string

	// DocsDir is the awareness docs directory (docs/awareness).
	// Used by scaffold and coverage checks to load fix_cases.yaml.
	DocsDir string

	// SkipAnnotations disables the annotation well-formedness check.
	SkipAnnotations bool

	// SkipContracts disables the hash schema contract check.
	SkipContracts bool

	// SkipTests disables the required-test existence check.
	SkipTests bool

	// SkipDrift disables the graph drift check.
	SkipDrift bool

	// SkipScaffold disables the scaffold TODO-skip detection check.
	SkipScaffold bool

	// SkipInvariantShape disables invariant implementation graph shape checks.
	SkipInvariantShape bool

	// SkipImplCoverage disables the invariant implementation coverage ratchet check.
	SkipImplCoverage bool
}

// Audit runs all enabled enforcement checks and returns an aggregated result.
// g may be nil — graph-dependent checks are skipped with a Warning finding.
func Audit(ctx context.Context, g *graph.Graph, opts AuditOptions) *AuditResult {
	var all []Finding

	// 1. Annotation well-formedness (file-system walk, no graph needed).
	if !opts.SkipAnnotations && opts.SrcDir != "" {
		all = append(all, ValidateAnnotations(opts.SrcDir)...)
	}

	// 2. Hash schema contracts (graph).
	if !opts.SkipContracts {
		if g == nil {
			all = append(all, Finding{
				Code:     "NO_GRAPH",
				Severity: SeverityWarning,
				Message:  "hash-schema contract check skipped — no graph DB (run 'globular awareness build' first)",
			})
		} else {
			all = append(all, ValidateContracts(ctx, g)...)
		}
	}

	// 3. Required test existence (graph).
	if !opts.SkipTests {
		if g == nil {
			all = append(all, Finding{
				Code:     "NO_GRAPH",
				Severity: SeverityWarning,
				Message:  "required-test check skipped — no graph DB",
			})
		} else {
			all = append(all, ValidateRequiredTestsWithRepo(ctx, g, opts.RepoRoot)...)
		}
	}

	// 4. Graph drift (graph + file system).
	if !opts.SkipDrift && opts.SrcDir != "" {
		if g == nil {
			all = append(all, Finding{
				Code:     "NO_GRAPH",
				Severity: SeverityWarning,
				Message:  "drift check skipped — no graph DB",
			})
		} else {
			all = append(all, AuditDrift(ctx, g, opts.SrcDir)...)
		}
	}

	// 5. Scaffold TODO-skip detection (file-system walk, no graph needed).
	if !opts.SkipScaffold && opts.RepoRoot != "" {
		scan := ScanScaffoldTests(opts.RepoRoot, opts.DocsDir)
		all = append(all, scan.Findings...)
	}

	// 6. Invariant implementation graph shape check (graph).
	if !opts.SkipInvariantShape {
		if g == nil {
			all = append(all, Finding{
				Code:     "NO_GRAPH",
				Severity: SeverityWarning,
				Message:  "invariant shape check skipped — no graph DB",
			})
		} else {
			shape := InvariantShapeCheck(ctx, g)
			all = append(all, shape.Findings...)
		}
	}

	// 7. Invariant implementation coverage ratchet (graph).
	if !opts.SkipImplCoverage {
		if g == nil {
			all = append(all, Finding{
				Code:     "NO_GRAPH",
				Severity: SeverityWarning,
				Message:  "invariant implementation coverage check skipped — no graph DB",
			})
		} else {
			cov := InvariantImplementationCoverage(ctx, g, InvariantImplCoverageOptions{
				MinPercent: defaultImplCoverageMinPercent,
				Enforced:   true,
			})
			all = append(all, cov.Findings...)
		}
	}

	return newAuditResult(all)
}

// AuditFiles runs a targeted audit on a specific set of source files.
// Only annotation validation is run (graph is not required), making this
// fast enough for a pre-edit hook.
func AuditFiles(files []string) *AuditResult {
	var all []Finding
	for _, f := range files {
		all = append(all, validateFileAnnotations(f, f)...)
	}
	return newAuditResult(all)
}

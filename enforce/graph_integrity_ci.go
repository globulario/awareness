package enforce

// graph_integrity_ci.go — CI-grade graph integrity check.
//
// GraphIntegrityCICheck runs awareness graph integrity checks with
// CI-grade failure thresholds. It is stricter than the default audit:
//
//   - CodeInvariantNoImplementation → escalated to SeverityError (must-fail)
//   - CodeInvariantNoForbiddenFix   → escalated to SeverityWarning (reported)
//   - CodeInvariantNoFailureMode    → escalated to SeverityWarning (reported)
//   - CodeRequiredTestMissing       → SeverityError (unchanged)
//   - CodeDoneFixcaseScaffoldOnly   → SeverityError (unchanged)
//   - ScaffoldTodoSkips > MaxScaffoldSkips → exit-grade failure
//   - RequiredTestNoPath > MaxRequiredTestNoPath → exit-grade failure
//
// Plain warnings (shape checks with no escalation path) do NOT fail CI.
// This matches the principle: "CI prevents awareness graph decay from
// silently entering the repository."

import (
	"context"
	"fmt"

	"github.com/globulario/awareness/graph"
)

// CICheckOptions configures a GraphIntegrityCICheck run.
type CICheckOptions struct {
	// MaxScaffoldSkips is the maximum number of scaffold TODO skips allowed.
	// Zero means none allowed.
	MaxScaffoldSkips int
	// MaxRequiredTestNoPath is the maximum number of required tests without
	// a path to a real implementation.
	MaxRequiredTestNoPath int
	// RepoRoot is the repo root for path resolution (required for scaffold scan).
	RepoRoot string
	// DocsDir is the docs/awareness directory for fix-case cross-reference.
	DocsDir string
}

// CICheckResult holds the result of a GraphIntegrityCICheck run.
type CICheckResult struct {
	Pass           bool
	ErrorCount     int
	WarningCount   int
	Findings       []Finding
	FailureReasons []string // human-readable summary of exit-grade failures
}

// GraphIntegrityCICheck runs CI-grade graph integrity checks.
// Pass is false if any must-fail condition is triggered.
// Plain warnings are included in Findings but do not affect Pass.
func GraphIntegrityCICheck(ctx context.Context, g *graph.Graph, opts CICheckOptions) CICheckResult {
	var res CICheckResult

	// 1. Invariant shape checks — with escalated severities for CI.
	if g != nil {
		shapeRes := InvariantShapeCheck(ctx, g)
		for _, f := range shapeRes.Findings {
			escalated := escalateCIFinding(f)
			res.Findings = append(res.Findings, escalated)
			switch escalated.Severity {
			case SeverityError:
				res.ErrorCount++
				res.FailureReasons = append(res.FailureReasons,
					fmt.Sprintf("[%s] %s", escalated.Code, truncateCI(escalated.Message, 120)))
			case SeverityWarning:
				res.WarningCount++
			}
		}
	}

	// 2. Required tests check — CodeRequiredTestMissing is already SeverityError.
	if g != nil {
		testFindings := ValidateRequiredTestsWithRepo(ctx, g, opts.RepoRoot)
		for _, f := range testFindings {
			res.Findings = append(res.Findings, f)
			switch f.Severity {
			case SeverityError:
				res.ErrorCount++
				res.FailureReasons = append(res.FailureReasons,
					fmt.Sprintf("[%s] %s", f.Code, truncateCI(f.Message, 120)))
			case SeverityWarning:
				res.WarningCount++
			}
		}
	}

	// 3. Scaffold / DONE fixcase checks.
	if opts.RepoRoot != "" {
		scaffoldRes := ScanScaffoldTests(opts.RepoRoot, opts.DocsDir)
		for _, f := range scaffoldRes.Findings {
			res.Findings = append(res.Findings, f)
			switch f.Severity {
			case SeverityError:
				res.ErrorCount++
				res.FailureReasons = append(res.FailureReasons,
					fmt.Sprintf("[%s] %s", f.Code, truncateCI(f.Message, 120)))
			case SeverityWarning:
				res.WarningCount++
			}
		}
		if scaffoldRes.TotalScaffoldSkips > opts.MaxScaffoldSkips {
			reason := fmt.Sprintf("SCAFFOLD_TODO_SKIP threshold exceeded: %d > %d",
				scaffoldRes.TotalScaffoldSkips, opts.MaxScaffoldSkips)
			res.FailureReasons = append(res.FailureReasons, reason)
			res.ErrorCount++
		}
	}

	// 4. Required-test-no-path threshold.
	noPathCount := 0
	for _, f := range res.Findings {
		if f.Code == CodeRequiredTestNoPath {
			noPathCount++
		}
	}
	if noPathCount > opts.MaxRequiredTestNoPath {
		reason := fmt.Sprintf("REQUIRED_TEST_NO_PATH threshold exceeded: %d > %d",
			noPathCount, opts.MaxRequiredTestNoPath)
		res.FailureReasons = append(res.FailureReasons, reason)
		res.ErrorCount++
	}

	res.Pass = res.ErrorCount == 0
	return res
}

// escalateCIFinding upgrades specific invariant shape findings to CI-grade severities.
//
// CI escalation rules:
//   - INVARIANT_NO_IMPLEMENTATION → SeverityError  (was Warning — impl evidence is required)
//   - INVARIANT_NO_FORBIDDEN_FIX  → SeverityWarning (was Info  — forbidden fixes are safety)
//   - INVARIANT_NO_FAILURE_MODE   → SeverityWarning (was Info  — failure modes are safety)
//
// All other findings keep their original severity.
func escalateCIFinding(f Finding) Finding {
	switch f.Code {
	case CodeInvariantNoImplementation:
		f.Severity = SeverityError
	case CodeInvariantNoForbiddenFix, CodeInvariantNoFailureMode:
		if f.Severity == SeverityInfo {
			f.Severity = SeverityWarning
		}
	}
	return f
}

func truncateCI(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

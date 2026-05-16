package enforce

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/fixledger"
	"github.com/globulario/awareness/graph"
	"gopkg.in/yaml.v3"
)

type AnnotationCoverageOptions struct {
	RepoRoot      string
	SrcDir        string
	WatchlistPath string
	DocsDir       string
}

type highRiskWatchlistFile struct {
	Files []string `yaml:"files"`
}

func AnnotationCoverage(ctx context.Context, g *graph.Graph, opts AnnotationCoverageOptions) *AuditResult {
	var findings []Finding

	watchlist := loadWatchlistPatterns(opts.WatchlistPath)
	for _, rel := range watchlist {
		abs := filepath.Join(opts.RepoRoot, rel)
		if strings.HasSuffix(rel, "/") {
			continue
		}
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		if strings.HasSuffix(rel, "_test.go") || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if !fileHasGlobularAnnotation(abs) {
			findings = append(findings, Finding{
				Code:     "HIGH_RISK_FILE_NO_ANNOTATIONS",
				Severity: SeverityWarning,
				File:     rel,
				Message:  "high-risk file has no //globular annotations",
			})
		}
	}

	if g != nil {
		findings = append(findings, criticalInvariantCoverageFindings(ctx, g)...)
		findings = append(findings, schemaAndTransitionCoverageFindings(ctx, g)...)
	}

	findings = append(findings, fixLedgerCoverageFindings(ctx, g, opts.DocsDir, opts.RepoRoot)...)
	return newAuditResult(findings)
}

func loadWatchlistPatterns(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f highRiskWatchlistFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil
	}
	var out []string
	for _, p := range f.Files {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fileHasGlobularAnnotation(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//globular:") {
			return true
		}
	}
	return false
}

func criticalInvariantCoverageFindings(ctx context.Context, g *graph.Graph) []Finding {
	invariants, err := g.AllInvariants(ctx)
	if err != nil {
		return []Finding{{
			Code:     "ANNOTATION_COVERAGE_QUERY_ERROR",
			Severity: SeverityError,
			Message:  "failed to query invariants: " + err.Error(),
		}}
	}
	enforces, _ := g.EdgesByKind(ctx, graph.EdgeEnforces)
	protects, _ := g.EdgesByKind(ctx, graph.EdgeProtects)
	covered := map[string]bool{}
	for _, e := range enforces {
		covered[e.Dst] = true
	}
	for _, e := range protects {
		covered[e.Dst] = true
	}
	var findings []Finding
	for _, inv := range invariants {
		if strings.ToLower(inv.Severity) != "critical" {
			continue
		}
		nodeID := "invariant:" + inv.ID
		if !covered[nodeID] {
			findings = append(findings, Finding{
				Code:     "CRITICAL_INVARIANT_NO_ENFORCER",
				Severity: SeverityWarning,
				Symbol:   inv.ID,
				Message:  fmt.Sprintf("critical invariant '%s' has no enforces/protects symbols", inv.ID),
			})
		}
	}
	return findings
}

func schemaAndTransitionCoverageFindings(ctx context.Context, g *graph.Graph) []Finding {
	testedBy, _ := g.EdgesByKind(ctx, graph.EdgeTestedBy)
	hasTest := map[string]bool{}
	for _, e := range testedBy {
		hasTest[e.Src] = true
	}

	var findings []Finding
	produces, _ := g.EdgesByKind(ctx, graph.EdgeProduces)
	requires, _ := g.EdgesByKind(ctx, graph.EdgeRequires)
	schemaSymbols := map[string]bool{}
	for _, e := range produces {
		if strings.HasPrefix(e.Dst, "hash_schema:") {
			schemaSymbols[e.Src] = true
		}
	}
	for _, e := range requires {
		if strings.HasPrefix(e.Dst, "hash_schema:") {
			schemaSymbols[e.Src] = true
		}
	}
	for sym := range schemaSymbols {
		if !hasTest[sym] {
			findings = append(findings, Finding{
				Code:     "HASH_SCHEMA_WITHOUT_TEST",
				Severity: SeverityWarning,
				Symbol:   sym,
				Message:  "symbol has hash_schema contract but no tested_by annotation",
			})
		}
	}

	affects, _ := g.EdgesByKind(ctx, graph.EdgeAffects)
	stateTransitionSymbols := map[string]bool{}
	for _, e := range affects {
		if strings.HasPrefix(e.Dst, "state_transition:") {
			stateTransitionSymbols[e.Src] = true
		}
	}
	for sym := range stateTransitionSymbols {
		if !hasTest[sym] {
			findings = append(findings, Finding{
				Code:     "STATE_TRANSITION_WITHOUT_TEST",
				Severity: SeverityWarning,
				Symbol:   sym,
				Message:  "symbol has state_transition annotation but no tested_by annotation",
			})
		}
	}
	return findings
}

func fixLedgerCoverageFindings(ctx context.Context, g *graph.Graph, docsDir, repoRoot string) []Finding {
	path := filepath.Join(docsDir, "fix_cases.yaml")
	cases, err := fixledger.LoadFixCases(path)
	if err != nil {
		return []Finding{{
			Code:     "FIX_LEDGER_LOAD_ERROR",
			Severity: SeverityWarning,
			Message:  "failed to load fix_cases.yaml: " + err.Error(),
		}}
	}

	isCriticalInvariant := func(id string) bool {
		if g == nil {
			return false
		}
		inv, err := g.FindInvariant(ctx, id)
		return err == nil && inv != nil && strings.ToLower(inv.Severity) == "critical"
	}

	var findings []Finding
	for _, fc := range cases {
		critical := false
		for _, inv := range fc.TargetInvariants {
			if isCriticalInvariant(inv) {
				critical = true
				break
			}
		}
		if !critical {
			continue
		}
		check := func(rel string) {
			rel = strings.TrimSpace(rel)
			if rel == "" || strings.HasSuffix(rel, "_test.go") || !strings.HasSuffix(rel, ".go") {
				return
			}
			abs := filepath.Join(repoRoot, rel)
			if _, err := os.Stat(abs); err != nil {
				return
			}
			if !fileHasGlobularAnnotation(abs) {
				findings = append(findings, Finding{
					Code:     "FIX_LEDGER_CRITICAL_FILE_NO_ANNOTATIONS",
					Severity: SeverityWarning,
					File:     rel,
					Symbol:   fc.ID,
					Message:  "file touched by critical fix-ledger case has no //globular annotations",
				})
			}
		}
		for _, f := range fc.FixedFiles {
			check(f)
		}
		for _, f := range fc.RemainingFiles {
			check(f)
		}
	}
	return findings
}

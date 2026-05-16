package enforce

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
)

// GoFileCoverageResult holds the graph Go-file coverage metrics.
type GoFileCoverageResult struct {
	EligibleGoFilesTotal        int     `json:"eligible_go_files_total"`
	IndexedGoFilesTotal         int     `json:"indexed_go_files_total"`
	CoveragePercentGoFiles      float64 `json:"coverage_percent_go_files"`
	EligibleNonTestGoFiles      int     `json:"eligible_non_test_go_files_total"`
	IndexedNonTestGoFiles       int     `json:"indexed_non_test_go_files_total"`
	CoveragePercentNonTestFiles float64 `json:"coverage_percent_non_test_go_files"`
	MissingFiles                []string `json:"missing_files,omitempty"`
	BlindSpots                  []string `json:"blind_spots,omitempty"`
	ConfidenceImpact            string   `json:"confidence_impact"`
	Findings                    []Finding `json:"-"`
}

// coverageThresholds defines warn/critical levels for Go file coverage.
var coverageThresholds = struct {
	WarnPercent     float64
	CriticalPercent float64
}{
	WarnPercent:     85.0,
	CriticalPercent: 70.0,
}

// GoFileCoverage counts eligible Go source files in repoRoot and compares them
// against the source_file nodes indexed in g. Returns metrics and findings.
// g may be nil — in that case the indexed count is 0.
func GoFileCoverage(ctx context.Context, g *graph.Graph, repoRoot string) GoFileCoverageResult {
	var res GoFileCoverageResult

	if repoRoot == "" {
		res.ConfidenceImpact = "unknown"
		res.BlindSpots = []string{"repo root not provided — cannot measure Go file coverage"}
		return res
	}

	// Walk eligible Go files.
	eligibleSet := map[string]bool{}
	_ = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		if isExcludedPath(rel) {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		if isGeneratedProto(rel) {
			return nil
		}
		eligibleSet[rel] = true
		res.EligibleGoFilesTotal++
		if !strings.HasSuffix(rel, "_test.go") {
			res.EligibleNonTestGoFiles++
		}
		return nil
	})

	if g == nil {
		res.ConfidenceImpact = "low"
		res.BlindSpots = []string{fmt.Sprintf("%d eligible Go files cannot be checked — graph not loaded", res.EligibleGoFilesTotal)}
		return res
	}

	// Query graph for indexed source_file nodes.
	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSourceFile)
	if err != nil {
		res.ConfidenceImpact = "low"
		res.BlindSpots = []string{"graph source_file query failed: " + err.Error()}
		return res
	}

	indexedSet := map[string]bool{}
	for _, n := range nodes {
		if n.Path == "" {
			continue
		}
		p := filepath.ToSlash(n.Path)
		indexedSet[p] = true
		if strings.HasSuffix(p, ".go") {
			res.IndexedGoFilesTotal++
			if !strings.HasSuffix(p, "_test.go") {
				res.IndexedNonTestGoFiles++
			}
		}
	}

	// Find eligible files not in graph.
	for rel := range eligibleSet {
		norm := filepath.ToSlash(rel)
		if !indexedSet[norm] {
			res.MissingFiles = append(res.MissingFiles, rel)
		}
	}

	// Coverage percentages.
	if res.EligibleGoFilesTotal > 0 {
		res.CoveragePercentGoFiles = float64(res.IndexedGoFilesTotal) / float64(res.EligibleGoFilesTotal) * 100
	}
	if res.EligibleNonTestGoFiles > 0 {
		res.CoveragePercentNonTestFiles = float64(res.IndexedNonTestGoFiles) / float64(res.EligibleNonTestGoFiles) * 100
	}

	// Confidence impact and findings.
	missing := len(res.MissingFiles)
	switch {
	case res.CoveragePercentGoFiles < coverageThresholds.CriticalPercent:
		res.ConfidenceImpact = "high"
		res.BlindSpots = append(res.BlindSpots,
			fmt.Sprintf("%d eligible Go files are not represented in the graph (coverage %.1f%% < %.0f%%)",
				missing, res.CoveragePercentGoFiles, coverageThresholds.CriticalPercent))
		res.Findings = append(res.Findings, Finding{
			Code:     CodeGraphCoverageCritical,
			Severity: SeverityError,
			Message: fmt.Sprintf("graph Go-file coverage critical: %.1f%% (%d/%d files indexed)",
				res.CoveragePercentGoFiles, res.IndexedGoFilesTotal, res.EligibleGoFilesTotal),
		})
	case res.CoveragePercentGoFiles < coverageThresholds.WarnPercent:
		res.ConfidenceImpact = "medium"
		res.BlindSpots = append(res.BlindSpots,
			fmt.Sprintf("%d eligible Go files are not represented in the graph (coverage %.1f%% < %.0f%%)",
				missing, res.CoveragePercentGoFiles, coverageThresholds.WarnPercent))
		res.Findings = append(res.Findings, Finding{
			Code:     CodeGraphCoverageLow,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("graph Go-file coverage low: %.1f%% (%d/%d files indexed)",
				res.CoveragePercentGoFiles, res.IndexedGoFilesTotal, res.EligibleGoFilesTotal),
		})
	default:
		res.ConfidenceImpact = "low"
	}

	return res
}

// isGeneratedProto returns true for .pb.go and .pb.gw.go files (generated protobuf).
func isGeneratedProto(rel string) bool {
	return strings.HasSuffix(rel, ".pb.go") || strings.HasSuffix(rel, ".pb.gw.go")
}

package enforce

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"gopkg.in/yaml.v3"
)

type StrictGateInput struct {
	Strict                   bool
	HighRisk                 bool
	Files                    []string
	Preflight                *preflight.Report
	FileAudit                *AuditResult
	AnnotationRefFindings    []Finding
	PerFileAuditFindings     []Finding
}

type StrictGateResult struct {
	ShouldBlock bool
	Reasons     []string
}

func EvaluateStrictGate(in StrictGateInput) StrictGateResult {
	if !in.Strict || !in.HighRisk {
		return StrictGateResult{}
	}
	var reasons []string
	hasClass := func(c preflight.TaskClass) bool {
		if in.Preflight == nil {
			return false
		}
		for _, cl := range in.Preflight.Classification {
			if cl == c {
				return true
			}
		}
		return false
	}

	if hasClass(preflight.ClassUnknownImpact) {
		reasons = append(reasons, "UNKNOWN_IMPACT on high-risk file")
	}
	if hasClass(preflight.ClassConvergenceRisk) && (in.Preflight == nil || len(in.Preflight.RequiredTests) == 0) {
		reasons = append(reasons, "CONVERGENCE_RISK with no required tests")
	}
	if len(in.AnnotationRefFindings) > 0 {
		reasons = append(reasons, "annotation references missing invariants/tests")
	}
	if in.FileAudit != nil && in.FileAudit.ErrorCount > 0 {
		reasons = append(reasons, "annotation validation contains ERROR findings")
	}
	for _, f := range in.PerFileAuditFindings {
		if f.Severity == SeverityError {
			reasons = append(reasons, "awareness audit reports ERROR severity for edited file")
			break
		}
	}
	if in.Preflight != nil && len(in.Preflight.ForbiddenFixes) > 0 && len(in.Preflight.RecommendedOrder) == 0 {
		reasons = append(reasons, "forbidden fixes present with no investigation order")
	}

	return StrictGateResult{
		ShouldBlock: len(reasons) > 0,
		Reasons:     reasons,
	}
}

func ValidateAnnotationReferences(ctx context.Context, g *graph.Graph, files []string) []Finding {
	if g == nil {
		return nil
	}
	var findings []Finding
	for _, f := range files {
		abs := f
		src, err := readFileText(abs)
		if err != nil {
			continue
		}
		for lineNo, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "//globular:") {
				continue
			}
			rest := strings.TrimPrefix(trimmed, "//globular:")
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) < 2 {
				continue
			}
			directive := parts[0]
			value := strings.TrimSpace(parts[1])
			if value == "" {
				continue
			}
			switch directive {
			case "enforces", "protects":
				id := "invariant:" + value
				n, _ := g.FindNode(ctx, id)
				if n == nil {
					findings = append(findings, Finding{
						Code:     "ANNOTATION_REF_INVARIANT_MISSING",
						Severity: SeverityError,
						File:     filepath.ToSlash(f),
						Message:  fmt.Sprintf("%s:%d references missing invariant '%s'", filepath.ToSlash(f), lineNo+1, value),
					})
				}
			case "tested_by":
				id := "test:" + value
				n, _ := g.FindNode(ctx, id)
				if n == nil {
					findings = append(findings, Finding{
						Code:     "ANNOTATION_REF_TEST_MISSING",
						Severity: SeverityError,
						File:     filepath.ToSlash(f),
						Message:  fmt.Sprintf("%s:%d references missing test '%s'", filepath.ToSlash(f), lineNo+1, value),
					})
				}
			}
		}
	}
	return findings
}

type highRiskList struct {
	Files []string `yaml:"files"`
}

func LoadHighRiskWatchlist(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w highRiskList
	if err := yaml.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	return w.Files, nil
}

func IsHighRiskFile(rel string, patterns []string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	for _, raw := range patterns {
		p := filepath.ToSlash(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(rel, strings.TrimSuffix(p, "/")+"/") {
				return true
			}
			continue
		}
		if rel == p {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

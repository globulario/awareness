package preflight

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/awareness/knowledge"
)

// kindSource maps a knowledge kind to its canonical source filename.
var kindSource = map[string]string{
	"invariant":            "invariants.yaml",
	"failure_mode":         "failure_modes.yaml",
	"forbidden_fix":        "forbidden_fixes.yaml",
	"incident_pattern":     "incident_patterns.yaml",
	"decision":             "decisions.yaml",
	"forbidden_assumption": "forbidden_assumptions.yaml",
	"required_test":        "required_tests.yaml",
	"subsystem_boundary":   "subsystem_boundaries.yaml",
	"authority_rule":       "authority_rules.yaml",
	"preflight_question":   "preflight_questions.yaml",
	"remediation_contract": "remediation_contracts.yaml",
}

// RawKnowledgeFallback scans hand-authored awareness YAML files directly.
// It is intentionally simple and deterministic: if the graph query misses,
// this gives the agent a second lantern before it walks into the cave.
func RawKnowledgeFallback(task string, files []string, docsDir string) []RawKnowledgeMatch {
	if strings.TrimSpace(docsDir) == "" {
		return nil
	}
	j := func(name string) string { return filepath.Join(docsDir, name) }

	// convergence_rules.yaml is a second invariants file specific to Globular.
	invariants := nonEmpty(j("invariants.yaml"), j("convergence_rules.yaml"))
	failureModes := nonEmpty(j("failure_modes.yaml"))
	forbiddenFixes := nonEmpty(j("forbidden_fixes.yaml"))
	incidentPatterns := nonEmpty(j("incident_patterns.yaml"))

	base, err := knowledge.LoadFromPaths(invariants, failureModes, forbiddenFixes, incidentPatterns, docsDir)
	if err != nil || base == nil {
		return nil
	}

	matches := knowledge.Search(base, task, files)
	if len(matches) == 0 {
		return nil
	}

	out := make([]RawKnowledgeMatch, 0, len(matches))
	for _, m := range matches {
		src := kindSource[m.Kind]
		if src == "" {
			src = m.Kind + ".yaml"
		}
		out = append(out, RawKnowledgeMatch{
			Source:       src,
			Kind:         m.Kind,
			ID:           m.ID,
			Score:        m.Score,
			MatchedTerms: m.MatchedTerms,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// nonEmpty returns only the non-empty strings from the variadic list.
func nonEmpty(paths ...string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

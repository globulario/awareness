package preflight

import (
	"sort"
	"strings"

	"github.com/globulario/awareness/knowledge"
	"github.com/globulario/awareness/project"
)

// RawKnowledgeMatch is a scored match from the hand-authored awareness YAML files.
// The public shape is kept stable so that services MCP tools that import this
// package continue to compile without changes.
type RawKnowledgeMatch struct {
	Source       string   `json:"source"`
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Score        int      `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
}

// kindSource maps a knowledge kind to its canonical filename.
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

// RawKnowledgeFallback scans hand-authored awareness YAML files for entries
// relevant to the given task and changed files. It is deterministic and
// requires no database. Results are sorted by score descending, capped at 12.
//
// docsDir is the .awareness/ directory path (prof.Awareness.Root).
func RawKnowledgeFallback(task string, files []string, docsDir string) []RawKnowledgeMatch {
	if strings.TrimSpace(docsDir) == "" {
		return nil
	}

	base, err := knowledge.Load(docsDir)
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

// ExtendedPreflightItems returns the extended knowledge items (decisions,
// forbidden assumptions, authority rules, required tests, preflight questions,
// and remediation contracts) that match the given task and changed files.
// docsDir is the .awareness/ directory path.
func ExtendedPreflightItems(task string, files []string, docsDir string) *knowledge.PreflightItems {
	if strings.TrimSpace(docsDir) == "" {
		return nil
	}
	base, err := knowledge.Load(docsDir)
	if err != nil || base == nil {
		return nil
	}
	items := knowledge.MatchedPreflightItems(base, task, files)
	return &items
}

// RawKnowledgeFallbackFromPaths is the profile-aware variant of RawKnowledgeFallback.
// It uses the multi-file path lists from the project profile instead of a single directory,
// enabling Globular-style configs where invariants, failure modes, and forbidden fixes
// each span multiple YAML files.
func RawKnowledgeFallbackFromPaths(task string, files []string, paths project.AwarenessPaths) []RawKnowledgeMatch {
	base, err := knowledge.LoadFromPaths(
		paths.Invariants,
		paths.FailureModes,
		paths.ForbiddenFixes,
		paths.IncidentPatterns,
		paths.Root,
	)
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

// ExtendedPreflightItemsFromPaths is the profile-aware variant of ExtendedPreflightItems.
// It uses the multi-file path lists from the project profile.
func ExtendedPreflightItemsFromPaths(task string, files []string, paths project.AwarenessPaths) *knowledge.PreflightItems {
	base, err := knowledge.LoadFromPaths(
		paths.Invariants,
		paths.FailureModes,
		paths.ForbiddenFixes,
		paths.IncidentPatterns,
		paths.Root,
	)
	if err != nil || base == nil {
		return nil
	}
	items := knowledge.MatchedPreflightItems(base, task, files)
	return &items
}

// UniqueStrings deduplicates a string slice preserving order.
func UniqueStrings(in []string) []string {
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

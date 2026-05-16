package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─── Typed knowledge entries ──────────────────────────────────────────────────

// InvariantEntry is one item from invariants.yaml.
type InvariantEntry struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Description string   `yaml:"description" json:"description"`
	Severity    string   `yaml:"severity,omitempty" json:"severity,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath  string   `yaml:"-" json:"source_path,omitempty"`
}

// FailureModeEntry is one item from failure_modes.yaml.
type FailureModeEntry struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Description string   `yaml:"description" json:"description"`
	Symptoms    []string `yaml:"symptoms,omitempty" json:"symptoms,omitempty"`
	WrongFixes  []string `yaml:"wrong_fixes,omitempty" json:"wrong_fixes,omitempty"`
	Severity    string   `yaml:"severity,omitempty" json:"severity,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath  string   `yaml:"-" json:"source_path,omitempty"`
}

// ForbiddenFixEntry is one item from forbidden_fixes.yaml.
type ForbiddenFixEntry struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Description string   `yaml:"description" json:"description"`
	AppliesWhen string   `yaml:"applies_when,omitempty" json:"applies_when,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath  string   `yaml:"-" json:"source_path,omitempty"`
}

// ─── Loaders ─────────────────────────────────────────────────────────────────

type invariantsFile struct {
	Invariants []InvariantEntry `yaml:"invariants"`
}

type failureModesFile struct {
	FailureModes []FailureModeEntry `yaml:"failure_modes"`
}

type forbiddenFixesFile struct {
	ForbiddenFixes []ForbiddenFixEntry `yaml:"forbidden_fixes"`
}

// loadInvariants loads all InvariantEntries from a list of YAML file paths.
func loadInvariants(paths []string) []InvariantEntry {
	var out []InvariantEntry
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f invariantsFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.Invariants {
			f.Invariants[i].SourcePath = p
		}
		out = append(out, f.Invariants...)
	}
	return out
}

// loadFailureModes loads all FailureModeEntries from a list of YAML file paths.
func loadFailureModes(paths []string) []FailureModeEntry {
	var out []FailureModeEntry
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f failureModesFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.FailureModes {
			f.FailureModes[i].SourcePath = p
		}
		out = append(out, f.FailureModes...)
	}
	return out
}

// loadForbiddenFixes loads all ForbiddenFixEntries from a list of YAML file paths.
func loadForbiddenFixes(paths []string) []ForbiddenFixEntry {
	var out []ForbiddenFixEntry
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f forbiddenFixesFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			continue
		}
		for i := range f.ForbiddenFixes {
			f.ForbiddenFixes[i].SourcePath = p
		}
		out = append(out, f.ForbiddenFixes...)
	}
	return out
}

// ─── Keyword search ──────────────────────────────────────────────────────────

type scoredInvariant struct {
	Entry InvariantEntry
	Score int
}

type scoredFailureMode struct {
	Entry FailureModeEntry
	Score int
}

// searchInvariants returns invariants that match query keywords, sorted by score.
func searchInvariants(entries []InvariantEntry, query string, limit int) []InvariantEntry {
	terms := knowledgeTerms(query)
	if len(terms) == 0 {
		if limit <= 0 || limit > len(entries) {
			return entries
		}
		return entries[:limit]
	}
	var scored []scoredInvariant
	for _, e := range entries {
		blob := strings.ToLower(e.ID + " " + e.Title + " " + e.Description + " " + strings.Join(e.Tags, " "))
		s := countMatches(blob, terms)
		if s > 0 {
			scored = append(scored, scoredInvariant{e, s})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]InvariantEntry, len(scored))
	for i, s := range scored {
		out[i] = s.Entry
	}
	return out
}

// searchFailureModes returns failure modes that match query keywords, sorted by score.
func searchFailureModes(entries []FailureModeEntry, query string, limit int) []FailureModeEntry {
	terms := knowledgeTerms(query)
	if len(terms) == 0 {
		if limit <= 0 || limit > len(entries) {
			return entries
		}
		return entries[:limit]
	}
	var scored []scoredFailureMode
	for _, e := range entries {
		blob := strings.ToLower(e.ID + " " + e.Title + " " + e.Description + " " + strings.Join(e.Symptoms, " ") + " " + strings.Join(e.Tags, " "))
		s := countMatches(blob, terms)
		if s > 0 {
			scored = append(scored, scoredFailureMode{e, s})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]FailureModeEntry, len(scored))
	for i, s := range scored {
		out[i] = s.Entry
	}
	return out
}

// knowledgeTerms splits a query into lowercase search tokens.
func knowledgeTerms(query string) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Fields(strings.ToLower(query)) {
		part = strings.Trim(part, ".,;:\"'()[]{}")
		if len(part) < 2 || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

func countMatches(blob string, terms []string) int {
	n := 0
	for _, t := range terms {
		if strings.Contains(blob, t) {
			n++
		}
	}
	return n
}

// ─── Graph file detection ────────────────────────────────────────────────────

// graphJSONPath returns the path to a graph.json file in cacheDir,
// or "" when none exists.
func graphJSONPath(cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	p := filepath.Join(cacheDir, "graph.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// graphDBPath returns the path to a graph.db file in cacheDir,
// or "" when none exists.
func graphDBPath(cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	p := filepath.Join(cacheDir, "graph.db")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// ─── Graph JSON query ─────────────────────────────────────────────────────────

// graphNode is the minimal JSON shape for a node in a serialised graph.json.
type graphNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type graphFile struct {
	Nodes []graphNode `json:"nodes"`
}

// queryGraphJSON loads graph.json and returns nodes whose ID/kind/label/properties
// contain at least one query term.
func queryGraphJSON(path, query string, limit int) ([]graphNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var gf graphFile
	if err := json.Unmarshal(data, &gf); err != nil {
		return nil, err
	}
	terms := knowledgeTerms(query)
	if len(terms) == 0 {
		if limit > 0 && len(gf.Nodes) > limit {
			return gf.Nodes[:limit], nil
		}
		return gf.Nodes, nil
	}
	type scored struct {
		n graphNode
		s int
	}
	var results []scored
	for _, n := range gf.Nodes {
		parts := []string{n.ID, n.Kind, n.Label}
		for _, v := range n.Properties {
			parts = append(parts, v)
		}
		blob := strings.ToLower(strings.Join(parts, " "))
		s := countMatches(blob, terms)
		if s > 0 {
			results = append(results, scored{n, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].s > results[j].s
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]graphNode, len(results))
	for i, r := range results {
		out[i] = r.n
	}
	return out, nil
}

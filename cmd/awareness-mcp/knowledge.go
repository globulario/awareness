package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ─── Typed knowledge entries ──────────────────────────────────────────────────

// InvariantEntry is one item from invariants.yaml.
//
// Two YAML schemas coexist in this project:
//   - module-self format (.awareness/):   uses "description:"
//   - docs/awareness format:              uses "summary:" (and "enforcement:", "protects:", etc.)
//
// Both are captured here so the search blob is populated regardless of which format the file uses.
type InvariantEntry struct {
	ID          string   `yaml:"id" json:"id"`
	Title       string   `yaml:"title" json:"title"`
	Summary     string   `yaml:"summary" json:"summary,omitempty"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Enforcement string   `yaml:"enforcement" json:"enforcement,omitempty"`
	Severity    string   `yaml:"severity,omitempty" json:"severity,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath  string   `yaml:"-" json:"source_path,omitempty"`
}

// FailureModeEntry is one item from failure_modes.yaml.
//
// Two YAML schemas coexist:
//   - module-self / failuregraph_seeds format: uses "description:", "wrong_fixes:"
//   - docs/awareness format:                   uses "summary:", "root_cause:", "known_bad_fixes:", "architecture_fix:"
type FailureModeEntry struct {
	ID              string   `yaml:"id" json:"id"`
	Title           string   `yaml:"title" json:"title"`
	Summary         string   `yaml:"summary" json:"summary,omitempty"`
	Description     string   `yaml:"description" json:"description,omitempty"`
	RootCause       string   `yaml:"root_cause" json:"root_cause,omitempty"`
	ArchitectureFix string   `yaml:"architecture_fix" json:"architecture_fix,omitempty"`
	Symptoms        []string `yaml:"symptoms,omitempty" json:"symptoms,omitempty"`
	WrongFixes      []string `yaml:"wrong_fixes,omitempty" json:"wrong_fixes,omitempty"`
	KnownBadFixes   []string `yaml:"known_bad_fixes,omitempty" json:"known_bad_fixes,omitempty"`
	Severity        string   `yaml:"severity,omitempty" json:"severity,omitempty"`
	Tags            []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath      string   `yaml:"-" json:"source_path,omitempty"`
}

// ForbiddenFixEntry is one item from forbidden_fixes.yaml.
//
// Two YAML schemas coexist:
//   - docs/awareness format:  uses "summary:", "safe_alternative:", "related_invariants:"
//   - module-self format:     uses "description:", "correct_approach:", "forbidden_pattern:"
type ForbiddenFixEntry struct {
	ID              string   `yaml:"id" json:"id"`
	Title           string   `yaml:"title" json:"title"`
	Summary         string   `yaml:"summary" json:"summary,omitempty"`
	Description     string   `yaml:"description" json:"description,omitempty"`
	SafeAlternative string   `yaml:"safe_alternative" json:"safe_alternative,omitempty"`
	CorrectApproach string   `yaml:"correct_approach" json:"correct_approach,omitempty"`
	AppliesWhen     string   `yaml:"applies_when,omitempty" json:"applies_when,omitempty"`
	Tags            []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	SourcePath      string   `yaml:"-" json:"source_path,omitempty"`
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
		blob := strings.ToLower(strings.Join([]string{
			e.ID, e.Title, e.Summary, e.Description, e.Enforcement,
			strings.Join(e.Tags, " "),
		}, " "))
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
		blob := strings.ToLower(strings.Join([]string{
			e.ID, e.Title, e.Summary, e.Description, e.RootCause, e.ArchitectureFix,
			strings.Join(e.Symptoms, " "),
			strings.Join(e.WrongFixes, " "),
			strings.Join(e.KnownBadFixes, " "),
			strings.Join(e.Tags, " "),
		}, " "))
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

// ─── Graph JSON types ─────────────────────────────────────────────────────────

// graphNode is the minimal JSON shape for a node in a serialised graph.json.
type graphNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type graphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// loadedGraphFile is the full deserialized graph.json.
type loadedGraphFile struct {
	SchemaVersion string      `json:"schema_version"`
	Project       string      `json:"project"`
	GeneratedAt   time.Time   `json:"generated_at"`
	Nodes         []graphNode `json:"nodes"`
	Edges         []graphEdge `json:"edges"`
}

// graphNodeResult is what the MCP tool returns for a graph node + neighbor.
type graphNodeResult struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Label      string            `json:"label,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	EdgeKind   string            `json:"edge_kind,omitempty"`
}

// loadGraphFile loads a graph.json from the cache directory, falling back to
// the awareness root cache. Returns an error when no graph file is found.
func loadGraphFile(cacheDir, awarenessRoot string) (*loadedGraphFile, error) {
	candidates := []string{}
	if cacheDir != "" {
		candidates = append(candidates, filepath.Join(cacheDir, "graph.json"))
	}
	if awarenessRoot != "" {
		candidates = append(candidates, filepath.Join(awarenessRoot, "cache", "graph.json"))
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var gf loadedGraphFile
		if err := json.Unmarshal(data, &gf); err != nil {
			continue
		}
		return &gf, nil
	}
	return nil, os.ErrNotExist
}

// lookupNodeInGraph finds the target node and its immediate neighbors.
// It matches by nodeID prefix (e.g. "invariant:foo") or by file path keywords.
func lookupNodeInGraph(gf *loadedGraphFile, nodeID, path string, maxNeighbors int) (*graphNodeResult, []graphNodeResult) {
	if gf == nil {
		return nil, nil
	}

	// Build node index.
	nodeByID := make(map[string]graphNode, len(gf.Nodes))
	for _, n := range gf.Nodes {
		nodeByID[n.ID] = n
	}

	// Find target node.
	var target *graphNode
	if nodeID != "" {
		if n, ok := nodeByID[nodeID]; ok {
			target = &n
		}
		// Prefix match: "invariant:foo" matches "invariant:foo.bar"
		if target == nil {
			for _, n := range gf.Nodes {
				if strings.HasPrefix(n.ID, nodeID) {
					tmp := n
					target = &tmp
					break
				}
			}
		}
	}
	// Path-based fallback: find a source_file node whose path matches.
	if target == nil && path != "" {
		base := filepath.Base(path)
		for _, n := range gf.Nodes {
			if n.Kind == "source_file" {
				if strings.Contains(n.ID, base) || strings.Contains(n.ID, filepath.ToSlash(path)) {
					tmp := n
					target = &tmp
					break
				}
			}
		}
	}

	if target == nil {
		return nil, nil
	}

	targetResult := &graphNodeResult{
		ID:         target.ID,
		Kind:       target.Kind,
		Label:      target.Label,
		Properties: target.Properties,
	}

	// Collect neighbors (depth 1).
	seen := map[string]bool{target.ID: true}
	var neighbors []graphNodeResult
	for _, e := range gf.Edges {
		if len(neighbors) >= maxNeighbors {
			break
		}
		neighborID := ""
		edgeKind := e.Kind
		if e.From == target.ID && !seen[e.To] {
			neighborID = e.To
		} else if e.To == target.ID && !seen[e.From] {
			neighborID = e.From
			edgeKind = "←" + edgeKind
		}
		if neighborID == "" {
			continue
		}
		seen[neighborID] = true
		if n, ok := nodeByID[neighborID]; ok {
			neighbors = append(neighbors, graphNodeResult{
				ID:         n.ID,
				Kind:       n.Kind,
				Label:      n.Label,
				Properties: n.Properties,
				EdgeKind:   edgeKind,
			})
		}
	}

	return targetResult, neighbors
}

// ─── Graph JSON query ─────────────────────────────────────────────────────────

// queryGraphJSON loads graph.json and returns nodes whose ID/kind/label/properties
// contain at least one query term.
func queryGraphJSON(path, query string, limit int) ([]graphNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var gf loadedGraphFile
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

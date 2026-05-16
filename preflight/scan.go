package preflight

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var rawTokenRE = regexp.MustCompile(`[a-zA-Z0-9_./:-]+`)

var rawStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "that": true,
	"this": true, "into": true, "when": true, "where": true, "what": true, "will": true,
	"make": true, "fix": true, "code": true, "file": true, "tool": true, "safe": true,
	"module": true, "awareness": true, "globular": true, "service": true, "services": true,
}

// RawKnowledgeMatch is a conservative fallback match from the source YAML files.
// It exists to make NO_MATCH honest: graph lookup can be silent while the
// hand-authored truth files still contain relevant knowledge.
type RawKnowledgeMatch struct {
	Source       string   `json:"source"`
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Score        int      `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
}

// RawKnowledgeFallback scans hand-authored awareness YAML files directly.
// It is intentionally simple and deterministic: if the graph query misses,
// this gives the agent a second lantern before it walks into the cave.
func RawKnowledgeFallback(task string, files []string, docsDir string) []RawKnowledgeMatch {
	if strings.TrimSpace(docsDir) == "" {
		return nil
	}
	terms := rawSearchTerms(task, files)
	if len(terms) == 0 {
		return nil
	}

	candidates := []struct{ file, kind, listKey string }{
		{"failure_modes.yaml", "failure_mode", "failure_modes"},
		{"invariants.yaml", "invariant", "invariants"},
		{"convergence_rules.yaml", "invariant", "invariants"},
		{"forbidden_fixes.yaml", "forbidden_fix", "forbidden_fixes"},
		{"design_patterns.yaml", "design_pattern", "design_patterns"},
		{"patterns.yaml", "pattern", "patterns"},
	}

	var out []RawKnowledgeMatch
	for _, c := range candidates {
		path := filepath.Join(docsDir, c.file)
		data, err := os.ReadFile(path)
		if err != nil || len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		var root map[string]interface{}
		if err := yaml.Unmarshal(data, &root); err != nil {
			continue
		}
		items, _ := root[c.listKey].([]interface{})
		for _, item := range items {
			m, _ := item.(map[string]interface{})
			if len(m) == 0 {
				continue
			}
			id, _ := m["id"].(string)
			blobBytes, _ := yaml.Marshal(item)
			blob := strings.ToLower(string(blobBytes))
			matched := make([]string, 0)
			for _, term := range terms {
				if strings.Contains(blob, strings.ToLower(term)) {
					matched = append(matched, term)
				}
			}
			if len(matched) == 0 {
				continue
			}
			score := len(matched)
			// Strong bump when a file path or dotted invariant/failure-mode ID matched.
			for _, mt := range matched {
				if strings.Contains(mt, "/") || strings.Contains(mt, ".") || strings.Contains(mt, "_") {
					score++
				}
			}
			out = append(out, RawKnowledgeMatch{
				Source:       c.file,
				Kind:         c.kind,
				ID:           id,
				Score:        score,
				MatchedTerms: UniqueStrings(matched),
			})
		}
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

func rawSearchTerms(task string, files []string) []string {
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.Trim(strings.ToLower(s), " \t\n\r,.;()[]{}'\"")
		if len(s) < 3 || rawStopWords[s] || seen[s] {
			return
		}
		seen[s] = true
	}
	for _, t := range rawTokenRE.FindAllString(task, -1) {
		add(t)
		if strings.Contains(t, ".") || strings.Contains(t, "_") || strings.Contains(t, ":") {
			for _, part := range regexp.MustCompile(`[._:/-]+`).Split(t, -1) {
				add(part)
			}
		}
	}
	for _, f := range files {
		f = filepath.ToSlash(f)
		add(f)
		add(filepath.Base(f))
		parts := strings.FieldsFunc(f, func(r rune) bool { return r == '/' || r == '-' || r == '_' || r == '.' })
		for _, part := range parts {
			add(part)
		}
	}
	terms := make([]string, 0, len(seen))
	for t := range seen {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	return terms
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

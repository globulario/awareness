// Package docs extracts architecture decisions and documentation sections from
// Markdown files in docs/ and CLAUDE.md. It parses YAML front matter blocks and
// Markdown headings to create graph nodes and edges.
package docs

import (
	"bufio"
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// FrontMatter is the decoded YAML front matter from a Markdown document.
type FrontMatter struct {
	ID           string   `yaml:"id"`
	Type         string   `yaml:"type"`
	Status       string   `yaml:"status"`
	Summary      string   `yaml:"summary"`
	Invariants   []string `yaml:"invariants"`
	FailureModes []string `yaml:"failure_modes"`
	Symbols      []string `yaml:"symbols"`
	ForbiddenFixes []string `yaml:"forbidden_fixes"`
	Tests        []string `yaml:"tests"`
	Services     []string `yaml:"services"`
	Tags         []string `yaml:"tags"`
}

// parseFrontMatter splits a Markdown document into its YAML front matter and body.
// Returns (nil, body) if no front matter is found.
func parseFrontMatter(src []byte) (*FrontMatter, string, error) {
	lines := splitLines(src)
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return nil, string(src), nil
	}

	// Find closing ---.
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return nil, string(src), nil
	}

	yamlBlock := strings.Join(lines[1:closeIdx], "\n")
	body := strings.Join(lines[closeIdx+1:], "\n")

	var fm FrontMatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return nil, body, err
	}
	return &fm, strings.TrimSpace(body), nil
}

// extractHeadings returns all Markdown headings (level and text) from a document body.
func extractHeadings(body string) []Heading {
	var out []Heading
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") {
			continue
		}
		level := 0
		for _, c := range line {
			if c == '#' {
				level++
			} else {
				break
			}
		}
		text := strings.TrimSpace(line[level:])
		// Strip explicit anchor {#id}.
		anchor := ""
		if idx := strings.Index(text, "{#"); idx > 0 {
			anchor = strings.TrimSuffix(text[idx+2:], "}")
			text = strings.TrimSpace(text[:idx])
		}
		out = append(out, Heading{Level: level, Text: text, Anchor: anchor})
	}
	return out
}

// firstParagraph returns the first non-empty, non-heading paragraph of a body.
func firstParagraph(body string) string {
	var buf bytes.Buffer
	inPara := false
	for _, line := range splitLines([]byte(body)) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if inPara {
				break
			}
			continue
		}
		if trimmed == "" {
			if inPara {
				break
			}
			continue
		}
		inPara = true
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(trimmed)
		if buf.Len() > 300 {
			break
		}
	}
	return buf.String()
}

// splitLines splits bytes into lines without a trailing newline on each.
func splitLines(src []byte) []string {
	raw := strings.Split(string(src), "\n")
	// Normalize \r\n.
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimRight(l, "\r"))
	}
	return out
}

// Heading holds one parsed Markdown heading.
type Heading struct {
	Level  int
	Text   string
	Anchor string // optional {#anchor} id
}

package docs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
)

// Extract walks the given directories and well-known files, parsing Markdown
// documents to create architecture_decision and documentation_section nodes.
// Parse errors in individual files are reported as warnings; the walk continues.
// Returns a slice of warning strings (non-fatal) and a fatal error if any.
func Extract(ctx context.Context, g *graph.Graph, repoRoot string) (warnings []string, err error) {
	// Directories and files to scan.
	scanDirs := []string{
		filepath.Join(repoRoot, "docs", "architecture"),
		filepath.Join(repoRoot, "docs", "awareness"),
		filepath.Join(repoRoot, "docs", "ai"),
		filepath.Join(repoRoot, "docs", "operators"),
		filepath.Join(repoRoot, "docs", "developers"),
	}
	scanFiles := []string{
		filepath.Join(repoRoot, "CLAUDE.md"),
		filepath.Join(repoRoot, "AGENTS.md"),
	}

	process := func(path string, relPath string) {
		w, e := processFile(ctx, g, path, relPath)
		warnings = append(warnings, w...)
		if e != nil {
			warnings = append(warnings, fmt.Sprintf("docs extractor: %s: %v", relPath, e))
		}
	}

	for _, dir := range scanDirs {
		info, statErr := os.Stat(dir)
		if os.IsNotExist(statErr) || (statErr == nil && !info.IsDir()) {
			continue
		}
		if statErr != nil {
			warnings = append(warnings, fmt.Sprintf("docs extractor: stat %s: %v", dir, statErr))
			continue
		}
		walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkE error) error {
			if walkE != nil {
				warnings = append(warnings, fmt.Sprintf("docs extractor: walk %s: %v", path, walkE))
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(repoRoot, path)
			process(path, rel)
			return nil
		})
		if walkErr != nil {
			warnings = append(warnings, fmt.Sprintf("docs extractor: walkdir %s: %v", dir, walkErr))
		}
	}

	for _, f := range scanFiles {
		rel, _ := filepath.Rel(repoRoot, f)
		if _, statErr := os.Stat(f); os.IsNotExist(statErr) {
			continue
		}
		process(f, rel)
	}

	return warnings, nil
}

// processFile parses one Markdown file and upserts nodes/edges into the graph.
func processFile(ctx context.Context, g *graph.Graph, path, relPath string) (warnings []string, err error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fm, body, parseErr := parseFrontMatter(src)
	if parseErr != nil {
		return []string{fmt.Sprintf("parse front matter %s: %v", relPath, parseErr)}, nil
	}

	// Always create a documentation_section node for the file itself.
	fileID := "doc:" + relPath
	fileSummary := firstParagraph(body)
	if fileSummary == "" && fm != nil && fm.Summary != "" {
		fileSummary = fm.Summary
	}
	fileNode := graph.Node{
		ID:      fileID,
		Type:    graph.NodeTypeDocumentationSection,
		Name:    filepath.Base(path),
		Path:    relPath,
		Summary: truncate(fileSummary, 200),
	}
	if err := g.AddNode(ctx, fileNode); err != nil {
		return nil, fmt.Errorf("add doc node %s: %w", fileID, err)
	}

	// If front matter declares an architecture decision, create that node too.
	if fm != nil && fm.ID != "" {
		w := addDecisionNode(ctx, g, fm, relPath, body, fileID)
		warnings = append(warnings, w...)
	}

	// Create documentation_section nodes for top-level headings.
	headings := extractHeadings(body)
	for _, h := range headings {
		if h.Level > 2 {
			continue
		}
		anchor := h.Anchor
		if anchor == "" {
			anchor = headingToAnchor(h.Text)
		}
		secID := fileID + "#" + anchor
		secNode := graph.Node{
			ID:      secID,
			Type:    graph.NodeTypeDocumentationSection,
			Name:    h.Text,
			Path:    relPath + "#" + anchor,
			Summary: h.Text,
		}
		if err := g.AddNode(ctx, secNode); err != nil {
			warnings = append(warnings, fmt.Sprintf("add section node %s: %v", secID, err))
			continue
		}
		// Section is part of the file.
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        fileID,
			Kind:       graph.EdgeOwns,
			Dst:        secID,
			Confidence: 1.0,
		})
	}

	return warnings, nil
}

// addDecisionNode upserts an architecture_decision node from front matter and
// links it to invariants, failure modes, forbidden fixes, symbols, and tests.
func addDecisionNode(ctx context.Context, g *graph.Graph, fm *FrontMatter, relPath, body, docNodeID string) (warnings []string) {
	nodeType := graph.NodeTypeArchitectureDecision
	if fm.Type == "design_rule" {
		nodeType = graph.NodeTypeDesignRule
	} else if fm.Type == "operational_principle" {
		nodeType = graph.NodeTypeOperationalPrinciple
	}

	summary := fm.Summary
	if summary == "" {
		summary = firstParagraph(body)
	}

	decID := "decision:" + fm.ID
	decNode := graph.Node{
		ID:      decID,
		Type:    nodeType,
		Name:    fm.ID,
		Path:    relPath,
		Summary: truncate(summary, 300),
		Metadata: map[string]any{
			"status": fm.Status,
			"tags":   fm.Tags,
		},
	}
	if err := g.AddNode(ctx, decNode); err != nil {
		return []string{fmt.Sprintf("add decision node %s: %v", decID, err)}
	}

	docMeta := map[string]any{
		"source_kind": "documentation",
		"extractor":   "docs",
		"source_file": relPath,
		"explicit":    true,
	}

	// Link decision to its source document.
	_ = g.AddEdge(ctx, graph.Edge{
		Src:        decID,
		Kind:       graph.EdgeDocuments,
		Dst:        docNodeID,
		Confidence: 1.0,
		Metadata:   docMeta,
	})

	// Link to invariants.
	for _, invID := range fm.Invariants {
		invNodeID := findOrSynthesize(ctx, g, graph.NodeTypeInvariant, invID)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        decID,
			Kind:       graph.EdgeExplains,
			Dst:        invNodeID,
			Confidence: 1.0,
			Metadata: map[string]any{
				"source_kind": "documentation",
				"extractor":   "docs",
				"source_file": relPath,
				"explicit":    true,
				"reason":      "front_matter invariants field",
			},
		})
	}

	// Link to failure modes.
	for _, fmID := range fm.FailureModes {
		fmNodeID := findOrSynthesize(ctx, g, graph.NodeTypeFailureMode, fmID)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        decID,
			Kind:       graph.EdgeCausedBy,
			Dst:        fmNodeID,
			Confidence: 1.0,
			Metadata: map[string]any{
				"source_kind": "documentation",
				"extractor":   "docs",
				"source_file": relPath,
				"explicit":    true,
				"reason":      "front_matter failure_modes field",
			},
		})
	}

	// Link to forbidden fixes.
	for _, fixID := range fm.ForbiddenFixes {
		fixNodeID := findOrSynthesize(ctx, g, graph.NodeTypeForbiddenFix, fixID)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        decID,
			Kind:       graph.EdgeForbids,
			Dst:        fixNodeID,
			Confidence: 1.0,
			Metadata: map[string]any{
				"source_kind": "documentation",
				"extractor":   "docs",
				"source_file": relPath,
				"explicit":    true,
				"reason":      "front_matter forbidden_fixes field",
			},
		})
	}

	// Link to symbols (documented_by).
	for _, sym := range fm.Symbols {
		symNodeID := findOrSynthesize(ctx, g, graph.NodeTypeSymbol, sym)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        decID,
			Kind:       graph.EdgeDocuments,
			Dst:        symNodeID,
			Confidence: 0.9,
			Metadata: map[string]any{
				"source_kind": "documentation",
				"extractor":   "docs",
				"source_file": relPath,
				"explicit":    true,
				"reason":      "front_matter symbols field",
			},
		})
	}

	// Link to tests.
	for _, testName := range fm.Tests {
		testNodeID := findOrSynthesize(ctx, g, graph.NodeTypeTest, testName)
		_ = g.AddEdge(ctx, graph.Edge{
			Src:        decID,
			Kind:       graph.EdgeTestedBy,
			Dst:        testNodeID,
			Confidence: 1.0,
			Metadata: map[string]any{
				"source_kind": "documentation",
				"extractor":   "docs",
				"source_file": relPath,
				"explicit":    true,
				"reason":      "front_matter tests field",
			},
		})
	}

	return nil
}

// findOrSynthesize returns the ID of an existing node by name, or creates a
// stub node if none exists. This lets decision edges point to nodes that may
// not yet be in the graph (they'll be populated by other extractors).
//
// Uses EnsureNode (not AddNode) so that if a canonical loader has already
// populated this node's metadata, we don't clobber it. See
// docs/awareness/composed_path_failures.md (lifecycle metadata loss).
func findOrSynthesize(ctx context.Context, g *graph.Graph, nodeType, name string) string {
	existing, _ := g.FindNodeByTypeAndName(ctx, nodeType, name)
	if existing != nil {
		return existing.ID
	}
	stubID := nodeType + ":" + name
	stub := graph.Node{
		ID:      stubID,
		Type:    nodeType,
		Name:    name,
		Summary: "(stub — populated by other extractors)",
	}
	_ = g.EnsureNode(ctx, stub)
	return stubID
}

// headingToAnchor converts a heading text to a GitHub-style anchor.
func headingToAnchor(text string) string {
	text = strings.ToLower(text)
	var b strings.Builder
	for _, r := range text {
		if r == ' ' || r == '-' {
			b.WriteRune('-')
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncate caps a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

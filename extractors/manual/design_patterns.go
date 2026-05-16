package manual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

type designPatternsFile struct {
	Patterns []yamlDesignPattern `yaml:"design_patterns"`
}

type yamlDesignPattern struct {
	ID                  string   `yaml:"id"`
	Title               string   `yaml:"title"`
	Type                string   `yaml:"type"` // "design_pattern" or "anti_pattern"
	Summary             string   `yaml:"summary"`
	AppliesTo           []string `yaml:"applies_to"`
	Invariants          []string `yaml:"invariants"`
	FailureModes        []string `yaml:"failure_modes"`
	ForbiddenFixes      []string `yaml:"forbidden_fixes"`
	CodeSmells          []string `yaml:"code_smells"`
	RequiredTests       []string `yaml:"required_tests"`
	RecommendedSearches []string `yaml:"recommended_searches"`
	Examples            []string `yaml:"examples"`
	SafeFixRule         string   `yaml:"safe_fix_rule"`
}

// LoadDesignPatterns loads docs/awareness/design_patterns.yaml into the graph.
// For each entry it creates a design_pattern or anti_pattern node, code_smell
// nodes, and edges linking to invariants, failure modes, forbidden fixes, tests,
// and applies_to file stubs.
// Missing file is silently skipped.
func LoadDesignPatterns(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadDesignPatterns: read %s: %w", path, err)
	}

	var f designPatternsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadDesignPatterns: parse %s: %w", path, err)
	}

	for _, p := range f.Patterns {
		if p.ID == "" {
			continue
		}
		if err := loadOneDesignPattern(ctx, g, p); err != nil {
			return fmt.Errorf("LoadDesignPatterns: pattern %s: %w", p.ID, err)
		}
	}
	return nil
}

func loadOneDesignPattern(ctx context.Context, g *graph.Graph, p yamlDesignPattern) error {
	nodeType := resolveNodeType(p.Type)
	nodeID := nodeType + ":" + p.ID

	smellsJSON, _ := json.Marshal(p.CodeSmells)
	examplesJSON, _ := json.Marshal(p.Examples)
	searchesJSON, _ := json.Marshal(p.RecommendedSearches)

	if err := g.AddNode(ctx, graph.Node{
		ID:      nodeID,
		Type:    nodeType,
		Name:    p.ID,
		Summary: strings.TrimSpace(p.Summary),
		Metadata: map[string]any{
			"title":                p.Title,
			"type":                 p.Type,
			"safe_fix_rule":        p.SafeFixRule,
			"code_smells":          json.RawMessage(smellsJSON),
			"examples":             json.RawMessage(examplesJSON),
			"recommended_searches": json.RawMessage(searchesJSON),
		},
	}); err != nil {
		return fmt.Errorf("add node: %w", err)
	}

	// Code smell nodes + EdgeSmellsLike edges.
	for _, smell := range p.CodeSmells {
		if smell == "" {
			continue
		}
		smellID := "code_smell:" + sanitizeID(smell)
		if err := g.AddNode(ctx, graph.Node{
			ID:      smellID,
			Type:    graph.NodeTypeCodeSmell,
			Name:    smell,
			Summary: smell,
		}); err != nil {
			return fmt.Errorf("add code_smell node: %w", err)
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  nodeID,
			Kind: graph.EdgeSmellsLike,
			Dst:  smellID,
		}); err != nil {
			return fmt.Errorf("edge smells_like: %w", err)
		}
	}

	// Link to invariants.
	for _, invID := range p.Invariants {
		if invID == "" {
			continue
		}
		invNodeID := "invariant:" + invID
		// EnsureNode: invariant loader owns metadata; we only need an edge target.
		if err := g.EnsureNode(ctx, graph.Node{
			ID:      invNodeID,
			Type:    graph.NodeTypeInvariant,
			Name:    invID,
			Summary: "(stub — populated by invariant loader)",
		}); err != nil {
			return fmt.Errorf("stub invariant %s: %w", invID, err)
		}
		edgeKind := graph.EdgeRequires // design_pattern requires invariant
		if nodeType == graph.NodeTypeAntiPattern {
			edgeKind = graph.EdgeViolates // anti_pattern violates invariant
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:        nodeID,
			Kind:       edgeKind,
			Dst:        invNodeID,
			Confidence: 0.95,
		}); err != nil {
			return fmt.Errorf("edge →invariant %s: %w", invID, err)
		}
	}

	// Link to failure modes.
	for _, fmID := range p.FailureModes {
		if fmID == "" {
			continue
		}
		fmNodeID := "failure_mode:" + fmID
		// Stub the failure_mode node only if it doesn't exist yet. EnsureNode
		// is a no-op when the canonical failure_modes loader has already
		// populated metadata (deprecated, intentional_gap, severity).
		if err := g.EnsureNode(ctx, graph.Node{
			ID:      fmNodeID,
			Type:    graph.NodeTypeFailureMode,
			Name:    fmID,
			Summary: "(stub — populated by failure_mode loader)",
		}); err != nil {
			return fmt.Errorf("stub failure_mode %s: %w", fmID, err)
		}
		edgeKind := graph.EdgeMitigates // design_pattern mitigates failure_mode
		if nodeType == graph.NodeTypeAntiPattern {
			edgeKind = graph.EdgeCausedBy // anti_pattern caused_by failure_mode? No — anti_pattern causes failure_mode
			// The anti-pattern is the cause; use EdgeProduces to express that.
			edgeKind = graph.EdgeProduces
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:        nodeID,
			Kind:       edgeKind,
			Dst:        fmNodeID,
			Confidence: 0.9,
		}); err != nil {
			return fmt.Errorf("edge →failure_mode %s: %w", fmID, err)
		}
	}

	// Link to forbidden fixes.
	for _, ffID := range p.ForbiddenFixes {
		if ffID == "" {
			continue
		}
		ffNodeID := "forbidden_fix:" + ffID
		// EnsureNode: forbidden_fix loader owns metadata.
		if err := g.EnsureNode(ctx, graph.Node{
			ID:      ffNodeID,
			Type:    graph.NodeTypeForbiddenFix,
			Name:    ffID,
			Summary: "(stub — populated by forbidden_fix loader)",
		}); err != nil {
			return fmt.Errorf("stub forbidden_fix %s: %w", ffID, err)
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  nodeID,
			Kind: graph.EdgeForbids,
			Dst:  ffNodeID,
		}); err != nil {
			return fmt.Errorf("edge →forbidden_fix %s: %w", ffID, err)
		}
	}

	// Link to required tests.
	for _, testName := range p.RequiredTests {
		if testName == "" {
			continue
		}
		testNodeID := "test:" + testName
		// EnsureNode: goast/test extractor owns canonical test metadata.
		if err := g.EnsureNode(ctx, graph.Node{
			ID:      testNodeID,
			Type:    graph.NodeTypeTest,
			Name:    testName,
			Summary: "(stub — populated by test extractor)",
		}); err != nil {
			return fmt.Errorf("stub test %s: %w", testName, err)
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:  nodeID,
			Kind: graph.EdgeRequiresTest,
			Dst:  testNodeID,
		}); err != nil {
			return fmt.Errorf("edge →test %s: %w", testName, err)
		}
	}

	// Link to applies_to file/service paths.
	for _, path := range p.AppliesTo {
		if path == "" {
			continue
		}
		fileNodeID := "source_file:" + path
		// EnsureNode: goast extractor owns canonical source_file metadata
		// (path-relative-to-repo, language, line-count, AST hashes).
		if err := g.EnsureNode(ctx, graph.Node{
			ID:      fileNodeID,
			Type:    graph.NodeTypeSourceFile,
			Name:    path,
			Path:    path,
			Summary: "(stub — populated by goast extractor)",
		}); err != nil {
			return fmt.Errorf("stub source_file %s: %w", path, err)
		}
		edgeKind := graph.EdgeImplements // file implements design_pattern
		if nodeType == graph.NodeTypeAntiPattern {
			edgeKind = graph.EdgeTouchesFile // anti_pattern touches file (potential exhibit)
		}
		if err := g.AddEdge(ctx, graph.Edge{
			Src:        nodeID,
			Kind:       edgeKind,
			Dst:        fileNodeID,
			Confidence: 0.7,
		}); err != nil {
			return fmt.Errorf("edge →file %s: %w", path, err)
		}
	}

	return nil
}

// resolveNodeType maps the YAML type field to a graph NodeType constant.
func resolveNodeType(t string) string {
	switch strings.ToLower(t) {
	case "anti_pattern", "antipattern":
		return graph.NodeTypeAntiPattern
	default:
		return graph.NodeTypeDesignPattern
	}
}

// sanitizeID converts a human-readable smell description to a compact ID safe
// for use as a graph node ID (lowercase, spaces replaced with underscores,
// non-alphanumeric chars dropped, truncated to 80 chars).
func sanitizeID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		case r == ' ' || r == '_' || r == '-':
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
		}
	}
	id := b.String()
	if len(id) > 80 {
		id = id[:80]
	}
	return strings.Trim(id, "_")
}

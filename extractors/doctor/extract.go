// Package doctor extracts cluster_doctor diagnostic rules from Go source so
// they appear as first-class graph nodes. Each rule's ID/Category/Scope is
// surfaced as a "detector:" graph node, sharing the canonical node id with
// any failure_modes.yaml entry that lists the same id under detectors:.
//
// The extractor parses Go source — it does NOT import the doctor package.
// Importing would couple awareness build to doctor's transitive deps and
// re-trigger every doctor compile when only YAML changed.
//
// Pattern recognised:
//
//	func (T) ID() string       { return "<id>" }
//	func (T) Category() string { return "<category>" }
//	func (T) Scope() string    { return "<scope>" }
//
// Each receiver type T is one rule. Rules are matched by their (T) receiver
// across the three methods. Anything that doesn't match the pattern is
// silently skipped — the extractor must be tolerant because doctor rules
// evolve faster than awareness extractors.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

// detectorMappingFile is the on-disk shape of detector_mapping.yaml.
type detectorMappingFile struct {
	Mappings []detectorMapping `yaml:"detector_mappings"`
}

type detectorMapping struct {
	FailureMode string   `yaml:"failure_mode"`
	Detectors   []string `yaml:"detectors"`
	Reason      string   `yaml:"reason"`
}

// Rule is the extracted shape of a single cluster_doctor rule.
type Rule struct {
	ID       string
	Category string
	Scope    string
	Receiver string // Go type name
	File     string // path relative to repoRoot
}

// ExtractResult summarises what the doctor extractor produced.
type ExtractResult struct {
	Rules            []Rule
	MappingsApplied  int
	MappingsSkipped  []string // mappings that referenced unknown rule IDs (logged for visibility)
}

// Extract walks rulesDir for *.go files (skipping _test.go) and emits one
// "detector:<id>" graph node per rule it finds. If mappingPath points to a
// detector_mapping.yaml, it also emits "detector:<id> --matches_failure_mode-->
// failure_mode:<fm_id>" edges. mappingPath="" disables the mapping pass.
//
// rulesDir is typically <repoRoot>/golang/cluster_doctor/cluster_doctor_server/rules.
// repoRoot is used to record File as a repo-relative path.
func Extract(ctx context.Context, g *graph.Graph, rulesDir, repoRoot, mappingPath string) (ExtractResult, error) {
	var result ExtractResult
	rules, err := scanRules(rulesDir, repoRoot)
	if err != nil {
		return result, err
	}
	result.Rules = rules

	knownRule := make(map[string]bool, len(rules))
	for _, r := range rules {
		knownRule[r.ID] = true
		nodeID := "detector:" + r.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      nodeID,
			Type:    graph.NodeTypeDoctorEvidence,
			Name:    r.ID,
			Path:    r.File,
			Summary: fmt.Sprintf("doctor rule %s (category=%s, scope=%s)", r.ID, r.Category, r.Scope),
			Metadata: map[string]any{
				"kind":     "doctor_rule",
				"category": r.Category,
				"scope":    r.Scope,
				"receiver": r.Receiver,
			},
		}); err != nil {
			return result, fmt.Errorf("doctor extractor: AddNode %s: %w", nodeID, err)
		}
	}

	if mappingPath != "" {
		mappings, err := loadDetectorMapping(mappingPath)
		if err != nil {
			return result, fmt.Errorf("doctor extractor: load mapping %s: %w", mappingPath, err)
		}
		for _, m := range mappings {
			if m.FailureMode == "" || len(m.Detectors) == 0 {
				continue
			}
			fmNodeID := "failure_mode:" + m.FailureMode
			for _, d := range m.Detectors {
				if !knownRule[d] {
					// Mapping points at a doctor rule that doesn't exist in
					// the source. Surface for awareness CI rather than
					// silently dropping it.
					result.MappingsSkipped = append(result.MappingsSkipped,
						fmt.Sprintf("%s -> %s (rule not found in source)", m.FailureMode, d))
					continue
				}
				edge := graph.Edge{
					Src:  "detector:" + d,
					Kind: graph.EdgeMatchesFailureMode,
					Dst:  fmNodeID,
					Metadata: map[string]any{
						"source": "detector_mapping.yaml",
						"reason": m.Reason,
					},
				}
				if err := g.AddEdge(ctx, edge); err != nil {
					return result, fmt.Errorf("doctor extractor: AddEdge %s -> %s: %w",
						edge.Src, edge.Dst, err)
				}
				result.MappingsApplied++
			}
		}
	}

	return result, nil
}

// loadDetectorMapping reads and parses detector_mapping.yaml. A missing file
// is not an error — it just means no mappings.
func loadDetectorMapping(path string) ([]detectorMapping, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f detectorMappingFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return f.Mappings, nil
}

// scanRules walks rulesDir and parses each .go file for the (ID/Category/Scope)
// triple defined on the same receiver type. Tolerant: missing methods just
// mean the rule is not surfaced.
func scanRules(rulesDir, repoRoot string) ([]Rule, error) {
	if _, err := os.Stat(rulesDir); err != nil {
		// No rules dir is a no-op, not an error — keeps awareness build green
		// when the doctor package layout shifts.
		return nil, nil
	}
	fset := token.NewFileSet()
	type partial struct {
		ID, Category, Scope string
		File                string
	}
	byReceiver := map[string]*partial{}

	walkErr := filepath.WalkDir(rulesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return nil // skip unparseable files; tolerant by design
		}
		rel, _ := filepath.Rel(repoRoot, path)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			recv := receiverTypeName(fn.Recv.List[0].Type)
			if recv == "" {
				continue
			}
			method := fn.Name.Name
			if method != "ID" && method != "Category" && method != "Scope" {
				continue
			}
			val := singleStringReturn(fn)
			if val == "" {
				continue
			}
			p, ok := byReceiver[recv]
			if !ok {
				p = &partial{File: rel}
				byReceiver[recv] = p
			}
			switch method {
			case "ID":
				p.ID = val
			case "Category":
				p.Category = val
			case "Scope":
				p.Scope = val
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	var out []Rule
	for recv, p := range byReceiver {
		// A rule must at least declare an ID. Category/Scope are nice-to-have.
		if p.ID == "" {
			continue
		}
		out = append(out, Rule{
			ID:       p.ID,
			Category: p.Category,
			Scope:    p.Scope,
			Receiver: recv,
			File:     p.File,
		})
	}
	return out, nil
}

// receiverTypeName returns the underlying type name for a receiver expression,
// stripping pointer wrapping. For (T) → "T"; for (*T) → "T".
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// singleStringReturn returns the literal string value if the function body is
// exactly `return "literal"`. Returns "" for anything more complex.
func singleStringReturn(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return ""
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	// strip quotes
	return strings.Trim(lit.Value, `"`)
}

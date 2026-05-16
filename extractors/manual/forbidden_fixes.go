package manual

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
)

type forbiddenFixesFile struct {
	ForbiddenFixes []yamlForbiddenFix `yaml:"forbidden_fixes"`
}

type yamlForbiddenFix struct {
	ID                string   `yaml:"id"`
	Summary           string   `yaml:"summary"`
	RelatedInvariants []string `yaml:"related_invariants"`
	RequiredTests     []string `yaml:"required_tests"`
}

// LoadForbiddenFixes loads forbidden_fixes.yaml into the graph.
// Missing files are silently skipped.
func LoadForbiddenFixes(ctx context.Context, g *graph.Graph, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("LoadForbiddenFixes: read %s: %w", path, err)
	}

	var f forbiddenFixesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("LoadForbiddenFixes: parse %s: %w", path, err)
	}

	for _, ff := range f.ForbiddenFixes {
		if ff.ID == "" {
			continue
		}
		fixID := "forbidden_fix:" + ff.ID
		if err := g.AddNode(ctx, graph.Node{
			ID:      fixID,
			Type:    graph.NodeTypeForbiddenFix,
			Name:    ff.ID,
			Summary: ff.Summary,
		}); err != nil {
			return err
		}

		// Ensure invariant -> forbids -> forbidden_fix edges exist.
		for _, inv := range ff.RelatedInvariants {
			if inv == "" {
				continue
			}
			invID := "invariant:" + inv
			if err := g.AddNode(ctx, graph.Node{
				ID:   invID,
				Type: graph.NodeTypeInvariant,
				Name: inv,
			}); err != nil {
				return err
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  invID,
				Kind: graph.EdgeForbids,
				Dst:  fixID,
			}); err != nil {
				return err
			}
		}

		// Link required tests to the forbidden fix as trace evidence.
		for _, test := range ff.RequiredTests {
			if test == "" {
				continue
			}
			testID := "test:" + test
			if err := g.AddNode(ctx, graph.Node{
				ID:   testID,
				Type: graph.NodeTypeTest,
				Name: test,
			}); err != nil {
				return err
			}
			if err := g.AddEdge(ctx, graph.Edge{
				Src:  fixID,
				Kind: graph.EdgeTestedBy,
				Dst:  testID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

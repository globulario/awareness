package failuregraph

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// rePlaceholder detects angle-bracket placeholders like <ip>, <version>.
var rePlaceholder = regexp.MustCompile(`<[a-z_]+>`)

//go:embed seeds/*.yaml
var seedFiles embed.FS

// SeedDefaults loads all bundled seed YAML files and upserts them into the store.
// Safe to call repeatedly — all operations are upserts.
func SeedDefaults(ctx context.Context, s *Store) (int, error) {
	entries, err := fs.ReadDir(seedFiles, "seeds")
	if err != nil {
		return 0, fmt.Errorf("failuregraph seed: read dir: %w", err)
	}
	seeded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := seedFiles.ReadFile("seeds/" + e.Name())
		if err != nil {
			return seeded, fmt.Errorf("failuregraph seed: read %s: %w", e.Name(), err)
		}
		if err := seedCategory(ctx, s, data); err != nil {
			return seeded, fmt.Errorf("failuregraph seed: %s: %w", e.Name(), err)
		}
		seeded++
	}
	return seeded, nil
}

// SeedFromYAML seeds a single failure category from raw YAML bytes.
func SeedFromYAML(ctx context.Context, s *Store, data []byte) error {
	return seedCategory(ctx, s, data)
}

func seedCategory(ctx context.Context, s *Store, data []byte) error {
	var seed CategorySeed
	if err := yaml.Unmarshal(data, &seed); err != nil {
		return fmt.Errorf("yaml unmarshal: %w", err)
	}
	if seed.ID == "" || seed.Name == "" {
		return fmt.Errorf("seed missing id or name")
	}

	// Upsert the category node.
	catNode := FailureNode{
		ID:       seed.ID,
		NodeType: NodeTypeErrorCategory,
		Name:     seed.Name,
		Summary:  seed.Summary,
		Severity: seed.Severity,
		Status:   StatusActive,
	}
	if catNode.Severity == "" {
		catNode.Severity = "warning"
	}
	if _, err := s.RecordFailureNode(ctx, catNode); err != nil {
		return err
	}

	// Error signatures
	for _, sig := range seed.Signatures {
		if sig == "" {
			continue
		}
		errSig := ErrorSignature{
			Signature:           sig,
			NormalizedSignature: NormalizeErrorSignature(sig),
			CategoryID:          seed.ID,
			Severity:            seed.Severity,
			Sample:              sig,
			MatcherKind:         MatcherKindExact,
		}
		switch {
		case strings.ContainsAny(sig, ".*+?[](){}\\|^$"):
			errSig.MatcherKind = MatcherKindRegex
			errSig.MatcherPattern = sig
		case rePlaceholder.MatchString(sig):
			// Template signature: extract non-placeholder words as keywords.
			clean := rePlaceholder.ReplaceAllString(sig, " ")
			words := strings.Fields(clean)
			errSig.MatcherKind = MatcherKindKeyword
			errSig.MatcherPattern = strings.Join(words, " ")
		}
		if _, err := s.RecordErrorSignature(ctx, errSig); err != nil {
			return err
		}
	}

	linkItems := func(items []SeedItem, nodeType, edgeType string) error {
		for _, item := range items {
			if item.ID == "" {
				continue
			}
			n := FailureNode{
				ID:       item.ID,
				NodeType: nodeType,
				Name:     item.ID,
				Summary:  item.Summary,
				Status:   StatusActive,
			}
			if _, err := s.RecordFailureNode(ctx, n); err != nil {
				return err
			}
			edge := FailureEdge{
				FromID:     seed.ID,
				ToID:       item.ID,
				EdgeType:   edgeType,
				Confidence: ConfidenceHigh,
				Evidence:   "seed",
				Source:     "seed_defaults",
			}
			if _, err := s.RecordFailureEdge(ctx, edge); err != nil {
				return err
			}
		}
		return nil
	}

	for _, fn := range []struct {
		items    []SeedItem
		nodeType string
		edgeType string
	}{
		{seed.Symptoms, NodeTypeSymptom, EdgeObservedAs},
		{seed.Causes, NodeTypeRootCause, EdgeCommonlyCausedBy},
		{seed.WrongFixes, NodeTypeWrongFix, EdgeAvoidFix},
		{seed.Tests, NodeTypeRegressionTest, EdgeClosureRequires},
	} {
		if err := linkItems(fn.items, fn.nodeType, fn.edgeType); err != nil {
			return err
		}
	}

	// Resolutions link via cause → fixed_by.
	// When we have causes, resolutions hang off the first cause.
	// When no causes, they hang off the category itself.
	parentID := seed.ID
	if len(seed.Causes) > 0 {
		parentID = seed.Causes[0].ID
	}
	for _, res := range seed.Resolutions {
		if res.ID == "" {
			continue
		}
		n := FailureNode{
			ID:       res.ID,
			NodeType: NodeTypeResolution,
			Name:     res.ID,
			Summary:  res.Summary,
			Status:   StatusActive,
		}
		if _, err := s.RecordFailureNode(ctx, n); err != nil {
			return err
		}
		edge := FailureEdge{
			FromID:     parentID,
			ToID:       res.ID,
			EdgeType:   EdgeFixedBy,
			Confidence: ConfidenceHigh,
			Evidence:   "seed",
			Source:     "seed_defaults",
		}
		if _, err := s.RecordFailureEdge(ctx, edge); err != nil {
			return err
		}
	}

	return nil
}

package learning

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ContextAliasMap maps a target ID (e.g. "infra.desired_hash_consistency")
// to a list of natural-language alias strings.
type ContextAliasMap map[string][]string

// LoadContextAliases reads docs/awareness/context_aliases.yaml and returns
// the alias map. Returns an empty map if the file does not exist.
func LoadContextAliases(path string) (ContextAliasMap, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ContextAliasMap{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LoadContextAliases %s: %w", path, err)
	}
	var f struct {
		Aliases ContextAliasMap `yaml:"aliases"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("LoadContextAliases parse %s: %w", path, err)
	}
	if f.Aliases == nil {
		return ContextAliasMap{}, nil
	}
	return f.Aliases, nil
}

// MatchAliasTargets returns the list of target IDs whose aliases match any
// phrase in the task string. Matching is case-insensitive substring matching.
// The task string is tested against the raw alias phrases (not tokenised).
//
// Target keys in the alias YAML may carry a type prefix to indicate what kind
// of graph node they refer to:
//
//	"invariant:foo"    → invariant node
//	"failure_mode:foo" → failure mode node
//	"service:foo"      → service node
//	"foo"              → backward-compat bare ID (invariant first)
//
// The returned IDs preserve any prefix present in the YAML key.
func MatchAliasTargets(task string, aliases ContextAliasMap) []string {
	lower := strings.ToLower(task)
	seen := make(map[string]bool)
	var matched []string

	for targetID, phrases := range aliases {
		if seen[targetID] {
			continue
		}
		for _, phrase := range phrases {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				seen[targetID] = true
				matched = append(matched, targetID)
				break
			}
		}
	}
	return matched
}

// AliasTargetKind classifies the kind of graph node a (possibly-prefixed) alias
// target ID refers to. Returns one of "invariant", "failure_mode", "service",
// or "invariant" as the default for bare IDs (backward compat).
func AliasTargetKind(targetID string) (kind, bareID string) {
	for _, prefix := range []string{"invariant:", "failure_mode:", "service:"} {
		if strings.HasPrefix(targetID, prefix) {
			return strings.TrimSuffix(prefix, ":"), strings.TrimPrefix(targetID, prefix)
		}
	}
	// Bare ID — backward compat: treat as invariant.
	return "invariant", targetID
}

// LearningRule is a single entry from learning_rules.yaml.
type LearningRule struct {
	ID      string `yaml:"id"`
	Summary string `yaml:"summary"`
}

// LoadLearningRules reads docs/awareness/learning_rules.yaml.
// Returns an empty slice if the file does not exist.
func LoadLearningRules(path string) ([]LearningRule, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("LoadLearningRules %s: %w", path, err)
	}
	var f struct {
		Rules []LearningRule `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("LoadLearningRules parse %s: %w", path, err)
	}
	return f.Rules, nil
}

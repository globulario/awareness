package learning_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/learning"
)

func TestLoadContextAliasesFromYAML(t *testing.T) {
	dir := t.TempDir()
	content := `aliases:
  infra.desired_hash_consistency:
    - desired hash mismatch
    - drift loop
  service.restart_singleflight:
    - restart storm
    - SIGTERM storm
`
	path := filepath.Join(dir, "context_aliases.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	aliases, err := learning.LoadContextAliases(path)
	if err != nil {
		t.Fatalf("LoadContextAliases: %v", err)
	}
	if len(aliases) != 2 {
		t.Errorf("expected 2 alias groups, got %d", len(aliases))
	}
	if len(aliases["infra.desired_hash_consistency"]) != 2 {
		t.Errorf("expected 2 aliases for infra.desired_hash_consistency, got %d",
			len(aliases["infra.desired_hash_consistency"]))
	}
}

func TestLoadContextAliasesMissingFileReturnsEmpty(t *testing.T) {
	aliases, err := learning.LoadContextAliases("/nonexistent/path/context_aliases.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(aliases))
	}
}

func TestMatchAliasTargetsFindsHashMismatch(t *testing.T) {
	aliases := learning.ContextAliasMap{
		"infra.desired_hash_consistency": {"desired hash mismatch", "drift loop"},
		"service.restart_singleflight":   {"restart storm", "SIGTERM storm"},
	}

	task := "fix catalog tab status 0 after envoy restart storm"
	matched := learning.MatchAliasTargets(task, aliases)

	found := false
	for _, m := range matched {
		if m == "service.restart_singleflight" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected service.restart_singleflight to be matched for task %q, got %v", task, matched)
	}
}

func TestMatchAliasTargetsCaseInsensitive(t *testing.T) {
	aliases := learning.ContextAliasMap{
		"infra.desired_hash_consistency": {"ComputeInfrastructureDesiredHash"},
	}

	task := "fix computeinfrastructuredesiredhash mismatch"
	matched := learning.MatchAliasTargets(task, aliases)

	found := false
	for _, m := range matched {
		if m == "infra.desired_hash_consistency" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected case-insensitive match, got %v", matched)
	}
}

func TestMatchAliasTargetsEnvoyFixtureTask(t *testing.T) {
	// Load the real context_aliases.yaml fixture.
	aliases, err := learning.LoadContextAliases("../../../docs/awareness/context_aliases.yaml")
	if err != nil {
		t.Fatalf("LoadContextAliases: %v", err)
	}

	task := "fix catalog tabs status 0 after envoy restart storm caused by desired hash mismatch"
	matched := learning.MatchAliasTargets(task, aliases)

	matchedSet := make(map[string]bool)
	for _, m := range matched {
		matchedSet[m] = true
	}

	expectedMatches := []string{
		"infra.desired_hash_consistency",
		"service.restart_singleflight",
	}
	for _, expected := range expectedMatches {
		if !matchedSet[expected] {
			t.Errorf("expected %q to be matched in task %q, got %v", expected, task, matched)
		}
	}
}

func TestLoadLearningRules(t *testing.T) {
	rules, err := learning.LoadLearningRules("../../../docs/awareness/learning_rules.yaml")
	if err != nil {
		t.Fatalf("LoadLearningRules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("expected at least one learning rule")
	}
	for _, r := range rules {
		if r.ID == "" {
			t.Error("learning rule missing id")
		}
		if r.Summary == "" {
			t.Error("learning rule missing summary")
		}
	}
}

func TestLoadLearningRulesMissingFileReturnsNil(t *testing.T) {
	rules, err := learning.LoadLearningRules("/nonexistent/learning_rules.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if rules != nil {
		t.Error("expected nil for missing file")
	}
}

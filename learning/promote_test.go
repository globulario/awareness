package learning_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/learning"
)

// copyAwarenessDir copies the real docs/awareness directory into a temp dir
// so promotion tests have a realistic YAML baseline to merge into.
func setupAwarenessDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Copy the four approved YAML files into the temp dir.
	realDir := "../../../docs/awareness"
	for _, name := range []string{"invariants.yaml", "failure_modes.yaml", "forbidden_fixes.yaml"} {
		src, err := os.ReadFile(filepath.Join(realDir, name))
		if err != nil {
			t.Logf("warning: could not copy %s: %v (using empty file)", name, err)
			_ = os.WriteFile(filepath.Join(dir, name), nil, 0o644)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatalf("copy %s: %v", name, err)
		}
	}

	// Write a minimal context_aliases.yaml.
	aliasContent := "aliases:\n  convergence.no_infinite_retry:\n    - retry loop\n"
	_ = os.WriteFile(filepath.Join(dir, "context_aliases.yaml"), []byte(aliasContent), 0o644)

	// Create proposals sub-directory.
	_ = os.MkdirAll(filepath.Join(dir, "proposals"), 0o755)

	return dir
}

func validateAndGetResult(t *testing.T, p *learning.ProposalSpec) *learning.ProposalValidationResult {
	t.Helper()
	ctx := context.Background()
	g := seedValidateGraph(t)

	// Add services the fixture references.
	for _, svc := range []string{"envoy", "xds", "cluster-controller", "workflow-service", "node-agent"} {
		_ = g.AddNode(ctx, graph.Node{ID: "service:" + svc, Type: graph.NodeTypeGlobularService, Name: svc})
	}
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "runtime.installed_state_not_liveness",
		Severity: "high",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:runtime.installed_state_not_liveness", Type: graph.NodeTypeInvariant, Name: "runtime.installed_state_not_liveness"})

	vr, err := learning.ValidateProposal(ctx, p, g)
	if err != nil {
		t.Fatalf("ValidateProposal: %v", err)
	}
	return vr
}

func TestPromoteProposalWritesToApprovedFiles(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	vr := validateAndGetResult(t, p)

	if vr.Status != learning.ValidationPass {
		t.Fatalf("proposal must pass validation before promotion")
	}

	result, err := learning.PromoteProposal(ctx, p, vr, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true})
	if err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}

	// At least one category must write something (invariants may already exist from prior
	// promotes — that is correct idempotent behaviour; failure modes and forbidden fixes
	// are always new for this fixture).
	if len(result.InvariantsAdded) == 0 && len(result.FailureModesAdded) == 0 && len(result.ForbiddenFixesAdded) == 0 {
		t.Error("expected at least invariants, failure modes, or forbidden fixes to be written during promotion")
	}

	// Verify failure modes were added.
	if len(result.FailureModesAdded) == 0 {
		t.Error("expected failure modes to be added during promotion")
	}

	// Read the updated invariants.yaml and check the new invariants are present.
	data, err := os.ReadFile(filepath.Join(docsDir, "invariants.yaml"))
	if err != nil {
		t.Fatalf("read invariants.yaml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "infra.desired_hash_consistency") {
		t.Error("infra.desired_hash_consistency not found in promoted invariants.yaml")
	}
	if !strings.Contains(content, "service.restart_singleflight") {
		t.Error("service.restart_singleflight not found in promoted invariants.yaml")
	}
}

func TestPromoteProposalDoesNotOverwriteExistingInvariants(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	// Read existing invariants before promotion.
	beforeData, err := os.ReadFile(filepath.Join(docsDir, "invariants.yaml"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	var beforeFile struct {
		Invariants []struct{ ID string `yaml:"id"` } `yaml:"invariants"`
	}
	_ = yaml.Unmarshal(beforeData, &beforeFile)
	existingCount := len(beforeFile.Invariants)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	vr := validateAndGetResult(t, p)

	if vr.Status != learning.ValidationPass {
		t.Fatalf("proposal must pass validation")
	}

	if _, err := learning.PromoteProposal(ctx, p, vr, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true}); err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}

	// Read after promotion.
	afterData, err := os.ReadFile(filepath.Join(docsDir, "invariants.yaml"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	var afterFile struct {
		Invariants []struct{ ID string `yaml:"id"` } `yaml:"invariants"`
	}
	_ = yaml.Unmarshal(afterData, &afterFile)
	afterCount := len(afterFile.Invariants)

	// afterCount must not be less than existingCount (no invariants must be lost).
	// It may stay the same if all proposed invariants already existed — that is correct
	// idempotent behaviour for invariants that were established before promotion ran.
	if afterCount < existingCount {
		t.Errorf("invariants must not be lost after promotion: had %d, now %d", existingCount, afterCount)
	}

	// Re-run promotion — idempotent (no doubles).
	if _, err := learning.PromoteProposal(ctx, p, vr, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true}); err != nil {
		t.Fatalf("second PromoteProposal: %v", err)
	}

	idempotentData, err := os.ReadFile(filepath.Join(docsDir, "invariants.yaml"))
	if err != nil {
		t.Fatalf("read idempotent: %v", err)
	}
	var idempotentFile struct {
		Invariants []struct{ ID string `yaml:"id"` } `yaml:"invariants"`
	}
	_ = yaml.Unmarshal(idempotentData, &idempotentFile)
	if len(idempotentFile.Invariants) != afterCount {
		t.Errorf("promotion is not idempotent: %d vs %d invariants", len(idempotentFile.Invariants), afterCount)
	}
}

func TestPromoteProposalRequiresValidatedResult(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)

	// Pass nil validation result — must fail.
	_, err = learning.PromoteProposal(ctx, p, nil, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true})
	if err == nil {
		t.Error("expected error when promoting without validated result")
	}

	// Pass a FAIL validation result — must fail.
	_, err = learning.PromoteProposal(ctx, p, &learning.ProposalValidationResult{Status: learning.ValidationFail}, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true})
	if err == nil {
		t.Error("expected error when promoting with FAIL validation result")
	}
}

func TestPromotedAliasesWrittenToContextAliasesYAML(t *testing.T) {
	ctx := context.Background()
	docsDir := setupAwarenessDir(t)

	b, err := learning.LoadIncidentBundle(fixtureEnvoy)
	if err != nil {
		t.Fatalf("LoadIncidentBundle: %v", err)
	}
	p := learning.GenerateProposalFromBundle(b)
	vr := validateAndGetResult(t, p)

	if vr.Status != learning.ValidationPass {
		t.Fatalf("proposal must pass validation")
	}

	result, err := learning.PromoteProposal(ctx, p, vr, docsDir, nil, learning.PromoteOptions{AllowUnapproved: true})
	if err != nil {
		t.Fatalf("PromoteProposal: %v", err)
	}

	if result.AliasesAdded == 0 {
		t.Error("expected aliases to be written during promotion")
	}

	data, err := os.ReadFile(filepath.Join(docsDir, "context_aliases.yaml"))
	if err != nil {
		t.Fatalf("read context_aliases.yaml: %v", err)
	}
	if !strings.Contains(string(data), "infra.desired_hash_consistency") {
		t.Error("infra.desired_hash_consistency aliases not written to context_aliases.yaml")
	}
}

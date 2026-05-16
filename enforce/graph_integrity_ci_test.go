package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
)

// TestGraphIntegrityCI_MissingRequiredTestFails verifies that an invariant with
// a required test that has no path to a real implementation causes CI to fail.
func TestGraphIntegrityCI_MissingRequiredTestFails(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Add an invariant with a required_test edge pointing to a non-existent test.
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:ci.missing_test", Type: graph.NodeTypeInvariant, Name: "ci.missing_test"})
	_ = g.UpsertInvariant(ctx, graph.Invariant{ID: "ci.missing_test", Title: "ci.missing_test", Severity: "high", Status: "active"})
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestCIMissingTest", Type: graph.NodeTypeTest, Name: "TestCIMissingTest",
		Metadata: map[string]any{"required": true}})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:ci.missing_test", Kind: graph.EdgeTestedBy, Dst: "test:TestCIMissingTest"})

	res := enforce.GraphIntegrityCICheck(ctx, g, enforce.CICheckOptions{})

	if res.Pass {
		t.Error("CI check passed but required test has no path — should fail")
	}
	if res.ErrorCount == 0 {
		t.Error("expected error count > 0 for missing required test path")
	}
}

// TestGraphIntegrityCI_SkippedRequiredTestFails verifies that a DONE fix case
// verified only by scaffold TODO stubs causes CI to fail.
func TestGraphIntegrityCI_SkippedRequiredTestFails(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a fix_cases.yaml with a DONE fix case.
	docsDir := tmpDir
	fixCasesYAML := `fix_cases:
  - id: ci.test.done_fixcase
    status: DONE
    required_tests:
      - TestCIDoneFixScaffold
`
	if err := os.WriteFile(filepath.Join(docsDir, "fix_cases.yaml"), []byte(fixCasesYAML), 0o644); err != nil {
		t.Fatalf("write fix_cases.yaml: %v", err)
	}

	// Write a test file with a scaffold TODO skip for that test.
	repoDir := t.TempDir()
	testContent := `package foo_test

import "testing"

func TestCIDoneFixScaffold(t *testing.T) {
	t.Skip("TODO: implement required awareness test")
}
`
	if err := os.MkdirAll(filepath.Join(repoDir, "foo"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "foo", "fix_test.go"), []byte(testContent), 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}

	ctx := context.Background()
	res := enforce.GraphIntegrityCICheck(ctx, nil, enforce.CICheckOptions{
		RepoRoot: repoDir,
		DocsDir:  docsDir,
	})

	// The DONE fixcase verified only by scaffold stubs should cause an error.
	foundDone := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeDoneFixcaseScaffoldOnly {
			foundDone = true
		}
	}
	if !foundDone {
		t.Error("expected DONE_FIXCASE_SCAFFOLD_ONLY finding for scaffold-only DONE fix case")
	}
}

// TestGraphIntegrityCI_InvariantWithoutImplementationFails verifies that an
// active invariant without any implementation evidence causes CI to fail.
// (CodeInvariantNoImplementation is escalated to SeverityError in CI mode.)
func TestGraphIntegrityCI_InvariantWithoutImplementationFails(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Add an invariant with no implementing source file.
	_ = g.AddNode(ctx, graph.Node{ID: "invariant:ci.no_impl", Type: graph.NodeTypeInvariant, Name: "ci.no_impl"})
	_ = g.UpsertInvariant(ctx, graph.Invariant{ID: "ci.no_impl", Title: "ci.no_impl", Severity: "high", Status: "active"})

	res := enforce.GraphIntegrityCICheck(ctx, g, enforce.CICheckOptions{})

	if res.Pass {
		t.Error("CI check passed but invariant has no implementation — should fail in CI mode")
	}
	// Verify the specific code is present as an error (escalated from warning).
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantNoImplementation && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Error("expected INVARIANT_NO_IMPLEMENTATION escalated to SeverityError in CI mode")
	}
}

// TestGraphIntegrityCI_MissingForbiddenFixFails verifies that an invariant
// without a forbidden_fix defined is escalated to SeverityWarning in CI mode.
// Plain warnings do not fail CI by default but are reported.
func TestGraphIntegrityCI_MissingForbiddenFixFails(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Add a fully-implemented invariant (has impl, test, failure mode) but no forbidden_fix.
	addBasicInvariant(t, g, "ci.no_forbidden_fix")
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:pkg/fix.go", Type: graph.NodeTypeSourceFile})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:pkg/fix.go", Kind: graph.EdgeImplements, Dst: "invariant:ci.no_forbidden_fix"})
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestCINoFix", Type: graph.NodeTypeTest})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:ci.no_forbidden_fix", Kind: graph.EdgeTestedBy, Dst: "test:TestCINoFix"})

	res := enforce.GraphIntegrityCICheck(ctx, g, enforce.CICheckOptions{})

	// INVARIANT_NO_FORBIDDEN_FIX should be escalated to Warning (from Info).
	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantNoForbiddenFix && f.Severity == enforce.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected INVARIANT_NO_FORBIDDEN_FIX escalated to SeverityWarning in CI mode")
	}
	// Warnings do not fail CI — Pass should still depend on error count only.
	// (This invariant has no implementation node in the strict sense — it does have
	// implements edge, so CodeInvariantNoImplementation won't fire. Only the forbidden_fix
	// warning should be present here.)
	if res.WarningCount == 0 {
		t.Error("expected WarningCount > 0 for missing forbidden_fix")
	}
}

// TestGraphIntegrityCI_StrictVerifiedMissingProofFails verifies that an
// annotation with a required test that is strictly_verified but backed by a
// missing test node is caught. Uses InvariantShapeCheck's unverified-impl path.
func TestGraphIntegrityCI_StrictVerifiedMissingProofFails(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Add an invariant with an implementing file that has no verified test.
	addBasicInvariant(t, g, "ci.unverified_impl")
	// Add implements edge but no verifies/tested_by edge.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:pkg/unverified.go", Type: graph.NodeTypeSourceFile})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:pkg/unverified.go", Kind: graph.EdgeImplements, Dst: "invariant:ci.unverified_impl"})
	// No tested_by or verifies edge — CodeInvariantNoTestCoverage fires.

	res := enforce.GraphIntegrityCICheck(ctx, g, enforce.CICheckOptions{})

	found := false
	for _, f := range res.Findings {
		if f.Code == enforce.CodeInvariantNoTestCoverage {
			found = true
		}
	}
	if !found {
		t.Error("expected INVARIANT_NO_TEST_COVERAGE finding for unverified implementation")
	}
}

// TestGraphIntegrityCI_WarningsDoNotFail verifies that plain warnings (not
// escalated codes) do not cause the CI check to fail.
func TestGraphIntegrityCI_WarningsDoNotFail(t *testing.T) {
	g := openCrossLayerGraph(t)
	ctx := context.Background()

	// Add a fully-wired invariant that should produce only Info findings (no_failure_mode
	// and no_forbidden_fix escalated to Warning), not errors.
	addBasicInvariant(t, g, "ci.warning_only")

	// Wire implementation + test.
	_ = g.AddNode(ctx, graph.Node{ID: "source_file:pkg/warn.go", Type: graph.NodeTypeSourceFile})
	_ = g.AddEdge(ctx, graph.Edge{Src: "source_file:pkg/warn.go", Kind: graph.EdgeImplements, Dst: "invariant:ci.warning_only"})
	_ = g.AddNode(ctx, graph.Node{ID: "test:TestCIWarningOnly", Type: graph.NodeTypeTest})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:ci.warning_only", Kind: graph.EdgeTestedBy, Dst: "test:TestCIWarningOnly"})
	// Wire failure mode (so no_failure_mode doesn't fire).
	_ = g.AddNode(ctx, graph.Node{ID: "failure_mode:ci_fm", Type: graph.NodeTypeFailureMode})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:ci.warning_only", Kind: graph.EdgeAffects, Dst: "failure_mode:ci_fm"})
	// Wire authority to avoid missing_authority warning.
	_ = g.AddNode(ctx, graph.Node{ID: "authority:etcd", Type: "authority"})
	_ = g.AddEdge(ctx, graph.Edge{Src: "invariant:ci.warning_only", Kind: graph.EdgeReadsAuthority, Dst: "authority:etcd"})
	// Still no forbidden_fix — this produces Warning, not Error.

	// MaxRequiredTestNoPath=100 — this test is about shape warnings, not test-path resolution.
	res := enforce.GraphIntegrityCICheck(ctx, g, enforce.CICheckOptions{MaxRequiredTestNoPath: 100})

	// There may be warnings (no_forbidden_fix escalated to warning), but errors should be 0.
	if res.ErrorCount > 0 {
		t.Errorf("expected no errors for warning-only invariant, got %d errors: %v", res.ErrorCount, res.FailureReasons)
	}
	// Warnings are acceptable — just verify they're present and don't break CI.
	t.Logf("warning count: %d (expected, warnings don't fail CI)", res.WarningCount)
}

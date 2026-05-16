package enforce_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
)

// Test: Audit with no errors passes.
func TestAuditPassesOnCleanSource(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "clean.go"), []byte(`package pkg

//globular:enforces infra.hash_consistency
//globular:state_transition DESIRED -> INSTALLED
//globular:tested_by TestHashConsistency
func ComputeHash() {}
`), 0o644)

	ctx := context.Background()
	result := enforce.Audit(ctx, nil, enforce.AuditOptions{
		SrcDir:      dir,
		SkipContracts: true,
		SkipTests:     true,
		SkipDrift:     true,
	})

	if !result.Pass {
		t.Errorf("expected audit to pass, got %d errors: %v", result.ErrorCount, result.Findings)
	}
}

// Test: Audit with a malformed annotation fails.
func TestAuditFailsOnMalformedAnnotation(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "bad.go"), []byte(`package pkg

//globular:state_transition NODASH
func Foo() {}
`), 0o644)

	ctx := context.Background()
	result := enforce.Audit(ctx, nil, enforce.AuditOptions{
		SrcDir:      dir,
		SkipContracts: true,
		SkipTests:     true,
		SkipDrift:     true,
	})

	if result.Pass {
		t.Error("expected audit to fail due to MALFORMED_STATE_TRANSITION")
	}
	if result.ErrorCount == 0 {
		t.Error("expected at least one ERROR finding")
	}
}

// Test: AuditFiles is annotation-only and does not require a graph.
func TestAuditFilesAnnotationOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.go")
	_ = os.WriteFile(path, []byte(`package pkg

//globular:hash_schema good_schema
//globular:state_transition A -> B
func Good() {}

//globular:tested_by notATestName
func Bad() {}
`), 0o644)

	result := enforce.AuditFiles([]string{path})
	if result.Pass {
		t.Error("expected audit to fail due to bad tested_by")
	}
	found := false
	for _, f := range result.Findings {
		if f.Code == "ANNOTATION_BAD_TEST_NAME" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ANNOTATION_BAD_TEST_NAME, got: %v", result.Findings)
	}
}

// TestAwarenessAudit_MaxRequiredTestNoPathZero verifies that Audit() scaffold
// check emits SCAFFOLD_TODO_SKIP findings when TODO stubs exist. These findings
// are used by the --max-todo-scaffold-skips CLI gate.
func TestAwarenessAudit_MaxRequiredTestNoPathZero(t *testing.T) {
	dir := t.TempDir()

	// Write a test file with scaffold TODO skips.
	content := `package foo_test

import "testing"

func TestScaffoldForAudit(t *testing.T) {
	t.Skip("TODO: implement required awareness test")
}
`
	if err := os.WriteFile(filepath.Join(dir, "s_test.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := enforce.Audit(context.Background(), nil, enforce.AuditOptions{
		RepoRoot:        dir,
		SkipAnnotations: true,
		SkipContracts:   true,
		SkipTests:       true,
		SkipDrift:       true,
	})

	found := false
	for _, f := range result.Findings {
		if f.Code == enforce.CodeScaffoldTodoSkip {
			found = true
		}
	}
	if !found {
		t.Error("expected SCAFFOLD_TODO_SKIP finding when TODO skips exist")
	}
}

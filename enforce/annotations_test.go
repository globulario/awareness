package enforce_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/enforce"
)

func writeGoSrc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// Test 1: Well-formed annotations produce no findings.
func TestValidateAnnotationsClean(t *testing.T) {
	dir := t.TempDir()
	writeGoSrc(t, dir, "ok.go", `package pkg

//globular:enforces infra.desired_hash_consistency
//globular:hash_schema infra_desired_hash
//globular:state_transition DESIRED -> INSTALLED
//globular:tested_by TestMyFunction
func MyFunction() {}
`)

	findings := enforce.ValidateAnnotations(dir)
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d: %v", len(findings), findings)
	}
}

// Test 2: Malformed state_transition (no arrow) produces ERROR.
func TestValidateAnnotationsMalformedStateTransition(t *testing.T) {
	dir := t.TempDir()
	writeGoSrc(t, dir, "bad.go", `package pkg

//globular:state_transition DESIREDINSTALLED
func Foo() {}
`)

	findings := enforce.ValidateAnnotations(dir)
	found := false
	for _, f := range findings {
		if f.Code == enforce.CodeAnnotationBadStateTrans && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s ERROR, got: %v", enforce.CodeAnnotationBadStateTrans, findings)
	}
}

// Test 3: tested_by that doesn't start with Test/Benchmark/Example → ERROR.
func TestValidateAnnotationsBadTestName(t *testing.T) {
	dir := t.TempDir()
	writeGoSrc(t, dir, "bad.go", `package pkg

//globular:tested_by myTestHelper
func Bar() {}
`)

	findings := enforce.ValidateAnnotations(dir)
	found := false
	for _, f := range findings {
		if f.Code == "ANNOTATION_BAD_TEST_NAME" && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ANNOTATION_BAD_TEST_NAME ERROR, got: %v", findings)
	}
}

// Test 4: _test.go files are skipped — //globular: patterns inside string literals don't produce false positives.
func TestValidateAnnotationsSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	// Write a _test.go file that embeds a malformed annotation as a string literal.
	// Before the fix this would produce MALFORMED_STATE_TRANSITION ERROR.
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(`package pkg_test

func TestSomething(t *testing.T) {
	src := `+"`"+`package pkg
//globular:state_transition NODASH
func Foo() {}
`+"`"+`
	_ = src
}
`), 0o644); err != nil {
		t.Fatalf("write fixture_test.go: %v", err)
	}

	findings := enforce.ValidateAnnotations(dir)
	if len(findings) != 0 {
		t.Errorf("expected no findings from _test.go files, got %d: %v", len(findings), findings)
	}
}

// Test 5: Annotation with no value produces ERROR.
func TestValidateAnnotationsMissingValue(t *testing.T) {
	dir := t.TempDir()
	writeGoSrc(t, dir, "bad.go", `package pkg

//globular:enforces
func Baz() {}
`)

	findings := enforce.ValidateAnnotations(dir)
	found := false
	for _, f := range findings {
		if f.Code == "ANNOTATION_MISSING_VALUE" && f.Severity == enforce.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ANNOTATION_MISSING_VALUE ERROR, got: %v", findings)
	}
}

package enforce_test

import (
	"strings"
	"testing"

	"github.com/globulario/awareness/enforce"
)

// Test 12: RenderAuditMarkdown includes severity counts and finding codes.
func TestRenderAuditMarkdown(t *testing.T) {
	r := &enforce.AuditResult{
		Findings: []enforce.Finding{
			{Code: enforce.CodeAnnotationBadStateTrans, Severity: enforce.SeverityError, File: "pkg/foo.go", Message: "bad transition"},
			{Code: enforce.CodeHashSchemaNoConsumer, Severity: enforce.SeverityWarning, Message: "no consumer"},
		},
		ErrorCount:   1,
		WarningCount: 1,
		Pass:         false,
	}

	md := enforce.RenderAuditMarkdown(r)
	if !strings.Contains(md, "FAIL") {
		t.Error("expected FAIL in markdown")
	}
	if !strings.Contains(md, enforce.CodeAnnotationBadStateTrans) {
		t.Error("expected ANNOTATION_BAD_STATE_TRANSITION in markdown")
	}
	if !strings.Contains(md, enforce.CodeHashSchemaNoConsumer) {
		t.Error("expected HASH_SCHEMA_NO_CONSUMER in markdown")
	}
}

// Test: RenderAuditJSON is valid JSON and includes pass/findings.
func TestRenderAuditJSON(t *testing.T) {
	r := &enforce.AuditResult{
		Findings:     []enforce.Finding{},
		ErrorCount:   0,
		WarningCount: 0,
		Pass:         true,
	}

	jsonStr := enforce.RenderAuditJSON(r)
	if !strings.Contains(jsonStr, `"pass": true`) {
		t.Errorf("expected pass:true in JSON, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"findings"`) {
		t.Error("expected findings key in JSON")
	}
}

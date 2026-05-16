package enforce_test

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/preflight"
)

func TestStrictGateDoesNotBlockByDefault(t *testing.T) {
	res := enforce.EvaluateStrictGate(enforce.StrictGateInput{
		Strict:   false,
		HighRisk: true,
		Preflight: &preflight.Report{
			Classification: []preflight.TaskClass{preflight.ClassUnknownImpact},
		},
	})
	if res.ShouldBlock {
		t.Fatalf("expected non-strict mode to not block, got reasons: %v", res.Reasons)
	}
}

func TestStrictGateBlocksHighRiskUnknownImpact(t *testing.T) {
	res := enforce.EvaluateStrictGate(enforce.StrictGateInput{
		Strict:   true,
		HighRisk: true,
		Preflight: &preflight.Report{
			Classification: []preflight.TaskClass{preflight.ClassArchitectureSensitive, preflight.ClassUnknownImpact},
		},
	})
	if !res.ShouldBlock {
		t.Fatalf("expected strict high-risk unknown impact to block")
	}
}

func TestStrictGateDoesNotBlockLowRiskFile(t *testing.T) {
	res := enforce.EvaluateStrictGate(enforce.StrictGateInput{
		Strict:   true,
		HighRisk: false,
		Preflight: &preflight.Report{
			Classification: []preflight.TaskClass{preflight.ClassUnknownImpact},
		},
	})
	if res.ShouldBlock {
		t.Fatalf("expected low-risk file to not block")
	}
}

func TestStrictGateBlocksAnnotationValidationError(t *testing.T) {
	res := enforce.EvaluateStrictGate(enforce.StrictGateInput{
		Strict: true, HighRisk: true,
		FileAudit: &enforce.AuditResult{ErrorCount: 1},
	})
	if !res.ShouldBlock {
		t.Fatalf("expected annotation validation error to block")
	}
}

func TestHookSummaryIncludesPreflightCommand(t *testing.T) {
	r, err := enforce.RunHook(context.Background(), nil, []string{"golang/foo.go"}, "fix reconcile loop")
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if !strings.Contains(r.Summary, "globular awareness preflight --task") {
		t.Fatalf("expected hook summary to include required preflight command, got: %s", r.Summary)
	}
}

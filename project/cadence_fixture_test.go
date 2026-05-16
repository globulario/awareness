package project_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/project"
	"github.com/globulario/awareness/runtime"
)

func cadenceFixtureDir(t *testing.T) string {
	t.Helper()
	td := testdataDir(t)
	return filepath.Join(filepath.Dir(td), "testdata", "cadence-like")
}

// TestCadenceLike_ProfileDoctor_OK proves that a project with runtime.enabled=false
// resolves cleanly, passes static doctor checks, and never requires a Globular cluster.
func TestCadenceLike_ProfileDoctor_OK(t *testing.T) {
	root := cadenceFixtureDir(t)

	prof, err := project.ResolveProfile(root, project.ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}

	// Identity.
	if prof.Name != "cadence" {
		t.Errorf("Name = %q, want %q", prof.Name, "cadence")
	}
	if string(prof.Kind) != "application" {
		t.Errorf("Kind = %q, want %q", prof.Kind, "application")
	}

	// Runtime must be disabled — no Globular cluster required.
	if prof.IsRuntimeEnabled() {
		t.Error("cadence-like must have runtime disabled (runtime.enabled=false)")
	}
	if prof.AdapterName() != "null" {
		t.Errorf("AdapterName = %q, want %q", prof.AdapterName(), "null")
	}

	// Static doctor must pass.
	report := project.Doctor(prof)
	if !report.OK {
		for _, c := range report.Checks {
			if c.Status == "error" {
				t.Errorf("doctor error: %s — %s", c.Name, c.Detail)
			}
		}
		t.Fatal("profile doctor failed")
	}
	if report.RuntimeStatus != "disabled" {
		t.Errorf("RuntimeStatus = %q, want %q", report.RuntimeStatus, "disabled")
	}

	// Awareness files referenced in profile must exist.
	for _, c := range report.Checks {
		if c.Status == "warn" {
			t.Logf("doctor warn: %s — %s", c.Name, c.Detail)
		}
	}
}

// TestCadenceLike_Preflight_NullAdapter_NoCluster proves that CollectSignals
// through NullAdapter returns a clean disabled result without touching any
// Globular infrastructure. No cluster, no bridge, no imports from services.
func TestCadenceLike_Preflight_NullAdapter_NoCluster(t *testing.T) {
	root := cadenceFixtureDir(t)

	prof, err := project.ResolveProfile(root, project.ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}

	// Select adapter exactly as the standalone runtime registry would.
	adapter, err := runtime.New(prof.AdapterName())
	if err != nil {
		t.Fatalf("runtime.New(%q): %v", prof.AdapterName(), err)
	}

	// Adapter must self-report as null and disabled.
	if adapter.Name() != "null" {
		t.Errorf("adapter.Name() = %q, want %q", adapter.Name(), "null")
	}
	if adapter.Enabled() {
		t.Error("null adapter must report Enabled() = false")
	}

	ctx := context.Background()

	// Doctor must return runtime_disabled, not an error.
	doctorReport, err := adapter.Doctor(ctx, prof)
	if err != nil {
		t.Fatalf("adapter.Doctor: %v", err)
	}
	if doctorReport.Status != "runtime_disabled" {
		t.Errorf("doctor status = %q, want %q", doctorReport.Status, "runtime_disabled")
	}
	if len(doctorReport.Findings) != 0 {
		t.Errorf("expected no findings from null adapter, got %d", len(doctorReport.Findings))
	}

	// CollectSignals must return an empty signals struct, not an error.
	signals, err := adapter.CollectSignals(ctx, prof, runtime.SignalOptions{})
	if err != nil {
		t.Fatalf("CollectSignals: %v", err)
	}
	if signals == nil {
		t.Fatal("CollectSignals returned nil signals")
	}
	if len(signals.DoctorFindings) != 0 {
		t.Errorf("expected no DoctorFindings from null adapter, got %d", len(signals.DoctorFindings))
	}
	if len(signals.ServiceStatuses) != 0 {
		t.Errorf("expected no ServiceStatuses from null adapter, got %d", len(signals.ServiceStatuses))
	}
	if len(signals.RepositoryStatus) != 0 {
		t.Errorf("expected no RepositoryStatus from null adapter")
	}

	// CollectFacts must return nil, nil.
	facts, err := adapter.CollectFacts(ctx, prof)
	if err != nil {
		t.Fatalf("CollectFacts: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected no facts from null adapter, got %d", len(facts))
	}
}

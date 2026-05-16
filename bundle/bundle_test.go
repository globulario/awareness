package bundle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBundle_GenericContract_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	orig := BundleManifest{
		SchemaVersion:          CurrentSchemaVersion,
		ProjectName:            "test-project",
		ProjectKind:            "application",
		SourceRoot:             "/home/user/project",
		SourceRevision:         "abc123",
		GeneratedAt:            now,
		GeneratorVersion:       "1.2.3",
		ProfilePath:            "profile.json",
		GraphPath:              "graph.db",
		FindingsPath:           "findings.json",
		EvidencePath:           "evidence.json",
		InvariantsPaths:        []string{"invariants.yaml"},
		FailureModesPaths:      []string{"failure_modes.yaml"},
		ForbiddenFixesPaths:    []string{"forbidden_fixes.yaml"},
		DecisionsPath:          "decisions/",
		RuntimeSignalsPath:     "runtime_signals.json",
		RuntimeSignalsIncluded: true,
	}

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got BundleManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion: want %q, got %q", CurrentSchemaVersion, got.SchemaVersion)
	}
	if got.ProjectName != orig.ProjectName {
		t.Errorf("ProjectName: want %q, got %q", orig.ProjectName, got.ProjectName)
	}
	if got.ProjectKind != orig.ProjectKind {
		t.Errorf("ProjectKind: want %q, got %q", orig.ProjectKind, got.ProjectKind)
	}
	if got.SourceRevision != orig.SourceRevision {
		t.Errorf("SourceRevision: want %q, got %q", orig.SourceRevision, got.SourceRevision)
	}
	if got.GeneratorVersion != orig.GeneratorVersion {
		t.Errorf("GeneratorVersion: want %q, got %q", orig.GeneratorVersion, got.GeneratorVersion)
	}
	if !got.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded: want true")
	}
	if got.RuntimeSignalsPath != orig.RuntimeSignalsPath {
		t.Errorf("RuntimeSignalsPath: want %q, got %q", orig.RuntimeSignalsPath, got.RuntimeSignalsPath)
	}
	if len(got.InvariantsPaths) != 1 || got.InvariantsPaths[0] != "invariants.yaml" {
		t.Errorf("InvariantsPaths: got %v", got.InvariantsPaths)
	}
}

func TestBundleManifest_SchemaVersionRequired(t *testing.T) {
	m := BundleManifest{ProjectName: "test-project"}
	if err := m.Validate(); err == nil {
		t.Error("expected error for empty SchemaVersion, got nil")
	}
	m.SchemaVersion = CurrentSchemaVersion
	if err := m.Validate(); err != nil {
		t.Errorf("unexpected error after setting SchemaVersion: %v", err)
	}
}

func TestBundleManifest_RuntimeOverlayCompatibilityAlias(t *testing.T) {
	// When both canonical and alias fields are true, both survive the round-trip.
	m := BundleManifest{
		SchemaVersion:          CurrentSchemaVersion,
		ProjectName:            "alias-test",
		GeneratedAt:            time.Now(),
		RuntimeSignalsIncluded: true,
		IncludesRuntimeOverlay: true,
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got BundleManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded must survive round-trip")
	}
	if !got.IncludesRuntimeOverlay {
		t.Error("IncludesRuntimeOverlay (v1 alias) must survive round-trip when set")
	}

	// When runtime signals are absent, both fields must be false after round-trip.
	m2 := BundleManifest{
		SchemaVersion:          CurrentSchemaVersion,
		ProjectName:            "null-adapter-test",
		GeneratedAt:            time.Now(),
		RuntimeSignalsIncluded: false,
		IncludesRuntimeOverlay: false,
	}
	b2, _ := json.Marshal(m2)
	var got2 BundleManifest
	json.Unmarshal(b2, &got2)
	if got2.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded should be false for NullAdapter bundle")
	}
	if got2.IncludesRuntimeOverlay {
		t.Error("IncludesRuntimeOverlay should be false for NullAdapter bundle")
	}
}

func TestBundleManifest_JSONRoundTripV1(t *testing.T) {
	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	m := BundleManifest{
		SchemaVersion:       CurrentSchemaVersion,
		ProjectName:         "cadence",
		ProjectKind:         "bpmn-workflow-engine",
		SourceRoot:          "/home/user/cadence",
		SourceRevision:      "abc1234def5678",
		GeneratedAt:         now,
		GeneratorVersion:    "0.1.0",
		ProfilePath:         "profile.json",
		InvariantsPaths:     []string{"invariants.yaml"},
		FailureModesPaths:   []string{"failure_modes.yaml"},
		ForbiddenFixesPaths: []string{"forbidden_fixes.yaml"},
		DecisionsPath:       "decisions/",
		// NullAdapter bundle — no runtime signals.
		RuntimeSignalsIncluded: false,
		IncludesRuntimeOverlay: false,
	}

	if err := m.Validate(); err != nil {
		t.Fatalf("Validate before marshal: %v", err)
	}

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got BundleManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion: want %q got %q", CurrentSchemaVersion, got.SchemaVersion)
	}
	if got.ProjectName != m.ProjectName {
		t.Errorf("ProjectName: want %q got %q", m.ProjectName, got.ProjectName)
	}
	if got.ProjectKind != m.ProjectKind {
		t.Errorf("ProjectKind: want %q got %q", m.ProjectKind, got.ProjectKind)
	}
	if got.SourceRevision != m.SourceRevision {
		t.Errorf("SourceRevision: want %q got %q", m.SourceRevision, got.SourceRevision)
	}
	if !got.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt: want %v got %v", now, got.GeneratedAt)
	}
	if got.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded should be false for NullAdapter bundle")
	}
	if got.IncludesRuntimeOverlay {
		t.Error("IncludesRuntimeOverlay should be false for NullAdapter bundle")
	}
	if len(got.InvariantsPaths) != 1 || got.InvariantsPaths[0] != "invariants.yaml" {
		t.Errorf("InvariantsPaths: got %v", got.InvariantsPaths)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate after round-trip: %v", err)
	}
}

func TestBundle_RuntimeSignalsOptional(t *testing.T) {
	// A bundle with no runtime signals should serialise and deserialise cleanly.
	m := BundleManifest{
		SchemaVersion: CurrentSchemaVersion,
		ProjectName:   "no-runtime-project",
		ProjectKind:   "library",
		GeneratedAt:   time.Now(),
		ProfilePath:   "profile.json",
	}

	if m.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded should default to false")
	}
	if m.RuntimeSignalsPath != "" {
		t.Errorf("RuntimeSignalsPath should be empty, got %q", m.RuntimeSignalsPath)
	}

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got BundleManifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.RuntimeSignalsIncluded {
		t.Error("RuntimeSignalsIncluded should remain false after round-trip")
	}
	if got.ProjectName != m.ProjectName {
		t.Errorf("ProjectName: want %q, got %q", m.ProjectName, got.ProjectName)
	}
}

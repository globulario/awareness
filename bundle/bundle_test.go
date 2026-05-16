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

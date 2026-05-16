package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/project"
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

// TestBuild_IncludesOptionalExtendedFiles verifies that Build copies optional
// extended knowledge files (decisions.yaml, forbidden_assumptions.yaml, etc.)
// into the bundle when they are present in the awareness root.
func TestBuild_IncludesOptionalExtendedFiles(t *testing.T) {
	// Create a fake awareness root with core + extended files.
	docsDir := t.TempDir()
	writeYAML := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(docsDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	writeYAML("invariants.yaml", "invariants:\n  - id: inv.test\n    summary: test invariant\n    severity: high\n")
	writeYAML("failure_modes.yaml", "failure_modes:\n  - id: fm.test\n    summary: test failure\n")
	writeYAML("forbidden_fixes.yaml", "forbidden_fixes:\n  - id: ff.test\n    summary: do not do this\n")
	writeYAML("decisions.yaml", "decisions:\n  - id: dec.test\n    title: test decision\n    status: accepted\n    because: [reason]\n")
	writeYAML("forbidden_assumptions.yaml", "forbidden_assumptions:\n  - id: fa.test\n    statement: do not assume\n    safer_checks: [verify first]\n")
	writeYAML("authority_rules.yaml", "authority_rules:\n  - id: ar.test\n    title: auth rule\n    layer: Desired\n    rule: use etcd\n    correct_authority: [etcd]\n")

	prof := &project.ProjectProfile{
		Name: "test-project",
		Kind: "library",
		Root: docsDir,
		Awareness: project.AwarenessPaths{
			Root:           docsDir,
			Invariants:     []string{filepath.Join(docsDir, "invariants.yaml")},
			FailureModes:   []string{filepath.Join(docsDir, "failure_modes.yaml")},
			ForbiddenFixes: []string{filepath.Join(docsDir, "forbidden_fixes.yaml")},
		},
	}

	outputDir := t.TempDir()
	manifest, err := Build(prof, outputDir, BuildOptions{Revision: "abc123"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if manifest.DecisionsPath == "" {
		t.Error("DecisionsPath should be set when decisions.yaml is present")
	}
	if manifest.ForbiddenAssumptionsPath == "" {
		t.Error("ForbiddenAssumptionsPath should be set when forbidden_assumptions.yaml is present")
	}
	if manifest.AuthorityRulesPath == "" {
		t.Error("AuthorityRulesPath should be set when authority_rules.yaml is present")
	}
	// Files that were NOT written should remain empty.
	if manifest.RequiredTestsPath != "" {
		t.Errorf("RequiredTestsPath should be empty when required_tests.yaml is absent, got %q", manifest.RequiredTestsPath)
	}

	// Verify the files were actually copied.
	if _, err := os.Stat(filepath.Join(outputDir, manifest.DecisionsPath)); err != nil {
		t.Errorf("decisions file not found in bundle: %v", err)
	}
}

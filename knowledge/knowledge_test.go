package knowledge_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/knowledge"
)

// writeFile creates a temp file with given content for tests.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

const invariantsYAML = `
invariants:
  - id: import.wall.maintained
    severity: critical
    summary: Standalone module must never import Globular-specific packages
    description: The import wall enforces project-agnostic guarantees.
    files:
      - go.mod
`

const failureModesYAML = `
failure_modes:
  - id: services.import.leak
    severity: critical
    summary: Transitive import from services enters standalone awareness
    symptoms:
      - go build pulls in Globular runtime dependencies
    root_cause: A new dependency transitively imports Globular packages.
    known_bad_fixes:
      - Adding a build tag to exclude the import

  - id: globular.vip_used_as_member_endpoint
    severity: critical
    summary: Keepalived floating VIP used where a real node NIC IP is required
    symptoms:
      - Scylla hosts include cluster VIP
    root_cause: PrimaryIP() returned keepalived VIP instead of real node NIC IP.
    known_bad_fixes:
      - Do not add VIP to service member hosts
`

const forbiddenFixesYAML = `
forbidden_fixes:
  - id: no.hardcode.services.import
    summary: Never add a direct import of services to work around the adapter boundary
    description: Importing services directly breaks the import wall.
    correct_approach: Add a method to the Adapter interface instead.
`

const incidentPatternsYAML = `
incident_patterns:
  - id: pat.vip_identity_confusion
    title: VIP used where stable NIC IP required
    severity: critical
    failure_mode: globular.vip_used_as_member_endpoint
    root_cause: Code calls PrimaryIP() without filtering the keepalived floating VIP.
    lesson: Never use PrimaryIP() for member-identity endpoints.
    edit_shapes:
      - any code that calls PrimaryIP() to build a Scylla hosts list
    wrong_fixes:
      - adding VIP to Scylla hosts list
    files:
      - golang/cluster_controller/
    related_invariants:
      - import.wall.maintained
`

func makeTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "invariants.yaml", invariantsYAML)
	writeFile(t, dir, "failure_modes.yaml", failureModesYAML)
	writeFile(t, dir, "forbidden_fixes.yaml", forbiddenFixesYAML)
	writeFile(t, dir, "incident_patterns.yaml", incidentPatternsYAML)
	return dir
}

// ── Load tests ────────────────────────────────────────────────────────────────

func TestLoad_AllFiles(t *testing.T) {
	dir := makeTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.Invariants) != 1 {
		t.Errorf("invariants: got %d, want 1", len(base.Invariants))
	}
	if len(base.FailureModes) != 2 {
		t.Errorf("failure_modes: got %d, want 2", len(base.FailureModes))
	}
	if len(base.ForbiddenFixes) != 1 {
		t.Errorf("forbidden_fixes: got %d, want 1", len(base.ForbiddenFixes))
	}
	if len(base.IncidentPatterns) != 1 {
		t.Errorf("incident_patterns: got %d, want 1", len(base.IncidentPatterns))
	}
}

func TestLoad_MissingFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	// Only write one file.
	writeFile(t, dir, "failure_modes.yaml", failureModesYAML)

	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load with missing files: %v", err)
	}
	if len(base.FailureModes) != 2 {
		t.Errorf("failure_modes: got %d, want 2", len(base.FailureModes))
	}
	if len(base.Invariants) != 0 {
		t.Errorf("invariants should be empty when file missing, got %d", len(base.Invariants))
	}
}

func TestLoad_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load from empty dir: %v", err)
	}
	if base == nil {
		t.Fatal("Load returned nil base")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "invariants.yaml", "this: [is: not: valid")
	_, err := knowledge.Load(dir)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestLoad_FieldPreservation(t *testing.T) {
	dir := makeTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	inv := base.Invariants[0]
	if inv.ID != "import.wall.maintained" {
		t.Errorf("invariant ID: got %q, want %q", inv.ID, "import.wall.maintained")
	}
	if inv.Severity != "critical" {
		t.Errorf("invariant severity: got %q, want %q", inv.Severity, "critical")
	}
	pat := base.IncidentPatterns[0]
	if pat.FailureMode != "globular.vip_used_as_member_endpoint" {
		t.Errorf("pattern failure_mode: got %q", pat.FailureMode)
	}
	if len(pat.EditShapes) != 1 {
		t.Errorf("pattern edit_shapes: got %d, want 1", len(pat.EditShapes))
	}
}

// ── Validate tests ────────────────────────────────────────────────────────────

func TestValidate_CleanBase(t *testing.T) {
	dir := makeTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := knowledge.Validate(base)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
}

func TestValidate_MissingID(t *testing.T) {
	base := &knowledge.Base{
		Invariants: []knowledge.Invariant{
			{ID: "", Summary: "no id here"},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty ID")
	}
}

func TestValidate_BrokenFailureModeRef(t *testing.T) {
	base := &knowledge.Base{
		IncidentPatterns: []knowledge.IncidentPattern{
			{
				ID:          "pat.orphan",
				Title:       "Orphan pattern",
				FailureMode: "nonexistent.failure_mode",
			},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for broken failure_mode reference")
	}
}

func TestValidate_BrokenInvariantRef(t *testing.T) {
	base := &knowledge.Base{
		IncidentPatterns: []knowledge.IncidentPattern{
			{
				ID:                "pat.broken",
				Title:             "Broken invariant ref",
				RelatedInvariants: []string{"nonexistent.invariant"},
			},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for broken invariant reference")
	}
}

// ── Search tests ──────────────────────────────────────────────────────────────

func TestSearch_TaskMatchesFailureMode(t *testing.T) {
	dir := makeTestDir(t)
	base, _ := knowledge.Load(dir)

	matches := knowledge.Search(base, "Scylla host contains VIP causing connection refused", nil)
	if len(matches) == 0 {
		t.Fatal("expected at least one match for VIP-related query")
	}
	found := false
	for _, m := range matches {
		if m.ID == "globular.vip_used_as_member_endpoint" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected globular.vip_used_as_member_endpoint in matches, got: %v", ids(matches))
	}
}

func TestSearch_FilePathMatchesPattern(t *testing.T) {
	dir := makeTestDir(t)
	base, _ := knowledge.Load(dir)

	matches := knowledge.Search(base, "update node endpoint", []string{"golang/cluster_controller/server.go"})
	if len(matches) == 0 {
		t.Fatal("expected matches for cluster_controller file path")
	}
	found := false
	for _, m := range matches {
		if m.ID == "pat.vip_identity_confusion" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pat.vip_identity_confusion in matches, got: %v", ids(matches))
	}
}

func TestSearch_SortedByScore(t *testing.T) {
	dir := makeTestDir(t)
	base, _ := knowledge.Load(dir)

	matches := knowledge.Search(base, "VIP Scylla cluster_controller PrimaryIP", nil)
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Errorf("matches not sorted by score: [%d].Score=%d > [%d].Score=%d",
				i, matches[i].Score, i-1, matches[i-1].Score)
		}
	}
}

func TestSearch_EmptyTask_NoResults(t *testing.T) {
	dir := makeTestDir(t)
	base, _ := knowledge.Load(dir)

	matches := knowledge.Search(base, "", nil)
	if len(matches) != 0 {
		t.Errorf("expected no results for empty task, got %d", len(matches))
	}
}

func TestSearch_NilBase_NoResults(t *testing.T) {
	matches := knowledge.Search(nil, "some task about VIP", nil)
	if len(matches) != 0 {
		t.Errorf("expected no results for nil base, got %d", len(matches))
	}
}

func TestSearch_MaxResults(t *testing.T) {
	// Build a base with many matching entries.
	base := &knowledge.Base{}
	for i := 0; i < 20; i++ {
		base.Invariants = append(base.Invariants, knowledge.Invariant{
			ID:      fmt.Sprintf("inv.%d", i),
			Summary: "import wall VIP cluster",
		})
	}
	matches := knowledge.Search(base, "import wall VIP cluster endpoint", nil)
	if len(matches) > 12 {
		t.Errorf("expected at most 12 results, got %d", len(matches))
	}
}

func ids(ms []knowledge.Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

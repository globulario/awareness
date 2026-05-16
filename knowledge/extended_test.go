package knowledge_test

import (
	"testing"
	"time"

	"github.com/globulario/awareness/knowledge"
)

// YAML snippets for the new knowledge types.

const decisionsYAML = `
decisions:
  - id: decision.import_wall_is_mandatory
    title: Standalone must not import services
    status: accepted
    date: 2026-05-16
    because:
      - consumers must not pull in runtime deps
    protects_invariants:
      - import.wall.maintained
    related_failure_modes:
      - services.import.leak
`

const forbiddenAssumptionsYAML = `
forbidden_assumptions:
  - id: assumption.missing_file_means_error
    statement: A missing .awareness YAML file means misconfiguration
    status: false
    why_wrong:
      - files are optional by design
    safer_checks:
      - use Load() and inspect the result
    related_invariants:
      - import.wall.maintained
`

const requiredTestsYAML = `
required_tests:
  - id: test.import_wall_check
    title: Import wall must pass after any dependency change
    protects:
      invariants:
        - import.wall.maintained
      failure_modes:
        - services.import.leak
    required_for_changes:
      paths:
        - go.mod
        - runtime/**
      task_terms:
        - import
        - adapter
        - runtime
    commands:
      - scripts/check-import-wall.sh
    evidence:
      - no services imports found
`

const subsystemBoundariesYAML = `
subsystem_boundaries:
  - subsystem: standalone
    owns:
      - knowledge loaders
      - preflight logic
    does_not_own:
      - live cluster state
    authoritative_sources:
      desired_state: hand-authored YAML
      installed_state: not applicable
      runtime_state: adapter interface
      inventory_state: not applicable
    dangerous_confusions:
      - importing services inside standalone
    related_invariants:
      - import.wall.maintained
`

const authorityRulesYAML = `
authority_rules:
  - id: authority.yaml_is_ground_truth
    title: YAML files are the authoritative knowledge source
    layer: Knowledge
    question: Where does awareness knowledge come from?
    rule: Read from .awareness/*.yaml only.
    wrong_authority:
      - code comments
      - git history
    correct_authority:
      - .awareness/invariants.yaml
    related_invariants:
      - import.wall.maintained
    forbidden_assumptions:
      - assumption.high_score_definitive
`

const preflightQuestionsYAML = `
preflight_questions:
  - id: preflight.import_change
    title: Import or dependency change preflight
    when:
      task_terms:
        - import
        - adapter
        - go.mod
      paths:
        - go.mod
        - runtime/**
    questions:
      - Does this change add any runtime-specific import?
      - Have you run the import wall check?
    blocking_if_unanswered:
      - import wall verification
`

const remediationContractsYAML = `
remediation_contracts:
  - id: remediation.import_leak
    title: Safe handling of a services import leak
    when:
      failure_modes:
        - services.import.leak
    allowed_actions:
      - identify the file that introduced the import
      - route it through the Adapter interface
    forbidden_actions:
      - add a build tag to hide the import
    requires_human_approval:
      - adding a new Adapter method
`

// makeExtendedTestDir creates a temp dir with all optional knowledge files.
func makeExtendedTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "invariants.yaml", invariantsYAML)
	writeFile(t, dir, "failure_modes.yaml", failureModesYAML)
	writeFile(t, dir, "decisions.yaml", decisionsYAML)
	writeFile(t, dir, "forbidden_assumptions.yaml", forbiddenAssumptionsYAML)
	writeFile(t, dir, "required_tests.yaml", requiredTestsYAML)
	writeFile(t, dir, "subsystem_boundaries.yaml", subsystemBoundariesYAML)
	writeFile(t, dir, "authority_rules.yaml", authorityRulesYAML)
	writeFile(t, dir, "preflight_questions.yaml", preflightQuestionsYAML)
	writeFile(t, dir, "remediation_contracts.yaml", remediationContractsYAML)
	return dir
}

// ── Load tests for new types ──────────────────────────────────────────────────

func TestLoad_Decisions(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.Decisions) != 1 {
		t.Errorf("decisions: got %d, want 1", len(base.Decisions))
	}
	d := base.Decisions[0]
	if d.ID != "decision.import_wall_is_mandatory" {
		t.Errorf("decision ID: got %q", d.ID)
	}
	if d.Title == "" {
		t.Error("decision title should not be empty")
	}
	if len(d.ProtectsInvariants) != 1 {
		t.Errorf("protects_invariants: got %d, want 1", len(d.ProtectsInvariants))
	}
}

func TestLoad_ForbiddenAssumptions(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.ForbiddenAssumptions) != 1 {
		t.Errorf("forbidden_assumptions: got %d, want 1", len(base.ForbiddenAssumptions))
	}
	fa := base.ForbiddenAssumptions[0]
	if fa.Statement == "" {
		t.Error("forbidden_assumption statement should not be empty")
	}
	if len(fa.SaferChecks) == 0 {
		t.Error("forbidden_assumption safer_checks should not be empty")
	}
}

func TestLoad_RequiredTests(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.RequiredTests) != 1 {
		t.Errorf("required_tests: got %d, want 1", len(base.RequiredTests))
	}
	rt := base.RequiredTests[0]
	if len(rt.Commands) == 0 {
		t.Error("required_test commands should not be empty")
	}
	if len(rt.RequiredForChanges.TaskTerms) == 0 {
		t.Error("required_test task_terms should not be empty")
	}
}

func TestLoad_SubsystemBoundaries(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.SubsystemBoundaries) != 1 {
		t.Errorf("subsystem_boundaries: got %d, want 1", len(base.SubsystemBoundaries))
	}
	sb := base.SubsystemBoundaries[0]
	if sb.Subsystem != "standalone" {
		t.Errorf("subsystem: got %q, want %q", sb.Subsystem, "standalone")
	}
}

func TestLoad_AuthorityRules(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.AuthorityRules) != 1 {
		t.Errorf("authority_rules: got %d, want 1", len(base.AuthorityRules))
	}
	ar := base.AuthorityRules[0]
	if ar.Layer != "Knowledge" {
		t.Errorf("authority_rule layer: got %q", ar.Layer)
	}
}

func TestLoad_PreflightQuestions(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.PreflightQuestions) != 1 {
		t.Errorf("preflight_questions: got %d, want 1", len(base.PreflightQuestions))
	}
	pq := base.PreflightQuestions[0]
	if len(pq.Questions) == 0 {
		t.Error("preflight_question questions should not be empty")
	}
}

func TestLoad_RemediationContracts(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(base.RemediationContracts) != 1 {
		t.Errorf("remediation_contracts: got %d, want 1", len(base.RemediationContracts))
	}
	rc := base.RemediationContracts[0]
	if len(rc.AllowedActions) == 0 {
		t.Error("remediation_contract allowed_actions should not be empty")
	}
	if len(rc.ForbiddenActions) == 0 {
		t.Error("remediation_contract forbidden_actions should not be empty")
	}
}

// ── Validate tests for new types ──────────────────────────────────────────────

func TestValidate_DecisionEmptyID(t *testing.T) {
	base := &knowledge.Base{
		Decisions: []knowledge.Decision{
			{ID: "", Title: "no id"},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty decision ID")
	}
}

func TestValidate_DecisionBrokenInvariantRef(t *testing.T) {
	base := &knowledge.Base{
		Decisions: []knowledge.Decision{
			{
				ID:                 "decision.orphan",
				Title:              "orphan decision",
				ProtectsInvariants: []string{"nonexistent.invariant"},
			},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for broken invariant ref in decision")
	}
}

func TestValidate_ForbiddenAssumptionEmptyStatement(t *testing.T) {
	base := &knowledge.Base{
		ForbiddenAssumptions: []knowledge.ForbiddenAssumption{
			{ID: "assumption.no_statement", Statement: ""},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty forbidden_assumption statement")
	}
}

func TestValidate_RequiredTestBrokenInvariantRef(t *testing.T) {
	base := &knowledge.Base{
		RequiredTests: []knowledge.RequiredTest{
			{
				ID:    "test.broken",
				Title: "broken test",
				Protects: knowledge.RequiredTestProtects{
					Invariants: []string{"nonexistent.invariant"},
				},
			},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for broken invariant ref in required_test")
	}
}

func TestValidate_SubsystemBoundaryEmptyName(t *testing.T) {
	base := &knowledge.Base{
		SubsystemBoundaries: []knowledge.SubsystemBoundary{
			{Subsystem: ""},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty subsystem_boundary name")
	}
}

func TestValidate_AuthorityRuleEmptyTitle(t *testing.T) {
	base := &knowledge.Base{
		AuthorityRules: []knowledge.AuthorityRule{
			{ID: "authority.no_title", Title: ""},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty authority_rule title")
	}
}

func TestValidate_RemediationContractEmptyTitle(t *testing.T) {
	base := &knowledge.Base{
		RemediationContracts: []knowledge.RemediationContract{
			{ID: "remediation.no_title", Title: ""},
		},
	}
	errs := knowledge.Validate(base)
	if len(errs) == 0 {
		t.Error("expected validation error for empty remediation_contract title")
	}
}

// ── Search tests for new types ────────────────────────────────────────────────

func TestSearch_MatchesDecision(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	matches := knowledge.Search(base, "standalone import wall services adapter", nil)
	found := false
	for _, m := range matches {
		if m.ID == "decision.import_wall_is_mandatory" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected decision.import_wall_is_mandatory in matches, got: %v", ids(matches))
	}
}

func TestSearch_MatchesForbiddenAssumption(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	matches := knowledge.Search(base, "missing awareness yaml file error", nil)
	found := false
	for _, m := range matches {
		if m.ID == "assumption.missing_file_means_error" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected assumption.missing_file_means_error in matches, got: %v", ids(matches))
	}
}

func TestSearch_MatchesRequiredTest(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	matches := knowledge.Search(base, "import wall check adapter runtime", nil)
	found := false
	for _, m := range matches {
		if m.ID == "test.import_wall_check" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected test.import_wall_check in matches, got: %v", ids(matches))
	}
}

func TestSearch_MatchesAuthorityRule(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	matches := knowledge.Search(base, "knowledge yaml ground truth invariants", nil)
	found := false
	for _, m := range matches {
		if m.ID == "authority.yaml_is_ground_truth" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected authority.yaml_is_ground_truth in matches, got: %v", ids(matches))
	}
}

func TestSearch_MatchesRemediationContract(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	matches := knowledge.Search(base, "services import leak handling", nil)
	found := false
	for _, m := range matches {
		if m.ID == "remediation.import_leak" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected remediation.import_leak in matches, got: %v", ids(matches))
	}
}

// ── MatchedPreflightItems tests ───────────────────────────────────────────────

func TestMatchedPreflightItems_RequiredTestByTaskTerm(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	items := knowledge.MatchedPreflightItems(base, "update the import adapter runtime bridge", nil)
	found := false
	for _, id := range items.RequiredTests {
		if id == "test.import_wall_check" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected test.import_wall_check in required_tests, got: %v", items.RequiredTests)
	}
}

func TestMatchedPreflightItems_RequiredTestByPath(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	items := knowledge.MatchedPreflightItems(base, "update module", []string{"go.mod"})
	found := false
	for _, id := range items.RequiredTests {
		if id == "test.import_wall_check" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected test.import_wall_check in required_tests, got: %v", items.RequiredTests)
	}
}

func TestMatchedPreflightItems_PreflightQuestionsByTaskTerm(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	items := knowledge.MatchedPreflightItems(base, "add new import to adapter", nil)
	if len(items.Questions) == 0 {
		t.Error("expected preflight questions to be populated for import-related task")
	}
}

func TestMatchedPreflightItems_NilBase(t *testing.T) {
	items := knowledge.MatchedPreflightItems(nil, "import runtime adapter", nil)
	if len(items.RequiredTests) != 0 || len(items.Questions) != 0 {
		t.Error("expected empty items for nil base")
	}
}

// ── Selfcheck tests ───────────────────────────────────────────────────────────

func TestSelfcheck_CleanBase(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := knowledge.Selfcheck(base)
	if len(r.Errors) > 0 {
		t.Errorf("expected no selfcheck errors, got: %v", r.Errors)
	}
}

func TestSelfcheck_FlagsOrphanDecision(t *testing.T) {
	base := &knowledge.Base{
		Decisions: []knowledge.Decision{
			{ID: "decision.orphan", Title: "orphan", ProtectsInvariants: []string{"missing.inv"}},
		},
	}
	r := knowledge.Selfcheck(base)
	if len(r.Errors) == 0 {
		t.Error("expected selfcheck error for orphan decision referencing missing invariant")
	}
}

func TestSelfcheck_FlagsCriticalInvariantWithoutProvenBy(t *testing.T) {
	base := &knowledge.Base{
		Invariants: []knowledge.Invariant{
			{ID: "inv.critical", Severity: "critical", Summary: "must be true"},
		},
	}
	r := knowledge.Selfcheck(base)
	hasWarn := false
	for _, w := range r.Warnings {
		if w != "" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected selfcheck warning for critical invariant without proven_by")
	}
}

func TestSelfcheck_FlagsStaleKnowledge(t *testing.T) {
	oldDate := time.Now().AddDate(-2, 0, 0).Format("2006-01-02")
	base := &knowledge.Base{
		Invariants: []knowledge.Invariant{
			{
				ID:       "inv.old",
				Severity: "high",
				Summary:  "stale invariant",
				FreshnessFields: knowledge.FreshnessFields{
					LastVerified:   oldDate,
					StaleAfterDays: 30,
				},
			},
		},
	}
	r := knowledge.Selfcheck(base)
	hasStaleWarn := false
	for _, w := range r.Warnings {
		if len(w) > 0 {
			hasStaleWarn = true
		}
	}
	if !hasStaleWarn {
		t.Error("expected selfcheck warning for stale invariant")
	}
}

func TestSelfcheck_FlagsRemediationContractMissingFailureMode(t *testing.T) {
	base := &knowledge.Base{
		RemediationContracts: []knowledge.RemediationContract{
			{
				ID:    "remediation.test",
				Title: "test contract",
				When: knowledge.RemediationWhen{
					FailureModes: []string{"nonexistent.failure"},
				},
				AllowedActions:   []string{"do something"},
				ForbiddenActions: []string{"do not do this"},
			},
		},
	}
	r := knowledge.Selfcheck(base)
	if len(r.Errors) == 0 {
		t.Error("expected selfcheck error for remediation_contract referencing missing failure_mode")
	}
}

func TestSelfcheck_NilBase(t *testing.T) {
	r := knowledge.Selfcheck(nil)
	if r.OK {
		t.Error("expected selfcheck to fail for nil base")
	}
}

// ── Assurance tests ───────────────────────────────────────────────────────────

func TestAssurance_Counts(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := knowledge.Assurance(base)
	if r.Counts.Invariants != 1 {
		t.Errorf("assurance invariants: got %d, want 1", r.Counts.Invariants)
	}
	if r.Counts.Decisions != 1 {
		t.Errorf("assurance decisions: got %d, want 1", r.Counts.Decisions)
	}
	if r.Counts.ForbiddenAssumptions != 1 {
		t.Errorf("assurance forbidden_assumptions: got %d, want 1", r.Counts.ForbiddenAssumptions)
	}
	if r.Counts.RequiredTests != 1 {
		t.Errorf("assurance required_tests: got %d, want 1", r.Counts.RequiredTests)
	}
	if r.Counts.SubsystemBoundaries != 1 {
		t.Errorf("assurance subsystem_boundaries: got %d, want 1", r.Counts.SubsystemBoundaries)
	}
}

func TestAssurance_SubsystemsNamed(t *testing.T) {
	dir := makeExtendedTestDir(t)
	base, err := knowledge.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := knowledge.Assurance(base)
	if len(r.SubsystemsNamed) == 0 {
		t.Error("expected at least one subsystem named in assurance report")
	}
	if r.SubsystemsNamed[0] != "standalone" {
		t.Errorf("subsystem: got %q, want %q", r.SubsystemsNamed[0], "standalone")
	}
}

func TestAssurance_WarnsMissingBoundaries(t *testing.T) {
	base := &knowledge.Base{
		Invariants: []knowledge.Invariant{
			{ID: "inv.x", Severity: "high", Summary: "x must hold"},
		},
	}
	r := knowledge.Assurance(base)
	hasWarn := false
	for _, w := range r.Warnings {
		if w != "" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Error("expected assurance warning for missing subsystem_boundaries")
	}
}

func TestAssurance_Lines(t *testing.T) {
	base := &knowledge.Base{}
	r := knowledge.Assurance(base)
	lines := r.Lines()
	if len(lines) == 0 {
		t.Error("expected at least one line from Assurance.Lines()")
	}
}

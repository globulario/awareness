// Package knowledge provides typed loaders and in-memory search for Awareness
// knowledge files (.awareness/*.yaml).
//
// All data comes from hand-authored YAML. There is no SQLite dependency.
// Callers load a Base with Load, then search it with Search.
package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Confidence model ─────────────────────────────────────────────────────────

// Confidence describes how certain a preflight verdict is.
type Confidence string

const (
	ConfidenceConfirmed            Confidence = "confirmed"
	ConfidenceSuspected            Confidence = "suspected"
	ConfidenceUnknown              Confidence = "unknown"
	ConfidenceStale                Confidence = "stale"
	ConfidenceContradicted         Confidence = "contradicted"
	ConfidenceInsufficientEvidence Confidence = "insufficient_evidence"
)

// ── Freshness helpers ─────────────────────────────────────────────────────────

// FreshnessFields holds fields shared by several knowledge types.
// They track when the knowledge was last verified and when it may go stale.
type FreshnessFields struct {
	LastVerified   string   `yaml:"last_verified"`
	VerifiedBy     []string `yaml:"verified_by"`
	StaleAfterDays int      `yaml:"stale_after_days"`
}

// ── Proof and usage links ─────────────────────────────────────────────────────

// ProvenByLinks records what proves the knowledge is correct.
type ProvenByLinks struct {
	Tests     []string `yaml:"tests"`
	Incidents []string `yaml:"incidents"`
}

// UsedByLinks records where the knowledge is applied.
type UsedByLinks struct {
	PreflightRules    []string `yaml:"preflight_rules"`
	EvidenceContracts []string `yaml:"evidence_contracts"`
}

// ── Core knowledge types ──────────────────────────────────────────────────────

// Invariant encodes what must never be broken.
type Invariant struct {
	ID          string        `yaml:"id"`
	Title       string        `yaml:"title"`   // human-readable name (Globular schema)
	Severity    string        `yaml:"severity"`
	Status      string        `yaml:"status"`  // active / deprecated
	Summary     string        `yaml:"summary"`
	Description string        `yaml:"description"`
	Files       []string      `yaml:"files"`
	// Globular extended fields
	ForbiddenFixes      []string `yaml:"forbidden_fixes"`
	RelatedFailureModes []string `yaml:"related_failure_modes"`
	RequiredTests       []string `yaml:"required_tests"`
	ProvenBy    ProvenByLinks `yaml:"proven_by"`
	UsedBy      UsedByLinks   `yaml:"used_by"`
	FreshnessFields `yaml:",inline"`
}

// FailureMode encodes a known failure: symptoms, root cause, and what not to do.
type FailureMode struct {
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`       // human-readable name (Globular schema)
	Severity    string   `yaml:"severity"`
	Source      string   `yaml:"source"`
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description"`
	Symptoms    []string `yaml:"symptoms"`
	RootCause   string   `yaml:"root_cause"`
	// correct_approach (standalone) / architecture_fix (Globular) — both loaded
	CorrectApproach []string `yaml:"correct_approach"`
	ArchitectureFix string   `yaml:"architecture_fix"`
	// known_bad_fixes (standalone) / forbidden_fixes (Globular) — both loaded
	KnownBadFixes   []string `yaml:"known_bad_fixes"`
	ForbiddenFixes  []string `yaml:"forbidden_fixes"`
	RegressionTests []string `yaml:"regression_tests"`
	// Globular extended fields
	RelatedInvariants []string `yaml:"related_invariants"`
	RelatedServices   []string `yaml:"related_services"`
	RequiredTests     []string `yaml:"required_tests"`
	FreshnessFields `yaml:",inline"`
}

// ForbiddenFix encodes a specific fix pattern that must never be applied.
type ForbiddenFix struct {
	ID               string   `yaml:"id"`
	Summary          string   `yaml:"summary"`
	Description      string   `yaml:"description"`
	ForbiddenPattern string   `yaml:"forbidden_pattern"`
	// correct_approach (standalone) / safe_alternative (Globular) — both loaded
	CorrectApproach  string   `yaml:"correct_approach"`
	SafeAlternative  string   `yaml:"safe_alternative"`
	RelatedInvariants []string `yaml:"related_invariants"`
}

// IncidentPattern encodes the shape of a dangerous edit extracted from a real incident.
type IncidentPattern struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Severity          string   `yaml:"severity"`
	FailureMode       string   `yaml:"failure_mode"`
	RootCause         string   `yaml:"root_cause"`
	Lesson            string   `yaml:"lesson"`
	EditShapes        []string `yaml:"edit_shapes"`
	WrongFixes        []string `yaml:"wrong_fixes"`
	Files             []string `yaml:"files"`
	RelatedInvariants []string `yaml:"related_invariants"`
	RelatedSymbols    []string `yaml:"related_symbols"`
}

// ── Extended knowledge types (missing pieces) ─────────────────────────────────

// Decision records why an architectural truth exists.
type Decision struct {
	ID                  string   `yaml:"id"`
	Title               string   `yaml:"title"`
	Status              string   `yaml:"status"`
	Date                string   `yaml:"date"`
	Because             []string `yaml:"because"`
	ProtectsInvariants  []string `yaml:"protects_invariants"`
	RelatedFailureModes []string `yaml:"related_failure_modes"`
	ForbiddenFixes      []string `yaml:"forbidden_fixes"`
	EvidenceContracts   []string `yaml:"evidence_contracts"`
}

// ForbiddenAssumption records a belief that is provably wrong and caused failures.
type ForbiddenAssumption struct {
	ID                string   `yaml:"id"`
	Statement         string   `yaml:"statement"`
	Status            string   `yaml:"status"` // typically "false"
	WhyWrong          []string `yaml:"why_wrong"`
	CausedFailures    []string `yaml:"caused_failures"`
	SaferChecks       []string `yaml:"safer_checks"`
	RelatedInvariants []string `yaml:"related_invariants"`
}

// RequiredTestProtects lists what a required test guards.
type RequiredTestProtects struct {
	Invariants   []string `yaml:"invariants"`
	FailureModes []string `yaml:"failure_modes"`
}

// RequiredTestChanges describes when this test is required (matching paths or task terms).
type RequiredTestChanges struct {
	Paths     []string `yaml:"paths"`
	TaskTerms []string `yaml:"task_terms"`
}

// RequiredTest links a test command to the invariants and failure modes it protects.
type RequiredTest struct {
	ID                 string               `yaml:"id"`
	Title              string               `yaml:"title"`
	Protects           RequiredTestProtects `yaml:"protects"`
	RequiredForChanges RequiredTestChanges  `yaml:"required_for_changes"`
	Commands           []string             `yaml:"commands"`
	Evidence           []string             `yaml:"evidence"`
}

// SubsystemAuthoritativeSources names the authoritative source for each state layer.
type SubsystemAuthoritativeSources struct {
	DesiredState   string `yaml:"desired_state"`
	InstalledState string `yaml:"installed_state"`
	RuntimeState   string `yaml:"runtime_state"`
	InventoryState string `yaml:"inventory_state"`
}

// SubsystemBoundary declares what a subsystem owns and what it must not touch.
type SubsystemBoundary struct {
	Subsystem            string                        `yaml:"subsystem"`
	Owns                 []string                      `yaml:"owns"`
	DoesNotOwn           []string                      `yaml:"does_not_own"`
	AuthoritativeSources SubsystemAuthoritativeSources `yaml:"authoritative_sources"`
	DangerousConfusions  []string                      `yaml:"dangerous_confusions"`
	RelatedInvariants    []string                      `yaml:"related_invariants"`
}

// AuthorityRule names which layer or source owns the answer to a specific question.
type AuthorityRule struct {
	ID                   string   `yaml:"id"`
	Title                string   `yaml:"title"`
	Layer                string   `yaml:"layer"`
	Question             string   `yaml:"question"`
	Rule                 string   `yaml:"rule"`
	WrongAuthority       []string `yaml:"wrong_authority"`
	CorrectAuthority     []string `yaml:"correct_authority"`
	RelatedInvariants    []string `yaml:"related_invariants"`
	ForbiddenAssumptions []string `yaml:"forbidden_assumptions"`
}

// PreflightQuestionWhen defines when a preflight question set applies.
type PreflightQuestionWhen struct {
	TaskTerms []string `yaml:"task_terms"`
	Paths     []string `yaml:"paths"`
}

// PreflightQuestion is a set of questions Claude must answer before editing risky code.
type PreflightQuestion struct {
	ID                   string                `yaml:"id"`
	Title                string                `yaml:"title"`
	When                 PreflightQuestionWhen `yaml:"when"`
	Questions            []string              `yaml:"questions"`
	BlockingIfUnanswered []string              `yaml:"blocking_if_unanswered"`
}

// RemediationWhen defines when a remediation contract applies.
type RemediationWhen struct {
	FailureModes []string `yaml:"failure_modes"`
}

// RemediationContract declares safe and forbidden actions when a failure mode triggers.
type RemediationContract struct {
	ID                    string          `yaml:"id"`
	Title                 string          `yaml:"title"`
	When                  RemediationWhen `yaml:"when"`
	AllowedActions        []string        `yaml:"allowed_actions"`
	ForbiddenActions      []string        `yaml:"forbidden_actions"`
	RequiresHumanApproval []string        `yaml:"requires_human_approval"`
}

// ── Base ─────────────────────────────────────────────────────────────────────

// Base holds all loaded knowledge for a project.
type Base struct {
	Invariants          []Invariant
	FailureModes        []FailureMode
	ForbiddenFixes      []ForbiddenFix
	IncidentPatterns    []IncidentPattern
	Decisions           []Decision
	ForbiddenAssumptions []ForbiddenAssumption
	RequiredTests       []RequiredTest
	SubsystemBoundaries []SubsystemBoundary
	AuthorityRules      []AuthorityRule
	PreflightQuestions  []PreflightQuestion
	RemediationContracts []RemediationContract
}

// ── YAML file wrappers ────────────────────────────────────────────────────────

type invariantsFile struct {
	Invariants []Invariant `yaml:"invariants"`
}

type failureModesFile struct {
	FailureModes []FailureMode `yaml:"failure_modes"`
}

type forbiddenFixesFile struct {
	ForbiddenFixes []ForbiddenFix `yaml:"forbidden_fixes"`
}

type incidentPatternsFile struct {
	IncidentPatterns []IncidentPattern `yaml:"incident_patterns"`
}

type decisionsFile struct {
	Decisions []Decision `yaml:"decisions"`
}

type forbiddenAssumptionsFile struct {
	ForbiddenAssumptions []ForbiddenAssumption `yaml:"forbidden_assumptions"`
}

type requiredTestsFile struct {
	RequiredTests []RequiredTest `yaml:"required_tests"`
}

type subsystemBoundariesFile struct {
	SubsystemBoundaries []SubsystemBoundary `yaml:"subsystem_boundaries"`
}

type authorityRulesFile struct {
	AuthorityRules []AuthorityRule `yaml:"authority_rules"`
}

type preflightQuestionsFile struct {
	PreflightQuestions []PreflightQuestion `yaml:"preflight_questions"`
}

type remediationContractsFile struct {
	RemediationContracts []RemediationContract `yaml:"remediation_contracts"`
}

// ── Loader ───────────────────────────────────────────────────────────────────

// Load reads all standard knowledge files from dir (the .awareness/ directory).
// Missing files are silently skipped — a project may not have all knowledge types.
func Load(dir string) (*Base, error) {
	b := &Base{}
	var errs []error

	loadInto(filepath.Join(dir, "invariants.yaml"), &invariantsFile{}, func(v any) {
		b.Invariants = v.(*invariantsFile).Invariants
	}, &errs)

	loadInto(filepath.Join(dir, "failure_modes.yaml"), &failureModesFile{}, func(v any) {
		b.FailureModes = v.(*failureModesFile).FailureModes
	}, &errs)

	loadInto(filepath.Join(dir, "forbidden_fixes.yaml"), &forbiddenFixesFile{}, func(v any) {
		b.ForbiddenFixes = v.(*forbiddenFixesFile).ForbiddenFixes
	}, &errs)

	loadInto(filepath.Join(dir, "incident_patterns.yaml"), &incidentPatternsFile{}, func(v any) {
		b.IncidentPatterns = v.(*incidentPatternsFile).IncidentPatterns
	}, &errs)

	loadInto(filepath.Join(dir, "decisions.yaml"), &decisionsFile{}, func(v any) {
		b.Decisions = v.(*decisionsFile).Decisions
	}, &errs)

	loadInto(filepath.Join(dir, "forbidden_assumptions.yaml"), &forbiddenAssumptionsFile{}, func(v any) {
		b.ForbiddenAssumptions = v.(*forbiddenAssumptionsFile).ForbiddenAssumptions
	}, &errs)

	loadInto(filepath.Join(dir, "required_tests.yaml"), &requiredTestsFile{}, func(v any) {
		b.RequiredTests = v.(*requiredTestsFile).RequiredTests
	}, &errs)

	loadInto(filepath.Join(dir, "subsystem_boundaries.yaml"), &subsystemBoundariesFile{}, func(v any) {
		b.SubsystemBoundaries = v.(*subsystemBoundariesFile).SubsystemBoundaries
	}, &errs)

	loadInto(filepath.Join(dir, "authority_rules.yaml"), &authorityRulesFile{}, func(v any) {
		b.AuthorityRules = v.(*authorityRulesFile).AuthorityRules
	}, &errs)

	loadInto(filepath.Join(dir, "preflight_questions.yaml"), &preflightQuestionsFile{}, func(v any) {
		b.PreflightQuestions = v.(*preflightQuestionsFile).PreflightQuestions
	}, &errs)

	loadInto(filepath.Join(dir, "remediation_contracts.yaml"), &remediationContractsFile{}, func(v any) {
		b.RemediationContracts = v.(*remediationContractsFile).RemediationContracts
	}, &errs)

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return b, fmt.Errorf("knowledge.Load: %s", strings.Join(msgs, "; "))
	}
	return b, nil
}

// LoadFromPaths loads knowledge from explicit file lists, as specified by a
// project profile. Each entry may be a file path or a directory; directories
// are expanded to all *.yaml files within them (non-recursive). Missing files
// and empty directories are silently skipped.
//
// An optional extendedRoot directory may be provided (as a single variadic
// argument). When present, the seven extended knowledge files are loaded from
// that directory by convention: decisions.yaml, forbidden_assumptions.yaml,
// required_tests.yaml, subsystem_boundaries.yaml, authority_rules.yaml,
// preflight_questions.yaml, and remediation_contracts.yaml. Missing files are
// silently skipped so projects that do not use extended knowledge still work.
//
// This is the profile-aware counterpart to Load (which expects a single fixed dir).
func LoadFromPaths(invariants, failureModes, forbiddenFixes, incidentPatterns []string, extendedRoot ...string) (*Base, error) {
	b := &Base{}
	var errs []error

	for _, path := range expandPaths(invariants) {
		var f invariantsFile
		if err := unmarshalFile(path, &f); err != nil {
			errs = append(errs, err)
			continue
		}
		b.Invariants = append(b.Invariants, f.Invariants...)
	}
	for _, path := range expandPaths(failureModes) {
		var f failureModesFile
		if err := unmarshalFile(path, &f); err != nil {
			errs = append(errs, err)
			continue
		}
		b.FailureModes = append(b.FailureModes, f.FailureModes...)
	}
	for _, path := range expandPaths(forbiddenFixes) {
		var f forbiddenFixesFile
		if err := unmarshalFile(path, &f); err != nil {
			errs = append(errs, err)
			continue
		}
		b.ForbiddenFixes = append(b.ForbiddenFixes, f.ForbiddenFixes...)
	}
	for _, path := range expandPaths(incidentPatterns) {
		var f incidentPatternsFile
		if err := unmarshalFile(path, &f); err != nil {
			errs = append(errs, err)
			continue
		}
		b.IncidentPatterns = append(b.IncidentPatterns, f.IncidentPatterns...)
	}

	// Extended types: loaded from extendedRoot by convention when provided.
	if len(extendedRoot) > 0 && extendedRoot[0] != "" {
		dir := extendedRoot[0]
		loadInto(filepath.Join(dir, "decisions.yaml"), &decisionsFile{}, func(v any) {
			b.Decisions = append(b.Decisions, v.(*decisionsFile).Decisions...)
		}, &errs)
		loadInto(filepath.Join(dir, "forbidden_assumptions.yaml"), &forbiddenAssumptionsFile{}, func(v any) {
			b.ForbiddenAssumptions = append(b.ForbiddenAssumptions, v.(*forbiddenAssumptionsFile).ForbiddenAssumptions...)
		}, &errs)
		loadInto(filepath.Join(dir, "required_tests.yaml"), &requiredTestsFile{}, func(v any) {
			b.RequiredTests = append(b.RequiredTests, v.(*requiredTestsFile).RequiredTests...)
		}, &errs)
		loadInto(filepath.Join(dir, "subsystem_boundaries.yaml"), &subsystemBoundariesFile{}, func(v any) {
			b.SubsystemBoundaries = append(b.SubsystemBoundaries, v.(*subsystemBoundariesFile).SubsystemBoundaries...)
		}, &errs)
		loadInto(filepath.Join(dir, "authority_rules.yaml"), &authorityRulesFile{}, func(v any) {
			b.AuthorityRules = append(b.AuthorityRules, v.(*authorityRulesFile).AuthorityRules...)
		}, &errs)
		loadInto(filepath.Join(dir, "preflight_questions.yaml"), &preflightQuestionsFile{}, func(v any) {
			b.PreflightQuestions = append(b.PreflightQuestions, v.(*preflightQuestionsFile).PreflightQuestions...)
		}, &errs)
		loadInto(filepath.Join(dir, "remediation_contracts.yaml"), &remediationContractsFile{}, func(v any) {
			b.RemediationContracts = append(b.RemediationContracts, v.(*remediationContractsFile).RemediationContracts...)
		}, &errs)
	}

	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return b, fmt.Errorf("knowledge.LoadFromPaths: %s", strings.Join(msgs, "; "))
	}
	return b, nil
}

// expandPaths expands a mixed list of file paths and directories into a flat
// list of *.yaml file paths. Missing entries are silently skipped.
func expandPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
					out = append(out, filepath.Join(p, e.Name()))
				}
			}
		} else {
			out = append(out, p)
		}
	}
	return out
}

// unmarshalFile reads and unmarshals one YAML file into dest.
// Files with an unrecognised schema (no matching top-level key) are silently
// skipped rather than treated as errors — this allows incident seed files in
// ErrorCategory format to coexist alongside standard incident_patterns files.
func unmarshalFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		// Schema mismatch (e.g. ErrorCategory seeds) — skip, don't fail.
		return nil
	}
	return nil
}

func loadInto(path string, dest any, set func(any), errs *[]error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		*errs = append(*errs, fmt.Errorf("read %s: %w", path, err))
		return
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		*errs = append(*errs, fmt.Errorf("parse %s: %w", path, err))
		return
	}
	set(dest)
}

// ── Validate ─────────────────────────────────────────────────────────────────

// ValidationError describes a problem in the knowledge base.
type ValidationError struct {
	Kind    string
	ID      string
	Problem string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s %q: %s", e.Kind, e.ID, e.Problem)
}

// Validate checks the knowledge base for empty IDs, missing summaries, and
// broken cross-references. It returns all problems found, not just the first.
func Validate(b *Base) []ValidationError {
	var errs []ValidationError

	invIDs := make(map[string]bool)
	for _, inv := range b.Invariants {
		if inv.ID == "" {
			errs = append(errs, ValidationError{"invariant", inv.ID, "empty id"})
			continue
		}
		if inv.Summary == "" {
			errs = append(errs, ValidationError{"invariant", inv.ID, "empty summary"})
		}
		invIDs[inv.ID] = true
	}

	fmIDs := make(map[string]bool)
	for _, fm := range b.FailureModes {
		if fm.ID == "" {
			errs = append(errs, ValidationError{"failure_mode", fm.ID, "empty id"})
			continue
		}
		if fm.Summary == "" {
			errs = append(errs, ValidationError{"failure_mode", fm.ID, "empty summary"})
		}
		fmIDs[fm.ID] = true
	}

	for _, ff := range b.ForbiddenFixes {
		if ff.ID == "" {
			errs = append(errs, ValidationError{"forbidden_fix", ff.ID, "empty id"})
			continue
		}
		if ff.Summary == "" {
			errs = append(errs, ValidationError{"forbidden_fix", ff.ID, "empty summary"})
		}
	}

	for _, pat := range b.IncidentPatterns {
		if pat.ID == "" {
			errs = append(errs, ValidationError{"incident_pattern", pat.ID, "empty id"})
			continue
		}
		title := pat.Title
		if title == "" {
			title = pat.ID
		}
		if title == "" {
			errs = append(errs, ValidationError{"incident_pattern", pat.ID, "empty title"})
		}
		if pat.FailureMode != "" && !fmIDs[pat.FailureMode] {
			errs = append(errs, ValidationError{
				"incident_pattern", pat.ID,
				fmt.Sprintf("references unknown failure_mode %q", pat.FailureMode),
			})
		}
		for _, ref := range pat.RelatedInvariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"incident_pattern", pat.ID,
					fmt.Sprintf("references unknown invariant %q", ref),
				})
			}
		}
	}

	for _, d := range b.Decisions {
		if d.ID == "" {
			errs = append(errs, ValidationError{"decision", d.ID, "empty id"})
			continue
		}
		if d.Title == "" {
			errs = append(errs, ValidationError{"decision", d.ID, "empty title"})
		}
		for _, ref := range d.ProtectsInvariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"decision", d.ID,
					fmt.Sprintf("references unknown invariant %q", ref),
				})
			}
		}
		for _, ref := range d.RelatedFailureModes {
			if !fmIDs[ref] {
				errs = append(errs, ValidationError{
					"decision", d.ID,
					fmt.Sprintf("references unknown failure_mode %q", ref),
				})
			}
		}
	}

	for _, fa := range b.ForbiddenAssumptions {
		if fa.ID == "" {
			errs = append(errs, ValidationError{"forbidden_assumption", fa.ID, "empty id"})
			continue
		}
		if fa.Statement == "" {
			errs = append(errs, ValidationError{"forbidden_assumption", fa.ID, "empty statement"})
		}
		for _, ref := range fa.RelatedInvariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"forbidden_assumption", fa.ID,
					fmt.Sprintf("references unknown invariant %q", ref),
				})
			}
		}
	}

	for _, rt := range b.RequiredTests {
		if rt.ID == "" {
			errs = append(errs, ValidationError{"required_test", rt.ID, "empty id"})
			continue
		}
		if rt.Title == "" {
			errs = append(errs, ValidationError{"required_test", rt.ID, "empty title"})
		}
		for _, ref := range rt.Protects.Invariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"required_test", rt.ID,
					fmt.Sprintf("protects unknown invariant %q", ref),
				})
			}
		}
		for _, ref := range rt.Protects.FailureModes {
			if !fmIDs[ref] {
				errs = append(errs, ValidationError{
					"required_test", rt.ID,
					fmt.Sprintf("protects unknown failure_mode %q", ref),
				})
			}
		}
	}

	for _, sb := range b.SubsystemBoundaries {
		if sb.Subsystem == "" {
			errs = append(errs, ValidationError{"subsystem_boundary", sb.Subsystem, "empty subsystem name"})
			continue
		}
		for _, ref := range sb.RelatedInvariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"subsystem_boundary", sb.Subsystem,
					fmt.Sprintf("references unknown invariant %q", ref),
				})
			}
		}
	}

	for _, ar := range b.AuthorityRules {
		if ar.ID == "" {
			errs = append(errs, ValidationError{"authority_rule", ar.ID, "empty id"})
			continue
		}
		if ar.Title == "" {
			errs = append(errs, ValidationError{"authority_rule", ar.ID, "empty title"})
		}
		for _, ref := range ar.RelatedInvariants {
			if !invIDs[ref] {
				errs = append(errs, ValidationError{
					"authority_rule", ar.ID,
					fmt.Sprintf("references unknown invariant %q", ref),
				})
			}
		}
	}

	for _, pq := range b.PreflightQuestions {
		if pq.ID == "" {
			errs = append(errs, ValidationError{"preflight_question", pq.ID, "empty id"})
			continue
		}
		if pq.Title == "" {
			errs = append(errs, ValidationError{"preflight_question", pq.ID, "empty title"})
		}
	}

	for _, rc := range b.RemediationContracts {
		if rc.ID == "" {
			errs = append(errs, ValidationError{"remediation_contract", rc.ID, "empty id"})
			continue
		}
		if rc.Title == "" {
			errs = append(errs, ValidationError{"remediation_contract", rc.ID, "empty title"})
		}
	}

	return errs
}

// ── Search ───────────────────────────────────────────────────────────────────

// Match is one search result from the knowledge base.
type Match struct {
	// Kind is one of: "invariant", "failure_mode", "forbidden_fix",
	// "incident_pattern", "decision", "forbidden_assumption", "required_test",
	// "subsystem_boundary", "authority_rule", "preflight_question", "remediation_contract".
	Kind         string
	ID           string
	Summary      string
	Severity     string
	Score        int
	MatchedTerms []string
}

// Search returns knowledge entries relevant to the given task and changed files,
// sorted by score descending. At most 12 results are returned.
func Search(base *Base, task string, files []string) []Match {
	terms := searchTerms(task, files)
	if len(terms) == 0 || base == nil {
		return nil
	}

	var out []Match

	for _, inv := range base.Invariants {
		title := inv.Title
		if title == "" {
			title = inv.Summary
		}
		blob := strings.ToLower(strings.Join([]string{
			inv.ID, inv.Title, inv.Summary, inv.Description,
			strings.Join(inv.Files, " "),
			strings.Join(inv.ForbiddenFixes, " "),
			strings.Join(inv.RelatedFailureModes, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "invariant", inv.ID, title, inv.Severity, terms); ok {
			out = append(out, m)
		}
	}

	for _, fm := range base.FailureModes {
		title := fm.Title
		if title == "" {
			title = fm.Summary
		}
		blob := strings.ToLower(strings.Join([]string{
			fm.ID, fm.Title, fm.Summary, fm.Description, fm.RootCause, fm.ArchitectureFix,
			strings.Join(fm.Symptoms, " "),
			strings.Join(fm.KnownBadFixes, " "),
			strings.Join(fm.ForbiddenFixes, " "),
			strings.Join(fm.CorrectApproach, " "),
			strings.Join(fm.RelatedInvariants, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "failure_mode", fm.ID, title, fm.Severity, terms); ok {
			out = append(out, m)
		}
	}

	for _, ff := range base.ForbiddenFixes {
		blob := strings.ToLower(strings.Join([]string{
			ff.ID, ff.Summary, ff.Description, ff.ForbiddenPattern,
			ff.CorrectApproach, ff.SafeAlternative,
			strings.Join(ff.RelatedInvariants, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "forbidden_fix", ff.ID, ff.Summary, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, pat := range base.IncidentPatterns {
		blob := strings.ToLower(strings.Join([]string{
			pat.ID, pat.Title, pat.RootCause, pat.Lesson, pat.FailureMode,
			strings.Join(pat.EditShapes, " "),
			strings.Join(pat.WrongFixes, " "),
			strings.Join(pat.Files, " "),
			strings.Join(pat.RelatedSymbols, " "),
		}, " "))
		summary := pat.Title
		if summary == "" {
			summary = pat.ID
		}
		if m, ok := scoreBlob(blob, "incident_pattern", pat.ID, summary, pat.Severity, terms); ok {
			out = append(out, m)
		}
	}

	for _, d := range base.Decisions {
		blob := strings.ToLower(strings.Join([]string{
			d.ID, d.Title, d.Status,
			strings.Join(d.Because, " "),
			strings.Join(d.ProtectsInvariants, " "),
			strings.Join(d.RelatedFailureModes, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "decision", d.ID, d.Title, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, fa := range base.ForbiddenAssumptions {
		blob := strings.ToLower(strings.Join([]string{
			fa.ID, fa.Statement,
			strings.Join(fa.WhyWrong, " "),
			strings.Join(fa.SaferChecks, " "),
			strings.Join(fa.RelatedInvariants, " "),
			strings.Join(fa.CausedFailures, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "forbidden_assumption", fa.ID, fa.Statement, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, rt := range base.RequiredTests {
		blob := strings.ToLower(strings.Join([]string{
			rt.ID, rt.Title,
			strings.Join(rt.RequiredForChanges.TaskTerms, " "),
			strings.Join(rt.RequiredForChanges.Paths, " "),
			strings.Join(rt.Protects.Invariants, " "),
			strings.Join(rt.Protects.FailureModes, " "),
			strings.Join(rt.Evidence, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "required_test", rt.ID, rt.Title, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, sb := range base.SubsystemBoundaries {
		blob := strings.ToLower(strings.Join([]string{
			sb.Subsystem,
			strings.Join(sb.Owns, " "),
			strings.Join(sb.DoesNotOwn, " "),
			strings.Join(sb.DangerousConfusions, " "),
			strings.Join(sb.RelatedInvariants, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "subsystem_boundary", sb.Subsystem, sb.Subsystem, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, ar := range base.AuthorityRules {
		blob := strings.ToLower(strings.Join([]string{
			ar.ID, ar.Title, ar.Layer, ar.Question, ar.Rule,
			strings.Join(ar.WrongAuthority, " "),
			strings.Join(ar.CorrectAuthority, " "),
			strings.Join(ar.RelatedInvariants, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "authority_rule", ar.ID, ar.Title, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, pq := range base.PreflightQuestions {
		blob := strings.ToLower(strings.Join([]string{
			pq.ID, pq.Title,
			strings.Join(pq.When.TaskTerms, " "),
			strings.Join(pq.When.Paths, " "),
			strings.Join(pq.Questions, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "preflight_question", pq.ID, pq.Title, "", terms); ok {
			out = append(out, m)
		}
	}

	for _, rc := range base.RemediationContracts {
		blob := strings.ToLower(strings.Join([]string{
			rc.ID, rc.Title,
			strings.Join(rc.When.FailureModes, " "),
			strings.Join(rc.AllowedActions, " "),
			strings.Join(rc.ForbiddenActions, " "),
		}, " "))
		if m, ok := scoreBlob(blob, "remediation_contract", rc.ID, rc.Title, "", terms); ok {
			out = append(out, m)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// scoreBlob scores a pre-lowercased blob against search terms.
// Returns (Match, true) when at least one term matched.
func scoreBlob(blob, kind, id, summary, severity string, terms []string) (Match, bool) {
	var matched []string
	seen := map[string]bool{}
	for _, term := range terms {
		if strings.Contains(blob, term) && !seen[term] {
			seen[term] = true
			matched = append(matched, term)
		}
	}
	if len(matched) == 0 {
		return Match{}, false
	}
	score := len(matched)
	for _, mt := range matched {
		if strings.Contains(mt, "/") || strings.Contains(mt, ".") || strings.Contains(mt, "_") {
			score++
		}
	}
	return Match{
		Kind:         kind,
		ID:           id,
		Summary:      summary,
		Severity:     severity,
		Score:        score,
		MatchedTerms: matched,
	}, true
}

var tokenRE = regexp.MustCompile(`[a-zA-Z0-9_./:-]+`)

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "when": true, "where": true,
	"what": true, "will": true, "make": true, "fix": true, "code": true,
	"file": true, "tool": true, "safe": true, "module": true,
	"awareness": true, "globular": true,
}

// searchTerms extracts normalized search terms from the task and file paths.
func searchTerms(task string, files []string) []string {
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.Trim(strings.ToLower(s), " \t\n\r,.;()[]{}'\"")
		if len(s) < 3 || stopWords[s] || seen[s] {
			return
		}
		seen[s] = true
	}
	split := regexp.MustCompile(`[._:/-]+`)
	for _, t := range tokenRE.FindAllString(task, -1) {
		add(t)
		if strings.ContainsAny(t, "._:-/") {
			for _, part := range split.Split(t, -1) {
				add(part)
			}
		}
	}
	for _, f := range files {
		f = filepath.ToSlash(f)
		add(f)
		add(filepath.Base(f))
		for _, part := range strings.FieldsFunc(f, func(r rune) bool {
			return r == '/' || r == '-' || r == '_' || r == '.'
		}) {
			add(part)
		}
	}
	terms := make([]string, 0, len(seen))
	for t := range seen {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	return terms
}

// ── Preflight matching helpers ────────────────────────────────────────────────

// MatchedPreflightItems returns the decisions, forbidden assumptions, authority rules,
// required tests, preflight questions, and remediation contracts that match the
// given task text and changed file paths. Results are IDs only.
func MatchedPreflightItems(base *Base, task string, files []string) PreflightItems {
	if base == nil {
		return PreflightItems{}
	}
	task = strings.ToLower(task)
	terms := searchTerms(task, files)

	var out PreflightItems

	for _, d := range base.Decisions {
		blob := strings.ToLower(strings.Join([]string{
			d.ID, d.Title, strings.Join(d.Because, " "),
			strings.Join(d.ProtectsInvariants, " "),
			strings.Join(d.RelatedFailureModes, " "),
		}, " "))
		if blobMatchesAny(blob, terms) {
			out.Decisions = append(out.Decisions, d.ID)
		}
	}

	for _, fa := range base.ForbiddenAssumptions {
		blob := strings.ToLower(strings.Join([]string{
			fa.ID, fa.Statement,
			strings.Join(fa.WhyWrong, " "),
			strings.Join(fa.RelatedInvariants, " "),
		}, " "))
		if blobMatchesAny(blob, terms) {
			out.ForbiddenAssumptions = append(out.ForbiddenAssumptions, fa.ID)
		}
	}

	for _, ar := range base.AuthorityRules {
		blob := strings.ToLower(strings.Join([]string{
			ar.ID, ar.Title, ar.Layer, ar.Question, ar.Rule,
			strings.Join(ar.RelatedInvariants, " "),
		}, " "))
		if blobMatchesAny(blob, terms) {
			out.AuthorityRules = append(out.AuthorityRules, ar.ID)
		}
	}

	for _, rt := range base.RequiredTests {
		if requiredTestMatches(rt, task, files) {
			out.RequiredTests = append(out.RequiredTests, rt.ID)
		}
	}

	for _, pq := range base.PreflightQuestions {
		if preflightQuestionMatches(pq, task, files) {
			out.PreflightQuestions = append(out.PreflightQuestions, pq.ID)
			out.Questions = append(out.Questions, pq.Questions...)
		}
	}

	for _, rc := range base.RemediationContracts {
		blob := strings.ToLower(strings.Join([]string{
			rc.ID, rc.Title,
			strings.Join(rc.When.FailureModes, " "),
		}, " "))
		if blobMatchesAny(blob, terms) {
			out.RemediationContracts = append(out.RemediationContracts, rc.ID)
		}
	}

	return out
}

// PreflightItems groups matched knowledge IDs for preflight output.
type PreflightItems struct {
	Decisions            []string
	ForbiddenAssumptions []string
	AuthorityRules       []string
	RequiredTests        []string
	PreflightQuestions   []string
	RemediationContracts []string
	Questions            []string // expanded question text
}

func blobMatchesAny(blob string, terms []string) bool {
	for _, t := range terms {
		if strings.Contains(blob, t) {
			return true
		}
	}
	return false
}

func requiredTestMatches(rt RequiredTest, task string, files []string) bool {
	taskLower := strings.ToLower(task)
	for _, term := range rt.RequiredForChanges.TaskTerms {
		if strings.Contains(taskLower, strings.ToLower(term)) {
			return true
		}
	}
	for _, pattern := range rt.RequiredForChanges.Paths {
		for _, f := range files {
			if globMatch(pattern, filepath.ToSlash(f)) {
				return true
			}
		}
	}
	return false
}

func preflightQuestionMatches(pq PreflightQuestion, task string, files []string) bool {
	taskLower := strings.ToLower(task)
	for _, term := range pq.When.TaskTerms {
		if strings.Contains(taskLower, strings.ToLower(term)) {
			return true
		}
	}
	for _, pattern := range pq.When.Paths {
		for _, f := range files {
			if globMatch(pattern, filepath.ToSlash(f)) {
				return true
			}
		}
	}
	return false
}

// globMatch implements simple ** glob matching for path patterns.
func globMatch(pattern, path string) bool {
	if !strings.Contains(pattern, "**") {
		matched, err := filepath.Match(pattern, path)
		return err == nil && matched
	}
	// Replace ** with a sentinel, then split on it and check prefix/suffix.
	parts := strings.SplitN(pattern, "**", 2)
	prefix := parts[0]
	suffix := parts[1]
	if prefix != "" && !strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")) {
		return false
	}
	if suffix != "" {
		suffix = strings.TrimPrefix(suffix, "/")
		return strings.HasSuffix(path, suffix) || strings.Contains(path, "/"+suffix)
	}
	return true
}

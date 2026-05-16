package integrity

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ShapeViolation describes a structural problem with a knowledge graph entry.
type ShapeViolation struct {
	NodeType string `json:"node_type"`
	NodeID   string `json:"node_id"`
	Field    string `json:"field"`
	Severity string `json:"severity"` // "critical" | "warning"
	Message  string `json:"message"`
}

// ── YAML schema types ─────────────────────────────────────────────────────────

// FixCase is the integrity-check view of a fix case entry in fix_cases.yaml.
type FixCase struct {
	ID               string   `yaml:"id"`
	Status           string   `yaml:"status"`
	TargetInvariants []string `yaml:"target_invariants"`
	FixedFiles       []string `yaml:"fixed_files"`
	RemainingFiles   []string `yaml:"remaining_files"`
	RequiredTests    []string `yaml:"required_tests"`
	Notes            string   `yaml:"notes"`
}

// ForbiddenFix is the integrity-check view of a forbidden fix entry.
type ForbiddenFix struct {
	ID                  string   `yaml:"id"`
	Summary             string   `yaml:"summary"`
	SafeAlternative     string   `yaml:"safe_alternative"`
	WhyWrong            string   `yaml:"why_wrong"`
	RelatedInvariants   []string `yaml:"related_invariants"`
	RelatedFailureModes []string `yaml:"related_failure_modes"`
	RequiredTests       []string `yaml:"required_tests"`
}

// FailureMode is the integrity-check view of a failure mode entry.
type FailureMode struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Symptoms           []string `yaml:"symptoms"`
	RootCause          string   `yaml:"root_cause"`
	AffectedComponents []string `yaml:"affected_components"`
	DiagnosticCommands []string `yaml:"diagnostic_commands"`
	ForbiddenFixes     []string `yaml:"forbidden_fixes"`
	RequiredTests      []string `yaml:"required_tests"`
}

// CausalRule is the integrity-check view of a causal rule entry.
type CausalRule struct {
	ID                  string        `yaml:"id"`
	RootSignal          string        `yaml:"root_signal"`
	Sequence            []interface{} `yaml:"sequence"`
	Confidence          string        `yaml:"confidence"`
	ExplanationTemplate string        `yaml:"explanation_template"`
	RecommendedFixOrder []string      `yaml:"recommended_fix_order"`
	BlindSpots          []string      `yaml:"blind_spots"`
}

// ── YAML file loaders ─────────────────────────────────────────────────────────

type fixCasesFile struct {
	FixCases []FixCase `yaml:"fix_cases"`
}

func loadIntegrityFixCases(docsDir string) ([]FixCase, error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/fix_cases.yaml", docsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fix_cases.yaml: %w", err)
	}
	var f fixCasesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse fix_cases.yaml: %w", err)
	}
	return f.FixCases, nil
}

type forbiddenFixesFile struct {
	ForbiddenFixes []ForbiddenFix `yaml:"forbidden_fixes"`
}

func loadIntegrityForbiddenFixes(docsDir string) ([]ForbiddenFix, error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/forbidden_fixes.yaml", docsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read forbidden_fixes.yaml: %w", err)
	}
	var f forbiddenFixesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse forbidden_fixes.yaml: %w", err)
	}
	return f.ForbiddenFixes, nil
}

type failureModesFile struct {
	FailureModes []FailureMode `yaml:"failure_modes"`
}

func loadIntegrityFailureModes(docsDir string) ([]FailureMode, error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/failure_modes.yaml", docsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read failure_modes.yaml: %w", err)
	}
	var f failureModesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse failure_modes.yaml: %w", err)
	}
	return f.FailureModes, nil
}

type causalRulesFile struct {
	Rules []CausalRule `yaml:"rules"`
}

func loadIntegrityCausalRules(docsDir string) ([]CausalRule, error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/knowledge/causal_rules.yaml", docsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read causal_rules.yaml: %w", err)
	}
	var f causalRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse causal_rules.yaml: %w", err)
	}
	return f.Rules, nil
}

// ── Shape validators ─────────────────────────────────────────────────────────

// ValidateFixCaseShapes validates the shape of each fix case.
// DONE fix cases must have required tests and at least one invariant.
func ValidateFixCaseShapes(fixCases []FixCase) []ShapeViolation {
	var violations []ShapeViolation
	for _, fc := range fixCases {
		if fc.ID == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "fix_case",
				NodeID:   "(missing id)",
				Field:    "id",
				Severity: "critical",
				Message:  "fix_case is missing required field 'id'",
			})
			continue
		}
		if strings.ToUpper(fc.Status) != "DONE" {
			continue
		}
		// DONE fix cases must have required tests.
		if len(fc.RequiredTests) == 0 {
			violations = append(violations, ShapeViolation{
				NodeType: "fix_case",
				NodeID:   fc.ID,
				Field:    "required_tests",
				Severity: "critical",
				Message:  fmt.Sprintf("DONE fix case %q has no required_tests — every completed fix must have at least one test proving the invariant holds", fc.ID),
			})
		}
		// DONE fix cases must have at least one invariant.
		if len(fc.TargetInvariants) == 0 {
			violations = append(violations, ShapeViolation{
				NodeType: "fix_case",
				NodeID:   fc.ID,
				Field:    "target_invariants",
				Severity: "warning",
				Message:  fmt.Sprintf("DONE fix case %q has no target_invariants — link to at least one invariant it protects", fc.ID),
			})
		}
		// DONE fix cases should have at least one fixed file or explicit notes.
		if len(fc.FixedFiles) == 0 && fc.Notes == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "fix_case",
				NodeID:   fc.ID,
				Field:    "fixed_files",
				Severity: "warning",
				Message:  fmt.Sprintf("DONE fix case %q has no fixed_files and no notes — add the implementation file(s) or document why they are absent", fc.ID),
			})
		}
	}
	return violations
}

// ValidateFailureModeShapes validates failure mode entries.
// forbiddenFixIDs is the set of known forbidden fix IDs for reference checking.
func ValidateFailureModeShapes(fms []FailureMode, forbiddenFixIDs map[string]bool) []ShapeViolation {
	var violations []ShapeViolation
	for _, fm := range fms {
		if fm.ID == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "failure_mode",
				NodeID:   "(missing id)",
				Field:    "id",
				Severity: "critical",
				Message:  "failure_mode is missing required field 'id'",
			})
			continue
		}
		if fm.Title == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "failure_mode",
				NodeID:   fm.ID,
				Field:    "title",
				Severity: "warning",
				Message:  fmt.Sprintf("failure_mode %q is missing 'title'", fm.ID),
			})
		}
		if len(fm.Symptoms) == 0 {
			violations = append(violations, ShapeViolation{
				NodeType: "failure_mode",
				NodeID:   fm.ID,
				Field:    "symptoms",
				Severity: "warning",
				Message:  fmt.Sprintf("failure_mode %q has no 'symptoms' — observable signals are required for diagnosis", fm.ID),
			})
		}
		if fm.RootCause == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "failure_mode",
				NodeID:   fm.ID,
				Field:    "root_cause",
				Severity: "warning",
				Message:  fmt.Sprintf("failure_mode %q is missing 'root_cause'", fm.ID),
			})
		}
		// Critical: fail if a referenced forbidden fix doesn't exist.
		for _, ffID := range fm.ForbiddenFixes {
			if ffID == "" {
				continue
			}
			if len(forbiddenFixIDs) > 0 && !forbiddenFixIDs[ffID] {
				violations = append(violations, ShapeViolation{
					NodeType: "failure_mode",
					NodeID:   fm.ID,
					Field:    "forbidden_fixes",
					Severity: "critical",
					Message:  fmt.Sprintf("failure_mode %q references forbidden fix %q which does not exist in forbidden_fixes.yaml", fm.ID, ffID),
				})
			}
		}
	}
	return violations
}

// ValidateForbiddenFixShapes validates forbidden fix entries.
func ValidateForbiddenFixShapes(fixes []ForbiddenFix) []ShapeViolation {
	var violations []ShapeViolation
	for _, ff := range fixes {
		if ff.ID == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "forbidden_fix",
				NodeID:   "(missing id)",
				Field:    "id",
				Severity: "critical",
				Message:  "forbidden_fix is missing required field 'id'",
			})
			continue
		}
		if ff.Summary == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "forbidden_fix",
				NodeID:   ff.ID,
				Field:    "summary",
				Severity: "warning",
				Message:  fmt.Sprintf("forbidden_fix %q is missing 'summary' (tempting_because explanation)", ff.ID),
			})
		}
		if ff.SafeAlternative == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "forbidden_fix",
				NodeID:   ff.ID,
				Field:    "safe_alternative",
				Severity: "warning",
				Message:  fmt.Sprintf("forbidden_fix %q is missing 'safe_alternative' — without it, engineers have no guidance on what to do instead", ff.ID),
			})
		}
	}
	return violations
}

// ValidateCausalRuleShapes validates causal rule entries.
func ValidateCausalRuleShapes(rules []CausalRule) []ShapeViolation {
	var violations []ShapeViolation
	for _, r := range rules {
		if r.ID == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "causal_rule",
				NodeID:   "(missing id)",
				Field:    "id",
				Severity: "warning",
				Message:  "causal_rule is missing required field 'id'",
			})
			continue
		}
		if r.RootSignal == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "causal_rule",
				NodeID:   r.ID,
				Field:    "root_signal",
				Severity: "warning",
				Message:  fmt.Sprintf("causal_rule %q is missing 'root_signal'", r.ID),
			})
		}
		if len(r.Sequence) == 0 {
			violations = append(violations, ShapeViolation{
				NodeType: "causal_rule",
				NodeID:   r.ID,
				Field:    "sequence",
				Severity: "warning",
				Message:  fmt.Sprintf("causal_rule %q has no 'sequence' — ordered event chain is required for causal reasoning", r.ID),
			})
		}
		if len(r.RecommendedFixOrder) == 0 {
			violations = append(violations, ShapeViolation{
				NodeType: "causal_rule",
				NodeID:   r.ID,
				Field:    "recommended_fix_order",
				Severity: "warning",
				Message:  fmt.Sprintf("causal_rule %q has no 'recommended_fix_order'", r.ID),
			})
		}
		if r.Confidence == "" {
			violations = append(violations, ShapeViolation{
				NodeType: "causal_rule",
				NodeID:   r.ID,
				Field:    "confidence",
				Severity: "warning",
				Message:  fmt.Sprintf("causal_rule %q is missing 'confidence' (low|medium|high)", r.ID),
			})
		}
	}
	return violations
}

// BuildForbiddenFixIDSet returns a set of known forbidden fix IDs for reference checking.
func BuildForbiddenFixIDSet(fixes []ForbiddenFix) map[string]bool {
	m := make(map[string]bool, len(fixes))
	for _, ff := range fixes {
		if ff.ID != "" {
			m[ff.ID] = true
		}
	}
	return m
}

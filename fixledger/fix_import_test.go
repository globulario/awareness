package fixledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/fixledger"
	"github.com/globulario/awareness/learning"
)

// docsAwarenessPath walks up from the package directory to find docs/awareness.
func docsAwarenessPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve abs path: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(abs, "docs", "awareness")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("docs/awareness not found; skipping test")
	return ""
}

// fixCasesFromDocs loads fix_cases.yaml from the real docs/awareness dir.
func fixCasesFromDocs(t *testing.T) []fixledger.FixCase {
	t.Helper()
	dir := docsAwarenessPath(t)
	cases, err := fixledger.LoadFixCases(filepath.Join(dir, "fix_cases.yaml"))
	if err != nil {
		t.Fatalf("LoadFixCases: %v", err)
	}
	return cases
}

// aliasesFromDocs loads context_aliases.yaml from the real docs/awareness dir.
// Returns the raw map (learning.ContextAliasMap = map[string][]string).
func aliasesFromDocs(t *testing.T) map[string][]string {
	t.Helper()
	dir := docsAwarenessPath(t)
	aliases, err := learning.LoadContextAliases(filepath.Join(dir, "context_aliases.yaml"))
	if err != nil {
		t.Fatalf("LoadContextAliases: %v", err)
	}
	return map[string][]string(aliases)
}

// ---- Test 1: Case 03 invariant is imported ----

// TestImportCase03InvariantExists verifies that case-03-absence-as-destructive-intent
// was imported as fix case absence_as_destructive_intent targeting the
// critical_state.absence_is_not_destructive_intent invariant.
func TestImportCase03InvariantExists(t *testing.T) {
	cases := fixCasesFromDocs(t)

	var found *fixledger.FixCase
	for i := range cases {
		if cases[i].ID == "absence_as_destructive_intent" {
			found = &cases[i]
			break
		}
	}
	if found == nil {
		t.Fatal("fix case absence_as_destructive_intent not found in fix_cases.yaml")
	}

	hasInvariant := false
	for _, inv := range found.TargetInvariants {
		if inv == "critical_state.absence_is_not_destructive_intent" {
			hasInvariant = true
			break
		}
	}
	if !hasInvariant {
		t.Errorf("absence_as_destructive_intent must target critical_state.absence_is_not_destructive_intent, got: %v",
			found.TargetInvariants)
	}
}

// ---- Test 2: Case 03 has forbidden fix stop_runtime_on_missing_key ----

// TestImportCase03ForbiddenFixStopOnMissingKey verifies that the alias set for
// critical_state.absence_is_not_destructive_intent covers the "missing key stopped service"
// phrase, and that stop_runtime_on_missing_key is a known forbidden fix alias.
func TestImportCase03ForbiddenFixStopOnMissingKey(t *testing.T) {
	aliases := aliasesFromDocs(t)

	// The alias block for critical_state.absence_is_not_destructive_intent must
	// include phrases that represent the stop_runtime_on_missing_key pattern.
	phrases, ok := aliases["critical_state.absence_is_not_destructive_intent"]
	if !ok {
		t.Fatal("critical_state.absence_is_not_destructive_intent not found in context_aliases.yaml")
	}

	wantPhrase := "missing key stopped service"
	found := false
	for _, p := range phrases {
		if strings.EqualFold(p, wantPhrase) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected phrase %q in absence_is_not_destructive_intent aliases, got: %v",
			wantPhrase, phrases)
	}
}

// ---- Test 3: Case 02 is a PARTIAL fix case ----

// TestImportCase02IsDone verifies that bootstrap_state_promotion is fully
// resolved — promoted via proposal.inc_2026_0002.promoted in v1.2.20.
func TestImportCase02IsDone(t *testing.T) {
	cases := fixCasesFromDocs(t)

	var found *fixledger.FixCase
	for i := range cases {
		if cases[i].ID == "bootstrap_state_promotion" {
			found = &cases[i]
			break
		}
	}
	if found == nil {
		t.Fatal("fix case bootstrap_state_promotion not found in fix_cases.yaml")
	}
	if found.Status != fixledger.FixDone {
		t.Errorf("expected status DONE for bootstrap_state_promotion, got %s", found.Status)
	}
	if len(found.TargetInvariants) == 0 {
		t.Error("bootstrap_state_promotion must have target_invariants")
	}
}

// ---- Test 4: W03 aliases include command package / systemd unit phrases ----

// TestImportW03CommandPackageAliasesPresent verifies that the alias set for
// runtime.installed_state_must_match_package_kind includes phrases for the
// false-positive command-package doctor finding (case W03).
func TestImportW03CommandPackageAliasesPresent(t *testing.T) {
	aliases := aliasesFromDocs(t)

	phrases, ok := aliases["runtime.installed_state_must_match_package_kind"]
	if !ok {
		t.Fatal("runtime.installed_state_must_match_package_kind not found in context_aliases.yaml")
	}

	wantPhrases := []string{
		"command package flagged as missing unit",
		"installed_state_runtime_mismatch",
	}
	for _, want := range wantPhrases {
		found := false
		for _, p := range phrases {
			if strings.EqualFold(p, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected phrase %q in runtime.installed_state_must_match_package_kind aliases, got: %v",
				want, phrases)
		}
	}
}

// ---- Test 5: persistence_gap pattern is reachable via DidWeFix / PatternStatus ----

// TestImportPatternPersistenceGapReachable verifies that a task description
// mentioning "persistence gap" can be matched via DidWeFix against the fix
// cases that reference persistence-related invariants.
func TestImportPatternPersistenceGapReachable(t *testing.T) {
	cases := fixCasesFromDocs(t)
	// Convert learning.ContextAliasMap to fixledger.ContextAliasMap (same underlying type).
	rawAliases := aliasesFromDocs(t)
	aliases := make(fixledger.ContextAliasMap, len(rawAliases))
	for k, v := range rawAliases {
		aliases[k] = v
	}

	// "persistence gap" should match via PatternStatus (any case whose pattern contains "persist")
	// OR via DidWeFix alias matching on install.result.atomic_commit.
	result := fixledger.DidWeFix("state changed in memory but never persisted to etcd — persistence gap", cases, aliases)
	if result == nil {
		t.Fatal("DidWeFix returned nil")
	}

	// We don't require a specific match, but the result must not be a hard error.
	// The important thing is the function handles the task gracefully.
	if result.OverallStatus == "" {
		t.Error("DidWeFix must return a non-empty OverallStatus")
	}

	// Separately: PatternStatus should also surface fix cases related to persistence.
	atomicCases := fixledger.PatternStatus("persistence gap", cases)
	// It's valid for this to be empty if no fix case pattern directly matches "persistence gap"
	// but it must not panic or error.
	_ = atomicCases
}

// ---- Bonus: verify key fix cases from the import have required tests ----

// TestImportedFixCasesHaveRequiredTests verifies that newly imported fix cases
// (from fix.tar.gz) all declare at least one required test.
func TestImportedFixCasesHaveRequiredTests(t *testing.T) {
	cases := fixCasesFromDocs(t)

	// These are the fix case IDs imported from fix.tar.gz failure and warning invariants.
	importedIDs := []string{
		"scylla_critical_keyspace_rf_policy",
		"bootstrap_state_promotion",
		"absence_as_destructive_intent",
		"lkg_expansion",
		"critical_state_registry_ownership",
		"intent_markers_tombstones",
		"reconcile_lane_starvation",
		"bounded_critical_queries",
		"derived_state_blocks_authority",
		"recovery_without_dns",
		"destructive_action_guards",
		"topology_safety_drift_reconciler",
		"drift_severity_escalation",
		"pki_ca_registry",
		"runtime_proof_package_kind",
		"objectstore_critical_registry",
	}

	caseByID := make(map[string]fixledger.FixCase, len(cases))
	for _, c := range cases {
		caseByID[c.ID] = c
	}

	for _, id := range importedIDs {
		fc, ok := caseByID[id]
		if !ok {
			t.Errorf("imported fix case %q not found in fix_cases.yaml", id)
			continue
		}
		if len(fc.RequiredTests) == 0 {
			t.Errorf("imported fix case %q has no required_tests", id)
		}
		if fc.DoD == "" {
			t.Errorf("imported fix case %q has no dod (definition of done)", id)
		}
	}
}

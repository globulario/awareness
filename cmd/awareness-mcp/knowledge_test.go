package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func writeYAML(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ── InvariantEntry: docs/awareness schema (summary:) ────────────────────────

func TestSearchInvariants_SummaryFieldIsSearchable(t *testing.T) {
	// docs/awareness format uses "summary:" not "description:"
	p := writeYAML(t, "invariants.yaml", `
invariants:
  - id: test.runtime_not_desired
    title: Runtime health must not be inferred from desired state
    severity: critical
    summary: |
      Desired state expresses intent. Runtime state expresses observed reality.
      Never treat desired.enabled=true as proof that a service is alive.
    enforcement: |
      Code that renders health indicators must read from the runtime authority.
`)
	entries := loadInvariants([]string{p})
	if len(entries) == 0 {
		t.Fatal("no invariants loaded")
	}

	// Search by a word that only appears in "summary:" — not in title or ID.
	hits := searchInvariants(entries, "expresses observed reality", 10)
	if len(hits) == 0 {
		t.Error("expected a match on summary field, got none — summary: is not being searched")
	}

	// Search by a word that only appears in "enforcement:".
	hits = searchInvariants(entries, "health indicators", 10)
	if len(hits) == 0 {
		t.Error("expected a match on enforcement field, got none — enforcement: is not being searched")
	}
}

func TestSearchInvariants_DescriptionFieldIsSearchable(t *testing.T) {
	// module-self format uses "description:"
	p := writeYAML(t, "invariants.yaml", `
invariants:
  - id: import.wall.maintained
    title: Standalone module must never import Globular packages
    severity: critical
    description: |
      The awareness module is project-agnostic and must remain importable
      without pulling in github.com/globulario/services dependencies.
`)
	entries := loadInvariants([]string{p})
	hits := searchInvariants(entries, "project-agnostic importable", 10)
	if len(hits) == 0 {
		t.Error("expected a match on description field, got none")
	}
}

// ── FailureModeEntry: docs/awareness schema ──────────────────────────────────

func TestSearchFailureModes_SummaryAndRootCauseSearchable(t *testing.T) {
	p := writeYAML(t, "failure_modes.yaml", `
failure_modes:
  - id: fm.industry.partial_failure_hidden_by_global_green
    title: Global healthy status conceals a degraded subsystem
    severity: high
    summary: The status aggregation defaults unknown to healthy.
    root_cause: |
      The collector silently omits data for an unreachable node.
      UNKNOWN is treated as HEALTHY in the rollup.
    symptoms:
      - Green dashboard with hidden degraded subsystem
    known_bad_fixes:
      - Default unknown to healthy to reduce noise
`)
	entries := loadFailureModes([]string{p})
	if len(entries) == 0 {
		t.Fatal("no failure modes loaded")
	}

	// Match via root_cause (previously invisible).
	hits := searchFailureModes(entries, "silently omits unreachable node", 10)
	if len(hits) == 0 {
		t.Error("expected match on root_cause field, got none — root_cause: is not being searched")
	}

	// Match via known_bad_fixes (mapped as known_bad_fixes:).
	hits = searchFailureModes(entries, "default unknown to healthy", 10)
	if len(hits) == 0 {
		t.Error("expected match on known_bad_fixes field, got none — known_bad_fixes: is not being searched")
	}

	// Match via symptoms (was already working).
	hits = searchFailureModes(entries, "degraded subsystem dashboard", 10)
	if len(hits) == 0 {
		t.Error("expected match on symptoms field, got none")
	}
}

// ── ForbiddenFixEntry: docs/awareness schema ─────────────────────────────────

func TestSearchForbiddenFixes_SummaryAndSafeAlternativeSearchable(t *testing.T) {
	p := writeYAML(t, "forbidden_fixes.yaml", `
forbidden_fixes:
  - id: forbidden.runtime_from_desired
    summary: |
      Infer runtime health from desired state instead of a live probe.
    safe_alternative: |
      Read runtime health from the systemd unit state or gRPC health RPC.
      Desired state must never answer: is it running right now?
`)
	fixes := loadForbiddenFixes([]string{p})
	if len(fixes) == 0 {
		t.Fatal("no forbidden fixes loaded")
	}

	entry := fixes[0]
	if entry.Summary == "" {
		t.Error("Summary field not populated — yaml:'summary' tag not working")
	}
	if entry.SafeAlternative == "" {
		t.Error("SafeAlternative field not populated — yaml:'safe_alternative' tag not working")
	}

	// Match via summary (previously invisible — only ID matched).
	terms := knowledgeTerms("infer runtime health desired state")
	blob := strings.ToLower(strings.Join([]string{
		entry.ID, entry.Title, entry.Summary, entry.Description,
		entry.SafeAlternative, entry.CorrectApproach,
	}, " "))
	if countMatches(blob, terms) == 0 {
		t.Error("expected match on summary+safe_alternative, got none")
	}
}

// ── Cross-link fields: related_principles, related_industry_patterns ─────────

func TestSearchInvariants_RelatedPrinciplesSearchable(t *testing.T) {
	p := writeYAML(t, "invariants.yaml", `
invariants:
  - id: ui.visible_state_requires_authority
    title: Every visible UI state must be bound to an explicit authority
    severity: critical
    summary: A badge that shows state without binding to an authority is displaying guesswork.
    related_principles:
      - state.unknown_must_not_default_to_healthy
      - evidence.missing_is_not_known_bad
`)
	entries := loadInvariants([]string{p})
	if len(entries) == 0 {
		t.Fatal("no invariants loaded")
	}
	if len(entries[0].RelatedPrinciples) == 0 {
		t.Error("RelatedPrinciples not populated — yaml:'related_principles' tag not working")
	}
	// Search by a principle ID that only appears in related_principles.
	hits := searchInvariants(entries, "state.unknown_must_not_default_to_healthy", 10)
	if len(hits) == 0 {
		t.Error("expected match on related_principles field, got none — related_principles: not being searched")
	}
}

func TestSearchFailureModes_RelatedIndustryPatternsSearchable(t *testing.T) {
	p := writeYAML(t, "failure_modes.yaml", `
failure_modes:
  - id: fm.globular.objectstore_partial_snapshot
    title: Partial snapshot classifies absent nodes as known-down
    severity: critical
    summary: False quorum-loss finding from incomplete snapshot.
    related_industry_patterns:
      - fm.industry.missing_inventory_misclassified_as_down
      - fm.industry.partial_failure_hidden_by_global_green
    related_globular_failure_modes: []
`)
	entries := loadFailureModes([]string{p})
	if len(entries) == 0 {
		t.Fatal("no failure modes loaded")
	}
	if len(entries[0].RelatedIndustryPatterns) == 0 {
		t.Error("RelatedIndustryPatterns not populated — yaml:'related_industry_patterns' tag not working")
	}
	// Search by an industry pattern ID that only appears in related_industry_patterns.
	hits := searchFailureModes(entries, "missing_inventory_misclassified_as_down", 10)
	if len(hits) == 0 {
		t.Error("expected match on related_industry_patterns field, got none")
	}
}

func TestSearchFailureModes_RelatedGlobularFMsSearchable(t *testing.T) {
	p := writeYAML(t, "failure_modes.yaml", `
failure_modes:
  - id: fm.industry.missing_inventory_misclassified_as_down
    title: Collector gap interpreted as known-down
    severity: critical
    summary: Absent NodeRecord is misclassified as proof of failure.
    related_globular_failure_modes:
      - fm.globular.objectstore_partial_snapshot
`)
	entries := loadFailureModes([]string{p})
	if len(entries) == 0 {
		t.Fatal("no failure modes loaded")
	}
	if len(entries[0].RelatedGlobularFMs) == 0 {
		t.Error("RelatedGlobularFMs not populated — yaml:'related_globular_failure_modes' tag not working")
	}
	hits := searchFailureModes(entries, "objectstore_partial_snapshot", 10)
	if len(hits) == 0 {
		t.Error("expected match on related_globular_failure_modes field, got none")
	}
}

func TestSearchForbiddenFixes_RelatedFailureModesSearchable(t *testing.T) {
	p := writeYAML(t, "forbidden_fixes.yaml", `
forbidden_fixes:
  - id: forbidden.missing_inventory_as_down
    summary: Treat missing inventory as known-down evidence.
    safe_alternative: Use UNKNOWN bucket; only KNOWN_BAD counts toward severity.
    related_failure_modes:
      - fm.industry.missing_inventory_misclassified_as_down
      - fm.globular.objectstore_partial_snapshot
`)
	fixes := loadForbiddenFixes([]string{p})
	if len(fixes) == 0 {
		t.Fatal("no forbidden fixes loaded")
	}
	if len(fixes[0].RelatedFailureModes) == 0 {
		t.Error("RelatedFailureModes not populated — yaml:'related_failure_modes' tag not working")
	}
	// Search by a failure mode ID that only appears in related_failure_modes.
	terms := knowledgeTerms("objectstore_partial_snapshot")
	blob := strings.ToLower(strings.Join([]string{
		fixes[0].ID, fixes[0].Summary, fixes[0].SafeAlternative,
		strings.Join(fixes[0].RelatedFailureModes, " "),
	}, " "))
	if countMatches(blob, terms) == 0 {
		t.Error("expected match on related_failure_modes, got none")
	}
}

// ── Module-self format: ForbiddenFixEntry ──────────────────────────────────

func TestLoadForbiddenFixes_ModuleSelfFormat(t *testing.T) {
	// .awareness/forbidden_fixes.yaml uses "description:" and "correct_approach:"
	p := writeYAML(t, "forbidden_fixes.yaml", `
forbidden_fixes:
  - id: no.hardcode.services.import
    summary: Never add a direct import of github.com/globulario/services
    description: |
      When a feature requires runtime cluster data, define a method on
      the Adapter interface instead of importing services directly.
    correct_approach: |
      Add a method to the Adapter interface. Implement NullAdapter to return
      ErrRuntimeDisabled. Implement GlobularAdapter in the services repo.
`)
	fixes := loadForbiddenFixes([]string{p})
	if len(fixes) == 0 {
		t.Fatal("no forbidden fixes loaded")
	}
	if fixes[0].CorrectApproach == "" {
		t.Error("CorrectApproach not populated — yaml:'correct_approach' tag not working")
	}
}

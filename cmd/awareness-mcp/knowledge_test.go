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

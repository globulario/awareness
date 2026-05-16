package preflight_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/preflight"
)

func makeTestReport() *preflight.Report {
	return &preflight.Report{
		Task:           "desired_hash mismatch after deploy",
		Classification: []preflight.TaskClass{preflight.ClassStateMismatch, preflight.ClassConvergenceRisk},
		MatchedAliases: []string{"infra.desired_hash_consistency"},
		Services:       []string{"envoy", "cluster-controller"},
		Invariants:     []string{"infra.desired_hash_consistency"},
		FailureModes:   []string{"failure_mode.desired_hash_restart_storm"},
		ForbiddenFixes: []string{"use raw artifact digest as desired_hash"},
		DidWeFix: &preflight.DidWeFixSection{
			Status:          "PARTIAL",
			MatchedPatterns: []string{"desired_hash"},
			FixCases:        []string{"desired_hash_consistency"},
			RemainingGaps:   []string{"golang/awareness/analysis/hash.go"},
			NextAction:      "complete partial fix before closing",
		},
		RequiredTests:    []string{"TestDriftWorkflowUsesDesiredHash"},
		RequiredSearches: []string{"ComputeInfrastructureDesiredHash"},
		RecommendedOrder: []string{"Check desired-hash computation", "Verify installed-state stamping"},
		AgentInstruction: "This task is architecture-sensitive. Do not apply a local fix.",
		Warnings:         []string{},
		Cycles:           []preflight.CycleWarning{},
	}
}

func TestJSONOutputIsValidAndStable(t *testing.T) {
	r := makeTestReport()
	out, err := preflight.Render(r, preflight.FormatJSON)
	if err != nil {
		t.Fatalf("Render JSON: %v", err)
	}

	// Must be valid JSON.
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}

	// Required keys present.
	for _, key := range []string{
		"task", "classification", "matched_aliases", "services", "invariants",
		"failure_modes", "forbidden_fixes", "did_we_fix", "required_tests",
		"required_searches", "recommended_investigation_order", "agent_instruction",
		"warnings", "cycles",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing key %q", key)
		}
	}

	// Render again — must be identical (stable).
	out2, _ := preflight.Render(r, preflight.FormatJSON)
	if out != out2 {
		t.Error("JSON render is not stable across two calls")
	}
}

func TestJSONOutputIncludesTrustEnvelope(t *testing.T) {
	r := makeTestReport()
	r.Trust = &assurance.TrustEnvelope{
		Verdict:         assurance.TrustLimited,
		Confidence:      assurance.ConfidenceLow,
		Freshness:       assurance.FreshnessStaleUnknown,
		Coverage:        assurance.TrustCoveragePartial,
		Limitations:     []string{"graph stale"},
		RequiredActions: []string{"rebuild graph"},
	}
	out, err := preflight.Render(r, preflight.FormatJSON)
	if err != nil {
		t.Fatalf("Render JSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	tr, ok := m["trust"].(map[string]any)
	if !ok {
		t.Fatalf("missing trust object: %v", m["trust"])
	}
	if tr["verdict"] != string(assurance.TrustLimited) {
		t.Fatalf("trust.verdict=%v", tr["verdict"])
	}
}

func TestMarkdownContainsAllSections(t *testing.T) {
	r := makeTestReport()
	out, err := preflight.Render(r, preflight.FormatMarkdown)
	if err != nil {
		t.Fatalf("Render Markdown: %v", err)
	}

	sections := []string{
		"# Globular Awareness Preflight",
		"## Task",
		"## Classification",
		"## Matched awareness",
		"## Did we already fix this?",
		"## Relevant invariants",
		"## Known failure modes",
		"## Forbidden fixes",
		"## Impacted files",
		"## Package admission",
		"## Dependency/cycle risks",
		"## Required tests",
		"## Required searches",
		"## Recommended investigation order",
		"## Agent instruction",
		"## Do not do",
	}
	for _, sec := range sections {
		if !strings.Contains(out, sec) {
			t.Errorf("Markdown missing section: %q", sec)
		}
	}
}

func TestAgentFormatContainsForbiddenFixes(t *testing.T) {
	r := makeTestReport()
	out, err := preflight.Render(r, preflight.FormatAgent)
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}

	if !strings.Contains(out, "AGENT PREFLIGHT RESULT") {
		t.Error("agent format missing header")
	}
	if !strings.Contains(out, "Forbidden fixes:") {
		t.Error("agent format missing Forbidden fixes section")
	}
	if !strings.Contains(out, "use raw artifact digest as desired_hash") {
		t.Error("agent format missing forbidden fix text")
	}
	if !strings.Contains(out, "architecture-sensitive") {
		t.Error("agent format missing architecture-sensitive warning")
	}
}

func TestAgentFormatIsDirective(t *testing.T) {
	r := makeTestReport()
	r.Classification = []preflight.TaskClass{preflight.ClassRestartStorm, preflight.ClassConvergenceRisk}
	out, _ := preflight.Render(r, preflight.FormatAgent)

	if !strings.Contains(out, "Restart storm detected") {
		t.Error("agent format must mention restart storm directive")
	}
}

func TestAgentFormatIncludesFalseSilenceWarning(t *testing.T) {
	r := makeTestReport()
	r.Warnings = []string{"NO_AWARENESS_MATCH: no awareness facts matched this task. This does not prove the task is safe."}
	out, _ := preflight.Render(r, preflight.FormatAgent)
	if !strings.Contains(out, "NO_AWARENESS_MATCH") {
		t.Fatalf("expected false-silence warning in agent format, got: %s", out)
	}
}

func TestAgentFormatSummarizesLongLists(t *testing.T) {
	r := makeTestReport()
	r.ForbiddenFixes = []string{
		"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10",
	}
	out, err := preflight.Render(r, preflight.FormatAgent)
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}
	if !strings.Contains(out, "- f8") {
		t.Fatalf("expected top list item in output, got: %s", out)
	}
	if strings.Contains(out, "- f10") {
		t.Fatalf("expected long list truncation in output, got: %s", out)
	}
	if !strings.Contains(out, "... 2 more (use --format json for full list)") {
		t.Fatalf("expected truncation summary in output, got: %s", out)
	}
}

func TestAgentFormatFullVerbosityShowsAllItems(t *testing.T) {
	r := makeTestReport()
	r.ForbiddenFixes = []string{
		"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10",
	}
	out, err := preflight.RenderWithOptions(r, preflight.FormatAgent, preflight.RenderOptions{
		Verbosity: preflight.VerbosityFull,
	})
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}
	if !strings.Contains(out, "- f10") {
		t.Fatalf("expected full list item in output, got: %s", out)
	}
	if strings.Contains(out, "more (use --format json for full list)") {
		t.Fatalf("did not expect truncation summary in full mode, got: %s", out)
	}
}

func TestAgentFormatShowsStaticOnlyConfidenceBanner(t *testing.T) {
	r := makeTestReport()
	r.Coverage.Runtime = preflight.CoverageNoop
	r.Coverage.IncidentStore = preflight.CoverageNotChecked
	out, err := preflight.Render(r, preflight.FormatAgent)
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}
	if !strings.Contains(out, "Static-only confidence: runtime/incident evidence not fully checked in this run.") {
		t.Fatalf("expected static-only confidence banner, got: %s", out)
	}
}

func TestAgentFormatRanksTaskRelevantItemsFirst(t *testing.T) {
	r := makeTestReport()
	r.Task = "mcp orphan port issue"
	r.ForbiddenFixes = []string{
		"generic_unrelated_fix",
		"restart_mcp_without_killing_orphan",
		"another_unrelated_fix",
	}
	out, err := preflight.RenderWithOptions(r, preflight.FormatAgent, preflight.RenderOptions{
		Verbosity: preflight.VerbosityCompact,
	})
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}
	idxRelevant := strings.Index(out, "restart_mcp_without_killing_orphan")
	idxUnrelated := strings.Index(out, "generic_unrelated_fix")
	if idxRelevant == -1 || idxUnrelated == -1 {
		t.Fatalf("missing expected items in output: %s", out)
	}
	if idxRelevant > idxUnrelated {
		t.Fatalf("expected relevant item ranked before unrelated item, got: %s", out)
	}
}

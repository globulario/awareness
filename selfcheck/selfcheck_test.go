package selfcheck_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/selfcheck"
)

// makeGraph builds a minimal in-memory awareness graph seeded with the
// invariants required by the smoke cases.
func makeGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	ctx := context.Background()

	invariants := []graph.Invariant{
		{
			ID:       "awareness.annotation_scanner.production_source_only",
			Title:    "Annotation scanner must restrict to production source",
			Severity: "high",
			Summary:  "Annotation validator must scan production source only, not test fixtures. annotation validator false positive check in production source.",
		},
		{
			ID:       "infra.desired_hash_consistency",
			Title:    "Desired hash must be computed consistently",
			Severity: "critical",
			Summary:  "ComputeInfrastructureDesiredHash must use identical inputs on both sides. desired_hash mismatch after deploy causes permanent non-convergence. use_raw_artifact_digest_as_desired_hash is forbidden.",
		},
		{
			ID:       "infra.heartbeat_not_desired_authority",
			Title:    "Heartbeat must not set desired state",
			Severity: "critical",
			Summary:  "Heartbeat must never write desired state entry. runtime observation created desired state is a violation of controller authority.",
		},
		{
			ID:       "critical_state.absence_is_not_destructive_intent",
			Title:    "Absent etcd key is not a delete command",
			Severity: "critical",
			Summary:  "A missing etcd key stopped running service must not be treated as intent to delete. Absence is not destructive intent.",
		},
		{
			ID:       "runtime.installed_state_must_match_package_kind",
			Title:    "Runtime proof must match package kind",
			Severity: "high",
			Summary:  "COMMAND package missing systemd unit file is expected — not a failure. Package kind is authority for runtime proof mismatch expectations.",
		},
		{
			ID:       "awareness.no_false_silence_for_sensitive_tasks",
			Title:    "Awareness must not be silent for sensitive tasks",
			Severity: "high",
			Summary:  "Awareness must surface at least one invariant for architectural tasks.",
		},
		{
			ID:       "awareness.mcp_must_not_expose_promotion",
			Title:    "MCP must not expose promote_proposal",
			Severity: "critical",
			Summary:  "The MCP server must never register promote_proposal. Promotion is CLI-only.",
		},
	}

	for _, inv := range invariants {
		if err := g.UpsertInvariant(ctx, inv); err != nil {
			t.Fatalf("UpsertInvariant %s: %v", inv.ID, err)
		}
	}

	// Seed forbidden fix node + edge for smoke case 2 (desired_hash_mismatch).
	// collectForbiddenFixes traverses EdgeForbids edges from invariant → forbidden_fix nodes.
	ffNode := graph.Node{
		ID:      "forbidden_fix:use_raw_artifact_digest_as_desired_hash",
		Type:    graph.NodeTypeForbiddenFix,
		Name:    "use_raw_artifact_digest_as_desired_hash",
		Summary: "Never use the raw artifact digest as the desired_hash value — it is not stable across builds.",
	}
	if err := g.AddNode(ctx, ffNode); err != nil {
		t.Fatalf("AddNode forbidden_fix: %v", err)
	}
	if err := g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:infra.desired_hash_consistency",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:use_raw_artifact_digest_as_desired_hash",
	}); err != nil {
		t.Fatalf("AddEdge forbids: %v", err)
	}

	return g
}

// makeDocsDir creates a temporary docs/awareness directory with context aliases
// that give smoke case tasks a chance to keyword-match invariants.
func makeDocsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	aliases := `context_aliases:
  infra.desired_hash_consistency:
    - desired_hash
    - hash mismatch
    - hash never converges
  infra.heartbeat_not_desired_authority:
    - heartbeat writes desired
    - heartbeat desired state
    - runtime observation created desired
  critical_state.absence_is_not_destructive_intent:
    - missing etcd key stopped
    - absence not treated as intent
  runtime.installed_state_must_match_package_kind:
    - COMMAND package missing systemd
    - runtime proof mismatch
  awareness.annotation_scanner.production_source_only:
    - annotation validator false positive
    - annotation scanner production source
`
	if err := os.WriteFile(filepath.Join(dir, "context_aliases.yaml"), []byte(aliases), 0o644); err != nil {
		t.Fatalf("write context_aliases.yaml: %v", err)
	}
	for _, f := range []string{"fix_cases.yaml", "guardrails.yaml"} {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("{}\n"), 0o644)
	}
	return dir
}

// ── Test 1: self-check passes on known smoke cases ────────────────────────────

func TestSmokePassesOnKnownInvariants(t *testing.T) {
	g := makeGraph(t)
	defer g.Close()

	docsDir := makeDocsDir(t)
	opts := selfcheck.Options{DocsDir: docsDir}

	r, err := selfcheck.Run(context.Background(), opts, g)
	if err != nil {
		t.Fatalf("selfcheck.Run: %v", err)
	}

	for _, cr := range r.Checks {
		if cr.Kind != selfcheck.KindSmoke {
			continue
		}
		if cr.Status == selfcheck.StatusFail {
			t.Errorf("smoke %q FAIL — false silences: %v", cr.Name, cr.FalseSilences)
		}
	}
}

// ── Test 2: self-check reports failure when expected invariant is missing ──────

func TestSmokeMissingInvariantReportsFalseSilence(t *testing.T) {
	// Empty graph — no invariants, keyword matching also empty.
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	docsDir := t.TempDir()
	for _, f := range []string{"context_aliases.yaml", "fix_cases.yaml", "guardrails.yaml"} {
		_ = os.WriteFile(filepath.Join(docsDir, f), []byte("{}\n"), 0o644)
	}

	opts := selfcheck.Options{DocsDir: docsDir}
	r, err := selfcheck.Run(context.Background(), opts, g)
	if err != nil {
		t.Fatalf("selfcheck.Run: %v", err)
	}

	smokeFailCount := 0
	for _, cr := range r.Checks {
		if cr.Kind == selfcheck.KindSmoke && cr.Status == selfcheck.StatusFail {
			smokeFailCount++
		}
	}
	if smokeFailCount == 0 {
		t.Error("expected at least one smoke FAIL with empty graph, got none")
	}
	if len(r.FalseSilences) == 0 {
		t.Error("expected non-empty FalseSilences with empty graph")
	}
}

// ── Test 3: self-check reports MCP failure if promote_proposal is exposed ──────

func TestMCPCheckFailsWhenPromoteProposalExposed(t *testing.T) {
	// The standalone awareness/mcp server was removed in v1.2.20.
	// The invariant (awareness.mcp_must_not_expose_promotion) is now enforced
	// in golang/mcp/proposal_drain_tool.go and verified via source inspection
	// by selfcheck.checkMCPDiscovery.
	//
	// Run full self-check with a DocsDir so the source inspection can locate
	// the MCP registration file.
	repoRoot, err := findRepoRoot(t)
	if err != nil {
		t.Skipf("repo root not found (%v) — skipping MCP discovery check", err)
	}
	opts := selfcheck.Options{
		DocsDir: filepath.Join(repoRoot, "docs", "awareness"),
	}
	r, runErr := selfcheck.Run(context.Background(), opts, nil)
	if runErr != nil {
		t.Fatalf("selfcheck.Run: %v", runErr)
	}

	found := false
	for _, cr := range r.Checks {
		if cr.Kind == selfcheck.KindMCPDiscovery {
			found = true
			if cr.Status == selfcheck.StatusFail {
				t.Errorf("MCP discovery check FAILED: %s", cr.Detail)
			}
		}
	}
	if !found {
		t.Error("KindMCPDiscovery check not found in report")
	}
}

func findRepoRoot(t *testing.T) (string, error) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// ── Test 4: --create-incident writes bundle, no proposal generated ─────────────

func TestCreateIncidentWritesBundleNotProposal(t *testing.T) {
	r := &selfcheck.Report{
		GeneratedAt:          time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Pass:                 false,
		ShouldCreateIncident: true,
		FalseSilences:        []string{"smoke:desired_hash_mismatch — infra.desired_hash_consistency not surfaced"},
		RecommendedFixes:     []string{"add context alias for desired_hash task"},
		Checks: []selfcheck.CheckResult{
			{
				Kind:   selfcheck.KindSmoke,
				Name:   "smoke:desired_hash_mismatch",
				Status: selfcheck.StatusFail,
				Detail: "expected invariant infra.desired_hash_consistency not surfaced",
			},
		},
	}

	docsDir := t.TempDir()

	path, err := selfcheck.CreateIncidentBundle(r, docsDir)
	if err != nil {
		t.Fatalf("CreateIncidentBundle: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("incident file not created at %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read incident: %v", err)
	}
	content := string(data)

	// Must not contain proposal or promotion content.
	for _, forbidden := range []string{"proposed:", "APPROVED", "PROMOTED", "promote_proposal"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("incident bundle contains forbidden content %q", forbidden)
		}
	}

	// Must be written under incidents/ subdirectory.
	if !strings.Contains(path, "incidents") {
		t.Errorf("incident path %q is not under incidents/", path)
	}

	// Must be a plain YAML file (not a Go source, not a proposal).
	if !strings.HasSuffix(path, ".yaml") {
		t.Errorf("incident path %q is not a .yaml file", path)
	}
}

// ── Test 5: self-check never promotes awareness law ───────────────────────────

func TestSelfCheckNeverPromotes(t *testing.T) {
	g := makeGraph(t)
	defer g.Close()

	docsDir := t.TempDir()
	for _, f := range []string{"context_aliases.yaml", "fix_cases.yaml", "guardrails.yaml"} {
		_ = os.WriteFile(filepath.Join(docsDir, f), []byte("{}\n"), 0o644)
	}

	_, err := selfcheck.Run(context.Background(), selfcheck.Options{DocsDir: docsDir}, g)
	if err != nil {
		t.Fatalf("selfcheck.Run: %v", err)
	}

	// Protected awareness truth files must not be created or modified.
	for _, protected := range []string{"invariants.yaml", "failure_modes.yaml", "forbidden_fixes.yaml"} {
		path := filepath.Join(docsDir, protected)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("self-check wrote protected file %s — this is forbidden", path)
		}
	}
}

// ── Test 6: report renders valid JSON ─────────────────────────────────────────

func TestReportRendersValidJSON(t *testing.T) {
	r := &selfcheck.Report{
		GeneratedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Pass:        true,
		Checks: []selfcheck.CheckResult{
			{Kind: selfcheck.KindBuild, Name: "graph_db_exists_and_recent",
				Status: selfcheck.StatusPass, Detail: "graph.db OK"},
			{Kind: selfcheck.KindMCPDiscovery, Name: "mcp_promote_not_exposed",
				Status: selfcheck.StatusPass, Detail: "no promotion tools exposed"},
		},
	}

	out, err := selfcheck.Render(r, selfcheck.FormatJSON)
	if err != nil {
		t.Fatalf("Render JSON: %v", err)
	}
	if out == "" {
		t.Fatal("Render produced empty output")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Errorf("Render produced invalid JSON: %v\noutput:\n%s", err, out)
	}
}

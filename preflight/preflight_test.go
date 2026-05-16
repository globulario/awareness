package preflight_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// seedPreflightGraph creates a minimal in-memory graph for preflight tests.
func seedPreflightGraph(t *testing.T) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	// Invariants.
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "infra.desired_hash_consistency",
		Title:    "Desired hash must be stable across convergence ticks",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID: "invariant:infra.desired_hash_consistency", Type: graph.NodeTypeInvariant,
		Name: "infra.desired_hash_consistency",
	})

	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "convergence.no_infinite_retry",
		Title:    "No infinite retry",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID: "invariant:convergence.no_infinite_retry", Type: graph.NodeTypeInvariant,
		Name: "convergence.no_infinite_retry",
	})

	// Failure modes.
	_ = g.UpsertFailureMode(ctx, graph.FailureMode{
		ID:    "failure_mode.desired_hash_restart_storm",
		Title: "Desired hash instability causes restart storm",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID: "failure_mode:failure_mode.desired_hash_restart_storm", Type: graph.NodeTypeFailureMode,
		Name: "failure_mode.desired_hash_restart_storm",
	})

	// Forbidden fix.
	_ = g.AddNode(ctx, graph.Node{
		ID: "forbidden_fix:use_raw_digest", Type: graph.NodeTypeForbiddenFix,
		Name: "use raw artifact digest as desired_hash",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:infra.desired_hash_consistency",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:use_raw_digest",
	})

	// Required test.
	_ = g.AddNode(ctx, graph.Node{
		ID: "test:TestDriftWorkflowUsesDesiredHash", Type: graph.NodeTypeTest,
		Name: "TestDriftWorkflowUsesDesiredHash",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:infra.desired_hash_consistency",
		Kind: graph.EdgeVerifiedBy,
		Dst:  "test:TestDriftWorkflowUsesDesiredHash",
	})

	// Services.
	for _, svc := range []string{"envoy", "cluster-controller", "node-agent"} {
		_ = g.AddNode(ctx, graph.Node{ID: "service:" + svc, Type: graph.NodeTypeGlobularService, Name: svc})
	}

	return g
}

// setupPreflightDocsDir creates a minimal docs/awareness dir with context aliases and fix cases.
func setupPreflightDocsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// context_aliases.yaml.
	aliases := `aliases:
  infra.desired_hash_consistency:
    - desired_hash
    - checksum mismatch
    - hash mismatch
  convergence.no_infinite_retry:
    - retry loop
    - infinite retry
`
	_ = os.WriteFile(filepath.Join(dir, "context_aliases.yaml"), []byte(aliases), 0o644)

	// fix_cases.yaml.
	fixCases := `fix_cases:
  - id: desired_hash_consistency
    title: "Desired hash consistency fix"
    status: PARTIAL
    pattern: "desired_hash"
    target_invariants:
      - infra.desired_hash_consistency
    fixed_files:
      - golang/cluster_controller/convergence.go
    remaining_files:
      - golang/awareness/analysis/hash.go
    required_tests:
      - TestDriftWorkflowUsesDesiredHash
`
	_ = os.WriteFile(filepath.Join(dir, "fix_cases.yaml"), []byte(fixCases), 0o644)

	// guardrails.yaml (empty — preflight just loads it).
	_ = os.WriteFile(filepath.Join(dir, "guardrails.yaml"), []byte("guardrails: []\n"), 0o644)

	return dir
}

func TestPreflightDetectsStateMismatchClassification(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash mismatch between controller and node-agent after deploy",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertHasClass(t, r.Classification, preflight.ClassStateMismatch)
	assertHasClass(t, r.Classification, preflight.ClassConvergenceRisk)
}

func TestPreflightDetectsRestartStormClassification(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "envoy restart storm, start-limit-hit after SIGTERM flood",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertHasClass(t, r.Classification, preflight.ClassRestartStorm)
	assertHasClass(t, r.Classification, preflight.ClassConvergenceRisk)
}

func TestPreflightIncludesAliases(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "checksum mismatch between installed and desired",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.MatchedAliases) == 0 {
		t.Error("expected matched aliases for 'checksum mismatch'")
	}
	found := false
	for _, a := range r.MatchedAliases {
		if strings.Contains(a, "desired_hash_consistency") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected infra.desired_hash_consistency alias, got %v", r.MatchedAliases)
	}
}

func TestPreflightIncludesAgentContextInvariants(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent across convergence ticks",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.Invariants) == 0 {
		t.Error("expected invariants from agent context")
	}
}

func TestPreflightIncludesDidWeFixResult(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "fix desired_hash computation drift",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.DidWeFix == nil {
		t.Fatal("expected DidWeFix section")
	}
	if r.DidWeFix.Status == "" {
		t.Error("expected DidWeFix.Status to be set")
	}
}

func TestPreflightIncludesRequiredTests(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "fix desired_hash computation drift",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Required tests can come from graph (via DidWeFix fix case) or agent context.
	if len(r.RequiredTests) == 0 && (r.DidWeFix == nil || len(r.DidWeFix.FixCases) == 0) {
		t.Error("expected required tests or fix cases in preflight result")
	}
}

func TestPreflightIncludesForbiddenFixes(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.ForbiddenFixes) == 0 {
		t.Error("expected forbidden fixes from graph (use raw artifact digest as desired_hash)")
	}
}

func TestPreflightWithFileIncludesImpactResult(t *testing.T) {
	ctx := context.Background()
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	// Add a source file node and link it to an invariant.
	_ = g.AddNode(ctx, graph.Node{
		ID: "source_file:golang/cluster_controller/convergence.go", Type: graph.NodeTypeSourceFile,
		Name: "convergence.go", Path: "golang/cluster_controller/convergence.go",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "source_file:golang/cluster_controller/convergence.go",
		Kind: graph.EdgeDefines,
		Dst:  "invariant:infra.desired_hash_consistency",
	})

	r, err := preflight.Run(ctx, preflight.Options{
		Task:    "refactor convergence logic",
		Files:   []string{"golang/cluster_controller/convergence.go"},
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Invariant should appear from impact analysis.
	found := false
	for _, inv := range r.Invariants {
		if strings.Contains(inv, "desired_hash") {
			found = true
		}
	}
	if !found {
		t.Errorf("impact analysis did not surface desired_hash invariant, got: %v", r.Invariants)
	}
}

func TestPreflightWithPackageIncludesAdmissionResult(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	// Point to the real node-agent package which has an awareness.yaml.
	pkgPath := "/home/dave/Documents/github.com/globulario/packages/metadata/node-agent"
	if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
		t.Skip("node-agent package not available")
	}

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:        "update node-agent package",
		PackagePath: pkgPath,
		DocsDir:     docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.PackageAdmission == nil {
		t.Fatal("expected package admission section")
	}
	if r.PackageAdmission.Status == "" {
		t.Error("expected package admission status to be set")
	}
	assertHasClass(t, r.Classification, preflight.ClassPackageAdmission)
}

func TestPreflightArchitectureSensitiveNoMatchesIsUnknownImpact(t *testing.T) {
	docsDir := setupPreflightDocsDir(t)
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "reconcile desired installed runtime behavior for component foo",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertHasClass(t, r.Classification, preflight.ClassArchitectureSensitive)
	assertHasClass(t, r.Classification, preflight.ClassUnknownImpact)
}

func TestPreflightLocalNoMatchesNotUnknownImpact(t *testing.T) {
	docsDir := setupPreflightDocsDir(t)
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "rename helper variable in local utility function",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range r.Classification {
		if c == preflight.ClassUnknownImpact {
			t.Fatalf("did not expect UNKNOWN_IMPACT for local harmless task, got %v", r.Classification)
		}
	}
}

func TestPreflightNilGraphProducesWarning(t *testing.T) {
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "some task",
		DocsDir: docsDir,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	hasWarning := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "no graph DB") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Errorf("expected 'no graph DB' warning, got: %v", r.Warnings)
	}
}

// TestPreflightOrdering_StaleGraphProducesUnknownNotSafe pins the implicit
// ordering contract documented in docs/awareness/composed_path_failures.md
// (2026-05-10 preflight ordering): Coverage MUST be assigned before
// SafetyStatus and DegradedMode are computed. Both helpers read
// r.Coverage.Graph; if they run on a zero-valued Coverage, they silently
// skip the stale-graph branch and the report lies about safety.
//
// Fixture: graph_builds row 25h old → triggers MaxAgeExceeded → graph
// freshness reports Stale → computeConfidence sets Coverage.Graph =
// CoverageStale. Architecture-sensitive task + no runtime included +
// stale graph must produce SafetyStatusUnknownNotSafe and
// DegradedMode.Enabled = true.
//
// If this test ever fails, the joined report is lying about safety on a
// stale graph; the ordering has been broken or the helpers stopped reading
// Coverage. Fix it before shipping.
func TestPreflightOrdering_StaleGraphProducesUnknownNotSafe(t *testing.T) {
	ctx := context.Background()
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	// Insert a graph_builds row 25h old so freshness flags MaxAgeExceeded.
	staleTs := time.Now().Add(-25 * time.Hour).Unix()
	if _, err := g.DB().ExecContext(ctx,
		`INSERT INTO graph_builds (id, repo_root, git_commit, release_id, created_at, stats_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"ordering-test-stale", "/r", "deadbeef", "", staleTs, `{}`,
	); err != nil {
		t.Fatalf("insert stale graph_builds: %v", err)
	}

	r, err := preflight.Run(ctx, preflight.Options{
		// Architecture-sensitive task → triggers the sensitive branch
		// inside computeSafetyStatus + computeDegradedMode.
		Task:           "reconcile desired installed runtime convergence for component foo",
		DocsDir:        docsDir,
		IncludeRuntime: false, // runtime is noop → graphStaleNoRuntime triggers
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if r.Coverage.Graph != preflight.CoverageStale {
		t.Errorf("Coverage.Graph = %s, want %s — ordering must compute Coverage before SafetyStatus/DegradedMode read it",
			r.Coverage.Graph, preflight.CoverageStale)
	}
	if r.SafetyStatus != preflight.SafetyStatusUnknownNotSafe {
		t.Errorf("SafetyStatus = %s, want %s on stale-graph architecture-sensitive task with no runtime",
			r.SafetyStatus, preflight.SafetyStatusUnknownNotSafe)
	}
	if !r.DegradedMode.Enabled {
		t.Errorf("DegradedMode.Enabled = false; want true on stale graph + UNKNOWN_NOT_SAFE")
	}
}

func TestPreflightJSONOutputIsValidAndStable(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash mismatch",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out, err := preflight.Render(r, preflight.FormatJSON)
	if err != nil {
		t.Fatalf("Render JSON: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := m["confidence_factors"]; !ok {
		t.Fatalf("json missing confidence_factors: %s", out)
	}
	if _, ok := m["safety_status"]; !ok {
		t.Fatalf("json missing safety_status: %s", out)
	}
	if _, ok := m["risk_tier"]; !ok {
		t.Fatalf("json missing risk_tier: %s", out)
	}
	if _, ok := m["fast_path_applied"]; !ok {
		t.Fatalf("json missing fast_path_applied: %s", out)
	}

	// Re-render must be identical.
	out2, _ := preflight.Render(r, preflight.FormatJSON)
	if out != out2 {
		t.Error("JSON render is not stable across two calls")
	}
}

func TestPreflightAgentFormatContainsForbiddenActions(t *testing.T) {
	g := seedPreflightGraph(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out, err := preflight.Render(r, preflight.FormatAgent)
	if err != nil {
		t.Fatalf("Render agent: %v", err)
	}

	if !strings.Contains(out, "AGENT PREFLIGHT RESULT") {
		t.Error("agent format missing header")
	}
	if !strings.Contains(out, "Forbidden fixes:") {
		t.Error("agent format must contain Forbidden fixes section")
	}
}

// seedPreflightGraphWithPattern adds a pattern node linked to the existing invariant in seedPreflightGraph.
func seedPreflightGraphWithPattern(t *testing.T) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g := seedPreflightGraph(t)

	// Pattern node with code smells, linked to infra.desired_hash_consistency.
	_ = g.AddNode(ctx, graph.Node{
		ID:   "pattern:raw_digest_as_hash",
		Type: graph.NodeTypePattern,
		Name: "raw_digest_as_hash",
		Metadata: map[string]any{
			"title":       "Raw Digest as Desired Hash",
			"code_smells": []any{"raw_artifact_digest_as_desired_hash", "missing_version_normalization"},
		},
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "pattern:raw_digest_as_hash",
		Kind: graph.EdgeRequires,
		Dst:  "invariant:infra.desired_hash_consistency",
	})
	return g
}

func TestPreflightCodeSmellsPopulated(t *testing.T) {
	g := seedPreflightGraphWithPattern(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent across convergence ticks",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.CodeSmells) == 0 {
		t.Fatal("expected code smells from pattern node linked to matched invariant")
	}
	found := false
	for _, s := range r.CodeSmells {
		if strings.Contains(s, "raw_artifact_digest") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'raw_artifact_digest_as_desired_hash' in code smells, got %v", r.CodeSmells)
	}
}

func TestPreflightCodeSmellsInMarkdown(t *testing.T) {
	g := seedPreflightGraphWithPattern(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent across convergence ticks",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.CodeSmells) == 0 {
		t.Skip("no code smells matched — skipping format check")
	}

	md, err := preflight.Render(r, preflight.FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "Code smells to watch for") {
		t.Error("markdown missing 'Code smells to watch for' section")
	}
}

func TestPreflightCodeSmellsInJSON(t *testing.T) {
	g := seedPreflightGraphWithPattern(t)
	docsDir := setupPreflightDocsDir(t)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "desired_hash is inconsistent across convergence ticks",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(r.CodeSmells) == 0 {
		t.Skip("no code smells matched — skipping JSON check")
	}

	j, err := preflight.Render(r, preflight.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(j), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := parsed["code_smells"]; !ok {
		t.Error("JSON missing 'code_smells' key")
	}
}

func TestPreflightWriteAudit(t *testing.T) {
	g := seedPreflightGraphWithPattern(t)
	docsDir := setupPreflightDocsDir(t)
	ctx := context.Background()

	_, err := preflight.Run(ctx, preflight.Options{
		Task:       "desired_hash is inconsistent",
		DocsDir:    docsDir,
		WriteAudit: true,
		GitSHA:     "deadbeef",
	}, g)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	records, err := g.QueryPreflightAudits(ctx, 0, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(records))
	}
	if records[0].GitSHA != "deadbeef" {
		t.Errorf("GitSHA: got %s", records[0].GitSHA)
	}
	if records[0].Task != "desired_hash is inconsistent" {
		t.Errorf("Task: got %s", records[0].Task)
	}
}

// Package selfcheck runs the awareness system against itself — detecting
// false silences, noise, stale graph facts, and MCP safety regressions.
//
// It is observational only: it never mutates approved YAML files, never
// promotes proposals, and never auto-approves anything.
package selfcheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/checkedit"
	"github.com/globulario/awareness/debugsession"
	"github.com/globulario/awareness/enforce"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/semantic"
)

// CheckKind identifies which self-check sub-step produced a result.
type CheckKind string

const (
	KindBuild          CheckKind = "build"
	KindAudit          CheckKind = "audit"
	KindCoverage       CheckKind = "annotation_coverage"
	KindDrift          CheckKind = "graph_drift"
	KindSmoke          CheckKind = "preflight_smoke"
	KindNodeContext    CheckKind = "node_context_smoke"
	KindSemanticPath   CheckKind = "semantic_path_smoke"
	KindDebugSession   CheckKind = "debug_session_smoke"
	KindCheckEdit      CheckKind = "check_edit_smoke"
	KindPreflightAudit CheckKind = "preflight_audit_smoke"
	KindMCPDiscovery   CheckKind = "mcp_tool_discovery"
)

// CheckStatus is the outcome of a single self-check step.
type CheckStatus string

const (
	StatusPass    CheckStatus = "PASS"
	StatusFail    CheckStatus = "FAIL"
	StatusWeak    CheckStatus = "WEAK"    // matched but without strong evidence
	StatusSkipped CheckStatus = "SKIPPED" // check not applicable (e.g., no graph)
)

// CheckResult is the outcome of one self-check step.
type CheckResult struct {
	Kind          CheckKind
	Name          string
	Status        CheckStatus
	Detail        string   // human-readable explanation
	FalseSilences []string // expected invariants/fixes NOT found
	Noisy         []string // unexpected warnings or over-fired results
	Missing       []string // missing aliases, tests, etc.
}

// Options configures a self-check run.
type Options struct {
	RepoPath string // repo root (empty = use cwd)
	DocsDir  string // path to docs/awareness
	DBPath   string // path to graph.db (empty = no graph)
	Strict   bool   // if true, Report.StrictFail is set when any check fails
	// MaxTopWarningGroup, when >= 0, fails self-check strict mode if the largest
	// warning group in audit exceeds this count.
	MaxTopWarningGroup int
}

// Report is the complete self-check output.
type Report struct {
	GeneratedAt          time.Time
	Pass                 bool
	StrictFail           bool // true when Strict=true and any FAIL exists
	Checks               []CheckResult
	FalseSilences        []string // aggregated from all smoke checks
	NoisySections        []string
	MissingAliases       []string
	MissingTests         []string
	StaleRefs            []string
	MCPIssues            []string
	RecommendedFixes     []string
	ShouldCreateIncident bool
}

// SmokeCase defines one synthetic preflight scenario used to verify the
// awareness graph surfaces expected constraints for a given task string.
type SmokeCase struct {
	Name               string
	Task               string
	ExpectedInvariants []string
	ExpectedForbidden  []string
	Description        string
}

// SmokeCases is the canonical battery. Exported so tests can reference it.
var SmokeCases = []SmokeCase{
	{
		Name: "annotation_false_positive",
		Task: "annotation validator false positive check in production source",
		ExpectedInvariants: []string{
			"awareness.annotation_scanner.production_source_only",
		},
		Description: "Annotation scanner must restrict scanning to production source, not test fixtures.",
	},
	{
		Name: "desired_hash_mismatch",
		Task: "desired_hash mismatch task — hash never converges after deploy",
		ExpectedInvariants: []string{
			"infra.desired_hash_consistency",
		},
		ExpectedForbidden: []string{
			"use_raw_artifact_digest_as_desired_hash",
		},
		Description: "Desired-hash instability must surface infra.desired_hash_consistency and block the raw-digest forbidden fix.",
	},
	{
		Name: "missing_key_stopped_runtime",
		Task: "missing key stopped service — absence is not destructive intent",
		ExpectedInvariants: []string{
			"critical_state.absence_is_not_destructive_intent",
		},
		Description: "A missing etcd key must never be treated as a delete intent for a running service.",
	},
	{
		Name: "command_package_missing_unit",
		Task: "COMMAND package missing systemd unit file — runtime proof mismatch",
		ExpectedInvariants: []string{
			"runtime.installed_state_must_match_package_kind",
		},
		Description: "COMMAND packages must not require systemd units; the doctor must use package_kind as authority.",
	},
	{
		Name: "runtime_observation_desired_state",
		Task: "runtime observation created desired state — heartbeat should not set desired",
		ExpectedInvariants: []string{
			"infra.heartbeat_not_desired_authority",
		},
		Description: "Heartbeat must never write desired state. Only the controller reconciler has that authority.",
	},
}

// Run executes all self-check steps and returns a consolidated Report.
// g may be nil; graph-dependent checks are skipped gracefully.
func Run(ctx context.Context, opts Options, g *graph.Graph) (*Report, error) {
	r := &Report{
		GeneratedAt: time.Now(),
	}

	var allResults []CheckResult

	// 1. Build check — verify graph.db exists and was built recently.
	allResults = append(allResults, checkBuild(opts))

	// 2. Full enforcement audit.
	allResults = append(allResults, checkAudit(ctx, g, opts))

	// 3. Annotation coverage.
	allResults = append(allResults, checkAnnotationCoverage(ctx, g, opts))

	// 4. Graph drift.
	allResults = append(allResults, checkGraphDrift(ctx, g, opts))

	// 5. Preflight smoke cases.
	for _, sc := range SmokeCases {
		allResults = append(allResults, checkSmoke(ctx, g, opts, sc))
	}

	// 6. Node-context smoke.
	allResults = append(allResults, checkNodeContext(ctx, g, opts))

	// 7. Semantic path smoke.
	allResults = append(allResults, checkSemanticPath(ctx, g))

	// 8. Debug-session smoke.
	allResults = append(allResults, checkDebugSession(ctx, g, opts))

	// 9. Check-edit smoke.
	allResults = append(allResults, checkCheckEdit(ctx, g, opts))

	// 10. Preflight-audit smoke.
	allResults = append(allResults, checkPreflightAudit(ctx, g))

	// 11. MCP tool discovery smoke.
	allResults = append(allResults, checkMCPDiscovery(opts))

	r.Checks = allResults

	// Aggregate across all checks.
	failCount := 0
	for _, cr := range allResults {
		if cr.Status == StatusFail {
			failCount++
		}
		r.FalseSilences = append(r.FalseSilences, cr.FalseSilences...)
		r.NoisySections = append(r.NoisySections, cr.Noisy...)
		r.MCPIssues = appendIfKind(r.MCPIssues, cr, KindMCPDiscovery)
		r.StaleRefs = appendIfKind(r.StaleRefs, cr, KindDrift)
		r.MissingAliases = append(r.MissingAliases, filterMissingAliases(cr)...)
		r.MissingTests = append(r.MissingTests, filterMissingTests(cr)...)
	}

	r.Pass = failCount == 0
	r.ShouldCreateIncident = failCount > 0
	r.RecommendedFixes = buildRecommendedFixes(allResults)

	if opts.Strict && !r.Pass {
		r.StrictFail = true
	}

	return r, nil
}

// ── individual check functions ────────────────────────────────────────────────

func checkBuild(opts Options) CheckResult {
	cr := CheckResult{Kind: KindBuild, Name: "graph_db_exists_and_recent"}

	dbPath := opts.DBPath
	if dbPath == "" {
		cr.Status = StatusSkipped
		cr.Detail = "no DBPath provided — build check skipped"
		return cr
	}

	info, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("graph.db not found at %s — run 'globular awareness build'", dbPath)
		return cr
	}
	if err != nil {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("stat graph.db: %v", err)
		return cr
	}

	age := time.Since(info.ModTime())
	if age > 24*time.Hour {
		cr.Status = StatusWeak
		cr.Detail = fmt.Sprintf("graph.db is %.0fh old — consider rebuilding (run 'globular awareness build')", age.Hours())
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("graph.db exists, built %.0fm ago", age.Minutes())
	return cr
}

func checkAudit(ctx context.Context, g *graph.Graph, opts Options) CheckResult {
	cr := CheckResult{Kind: KindAudit, Name: "enforcement_audit"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — audit skipped"
		return cr
	}

	result := enforce.Audit(ctx, g, enforce.AuditOptions{
		RepoRoot:  opts.RepoPath,
		SrcDir:    opts.RepoPath,
		SkipDrift: true, // drift has its own check
	})

	if result.ErrorCount > 0 {
		cr.Status = StatusFail
		msgs := make([]string, 0, result.ErrorCount)
		for _, f := range result.Findings {
			if f.Severity == enforce.SeverityError {
				msgs = append(msgs, f.Message)
			}
		}
		cr.Detail = fmt.Sprintf("%d audit errors: %s", result.ErrorCount, strings.Join(msgs, "; "))
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("audit passed — %d warnings, %d info findings", result.WarningCount, result.InfoCount)
	if result.WarningCount > 3 {
		cr.Noisy = []string{fmt.Sprintf("%d audit warnings — consider triaging", result.WarningCount)}
		cr.Noisy = append(cr.Noisy, summarizeWarningGroups(result.Findings, 3)...)
	}
	if opts.Strict && opts.MaxTopWarningGroup >= 0 {
		topCount := maxWarningGroupCount(result.Findings)
		if topCount > opts.MaxTopWarningGroup {
			cr.Status = StatusFail
			cr.Detail = fmt.Sprintf(
				"audit warning-group threshold exceeded: top warning group count=%d > max=%d",
				topCount, opts.MaxTopWarningGroup)
		}
	}
	return cr
}

func summarizeWarningGroups(findings []enforce.Finding, maxGroups int) []string {
	if maxGroups <= 0 {
		return nil
	}
	var warnings []enforce.Finding
	for _, f := range findings {
		if f.Severity == enforce.SeverityWarning {
			warnings = append(warnings, f)
		}
	}
	if len(warnings) == 0 {
		return nil
	}
	groups := enforce.GroupFindings(warnings)
	if len(groups) > maxGroups {
		groups = groups[:maxGroups]
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, fmt.Sprintf("top warning group: %s (%d) — %s", g.Code, g.Count, g.SuggestedAction))
	}
	return out
}

func maxWarningGroupCount(findings []enforce.Finding) int {
	var warnings []enforce.Finding
	for _, f := range findings {
		if f.Severity == enforce.SeverityWarning {
			warnings = append(warnings, f)
		}
	}
	if len(warnings) == 0 {
		return 0
	}
	groups := enforce.GroupFindings(warnings)
	if len(groups) == 0 {
		return 0
	}
	return groups[0].Count
}

func checkAnnotationCoverage(ctx context.Context, g *graph.Graph, opts Options) CheckResult {
	cr := CheckResult{Kind: KindCoverage, Name: "annotation_coverage"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — coverage check skipped"
		return cr
	}

	watchlistPath := ""
	if opts.DocsDir != "" {
		watchlistPath = filepath.Join(opts.DocsDir, "high_risk_files.yaml")
	}

	result := enforce.AnnotationCoverage(ctx, g, enforce.AnnotationCoverageOptions{
		RepoRoot:      opts.RepoPath,
		SrcDir:        opts.RepoPath,
		WatchlistPath: watchlistPath,
		DocsDir:       opts.DocsDir,
	})

	if result.ErrorCount > 0 {
		cr.Status = StatusWeak
		msgs := make([]string, 0, result.ErrorCount)
		for _, f := range result.Findings {
			if f.Severity == enforce.SeverityError {
				msgs = append(msgs, f.File+": "+f.Message)
			}
		}
		cr.Detail = fmt.Sprintf("%d coverage gaps: %s", result.ErrorCount, strings.Join(msgs, "; "))
		cr.Missing = msgs
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = "annotation coverage OK"
	return cr
}

func checkGraphDrift(ctx context.Context, g *graph.Graph, opts CheckOptions) CheckResult {
	cr := CheckResult{Kind: KindDrift, Name: "graph_drift"}

	if g == nil || opts.RepoPath == "" {
		cr.Status = StatusSkipped
		cr.Detail = "no graph or repo path — drift check skipped"
		return cr
	}

	findings := enforce.AuditDrift(ctx, g, opts.RepoPath)

	errors := 0
	var stale []string
	for _, f := range findings {
		if f.Severity == enforce.SeverityError {
			errors++
			stale = append(stale, f.Message)
		}
	}

	if errors > 0 {
		cr.Status = StatusWeak
		cr.Detail = fmt.Sprintf("%d stale graph references: %s", errors, strings.Join(stale, "; "))
		cr.Missing = stale
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = "no stale graph references"
	return cr
}

// checkSmoke runs a single preflight smoke case and verifies expected invariants fire.
func checkSmoke(ctx context.Context, g *graph.Graph, opts Options, sc SmokeCase) CheckResult {
	cr := CheckResult{
		Kind: KindSmoke,
		Name: "smoke:" + sc.Name,
	}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — smoke case skipped"
		return cr
	}

	r, err := preflight.Run(ctx, preflight.Options{
		Task:    sc.Task,
		DocsDir: opts.DocsDir,
	}, g)
	if err != nil {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("preflight.Run failed: %v", err)
		return cr
	}

	// Check expected invariants.
	invSet := strSet(r.Invariants)
	for _, expected := range sc.ExpectedInvariants {
		if !invSet[expected] {
			cr.FalseSilences = append(cr.FalseSilences,
				fmt.Sprintf("smoke:%s — expected invariant %q not surfaced for task %q", sc.Name, expected, sc.Task))
		}
	}

	// Check expected forbidden fixes.
	ffSet := normalizeSet(r.ForbiddenFixes)
	for _, expected := range sc.ExpectedForbidden {
		if !ffSet[normalizeToken(expected)] {
			cr.FalseSilences = append(cr.FalseSilences,
				fmt.Sprintf("smoke:%s — expected forbidden fix %q not surfaced for task %q", sc.Name, expected, sc.Task))
		}
	}

	if len(cr.FalseSilences) > 0 {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("%d false silences: %s", len(cr.FalseSilences), strings.Join(cr.FalseSilences, "; "))
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("expected invariants %v surfaced", sc.ExpectedInvariants)
	return cr
}

func checkNodeContext(ctx context.Context, g *graph.Graph, opts Options) CheckResult {
	cr := CheckResult{Kind: KindNodeContext, Name: "node_context_smoke"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — node context smoke skipped"
		return cr
	}

	// Use tasks known to hit heartbeat/authority invariants.
	tasks := []string{
		"heartbeat writes desired state — authority violation",
		"runtime promoted to desired heartbeat authority",
	}
	var result analysis.AgentContextResult
	var err error
	for _, task := range tasks {
		_, result, err = analysis.GenerateAgentContext(ctx, g, task, analysis.AgentContextHints{})
		if err != nil {
			cr.Status = StatusFail
			cr.Detail = fmt.Sprintf("GenerateAgentContext: %v", err)
			return cr
		}
		if len(result.InvariantIDs) > 0 || len(result.FailureModeIDs) > 0 {
			cr.Status = StatusPass
			cr.Detail = fmt.Sprintf("node context returned %d invariants, %d failure modes",
				len(result.InvariantIDs), len(result.FailureModeIDs))
			return cr
		}
	}
	cr.Status = StatusWeak
	cr.Detail = "node-context smoke returned no invariants or failure modes — possible false silence for architectural task"
	cr.FalseSilences = []string{"node_context_smoke: no context returned for heartbeat-authority task"}
	return cr
}

func normalizeSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		out[normalizeToken(it)] = true
	}
	return out
}

func normalizeToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func checkSemanticPath(ctx context.Context, g *graph.Graph) CheckResult {
	cr := CheckResult{Kind: KindSemanticPath, Name: "semantic_path_smoke"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — semantic path smoke skipped"
		return cr
	}

	// Run a path query between two well-known invariant nodes.
	// The path may be empty (sparse graph) but the call must not error.
	fromID := "invariant:infra.desired_hash_consistency"
	toID := "invariant:infra.heartbeat_not_desired_authority"

	_, err := semantic.ShortestPath(ctx, g, fromID, toID, semantic.PathOptions{MaxDepth: 6})
	if err != nil && !isNotFoundErr(err) {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("semantic path query failed: %v", err)
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("semantic path query completed without error (from %s → %s)", fromID, toID)
	return cr
}

func checkDebugSession(ctx context.Context, g *graph.Graph, opts Options) CheckResult {
	cr := CheckResult{Kind: KindDebugSession, Name: "debug_session_smoke"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — debug session smoke skipped"
		return cr
	}

	sess, err := debugsession.Run(ctx, debugsession.Options{
		Task:    "restart storm after controller deploy — convergence failure",
		DocsDir: opts.DocsDir,
	}, g)
	if err != nil {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("debugsession.Run: %v", err)
		return cr
	}
	if sess == nil {
		cr.Status = StatusFail
		cr.Detail = "debugsession.Run returned nil session"
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("debug session produced %d root cause paths, confidence=%s",
		len(sess.LikelyRootCausePaths), sess.Confidence)
	return cr
}

func checkCheckEdit(ctx context.Context, g *graph.Graph, opts Options) CheckResult {
	cr := CheckResult{Kind: KindCheckEdit, Name: "check_edit_smoke"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — check-edit smoke skipped"
		return cr
	}

	// A known high-risk file — must be index-able by the awareness graph.
	smokeFile := "golang/cluster_controller/cluster_controller_server/release_reconciler.go"

	result, err := checkedit.Run(ctx, g, checkedit.Options{File: smokeFile})
	if err != nil {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("checkedit.Run: %v", err)
		return cr
	}

	// We don't assert specific forbidden fixes — just that the call completed and
	// the high-risk file has at least one awareness signal (forbidden fix or code smell).
	if len(result.Warnings) > 0 && !result.HasIssues {
		cr.Status = StatusWeak
		cr.Detail = "check-edit completed with warnings but no issues — high-risk file may be missing annotations"
		cr.Noisy = result.Warnings
		return cr
	}

	cr.Status = StatusPass
	cr.Detail = fmt.Sprintf("check-edit completed: has_issues=%v, %d forbidden fixes, %d code smells",
		result.HasIssues, len(result.ForbiddenFixes), len(result.CodeSmells))
	return cr
}

func checkPreflightAudit(ctx context.Context, g *graph.Graph) CheckResult {
	cr := CheckResult{Kind: KindPreflightAudit, Name: "preflight_audit_smoke"}

	if g == nil {
		cr.Status = StatusSkipped
		cr.Detail = "no graph — preflight audit smoke skipped"
		return cr
	}

	records, err := g.QueryPreflightAudits(ctx, 0, "")
	if err != nil {
		cr.Status = StatusFail
		cr.Detail = fmt.Sprintf("QueryPreflightAudits: %v", err)
		return cr
	}

	cr.Status = StatusPass
	if len(records) == 0 {
		cr.Detail = "preflight audit DB is readable — no records yet (run preflight with --write-audit to populate)"
	} else {
		cr.Detail = fmt.Sprintf("preflight audit DB readable — %d records found", len(records))
	}
	return cr
}

// checkMCPDiscovery verifies promote_proposal is NOT exposed via MCP.
// MCP safety invariant: awareness.mcp_must_not_expose_promotion.
// checkMCPDiscovery verifies the awareness.mcp_must_not_expose_promotion invariant
// by scanning the MCP registration source for forbidden calls.
// The standalone awareness/mcp server was removed in v1.2.20; the invariant is now
// enforced in golang/mcp/proposal_drain_tool.go (registerPromoteApprovedProposalsTool
// is deliberately not called from registerProposalDrainTools).
func checkMCPDiscovery(opts Options) CheckResult {
	cr := CheckResult{Kind: KindMCPDiscovery, Name: "mcp_promote_not_exposed"}

	// Locate the MCP proposal drain tool file relative to opts.DocsDir (../golang/mcp/).
	repoRoot := ""
	if opts.DocsDir != "" {
		// DocsDir is typically <repo>/docs/awareness — go up two levels.
		repoRoot = filepath.Join(opts.DocsDir, "..", "..")
	}
	if repoRoot == "" {
		cr.Status = StatusSkipped
		cr.Detail = "docs dir unknown — cannot verify MCP promotion invariant"
		return cr
	}

	drainFile := filepath.Join(repoRoot, "golang", "mcp", "proposal_drain_tool.go")
	data, err := os.ReadFile(drainFile)
	if err != nil {
		cr.Status = StatusSkipped
		cr.Detail = fmt.Sprintf("proposal_drain_tool.go not found (%v) — skipping MCP promotion check", err)
		return cr
	}

	// The invariant is violated if registerPromoteApprovedProposalsTool is called
	// from registerProposalDrainTools (i.e., the call is present and uncommented).
	// Look for a non-comment call to registerPromoteApprovedProposalsTool.
	// The function definition line starts with "func " — skip it.
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "func ") {
			continue
		}
		if strings.Contains(trimmed, "registerPromoteApprovedProposalsTool") {
			cr.Status = StatusFail
			cr.Detail = "MCP exposes promote_approved_proposals (must be CLI-only) — remove call from registerProposalDrainTools"
			return cr
		}
	}

	cr.Status = StatusPass
	cr.Detail = "MCP proposal_drain_tool.go does not call registerPromoteApprovedProposalsTool — promotion invariant holds"
	return cr
}

// ── helpers ───────────────────────────────────────────────────────────────────

// CheckOptions is an alias to Options used inside individual check functions
// to avoid the redundant type name.
type CheckOptions = Options

func strSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "no path") ||
		strings.Contains(err.Error(), "no route")
}

func appendIfKind(dst []string, cr CheckResult, kind CheckKind) []string {
	if cr.Kind != kind {
		return dst
	}
	if cr.Status == StatusFail && cr.Detail != "" {
		return append(dst, cr.Detail)
	}
	return dst
}

func filterMissingAliases(cr CheckResult) []string {
	var out []string
	for _, m := range cr.Missing {
		if strings.Contains(m, "alias") {
			out = append(out, m)
		}
	}
	return out
}

func filterMissingTests(cr CheckResult) []string {
	var out []string
	for _, m := range cr.Missing {
		if strings.Contains(m, "test") || strings.Contains(m, "Test") {
			out = append(out, m)
		}
	}
	return out
}

func buildRecommendedFixes(checks []CheckResult) []string {
	var out []string
	seen := map[string]bool{}

	addOnce := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	for _, cr := range checks {
		if cr.Status != StatusFail && cr.Status != StatusWeak {
			continue
		}
		switch cr.Kind {
		case KindBuild:
			addOnce("Run 'globular awareness build' to rebuild the graph")
		case KindAudit:
			addOnce("Fix enforcement audit errors (run 'globular awareness audit')")
		case KindCoverage:
			addOnce("Add +globular: annotations to uncovered high-risk files")
		case KindDrift:
			addOnce("Remove stale graph references (run 'globular awareness graph-drift')")
		case KindSmoke:
			addOnce("Add context aliases for tasks that produce false silences (docs/awareness/context_aliases.yaml)")
		case KindNodeContext:
			addOnce("Verify node-context aliases cover architectural task patterns")
		case KindSemanticPath:
			addOnce("Check graph edge provenance for semantic path disconnections")
		case KindDebugSession:
			addOnce("Verify debugsession package compiles and runs against the current graph schema")
		case KindCheckEdit:
			addOnce("Add +globular: annotations to high-risk files to enable check-edit signals")
		case KindMCPDiscovery:
			addOnce("Remove promote_proposal from MCP tool registration (promotion must be CLI-only)")
		}
	}

	return out
}

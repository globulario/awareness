package preflight

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/analysis/contextnav"
	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/bundlesync"
	"github.com/globulario/awareness/extractors/packages"
	"github.com/globulario/awareness/fixledger"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/integrity"
	"github.com/globulario/awareness/learning"
	"github.com/globulario/awareness/runtime"
)

// Options configures a preflight run.
type Options struct {
	Task               string                 // required: task description
	Files              []string               // optional: files to run impact analysis on
	PackagePath        string                 // optional: path to package dir with awareness.yaml
	Phase              string                 // optional: dependency phase filter for cycle detection
	DocsDir            string                 // path to docs/awareness (aliases, fix_cases, guardrails)
	RepoRoot           string                 // optional: repo root for Go file coverage computation
	IncludeRuntime     bool                   // collect live runtime snapshot
	RuntimeWindow      time.Duration          // lookback window for events/workflows (default 15m)
	Bridge             *runtime.RuntimeBridge // optional: if nil and IncludeRuntime, uses noop bridge
	WriteAudit         bool                   // if true, persist a PreflightAuditRecord to the graph DB after the run
	GitSHA             string                 // optional: current git commit SHA for the audit record
	Now                time.Time              // injectable clock for tests; zero → time.Now()
	BundleManifestPath string                 // optional: path to installed awareness-bundle manifest.json; populates Staleness.BundlePresent
}

// Run executes the full preflight and returns a structured report.
// g may be nil — in that case graph-dependent sections are skipped with a warning.
func Run(ctx context.Context, opts Options, g *graph.Graph) (*Report, error) {
	r := &Report{
		Task:     opts.Task,
		Files:    opts.Files,
		Cycles:   []CycleWarning{},
		DidWeFix: &DidWeFixSection{},
	}

	// 1. Classify task.
	r.Classification = ClassifyTask(opts.Task)

	// 2. Load aliases from docs dir (silent on missing).
	aliasMap := loadAliases(opts.DocsDir)

	// 3. Load fix cases and guardrails.
	fixCases := loadFixCases(opts.DocsDir)

	// 4. Alias matching — collect which aliases fired.
	r.MatchedAliases = matchAliases(opts.Task, aliasMap)

	// 5. Agent context from graph (invariants, failure modes, forbidden fixes, tests, searches, services).
	r.GraphAvailable = g != nil
	if g != nil {
		acResult, warn := runAgentContext(ctx, g, opts.Task, opts.Files, aliasMap)
		if warn != "" {
			r.Warnings = append(r.Warnings, warn)
		}
		r.Invariants = acResult.InvariantIDs
		r.FailureModes = acResult.FailureModeIDs
		r.ForbiddenFixes = acResult.ForbiddenFixes
		r.RequiredTests = acResult.RequiredTests
		r.RequiredSearches = acResult.RequiredSearches
		r.Services = acResult.ServiceNames
		r.GraphMatchCount = len(r.Invariants) + len(r.FailureModes) + len(r.ForbiddenFixes)

		// 5b. Trust filter: annotate low-trust graph matches into FilteredMatches.
		// Low-trust matches are still present in the main result lists (per spec:
		// do not suppress them entirely) but are also reported under FilteredMatches
		// so callers understand why graph_filtered_by_trust_count > 0.
		r.FilteredMatches = checkNodesTrust(ctx, g, r.Invariants, r.FailureModes, r.ForbiddenFixes)
		r.GraphFilteredByTrustCount = len(r.FilteredMatches)
	} else {
		r.Warnings = append(r.Warnings, "no graph DB — graph-dependent sections skipped (run 'globular awareness build' first)")
		r.Warnings = append(r.Warnings, "UNKNOWN_IMPACT: graph unavailable — use awareness.preflight with graph for reliable classification")
	}

	// 6. Impact analysis per file (annotation-priority first, then transitive traversal).
	if g != nil && len(opts.Files) > 0 {
		r = mergeImpact(ctx, g, opts.Files, r)
	}

	// 7. Package context + admission if --package provided.
	if opts.PackagePath != "" {
		r.Classification = appendClass(r.Classification, ClassPackageAdmission)
		pas, pkgNames := runPackageAdmission(ctx, g, opts.PackagePath)
		r.PackageAdmission = pas
		r.Packages = unique(append(r.Packages, pkgNames...))
	}

	// 8. Cycle detection if --phase provided.
	if g != nil && opts.Phase != "" {
		cycleWarnings, err := runCycles(ctx, g, opts.Phase)
		if err != nil {
			r.Warnings = append(r.Warnings, "cycle detection: "+err.Error())
		} else if len(cycleWarnings) > 0 {
			r.Cycles = cycleWarnings
			r.Classification = appendClass(r.Classification, ClassDependencyCycle)
		}
	}

	// 9. Fix-ledger: did-we-fix.
	r.DidWeFix = runDidWeFix(opts.Task, fixCases, aliasMap)

	// 10. Aggregate required tests from impact results (dedup with acResult already merged).
	r.RequiredTests = unique(r.RequiredTests)

	// 11. Aggregate forbidden fixes (dedup).
	r.ForbiddenFixes = unique(r.ForbiddenFixes)
	r.ForbiddenFixes = append(r.ForbiddenFixes, guardrailForbiddenFixes(opts.Task, opts.DocsDir)...)
	r.ForbiddenFixes = unique(r.ForbiddenFixes)

	// 11b. Collect code smells and design context from invariants.
	// Use both graph-matched invariants and alias-matched invariant IDs so that
	// design patterns surface even when GenerateAgentContext doesn't traverse
	// to those invariant nodes via graph edges.
	if g != nil {
		invIDSet := make(map[string]bool)
		for _, id := range r.Invariants {
			invIDSet["invariant:"+id] = true
		}
		for _, alias := range r.MatchedAliases {
			invIDSet["invariant:"+alias] = true
		}
		if len(invIDSet) > 0 {
			invNodeIDs := make([]string, 0, len(invIDSet))
			for id := range invIDSet {
				invNodeIDs = append(invNodeIDs, id)
			}
			// Legacy pattern nodes (patterns.yaml).
			if smells, err := g.CodeSmellsForInvariants(ctx, invNodeIDs); err == nil {
				r.CodeSmells = smells
			}
			// Design pattern layer (design_patterns.yaml).
			if dc, err := g.DesignContextForInvariants(ctx, invNodeIDs); err == nil {
				r.DesignPatterns = unique(dc.DesignPatterns)
				r.AntiPatterns = unique(dc.AntiPatterns)
				r.CodeSmells = unique(append(r.CodeSmells, dc.CodeSmells...))
			}
		}
	}

	// 12. Raw YAML fallback: graph/query NO_MATCH is not proof of safety.
	// The source awareness YAML files are the authority; the graph is an index.
	// Cross-check them directly so stale graph nodes or query misses do not
	// create false confidence.
	rawMatches := RawKnowledgeFallback(opts.Task, opts.Files, opts.DocsDir)
	r.RawKnowledgeMatches = rawMatches
	r.RawYAMLMatchCount = len(rawMatches)
	if len(rawMatches) > 0 {
		r = mergeRawKnowledgeMatches(r, rawMatches)
	}

	// 13. False-silence detection and UNKNOWN_IMPACT gating.
	noFacts := noAwarenessFactsMatched(r)
	if noFacts {
		r.Warnings = append(r.Warnings, "NO_AWARENESS_MATCH: no awareness facts matched this task. This does not prove the task is safe.")
	} else if len(rawMatches) > 0 {
		r.Classification = appendClass(r.Classification, ClassStaticFallback)
		r.Warnings = append(r.Warnings, "RAW_KNOWLEDGE_CROSSCHECK: source YAML matched relevant awareness facts; graph/query silence must not be treated as safe.")
	}
	if hasClass(r.Classification, ClassArchitectureSensitive) && noFacts {
		r.Classification = appendClass(r.Classification, ClassUnknownImpact)
	}

	// 14. Build recommended investigation order.
	if g != nil {
		domain := inferExperienceDomain(r)
		capability := inferExperienceCapability(r)
		hits, err := g.SearchSimilarExperiences(ctx, graph.ExperienceSearchQuery{
			Goal:            opts.Task,
			Domain:          domain,
			Capability:      capability,
			Files:           opts.Files,
			InvariantIDs:    r.Invariants,
			ForbiddenFixIDs: r.ForbiddenFixes,
			Limit:           3,
		})
		if err != nil {
			r.Warnings = append(r.Warnings, "experience search failed: "+err.Error())
		} else {
			for _, h := range hits {
				r.ExperienceHints = append(r.ExperienceHints, ExperienceHint{
					ExperienceID:  h.ExperienceID,
					Score:         h.Score,
					Strategy:      h.StrategyID,
					Hint:          h.Hint,
					Status:        h.Status,
					Summary:       h.Summary,
					Verdict:       h.Verdict,
					FinalScore:    h.FinalScore,
					Reasons:       h.Reasons,
					WorkedPaths:   h.WorkedPaths,
					FailedPaths:   h.FailedPaths,
					EvidenceTypes: h.EvidenceTypes,
				})
			}
		}
	}

	// 14. Build recommended investigation order.
	r.RecommendedOrder = buildInvestigationOrder(r)

	// 15. Build agent instruction.
	r.AgentInstruction = buildAgentInstruction(r)

	// 16. Runtime snapshot (optional).
	if opts.IncludeRuntime {
		r = mergeRuntime(ctx, opts, g, r)
	}

	// 17. Durable audit record (optional).
	if opts.WriteAudit && g != nil {
		rec := graph.PreflightAuditRecord{
			Task:           opts.Task,
			GitSHA:         opts.GitSHA,
			Files:          r.Files,
			ForbiddenFixes: r.ForbiddenFixes,
			Invariants:     r.Invariants,
			CodeSmells:     r.CodeSmells,
		}
		if err := g.InsertPreflightAudit(ctx, rec); err != nil {
			r.Warnings = append(r.Warnings, "preflight-audit write failed: "+err.Error())
		}
	}

	// 18. Graph freshness + collector health.
	if g != nil && opts.DocsDir != "" {
		f := g.Freshness(ctx, opts.DocsDir)
		gfr := &GraphFreshnessReport{
			BuiltAt:             formatTime(f.BuiltAt),
			AgeSeconds:          f.AgeSeconds,
			Stale:               f.Stale,
			StaleReason:         f.StaleReason,
			KnowledgeMtime:      formatTime(f.KnowledgeMtime),
			KnowledgeSourceHash: f.KnowledgeSourceHash,
			RebuildRecommended:  f.RebuildRecommended,
		}
		// Include last build duration and collector health if available.
		if rec, err := g.LatestBuildRecord(ctx); err == nil && rec != nil {
			gfr.LastBuildDurationMs = rec.Stats.DurationMs
			for _, ch := range rec.CollectorHealth {
				r.CollectorHealth = append(r.CollectorHealth, CollectorHealthSummary{
					CollectorID:  ch.CollectorID,
					Status:       ch.Status,
					NodesEmitted: ch.NodesEmitted,
					Error:        ch.Error,
				})
				if ch.Status == "error" {
					r.Warnings = append(r.Warnings,
						fmt.Sprintf("COLLECTOR_ERROR: %s failed: %s — graph may be missing nodes from this tier", ch.CollectorID, ch.Error))
					r.BlindSpots = append(r.BlindSpots,
						fmt.Sprintf("collector %s errored: nodes from this source may be absent from the graph", ch.CollectorID))
				}
			}
		}
		r.GraphFreshness = gfr
		if f.Stale && len(r.RawKnowledgeMatches) > 0 {
			r.Warnings = append(r.Warnings, "GRAPH_STALE: "+f.StaleReason+". Raw YAML matched — treat graph as incomplete.")
		}
	}

	// 18a. Go file coverage — measures what fraction of eligible Go files are indexed.
	// Low coverage lowers confidence and is reported as a blind spot.
	if opts.RepoRoot != "" {
		cov := computeGoFileCoverage(ctx, g, opts.RepoRoot)
		r.GoFileCoverage = cov
		if cov.ConfidenceImpact == "high" {
			r.BlindSpots = append(r.BlindSpots, cov.BlindSpots...)
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"UNKNOWN_IMPACT_LOW_COVERAGE: graph indexes only %.1f%% of eligible Go files — NO_MATCH results cannot be trusted as safe",
				cov.CoveragePercentGoFiles))
		} else if cov.ConfidenceImpact == "medium" {
			r.BlindSpots = append(r.BlindSpots, cov.BlindSpots...)
		}
	}

	// 18b. Live overlay freshness — separate from graph freshness.
	// Missing or stale live overlay is a blind spot for runtime-sensitive tasks.
	if g != nil {
		r.LiveOverlay = ComputeLiveOverlayFreshness(ctx, g, opts.Now)
		if r.LiveOverlay.Status == "absent" {
			r.BlindSpots = append(r.BlindSpots,
				"live overlay absent — run 'globular awareness live-snapshot' or enable systemd timer for scheduled refresh")
		} else if r.LiveOverlay.Status == "stale" {
			r.Warnings = append(r.Warnings,
				fmt.Sprintf("LIVE_OVERLAY_STALE: live mirror is %.0fs old (threshold %ds) — runtime evidence may be outdated",
					r.LiveOverlay.AgeSeconds, LiveOverlayStaleSeconds))
		} else if r.LiveOverlay.Status == "failed" || r.LiveOverlay.Status == "partial" {
			r.BlindSpots = append(r.BlindSpots,
				fmt.Sprintf("live overlay %s — some collectors could not refresh runtime evidence", r.LiveOverlay.Status))
		}
	}

	// 18c. Risk tier and optional low-risk fast path.
	r.RiskTier = computeRiskTier(r, g)
	r.FastPathApplied = applyLowRiskFastPath(r)

	// 19. Confidence model.
	r.Coverage, r.Confidence, r.ConfidenceReason, r.BlindSpots, r.ConfidenceFactors = computeConfidence(r, g)
	// Demote confidence if Go file coverage is critically low: the graph cannot
	// give reliable NO_MATCH answers for files it has never seen.
	if r.GoFileCoverage != nil && r.GoFileCoverage.ConfidenceImpact == "high" {
		if r.Confidence == ConfidenceHigh {
			r.Confidence = ConfidenceMedium
			r.ConfidenceReason += "; demoted: graph Go-file coverage < 70%"
		}
	}
	// Merge GoFileCoverage blind spots (computeConfidence builds its own slice).
	if r.GoFileCoverage != nil && len(r.GoFileCoverage.BlindSpots) > 0 {
		r.BlindSpots = unique(append(r.BlindSpots, r.GoFileCoverage.BlindSpots...))
	}
	r.SafetyStatus = computeSafetyStatus(r)
	r.DegradedMode = computeDegradedMode(r)
	if r.SafetyStatus == SafetyStatusUnknownNotSafe {
		r.Warnings = append(r.Warnings,
			"UNKNOWN_NOT_SAFE: evidence is incomplete for a sensitive task. Rebuild graph and collect runtime evidence before code changes.")
	}
	r.Trust = computeTrustEnvelope(ctx, g, opts, r)

	// Per-finding decision traces (analysis/contextnav).
	//
	// Phase 8 ordering contract: r.Trust MUST be computed before
	// buildContextnavInputs runs, so the adapter can lift TrustVerdict /
	// TrustConfidence / TrustFreshness / TrustReason into BuildInputs
	// and the trust_envelope EvidenceRef lands on every non-rawKnowledge
	// trace. Re-ordering these two lines silently empties the trust
	// evidence on every trace — keep them adjacent in this order.
	//
	// Pure composition over data already in the Report; produces an empty
	// slice when no findings matched so the JSON shape stays
	// "decision_traces: []" rather than null — agents must not read the
	// absence of a key as the absence of risk. The trust envelope above
	// remains the authority for NO_MATCH safety.
	r.DecisionTraces = contextnav.Build(buildContextnavInputs(ctx, r, g, opts.Task, opts.Files))

	return r, nil
}

func computeTrustEnvelope(ctx context.Context, g *graph.Graph, opts Options, r *Report) *assurance.TrustEnvelope {
	if r == nil {
		return nil
	}
	// PrimaryMatchKind picks the most-specific match kind so the trust
	// envelope's reason text stays honest. Order matters: failure_mode wins
	// when both are present (it's the most actionable axis); raw YAML is a
	// last-resort fallback. See docs/awareness/composed_path_failures.md
	// (TrustEnvelope match-kind).
	primaryKind := ""
	switch {
	case len(r.FailureModes) > 0:
		primaryKind = assurance.MatchKindFailureMode
	case len(r.Invariants) > 0:
		primaryKind = assurance.MatchKindInvariant
	case len(r.ForbiddenFixes) > 0:
		primaryKind = assurance.MatchKindForbiddenFix
	case len(r.RawKnowledgeMatches) > 0:
		primaryKind = assurance.MatchKindRawYAML
	}
	in := assurance.ComposeInputs{
		MatchFound:       len(r.Invariants) > 0 || len(r.FailureModes) > 0 || len(r.ForbiddenFixes) > 0 || len(r.RawKnowledgeMatches) > 0,
		PrimaryMatchKind: primaryKind,
	}
	if g != nil {
		stOpts := assurance.Options{DocsDir: opts.DocsDir}
		// Resolve the manifest path: explicit caller value first, then the
		// canonical install path. This makes the joined freshness pipeline
		// usable by every caller (CLI, MCP, in-process) without each one
		// having to re-derive the path. A missing/unreadable manifest is
		// silently OK — Staleness.BundlePresent stays false and the verdict
		// reflects the gap honestly.
		manifestPath := opts.BundleManifestPath
		if manifestPath == "" {
			manifestPath = bundlesync.DefaultManifestPath()
		}
		if m, mErr := bundlesync.LoadManifest(manifestPath); mErr == nil {
			stOpts.Manifest = m
		}
		if st, err := assurance.CheckStaleness(ctx, g, stOpts); err == nil {
			in.Staleness = st
		}
		if len(r.FailureModes) > 0 {
			if cov, err := assurance.ComputeCoverage(ctx, g); err == nil {
				in.PerFailureMode = chooseFailureModeCoverage(cov, r.FailureModes)
			}
		}
	}
	env := assurance.Compose(in)
	return &env
}

// chooseFailureModeCoverage picks the worst-coverage entry among the matched
// failure_modes, since the trust envelope must reflect the weakest link in the
// match set. Uses CoverageReport.CoverageFor (O(1) per id) so the whole pass
// is O(len(matched)) instead of O(len(PerFailureMode) * len(matched)).
func chooseFailureModeCoverage(cov *assurance.CoverageReport, matched []string) *assurance.FailureModeCoverage {
	if cov == nil || len(cov.PerFailureMode) == 0 || len(matched) == 0 {
		return nil
	}
	priority := map[string]int{"ORPHAN": 0, "PARTIAL": 1, "DETECTED": 2, "TESTED": 3, "ENFORCED": 4, "DEPRECATED": 5, "INTENTIONAL_GAP": 6}
	var best *assurance.FailureModeCoverage
	bestRank := 99
	for _, id := range matched {
		fm := cov.CoverageFor(id)
		if fm == nil {
			continue
		}
		rank := 99
		if r, ok := priority[fm.State]; ok {
			rank = r
		}
		if best == nil || rank < bestRank {
			best = fm
			bestRank = rank
		}
	}
	return best
}

func inferExperienceDomain(r *Report) string {
	for _, s := range r.Services {
		switch strings.ToLower(s) {
		case "workflow":
			return "workflow"
		case "repository":
			return "repository"
		case "cluster-controller", "cluster":
			return "cluster"
		}
	}
	if len(r.Classification) > 0 {
		switch r.Classification[0] {
		case ClassRuntimeIncident, ClassRetryLoop, ClassRestartStorm:
			return "workflow"
		}
	}
	return ""
}

func inferExperienceCapability(r *Report) string {
	for _, s := range r.Services {
		if strings.EqualFold(s, "workflow") {
			return "workflow.defer"
		}
	}
	return ""
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func computeConfidence(r *Report, g *graph.Graph) (Coverage, Confidence, string, []string, ConfidenceFactors) {
	// Graph coverage.
	var graphCov CoverageState
	if g == nil {
		graphCov = CoverageNotChecked
	} else if len(r.Invariants) > 0 || len(r.FailureModes) > 0 {
		graphCov = CoverageCheckedWithMatch
	} else {
		graphCov = CoverageCheckedClean
	}
	if r.GraphFreshness != nil && r.GraphFreshness.Stale {
		graphCov = CoverageStale
	}

	// Raw YAML coverage — the raw fallback always runs in step 12.
	// We track whether it ran (it always does) and whether it found matches.
	var rawYAMLCov CoverageState
	if len(r.RawKnowledgeMatches) > 0 {
		rawYAMLCov = CoverageCheckedWithMatch
	} else {
		rawYAMLCov = CoverageCheckedClean
	}

	// Runtime coverage.
	var runtimeCov CoverageState
	if r.Runtime == nil || !r.Runtime.Included {
		runtimeCov = CoverageNoop
	} else {
		hasEvidence := len(r.Runtime.DoctorFindings) > 0 ||
			len(r.Runtime.ServiceStatuses) > 0 ||
			len(r.Runtime.WorkflowReceipts) > 0 ||
			len(r.Runtime.StateDeltas) > 0 ||
			len(r.Runtime.MatchedInvariants) > 0 ||
			len(r.Runtime.MatchedFailureModes) > 0
		if hasEvidence {
			runtimeCov = CoverageCheckedWithMatch
		} else {
			runtimeCov = CoverageCheckedClean
		}
	}

	// Metrics coverage.
	var metricsCov CoverageState
	if r.Runtime == nil || !r.Runtime.Included {
		metricsCov = CoverageNoop
	} else if len(r.Runtime.MetricWarnings) > 0 {
		metricsCov = CoverageCheckedWithMatch
	} else {
		metricsCov = CoverageCheckedClean
	}

	cov := Coverage{
		Graph:         graphCov,
		RawYAML:       rawYAMLCov,
		Runtime:       runtimeCov,
		Metrics:       metricsCov,
		CodeScan:      CoverageNotChecked, // scan_violations is a separate tool
		IncidentStore: CoverageNotChecked,
	}

	var blindSpots []string
	if graphCov == CoverageNotChecked {
		blindSpots = append(blindSpots, "graph unavailable — static pattern matching only")
	}
	if graphCov == CoverageStale {
		blindSpots = append(blindSpots, "graph stale — knowledge may be incomplete")
	}
	if runtimeCov == CoverageNoop {
		blindSpots = append(blindSpots, "runtime snapshot not collected — no live cluster evidence")
	}
	if metricsCov == CoverageNoop {
		blindSpots = append(blindSpots, "metrics not available — resource saturation not assessed")
	}
	if cov.CodeScan == CoverageNotChecked {
		blindSpots = append(blindSpots, "code violation scan not run — use awareness.scan_violations")
	}

	// Score the confidence.
	// graph checked (any non-not_checked state except stale):
	graphChecked := graphCov == CoverageCheckedClean || graphCov == CoverageCheckedWithMatch
	rawYAMLChecked := rawYAMLCov == CoverageCheckedClean || rawYAMLCov == CoverageCheckedWithMatch
	runtimeActive := runtimeCov == CoverageCheckedClean || runtimeCov == CoverageCheckedWithMatch
	metricsActive := metricsCov == CoverageCheckedClean || metricsCov == CoverageCheckedWithMatch

	score := 0
	if graphChecked {
		score++
	}
	if rawYAMLChecked {
		score++
	}
	if runtimeActive {
		score++
	}
	if metricsActive {
		score++
	}

	var conf Confidence
	var reason string
	switch {
	case score >= 3:
		conf = ConfidenceHigh
		reason = "graph, raw YAML, and runtime evidence all collected"
	case score == 2:
		conf = ConfidenceMedium
		reason = "static analysis complete; runtime evidence partial or unavailable"
	case score == 1:
		conf = ConfidenceLow
		reason = "only partial static analysis available; runtime sources unavailable"
	default:
		conf = ConfidenceUnknown
		reason = "no awareness data collected — graph missing and runtime unavailable"
	}

	// Override if graph is stale and no runtime compensates.
	if graphCov == CoverageStale && !runtimeActive {
		if conf == ConfidenceHigh {
			conf = ConfidenceMedium
			reason += "; demoted due to stale graph"
		}
	}

	factors := ConfidenceFactors{
		Coverage:        cov.Graph,
		Provenance:      "graph+raw_yaml",
		GraphFreshness:  graphCov,
		PathQuality:     "trusted_or_declared",
		RuntimeEvidence: runtimeCov,
	}
	if r.GraphFilteredByTrustCount > 0 {
		factors.PathQuality = "mixed_trust_paths"
	}
	if r.GraphMatchCount == 0 && r.RawYAMLMatchCount > 0 {
		factors.Provenance = "raw_yaml_fallback"
	}
	if g == nil {
		factors.Provenance = "raw_yaml_only"
	}

	return cov, conf, reason, blindSpots, factors
}

func computeSafetyStatus(r *Report) SafetyStatus {
	if r == nil {
		return SafetyStatusUnknownNotSafe
	}
	sensitive := hasClass(r.Classification, ClassArchitectureSensitive) ||
		hasClass(r.Classification, ClassConvergenceRisk)
	unknownImpact := hasClass(r.Classification, ClassUnknownImpact)
	graphStaleNoRuntime := r.Coverage.Graph == CoverageStale && r.Coverage.Runtime == CoverageNoop
	if sensitive && (unknownImpact || graphStaleNoRuntime) {
		return SafetyStatusUnknownNotSafe
	}
	return SafetyStatusProceed
}

func computeDegradedMode(r *Report) DegradedModePlaybook {
	p := DegradedModePlaybook{}
	if r == nil {
		p.Enabled = true
		p.Reason = "preflight report unavailable"
		return p
	}

	graphUnavailable := r.Coverage.Graph == CoverageNotChecked
	graphStale := r.Coverage.Graph == CoverageStale
	sensitive := hasClass(r.Classification, ClassArchitectureSensitive) || hasClass(r.Classification, ClassConvergenceRisk)
	if !(graphUnavailable || graphStale || r.SafetyStatus == SafetyStatusUnknownNotSafe) {
		return p
	}

	p.Enabled = true
	if graphUnavailable {
		p.Reason = "graph unavailable"
	} else if graphStale {
		p.Reason = "graph stale"
	} else {
		p.Reason = "insufficient trusted evidence"
	}
	p.AllowedNextSteps = []string{
		"Run `globular awareness build --clean` to refresh graph evidence.",
		"Re-run preflight with the exact task and target files.",
		"Use raw YAML matches as guidance only; require tests before any behavior change.",
	}
	p.BlockedActions = []string{
		"Do not apply destructive or runtime-stop changes from inferred intent.",
		"Do not treat no-match as safe for architecture-sensitive tasks.",
		"Do not promote confidence to high without refreshed graph or runtime evidence.",
	}
	p.StopConditions = []string{
		"Stop and escalate if preflight still returns UNKNOWN_NOT_SAFE after rebuild.",
		"Stop if required tests cannot be identified for a sensitive task.",
	}
	if sensitive {
		p.StopConditions = append(p.StopConditions,
			"Stop if any proposed fix violates listed forbidden fixes.")
	}
	return p
}

func computeRiskTier(r *Report, g *graph.Graph) RiskTier {
	if r == nil {
		return RiskHigh
	}
	if hasClass(r.Classification, ClassArchitectureSensitive) ||
		hasClass(r.Classification, ClassConvergenceRisk) ||
		hasClass(r.Classification, ClassDependencyCycle) ||
		hasClass(r.Classification, ClassPackageAdmission) ||
		hasClass(r.Classification, ClassRuntimeIncident) {
		return RiskHigh
	}
	if hasClass(r.Classification, ClassLocalCodeChange) && len(r.Files) == 0 {
		// Only low risk when the graph was available and confirmed no file impact.
		// With a nil graph, zero file matches means the graph did not cover this
		// task — that is a coverage gap, not confirmed low impact.
		if g != nil {
			return RiskLow
		}
	}
	return RiskMedium
}

func applyLowRiskFastPath(r *Report) bool {
	if r == nil {
		return false
	}
	if r.RiskTier != RiskLow {
		return false
	}
	if hasClass(r.Classification, ClassUnknownImpact) {
		return false
	}
	// Fast-path is only for truly local/no-context tasks. If awareness already
	// matched aliases or architectural facts, keep full context fidelity.
	if len(r.MatchedAliases) > 0 || len(r.Invariants) > 0 || len(r.FailureModes) > 0 {
		return false
	}
	if r.GraphFreshness != nil && r.GraphFreshness.Stale {
		return false
	}
	// FastPathApplied is a pure signal — no data is truncated here.
	// Context reduction for presentation belongs in the render layer, not
	// on the shared Report struct where truncation is unrecoverable.
	r.Warnings = append(r.Warnings, "FAST_PATH_APPLIED: low-risk task; graph confirmed no architectural impact.")
	return true
}

func noAwarenessFactsMatched(r *Report) bool {
	if r == nil {
		return true
	}
	hasFixLedgerMatch := false
	if r.DidWeFix != nil {
		hasFixLedgerMatch = len(r.DidWeFix.FixCases) > 0
	}
	hasPackageContext := len(r.Packages) > 0 || r.PackageAdmission != nil
	hasRuntimeEvidence := false
	if r.Runtime != nil {
		hasRuntimeEvidence = len(r.Runtime.DoctorFindings) > 0 ||
			len(r.Runtime.ServiceStatuses) > 0 ||
			len(r.Runtime.WorkflowReceipts) > 0 ||
			len(r.Runtime.StateDeltas) > 0 ||
			len(r.Runtime.MatchedInvariants) > 0 ||
			len(r.Runtime.MatchedFailureModes) > 0
	}

	return len(r.Invariants) == 0 &&
		len(r.FailureModes) == 0 &&
		!hasFixLedgerMatch &&
		len(r.MatchedAliases) == 0 &&
		!hasPackageContext &&
		!hasRuntimeEvidence
}

// runAgentContext calls analysis.GenerateAgentContext and returns the result.
func runAgentContext(ctx context.Context, g *graph.Graph, task string, files []string, aliasMap learning.ContextAliasMap) (analysis.AgentContextResult, string) {
	hints := analysis.AgentContextHints{Files: files}
	_, result, err := analysis.GenerateAgentContext(ctx, g, task, hints, analysis.AgentContextAliases(aliasMap))
	if err != nil {
		return analysis.AgentContextResult{}, "agent-context: " + err.Error()
	}
	return result, ""
}

// mergeImpact runs ImpactByFile for each file and merges results into r.
// Explicit annotation results (from //globular: directives) are surfaced first
// and outrank keyword-matched results — annotation edges carry Required=true.
func mergeImpact(ctx context.Context, g *graph.Graph, files []string, r *Report) *Report {
	// Pass 1: explicit annotations — these outrank keyword matches.
	for _, f := range files {
		ann, err := g.AnnotationsForFile(ctx, f)
		if err != nil {
			r.Warnings = append(r.Warnings, "annotations "+f+": "+err.Error())
			continue
		}
		// Prepend so annotation-derived entries survive unique() dedup first.
		r.Invariants = unique(append(ann.Invariants, r.Invariants...))
		r.ForbiddenFixes = unique(append(ann.ForbiddenFixes, r.ForbiddenFixes...))
		r.RequiredTests = unique(append(ann.RequiredTests, r.RequiredTests...))
		r.HashSchemas = unique(append(r.HashSchemas, ann.HashSchemas...))
		r.StateTransitions = unique(append(r.StateTransitions, ann.StateTransitions...))

		if ann.HasCritical {
			r.Classification = appendClass(r.Classification, ClassArchitectureSensitive)
		}
	}

	// Pass 2: transitive graph traversal from each file.
	for _, f := range files {
		res, err := analysis.ImpactByFile(ctx, g, f)
		if err != nil {
			r.Warnings = append(r.Warnings, "impact "+f+": "+err.Error())
			continue
		}
		if res.SourceFile == nil {
			r.Warnings = append(r.Warnings, "impact: no graph node for file "+f+" (run 'globular awareness build')")
			continue
		}
		for _, n := range res.Invariants {
			r.Invariants = append(r.Invariants, n.Name)
		}
		for _, n := range res.FailureModes {
			r.FailureModes = append(r.FailureModes, n.Name)
		}
		for _, n := range res.ForbiddenFixes {
			r.ForbiddenFixes = append(r.ForbiddenFixes, n.Name)
		}
		for _, n := range res.Tests {
			r.RequiredTests = append(r.RequiredTests, n.Name)
		}
		for _, n := range res.Services {
			r.Services = append(r.Services, n.Name)
		}
	}

	r.Invariants = unique(r.Invariants)
	r.FailureModes = unique(r.FailureModes)
	r.ForbiddenFixes = unique(r.ForbiddenFixes)
	r.RequiredTests = unique(r.RequiredTests)
	r.Services = unique(r.Services)
	return r
}

// runPackageAdmission loads a contract and validates it.
func runPackageAdmission(ctx context.Context, g *graph.Graph, packagePath string) (*PackageAdmissionSection, []string) {
	contract, err := packages.LoadAwarenessContract(packagePath)
	if err != nil {
		return &PackageAdmissionSection{Status: "ERROR", Reasons: []string{err.Error()}}, nil
	}

	packageKind := ""
	if contract != nil {
		packageKind = contract.PackageKind
	}

	var pkgNames []string
	if contract != nil {
		pkgNames = append(pkgNames, contract.Package)
	}

	if g == nil {
		return &PackageAdmissionSection{Status: "SKIPPED", Reasons: []string{"no graph DB"}}, pkgNames
	}

	result, err := analysis.ValidatePackage(ctx, contract, packageKind, g)
	if err != nil {
		return &PackageAdmissionSection{Status: "ERROR", Reasons: []string{err.Error()}}, pkgNames
	}

	reasons := make([]string, 0, len(result.Reasons))
	for _, reason := range result.Reasons {
		reasons = append(reasons, reason.Message)
	}

	return &PackageAdmissionSection{
		Status:  string(result.Status),
		Reasons: reasons,
	}, pkgNames
}

// runCycles runs cycle detection for the given phase.
func runCycles(ctx context.Context, g *graph.Graph, phase string) ([]CycleWarning, error) {
	cycles, err := analysis.FindCycles(ctx, g, phase)
	if err != nil {
		return nil, err
	}
	out := make([]CycleWarning, 0, len(cycles))
	for _, c := range cycles {
		out = append(out, CycleWarning{
			Phase:          c.Phase,
			Classification: string(c.Classification),
			Path:           c.Path,
			Reason:         c.Reason,
		})
	}
	return out, nil
}

// runDidWeFix calls the fix-ledger DidWeFix query.
func runDidWeFix(task string, fixCases []fixledger.FixCase, aliasMap learning.ContextAliasMap) *DidWeFixSection {
	result := fixledger.DidWeFix(task, fixCases, fixledger.ContextAliasMap(aliasMap))

	patterns := []string{}
	if result.MatchedPattern != "" {
		patterns = []string{result.MatchedPattern}
	}

	caseIDs := make([]string, 0, len(result.MatchedFixCases))
	for _, fc := range result.MatchedFixCases {
		caseIDs = append(caseIDs, fc.ID)
	}

	gaps := result.RemainingFiles
	if len(gaps) == 0 {
		gaps = []string{}
	}

	return &DidWeFixSection{
		Status:          string(result.OverallStatus),
		MatchedPatterns: patterns,
		FixCases:        caseIDs,
		RemainingGaps:   gaps,
		NextAction:      result.NextAction,
	}
}

// guardrailForbiddenFixes loads guardrails from docsDir and returns forbidden fixes that
// match the task — derived from guardrail summaries and target invariants.
func guardrailForbiddenFixes(task string, docsDir string) []string {
	if docsDir == "" {
		return nil
	}
	guards, err := fixledger.LoadGuardrails(filepath.Join(docsDir, "guardrails.yaml"))
	if err != nil || len(guards) == 0 {
		return nil
	}
	lower := strings.ToLower(task)
	var out []string
	for _, g := range guards {
		for _, inv := range g.TargetInvariants {
			if strings.Contains(lower, strings.ToLower(inv)) {
				out = append(out, g.Summary)
				break
			}
		}
	}
	return out
}

// buildInvestigationOrder constructs a prioritised investigation sequence.
func buildInvestigationOrder(r *Report) []string {
	var steps []string

	if hasClass(r.Classification, ClassStateMismatch) {
		steps = append(steps,
			"Check desired-hash computation (ComputeInfrastructureDesiredHash)",
			"Verify installed-state stamping is complete before heartbeat",
			"Confirm build_id flow from repository → controller → node-agent → etcd",
		)
	}

	if hasClass(r.Classification, ClassRestartStorm) {
		steps = append(steps,
			"Check restart singleflight gate (one restart per service per convergence tick)",
			"Inspect SIGTERM delivery and supervisor acknowledgement",
			"Verify start-limit reset is guarded (systemctl reset-failed before restart)",
		)
	}

	if hasClass(r.Classification, ClassConvergenceRisk) && !hasClass(r.Classification, ClassStateMismatch) {
		steps = append(steps,
			"Inspect convergence committer (workflow step that stamps CONVERGED)",
			"Verify desired → installed → runtime progression is not short-circuited",
		)
	}

	if len(r.Cycles) > 0 {
		steps = append(steps,
			"Resolve dependency cycles before proceeding (see cycles section)",
		)
	}

	if len(r.Invariants) > 0 {
		steps = append(steps, "Review impacted invariants: "+strings.Join(r.Invariants, ", "))
	}

	if len(r.FailureModes) > 0 {
		steps = append(steps, "Review known failure modes: "+strings.Join(r.FailureModes, ", "))
	}

	if len(r.Files) > 0 {
		steps = append(steps, "Inspect impacted files: "+strings.Join(r.Files, ", "))
	}

	if r.DidWeFix != nil && r.DidWeFix.Status == string(fixledger.FixPartial) {
		steps = append(steps, "Review remaining gaps from partial fix: "+strings.Join(r.DidWeFix.RemainingGaps, ", "))
	}

	if len(r.RequiredTests) > 0 {
		steps = append(steps, "Run required tests before committing: "+strings.Join(r.RequiredTests, ", "))
	}

	if r.PackageAdmission != nil && r.PackageAdmission.Status == string(analysis.AdmissionBlock) {
		steps = append(steps, "Resolve package admission BLOCKs before merging")
	}

	if len(steps) == 0 {
		steps = append(steps, "No specific order — verify with agent-context and impact analysis")
	}

	return steps
}

// buildAgentInstruction produces a concise instruction sentence.
func buildAgentInstruction(r *Report) string {
	var parts []string

	if hasClass(r.Classification, ClassArchitectureSensitive) || hasClass(r.Classification, ClassConvergenceRisk) {
		parts = append(parts, "This task is architecture-sensitive. Do not apply a local fix without checking the impacted invariants and forbidden fixes listed above.")
	}

	if hasClass(r.Classification, ClassRestartStorm) {
		parts = append(parts, "Restart storms in Globular must go through the singleflight restart gate — never restart a service directly from a convergence tick.")
	}

	if hasClass(r.Classification, ClassStateMismatch) {
		parts = append(parts, "State mismatches must be resolved at the correct layer (Desired → Installed → Runtime). Do not patch the symptom at the runtime layer.")
	}

	if r.DidWeFix != nil && r.DidWeFix.Status == string(fixledger.FixDone) {
		parts = append(parts, "This class of problem has already been fixed. Check fix_cases.yaml for the exact file and test coverage before re-implementing.")
	}

	if r.DidWeFix != nil && r.DidWeFix.Status == string(fixledger.FixRegressed) {
		parts = append(parts, "A regression has been detected. Do not add a new workaround — find the regression root cause and restore the original fix.")
	}

	if len(r.ForbiddenFixes) > 0 {
		parts = append(parts, "The following fixes are explicitly forbidden: "+strings.Join(r.ForbiddenFixes, "; ")+".")
	}

	if len(r.RequiredTests) > 0 {
		parts = append(parts, "Before submitting, run: "+strings.Join(r.RequiredTests, ", ")+".")
	}

	if len(parts) == 0 {
		return "No specific constraint detected. Proceed with standard code review and test coverage."
	}

	return strings.Join(parts, " ")
}

// loadAliases loads context aliases from docs/awareness/context_aliases.yaml.
func loadAliases(docsDir string) learning.ContextAliasMap {
	if docsDir == "" {
		return learning.ContextAliasMap{}
	}
	aliases, _ := learning.LoadContextAliases(filepath.Join(docsDir, "context_aliases.yaml"))
	return aliases
}

// loadFixCases loads fix_cases.yaml from docs dir.
func loadFixCases(docsDir string) []fixledger.FixCase {
	if docsDir == "" {
		return nil
	}
	cases, _ := fixledger.LoadFixCases(filepath.Join(docsDir, "fix_cases.yaml"))
	return cases
}

// matchAliases returns the alias keys that fired for the task.
func matchAliases(task string, aliasMap learning.ContextAliasMap) []string {
	lower := strings.ToLower(task)
	var matched []string
	seen := make(map[string]bool)
	for targetID, phrases := range aliasMap {
		for _, phrase := range phrases {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				if !seen[targetID] {
					seen[targetID] = true
					matched = append(matched, targetID)
				}
				break
			}
		}
	}
	return matched
}

// appendClass adds c to classes if not already present.
func appendClass(classes []TaskClass, c TaskClass) []TaskClass {
	for _, existing := range classes {
		if existing == c {
			return classes
		}
	}
	return append(classes, c)
}

// unique deduplicates a string slice preserving order.
func unique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// mergeRuntime collects a runtime snapshot and merges its findings into the report.
func mergeRuntime(ctx context.Context, opts Options, g *graph.Graph, r *Report) *Report {
	bridge := opts.Bridge
	if bridge == nil {
		bridge = runtime.NewBridge("", "")
	}
	window := opts.RuntimeWindow
	if window <= 0 {
		window = 15 * time.Minute
	}

	snap, err := bridge.Snapshot(ctx, window, g)
	if err != nil {
		r.Warnings = append(r.Warnings, "runtime snapshot failed: "+err.Error())
		return r
	}

	// Build compact runtime section.
	rs := &RuntimeSection{
		Included:            true,
		CapturedAt:          snap.CapturedAt.Format(time.RFC3339),
		MatchedInvariants:   snap.MatchedInvariants,
		MatchedFailureModes: snap.MatchedFailureModes,
		MetricWarnings:      metricWarnings(snap.Warnings),
		Warnings:            snap.Warnings,
		DoctorFindings:      make([]DoctorFindingSummary, 0, len(snap.DoctorFindings)),
		ServiceStatuses:     make([]ServiceStatusSummary, 0, len(snap.RuntimeServices)),
		WorkflowReceipts:    make([]WorkflowReceiptSummary, 0, len(snap.WorkflowReceipts)),
		StateDeltas:         make([]StateDeltaSummary, 0, len(snap.StateDelta)),
	}

	for _, f := range snap.DoctorFindings {
		if !f.Suppressed {
			rs.DoctorFindings = append(rs.DoctorFindings, DoctorFindingSummary{
				ID:       f.FindingID,
				Severity: f.Severity,
				Title:    f.Title,
			})
		}
	}
	for _, svc := range snap.RuntimeServices {
		rs.ServiceStatuses = append(rs.ServiceStatuses, ServiceStatusSummary{
			ServiceID: svc.ServiceID,
			State:     svc.State,
			NodeID:    svc.NodeID,
		})
	}
	for _, wf := range snap.WorkflowReceipts {
		rs.WorkflowReceipts = append(rs.WorkflowReceipts, WorkflowReceiptSummary{
			WorkflowType: wf.WorkflowType,
			Status:       wf.Status,
			ErrorMsg:     wf.ErrorMsg,
		})
	}
	for _, d := range snap.StateDelta {
		rs.StateDeltas = append(rs.StateDeltas, StateDeltaSummary{
			ServiceID: d.ServiceID,
			DeltaType: d.DeltaType,
			Desired:   d.DesiredVersion,
			Installed: d.InstalledVersion,
		})
	}

	// Attach live workflow runtime section from graph overlay.
	rs.WorkflowRuntime = buildWorkflowRuntimeSection(ctx, g)
	r.Runtime = rs

	// Merge matched invariants and failure modes (deduplicate).
	r.Invariants = unique(append(r.Invariants, snap.MatchedInvariants...))
	r.FailureModes = unique(append(r.FailureModes, snap.MatchedFailureModes...))
	r.Warnings = append(r.Warnings, snap.Warnings...)

	// Propagate workflow runtime stale/failed as a blind spot and lower confidence.
	if rs.WorkflowRuntime != nil {
		switch rs.WorkflowRuntime.Coverage {
		case "stale":
			r.BlindSpots = append(r.BlindSpots, "workflow_runtime_stale: live workflow overlay is expired — rebuild with --collect-workflow")
		case "failed":
			r.BlindSpots = append(r.BlindSpots, "workflow_runtime_failed: live workflow overlay collection failed — source may be unreachable")
		case "not_checked", "disabled":
			r.BlindSpots = append(r.BlindSpots, "workflow_runtime_not_checked: no live workflow overlay in graph — run awareness build --collect-workflow to enable")
		}
	}

	// Adjust classification based on runtime evidence.
	if len(snap.StateDelta) > 0 {
		r.Classification = appendClass(r.Classification, ClassStateMismatch)
		r.Classification = appendClass(r.Classification, ClassConvergenceRisk)
	}

	// Runtime warnings can promote static preflight into dynamic risk.
	for _, w := range snap.Warnings {
		lw := strings.ToLower(w)
		if strings.Contains(lw, "start-limit-hit") {
			r.Classification = appendClass(r.Classification, ClassRestartStorm)
		}
		if strings.Contains(lw, "metric saturation") || strings.Contains(lw, "metric error signal") {
			r.Classification = appendClass(r.Classification, ClassRuntimeIncident)
			r.Classification = appendClass(r.Classification, ClassConvergenceRisk)
		}
	}

	// Critical doctor findings → ClassArchitectureSensitive.
	for _, f := range snap.DoctorFindings {
		if !f.Suppressed && f.Severity == "critical" {
			r.Classification = appendClass(r.Classification, ClassArchitectureSensitive)
			break
		}
	}

	// Repository non-NORMAL mode → ClassArchitectureSensitive.
	for _, rs2 := range snap.RepositoryStatus {
		if rs2.Mode != "NORMAL" && rs2.Mode != "" {
			r.Classification = appendClass(r.Classification, ClassArchitectureSensitive)
			break
		}
	}

	return r
}

// block-level reference to keep import alive for buildInvestigationOrder
var _ = analysis.AdmissionBlock

// computeGoFileCoverage walks repoRoot to count eligible Go files and compares
// them against source_file nodes indexed in g. Duplicates the core walk from
// enforce.GoFileCoverage to avoid the circular import (enforce → preflight).
//
// Mirrors enforce.GoFileCoverage's empty-repoRoot guard: when no source is
// available to walk, returns with ConfidenceImpact="unknown" and a blind
// spot — never zero counts that downstream consumers can collapse into a
// "0% coverage critical" finding. See
// awareness.source_scan_requires_verified_repo_root.
func computeGoFileCoverage(ctx context.Context, g *graph.Graph, repoRoot string) *GoFileCoverageReport {
	res := &GoFileCoverageReport{}

	if repoRoot == "" {
		res.ConfidenceImpact = "unknown"
		res.BlindSpots = []string{"repo root not provided — cannot measure Go file coverage"}
		return res
	}

	excludedDirs := map[string]bool{
		"vendor": true, ".git": true, "node_modules": true,
		"dist": true, "build": true, ".cache": true,
	}
	isExcluded := func(rel string) bool {
		parts := strings.SplitN(rel, string(os.PathSeparator), 2)
		return excludedDirs[parts[0]]
	}
	isGeneratedProto := func(rel string) bool {
		return strings.HasSuffix(rel, ".pb.go") || strings.HasSuffix(rel, ".pb.gw.go")
	}

	eligibleSet := map[string]bool{}
	_ = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(repoRoot, path)
		if info.IsDir() {
			if isExcluded(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if isExcluded(rel) || !strings.HasSuffix(rel, ".go") || isGeneratedProto(rel) {
			return nil
		}
		eligibleSet[rel] = true
		res.EligibleGoFilesTotal++
		if !strings.HasSuffix(rel, "_test.go") {
			res.EligibleNonTestGoFiles++
		}
		return nil
	})

	if g == nil {
		res.ConfidenceImpact = "low"
		res.BlindSpots = []string{fmt.Sprintf("%d eligible Go files cannot be checked — graph not loaded", res.EligibleGoFilesTotal)}
		return res
	}

	nodes, err := g.FindNodesByType(ctx, graph.NodeTypeSourceFile)
	if err != nil {
		res.ConfidenceImpact = "low"
		res.BlindSpots = []string{"graph source_file query failed: " + err.Error()}
		return res
	}

	indexedSet := map[string]bool{}
	for _, n := range nodes {
		if n.Path == "" {
			continue
		}
		p := filepath.ToSlash(n.Path)
		indexedSet[p] = true
		if strings.HasSuffix(p, ".go") {
			res.IndexedGoFilesTotal++
			if !strings.HasSuffix(p, "_test.go") {
				res.IndexedNonTestGoFiles++
			}
		}
	}

	for rel := range eligibleSet {
		if !indexedSet[filepath.ToSlash(rel)] {
			res.MissingFiles = append(res.MissingFiles, rel)
		}
	}

	if res.EligibleGoFilesTotal > 0 {
		res.CoveragePercentGoFiles = float64(res.IndexedGoFilesTotal) / float64(res.EligibleGoFilesTotal) * 100
	}
	if res.EligibleNonTestGoFiles > 0 {
		// stored in struct but not used in confidence path here; kept for completeness
		_ = float64(res.IndexedNonTestGoFiles) / float64(res.EligibleNonTestGoFiles) * 100
	}

	missing := len(res.MissingFiles)
	switch {
	case res.CoveragePercentGoFiles < 70.0:
		res.ConfidenceImpact = "high"
		res.BlindSpots = append(res.BlindSpots,
			fmt.Sprintf("%d eligible Go files are not represented in the graph (coverage %.1f%% < 70%%)", missing, res.CoveragePercentGoFiles))
	case res.CoveragePercentGoFiles < 85.0:
		res.ConfidenceImpact = "medium"
		res.BlindSpots = append(res.BlindSpots,
			fmt.Sprintf("%d eligible Go files are not represented in the graph (coverage %.1f%% < 85%%)", missing, res.CoveragePercentGoFiles))
	default:
		res.ConfidenceImpact = "none"
	}
	return res
}

func metricWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		lw := strings.ToLower(w)
		if strings.Contains(lw, "metric ") || strings.Contains(lw, "saturation") {
			out = append(out, w)
		}
	}
	return out
}

// lowTrustLevels are VerificationLevel values that indicate a match is present
// in the graph but not yet fully validated. The match is kept in the main result
// lists — it is NOT suppressed — but also reported in FilteredMatches with the
// reason so callers can lower their confidence accordingly.
var lowTrustLevels = map[string]bool{
	integrity.TrustStale:    true,
	integrity.TrustInferred: true,
	integrity.TrustProposal: true,
	integrity.TrustInvalid:  true,
}

// trustReasonFor maps a trust level to the human-readable reason reported in
// FilteredMatch.Reason. When the node has no trust metadata the reason is
// "missing_provenance" — not an error, but worth surfacing.
func trustReasonFor(level string) string {
	switch level {
	case integrity.TrustStale:
		return "stale"
	case integrity.TrustInferred:
		return "inferred"
	case integrity.TrustProposal:
		return "proposal"
	case integrity.TrustInvalid:
		return "invalid"
	default:
		return level
	}
}

// checkNodesTrust inspects matched invariant/failure_mode/forbidden_fix node
// metadata for low trust provenance. It does not remove the matches from the
// main result; it returns a parallel FilteredMatch slice so callers know which
// graph findings carry reduced confidence.
func checkNodesTrust(ctx context.Context, g *graph.Graph, invariantIDs, failureModeIDs, forbiddenFixIDs []string) []FilteredMatch {
	var out []FilteredMatch

	check := func(id, kind, nodePrefix string) {
		nodeID := nodePrefix + id
		n, err := g.FindNode(ctx, nodeID)
		if err != nil || n == nil {
			return
		}
		tl, _ := n.Metadata["trust_level"].(string)
		if tl == "" {
			tl, _ = n.Metadata["verification_level"].(string)
		}
		if tl != "" && lowTrustLevels[tl] {
			out = append(out, FilteredMatch{
				ID:         id,
				Kind:       kind,
				Reason:     trustReasonFor(tl),
				TrustLevel: tl,
			})
		}
	}

	for _, id := range invariantIDs {
		check(id, "invariant", "invariant:")
	}
	for _, id := range failureModeIDs {
		check(id, "failure_mode", "failure_mode:")
	}
	for _, id := range forbiddenFixIDs {
		check(id, "forbidden_fix", "forbidden_fix:")
	}
	return out
}

// buildWorkflowRuntimeSection reads workflow_run overlay nodes from the graph
// and summarises their freshness and coverage for the preflight report.
// Returns nil when the graph is nil or no workflow nodes exist.
func buildWorkflowRuntimeSection(ctx context.Context, g *graph.Graph) *WorkflowRuntimeSection {
	if g == nil {
		return nil
	}
	runs, err := g.FindNodesByType(ctx, graph.NodeTypeWorkflowRun)
	if err != nil || len(runs) == 0 {
		return &WorkflowRuntimeSection{
			Coverage:        "not_checked",
			Freshness:       "unknown",
			Source:          "none",
			CollectorStatus: "disabled",
		}
	}

	ws := &WorkflowRuntimeSection{
		Source:          "graph_cache",
		CollectorStatus: "ok",
	}
	now := time.Now()
	stale := false
	for _, n := range runs {
		ws.RunsSeen++
		if status, ok := n.Metadata["status"].(string); ok {
			if status == "failed" {
				ws.FailedRuns++
			}
			if status == "blocked" {
				ws.BlockedRuns++
			}
		}
		// Check TTL freshness.
		if expiresStr, ok := n.Metadata["expires_at"].(string); ok {
			exp, parseErr := time.Parse(time.RFC3339, expiresStr)
			if parseErr == nil && now.After(exp) {
				stale = true
			}
		}
		if collectedAt, ok := n.Metadata["collected_at"].(string); ok && ws.CollectedAt == "" {
			ws.CollectedAt = collectedAt
		}
		if ttl, ok := n.Metadata["ttl_seconds"].(int); ok && ws.TTLSeconds == 0 {
			ws.TTLSeconds = ttl
		}
	}

	if stale {
		ws.Freshness = "stale"
		ws.Coverage = "stale"
	} else {
		ws.Freshness = "fresh"
		if ws.FailedRuns > 0 || ws.BlockedRuns > 0 {
			ws.Coverage = "checked_with_matches"
		} else {
			ws.Coverage = "checked_clean"
		}
	}

	return ws
}

// ComputeLiveOverlayFreshness checks when the last live-snapshot was run
// and returns a freshness report. Returns status "absent" if never run.
// Exported so tests in other packages can call it directly.
func ComputeLiveOverlayFreshness(ctx context.Context, g *graph.Graph, now time.Time) *LiveOverlayFreshness {
	if now.IsZero() {
		now = time.Now()
	}
	rec, err := g.LatestLiveSnapshotRecord(ctx)
	if err != nil || rec == nil {
		return &LiveOverlayFreshness{Status: "absent"}
	}

	age := now.Unix() - rec.CreatedAt
	ageSeconds := float64(age)

	status := "fresh"
	if ageSeconds > float64(LiveOverlayStaleSeconds) {
		status = "absent"
	} else if ageSeconds > float64(LiveOverlayTTLSeconds) {
		status = "stale"
	}

	// Derive status from collector health if any collectors failed.
	okCount, failCount := 0, 0
	var collectors []CollectorHealthSummary
	for _, ch := range rec.CollectorHealth {
		c := CollectorHealthSummary{
			CollectorID:  ch.CollectorID,
			Status:       ch.Status,
			NodesEmitted: ch.NodesEmitted,
			Error:        ch.Error,
		}
		collectors = append(collectors, c)
		if ch.Status == "error" || ch.Status == "failed" {
			failCount++
		} else {
			okCount++
		}
	}
	if status == "fresh" && failCount > 0 && okCount == 0 {
		status = "failed"
	} else if status == "fresh" && failCount > 0 {
		status = "partial"
	}

	collectedAt := time.Unix(rec.CreatedAt, 0).UTC().Format(time.RFC3339)
	return &LiveOverlayFreshness{
		Status:      status,
		AgeSeconds:  ageSeconds,
		CollectedAt: collectedAt,
		Collectors:  collectors,
	}
}

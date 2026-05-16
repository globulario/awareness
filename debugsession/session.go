// Package debugsession orchestrates a guided debugging plan for AI agents.
// It composes preflight, semantic navigation, runtime evidence, fix-ledger,
// and node context into a single actionable DebugSessionReport.
//
// This package is read-only — it never mutates runtime state, edits code,
// promotes proposals, or dispatches remediation.
package debugsession

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/runtime"
)

// Options configures a debug session.
type Options struct {
	Task           string
	Files          []string
	PackagePath    string
	Phase          string
	DocsDir        string
	IncludeRuntime bool
	RuntimeWindow  time.Duration
	Bridge         *runtime.RuntimeBridge
}

// DebugSessionReport is the complete output of a debug session.
type DebugSessionReport struct {
	Task                   string                             `json:"task"`
	Classification         []preflight.TaskClass              `json:"classification"`
	Confidence             string                             `json:"confidence"`
	StartingNodes          []StartingNode                     `json:"starting_nodes"`
	RuntimeEvidence        *preflight.RuntimeSection          `json:"runtime_evidence,omitempty"`
	LikelyRootCausePaths   []RootCausePath                    `json:"likely_root_cause_paths"`
	RelevantInvariants     []string                           `json:"relevant_invariants"`
	RelevantFailureModes   []string                           `json:"relevant_failure_modes"`
	RelevantFixCases       []string                           `json:"relevant_fix_cases"`
	ForbiddenFixes         []string                           `json:"forbidden_fixes"`
	SuggestedFiles         []string                           `json:"suggested_files"`
	SuggestedSymbols       []string                           `json:"suggested_symbols"`
	PackageContext         *preflight.PackageAdmissionSection `json:"package_context,omitempty"`
	DependencyCycleRisks   []preflight.CycleWarning           `json:"dependency_cycle_risks"`
	RequiredTests          []string                           `json:"required_tests"`
	RequiredSearches       []string                           `json:"required_searches"`
	InvestigationPlan      []string                           `json:"investigation_plan"`
	DoNotDo                []string                           `json:"do_not_do"`
	LearningRecommendation string                             `json:"learning_recommendation"`
	Warnings               []string                           `json:"warnings"`
}

// StartingNode is a resolved graph node that serves as a debug entry point.
type StartingNode struct {
	NodeID   string `json:"node_id"`
	NodeType string `json:"node_type"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Source   string `json:"source"` // "file", "preflight_impact", "alias", "runtime", "semantic"
}

// RootCausePath is a semantic connection from a starting node to a likely root cause.
type RootCausePath struct {
	StartNodeID     string   `json:"start_node_id"`
	StartNodeName   string   `json:"start_node_name"`
	TargetNodeID    string   `json:"target_node_id"`
	TargetNodeName  string   `json:"target_node_name"`
	TargetNodeType  string   `json:"target_node_type"` // invariant, failure_mode, forbidden_fix
	PathSummary     string   `json:"path_summary"`
	SemanticCost    float64  `json:"semantic_cost"`
	WhyItMatters    string   `json:"why_it_matters"`
	ForbiddenFixes  []string `json:"forbidden_fixes,omitempty"`
	RequiredTests   []string `json:"required_tests,omitempty"`
	Severity        string   `json:"severity"` // "critical", "warning", "info"
	FixLedgerStatus string   `json:"fix_ledger_status,omitempty"`
}

// Run executes a debug session and returns a structured report.
// g may be nil — graph-dependent sections degrade gracefully with warnings.
func Run(ctx context.Context, opts Options, g *graph.Graph) (*DebugSessionReport, error) {
	// Phase 1: preflight for classification, fix-ledger, aliases, runtime.
	pfOpts := preflight.Options{
		Task:           opts.Task,
		Files:          opts.Files,
		PackagePath:    opts.PackagePath,
		Phase:          opts.Phase,
		DocsDir:        opts.DocsDir,
		IncludeRuntime: opts.IncludeRuntime,
		RuntimeWindow:  opts.RuntimeWindow,
		Bridge:         opts.Bridge,
	}
	pf, err := preflight.Run(ctx, pfOpts, g)
	if err != nil {
		return nil, fmt.Errorf("preflight: %w", err)
	}

	report := &DebugSessionReport{
		Task:                 opts.Task,
		Classification:       pf.Classification,
		RuntimeEvidence:      pf.Runtime,
		RelevantInvariants:   dedup(pf.Invariants),
		RelevantFailureModes: dedup(pf.FailureModes),
		ForbiddenFixes:       dedup(pf.ForbiddenFixes),
		PackageContext:       pf.PackageAdmission,
		DependencyCycleRisks: pf.Cycles,
		RequiredTests:        dedup(pf.RequiredTests),
		RequiredSearches:     dedup(pf.RequiredSearches),
		DoNotDo:              dedup(pf.ForbiddenFixes),
		SuggestedFiles:       dedup(pf.Files),
		Warnings:             pf.Warnings,
	}

	// Propagate fix-ledger status as a warning.
	if pf.DidWeFix != nil {
		report.RelevantFixCases = pf.DidWeFix.FixCases
		if pf.DidWeFix.Status != "" && pf.DidWeFix.NextAction != "" {
			report.Warnings = append(report.Warnings,
				"fix-ledger: "+pf.DidWeFix.Status+" — "+pf.DidWeFix.NextAction)
		}
	}

	// Phase 2: starting node selection.
	if g != nil {
		report.StartingNodes = selectStartingNodes(ctx, g, opts, pf)
	}

	// Phase 3: root-cause paths from starting nodes.
	if g != nil && len(report.StartingNodes) > 0 {
		paths, files, symbols := buildRootCausePaths(ctx, g, report.StartingNodes, pf)
		report.LikelyRootCausePaths = paths
		report.SuggestedFiles = dedup(append(report.SuggestedFiles, files...))
		report.SuggestedSymbols = dedup(symbols)

		for _, p := range paths {
			switch p.TargetNodeType {
			case graph.NodeTypeInvariant:
				report.RelevantInvariants = dedup(append(report.RelevantInvariants, p.TargetNodeName))
			case graph.NodeTypeFailureMode:
				report.RelevantFailureModes = dedup(append(report.RelevantFailureModes, p.TargetNodeName))
			}
			report.ForbiddenFixes = dedup(append(report.ForbiddenFixes, p.ForbiddenFixes...))
			report.RequiredTests = dedup(append(report.RequiredTests, p.RequiredTests...))
		}
		report.DoNotDo = dedup(report.ForbiddenFixes)
	}

	// Phase 4: confidence rating.
	report.Confidence = rateConfidence(report)

	// Phase 5: investigation plan.
	report.InvestigationPlan = buildInvestigationPlan(report, pf)

	// Phase 6: learning recommendation.
	report.LearningRecommendation = buildLearningRecommendation(report, pf)

	return report, nil
}

// dedup removes empty strings and duplicates, preserving insertion order.
func dedup(in []string) []string {
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

package debugsession

import (
	"fmt"
	"strings"

	"github.com/globulario/awareness/preflight"
)

// buildInvestigationPlan generates an ordered investigation sequence.
func buildInvestigationPlan(r *DebugSessionReport, pf *preflight.Report) []string {
	var steps []string

	// Step 1: always start by verifying the correct 4-layer.
	steps = append(steps,
		"Verify the failing layer: walk Repository → Desired → Installed → Runtime in order. "+
			"The symptom layer is rarely the root-cause layer.")

	// Step 2: class-specific investigation.
	for _, c := range r.Classification {
		switch c {
		case preflight.ClassStateMismatch:
			steps = append(steps,
				"State mismatch: inspect ComputeInfrastructureDesiredHash, verify "+
					"build_id propagation (repository→controller→node-agent→etcd), and "+
					"confirm installed-state stamping completes before the next heartbeat.")

		case preflight.ClassRestartStorm:
			steps = append(steps,
				"Restart storm: verify the singleflight gate prevents concurrent restarts "+
					"per service per convergence tick. Check for stale SIGTERM and "+
					"start-limit-hit in the systemd journal.")

		case preflight.ClassConvergenceRisk:
			steps = append(steps,
				"Convergence risk: inspect the convergence committer that stamps CONVERGED. "+
					"Verify the desired→installed→runtime progression is not short-circuited.")

		case preflight.ClassDependencyCycle:
			if len(r.DependencyCycleRisks) > 0 {
				steps = append(steps,
					"Resolve dependency cycle before editing: "+cycleDesc(r.DependencyCycleRisks[0]))
			}

		case preflight.ClassUnknownImpact:
			steps = append(steps,
				"UNKNOWN_IMPACT: no awareness facts matched this task. "+
					"Query the graph before editing: 'globular awareness node-context --node <service>'")
		}
	}

	// Step 3: top root-cause path.
	if len(r.LikelyRootCausePaths) > 0 {
		top := r.LikelyRootCausePaths[0]
		steps = append(steps,
			fmt.Sprintf("Inspect top root-cause path (cost %.1f): %s", top.SemanticCost, top.PathSummary))
		if top.WhyItMatters != "" {
			steps = append(steps, "Why it matters: "+top.WhyItMatters)
		}
	}

	// Step 4: files to inspect.
	if len(r.SuggestedFiles) > 0 {
		steps = append(steps, "Read these files before editing: "+strings.Join(r.SuggestedFiles, ", "))
	}

	// Step 5: forbidden fixes.
	if len(r.ForbiddenFixes) > 0 {
		steps = append(steps,
			"Check forbidden fixes — do not apply: "+strings.Join(r.ForbiddenFixes, "; "))
	}

	// Step 6: required tests.
	if len(r.RequiredTests) > 0 {
		steps = append(steps,
			"Run required tests before committing: "+strings.Join(r.RequiredTests, ", "))
	}

	// Step 7: invariant review.
	if len(r.RelevantInvariants) > 0 {
		steps = append(steps,
			"Review impacted invariants: "+strings.Join(r.RelevantInvariants, ", "))
	}

	// Step 8: failure mode review.
	if len(r.RelevantFailureModes) > 0 {
		steps = append(steps,
			"Review known failure modes: "+strings.Join(r.RelevantFailureModes, ", "))
	}

	// Step 9: fix-ledger.
	if len(r.RelevantFixCases) > 0 && pf.DidWeFix != nil && pf.DidWeFix.NextAction != "" {
		steps = append(steps,
			"Fix-ledger: "+strings.Join(r.RelevantFixCases, ", ")+" — "+pf.DidWeFix.NextAction)
	}

	// Step 10: runtime evidence.
	if r.RuntimeEvidence != nil && r.RuntimeEvidence.Included {
		steps = append(steps,
			"Review runtime evidence: doctor findings, state deltas, and workflow receipts in runtime_evidence.")
	}

	// Step 11: learning.
	if r.LearningRecommendation != "" {
		steps = append(steps, "If this is a new failure class: "+r.LearningRecommendation)
	}

	return steps
}

// cycleDesc produces a one-line description of a cycle warning.
func cycleDesc(cw preflight.CycleWarning) string {
	if len(cw.Path) > 0 {
		return strings.Join(cw.Path, " → ")
	}
	return "phase " + cw.Phase
}

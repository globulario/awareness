package debugsession

import (
	"strings"

	"github.com/globulario/awareness/preflight"
)

// rateConfidence assigns a confidence label based on how many evidence sources fired.
func rateConfidence(r *DebugSessionReport) string {
	for _, c := range r.Classification {
		if c == preflight.ClassUnknownImpact {
			return "UNKNOWN"
		}
	}

	hasNodes := len(r.StartingNodes) > 0
	hasPaths := len(r.LikelyRootCausePaths) > 0
	hasInvariants := len(r.RelevantInvariants) > 0
	hasRuntime := r.RuntimeEvidence != nil && r.RuntimeEvidence.Included &&
		(len(r.RuntimeEvidence.DoctorFindings) > 0 ||
			len(r.RuntimeEvidence.StateDeltas) > 0 ||
			len(r.RuntimeEvidence.WorkflowReceipts) > 0)
	hasFixCases := len(r.RelevantFixCases) > 0

	switch {
	case hasNodes && hasPaths && hasInvariants && (hasRuntime || hasFixCases):
		return "HIGH"
	case hasNodes && hasPaths && hasInvariants:
		return "MEDIUM"
	case hasNodes && hasPaths:
		return "MEDIUM"
	case hasNodes || hasInvariants:
		return "LOW"
	default:
		return "LOW"
	}
}

// buildLearningRecommendation suggests whether to create a learning proposal.
func buildLearningRecommendation(r *DebugSessionReport, pf *preflight.Report) string {
	noFixCases := len(r.RelevantFixCases) == 0

	didWeFixUnknown := false
	if pf.DidWeFix != nil && pf.DidWeFix.Status == "UNKNOWN" {
		didWeFixUnknown = true
	}

	isArchSensitive := false
	for _, c := range r.Classification {
		if c == preflight.ClassArchitectureSensitive {
			isArchSensitive = true
			break
		}
	}

	newPattern := (len(r.LikelyRootCausePaths) > 0 || len(r.RelevantFailureModes) > 0) &&
		noFixCases && didWeFixUnknown

	if newPattern {
		return "New failure pattern detected. After resolving, create an incident proposal: " +
			"'globular awareness propose --incident INC-<ID> --task \"" + r.Task + "\"'"
	}

	if isArchSensitive && noFixCases {
		return "Architecture-sensitive task with no prior fix record. " +
			"After resolving, add a fix case to docs/awareness/fix_cases.yaml."
	}

	return ""
}

// classString formats a classification list as a readable summary.
func classString(classes []preflight.TaskClass) string {
	if len(classes) == 0 {
		return string(preflight.ClassLocalCodeChange)
	}
	parts := make([]string, len(classes))
	for i, c := range classes {
		parts[i] = string(c)
	}
	return strings.Join(parts, " | ")
}

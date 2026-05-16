package preflight

import "strings"

// architectureSensitiveKeywords trigger ARCHITECTURE_SENSITIVE classification.
var architectureSensitiveKeywords = []string{
	"retry", "loop", "restart", "drift", "convergence",
	"desired", "installed", "runtime", "package install",
	"leader", "failover", "status 0", "start-limit-hit",
	"dependency", "deadlock", "missing state", "checksum",
	"desired_hash", "build_id",
}

// restartStormKeywords trigger RESTART_STORM + CONVERGENCE_RISK.
var restartStormKeywords = []string{
	"restart storm", "sigterm storm", "start-limit-hit",
}

// stateMismatchKeywords trigger STATE_MISMATCH + CONVERGENCE_RISK.
var stateMismatchKeywords = []string{
	"desired_hash", "checksum mismatch", "build_id mismatch",
}

// regressionKeywords trigger a DidWeFix query hint.
var regressionKeywords = []string{
	"did we already fix", "again", "same bug", "regression",
}

// ClassifyTask returns the set of TaskClass labels that apply to the task string.
// Classification is deterministic keyword matching — no LLM calls.
func ClassifyTask(task string) []TaskClass {
	lower := strings.ToLower(task)
	seen := make(map[TaskClass]bool)
	var classes []TaskClass

	add := func(c TaskClass) {
		if !seen[c] {
			seen[c] = true
			classes = append(classes, c)
		}
	}

	containsAny := func(keywords []string) bool {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
		return false
	}

	if containsAny(restartStormKeywords) {
		add(ClassRestartStorm)
		add(ClassConvergenceRisk)
	}

	if containsAny(stateMismatchKeywords) {
		add(ClassStateMismatch)
		add(ClassConvergenceRisk)
	}

	if containsAny(architectureSensitiveKeywords) {
		add(ClassArchitectureSensitive)
	}

	// RETRY_LOOP: explicit "retry loop" or "retry" + "loop" together.
	if strings.Contains(lower, "retry loop") ||
		(strings.Contains(lower, "retry") && strings.Contains(lower, "loop")) {
		add(ClassRetryLoop)
	}

	// RUNTIME_INCIDENT: incident, crash, oom, panic, fatal.
	for _, kw := range []string{"incident", "crash", "oom", "panic", "fatal"} {
		if strings.Contains(lower, kw) {
			add(ClassRuntimeIncident)
			break
		}
	}

	// PACKAGE_ADMISSION: "package install", "admit", "awareness.yaml".
	for _, kw := range []string{"package install", "admit", "awareness.yaml"} {
		if strings.Contains(lower, kw) {
			add(ClassPackageAdmission)
			break
		}
	}

	// DEPENDENCY_CYCLE: "dependency cycle", "circular".
	for _, kw := range []string{"dependency cycle", "circular dependency", "deadlock"} {
		if strings.Contains(lower, kw) {
			add(ClassDependencyCycle)
			break
		}
	}

	// Regression hint.
	if containsAny(regressionKeywords) {
		// CONVERGENCE_RISK signals the agent to run did-we-fix first.
		add(ClassConvergenceRisk)
	}

	if len(classes) == 0 {
		add(ClassLocalCodeChange)
	}

	return classes
}

// hasClass returns true if the class list contains c.
func hasClass(classes []TaskClass, c TaskClass) bool {
	for _, cl := range classes {
		if cl == c {
			return true
		}
	}
	return false
}

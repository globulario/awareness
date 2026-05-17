package preflight

// TaskClass labels the nature of a task for agent routing.
type TaskClass string

const (
	ClassLocalCodeChange       TaskClass = "LOCAL_CODE_CHANGE"
	ClassArchitectureSensitive TaskClass = "ARCHITECTURE_SENSITIVE"
	ClassConvergenceRisk       TaskClass = "CONVERGENCE_RISK"
	ClassPackageAdmission      TaskClass = "PACKAGE_ADMISSION"
	ClassRuntimeIncident       TaskClass = "RUNTIME_INCIDENT"
	ClassRetryLoop             TaskClass = "RETRY_LOOP"
	ClassRestartStorm          TaskClass = "RESTART_STORM"
	ClassStateMismatch         TaskClass = "STATE_MISMATCH"
	ClassDependencyCycle       TaskClass = "DEPENDENCY_CYCLE"
	ClassUnknownImpact         TaskClass = "UNKNOWN_IMPACT"
	ClassStaticFallback        TaskClass = "STATIC_KNOWLEDGE_FALLBACK"
)

// RawKnowledgeMatch is a conservative fallback match from the source YAML files.
// It exists to make NO_MATCH honest: graph lookup can be silent while the
// hand-authored truth files still contain relevant knowledge.
type RawKnowledgeMatch struct {
	Source       string   `json:"source"`
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	Score        int      `json:"score"`
	MatchedTerms []string `json:"matched_terms"`
}

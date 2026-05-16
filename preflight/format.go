package preflight

// Format is the output format for a preflight result.
type Format string

const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
	FormatAgent    Format = "agent"
)

// Verbosity controls how much output is emitted.
type Verbosity string

const (
	VerbosityCompact  Verbosity = "compact"
	VerbosityStandard Verbosity = "standard"
	VerbosityFull     Verbosity = "full"
)

// Budget selects which sections appear in agent format output.
//
// compact   — safety header, root-cause(3), forbidden(3), tests(5), warnings, safety/risk/confidence/trust, agent instruction
// standard  — all sections with existing top-N limits (current default)
// deep      — standard + full decision traces (no truncation), all pivots expanded
// forensic  — full verbosity across all sections
type Budget string

const (
	// BudgetCompact emits only the essential safety fields: classification
	// header, safety_status, risk_tier, confidence, trust, warnings, top-3
	// findings, top-3 forbidden fixes, top-5 required tests, agent
	// instruction. All other sections (decision traces, design patterns,
	// anti-patterns, code smells, did-we-fix, experience hints, required
	// searches, investigation order, package admission, cycles) are omitted.
	BudgetCompact Budget = "compact"

	// BudgetStandard is the current default: all sections with existing top-N
	// limits. Equivalent to no --budget flag.
	BudgetStandard Budget = "standard"

	// BudgetDeep is standard plus full decision traces (no truncation) with
	// all pivots expanded. Use for architecture-sensitive changes.
	BudgetDeep Budget = "deep"

	// BudgetForensic is full verbosity across all sections. Use only when the
	// cluster is actively broken or the root cause is unknown.
	BudgetForensic Budget = "forensic"
)

// RenderOptions controls preflight output rendering.
type RenderOptions struct {
	Verbosity Verbosity
	Budget    Budget
}

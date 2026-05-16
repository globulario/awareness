package assurance

// Trust Envelope — every awareness response gets one.
//
// The envelope is the answer to "should the consumer trust this awareness
// output enough to base a safety decision on it?" It is intentionally narrow:
// verdict + confidence + freshness + coverage + limitations, no more. A
// caller that only reads the envelope must still get a correct safety call.

import "time"

// FreshnessStatus is the single-value freshness verdict that pairs with the
// detailed Staleness report. The Staleness report says *what* is stale; the
// FreshnessStatus says how the consumer should treat the awareness output as
// a whole.
type FreshnessStatus string

const (
	FreshnessFresh          FreshnessStatus = "fresh"
	FreshnessStaleRepo      FreshnessStatus = "stale_repo"
	FreshnessStaleRuntime   FreshnessStatus = "stale_runtime"
	FreshnessStaleIncidents FreshnessStatus = "stale_incidents"
	FreshnessStaleTestIndex FreshnessStatus = "stale_test_index"
	FreshnessStaleUnknown   FreshnessStatus = "stale_unknown"
	FreshnessUnknown        FreshnessStatus = "unknown"
)

// TrustVerdict is the headline answer to "can the consumer act on this?".
//
// Hierarchy (worst → best): unsafe < unknown ≈ stale < limited < usable < trusted.
// "unknown" and "stale" are siblings: both block safety verdicts.
type TrustVerdict string

const (
	TrustTrusted TrustVerdict = "trusted"
	TrustUsable  TrustVerdict = "usable"
	TrustLimited TrustVerdict = "limited"
	TrustUnsafe  TrustVerdict = "unsafe"
	TrustStale   TrustVerdict = "stale"
	TrustUnknown TrustVerdict = "unknown"
)

// TrustCoverage is the coverage axis used by the trust envelope. It is a
// 4-step scale, distinct from the per-failure_mode CoverageLevel (orphan /
// theoretical / partial / well_covered) which is a *diagnostic* classification.
//
//	none       — no relevant coverage at all
//	partial    — mitigation OR test OR detector, but not all
//	sufficient — mitigation + (test OR detector); enough to reason
//	strong     — mitigation + test + detector; full triangulation
type TrustCoverage string

const (
	TrustCoverageNone       TrustCoverage = "none"
	TrustCoveragePartial    TrustCoverage = "partial"
	TrustCoverageSufficient TrustCoverage = "sufficient"
	TrustCoverageStrong     TrustCoverage = "strong"
)

// ConfidenceLevel is a 4-step subjective certainty scale that callers may
// surface alongside Verdict for human consumption.
type ConfidenceLevel string

const (
	ConfidenceNone   ConfidenceLevel = "none"
	ConfidenceLow    ConfidenceLevel = "low"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceHigh   ConfidenceLevel = "high"
)

// TrustEnvelope is the canonical "should I act on this awareness output?" verdict.
type TrustEnvelope struct {
	Verdict     TrustVerdict    `json:"verdict" yaml:"verdict"`
	Confidence  ConfidenceLevel `json:"confidence" yaml:"confidence"`
	Freshness   FreshnessStatus `json:"freshness" yaml:"freshness"`
	Coverage    TrustCoverage   `json:"coverage" yaml:"coverage"`
	Limitations []string        `json:"limitations,omitempty" yaml:"limitations,omitempty"`

	// Reason is a single human-readable line that summarises why the verdict
	// is what it is. Optional but strongly encouraged in tool output.
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`

	// RequiredActions are next steps the consumer should take before treating
	// the verdict as stronger. Empty for a TrustTrusted envelope.
	RequiredActions []string `json:"required_actions,omitempty" yaml:"required_actions,omitempty"`

	// Closure-loop provenance (P1-4): when the matched failure_mode has a
	// learning entry or a regression test wired into the graph, the agent
	// gets to see WHY the verdict is partly load-bearing rather than just
	// "trusted". Empty when no closure-loop evidence exists for the match.
	//
	// LearnedFromIncident: an incident_id (e.g. "INC-2026-0007") from
	//   incident_patterns whose pattern names this failure_mode. Smallest by
	//   id when multiple patterns exist — deterministic, stable across runs.
	// RegressionTest: a test name (e.g. "TestResumeRequiresReceipt") for a
	//   test that has a verifies/tested_by/validated_by edge to this
	//   failure_mode. Smallest by id when multiple tests exist.
	LearnedFromIncident string `json:"learned_from_incident,omitempty" yaml:"learned_from_incident,omitempty"`
	RegressionTest      string `json:"regression_test,omitempty" yaml:"regression_test,omitempty"`

	GeneratedAtUnix int64 `json:"generated_at_unix" yaml:"generated_at_unix"`
}

// ComposeInputs is the per-query data Compose folds into an envelope. It is
// kept separate from CoverageReport / Staleness so callers can synthesise an
// envelope for a single match without running the full meta-check.
type ComposeInputs struct {
	// MatchFound is true when the awareness query produced at least one match
	// (a failure_mode, invariant, forbidden_fix, etc.). False means NO_MATCH.
	MatchFound bool

	// PrimaryMatchKind names the type of awareness object the query primarily
	// matched. One of "failure_mode", "invariant", "forbidden_fix",
	// "raw_yaml", or "" (unknown). Used to keep the verdict reason honest:
	// FailureModeCoverage logic only fires when the match actually was a
	// failure_mode. See docs/awareness/composed_path_failures.md
	// (TrustEnvelope match-kind conflation).
	PrimaryMatchKind string

	// PerFailureMode lets Compose downgrade trust when the matched failure
	// mode itself is poorly covered. Optional; nil is treated as
	// "no per-mode info available."
	PerFailureMode *FailureModeCoverage

	// Staleness from CheckStaleness. Optional; nil means "freshness unknown."
	Staleness *Staleness
}

// MatchKind constants for ComposeInputs.PrimaryMatchKind. These are the
// shapes the verdict reason knows how to talk about honestly.
const (
	MatchKindFailureMode  = "failure_mode"
	MatchKindInvariant    = "invariant"
	MatchKindForbiddenFix = "forbidden_fix"
	MatchKindRawYAML      = "raw_yaml"
)

// Compose turns coverage + staleness into a TrustEnvelope per the rules in
// the assurance spec:
//
//	stale bundle => verdict cannot be trusted
//	NO_MATCH => verdict must be unsafe or unknown
//	coverage none => verdict must be unsafe or unknown
//	coverage partial => verdict no stronger than limited
//	coverage sufficient + fresh => usable
//	coverage strong + fresh + tests linked => trusted
//
// Compose is deterministic: same inputs → same envelope.
func Compose(in ComposeInputs) TrustEnvelope {
	env := TrustEnvelope{
		GeneratedAtUnix: time.Now().Unix(),
	}

	// --- Freshness leg --------------------------------------------------------
	env.Freshness = freshnessFromStaleness(in.Staleness)

	// --- Coverage leg ---------------------------------------------------------
	env.Coverage = coverageFromInputs(in)

	// --- Verdict --------------------------------------------------------------
	env.Verdict, env.Confidence, env.Reason = decideVerdict(in, env.Freshness, env.Coverage)

	// --- Limitations + RequiredActions ---------------------------------------
	env.Limitations, env.RequiredActions = explainGaps(in, env.Freshness, env.Coverage, env.Verdict)

	// --- Closure-loop provenance (P1-4) --------------------------------------
	// Only meaningful when the match was a failure_mode: invariant /
	// forbidden_fix / raw_yaml matches don't carry per-mode coverage data,
	// so there's no closure-loop pointer to surface. Empty strings stay
	// omitted via the `omitempty` JSON tag.
	if in.PerFailureMode != nil {
		env.LearnedFromIncident = in.PerFailureMode.LearnedFromIncident
		env.RegressionTest = in.PerFailureMode.FirstVerifyingTest
	}

	return env
}

// freshnessFromStaleness picks the single dominant freshness label. We err on
// the side of conservative — if any critical staleness alarm is present,
// freshness is reported as a stale_* status.
func freshnessFromStaleness(s *Staleness) FreshnessStatus {
	if s == nil {
		return FreshnessUnknown
	}
	if s.GraphStale {
		// Graph staleness covers both repo and test-index drift; we map
		// "graph older than YAML inputs" to stale_repo because it usually
		// means the source tree advanced past the graph.
		return FreshnessStaleRepo
	}
	for _, a := range s.Alarms {
		switch a.ID {
		case "bundle_age_exceeded":
			if a.Severity == AlarmCritical {
				return FreshnessStaleRepo
			}
		case "yaml_newer_than_graph":
			return FreshnessStaleRepo
		case "bundle_older_than_graph":
			return FreshnessStaleRepo
		case "unknown_role_knowledge_files":
			// YAML files the system genuinely can't classify (top-level key
			// is neither in the graph dispatcher nor the config allowlist).
			// THIS is what should cap trust — the older "untracked" rule
			// fired on every config-only file too, which permanently capped
			// the verdict at stale_unknown on a healthy graph.
			return FreshnessStaleUnknown
		}
	}
	if s.UnknownRoleYAMLCount > 0 {
		return FreshnessStaleUnknown
	}
	if !s.BundlePresent {
		// We have a graph but no bundle. The graph may be fresh, but the
		// shipped artifact is missing — runtime consumers cannot rely on it.
		return FreshnessStaleUnknown
	}
	return FreshnessFresh
}

// coverageFromInputs decides which axis the coverage leg uses. Failure-mode
// coverage applies only when the primary match was a failure_mode. For
// invariant-only / forbidden_fix-only / raw-YAML matches, the FM coverage
// vocabulary doesn't apply — we have YAML guidance but no failure-mode
// triangulation, which is honestly TrustCoveragePartial. Conflating the two
// caused the "matched a failure_mode with no enforcement" reason to fire
// for invariant-only matches; see docs/awareness/composed_path_failures.md
// (TrustEnvelope match-kind).
func coverageFromInputs(in ComposeInputs) TrustCoverage {
	// FM coverage is the most informative axis when applicable.
	if in.PrimaryMatchKind == "" || in.PrimaryMatchKind == MatchKindFailureMode {
		return coverageFromFailureMode(in.PerFailureMode)
	}
	// Non-FM match. We have at least one YAML/raw signal but no failure_mode
	// to triangulate against; report partial.
	return TrustCoveragePartial
}

// coverageFromFailureMode collapses our diagnostic CoverageLevel onto the
// trust 4-step coverage axis.
//
// Lifecycle hint propagation: an INTENTIONAL_GAP failure_mode reaches this
// function with Level=CoverageTheoretical (the classifier short-circuits
// for the lifecycle hint). Without special-casing, that maps to None →
// Unsafe verdict, which conflates "we deliberately accepted this gap" with
// "this is unenforced and we missed it." See
// docs/awareness/composed_path_failures.md (2026-05-10 intentional_gap
// conflated with orphan). The lifecycle hint is preserved on State, so we
// read it here and surface the distinction as TrustCoveragePartial.
// DEPRECATED stays None — deprecated ids are placeholders we shouldn't
// trust at all.
func coverageFromFailureMode(fmc *FailureModeCoverage) TrustCoverage {
	if fmc == nil {
		// Caller did not give us a per-mode coverage tuple — the most we can
		// honestly say is "we don't know."
		return TrustCoverageNone
	}
	if fmc.State == "INTENTIONAL_GAP" {
		// Deliberately-accepted gap. Acknowledge it as partial, not none —
		// the gap is reviewed and (typically) backed by a test or
		// architecture fix even if a runtime detector is absent.
		return TrustCoveragePartial
	}
	switch fmc.Level {
	case CoverageWellCovered:
		return TrustCoverageStrong
	case CoveragePartial:
		// Mitigation + test + detector → 3 legs → strong; we'd already be
		// CoverageWellCovered. Two legs → sufficient. One leg → partial.
		legs := 0
		if fmc.Mitigations > 0 {
			legs++
		}
		if fmc.Tests > 0 {
			legs++
		}
		if fmc.Detectors > 0 {
			legs++
		}
		if legs >= 2 {
			return TrustCoverageSufficient
		}
		return TrustCoveragePartial
	case CoverageTheoretical, CoverageOrphan:
		return TrustCoverageNone
	}
	return TrustCoverageNone
}

func decideVerdict(in ComposeInputs, fresh FreshnessStatus, cov TrustCoverage) (TrustVerdict, ConfidenceLevel, string) {
	// Rule 1: NO_MATCH must never be safe.
	if !in.MatchFound {
		return TrustUnknown, ConfidenceNone,
			"no awareness match found; coverage of this query is unknown — do not infer safety from silence"
	}

	// Rule 2: stale bundle blocks safety verdicts. We cap at TrustStale so
	// the consumer cannot mistake stale data for a usable verdict.
	if fresh != FreshnessFresh {
		return TrustStale, ConfidenceLow,
			"awareness inputs are stale (" + string(fresh) + ") — verdict held until awareness is regenerated"
	}

	// Rule 3+4: coverage gates verdict ceiling.
	switch cov {
	case TrustCoverageNone:
		// Match found but the failure_mode it points at has no enforcement.
		// This is the "rubber stamp" risk — we have a label but no laws.
		// Reason text adapts to the match kind so non-FM matches don't get
		// a false "orphan failure_mode" label.
		reason := "awareness matched a failure_mode with no enforcing mitigation, test, or detector"
		switch in.PrimaryMatchKind {
		case MatchKindInvariant:
			reason = "awareness matched an invariant but failure-mode triangulation is unavailable for this query"
		case MatchKindForbiddenFix:
			reason = "awareness matched a forbidden_fix but failure-mode triangulation is unavailable for this query"
		case MatchKindRawYAML:
			reason = "awareness matched raw YAML knowledge but no failure-mode coverage was available"
		}
		return TrustUnsafe, ConfidenceLow, reason
	case TrustCoveragePartial:
		return TrustLimited, ConfidenceLow,
			"awareness has partial coverage of this failure mode — treat verdict as a hint, not a clearance"
	case TrustCoverageSufficient:
		return TrustUsable, ConfidenceMedium,
			"awareness has two of (mitigation, test, detector) for this failure mode"
	case TrustCoverageStrong:
		// Tests linked is implied by strong coverage; we double-check.
		hasTest := in.PerFailureMode != nil && in.PerFailureMode.Tests > 0
		if hasTest {
			return TrustTrusted, ConfidenceHigh,
				"awareness has full triangulation (mitigation + test + detector) and inputs are fresh"
		}
		return TrustUsable, ConfidenceMedium,
			"awareness coverage is strong but no tests are linked — verdict capped at usable"
	}
	return TrustUnknown, ConfidenceNone, "unable to compute verdict"
}

// explainGaps lists what is missing for a stronger verdict and what the
// consumer should do next. Both lists are intentionally short — the trust
// envelope is a verdict, not a full report.
func explainGaps(in ComposeInputs, fresh FreshnessStatus, cov TrustCoverage, verdict TrustVerdict) ([]string, []string) {
	var limits, actions []string

	if !in.MatchFound {
		limits = append(limits, "no awareness match for this query")
		actions = append(actions,
			"inspect architecture manually",
			"create a provisional incident or invariant if this is a new pattern",
		)
	}

	if fresh != FreshnessFresh {
		limits = append(limits, "awareness inputs are stale: "+string(fresh))
		// Stale_repo means the YAML inputs advanced past the last graph
		// build (or the graph row is older than the max-age threshold). The
		// concrete fix is a clean rebuild — surface the exact command so
		// operators don't have to guess. Other stale flavours
		// (stale_runtime, stale_unknown, unknown) keep the generic message
		// because the right action depends on context.
		if fresh == FreshnessStaleRepo {
			actions = append(actions, "Run: globular awareness build --clean")
		} else {
			actions = append(actions, "regenerate awareness bundle and rebuild graph before treating verdict as authoritative")
		}
	}

	if in.PerFailureMode != nil {
		fmc := in.PerFailureMode
		if fmc.Tests == 0 {
			limits = append(limits, "no regression test linked to this failure mode")
			actions = append(actions, "add a regression test before treating awareness verdict as trusted")
		}
		if fmc.Mitigations == 0 {
			limits = append(limits, "no design pattern mitigates this failure mode")
		}
		if fmc.Detectors == 0 {
			limits = append(limits, "no runtime/metric/workflow detector for this failure mode")
		}
	} else if in.MatchFound {
		limits = append(limits, "per-failure_mode coverage tuple unavailable for this match")
	}

	switch verdict {
	case TrustTrusted:
		// no further actions required
	}
	return dedupStrings(limits), dedupStrings(actions)
}

func dedupStrings(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

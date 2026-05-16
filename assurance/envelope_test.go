package assurance_test

import (
	"strings"
	"testing"

	"github.com/globulario/awareness/assurance"
)

// TestCompose_NoMatchIsNeverSafe is the load-bearing test for the assurance
// layer's core principle: if awareness has no match, the verdict MUST be
// "unknown" — never trusted, never usable, never even limited. This is the
// rubber-stamp prevention rule.
func TestCompose_NoMatchIsNeverSafe(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound: false,
		Staleness:  &assurance.Staleness{}, // graph fresh, no alarms
	})
	if env.Verdict != assurance.TrustUnknown {
		t.Errorf("NO_MATCH verdict = %s, want unknown", env.Verdict)
	}
	if len(env.Limitations) == 0 {
		t.Error("expected limitations to be populated")
	}
	if len(env.RequiredActions) == 0 {
		t.Error("expected required_actions to be populated for NO_MATCH")
	}
}

// TestCompose_StaleBundleBlocksSafetyVerdict: even with a strong coverage
// match, stale inputs cap the verdict at TrustStale.
func TestCompose_StaleBundleBlocksSafetyVerdict(t *testing.T) {
	stale := &assurance.Staleness{
		GraphStale:       true,
		GraphStaleReason: "test: forced stale",
		Alarms: []assurance.Alarm{
			{ID: "graph_stale", Severity: assurance.AlarmCritical, Message: "test"},
		},
	}
	fmc := &assurance.FailureModeCoverage{
		ID:          "FM-strong",
		Mitigations: 2,
		Tests:       3,
		Detectors:   1,
		Level:       assurance.CoverageWellCovered,
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      stale,
	})
	if env.Verdict != assurance.TrustStale {
		t.Errorf("stale verdict = %s, want stale (must NOT be trusted/usable)", env.Verdict)
	}
}

// TestCompose_StrongCoverageFreshTrustedVerdict: the only path to TrustTrusted.
func TestCompose_StrongCoverageFreshTrustedVerdict(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:          "FM-good",
		Mitigations: 1,
		Tests:       2,
		Detectors:   1,
		Level:       assurance.CoverageWellCovered,
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict != assurance.TrustTrusted {
		t.Errorf("strong+fresh verdict = %s, want trusted", env.Verdict)
	}
	if env.Coverage != assurance.TrustCoverageStrong {
		t.Errorf("coverage = %s, want strong", env.Coverage)
	}
}

// TestCompose_StrongCoverageNoTestsCappedAtUsable: the spec says "trusted"
// requires tests linked. Even with full triangulation, missing test → usable.
// (We construct an unusual FailureModeCoverage where Level says
// well_covered but Tests=0 to exercise that branch.)
func TestCompose_StrongCoverageNoTestsCappedAtUsable(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:          "FM-good-no-test",
		Mitigations: 1,
		Tests:       0,
		Detectors:   1,
		Level:       assurance.CoverageWellCovered, // forced
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict != assurance.TrustUsable {
		t.Errorf("strong-no-tests verdict = %s, want usable", env.Verdict)
	}
	foundTestLimitation := false
	for _, l := range env.Limitations {
		if strings.Contains(l, "regression test") {
			foundTestLimitation = true
		}
	}
	if !foundTestLimitation {
		t.Errorf("expected limitations to mention missing regression test, got %v", env.Limitations)
	}
}

// TestCompose_PartialCoverageCappedAtLimited: a single-leg coverage match
// must not exceed TrustLimited.
func TestCompose_PartialCoverageCappedAtLimited(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:          "FM-partial",
		Mitigations: 1,
		Tests:       0,
		Detectors:   0,
		Level:       assurance.CoveragePartial,
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict != assurance.TrustLimited {
		t.Errorf("partial-1leg verdict = %s, want limited", env.Verdict)
	}
}

// TestCompose_OrphanFailureModeIsUnsafe: an orphan failure_mode (named in
// YAML but with zero enforcement) is the rubber-stamp risk; matched but the
// match is meaningless. Verdict must be unsafe.
func TestCompose_OrphanFailureModeIsUnsafe(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:    "FM-orphan",
		Level: assurance.CoverageOrphan,
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict != assurance.TrustUnsafe {
		t.Errorf("orphan-match verdict = %s, want unsafe", env.Verdict)
	}
}

// TestCompose_IntentionalGapIsLimitedNotUnsafe pins the lifecycle-aware
// rule: an INTENTIONAL_GAP failure_mode is a deliberately-accepted gap,
// not the orphan rubber-stamp risk. Verdict must be limited (proceed with
// caution), never unsafe. See docs/awareness/composed_path_failures.md
// (2026-05-10 intentional_gap conflated with orphan).
func TestCompose_IntentionalGapIsLimitedNotUnsafe(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:    "FM-intentional-gap",
		Level: assurance.CoverageTheoretical, // classifier short-circuits to theoretical
		State: "INTENTIONAL_GAP",
		Tests: 1, // architecture fix shipped, regression test guards it
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict == assurance.TrustUnsafe {
		t.Fatalf("intentional_gap verdict = unsafe; must distinguish accepted gap from orphan")
	}
	if env.Verdict != assurance.TrustLimited {
		t.Errorf("intentional_gap verdict = %s, want limited", env.Verdict)
	}
	if env.Coverage != assurance.TrustCoveragePartial {
		t.Errorf("intentional_gap coverage = %s, want partial", env.Coverage)
	}
}

// TestCompose_DeprecatedFailureModeStaysUnsafe: the lifecycle softening
// applies only to INTENTIONAL_GAP. DEPRECATED ids are placeholders the
// keyword matcher shouldn't trust at all — verdict must remain unsafe.
func TestCompose_DeprecatedFailureModeStaysUnsafe(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:    "FM-deprecated",
		Level: assurance.CoverageTheoretical,
		State: "DEPRECATED",
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: fmc,
		Staleness:      &assurance.Staleness{BundlePresent: true},
	})
	if env.Verdict != assurance.TrustUnsafe {
		t.Errorf("deprecated verdict = %s, want unsafe", env.Verdict)
	}
}

// TestCompose_InvariantMatchDoesNotLieAboutFailureMode pins the match-kind
// rule from docs/awareness/composed_path_failures.md (TrustEnvelope
// match-kind conflation). An invariant-only match must NOT produce the
// "orphan failure_mode" reason — failure_mode coverage language only
// applies when the match was actually a failure_mode.
func TestCompose_InvariantMatchDoesNotLieAboutFailureMode(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindInvariant,
		PerFailureMode:   nil, // no FM coverage — that's the original bug shape
		Staleness:        &assurance.Staleness{BundlePresent: true},
	})
	// Coverage axis: partial (we have an invariant, just no FM triangulation).
	if env.Coverage != assurance.TrustCoveragePartial {
		t.Errorf("coverage = %s, want partial for invariant-only match",
			env.Coverage)
	}
	// Verdict: limited (not unsafe).
	if env.Verdict != assurance.TrustLimited {
		t.Errorf("verdict = %s, want limited for invariant-only match", env.Verdict)
	}
	// Reason must NOT mention "failure_mode" when the match wasn't one.
	if strings.Contains(env.Reason, "failure_mode") {
		t.Errorf("reason talks about failure_mode for invariant match: %q", env.Reason)
	}
}

// TestCompose_RawYAMLMatchHonestReason: a raw-YAML-only match (graph empty
// but raw knowledge files matched) gets the raw-YAML reason text, not the
// orphan-failure-mode lie. Coverage stays partial (we have YAML guidance).
func TestCompose_RawYAMLMatchHonestReason(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindRawYAML,
		Staleness:        &assurance.Staleness{BundlePresent: true},
	})
	if env.Coverage != assurance.TrustCoveragePartial {
		t.Errorf("coverage = %s, want partial for raw_yaml match", env.Coverage)
	}
	if strings.Contains(env.Reason, "failure_mode") {
		t.Errorf("reason mentions failure_mode for raw_yaml match: %q", env.Reason)
	}
}

// TestCompose_FailureModeMatchKeepsExistingBehavior: when the match IS
// actually a failure_mode and there's no PerFailureMode supplied, the
// original "orphan failure_mode" reason still fires. The match-kind rule
// only refines non-FM cases.
func TestCompose_FailureModeMatchKeepsExistingBehavior(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindFailureMode,
		PerFailureMode:   nil,
		Staleness:        &assurance.Staleness{BundlePresent: true},
	})
	if env.Coverage != assurance.TrustCoverageNone {
		t.Errorf("coverage = %s, want none for FM match without coverage data", env.Coverage)
	}
	if env.Verdict != assurance.TrustUnsafe {
		t.Errorf("verdict = %s, want unsafe for orphan FM match", env.Verdict)
	}
	if !strings.Contains(env.Reason, "failure_mode") {
		t.Errorf("FM-match reason should mention failure_mode, got: %q", env.Reason)
	}
}

// TestCompose_StaleRepoIncludesBuildCommandHint pins the operator-facing
// hint introduced as the day-to-day usability fix: when freshness is
// stale_repo, RequiredActions must include the concrete rebuild command.
// This converts an honest gate (verdict held until rebuild) into an
// actionable signal (here is the command). Existing actions are preserved.
func TestCompose_StaleRepoIncludesBuildCommandHint(t *testing.T) {
	stale := &assurance.Staleness{
		Alarms: []assurance.Alarm{
			{ID: "yaml_newer_than_graph", Severity: assurance.AlarmWarn, Message: "test"},
		},
		BundlePresent: true,
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindFailureMode,
		PerFailureMode: &assurance.FailureModeCoverage{
			Level: assurance.CoverageWellCovered, Mitigations: 1, Tests: 1, Detectors: 1,
		},
		Staleness: stale,
	})
	if env.Freshness != assurance.FreshnessStaleRepo {
		t.Fatalf("freshness = %s, want stale_repo (precondition for this test)", env.Freshness)
	}
	found := false
	for _, a := range env.RequiredActions {
		if strings.Contains(a, "globular awareness build --clean") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RequiredActions to include 'globular awareness build --clean'; got %v", env.RequiredActions)
	}
}

// TestCompose_StalenessNilIsUnknown: when the caller can't supply staleness,
// freshness must be reported as unknown — never assumed fresh.
func TestCompose_StalenessNilIsUnknown(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:     true,
		PerFailureMode: &assurance.FailureModeCoverage{Level: assurance.CoverageWellCovered, Mitigations: 1, Tests: 1, Detectors: 1},
		Staleness:      nil,
	})
	if env.Freshness != assurance.FreshnessUnknown {
		t.Errorf("freshness with nil staleness = %s, want unknown", env.Freshness)
	}
	// Unknown freshness must be treated as stale → block trusted verdict.
	if env.Verdict == assurance.TrustTrusted {
		t.Errorf("verdict with unknown freshness must not be trusted, got %s", env.Verdict)
	}
}

// TestCompose_UnknownRoleYAMLsCapTrust pins the refined freshness rule:
// only YAMLs the system genuinely cannot classify (unknown role) cap trust
// at stale_unknown. Config-only YAMLs (incidents, proposals, knowledge/*)
// must NOT downgrade — they are explicitly known to not contribute to the
// graph. The previous version of this test pinned the buggy behavior where
// every config-only file capped the verdict, which trained agents to ignore
// the trust signal because it permanently said "stale" on healthy graphs.
func TestCompose_UnknownRoleYAMLsCapTrust(t *testing.T) {
	staleness := &assurance.Staleness{
		BundlePresent:        true,
		UntrackedYAMLCount:   30, // many config-only files
		ConfigYAMLCount:      30, // ALL of them are explicitly classified
		UnknownRoleYAMLCount: 0,  // nothing the system can't classify
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound: true,
		PerFailureMode: &assurance.FailureModeCoverage{
			Level:       assurance.CoverageWellCovered,
			Mitigations: 1,
			Tests:       1,
			Detectors:   1,
		},
		Staleness: staleness,
	})
	if env.Freshness != assurance.FreshnessFresh {
		t.Fatalf("freshness=%s want fresh: 30 config-only files must NOT cap trust", env.Freshness)
	}
	if env.Verdict != assurance.TrustTrusted {
		t.Fatalf("verdict=%s want trusted: refined rule should produce trusted on full coverage + classified files", env.Verdict)
	}

	// Now flip: ONE unknown-role file must downgrade the verdict.
	staleness.UnknownRoleYAMLCount = 1
	env = assurance.Compose(assurance.ComposeInputs{
		MatchFound: true,
		PerFailureMode: &assurance.FailureModeCoverage{
			Level:       assurance.CoverageWellCovered,
			Mitigations: 1, Tests: 1, Detectors: 1,
		},
		Staleness: staleness,
	})
	if env.Freshness != assurance.FreshnessStaleUnknown {
		t.Fatalf("freshness=%s want stale_unknown when UnknownRoleYAMLCount=1", env.Freshness)
	}
	if env.Verdict != assurance.TrustStale {
		t.Fatalf("verdict=%s want stale when UnknownRoleYAMLCount=1", env.Verdict)
	}
}

// TestCompose_ClosureLoopSurfacesIncidentAndTest is the P1-4 acceptance test:
// when the matched failure_mode carries closure-loop provenance (a learned
// incident_id and a verifying test), the envelope must surface both so the
// agent reading "trusted" can see WHY the verdict is partly load-bearing.
func TestCompose_ClosureLoopSurfacesIncidentAndTest(t *testing.T) {
	fmc := &assurance.FailureModeCoverage{
		ID:                  "FM-closed-loop",
		Mitigations:         1,
		Tests:               2,
		Detectors:           1,
		Level:               assurance.CoverageWellCovered,
		LearnedFromIncident: "INC-2026-0007",
		FirstVerifyingTest:  "TestResumeRequiresReceipt",
	}
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindFailureMode,
		PerFailureMode:   fmc,
		Staleness:        &assurance.Staleness{BundlePresent: true},
	})
	if env.LearnedFromIncident != "INC-2026-0007" {
		t.Errorf("envelope.LearnedFromIncident = %q, want INC-2026-0007", env.LearnedFromIncident)
	}
	if env.RegressionTest != "TestResumeRequiresReceipt" {
		t.Errorf("envelope.RegressionTest = %q, want TestResumeRequiresReceipt", env.RegressionTest)
	}
	if env.Verdict != assurance.TrustTrusted {
		t.Errorf("verdict = %s, want trusted (closure-loop should not change verdict)", env.Verdict)
	}
}

// TestCompose_ClosureLoopEmptyWhenNoPerFailureMode pins the negative
// contract: invariant / forbidden_fix / raw_yaml matches do not carry
// per-mode coverage data and therefore have no closure-loop pointer. Fields
// must stay empty so the omitempty JSON tag drops them from output.
func TestCompose_ClosureLoopEmptyWhenNoPerFailureMode(t *testing.T) {
	env := assurance.Compose(assurance.ComposeInputs{
		MatchFound:       true,
		PrimaryMatchKind: assurance.MatchKindInvariant,
		PerFailureMode:   nil,
		Staleness:        &assurance.Staleness{BundlePresent: true},
	})
	if env.LearnedFromIncident != "" {
		t.Errorf("LearnedFromIncident = %q, want empty (no per-mode data)", env.LearnedFromIncident)
	}
	if env.RegressionTest != "" {
		t.Errorf("RegressionTest = %q, want empty (no per-mode data)", env.RegressionTest)
	}
}

// TestCompose_ClosureLoopPartialPopulation: when the failure_mode has a
// learned incident but no verifying test (or vice-versa), the populated
// field is surfaced and the empty one stays empty rather than emitting a
// placeholder. This protects against the "must always have both" rebuke
// agents might apply if the envelope shape implied a tightly-coupled pair.
func TestCompose_ClosureLoopPartialPopulation(t *testing.T) {
	t.Run("IncidentOnly", func(t *testing.T) {
		fmc := &assurance.FailureModeCoverage{
			ID:                  "FM-partial-1",
			Mitigations:         1,
			Tests:               0,
			Detectors:           1,
			Level:               assurance.CoveragePartial,
			LearnedFromIncident: "INC-2026-0042",
		}
		env := assurance.Compose(assurance.ComposeInputs{
			MatchFound:       true,
			PrimaryMatchKind: assurance.MatchKindFailureMode,
			PerFailureMode:   fmc,
			Staleness:        &assurance.Staleness{BundlePresent: true},
		})
		if env.LearnedFromIncident != "INC-2026-0042" {
			t.Errorf("LearnedFromIncident = %q, want INC-2026-0042", env.LearnedFromIncident)
		}
		if env.RegressionTest != "" {
			t.Errorf("RegressionTest = %q, want empty", env.RegressionTest)
		}
	})
	t.Run("TestOnly", func(t *testing.T) {
		fmc := &assurance.FailureModeCoverage{
			ID:                 "FM-partial-2",
			Mitigations:        1,
			Tests:              1,
			Detectors:          0,
			Level:              assurance.CoveragePartial,
			FirstVerifyingTest: "TestSomeInvariant",
		}
		env := assurance.Compose(assurance.ComposeInputs{
			MatchFound:       true,
			PrimaryMatchKind: assurance.MatchKindFailureMode,
			PerFailureMode:   fmc,
			Staleness:        &assurance.Staleness{BundlePresent: true},
		})
		if env.LearnedFromIncident != "" {
			t.Errorf("LearnedFromIncident = %q, want empty", env.LearnedFromIncident)
		}
		if env.RegressionTest != "TestSomeInvariant" {
			t.Errorf("RegressionTest = %q, want TestSomeInvariant", env.RegressionTest)
		}
	})
}

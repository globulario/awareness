package contextnav

// evidence_test.go — Phase 5 acceptance tests for the cross-cutting
// evidence sources (trust_envelope, fix_ledger, experience_store, metrics).
// Each test asserts a single appender's behavior in isolation, plus one
// integration test for the rawKnowledge-skip contract.

import (
	"testing"
)

// findEvidence returns the first EvidenceRef matching source, or nil if
// none.
func findEvidence(refs []EvidenceRef, source string) *EvidenceRef {
	for i := range refs {
		if refs[i].Source == source {
			return &refs[i]
		}
	}
	return nil
}

// TestEvidence_TrustEnvelopeAttachedWithVerdict pins that a non-empty
// TrustVerdict produces a trust_envelope EvidenceRef on every non-raw
// trace, with the right confidence per the verdict ladder.
func TestEvidence_TrustEnvelopeAttachedWithVerdict(t *testing.T) {
	cases := []struct {
		verdict  string
		wantConf float64
	}{
		{"trusted", 0.9},
		{"usable", 0.75},
		{"limited", 0.6},
		{"stale", 0.5},
		{"unknown", 0.5},
		{"unsafe", 0.4},
	}
	for _, c := range cases {
		t.Run(c.verdict, func(t *testing.T) {
			traces := Build(BuildInputs{
				FailureModes:        []string{"fm.x"},
				Confidence:          ConfidenceMedium,
				GraphFreshnessKnown: true,
				TrustVerdict:        c.verdict,
				TrustReason:         "test reason",
			})
			if len(traces) != 1 {
				t.Fatalf("expected 1 trace, got %d", len(traces))
			}
			ev := findEvidence(traces[0].MatchedBy, "trust_envelope")
			if ev == nil {
				t.Fatalf("trust_envelope evidence not attached; got %+v", traces[0].MatchedBy)
			}
			if ev.Confidence != c.wantConf {
				t.Errorf("verdict=%q confidence=%v, want %v", c.verdict, ev.Confidence, c.wantConf)
			}
			if ev.Reason != "test reason" {
				t.Errorf("expected TrustReason to flow into evidence.Reason; got %q", ev.Reason)
			}
		})
	}
}

// TestEvidence_TrustEnvelopeSkippedWhenAbsent pins that an empty
// TrustVerdict skips the appender (so traces don't carry an empty
// evidence entry).
func TestEvidence_TrustEnvelopeSkippedWhenAbsent(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes: []string{"fm.x"},
		Confidence:   ConfidenceMedium,
	})
	if findEvidence(traces[0].MatchedBy, "trust_envelope") != nil {
		t.Errorf("trust_envelope evidence should be absent when TrustVerdict==\"\"")
	}
}

// TestEvidence_FixLedgerAttribution pins the fix_ledger attribution: the
// EvidenceRef includes the first fix_case id and its status in Reason.
func TestEvidence_FixLedgerAttribution(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes: []string{"fm.x"},
		Confidence:   ConfidenceMedium,
		FixCases: []FixCaseRef{
			{ID: "fc.previous", Status: "partial"},
		},
		FixLedgerGaps: []string{"missing_regression_test"},
	})
	ev := findEvidence(traces[0].MatchedBy, "fix_ledger")
	if ev == nil {
		t.Fatalf("fix_ledger evidence not attached; got %+v", traces[0].MatchedBy)
	}
	if ev.NodeID != "fix_case:fc.previous" {
		t.Errorf("NodeID = %q, want fix_case:fc.previous", ev.NodeID)
	}
	// "partial" status must demote confidence below the all-complete case.
	if ev.Confidence > 0.65 {
		t.Errorf("partial fix confidence = %v, want <= 0.65", ev.Confidence)
	}
}

// TestEvidence_FixLedgerCompleteIsHighConfidence pins that a fix marked
// complete keeps confidence at the top of the fix-ledger band (0.75).
func TestEvidence_FixLedgerCompleteIsHighConfidence(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes: []string{"fm.x"},
		Confidence:   ConfidenceMedium,
		FixCases: []FixCaseRef{
			{ID: "fc.done", Status: "complete"},
		},
	})
	ev := findEvidence(traces[0].MatchedBy, "fix_ledger")
	if ev == nil {
		t.Fatalf("fix_ledger evidence not attached")
	}
	if ev.Confidence != 0.75 {
		t.Errorf("complete fix confidence = %v, want 0.75", ev.Confidence)
	}
}

// TestEvidence_ExperienceStoreFromTopHint pins the experience appender:
// only the highest-scoring hint shapes the evidence, and confidence
// caps at 0.7 even for a perfect score.
func TestEvidence_ExperienceStoreFromTopHint(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes: []string{"fm.x"},
		Confidence:   ConfidenceMedium,
		Experiences: []ExperienceRef{
			{ID: "exp.alpha", Hint: "watched a similar restart storm", Score: 1.0},
			{ID: "exp.beta", Hint: "lower scoring", Score: 0.3},
		},
	})
	ev := findEvidence(traces[0].MatchedBy, "experience_store")
	if ev == nil {
		t.Fatalf("experience_store evidence not attached")
	}
	if ev.NodeID != "experience:exp.alpha" {
		t.Errorf("NodeID = %q, want experience:exp.alpha (top score wins)", ev.NodeID)
	}
	if ev.Confidence > 0.7 {
		t.Errorf("confidence = %v, want capped at 0.7", ev.Confidence)
	}
}

// TestEvidence_MetricsEvidenceFresh pins the metrics appender at 0.85
// when the live overlay is fresh.
func TestEvidence_MetricsEvidenceFresh(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:      []string{"fm.x"},
		Confidence:        ConfidenceMedium,
		LiveOverlayStatus: "fresh",
		MetricWarnings:    []string{"workflow_restart_rate > 0.5"},
	})
	ev := findEvidence(traces[0].MatchedBy, "metrics")
	if ev == nil {
		t.Fatalf("metrics evidence not attached")
	}
	if ev.Confidence != 0.85 {
		t.Errorf("fresh metrics confidence = %v, want 0.85", ev.Confidence)
	}
	if ev.Freshness != "fresh" {
		t.Errorf("Freshness = %q, want fresh", ev.Freshness)
	}
}

// TestEvidence_MetricsEvidenceStaleCapsConfidence pins that a stale live
// overlay caps metrics evidence at 0.5 per the doc's stale-cap rule.
func TestEvidence_MetricsEvidenceStaleCapsConfidence(t *testing.T) {
	traces := Build(BuildInputs{
		FailureModes:      []string{"fm.x"},
		Confidence:        ConfidenceMedium,
		LiveOverlayStatus: "stale",
		MetricWarnings:    []string{"x"},
	})
	ev := findEvidence(traces[0].MatchedBy, "metrics")
	if ev == nil {
		t.Fatalf("metrics evidence not attached")
	}
	if ev.Confidence > 0.5 {
		t.Errorf("stale metrics confidence = %v, want <= 0.5", ev.Confidence)
	}
}

// TestEvidence_RawKnowledgeTraceSkipsCrossCutting pins the rawKnowledge
// honesty contract: a raw-yaml-only trace does NOT receive trust /
// fix_ledger / experience / metrics evidence, so the agent reads it
// purely as a fallback hint.
func TestEvidence_RawKnowledgeTraceSkipsCrossCutting(t *testing.T) {
	traces := Build(BuildInputs{
		RawKnowledge: []RawKnowledgeRef{{
			Source: "failure_modes.yaml",
			Kind:   "failure_mode",
			ID:     "fm.fallback",
		}},
		Confidence:     ConfidenceMedium,
		TrustVerdict:   "trusted",
		FixCases:       []FixCaseRef{{ID: "fc.x", Status: "complete"}},
		Experiences:    []ExperienceRef{{ID: "exp.x", Score: 1.0}},
		MetricWarnings: []string{"x"},
	})
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	for _, src := range []string{"trust_envelope", "fix_ledger", "experience_store", "metrics"} {
		if findEvidence(traces[0].MatchedBy, src) != nil {
			t.Errorf("raw-knowledge trace must not carry %s evidence; got %+v",
				src, traces[0].MatchedBy)
		}
	}
}

// TestEvidence_RawYAMLConfidenceMatchesDocMatrix pins the doc's confidence
// matrix entry: raw YAML fallback EvidenceRef.Confidence = 0.65, NOT 0.5.
// The original Phase 2 implementation used 0.5 — Phase 5 tightened it.
func TestEvidence_RawYAMLConfidenceMatchesDocMatrix(t *testing.T) {
	traces := Build(BuildInputs{
		RawKnowledge: []RawKnowledgeRef{{
			Source: "failure_modes.yaml",
			Kind:   "failure_mode",
			ID:     "fm.fallback",
		}},
		Confidence: ConfidenceMedium,
	})
	ev := findEvidence(traces[0].MatchedBy, "raw_yaml")
	if ev == nil {
		t.Fatalf("raw_yaml evidence not attached")
	}
	if ev.Confidence != 0.65 {
		t.Errorf("raw_yaml confidence = %v, want 0.65 (doc matrix)", ev.Confidence)
	}
}

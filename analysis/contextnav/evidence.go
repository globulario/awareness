package contextnav

// evidence.go — Phase 5 of the context-navigation effort. Extends the
// MatchedBy chain on each DecisionTrace with cross-cutting evidence
// sources that don't anchor to a single graph edge: the trust envelope,
// the fix ledger, the experience store, and metric warnings.
//
// Why "cross-cutting": these signals describe the overall posture of the
// awareness output (trust verdict, prior fix coverage, similar past
// experiences, metric anomalies). They apply to every load-bearing
// finding, not just the one their graph edges point at. The agent reads
// them per-trace so the per-finding view stays self-contained — opening
// a single trace gives you the full evidence picture without separately
// consulting Report.Trust / Report.DidWeFix / Report.ExperienceHints.
//
// Sources covered:
//
//   - trust_envelope: per-trace summary of the overall TrustEnvelope
//     verdict + confidence + freshness. Confidence scoring follows the
//     doc table (stale/unknown verdicts cap at 0.5).
//   - fix_ledger: did_we_fix output (fix_cases + remaining_gaps). One
//     entry per non-raw trace when the ledger has matches.
//   - experience_store: the top-scoring ExperienceHint that's relevant
//     to the task — past attempts at similar work.
//   - metrics: Runtime.MetricWarnings — Prometheus warnings active when
//     the preflight ran.
//
// Raw-knowledge traces deliberately do NOT receive these enrichments:
// they're graph-misses by definition, so attaching strong cross-cutting
// signals would compete with the "this is a fallback" warning that ships
// in the trace itself.

import "fmt"

// FixCaseRef carries the minimum info needed for a fix_ledger EvidenceRef.
type FixCaseRef struct {
	ID     string
	Status string // complete | partial | etc.
}

// ExperienceRef carries the minimum info needed for an experience_store
// EvidenceRef.
type ExperienceRef struct {
	ID    string
	Hint  string
	Score float64
}

// trust verdict → base confidence mapping. Mirrors the per-evidence
// scoring matrix from the design doc: trusted/usable evidence above 0.7,
// stale/unknown capped at 0.5, unsafe demoted to 0.4.
func trustEvidenceConfidence(verdict string) float64 {
	switch verdict {
	case "trusted":
		return 0.9
	case "usable":
		return 0.75
	case "limited":
		return 0.6
	case "stale", "unknown":
		return 0.5
	case "unsafe":
		return 0.4
	}
	return 0.4
}

// appendTrustEvidence attaches a trust_envelope EvidenceRef to a trace
// when the inputs carry a verdict. The Reason field carries the trust
// envelope's own reason text when present, so the agent sees WHY the
// verdict is what it is without separately consulting Report.Trust.
func appendTrustEvidence(t *DecisionTrace, in *BuildInputs) {
	if in.TrustVerdict == "" {
		return
	}
	reason := fmt.Sprintf("overall trust verdict: %s", in.TrustVerdict)
	if in.TrustReason != "" {
		reason = in.TrustReason
	}
	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "trust_envelope",
		Confidence: trustEvidenceConfidence(in.TrustVerdict),
		Freshness:  in.TrustFreshness,
		Reason:     reason,
	})
}

// appendFixLedgerEvidence attaches a fix_ledger EvidenceRef when the
// did_we_fix lookup returned at least one fix_case. The Reason field
// summarises status across fix_cases so a partial/missing fix doesn't
// look like a clean resolution.
//
// Confidence: 0.75 when fix_cases all have status="complete"; 0.6 when
// any are partial; 0.5 when only remaining_gaps without fix_cases.
func appendFixLedgerEvidence(t *DecisionTrace, in *BuildInputs) {
	if len(in.FixCases) == 0 && len(in.FixLedgerGaps) == 0 {
		return
	}
	conf := 0.5
	if len(in.FixCases) > 0 {
		conf = 0.75
		for _, fc := range in.FixCases {
			if fc.Status != "" && fc.Status != "complete" && fc.Status != "fixed" {
				conf = 0.6
				break
			}
		}
	}
	reason := fmt.Sprintf("fix ledger: %d fix_case(s), %d remaining gap(s)", len(in.FixCases), len(in.FixLedgerGaps))
	nodeID := ""
	if len(in.FixCases) > 0 {
		nodeID = "fix_case:" + in.FixCases[0].ID
		if in.FixCases[0].Status != "" {
			reason = fmt.Sprintf("%s — first: %s (%s)", reason, in.FixCases[0].ID, in.FixCases[0].Status)
		}
	}
	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "fix_ledger",
		NodeID:     nodeID,
		Confidence: conf,
		Reason:     reason,
	})
}

// appendExperienceEvidence attaches an experience_store EvidenceRef from
// the top-scoring ExperienceHint when one exists. Confidence is derived
// from the hint's score (capped at 0.7 because experiences are
// reflective, not load-bearing graph proof).
func appendExperienceEvidence(t *DecisionTrace, in *BuildInputs) {
	if len(in.Experiences) == 0 {
		return
	}
	top := in.Experiences[0]
	conf := 0.5 + 0.2*top.Score
	if conf > 0.7 {
		conf = 0.7
	}
	if conf < 0.4 {
		conf = 0.4
	}
	reason := fmt.Sprintf("similar past experience: %s", top.Hint)
	if top.Hint == "" {
		reason = fmt.Sprintf("similar past experience: %s (score %.2f)", top.ID, top.Score)
	}
	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "experience_store",
		NodeID:     "experience:" + top.ID,
		Confidence: conf,
		Reason:     reason,
	})
}

// appendMetricsEvidence attaches a metrics EvidenceRef when at least one
// metric warning is active. Confidence is fixed at 0.85 (per the doc's
// runtime fresh evidence row) when the live overlay is fresh, capped at
// 0.5 when stale.
func appendMetricsEvidence(t *DecisionTrace, in *BuildInputs) {
	if len(in.MetricWarnings) == 0 {
		return
	}
	conf := 0.85
	if in.LiveOverlayStatus == "stale" {
		conf = 0.5
	} else if in.LiveOverlayStatus == "absent" || in.LiveOverlayStatus == "" {
		conf = 0.6
	}
	reason := fmt.Sprintf("metric warnings active: %d (first: %s)", len(in.MetricWarnings), in.MetricWarnings[0])
	t.MatchedBy = append(t.MatchedBy, EvidenceRef{
		Source:     "metrics",
		Confidence: conf,
		Freshness:  in.LiveOverlayStatus,
		Reason:     reason,
	})
}

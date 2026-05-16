package incidentpattern

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// Signal weight constants from spec.
const (
	weightFile        = 0.20
	weightSymbol      = 0.20
	weightComponent   = 0.15
	weightInvariant   = 0.25
	weightFailureMode = 0.25
	weightEditShape   = 0.35
	weightTaskText    = 0.10
	weightDiffPreview = 0.15
	weightRevertedFix = 0.30
	weightRepeated    = 0.20

	// Score thresholds
	thresholdHigh   = 0.80
	thresholdMedium = 0.55
	thresholdLow    = 0.30
)

// Match loads all active incident patterns and scores each against req.
// Returns matches above the low-confidence threshold, sorted by descending score.
func Match(ctx context.Context, g *graph.Graph, req IncidentMatchRequest) ([]IncidentPatternMatch, error) {
	st := NewStore(g)
	patterns, err := st.ListPatterns(ctx)
	if err != nil {
		return nil, fmt.Errorf("incidentpattern.Match: list patterns: %w", err)
	}

	ack := NewAcknowledgementStore(g)

	var results []IncidentPatternMatch
	for _, p := range patterns {
		score, signals := scorePattern(p, req)
		if score < thresholdLow {
			continue
		}
		conf := confidenceLabel(score)
		match := buildMatch(p, score, conf, signals)
		match.Block = shouldBlock(match, p, req.SessionID, ack, ctx)
		results = append(results, match)
	}

	// Sort descending by score.
	sortMatchesByScore(results)
	return results, nil
}

// scorePattern scores a single pattern against the request.
// Each signal type contributes at most its weight once (any overlap fires it).
func scorePattern(p IncidentPattern, req IncidentMatchRequest) (float64, []MatchedSignal) {
	var score float64
	var signals []MatchedSignal

	// File overlap
	for _, pf := range p.Files {
		for _, rf := range req.Files {
			if pathMatch(pf.Path, rf) {
				score += weightFile
				signals = append(signals, MatchedSignal{
					Kind:        "file",
					Value:       pf.Path,
					Weight:      weightFile,
					Explanation: fmt.Sprintf("File was part of %s (%s).", p.IncidentID, pf.Role),
				})
				goto doneFiles
			}
		}
	}
doneFiles:

	// Symbol overlap
	for _, ps := range p.Symbols {
		for _, rs := range req.Symbols {
			if strings.EqualFold(ps.Symbol, rs) {
				score += weightSymbol
				signals = append(signals, MatchedSignal{
					Kind:        "symbol",
					Value:       ps.Symbol,
					Weight:      weightSymbol,
					Explanation: fmt.Sprintf("Symbol %q was a %s in %s.", ps.Symbol, ps.Role, p.IncidentID),
				})
				goto doneSymbols
			}
		}
	}
doneSymbols:

	// Component overlap (pattern uses component names from files/summary)
	if len(req.Components) > 0 {
		for _, rc := range req.Components {
			if strings.Contains(strings.ToLower(p.Summary+" "+p.Title), strings.ToLower(rc)) {
				score += weightComponent
				signals = append(signals, MatchedSignal{
					Kind:        "component",
					Value:       rc,
					Weight:      weightComponent,
					Explanation: fmt.Sprintf("Component %q is mentioned in %s pattern.", rc, p.IncidentID),
				})
				break
			}
		}
	}

	// Invariant overlap
	for _, pi := range p.Invariants {
		for _, ri := range req.Invariants {
			if strings.EqualFold(pi.InvariantID, ri) {
				score += weightInvariant
				signals = append(signals, MatchedSignal{
					Kind:        "invariant",
					Value:       pi.InvariantID,
					Weight:      weightInvariant,
					Explanation: fmt.Sprintf("Invariant %q was %s in %s.", pi.InvariantID, pi.Relationship, p.IncidentID),
				})
				goto doneInvariants
			}
		}
	}
doneInvariants:

	// Failure mode overlap (against components and task text)
	if p.FailureMode != "" {
		haystack := strings.ToLower(req.Task + " " + strings.Join(req.Components, " "))
		if strings.Contains(haystack, strings.ToLower(p.FailureMode)) {
			score += weightFailureMode
			signals = append(signals, MatchedSignal{
				Kind:        "failure_mode",
				Value:       p.FailureMode,
				Weight:      weightFailureMode,
				Explanation: fmt.Sprintf("Task/components mention failure mode %q from %s.", p.FailureMode, p.IncidentID),
			})
		}
	}

	// Dangerous edit-shape overlap
	for _, es := range p.EditShapes {
		if !es.Dangerous {
			continue
		}
		for _, rs := range req.ProposedShape {
			if strings.EqualFold(es.ShapeKind, rs) {
				score += weightEditShape
				signals = append(signals, MatchedSignal{
					Kind:        "shape",
					Value:       es.ShapeKind,
					Weight:      weightEditShape,
					Explanation: fmt.Sprintf("Proposed shape %q matches a dangerous edit shape in %s: %s", es.ShapeKind, p.IncidentID, es.Description),
				})
				goto doneShapes
			}
		}
	}
doneShapes:

	// Task text similarity (keyword overlap with title/summary/root_cause)
	if req.Task != "" {
		patternText := strings.ToLower(p.Title + " " + p.Summary + " " + p.RootCause + " " + p.Lesson)
		taskWords := tokenize(req.Task)
		hitCount := 0
		for _, w := range taskWords {
			if len(w) > 3 && strings.Contains(patternText, w) {
				hitCount++
			}
		}
		if hitCount >= 2 {
			score += weightTaskText
			signals = append(signals, MatchedSignal{
				Kind:        "task_text",
				Value:       req.Task,
				Weight:      weightTaskText,
				Explanation: fmt.Sprintf("Task language overlaps with %s pattern description (%d keyword hits).", p.IncidentID, hitCount),
			})
		}
	}

	// Diff preview similarity
	if req.DiffPreview != "" {
		diffLower := strings.ToLower(req.DiffPreview)
		patternText := strings.ToLower(p.RootCause + " " + p.Lesson)
		words := tokenize(patternText)
		hitCount := 0
		for _, w := range words {
			if len(w) > 4 && strings.Contains(diffLower, w) {
				hitCount++
			}
		}
		if hitCount >= 2 {
			score += weightDiffPreview
			signals = append(signals, MatchedSignal{
				Kind:        "diff_preview",
				Value:       "diff",
				Weight:      weightDiffPreview,
				Explanation: fmt.Sprintf("Diff preview overlaps with %s root cause language.", p.IncidentID),
			})
		}
	}

	// Past reverted fix bonus
	for _, ff := range p.FailedFixes {
		if ff.Reverted {
			score += weightRevertedFix
			signals = append(signals, MatchedSignal{
				Kind:        "reverted_fix",
				Value:       ff.Description,
				Weight:      weightRevertedFix,
				Explanation: fmt.Sprintf("A previous fix was reverted: %s", ff.RevertReason),
			})
			break // one reverted fix is enough
		}
	}

	if score > 1.0 {
		score = 1.0
	}
	return score, signals
}

func buildMatch(p IncidentPattern, score float64, conf string, signals []MatchedSignal) IncidentPatternMatch {
	warning := fmt.Sprintf(
		"This edit pattern matches %s: %s. Score %.2f (%s confidence).",
		p.IncidentID, p.Title, score, conf)
	if hasRevertedFix(p) {
		warning += " A previous fix was reverted. Read the incident before editing."
	}

	recommended := []string{
		fmt.Sprintf("Read incident %s.", p.IncidentID),
		"Check ownership boundaries between affected components.",
	}
	if p.Lesson != "" {
		recommended = append(recommended, "Apply lesson: "+p.Lesson)
	}

	return IncidentPatternMatch{
		PatternID:       p.ID,
		IncidentID:      p.IncidentID,
		Title:           p.Title,
		Severity:        p.Severity,
		Score:           score,
		Confidence:      conf,
		MatchedSignals:  signals,
		FailedFixes:     p.FailedFixes,
		Lesson:          p.Lesson,
		Warning:         warning,
		RecommendedNext: recommended,
	}
}

// shouldBlock returns true when the match warrants blocking the edit.
// Blocking requires: critical severity + high confidence + at least one strong signal.
// File overlap alone NEVER blocks.
func shouldBlock(m IncidentPatternMatch, p IncidentPattern, sessionID string, ack *AcknowledgementStore, ctx context.Context) bool {
	if m.Severity != "critical" {
		return false
	}
	if m.Score < thresholdHigh {
		return false
	}
	if !hasStrongSignal(m) {
		return false
	}
	// If already acknowledged this session, don't re-block (unless shape/files changed,
	// which the caller detects by the signals still being present).
	if sessionID != "" && ack != nil {
		if ack.IsAcknowledgedInSession(ctx, sessionID, p.IncidentID) {
			return false
		}
	}
	return true
}

// hasStrongSignal returns true when at least one signal is a shape, invariant, or reverted fix.
func hasStrongSignal(m IncidentPatternMatch) bool {
	for _, s := range m.MatchedSignals {
		switch s.Kind {
		case "shape", "invariant", "reverted_fix":
			return true
		}
	}
	return false
}

func hasRevertedFix(p IncidentPattern) bool {
	for _, ff := range p.FailedFixes {
		if ff.Reverted {
			return true
		}
	}
	return false
}

func confidenceLabel(score float64) string {
	switch {
	case score >= thresholdHigh:
		return "high"
	case score >= thresholdMedium:
		return "medium"
	default:
		return "low"
	}
}

// pathMatch returns true if the request path contains the pattern path or vice versa.
func pathMatch(patternPath, requestPath string) bool {
	return strings.EqualFold(patternPath, requestPath) ||
		strings.HasSuffix(strings.ToLower(requestPath), strings.ToLower(patternPath)) ||
		strings.HasSuffix(strings.ToLower(patternPath), strings.ToLower(requestPath))
}

func tokenize(s string) []string {
	lower := strings.ToLower(s)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-' || r == '.' || r == '/' || r == '\n'
	})
}

func sortMatchesByScore(matches []IncidentPatternMatch) {
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].Score > matches[j-1].Score; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
}

// FormatAgentContextSection formats matched patterns as a Markdown section
// suitable for inclusion in agent-context output.
func FormatAgentContextSection(matches []IncidentPatternMatch) string {
	if len(matches) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Relevant Incident Warnings\n\n")
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("### %s: %s\n\n", m.IncidentID, m.Title))
		sb.WriteString(fmt.Sprintf("**Confidence**: %s (score %.2f)  \n", m.Confidence, m.Score))
		if len(m.MatchedSignals) > 0 {
			sb.WriteString("**Why surfaced**:\n")
			for _, sig := range m.MatchedSignals {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", sig.Kind, sig.Explanation))
			}
		}
		for _, ff := range m.FailedFixes {
			if ff.Reverted {
				sb.WriteString(fmt.Sprintf("\n**Past failed fix** (reverted): %s  \n", ff.Description))
				sb.WriteString(fmt.Sprintf("Reverted because: %s\n", ff.RevertReason))
			}
		}
		if m.Lesson != "" {
			sb.WriteString(fmt.Sprintf("\n**Lesson**: %s\n", m.Lesson))
		}
		if m.Block {
			sb.WriteString("\n> **STOP**: This is a blocking warning. Read the incident and acknowledge before editing.\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

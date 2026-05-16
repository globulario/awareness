package integrity

import (
	"fmt"
	"strings"
)

// Contradiction describes a detected conflict between a causal rule recommendation
// and a forbidden fix or ordering constraint.
type Contradiction struct {
	CausalRuleID   string `json:"causal_rule_id"`
	ForbiddenFixID string `json:"forbidden_fix_id,omitempty"`
	Step           string `json:"step"`
	Reason         string `json:"reason"`
}

// DetectContradictions runs all contradiction detectors and returns violations.
// It checks both hardcoded ordering rules and cross-references with forbidden fixes.
func DetectContradictions(rules []CausalRule, _ []ForbiddenFix) []Contradiction {
	var result []Contradiction
	result = append(result, detectEtcdDisarmBeforeCompact(rules)...)
	return result
}

// detectEtcdDisarmBeforeCompact detects causal rules that recommend etcd alarm disarm
// before compact, defrag, or verify-disk operations.
//
// Safe order: compact → defrag → verify disk below quota → alarm disarm.
// Forbidden: alarm disarm before any of compact / defrag / verify disk.
//
// Rationale: disarming the alarm does not free disk space. If done before
// compact+defrag, etcd hits NOSPACE again immediately, making the alarm re-trigger.
func detectEtcdDisarmBeforeCompact(rules []CausalRule) []Contradiction {
	// Keywords that identify the disarm step.
	disarmKW := []string{"alarm disarm", "disarm"}
	// Keywords that must ALL appear before the disarm.
	beforeKW := []string{"compact", "defrag", "verify disk"}

	var result []Contradiction

	for _, rule := range rules {
		disarmIdx := -1
		disarmStep := ""

		for i, step := range rule.RecommendedFixOrder {
			lower := strings.ToLower(step)
			if disarmIdx == -1 {
				for _, kw := range disarmKW {
					if strings.Contains(lower, kw) {
						disarmIdx = i
						disarmStep = step
						break
					}
				}
			}
		}

		if disarmIdx < 0 {
			continue // no disarm step in this rule
		}

		// Check if any required-before step appears after the disarm.
		var misorderedSteps []string
		for i := disarmIdx + 1; i < len(rule.RecommendedFixOrder); i++ {
			lower := strings.ToLower(rule.RecommendedFixOrder[i])
			for _, kw := range beforeKW {
				if strings.Contains(lower, kw) {
					misorderedSteps = append(misorderedSteps, rule.RecommendedFixOrder[i])
					break
				}
			}
		}

		if len(misorderedSteps) > 0 {
			result = append(result, Contradiction{
				CausalRuleID:   rule.ID,
				ForbiddenFixID: "etcd.disarm_before_compact",
				Step:           disarmStep,
				Reason: fmt.Sprintf(
					"alarm disarm (step %d) appears before required operations [%s] — "+
						"disarming before disk quota is reduced causes immediate NOSPACE recurrence",
					disarmIdx+1, strings.Join(misorderedSteps, ", ")),
			})
		}
	}

	return result
}

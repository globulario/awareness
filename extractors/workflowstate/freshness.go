package workflowstate

import (
	"time"
)

// FreshnessState describes how current a piece of live workflow evidence is.
type FreshnessState string

const (
	FreshnessFresh    FreshnessState = "fresh"
	FreshnessStale    FreshnessState = "stale"
	FreshnessExpired  FreshnessState = "expired"
	FreshnessAbsent   FreshnessState = "absent"
	FreshnessFailed   FreshnessState = "failed"
	FreshnessDisabled FreshnessState = "disabled"
)

// ConfidenceImpact describes how freshness affects decision confidence.
type ConfidenceImpact string

const (
	ConfidenceImpactNone    ConfidenceImpact = "none"
	ConfidenceImpactLowered ConfidenceImpact = "lowered"
	ConfidenceImpactBlocked ConfidenceImpact = "blocked" // expired — must not drive decisions
)

// staleThreshold is the fraction of TTL after which a node is considered stale.
// At 0.75 of TTL, the node is stale. At 1.0, it is expired.
const staleThreshold = 0.75

// FreshnessResult encapsulates the freshness state and its impact on confidence.
type FreshnessResult struct {
	State            FreshnessState
	ConfidenceImpact ConfidenceImpact
	// BlindSpot is non-empty when this freshness state should produce a blind spot entry.
	BlindSpot string
	// AgeSeconds is how old the evidence is (0 if unknown).
	AgeSeconds float64
}

// CheckFreshness evaluates TTL metadata from a live node and returns a FreshnessResult.
// metadata must be the Metadata map from a graph.Node.
func CheckFreshness(metadata map[string]any, now time.Time) FreshnessResult {
	expiresStr, hasExpires := metadata["expires_at"].(string)
	collectedStr, hasCollected := metadata["collected_at"].(string)

	if !hasExpires && !hasCollected {
		return FreshnessResult{
			State:            FreshnessAbsent,
			ConfidenceImpact: ConfidenceImpactLowered,
			BlindSpot:        "workflow_node_missing_freshness_metadata",
		}
	}

	var ttlSeconds float64
	if v, ok := metadata["ttl_seconds"].(int); ok {
		ttlSeconds = float64(v)
	} else if v, ok := metadata["ttl_seconds"].(float64); ok {
		ttlSeconds = v
	}

	var collectedAt time.Time
	if hasCollected {
		if t, err := time.Parse(time.RFC3339, collectedStr); err == nil {
			collectedAt = t
		}
	}

	var age float64
	if !collectedAt.IsZero() {
		age = now.Sub(collectedAt).Seconds()
	}

	if hasExpires {
		expAt, err := time.Parse(time.RFC3339, expiresStr)
		if err == nil && now.After(expAt) {
			return FreshnessResult{
				State:            FreshnessExpired,
				ConfidenceImpact: ConfidenceImpactBlocked,
				BlindSpot:        "workflow_runtime_expired",
				AgeSeconds:       age,
			}
		}
	}

	// Check for stale (past staleThreshold of TTL but not yet expired).
	if ttlSeconds > 0 && age > staleThreshold*ttlSeconds {
		return FreshnessResult{
			State:            FreshnessStale,
			ConfidenceImpact: ConfidenceImpactLowered,
			BlindSpot:        "workflow_runtime_stale",
			AgeSeconds:       age,
		}
	}

	return FreshnessResult{
		State:            FreshnessFresh,
		ConfidenceImpact: ConfidenceImpactNone,
		AgeSeconds:       age,
	}
}

// CanDriveDecision returns true iff the freshness state allows high-confidence decisions.
// Expired data must never drive active incident candidates or high-confidence decisions.
func (f FreshnessResult) CanDriveDecision() bool {
	return f.State == FreshnessFresh
}

// CanDriveMediumConfidence returns true for fresh or stale evidence.
func (f FreshnessResult) CanDriveMediumConfidence() bool {
	return f.State == FreshnessFresh || f.State == FreshnessStale
}

// effectiveConfidence downgrades a raw confidence string based on freshness.
func effectiveConfidence(raw string, f FreshnessResult) string {
	switch f.State {
	case FreshnessExpired:
		return "none"
	case FreshnessStale:
		switch raw {
		case "high":
			return "medium"
		case "medium":
			return "low"
		default:
			return "low"
		}
	default:
		return raw
	}
}

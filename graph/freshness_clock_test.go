package graph_test

import (
	"context"
	"testing"
	"time"
)

// TestFreshnessAt_DeterministicClock pins the clock-injection contract
// surfaced by docs/awareness/composed_path_failures.md (freshness clocks).
// The outer assurance.CheckStaleness exposes Options.Now but used to call
// graph.Freshness which read time.Now() internally — two clocks, one
// concept. FreshnessAt accepts an injected clock so the joined freshness
// check has a single "now" for both legs.
//
// If this test ever fails, freshness has regressed back to wall-clock
// dependence and tests will be flaky in CI.
func TestFreshnessAt_DeterministicClock(t *testing.T) {
	ctx := context.Background()
	g := openTestGraph(t)

	// Insert a graph_builds row at a fixed timestamp.
	builtAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, err := g.DB().ExecContext(ctx,
		`INSERT INTO graph_builds (id, repo_root, git_commit, release_id, created_at, stats_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"clock-test", "/r", "abc", "", builtAt.Unix(), `{}`,
	); err != nil {
		t.Fatalf("insert graph_builds: %v", err)
	}

	// "Now" is exactly 6 hours later. AgeSeconds must equal 6h, not whatever
	// the wall clock reports.
	now := builtAt.Add(6 * time.Hour)
	got := g.FreshnessAt(ctx, "", now)
	wantAge := (6 * time.Hour).Seconds()
	if got.AgeSeconds != wantAge {
		t.Errorf("AgeSeconds = %.0f, want %.0f (deterministic clock not threaded)",
			got.AgeSeconds, wantAge)
	}
	// 6h is below the 24h MaxAgeExceeded threshold → not stale on age alone.
	if got.MaxAgeExceeded {
		t.Errorf("MaxAgeExceeded = true at 6h, want false")
	}

	// Now advance the clock beyond 24h and confirm the threshold flips.
	stale := g.FreshnessAt(ctx, "", builtAt.Add(25*time.Hour))
	if !stale.MaxAgeExceeded {
		t.Errorf("MaxAgeExceeded = false at 25h, want true")
	}
}

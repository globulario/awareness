package graph

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GraphFreshness describes the freshness of the awareness graph.
type GraphFreshness struct {
	BuiltAt             time.Time
	BuiltAtUnix         int64
	AgeSeconds          float64
	KnowledgeMtime      time.Time
	KnowledgeSourceHash string // SHA256 of concatenated knowledge YAML content
	Stale               bool
	StaleReason         string
	RebuildRecommended  bool
	MaxAgeExceeded      bool
}

// LatestBuildTime queries graph_builds for the most recent created_at timestamp.
// Returns (zero time, false, nil) if no build records exist.
func (g *Graph) LatestBuildTime(ctx context.Context) (time.Time, bool, error) {
	// Use sql.NullInt64 to handle NULL when the table is empty.
	var ts nullInt64
	err := g.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM graph_builds`).Scan(&ts)
	if err != nil {
		return time.Time{}, false, err
	}
	if !ts.Valid || ts.Value == 0 {
		return time.Time{}, false, nil
	}
	return time.Unix(ts.Value, 0).UTC(), true, nil
}

// nullInt64 is a simple nullable int64 scanner.
type nullInt64 struct {
	Value int64
	Valid bool
}

func (n *nullInt64) Scan(src interface{}) error {
	if src == nil {
		n.Valid = false
		n.Value = 0
		return nil
	}
	switch v := src.(type) {
	case int64:
		n.Value = v
		n.Valid = true
	case float64:
		n.Value = int64(v)
		n.Valid = true
	default:
		n.Valid = false
	}
	return nil
}

// knowledgeFiles are the canonical YAML files that contribute graph nodes
// and edges to the awareness knowledge base.
//
// Every entry must correspond to a top-level YAML key that the manual loader
// (extractors/manual/load.go dispatchTable) recognises and turns into graph
// nodes/edges. Files that only configure subsystems (learning_rules,
// guardrails, audit_suppressions, etc.) are intentionally NOT in this list —
// they are picked up by assurance.CheckStaleness's UntrackedYAMLCount alarm
// instead, which tracks "visible but not graph-contributing" inputs.
//
// Adding a new entry here means: edits to that file should make the graph
// stale and trigger a rebuild prompt.
var knowledgeFiles = []string{
	"failure_modes.yaml", "invariants.yaml", "convergence_rules.yaml",
	"forbidden_fixes.yaml", "design_patterns.yaml", "patterns.yaml",
	"services.yaml",
	"detector_mapping.yaml",
}

// KnowledgeFiles returns a copy of the canonical list. Exposed for callers
// (e.g. assurance.CheckStaleness) that need to classify on-disk YAML as
// tracked vs visible-but-untracked.
func KnowledgeFiles() []string {
	out := make([]string, len(knowledgeFiles))
	copy(out, knowledgeFiles)
	return out
}

// Freshness computes graph freshness relative to the knowledge YAML files in docsDir.
// docsDir is the docs/awareness directory (e.g. /path/to/docs/awareness).
//
// Uses time.Now() as the clock. Tests and orchestrators that need a
// deterministic clock should call FreshnessAt directly. The two functions
// resolve to the same outcome when now == time.Now().
func (g *Graph) Freshness(ctx context.Context, docsDir string) GraphFreshness {
	return g.FreshnessAt(ctx, docsDir, time.Now())
}

// FreshnessAt is the clock-injectable form of Freshness. The age comparison
// is computed as now.Sub(builtAt) instead of time.Since(builtAt) so that
// freshness checks are reproducible across processes that share a clock.
//
// Consolidation 2026-05-10 (P1): the outer assurance.CheckStaleness exposed
// Options.Now but the graph leg ignored it, producing two clocks for one
// concept — see docs/awareness/composed_path_failures.md (freshness clocks).
// CheckStaleness now threads its clock through this function so a single
// "now" governs both legs of the freshness check.
func (g *Graph) FreshnessAt(ctx context.Context, docsDir string, now time.Time) GraphFreshness {
	if now.IsZero() {
		now = time.Now()
	}
	builtAt, ok, _ := g.LatestBuildTime(ctx)
	if !ok {
		return GraphFreshness{
			Stale:              true,
			StaleReason:        "no graph build record found — run 'globular awareness build'",
			RebuildRecommended: true,
		}
	}

	f := GraphFreshness{
		BuiltAt:     builtAt,
		BuiltAtUnix: builtAt.Unix(),
		AgeSeconds:  now.Sub(builtAt).Seconds(),
	}

	// Check max age (24h).
	if f.AgeSeconds > 24*3600 {
		f.MaxAgeExceeded = true
		f.Stale = true
		f.StaleReason = fmt.Sprintf("graph is %.1f hours old — run 'globular awareness build'", f.AgeSeconds/3600)
	}

	// Check knowledge file mtimes.
	if docsDir != "" {
		knowledgeMtime := latestMtime(docsDir, knowledgeFiles)
		f.KnowledgeMtime = knowledgeMtime

		if !knowledgeMtime.IsZero() && knowledgeMtime.After(builtAt) {
			f.Stale = true
			f.StaleReason = "knowledge YAML files modified after last graph build — run 'globular awareness build'"
		}

		// Compute hash of knowledge files.
		f.KnowledgeSourceHash = computeKnowledgeHash(docsDir)
	}

	f.RebuildRecommended = f.Stale
	return f
}

// computeKnowledgeHash returns a hex SHA256 of the concatenated content of all knowledge YAML files.
// Files that cannot be read are skipped silently.
func computeKnowledgeHash(docsDir string) string {
	h := sha256.New()
	for _, name := range knowledgeFiles {
		data, err := os.ReadFile(filepath.Join(docsDir, name))
		if err != nil {
			continue
		}
		// Include the filename as a separator so reordering files changes the hash.
		h.Write([]byte(name))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func latestMtime(dir string, files []string) time.Time {
	var latest time.Time
	for _, name := range files {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

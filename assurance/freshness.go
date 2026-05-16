package assurance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/awareness/bundlesync"
	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

// Default thresholds. These are intentionally generous so an idle developer
// laptop does not fire alarms; the thresholds exist to catch "the bundle has
// not been rebuilt in weeks" and "the YAML I just edited is not in the graph
// yet" classes of bug.
const (
	// DefaultBundleMaxAge is the upper bound on bundle build age. Past this,
	// even a well-formed manifest is treated as stale.
	DefaultBundleMaxAge = 7 * 24 * time.Hour
	// DefaultBundleStaleAge is the soft threshold — warn but do not alarm.
	DefaultBundleStaleAge = 48 * time.Hour
	// DefaultGraphMaxAge is the upper bound on graph build age. graph/freshness
	// already enforces a 24h hard cap; this constant matches it for symmetry.
	DefaultGraphMaxAge = 24 * time.Hour
)

// AlarmSeverity ranks staleness signals so callers can decide whether to gate.
type AlarmSeverity string

const (
	AlarmInfo     AlarmSeverity = "info"
	AlarmWarn     AlarmSeverity = "warn"
	AlarmCritical AlarmSeverity = "critical"
)

// Alarm is a single staleness finding.
type Alarm struct {
	ID       string        `json:"id"`
	Severity AlarmSeverity `json:"severity"`
	Message  string        `json:"message"`
	Subject  string        `json:"subject,omitempty"` // file or component the alarm refers to
}

// SourceFile records the on-disk state of a single knowledge YAML.
type SourceFile struct {
	Path         string `json:"path"`
	RelPath      string `json:"rel_path"`
	ModTimeUnix  int64  `json:"mod_time_unix"`
	SHA256Prefix string `json:"sha256_prefix,omitempty"` // first 12 hex chars
	Tracked      bool   `json:"tracked"`                 // in graph.Freshness's canonical 6-file list
	// Role is the YAML's classification: graph (loads into graph), config
	// (loaded by some subsystem but not graph-contributing), or unknown
	// (system can't classify by top-level key — needs human input).
	// Empty string for files whose top-level key couldn't be parsed.
	Role string `json:"role,omitempty"`
}

// Staleness is the combined freshness report for graph + bundle + sources.
type Staleness struct {
	GeneratedAtUnix int64 `json:"generated_at_unix"`

	// Graph freshness from graph.Freshness.
	GraphBuiltAtUnix    int64   `json:"graph_built_at_unix,omitempty"`
	GraphAgeSeconds     float64 `json:"graph_age_seconds,omitempty"`
	GraphStale          bool    `json:"graph_stale"`
	GraphStaleReason    string  `json:"graph_stale_reason,omitempty"`
	KnowledgeSourceHash string  `json:"knowledge_source_hash,omitempty"`

	// Bundle freshness — present only when manifest is supplied.
	BundlePresent        bool    `json:"bundle_present"`
	BundleBuiltAtUnix    int64   `json:"bundle_built_at_unix,omitempty"`
	BundleAgeSeconds     float64 `json:"bundle_age_seconds,omitempty"`
	BundleVersion        string  `json:"bundle_version,omitempty"`
	BundleBuildID        string  `json:"bundle_build_id,omitempty"`
	BundleOlderThanGraph bool    `json:"bundle_older_than_graph"`

	// Source-file inventory: every YAML under docs/awareness, including ones
	// the canonical 6-file list ignores.
	Sources             []SourceFile `json:"sources,omitempty"`
	TrackedYAMLCount    int          `json:"tracked_yaml_count"`
	UntrackedYAMLCount  int          `json:"untracked_yaml_count"`
	// ConfigYAMLCount counts files whose top-level key is in the config-only
	// allowlist (e.g. fix_cases, guardrails, knowledge/*). These are
	// explicitly known and do NOT contribute to the graph or staleness.
	ConfigYAMLCount int `json:"config_yaml_count"`
	// UnknownRoleYAMLCount counts files the system genuinely can't classify
	// — top-level key is neither in the graph dispatcher nor the config
	// allowlist. THIS is the count that should cap trust at stale_unknown.
	UnknownRoleYAMLCount int      `json:"unknown_role_yaml_count"`
	VisibleButUntracked  []string `json:"visible_but_untracked,omitempty"` // rel paths
	NewerThanGraph       []string `json:"newer_than_graph,omitempty"`      // rel paths

	Alarms []Alarm `json:"alarms,omitempty"`
}

// Options for CheckStaleness.
type Options struct {
	// DocsDir is repo_root/docs/awareness — the YAML truth root.
	DocsDir string
	// Manifest is the active bundle manifest (nil if no bundle is installed).
	Manifest *bundlesync.Manifest
	// BundleMaxAge overrides DefaultBundleMaxAge when non-zero.
	BundleMaxAge time.Duration
	// BundleStaleAge overrides DefaultBundleStaleAge when non-zero.
	BundleStaleAge time.Duration
	// Now is injected for tests; defaults to time.Now().
	Now time.Time
}

// CheckStaleness inspects graph build state, bundle manifest age, and the full
// docs/awareness YAML inventory. It returns a Staleness report whose Alarms
// list is the actionable summary — empty list means everything looked recent.
func CheckStaleness(ctx context.Context, g *graph.Graph, opts Options) (*Staleness, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	bundleMaxAge := opts.BundleMaxAge
	if bundleMaxAge <= 0 {
		bundleMaxAge = DefaultBundleMaxAge
	}
	bundleStaleAge := opts.BundleStaleAge
	if bundleStaleAge <= 0 {
		bundleStaleAge = DefaultBundleStaleAge
	}

	report := &Staleness{
		GeneratedAtUnix: now.Unix(),
	}

	// --- Graph freshness leg --------------------------------------------------
	if g != nil {
		// Single clock for both legs of the joined freshness check —
		// see docs/awareness/composed_path_failures.md (freshness clocks).
		gf := g.FreshnessAt(ctx, opts.DocsDir, now)
		report.GraphBuiltAtUnix = gf.BuiltAtUnix
		report.GraphAgeSeconds = gf.AgeSeconds
		report.GraphStale = gf.Stale
		report.GraphStaleReason = gf.StaleReason
		report.KnowledgeSourceHash = gf.KnowledgeSourceHash

		if gf.Stale {
			sev := AlarmWarn
			if gf.MaxAgeExceeded {
				sev = AlarmCritical
			}
			report.Alarms = append(report.Alarms, Alarm{
				ID:       "graph_stale",
				Severity: sev,
				Message:  gf.StaleReason,
			})
		}
	}

	// --- Source-file inventory leg --------------------------------------------
	if opts.DocsDir != "" {
		sources, err := scanDocsDir(opts.DocsDir)
		if err == nil {
			report.Sources = sources
			for _, s := range sources {
				if s.Tracked {
					report.TrackedYAMLCount++
				} else {
					report.UntrackedYAMLCount++
					report.VisibleButUntracked = append(report.VisibleButUntracked, s.RelPath)
				}
				switch s.Role {
				case string(manual.YAMLRoleConfig):
					report.ConfigYAMLCount++
				case string(manual.YAMLRoleUnknown):
					report.UnknownRoleYAMLCount++
				}
				if report.GraphBuiltAtUnix > 0 && s.ModTimeUnix > report.GraphBuiltAtUnix {
					report.NewerThanGraph = append(report.NewerThanGraph, s.RelPath)
				}
			}
		}
		if len(report.NewerThanGraph) > 0 {
			sort.Strings(report.NewerThanGraph)
			report.Alarms = append(report.Alarms, Alarm{
				ID:       "yaml_newer_than_graph",
				Severity: AlarmWarn,
				Message: fmt.Sprintf("%d knowledge YAML(s) modified after the last graph build — run 'globular awareness build'",
					len(report.NewerThanGraph)),
			})
		}
		// Only alarm on YAMLs the system genuinely can't classify. Files
		// whose top-level key is in the config-only allowlist (incidents,
		// proposals, knowledge/*, etc.) are explicitly known to NOT contribute
		// to the graph and must not block trust.
		// Bug fix (2026-05-10): the previous rule fired on ALL non-canonical
		// files, which capped the trust verdict at "stale_unknown" even on a
		// freshly-built graph. That trained agents to ignore the trust signal.
		if report.UnknownRoleYAMLCount > 0 {
			sort.Strings(report.VisibleButUntracked)
			report.Alarms = append(report.Alarms, Alarm{
				ID:       "unknown_role_knowledge_files",
				Severity: AlarmWarn,
				Message: fmt.Sprintf("%d YAML file(s) under docs/awareness have an unrecognised top-level key — classify as graph-contributing or config-only before treating awareness as fully fresh",
					report.UnknownRoleYAMLCount),
			})
		} else if report.UntrackedYAMLCount > 0 {
			// All untracked files ARE classified (config-only); just inform.
			report.Alarms = append(report.Alarms, Alarm{
				ID:       "untracked_knowledge_files",
				Severity: AlarmInfo,
				Message: fmt.Sprintf("%d YAML file(s) under docs/awareness are config-only (not graph-contributing) — informational",
					report.UntrackedYAMLCount),
			})
		}
	}

	// --- Bundle freshness leg -------------------------------------------------
	if opts.Manifest != nil {
		report.BundlePresent = true
		report.BundleVersion = opts.Manifest.Version
		report.BundleBuildID = opts.Manifest.BuildID

		if t, ok := parseManifestTime(opts.Manifest.CreatedAt); ok {
			report.BundleBuiltAtUnix = t.Unix()
			report.BundleAgeSeconds = now.Sub(t).Seconds()
			age := now.Sub(t)

			switch {
			case age >= bundleMaxAge:
				report.Alarms = append(report.Alarms, Alarm{
					ID:       "bundle_age_exceeded",
					Severity: AlarmCritical,
					Message: fmt.Sprintf("bundle build is %.1f hours old (max %s) — release a new bundle or pin a fresh one",
						age.Hours(), bundleMaxAge),
				})
			case age >= bundleStaleAge:
				report.Alarms = append(report.Alarms, Alarm{
					ID:       "bundle_age_stale",
					Severity: AlarmWarn,
					Message: fmt.Sprintf("bundle build is %.1f hours old (warn at %s)",
						age.Hours(), bundleStaleAge),
				})
			}

			if report.GraphBuiltAtUnix > 0 && report.BundleBuiltAtUnix < report.GraphBuiltAtUnix {
				report.BundleOlderThanGraph = true
				report.Alarms = append(report.Alarms, Alarm{
					ID:       "bundle_older_than_graph",
					Severity: AlarmWarn,
					Message:  "the locally built graph is newer than the installed bundle — bundle ships stale knowledge",
				})
			}
		}
	} else {
		report.Alarms = append(report.Alarms, Alarm{
			ID:       "bundle_missing",
			Severity: AlarmWarn,
			Message:  "no awareness bundle manifest provided — staleness check covers graph only",
		})
	}

	return report, nil
}

// CriticalAlarms returns the subset of alarms with severity == critical.
func (s *Staleness) CriticalAlarms() []Alarm {
	var out []Alarm
	for _, a := range s.Alarms {
		if a.Severity == AlarmCritical {
			out = append(out, a)
		}
	}
	return out
}

// scanDocsDir walks a docs/awareness directory and returns every *.yaml /
// *.yml file with its mtime and a short content hash. It also marks which of
// those files are part of the canonical knowledge_files list that
// graph.Freshness honours, so callers can see at a glance how big the gap is.
func scanDocsDir(docsDir string) ([]SourceFile, error) {
	canonical := canonicalKnowledgeSet()

	var out []SourceFile
	walkErr := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Skip hidden directories and very large unrelated ones; we still
			// descend into failuregraph_seeds/, knowledge/, contracts/, etc.
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(docsDir, path)
		sf := SourceFile{
			Path:        path,
			RelPath:     rel,
			ModTimeUnix: info.ModTime().Unix(),
			Tracked:     canonical[d.Name()],
		}
		if data, err := os.ReadFile(path); err == nil {
			sum := sha256.Sum256(data)
			sf.SHA256Prefix = fmt.Sprintf("%x", sum)[:12]
			// Classify the file. P1-3: prefer an explicit
			// `awareness_role: graph|config|seed|none` declaration on the
			// file, then fall back to the top-key heuristic. This lets
			// authors declare a file's role without the heuristic having to
			// guess (and gracefully grandfathers every legacy file that
			// already classified correctly).
			role, _ := manual.ClassifyYAML(data)
			sf.Role = string(role)
		}
		out = append(out, sf)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// canonicalKnowledgeSet returns the YAMLs whose edits should mark the graph
// stale, sourced from graph.KnowledgeFiles() so the two packages cannot drift.
// Earlier versions duplicated the list here and silently fell behind when
// new graph-contributing YAMLs (services.yaml) were added.
func canonicalKnowledgeSet() map[string]bool {
	files := graph.KnowledgeFiles()
	out := make(map[string]bool, len(files))
	for _, f := range files {
		out[f] = true
	}
	return out
}

// parseManifestTime accepts either RFC3339 or unix-seconds-as-string formats
// because different builders have used both. Returns ok=false on empty input
// or unrecognised format — the caller treats absence as "no timing info."
func parseManifestTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	// Unix seconds fallback.
	var sec int64
	if _, err := fmt.Sscanf(s, "%d", &sec); err == nil && sec > 0 {
		return time.Unix(sec, 0).UTC(), true
	}
	return time.Time{}, false
}

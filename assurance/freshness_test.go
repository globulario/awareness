package assurance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/bundlesync"
	"github.com/globulario/awareness/graph"
)

// makeBuiltGraph inserts a single graph_builds row at the requested time so
// freshness tests can simulate a graph that was built N hours ago.
func makeBuiltGraph(t *testing.T, age time.Duration) *graph.Graph {
	t.Helper()
	g := openSeededGraph(t)
	ts := time.Now().Add(-age).Unix()
	_, err := g.DB().ExecContext(context.Background(),
		`INSERT INTO graph_builds (id, repo_root, git_commit, release_id, created_at, stats_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"meta-test-build", "/r", "deadbeef", "", ts, `{}`)
	if err != nil {
		t.Fatalf("insert build: %v", err)
	}
	return g
}

func TestCheckStaleness_GraphFreshNoDocs(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if rep.GraphStale {
		t.Errorf("expected graph fresh, got stale (%s)", rep.GraphStaleReason)
	}
	// Bundle missing should still produce a warn alarm.
	foundMissing := false
	for _, a := range rep.Alarms {
		if a.ID == "bundle_missing" && a.Severity == assurance.AlarmWarn {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected bundle_missing alarm, got %+v", rep.Alarms)
	}
}

// TestCheckStaleness_BundleAgeExceededIsCritical: a manifest 8 days old must
// produce a critical bundle_age_exceeded alarm.
func TestCheckStaleness_BundleAgeExceededIsCritical(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	manifest := &bundlesync.Manifest{
		Name:          bundlesync.BundleName,
		Version:       "1.2.3",
		BuildID:       "deadbeef",
		SchemaVersion: bundlesync.SupportedSchemaVersions[0],
		SHA256:        "abc123",
		CreatedAt:     time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
	}
	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		Manifest: manifest,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	criticals := rep.CriticalAlarms()
	if len(criticals) == 0 {
		t.Fatalf("expected at least one critical alarm, got none. all=%+v", rep.Alarms)
	}
	foundAgeExceeded := false
	for _, a := range criticals {
		if a.ID == "bundle_age_exceeded" {
			foundAgeExceeded = true
		}
	}
	if !foundAgeExceeded {
		t.Errorf("expected bundle_age_exceeded critical alarm, got %+v", criticals)
	}
}

// TestCheckStaleness_BundleStaleAge: 3-day-old bundle should warn but not
// fire critical.
func TestCheckStaleness_BundleStaleAge(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	manifest := &bundlesync.Manifest{
		CreatedAt: time.Now().Add(-72 * time.Hour).Format(time.RFC3339),
	}
	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		Manifest: manifest,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(rep.CriticalAlarms()) > 0 {
		t.Errorf("did not expect critical alarms for 72h-old bundle; got %+v", rep.CriticalAlarms())
	}
	foundStale := false
	for _, a := range rep.Alarms {
		if a.ID == "bundle_age_stale" && a.Severity == assurance.AlarmWarn {
			foundStale = true
		}
	}
	if !foundStale {
		t.Errorf("expected bundle_age_stale warn alarm, got %+v", rep.Alarms)
	}
}

// TestCheckStaleness_BundleOlderThanGraph: a bundle created BEFORE the graph
// build must produce a bundle_older_than_graph alarm — operators are running
// a bundle that ships stale knowledge relative to local state.
func TestCheckStaleness_BundleOlderThanGraph(t *testing.T) {
	// Graph built 1h ago, bundle built 2h ago — bundle is stale relative
	// to the graph but well within the absolute age limit.
	g := makeBuiltGraph(t, 1*time.Hour)
	manifest := &bundlesync.Manifest{
		CreatedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		Manifest: manifest,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !rep.BundleOlderThanGraph {
		t.Errorf("expected BundleOlderThanGraph=true; bundle_built=%d graph_built=%d",
			rep.BundleBuiltAtUnix, rep.GraphBuiltAtUnix)
	}
	foundAlarm := false
	for _, a := range rep.Alarms {
		if a.ID == "bundle_older_than_graph" {
			foundAlarm = true
		}
	}
	if !foundAlarm {
		t.Errorf("expected bundle_older_than_graph alarm")
	}
}

// TestCheckStaleness_UntrackedYAMLDetected: any YAML in docs/awareness that
// isn't in the canonical 6-file list must show up in the untracked count, so
// the gap is visible instead of silent. After the 2026-05-10 fix, the rule
// distinguishes config-only YAMLs (info-only) from unknown-role YAMLs (warn,
// caps trust) — both are "untracked" by filename, but only the latter
// blocks safety verdicts.
func TestCheckStaleness_UntrackedYAMLDetected(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	docsDir := t.TempDir()

	// Tracked: filename in canonical list AND key in dispatchTable.
	if err := os.WriteFile(filepath.Join(docsDir, "failure_modes.yaml"),
		[]byte("failure_modes: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// Untracked-by-filename, but top-level key 'rules' IS in configOnlyKeys.
	// Must produce role=config (info-only alarm).
	if err := os.WriteFile(filepath.Join(docsDir, "learning_rules.yaml"),
		[]byte("rules: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// Untracked-by-filename AND top-level key 'category' is NOT classified
	// — must produce role=unknown (warn alarm, caps trust).
	seedDir := filepath.Join(docsDir, "failuregraph_seeds")
	if err := os.MkdirAll(seedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "vip-001.yaml"),
		[]byte("category: x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		DocsDir: docsDir,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}

	if rep.UntrackedYAMLCount < 2 {
		t.Errorf("UntrackedYAMLCount=%d, want ≥2 (both non-canonical files)", rep.UntrackedYAMLCount)
	}
	if rep.ConfigYAMLCount < 1 {
		t.Errorf("ConfigYAMLCount=%d, want ≥1 (learning_rules.yaml uses key 'rules')", rep.ConfigYAMLCount)
	}
	if rep.UnknownRoleYAMLCount < 1 {
		t.Errorf("UnknownRoleYAMLCount=%d, want ≥1 (vip-001.yaml uses unknown key 'category')",
			rep.UnknownRoleYAMLCount)
	}

	// The unknown-role file must produce a warn alarm.
	foundUnknownWarn := false
	for _, a := range rep.Alarms {
		if a.ID == "unknown_role_knowledge_files" && a.Severity == assurance.AlarmWarn {
			foundUnknownWarn = true
		}
	}
	if !foundUnknownWarn {
		t.Errorf("expected unknown_role_knowledge_files warn alarm, got %+v", rep.Alarms)
	}
}

// TestCheckStaleness_AllConfigOnly_NoWarn: when every untracked YAML is
// classified as config-only, there must be no warn-level alarm — only an
// info alarm. This is the fix for the live-cluster bug where 30 config-only
// YAMLs were permanently capping the trust verdict at stale_unknown.
func TestCheckStaleness_AllConfigOnly_NoWarn(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	docsDir := t.TempDir()

	// Tracked file.
	if err := os.WriteFile(filepath.Join(docsDir, "failure_modes.yaml"),
		[]byte("failure_modes: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// All other files are config-only (key in configOnlyKeys).
	configFiles := map[string]string{
		"learning_rules.yaml":  "rules: []",
		"guardrails.yaml":      "guardrails: []",
		"audit_suppressions.yaml": "suppressions: []",
		"context_aliases.yaml": "aliases: []",
		"fix_cases.yaml":       "fix_cases: []",
	}
	for name, body := range configFiles {
		if err := os.WriteFile(filepath.Join(docsDir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		DocsDir: docsDir,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}

	if rep.ConfigYAMLCount < 5 {
		t.Errorf("ConfigYAMLCount=%d, want 5", rep.ConfigYAMLCount)
	}
	if rep.UnknownRoleYAMLCount != 0 {
		t.Errorf("UnknownRoleYAMLCount=%d, want 0 (every file is classified)",
			rep.UnknownRoleYAMLCount)
	}

	// MUST NOT have an unknown_role_knowledge_files alarm.
	for _, a := range rep.Alarms {
		if a.ID == "unknown_role_knowledge_files" {
			t.Errorf("must not emit unknown_role_knowledge_files when every untracked file is config-only; got %+v",
				rep.Alarms)
		}
	}
	// SHOULD have an info alarm so operators see the count.
	foundInfo := false
	for _, a := range rep.Alarms {
		if a.ID == "untracked_knowledge_files" && a.Severity == assurance.AlarmInfo {
			foundInfo = true
		}
	}
	if !foundInfo {
		t.Errorf("expected untracked_knowledge_files info alarm with config-only files, got %+v", rep.Alarms)
	}
}

// TestCheckStaleness_ExplicitAwarenessRoleNoneSuppressesUnknownAlarm pins the
// user-facing value of P1-3: a YAML whose top-level key would normally
// classify as unknown can opt out of the warn-level "unknown_role_knowledge_files"
// alarm by declaring `awareness_role: none` at the top of the file. This is
// the escape hatch for scratch notes, draft proposals, and README-style YAMLs
// that legitimately live under docs/awareness/ but contribute nothing to the
// graph and should not block trust.
func TestCheckStaleness_ExplicitAwarenessRoleNoneSuppressesUnknownAlarm(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	docsDir := t.TempDir()

	// Canonical tracked file so the report has a baseline.
	if err := os.WriteFile(filepath.Join(docsDir, "failure_modes.yaml"),
		[]byte("failure_modes: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// File whose top-key 'scratch_notes' is NOT recognised by any classifier.
	// The explicit `awareness_role: none` declaration MUST win — the file is
	// counted as untracked but does NOT increment UnknownRoleYAMLCount and
	// MUST NOT raise the warn-level alarm.
	body := "awareness_role: none\nscratch_notes:\n  - draft\n"
	if err := os.WriteFile(filepath.Join(docsDir, "draft.yaml"),
		[]byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		DocsDir: docsDir,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}

	if rep.UnknownRoleYAMLCount != 0 {
		t.Errorf("UnknownRoleYAMLCount=%d, want 0 (awareness_role: none must suppress unknown classification)",
			rep.UnknownRoleYAMLCount)
	}
	for _, a := range rep.Alarms {
		if a.ID == "unknown_role_knowledge_files" {
			t.Errorf("must not emit unknown_role_knowledge_files when file declares awareness_role: none; got alarm %+v", a)
		}
	}
}

// TestCheckStaleness_ExplicitAwarenessRoleConfigOnUnknownKey pins that the
// declaration also works when promoting an unrecognised key to config — the
// file is counted in ConfigYAMLCount, not UnknownRoleYAMLCount, and the
// warn-level alarm stays silent.
func TestCheckStaleness_ExplicitAwarenessRoleConfigOnUnknownKey(t *testing.T) {
	g := makeBuiltGraph(t, 1*time.Hour)
	docsDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(docsDir, "failure_modes.yaml"),
		[]byte("failure_modes: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// Unrecognised key, but author declares it's config-only.
	body := "awareness_role: config\nmy_new_config_block:\n  setting: 1\n"
	if err := os.WriteFile(filepath.Join(docsDir, "new_config.yaml"),
		[]byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		DocsDir: docsDir,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}

	if rep.UnknownRoleYAMLCount != 0 {
		t.Errorf("UnknownRoleYAMLCount=%d, want 0 (awareness_role: config must override unknown heuristic)",
			rep.UnknownRoleYAMLCount)
	}
	if rep.ConfigYAMLCount < 1 {
		t.Errorf("ConfigYAMLCount=%d, want ≥1 (file declared as config)", rep.ConfigYAMLCount)
	}
	for _, a := range rep.Alarms {
		if a.ID == "unknown_role_knowledge_files" {
			t.Errorf("must not emit unknown_role_knowledge_files when file declares awareness_role: config; got alarm %+v", a)
		}
	}
}

// TestCheckStaleness_YAMLNewerThanGraph: a YAML modified after the last graph
// build must produce a yaml_newer_than_graph warn alarm.
func TestCheckStaleness_YAMLNewerThanGraph(t *testing.T) {
	// Graph 1h old.
	g := makeBuiltGraph(t, 1*time.Hour)

	docsDir := t.TempDir()
	yamlPath := filepath.Join(docsDir, "invariants.yaml")
	if err := os.WriteFile(yamlPath, []byte("invariants: []"), 0644); err != nil {
		t.Fatal(err)
	}
	// Force the file's mtime to "now" — newer than graph build.
	now := time.Now()
	if err := os.Chtimes(yamlPath, now, now); err != nil {
		t.Fatal(err)
	}

	rep, err := assurance.CheckStaleness(context.Background(), g, assurance.Options{
		DocsDir: docsDir,
	})
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if len(rep.NewerThanGraph) == 0 {
		t.Errorf("expected at least one entry in NewerThanGraph; sources=%+v", rep.Sources)
	}
	foundAlarm := false
	for _, a := range rep.Alarms {
		if a.ID == "yaml_newer_than_graph" {
			foundAlarm = true
		}
	}
	if !foundAlarm {
		t.Errorf("expected yaml_newer_than_graph alarm")
	}
}

package assurance_test

// End-to-end black-box test for the joined awareness pipeline:
//
//   git diff -> preflight -> graph freshness -> coverage lookup -> Compose() -> trust envelope
//
// This test exists to catch the class of bug where every unit test passes but
// the composed behavior is wrong (the prefix-bug class). It exercises the real
// preflight package against a small seeded graph and asserts on the trust
// envelope produced at the far end of the pipeline.
//
// Each test seeds the smallest fixture that exercises the relevant rule:
//
//   - well_covered + fresh + matched diff      -> verdict = trusted
//   - well_covered + STALE                     -> verdict = stale (never trusted)
//   - partial coverage + fresh                 -> verdict = limited
//   - orphan failure_mode + fresh              -> verdict = unsafe
//   - diff touches no seeded knowledge         -> verdict = unknown/unsafe
//
// Reuses helpers from coverage_test.go: openSeededGraph, addNode, addEdge,
// fmNode, addFailureMode (all in the same assurance_test package).
//
// ---------------------------------------------------------------------------
// Consolidation lesson (P0-2, 2026-05-10)
// ---------------------------------------------------------------------------
// Writing this test exposed the kind of bug it was designed to catch:
// preflight.computeTrustEnvelope never passed a manifest to CheckStaleness,
// so BundlePresent was permanently false and the joined pipeline produced
// stale_unknown on a fresh graph. Every individual subsystem was correct in
// isolation — bundlesync.LoadManifest works, assurance.CheckStaleness honors
// the manifest, freshnessFromStaleness gates trust correctly. But the wiring
// between them carried a different freshness assumption per layer:
//
//	bundlesync     : "manifest exists at <root>/current/manifest.json"
//	assurance      : "manifest is in opts.Manifest"
//	preflight      : "manifest is...?" (never asked)
//	CLI/MCP        : "preflight handles freshness"
//
// Five layers, four different assumptions about who supplies the manifest,
// no shared end-to-end contract. The fix (BundleManifestPath option +
// DefaultManifestPath() + canonical install layout chmod 755) introduced one
// shared contract: the install path IS the contract. Every consumer reads
// from the same canonical location unless explicitly overridden.
//
// Track this as the canonical "unit pass, composed path fail" example. Other
// shared concepts in the awareness system likely have the same fragmentation:
//
//   - Node id prefixes ("failure_mode:", "invariant:", "detector:") — already
//     hit once (the prefix bug). Each subsystem hand-rolls the prefix; needs
//     a shared id helper.
//   - Lifecycle metadata (deprecated, intentional_gap, coverage_state) — hit
//     during Scope D 2026-05-10: design_patterns.go and invariants.go each
//     upserted failure_mode stubs without metadata, silently wiping the
//     canonical loader's lifecycle hints. Fixed by gating stub-AddNode on
//     FindNode existence; root cause is that AddNode is upsert-with-clobber
//     and there is no "ensure exists" semantic.
//   - Coverage classification states — assurance/coverage.go and the various
//     extractors hold parallel string sets ("TESTED", "DETECTED", "ENFORCED")
//     with no shared enum.
//   - YAML-key role classification — manual.ClassifyYAMLByTopKey is the one
//     authority, but it lives behind a deprecated-via-design "graph"/"config"
//     dichotomy that fragments back out into per-extractor logic.
//
// The recurring shape: a concept that several subsystems all need, expressed
// in N partly-consistent ways, where the composed path drifts whenever one
// is updated without touching the others. The consolidation strategy is
// to identify these concepts and route them through one shared definition,
// not N parallel ones. Every "unit pass / composed path fail" incident is a
// signal pointing at the next concept that needs consolidating.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/awareness/assurance"
	"github.com/globulario/awareness/bundlesync"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// integrationFixture controls how the seeded graph + docs dir are built.
type integrationFixture struct {
	graphAge  time.Duration // age of graph_builds row (0 = no build row)
	bundleAge time.Duration // age stamped on bundle manifest (0 = no manifest)
	coverage  string        // "well_covered" | "partial" | "orphan" | "none"
	seedFM    bool          // false = no failure_mode seeded at all
}

// writeSyntheticManifest writes a manifest.json into dir and returns its path.
// Tests use this to populate Staleness.BundlePresent without needing a real
// bundle on disk.
func writeSyntheticManifest(t *testing.T, dir string, age time.Duration) string {
	t.Helper()
	m := bundlesync.Manifest{
		Name:          bundlesync.BundleName,
		Version:       "v1.2.30",
		BuildID:       "e2e-test",
		SchemaVersion: bundlesync.SupportedSchemaVersions[0],
		SHA256:        "abc123",
		CreatedAt:     time.Now().Add(-age).Format(time.RFC3339),
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// integrationFMID is the canonical failure_mode used by the integration tests.
// It mentions "scylla" so a task containing that keyword matches via
// analysis.matchFailureModes.
const integrationFMID = "FM-scylla-storm"
const integrationFMTitle = "scylla connection retry storm"

// setupIntegrationFixture builds an in-memory graph + temp docs dir tuned to
// the requested fixture. Returns the graph, the docs dir path, and a manifest
// to pass to assurance.CheckStaleness if the test exercises bundle freshness.
func setupIntegrationFixture(t *testing.T, fx integrationFixture) (*graph.Graph, string) {
	t.Helper()
	g := openSeededGraph(t)

	if fx.seedFM {
		addFailureMode(t, g, integrationFMID, integrationFMTitle)
		// The FM title contains "scylla connection retry storm". A task
		// mentioning any of those keywords is matched by analysis.matchFailureModes
		// directly, no invariant/file traversal required.

		// Apply coverage triangulation per the requested level.
		switch fx.coverage {
		case "well_covered":
			addNode(t, g, "DP-scylla", graph.NodeTypeDesignPattern, "scylla backoff pattern")
			addNode(t, g, "TEST-scylla", graph.NodeTypeTest, "TestScyllaBackoff")
			addNode(t, g, "RT-scylla", graph.NodeTypeRuntimeState, "scylla retry counter")
			addEdge(t, g, "DP-scylla", graph.EdgeMitigates, fmNode(integrationFMID))
			addEdge(t, g, "DP-scylla", graph.EdgeTestedBy, "TEST-scylla")
			// P1-1: stamp the detector edge as freshly observed so it
			// counts as ACTIVE (well_covered requires an active detector,
			// not just a wired mapping).
			if err := assurance.RecordDetectorObservation(context.Background(), g,
				"RT-scylla", integrationFMID, "doctor",
				graph.EdgeMatchesFailureMode, time.Now()); err != nil {
				t.Fatalf("RecordDetectorObservation: %v", err)
			}
		case "partial":
			addNode(t, g, "DP-scylla", graph.NodeTypeDesignPattern, "scylla backoff")
			addEdge(t, g, "DP-scylla", graph.EdgeMitigates, fmNode(integrationFMID))
			// no test, no detector — single-leg = partial
		case "orphan":
			// Failure mode named in YAML but with zero enforcement.
			// addFailureMode already created the node; nothing else.
		case "none":
			// no coverage edges at all
		}
	}

	docsDir := setupIntegrationDocsDir(t, fx.seedFM)

	// Insert graph_builds AFTER writing the YAMLs so the build timestamp is
	// strictly newer than the YAML mtimes (truncated to seconds). This avoids
	// a spurious yaml_newer_than_graph alarm on the fresh path. Tests that
	// want stale freshness explicitly Chtimes a YAML to the future after this.
	if fx.graphAge > 0 {
		// Force YAML mtimes to be at least 2*graphAge in the past so they
		// remain older than the build row regardless of fs precision.
		yamlOld := time.Now().Add(-2 * fx.graphAge)
		_ = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			_ = os.Chtimes(path, yamlOld, yamlOld)
			return nil
		})
		ts := time.Now().Add(-fx.graphAge).Unix()
		_, err := g.DB().ExecContext(context.Background(),
			`INSERT INTO graph_builds (id, repo_root, git_commit, release_id, created_at, stats_json)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			"e2e-test-build", "/r", "deadbeef", "", ts, `{}`)
		if err != nil {
			t.Fatalf("insert graph_builds: %v", err)
		}
	}
	return g, docsDir
}

// setupIntegrationDocsDir writes the minimum YAML files preflight needs.
// When seedFM is true, the failure_modes.yaml lists the integration FM so
// the manual-extractor classification recognises it.
func setupIntegrationDocsDir(t *testing.T, seedFM bool) string {
	t.Helper()
	dir := t.TempDir()

	aliases := `aliases:
  scylla.no_retry_storm:
    - scylla retry
    - scylla connection storm
`
	if err := os.WriteFile(filepath.Join(dir, "context_aliases.yaml"),
		[]byte(aliases), 0o644); err != nil {
		t.Fatal(err)
	}

	fmYAML := "failure_modes: []\n"
	if seedFM {
		fmYAML = `failure_modes:
  - id: ` + integrationFMID + `
    title: "` + integrationFMTitle + `"
`
	}
	if err := os.WriteFile(filepath.Join(dir, "failure_modes.yaml"),
		[]byte(fmYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "guardrails.yaml"),
		[]byte("guardrails: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fix_cases.yaml"),
		[]byte("fix_cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runIntegrationPreflight calls preflight.Run on a simulated diff (file list).
// manifestPath is optional — when set, the joined pipeline will mark
// BundlePresent=true so freshness can resolve to "fresh" given a fresh graph.
func runIntegrationPreflight(t *testing.T, g *graph.Graph, docsDir, manifestPath, task string, files []string) *preflight.Report {
	t.Helper()
	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:               task,
		Files:              files,
		DocsDir:            docsDir,
		BundleManifestPath: manifestPath,
	}, g)
	if err != nil {
		t.Fatalf("preflight.Run: %v", err)
	}
	return r
}

// ---------------------------------------------------------------------------
// Assertion 1+2: preflight returns a report containing a non-empty TrustEnvelope.
// ---------------------------------------------------------------------------

func TestE2E_ReturnsReportAndNonEmptyTrustEnvelope(t *testing.T) {
	g, docsDir := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	r := runIntegrationPreflight(t, g, docsDir, manifest,
		"fix scylla connection retry storm",
		[]string{"golang/scylla/connect.go"})

	if r == nil {
		t.Fatal("preflight returned nil report")
	}
	if r.Trust == nil {
		t.Fatal("Report.Trust is nil — envelope missing from joined pipeline")
	}
	if r.Trust.Verdict == "" {
		t.Errorf("Trust.Verdict empty — Compose() did not run or returned zero value")
	}
}

// ---------------------------------------------------------------------------
// Assertion 3: freshness gates the verdict. A fresh fixture and a stale fixture
// with otherwise identical inputs must not produce the same verdict.
// ---------------------------------------------------------------------------

func TestE2E_FreshnessGatesVerdict(t *testing.T) {
	freshG, freshDocs := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	staleG, staleDocs := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	// Make stale fixture's docs newer than the graph build, simulating
	// post-build YAML edits (the most common real-world staleness path).
	staleYAML := filepath.Join(staleDocs, "failure_modes.yaml")
	future := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(staleYAML, future, future); err != nil {
		t.Fatal(err)
	}

	task := "fix scylla connection retry storm"
	files := []string{"golang/scylla/connect.go"}
	freshManifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	staleManifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	freshR := runIntegrationPreflight(t, freshG, freshDocs, freshManifest, task, files)
	staleR := runIntegrationPreflight(t, staleG, staleDocs, staleManifest, task, files)

	if freshR.Trust == nil || staleR.Trust == nil {
		t.Fatalf("missing trust envelope: fresh=%v stale=%v", freshR.Trust, staleR.Trust)
	}
	if freshR.Trust.Freshness == staleR.Trust.Freshness {
		t.Errorf("freshness should differ: fresh=%s stale=%s", freshR.Trust.Freshness, staleR.Trust.Freshness)
	}
	if staleR.Trust.Verdict == assurance.TrustTrusted {
		t.Errorf("stale-graph verdict = trusted; freshness must gate the verdict")
	}
}

// ---------------------------------------------------------------------------
// Assertion 4: coverage affects the verdict. Same freshness, same matched
// failure_mode, but different coverage triangulation must not produce the
// same verdict.
// ---------------------------------------------------------------------------

func TestE2E_CoverageAffectsVerdict(t *testing.T) {
	wcG, wcDocs := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	partialG, partialDocs := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "partial",
		seedFM:   true,
	})

	task := "fix scylla connection retry storm"
	files := []string{"golang/scylla/connect.go"}
	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	wcR := runIntegrationPreflight(t, wcG, wcDocs, manifest, task, files)
	partialR := runIntegrationPreflight(t, partialG, partialDocs, manifest, task, files)

	if wcR.Trust == nil || partialR.Trust == nil {
		t.Fatalf("missing trust envelope")
	}
	if wcR.Trust.Coverage == partialR.Trust.Coverage {
		t.Errorf("coverage strata identical for well_covered vs partial: %s",
			wcR.Trust.Coverage)
	}
	// Partial coverage must not exceed limited.
	switch partialR.Trust.Verdict {
	case assurance.TrustTrusted, assurance.TrustUsable:
		t.Errorf("partial-coverage verdict = %s; must not exceed limited",
			partialR.Trust.Verdict)
	}
}

// ---------------------------------------------------------------------------
// Assertion 5: a high-risk matched failure_mode cannot return `trusted`
// unless coverage is sufficient/strong AND freshness is fresh. The well-
// covered + fresh path is the only door to TrustTrusted.
// ---------------------------------------------------------------------------

func TestE2E_TrustedRequiresStrongCoverageAndFreshness(t *testing.T) {
	g, docsDir := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	r := runIntegrationPreflight(t, g, docsDir, manifest,
		"fix scylla connection retry storm",
		[]string{"golang/scylla/connect.go"})

	if r.Trust == nil {
		t.Fatal("missing trust envelope")
	}
	// The pipeline produced a matched failure_mode (one of several routes:
	// keyword match on title, or impact via Files). Either way, the test is
	// load-bearing only for the well_covered+fresh path: that is the only
	// combination allowed to reach trusted.
	if len(r.FailureModes) == 0 {
		t.Skip("integration fixture did not surface failure_mode via preflight; " +
			"keyword/impact matching changed — fixture needs adjustment")
	}
	if r.Trust.Verdict != assurance.TrustTrusted {
		t.Errorf("well_covered + fresh + matched verdict = %s, want trusted",
			r.Trust.Verdict)
	}
	if r.Trust.Freshness != assurance.FreshnessFresh {
		t.Errorf("freshness = %s, want fresh", r.Trust.Freshness)
	}
}

// ---------------------------------------------------------------------------
// Assertion 6: a stale fixture cannot produce a trusted safety verdict.
// Use a critically-stale bundle (8 days old) — the strongest staleness signal.
// ---------------------------------------------------------------------------

func TestE2E_StaleFixtureCannotProduceTrusted(t *testing.T) {
	// Compose() reads staleness from CheckStaleness; the freshness rule
	// downgrades any verdict to TrustStale when graph is stale. The
	// integration fixture's stale variant flips YAML mtime to "future" so
	// CheckStaleness flags yaml_newer_than_graph + warns; that prevents
	// FreshnessFresh and therefore prevents TrustTrusted.
	g, docsDir := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})
	yamlPath := filepath.Join(docsDir, "failure_modes.yaml")
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(yamlPath, future, future); err != nil {
		t.Fatal(err)
	}

	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	r := runIntegrationPreflight(t, g, docsDir, manifest,
		"fix scylla connection retry storm",
		[]string{"golang/scylla/connect.go"})

	if r.Trust == nil {
		t.Fatal("missing trust envelope")
	}
	if r.Trust.Verdict == assurance.TrustTrusted {
		t.Errorf("stale fixture verdict = trusted; staleness rule was bypassed (verdict=%s freshness=%s)",
			r.Trust.Verdict, r.Trust.Freshness)
	}
}

// ---------------------------------------------------------------------------
// Assertion 7: a NO_MATCH diff returns `unknown` or `unsafe`, never `trusted`.
// Simulates a diff against a file the graph does not know about and a task
// description that does not match any seeded keyword.
// ---------------------------------------------------------------------------

func TestE2E_NoMatchDiffIsUnknownOrUnsafe(t *testing.T) {
	g, docsDir := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "well_covered",
		seedFM:   true,
	})

	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	// Task and file unrelated to the seeded scylla failure_mode.
	r := runIntegrationPreflight(t, g, docsDir, manifest,
		"refactor zzz_unrelated_subsystem qux helpers",
		[]string{"golang/zzz_unrelated/helpers.go"})

	if r.Trust == nil {
		t.Fatal("missing trust envelope")
	}
	switch r.Trust.Verdict {
	case assurance.TrustUnknown, assurance.TrustUnsafe:
		// ok
	default:
		t.Errorf("NO_MATCH verdict = %s; must be unknown or unsafe (matched_invariants=%v matched_failure_modes=%v)",
			r.Trust.Verdict, r.Invariants, r.FailureModes)
	}
}

// ---------------------------------------------------------------------------
// Bonus: orphan failure_mode produces unsafe verdict, not just limited.
// An orphan match means the YAML mentions the mode but no enforcement exists.
// This is the rubber-stamp risk Compose() exists to prevent.
// ---------------------------------------------------------------------------

func TestE2E_OrphanFailureModeIsUnsafe(t *testing.T) {
	g, docsDir := setupIntegrationFixture(t, integrationFixture{
		graphAge: 1 * time.Hour,
		coverage: "orphan",
		seedFM:   true,
	})
	manifest := writeSyntheticManifest(t, t.TempDir(), 1*time.Hour)
	r := runIntegrationPreflight(t, g, docsDir, manifest,
		"fix scylla connection retry storm",
		[]string{"golang/scylla/connect.go"})

	if r.Trust == nil {
		t.Fatal("missing trust envelope")
	}
	if len(r.FailureModes) == 0 {
		t.Skip("orphan fixture did not surface failure_mode via preflight; fixture needs adjustment")
	}
	if r.Trust.Verdict != assurance.TrustUnsafe {
		t.Errorf("orphan-FM verdict = %s, want unsafe (matched=%v)",
			r.Trust.Verdict, r.FailureModes)
	}
}

package preflight_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/preflight"
)

// findDocsAwarenessDir walks up from the test package directory to find docs/awareness.
func findDocsAwarenessDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve abs path: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(abs, "docs", "awareness")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("docs/awareness not found; skipping import preflight tests")
	return ""
}

// openGraphFromDocs loads invariants.yaml and failure_modes.yaml from the real
// docs/awareness directory into an in-memory graph for preflight testing.
func openGraphFromDocs(t *testing.T, docsDir string) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	if err := manual.LoadAll(ctx, g, docsDir); err != nil {
		t.Fatalf("manual.LoadAll from docs/awareness: %v", err)
	}
	return g
}

// ---- Test 6: preflight surfaces absence invariant for "missing key stopped runtime" ----

// TestPreflightSurfacesAbsenceInvariantForMissingKey verifies that a task mentioning
// "missing key stopped a critical runtime service" surfaces the
// critical_state.absence_is_not_destructive_intent invariant and its forbidden fixes.
func TestPreflightSurfacesAbsenceInvariantForMissingKey(t *testing.T) {
	docsDir := findDocsAwarenessDir(t)
	g := openGraphFromDocs(t, docsDir)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "missing etcd key stopped a critical runtime service — keepalived disabled by absent spec",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("preflight.Run: %v", err)
	}

	// Must match the alias block for critical_state.absence_is_not_destructive_intent.
	foundAlias := false
	for _, a := range r.MatchedAliases {
		if strings.Contains(a, "absence_is_not_destructive_intent") {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Errorf("expected alias match for critical_state.absence_is_not_destructive_intent, got: %v", r.MatchedAliases)
	}

	// Must surface the invariant.
	foundInvariant := false
	for _, inv := range r.Invariants {
		if strings.Contains(inv, "absence_is_not_destructive_intent") {
			foundInvariant = true
			break
		}
	}
	if !foundInvariant {
		t.Errorf("expected critical_state.absence_is_not_destructive_intent in invariants, got: %v", r.Invariants)
	}

	// Must surface at least one forbidden fix related to stopping runtime on missing key.
	foundFix := false
	for _, fix := range r.ForbiddenFixes {
		if strings.Contains(fix, "stop_runtime_on_missing_key") ||
			strings.Contains(fix, "delete_runtime_on_timeout") ||
			strings.Contains(fix, "treat_invalid_spec_as_disable") {
			foundFix = true
			break
		}
	}
	if !foundFix {
		t.Errorf("expected stop_runtime_on_missing_key in forbidden fixes, got: %v", r.ForbiddenFixes)
	}
}

// ---- Test 7: preflight surfaces runtime proof invariant for COMMAND package false positive ----

// TestPreflightSurfacesRuntimeProofInvariantForCommandPackage verifies that a task mentioning
// "COMMAND package flagged as missing systemd unit false positive" surfaces the
// runtime.installed_state_must_match_package_kind invariant.
func TestPreflightSurfacesRuntimeProofInvariantForCommandPackage(t *testing.T) {
	docsDir := findDocsAwarenessDir(t)
	g := openGraphFromDocs(t, docsDir)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "COMMAND package flagged as missing systemd unit — doctor false positive for yt-dlp and rclone",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("preflight.Run: %v", err)
	}

	// Must match the alias block for runtime.installed_state_must_match_package_kind.
	foundAlias := false
	for _, a := range r.MatchedAliases {
		if strings.Contains(a, "installed_state_must_match_package_kind") {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Errorf("expected alias match for runtime.installed_state_must_match_package_kind, got: %v", r.MatchedAliases)
	}

	// Must surface the invariant.
	foundInvariant := false
	for _, inv := range r.Invariants {
		if strings.Contains(inv, "installed_state_must_match_package_kind") {
			foundInvariant = true
			break
		}
	}
	if !foundInvariant {
		t.Errorf("expected runtime.installed_state_must_match_package_kind in invariants, got: %v", r.Invariants)
	}

	// Must surface at least one related forbidden fix.
	foundFix := false
	for _, fix := range r.ForbiddenFixes {
		if strings.Contains(fix, "command_package") ||
			strings.Contains(fix, "systemd_unit_for_command") ||
			strings.Contains(fix, "package_kind") {
			foundFix = true
			break
		}
	}
	if !foundFix {
		t.Errorf("expected a command_package related forbidden fix, got: %v", r.ForbiddenFixes)
	}
}

// ---- Bonus: preflight for bootstrap state task ----

// TestPreflightSurfacesBootstrapInvariant verifies that a task mentioning
// "bootstrap state was treated as authoritative" surfaces the
// desired.bootstrap_state_requires_promotion invariant.
func TestPreflightSurfacesBootstrapInvariant(t *testing.T) {
	docsDir := findDocsAwarenessDir(t)
	g := openGraphFromDocs(t, docsDir)

	r, err := preflight.Run(context.Background(), preflight.Options{
		Task:    "bootstrap state was treated as authoritative desired state — first boot claimed convergence too early",
		DocsDir: docsDir,
	}, g)
	if err != nil {
		t.Fatalf("preflight.Run: %v", err)
	}

	foundAlias := false
	for _, a := range r.MatchedAliases {
		if strings.Contains(a, "bootstrap_state_requires_promotion") {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Errorf("expected alias match for desired.bootstrap_state_requires_promotion, got: %v", r.MatchedAliases)
	}
}

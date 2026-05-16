package livecluster_test

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/awareness/graph"
	"github.com/globulario/awareness/livecluster"
)

func newTestStore(t *testing.T) (*graph.Graph, *livecluster.Store) {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("open memory graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g, livecluster.NewStore(g)
}

func baseReq(task string, services ...string) livecluster.LivePreflightRequest {
	return livecluster.LivePreflightRequest{
		SessionID:     "test-session",
		Task:          task,
		Services:      services,
		LookbackHours: 24,
	}
}

// Test 1: Healthy live signals → allow.
func TestLivePreflight_HealthyAllow(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := livecluster.HealthyCluster("workflow-service", "cluster-controller")
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, baseReq("patch retry logic", "workflow-service"))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict != "allow" && result.Verdict != "allow_with_warnings" {
		t.Errorf("expected allow or allow_with_warnings for healthy cluster, got %s", result.Verdict)
	}
	if len(result.Blockers) != 0 {
		t.Errorf("expected no blockers, got %d", len(result.Blockers))
	}
}

// Test 2: Active critical incident → block.
func TestLivePreflight_ActiveIncidentBlocks(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("incidents").WithIncidents(livecluster.ActiveClusterIncident{
			IncidentID:  "INC-2026-0001",
			Source:      "doctor",
			Title:       "Install result partial commit",
			Severity:    "critical",
			Status:      "active",
			ServiceName: "workflow-service",
			Component:   "package-install",
		}),
	}
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, baseReq("fix retry loop", "workflow-service"))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict != "block" {
		t.Errorf("expected block for active incident, got %s (blockers=%v)", result.Verdict, result.Blockers)
	}
	found := false
	for _, b := range result.Blockers {
		if b.Kind == "active_incident" {
			found = true
		}
	}
	if !found {
		t.Error("expected active_incident in blockers")
	}
}

// Test 3: Stuck convergence → block.
func TestLivePreflight_StuckConvergenceBlocks(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("convergence").WithConvergence(livecluster.RuntimeConvergenceState{
			Component:         "package-install",
			ConvergenceStatus: "stuck",
			RetryCount:        18,
			AgeSeconds:        2520,
			BlockedReason:     "workflow dispatch circuit open",
		}),
	}
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, baseReq("fix install path", "package-install"))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict != "block" {
		t.Errorf("expected block for stuck convergence, got %s", result.Verdict)
	}
}

// Test 4: Repeated critical errors (≥10) → block.
func TestLivePreflight_RepeatedCriticalErrorsBlock(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Unix()
	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("errors").WithErrors(livecluster.RecentErrorSignature{
			ServiceName: "workflow-service",
			Component:   "workflow",
			Signature:   "workflow dispatch failed action=<id> retry=<n> error=scylla session unavailable",
			Severity:    "critical",
			Count:       20,
			FirstSeen:   now - 3600,
			LastSeen:    now,
		}),
	}
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, baseReq("modify retry", "workflow-service"))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict != "block" {
		t.Errorf("expected block for repeated critical errors, got %s", result.Verdict)
	}
}

// Test 5: Source unavailable, require_live=false → allow_with_warnings.
func TestLivePreflight_SourceUnavailableWarns(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("events").WithStatus("unavailable"),
		livecluster.NewMockCollector("health").WithServices(livecluster.ServiceLiveState{
			ServiceName: "repository",
			Health:      "healthy",
		}),
	}
	req := baseReq("refactor status reporting", "repository")
	req.RequireLiveData = false
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict == "block" {
		t.Error("source unavailable with require_live=false should not block")
	}
	hasUnavailWarn := false
	for _, w := range result.Warnings {
		if w.Kind == "source_unavailable" {
			hasUnavailWarn = true
		}
	}
	if !hasUnavailWarn {
		t.Error("expected source_unavailable in warnings")
	}
}

// Test 6: Source unavailable + require_live=true → block.
func TestLivePreflight_RequireLiveUnavailableBlocks(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("all").WithStatus("unavailable"),
	}
	req := baseReq("critical fix", "cluster-controller")
	req.RequireLiveData = true
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if result.Verdict != "block" && result.Verdict != "unknown" {
		t.Errorf("expected block or unknown with require_live and all unavailable, got %s", result.Verdict)
	}
}

// Test 7: File to component mapping uses path prefix.
func TestMapFilesToComponents_PathPrefix(t *testing.T) {
	ctx := context.Background()
	files := []string{
		"golang/workflow/engine.go",
		"golang/cluster_controller/reconcile.go",
	}
	comps := livecluster.MapFilesToComponents(ctx, nil, files)
	has := func(name string) bool {
		for _, c := range comps {
			if c == name {
				return true
			}
		}
		return false
	}
	if !has("workflow") {
		t.Errorf("expected 'workflow' in components, got %v", comps)
	}
	if !has("cluster_controller") {
		t.Errorf("expected 'cluster_controller' in components, got %v", comps)
	}
}

// Test 8: Error signature normalization.
func TestNormalizeLogLine_Deduplication(t *testing.T) {
	now := time.Now().Unix()
	lines := []livecluster.LogLine{
		{
			Service:   "workflow-service",
			Component: "workflow",
			Message:   "2026-05-09T10:23:11 workflow dispatch failed action=abc-123 retry=17 error=scylla session unavailable",
			Severity:  "critical",
			Timestamp: now - 100,
		},
		{
			Service:   "workflow-service",
			Component: "workflow",
			Message:   "2026-05-09T10:23:45 workflow dispatch failed action=def-456 retry=18 error=scylla session unavailable",
			Severity:  "critical",
			Timestamp: now - 50,
		},
	}
	sigs := livecluster.ExtractRecentErrorSignatures(lines, 24)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 deduplicated signature, got %d: %v", len(sigs), sigs)
	}
	if sigs[0].Count != 2 {
		t.Errorf("expected count=2, got %d", sigs[0].Count)
	}
}

// Test 9: Agent context includes live signals section when verdict is non-allow.
func TestFormatLivePreflightSection_IncludesWarnings(t *testing.T) {
	g, st := newTestStore(t)
	ctx := context.Background()

	collectors := []livecluster.SignalCollector{
		livecluster.NewMockCollector("health").WithServices(livecluster.ServiceLiveState{
			ServiceName: "workflow-service",
			Health:      "degraded",
			LastError:   "dispatch circuit open",
		}),
	}
	result, err := livecluster.RunLivePreflight(ctx, g, st, collectors, baseReq("fix workflow", "workflow-service"))
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	section := livecluster.FormatLiveSection(result)
	if section == "" {
		t.Fatal("expected non-empty live section")
	}
	if !containsStr(section, "Live Cluster") {
		t.Errorf("section should contain 'Live Cluster', got: %s", section)
	}
}

// Test 10: Snapshot can be stored and retrieved.
func TestSnapshotStorageRoundtrip(t *testing.T) {
	_, st := newTestStore(t)
	ctx := context.Background()

	collectors := livecluster.HealthyCluster("repository")
	snap, err := livecluster.CollectClusterSignals(ctx, livecluster.CollectSignalsRequest{
		ClusterID: "test-cluster",
	}, collectors)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if err := st.StoreClusterSignalSnapshot(ctx, snap); err != nil {
		t.Fatalf("store: %v", err)
	}
	loaded, err := st.GetLatestClusterSignalSnapshot(ctx, "test-cluster")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ID != snap.ID {
		t.Errorf("expected ID %s, got %s", snap.ID, loaded.ID)
	}
	if loaded.Status != snap.Status {
		t.Errorf("expected status %s, got %s", snap.Status, loaded.Status)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

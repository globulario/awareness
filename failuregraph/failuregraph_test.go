package failuregraph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/awareness/failuregraph"
	"github.com/globulario/awareness/graph"
)

func openTestStore(t *testing.T) (*graph.Graph, *failuregraph.Store) {
	t.Helper()
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("open test graph: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g, failuregraph.New(g)
}

// Test 1: Match exact error signature.
func TestMatchExactSignature(t *testing.T) {
	g, s := openTestStore(t)
	_ = g
	ctx := context.Background()

	n, _ := seeded(t, ctx, s)
	_ = n

	exp, err := failuregraph.MatchError(ctx, s, failuregraph.MatchErrorRequest{
		RawError: "GetTXT unmarshal: unexpected end of JSON input",
	})
	if err != nil {
		t.Fatalf("MatchError: %v", err)
	}
	if exp == nil {
		t.Fatal("expected a match, got nil")
	}
	if exp.Category.Name != "empty_store_result_deserialization" {
		t.Errorf("expected empty_store_result_deserialization, got %s", exp.Category.Name)
	}
}

// Test 2: Normalize IP/SAN error → endpoint_identity_scope_violation.
func TestNormalizeSANError(t *testing.T) {
	raw := "x509: certificate is valid for globule-ryzen.globular.internal, not 10.0.0.100"
	normalized := failuregraph.NormalizeErrorSignature(raw)
	if strings.Contains(normalized, "10.0.0.100") {
		t.Errorf("IP not normalized: %s", normalized)
	}
	if strings.Contains(normalized, "globule-ryzen") {
		t.Errorf("hostname not normalized: %s", normalized)
	}
	if !strings.Contains(normalized, "<ip>") {
		t.Errorf("expected <ip> placeholder in normalized: %s", normalized)
	}
}

// Test 3: Return wrong fixes for endpoint_identity_scope_violation.
func TestWrongFixesReturned(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()
	seeded(t, ctx, s)

	exp, err := failuregraph.ExplainCategory(ctx, s, "ERRCAT-endpoint_identity_scope_violation")
	if err != nil {
		t.Fatalf("ExplainCategory: %v", err)
	}
	if len(exp.WrongFixes) == 0 {
		t.Fatal("expected wrong fixes, got none")
	}
	found := false
	for _, w := range exp.WrongFixes {
		if strings.Contains(strings.ToLower(w.Summary), "tls") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected wrong fix mentioning TLS verification, got: %+v", exp.WrongFixes)
	}
}

// Test 4: build_id missing routes to installed_state_build_id_missing.
func TestBuildIDMissingRoutesCorrectly(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()
	seeded(t, ctx, s)

	exp, err := failuregraph.MatchError(ctx, s, failuregraph.MatchErrorRequest{
		RawError:  "missing_package node-agent version=1.2.26 build_id=",
		Component: "cluster-controller",
	})
	if err != nil {
		t.Fatalf("MatchError: %v", err)
	}
	if exp == nil {
		t.Fatal("expected a match, got nil")
	}
	if exp.Category.Name != "installed_state_build_id_missing" {
		t.Errorf("expected installed_state_build_id_missing, got %s", exp.Category.Name)
	}
	// Recommended action must NOT say "relax drift scanner".
	if strings.Contains(strings.ToLower(exp.RecommendedAction), "relax") {
		t.Errorf("recommended action must not say 'relax': %s", exp.RecommendedAction)
	}
}

// Test 5: ExplainCategory returns nodes for all edge types.
func TestExplainCategoryFullGraph(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()
	seeded(t, ctx, s)

	exp, err := failuregraph.ExplainCategory(ctx, s, "ERRCAT-vip_used_as_member_endpoint")
	if err != nil {
		t.Fatalf("ExplainCategory: %v", err)
	}
	if len(exp.LikelyCauses) == 0 {
		t.Error("expected at least one likely cause")
	}
	if len(exp.Resolutions) == 0 {
		t.Error("expected at least one resolution")
	}
	if len(exp.WrongFixes) == 0 {
		t.Error("expected at least one wrong fix")
	}
	if len(exp.RequiredTests) == 0 {
		t.Error("expected at least one required test")
	}
}

// Test 6: FindSimilar returns relevant categories.
func TestFindSimilar(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()
	seeded(t, ctx, s)

	results, err := failuregraph.FindSimilar(ctx, s, failuregraph.SimilarFailureRequest{
		RawError:  "unexpected end of JSON input",
		Component: "workflow",
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one similar result")
	}
}

// Test 7: Normalization determinism — same raw error always produces same normalized form.
func TestNormalizationDeterminism(t *testing.T) {
	raw := "gocql: unable to create session: dial tcp 10.0.0.100:9042: attempt 57 failed 2026-05-09T06:01:21Z run_id=abc123"
	n1 := failuregraph.NormalizeErrorSignature(raw)
	n2 := failuregraph.NormalizeErrorSignature(raw)
	if n1 != n2 {
		t.Errorf("normalization is not deterministic: %q vs %q", n1, n2)
	}
	if strings.Contains(n1, "10.0.0.100") {
		t.Errorf("IP not normalized: %s", n1)
	}
	if strings.Contains(n1, "9042") {
		t.Errorf("port not normalized: %s", n1)
	}
}

// Test 8: LearnFromIncident creates graph nodes and edges.
func TestLearnFromIncident(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()

	nodes, edges, err := failuregraph.LearnFromIncident(ctx, s, "INC-2026-TEST", "test_category",
		[]string{"service crashed"},
		[]string{"nil pointer dereference"},
		[]string{"add nil check"},
		[]string{"do not suppress panic"},
		[]string{"nil input returns error not panic"},
	)
	if err != nil {
		t.Fatalf("LearnFromIncident: %v", err)
	}
	if nodes == 0 {
		t.Error("expected nodes to be created")
	}
	if edges == 0 {
		t.Error("expected edges to be created")
	}
}

// Test 9: SeedDefaults is idempotent.
func TestSeedDefaultsIdempotent(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()

	n1, err := failuregraph.SeedDefaults(ctx, s)
	if err != nil {
		t.Fatalf("SeedDefaults first call: %v", err)
	}
	n2, err := failuregraph.SeedDefaults(ctx, s)
	if err != nil {
		t.Fatalf("SeedDefaults second call: %v", err)
	}
	if n1 != n2 {
		t.Errorf("SeedDefaults not idempotent: first=%d second=%d", n1, n2)
	}
}

// Test 10: ListCategories returns all seeded categories.
func TestListCategories(t *testing.T) {
	_, s := openTestStore(t)
	ctx := context.Background()
	seeded(t, ctx, s)

	cats, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(cats) < 8 {
		t.Errorf("expected at least 8 seeded categories, got %d", len(cats))
	}
}

// seeded seeds defaults and returns the store.
func seeded(t *testing.T, ctx context.Context, s *failuregraph.Store) (int, *failuregraph.Store) {
	t.Helper()
	n, err := failuregraph.SeedDefaults(ctx, s)
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	return n, s
}

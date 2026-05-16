package awarectx_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	awarectx "github.com/globulario/awareness/context"
	"github.com/globulario/awareness/graph"
)

// openTestGraph opens a temp-file awareness graph pre-populated with test fixtures.
func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })

	ctx := context.Background()

	// Nodes.
	nodes := []graph.Node{
		{ID: "file:golang/workflow/server.go", Type: graph.NodeTypeSourceFile, Name: "server.go", Path: "golang/workflow/server.go", Summary: "Workflow server implementation"},
		{ID: "sym:Serve", Type: graph.NodeTypeSymbol, Name: "Serve", Path: "golang/workflow/server.go", Summary: "Main serve loop"},
		{ID: "pkg:workflow", Type: graph.NodeTypeGoPackage, Name: "workflow", Summary: "Workflow Go package"},
		{ID: "svc:workflow", Type: graph.NodeTypeGlobularService, Name: "workflow", Summary: "Globular workflow service"},
		{ID: "inv:service.endpoint.reachability", Type: graph.NodeTypeInvariant, Name: "service.endpoint.reachability", Summary: "Etcd address must be reachable"},
		{ID: "fm:service.endpoint.port_squatting", Type: graph.NodeTypeFailureMode, Name: "service.endpoint.port_squatting_cgroup_escape", Summary: "Cgroup-escaped orphan squats port"},
		{ID: "test:TestWorkflowOrphan", Type: graph.NodeTypeTest, Name: "TestWorkflowOrphan", Summary: "Verifies orphan is killed before restart"},
		{ID: "fix:relying_on_restart", Type: graph.NodeTypeForbiddenFix, Name: "relying_on_restart_without_pkill_guard", Summary: "Forbidden: restart without pkill guard"},
		{ID: "unit:globular-workflow.service", Type: graph.NodeTypeSystemdUnit, Name: "globular-workflow.service", Summary: "Workflow systemd unit"},
		{ID: "etcd:/globular/services/workflow/config", Type: graph.NodeTypeEtcdKey, Name: "/globular/services/workflow/config", Summary: "Workflow etcd config"},
	}
	for _, n := range nodes {
		if err := g.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode %s: %v", n.ID, err)
		}
	}

	// Invariant record.
	if err := g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "service.endpoint.reachability",
		Title:    "Service endpoint must be reachable",
		Summary:  "Etcd address must be probed before reconciliation trusts it",
		Severity: "critical",
		Status:   "active",
	}); err != nil {
		t.Fatalf("UpsertInvariant: %v", err)
	}

	// Failure mode record.
	if err := g.UpsertFailureMode(ctx, graph.FailureMode{
		ID:      "service.endpoint.port_squatting",
		Title:   "Port squatting via cgroup escape",
		Summary: "Orphan process escapes cgroup, squats original port",
	}); err != nil {
		t.Fatalf("UpsertFailureMode: %v", err)
	}

	// Edges.
	edges := []graph.Edge{
		{Src: "svc:workflow", Dst: "file:golang/workflow/server.go", Kind: graph.EdgeOwns, Confidence: 1.0},
		{Src: "file:golang/workflow/server.go", Dst: "sym:Serve", Kind: graph.EdgeDefines, Confidence: 1.0},
		{Src: "file:golang/workflow/server.go", Dst: "pkg:workflow", Kind: graph.EdgeImports, Confidence: 1.0},
		{Src: "inv:service.endpoint.reachability", Dst: "svc:workflow", Kind: graph.EdgeProtects, Confidence: 1.0, Required: true},
		{Src: "inv:service.endpoint.reachability", Dst: "fix:relying_on_restart", Kind: graph.EdgeForbids, Confidence: 1.0},
		{Src: "fm:service.endpoint.port_squatting", Dst: "svc:workflow", Kind: graph.EdgeAffects, Confidence: 1.0},
		{Src: "svc:workflow", Dst: "etcd:/globular/services/workflow/config", Kind: graph.EdgeReads, Confidence: 1.0},
		{Src: "svc:workflow", Dst: "test:TestWorkflowOrphan", Kind: graph.EdgeTestedBy, Confidence: 1.0},
		{Src: "unit:globular-workflow.service", Dst: "svc:workflow", Kind: graph.EdgeRunsAs, Confidence: 1.0},
	}
	for _, e := range edges {
		if err := g.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge %s->%s: %v", e.Src, e.Dst, err)
		}
	}

	return g
}

// --- ResolveNode tests ---

func TestResolveByExactID(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "svc:workflow")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match, got nil")
	}
	if r.Kind != awarectx.RefKindExact {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindExact)
	}
	if r.Exact.Name != "workflow" {
		t.Errorf("name = %q, want %q", r.Exact.Name, "workflow")
	}
}

func TestResolveByFilePath(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "golang/workflow/server.go")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via file path, got nil")
	}
	if r.Kind != awarectx.RefKindFile {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindFile)
	}
}

func TestResolveBySymbolName(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "Serve")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match by symbol name, got nil")
	}
	if r.Kind != awarectx.RefKindSymbol {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindSymbol)
	}
}

func TestResolveByServiceName(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "workflow")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatalf("expected exact match by service name, got nil (candidates: %d)", len(r.Candidates))
	}
	if r.Kind != awarectx.RefKindService {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindService)
	}
}

func TestResolveByInvariantID(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "service.endpoint.reachability")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact invariant match, got nil")
	}
	if r.Kind != awarectx.RefKindInvariant {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindInvariant)
	}
}

func TestResolveByNameLike(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "TestWorkflow")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	// Should find TestWorkflowOrphan via name-like search.
	found := r.Exact != nil || len(r.Candidates) > 0
	if !found {
		t.Error("expected at least one candidate for 'TestWorkflow', got none")
	}
}

func TestResolveEmptyRef(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact != nil {
		t.Error("expected nil exact for empty ref")
	}
}

// --- Build tests ---

func TestBuildNodeContext_ServiceNode(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nc, err := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if nc.NodeID != "svc:workflow" {
		t.Errorf("NodeID = %q", nc.NodeID)
	}
	if nc.NodeType != graph.NodeTypeGlobularService {
		t.Errorf("NodeType = %q", nc.NodeType)
	}
	// Invariant should be collected from incoming EdgeProtects.
	if len(nc.RelatedInvariants) == 0 {
		t.Error("expected at least one related invariant via EdgeProtects")
	}
	// Etcd read should be captured.
	if len(nc.StateReads) == 0 {
		t.Error("expected StateReads to include etcd config key")
	}
	// Required test.
	if len(nc.RequiredTests) == 0 {
		t.Error("expected RequiredTests to include TestWorkflowOrphan")
	}
	// Edit warnings should mention workflow invariant/service note.
	if len(nc.EditWarnings) == 0 {
		t.Error("expected EditWarnings for service node")
	}
}

func TestBuildNodeContext_InvariantNode(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nc, err := awarectx.Build(ctx, g, "inv:service.endpoint.reachability", awarectx.Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if nc.SourceLabel != awarectx.ConfidenceExplicit {
		t.Errorf("SourceLabel = %q, want explicit", nc.SourceLabel)
	}
	// Outgoing EdgeForbids → forbidden fix should be captured.
	if len(nc.ForbiddenFixes) == 0 {
		t.Error("expected ForbiddenFixes from EdgeForbids")
	}
}

func TestBuildNodeContext_NotFound(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	_, err := awarectx.Build(ctx, g, "nonexistent:node", awarectx.Options{})
	if err == nil {
		t.Error("expected error for non-existent node, got nil")
	}
}

// --- Neighborhood tests ---

func TestNeighborhood_Depth1(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nr, err := awarectx.Neighborhood(ctx, g, "svc:workflow", 1)
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}
	if nr.Center == nil || nr.Center.ID != "svc:workflow" {
		t.Error("center node mismatch")
	}
	if len(nr.Nodes) < 2 {
		t.Errorf("expected more than 1 node at depth 1, got %d", len(nr.Nodes))
	}
	// At depth 1 from svc:workflow we should see file, etcd key, test, invariant neighbor.
	if len(nr.Edges) == 0 {
		t.Error("expected edges in result")
	}
}

func TestNeighborhood_Depth2(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nr, err := awarectx.Neighborhood(ctx, g, "svc:workflow", 2)
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}
	// At depth 2 we should reach symbol via file.
	if len(nr.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes at depth 2, got %d", len(nr.Nodes))
	}
}

// --- ExplainNode tests ---

func TestExplainNode_Service(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	ex, err := awarectx.ExplainNode(ctx, g, "svc:workflow", awarectx.Options{})
	if err != nil {
		t.Fatalf("ExplainNode: %v", err)
	}
	if ex.NodeID != "svc:workflow" {
		t.Errorf("NodeID = %q", ex.NodeID)
	}
	if !strings.Contains(ex.Role, "workflow") {
		t.Errorf("Role does not mention 'workflow': %q", ex.Role)
	}
	// Risks should mention the failure mode.
	foundRisk := false
	for _, r := range ex.Risks {
		if strings.Contains(r, "failure mode") || strings.Contains(r, "port") || strings.Contains(r, "squatting") {
			foundRisk = true
		}
	}
	if !foundRisk {
		t.Errorf("expected failure mode in Risks, got: %v", ex.Risks)
	}
	if len(ex.Warnings) == 0 {
		t.Error("expected at least one warning for service node")
	}
}

// --- Format tests ---

func TestFormatNodeContext_Markdown(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nc, _ := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{})
	out := awarectx.FormatNodeContext(nc, "markdown")
	if !strings.Contains(out, "## Node Context") {
		t.Error("markdown output missing '## Node Context' header")
	}
	if !strings.Contains(out, "workflow") {
		t.Error("markdown output missing service name")
	}
}

func TestFormatNodeContext_JSON(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nc, _ := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{})
	out := awarectx.FormatNodeContext(nc, "json")
	if !strings.Contains(out, `"node_id"`) {
		t.Error("json output missing node_id field")
	}
}

func TestFormatNodeContext_Agent(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	nc, _ := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{})
	out := awarectx.FormatNodeContext(nc, "agent")
	if !strings.Contains(out, "node_id:") {
		t.Error("agent output missing 'node_id:' key")
	}
}

// --- Zoom-level tests ---

// openTestGraphWithDecision returns a graph that includes a design decision node
// linked to the workflow service via EdgeExplains.
func openTestGraphWithDecision(t *testing.T) *graph.Graph {
	t.Helper()
	g := openTestGraph(t)
	ctx := context.Background()

	dec := graph.Node{
		ID:      "decision:workflow_state_authority",
		Type:    graph.NodeTypeArchitectureDecision,
		Name:    "workflow_state_authority",
		Summary: "Workflow state must be authoritative via etcd",
	}
	if err := g.AddNode(ctx, dec); err != nil {
		t.Fatalf("AddNode decision: %v", err)
	}
	// Link decision → service (explains).
	if err := g.AddEdge(ctx, graph.Edge{
		Src:        dec.ID,
		Dst:        "svc:workflow",
		Kind:       graph.EdgeExplains,
		Confidence: 1.0,
	}); err != nil {
		t.Fatalf("AddEdge decision→service: %v", err)
	}
	return g
}

// TestZoomArchitecture_SurfacesDesignDecision verifies that ZoomArchitecture
// includes architecture_decision nodes in the context output.
func TestZoomArchitecture_SurfacesDesignDecision(t *testing.T) {
	g := openTestGraphWithDecision(t)
	ctx := context.Background()

	nc, err := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{
		Zoom:     awarectx.ZoomArchitecture,
		MaxItems: 20,
		Depth:    2,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(nc.DesignDecisions) == 0 {
		t.Error("ZoomArchitecture: expected at least one design decision, got none")
	}
	found := false
	for _, d := range nc.DesignDecisions {
		if strings.Contains(d, "workflow_state_authority") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ZoomArchitecture: 'workflow_state_authority' not in DesignDecisions: %v", nc.DesignDecisions)
	}
}

// TestZoomHistory_SurfacesFailureMode verifies that ZoomHistory includes
// failure modes in the context output.
func TestZoomHistory_SurfacesFailureMode(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	nc, err := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{
		Zoom:     awarectx.ZoomHistory,
		MaxItems: 20,
		Depth:    2,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(nc.RelatedFailureModes) == 0 {
		t.Error("ZoomHistory: expected at least one failure mode, got none")
	}
}

// TestZoomLocal_ExcludesDesignDecision verifies that ZoomLocal does NOT
// include architecture decision nodes (those belong to ZoomArchitecture).
func TestZoomLocal_ExcludesDesignDecision(t *testing.T) {
	g := openTestGraphWithDecision(t)
	ctx := context.Background()

	nc, err := awarectx.Build(ctx, g, "svc:workflow", awarectx.Options{
		Zoom:     awarectx.ZoomLocal,
		MaxItems: 20,
		Depth:    2,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(nc.DesignDecisions) > 0 {
		t.Errorf("ZoomLocal: expected no design decisions, got: %v", nc.DesignDecisions)
	}
}

// --- Symbol → invariant surfacing ---

// TestBuildNodeContext_SymbolSurfacesInvariant verifies that building context for
// an annotated symbol surfaces the invariant that protects its parent service.
func TestBuildNodeContext_SymbolSurfacesInvariant(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	// sym:Serve is defined in file:golang/workflow/server.go, which is owned by svc:workflow.
	// inv:service.endpoint.reachability protects svc:workflow.
	// At depth 2, the traversal from sym:Serve should reach svc:workflow and find the invariant.
	nc, err := awarectx.Build(ctx, g, "sym:Serve", awarectx.Options{
		Zoom:     awarectx.ZoomAll,
		MaxItems: 20,
		Depth:    3,
	})
	if err != nil {
		t.Fatalf("Build for symbol: %v", err)
	}
	if len(nc.RelatedInvariants) == 0 {
		t.Error("expected symbol's context to surface invariant via service link, got none")
	}
}

// --- Neighborhood depth 2 type coverage ---

// TestNeighborhood_Depth2_IncludesInvariantAndFailure verifies that at depth 2
// from the service, the neighborhood includes invariant and failure mode nodes.
func TestNeighborhood_Depth2_IncludesInvariantAndFailure(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	nr, err := awarectx.Neighborhood(ctx, g, "svc:workflow", 2)
	if err != nil {
		t.Fatalf("Neighborhood: %v", err)
	}

	if len(nr.Invariants) == 0 {
		t.Error("expected invariant nodes in depth-2 neighborhood of service, got none")
	}
	if len(nr.FailureModes) == 0 {
		t.Error("expected failure mode nodes in depth-2 neighborhood of service, got none")
	}
	if len(nr.Tests) == 0 {
		t.Error("expected test nodes in depth-2 neighborhood of service, got none")
	}
}

// --- Typed prefix resolution tests ---

// openTestGraphWithSemanticNodes returns a graph with nodes covering all typed
// prefix kinds used by the extended ResolveNode.
func openTestGraphWithSemanticNodes(t *testing.T) *graph.Graph {
	t.Helper()
	g := openTestGraph(t)
	ctx := context.Background()

	extras := []graph.Node{
		{ID: "stored:fix_case_node", Type: graph.NodeTypeFixCase, Name: "critical_state.absence_is_not_destructive_intent", Summary: "Fix case for absence-is-not-destructive"},
		{ID: "pattern:some_pattern", Type: graph.NodeTypePattern, Name: "runtime_observation_as_desired_authority", Summary: "Pattern: observation as authority"},
		{ID: "decision:desired_hash_is_convergence_identity", Type: graph.NodeTypeArchitectureDecision, Name: "desired_hash_is_convergence_identity", Summary: "Desired hash is convergence identity"},
		{ID: "stored:design_rule_node", Type: graph.NodeTypeDesignRule, Name: "no_raw_digest_in_desired_state", Summary: "Design rule: no raw digest"},
		{ID: "stored:guardrail_node", Type: graph.NodeTypeGuardrail, Name: "downgrade_guard", Summary: "Guardrail: no downgrade"},
		{ID: "stored:doc_section_node", Type: graph.NodeTypeDocumentationSection, Name: "convergence_identity_design", Summary: "Doc section: convergence identity"},
	}
	for _, n := range extras {
		if err := g.AddNode(ctx, n); err != nil {
			t.Fatalf("AddNode %s: %v", n.ID, err)
		}
	}
	return g
}

func TestResolveTypedPrefix_ForbiddenFix(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()
	// The test graph has a forbidden_fix node with name "relying_on_restart_without_pkill_guard".
	// Using typed prefix should find it even though its ID is "fix:relying_on_restart".
	r, err := awarectx.ResolveNode(ctx, g, "forbidden_fix:relying_on_restart_without_pkill_guard")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via forbidden_fix typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindForbiddenFix {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindForbiddenFix)
	}
}

func TestResolveTypedPrefix_FixCase(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "fix_case:critical_state.absence_is_not_destructive_intent")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via fix_case typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindFixCase {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindFixCase)
	}
}

func TestResolveTypedPrefix_Pattern(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	// "pattern:" prefix maps to NodeTypePattern.
	r, err := awarectx.ResolveNode(ctx, g, "pattern:runtime_observation_as_desired_authority")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via pattern typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindPattern {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindPattern)
	}
}

func TestResolveTypedPrefix_ArchitectureDecision(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	// The node's stored ID is "decision:desired_hash_is_convergence_identity".
	// Typed prefix "architecture_decision:" should find it via name lookup.
	r, err := awarectx.ResolveNode(ctx, g, "architecture_decision:desired_hash_is_convergence_identity")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via architecture_decision typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindArchDecision {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindArchDecision)
	}
	// Verify we resolved the correct node.
	if r.Exact.Type != graph.NodeTypeArchitectureDecision {
		t.Errorf("resolved node type = %q, want %q", r.Exact.Type, graph.NodeTypeArchitectureDecision)
	}
}

func TestResolveTypedPrefix_DesignRule(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "design_rule:no_raw_digest_in_desired_state")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via design_rule typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindDesignRule {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindDesignRule)
	}
}

func TestResolveTypedPrefix_Guardrail(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "guardrail:downgrade_guard")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via guardrail typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindGuardrail {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindGuardrail)
	}
}

func TestResolveTypedPrefix_DocumentationSection(t *testing.T) {
	g := openTestGraphWithSemanticNodes(t)
	ctx := context.Background()
	r, err := awarectx.ResolveNode(ctx, g, "documentation_section:convergence_identity_design")
	if err != nil {
		t.Fatalf("ResolveNode: %v", err)
	}
	if r.Exact == nil {
		t.Fatal("expected exact match via documentation_section typed prefix, got nil")
	}
	if r.Kind != awarectx.RefKindDocSection {
		t.Errorf("kind = %q, want %q", r.Kind, awarectx.RefKindDocSection)
	}
}

// --- ExplainNode agent format with forbidden fix warning ---

// TestExplainNode_AgentOutputIncludesForbiddenFix verifies that the agent-format
// explain output includes at least one "do not apply" warning derived from an
// invariant that protects the node.
func TestExplainNode_AgentOutputIncludesForbiddenFix(t *testing.T) {
	g := openTestGraph(t)
	ctx := context.Background()

	// inv:service.endpoint.reachability protects svc:workflow (EdgeProtects).
	// inv:service.endpoint.reachability forbids fix:relying_on_restart (EdgeForbids).
	// ExplainNode should surface this as a warning.
	ex, err := awarectx.ExplainNode(ctx, g, "svc:workflow", awarectx.Options{MaxItems: 20, Depth: 2})
	if err != nil {
		t.Fatalf("ExplainNode: %v", err)
	}

	foundForbidden := false
	for _, w := range ex.Warnings {
		if strings.Contains(w, "do not apply") {
			foundForbidden = true
			break
		}
	}
	if !foundForbidden {
		t.Errorf("expected 'do not apply' warning from forbidden fix, got warnings: %v", ex.Warnings)
	}

	// Verify the agent-format output also includes it.
	out := awarectx.FormatExplanation(ex, "agent")
	if !strings.Contains(out, "do not apply") {
		t.Errorf("agent format output missing forbidden fix warning:\n%s", out)
	}
}

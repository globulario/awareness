package analysis_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/analysis"
	"github.com/globulario/awareness/extractors/packages"
	"github.com/globulario/awareness/graph"
)

// ---- graph fixtures ----

// seedMainGraph populates a graph with invariants, protected keys, and forbidden fixes
// that the admission tests check against.
func seedMainGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := openGraph(t)
	ctx := context.Background()

	// Invariant: install.result.atomic_commit — protects the install state key.
	invNodeID := "invariant:install.result.atomic_commit"
	_ = g.AddNode(ctx, graph.Node{
		ID: invNodeID, Type: graph.NodeTypeInvariant,
		Name: "install.result.atomic_commit", Summary: "atomic commit required",
	})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID: "install.result.atomic_commit", Title: "Atomic commit", Severity: "critical",
	})
	// Protected etcd key.
	keyNodeID := "etcd_key:/globular/nodes/{node_id}/packages/{kind}/{name}"
	_ = g.AddNode(ctx, graph.Node{
		ID: keyNodeID, Type: graph.NodeTypeEtcdKey,
		Name: "/globular/nodes/{node_id}/packages/{kind}/{name}",
	})
	_ = g.AddEdge(ctx, graph.Edge{Src: invNodeID, Kind: graph.EdgeProtects, Dst: keyNodeID})

	// Invariant: repository.metadata_first — no protected keys in this seeded graph.
	_ = g.AddNode(ctx, graph.Node{
		ID: "invariant:repository.metadata_first", Type: graph.NodeTypeInvariant,
		Name: "repository.metadata_first",
	})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID: "repository.metadata_first", Title: "Metadata first", Severity: "high",
	})

	// Globally forbidden fix — registering it as a node.
	_ = g.AddNode(ctx, graph.Node{
		ID: "forbidden_fix:blind_retry_workflow", Type: graph.NodeTypeForbiddenFix,
		Name: "blind_retry_workflow",
	})

	// Existing services for cycle detection.
	for _, svc := range []string{"etcd", "scylladb", "minio", "repository", "node-agent"} {
		_ = g.AddNode(ctx, graph.Node{
			ID: "service:" + svc, Type: graph.NodeTypeGlobularService, Name: svc,
		})
	}

	// Existing depends_on edges that enable cycle detection in tests.
	// repository depends on minio (blob_read, required).
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:repository", Kind: graph.EdgeDependsOn,
		Dst: "service:minio", Phase: "blob_read", Required: true,
	})
	// minio depends on node-agent (bootstrap_recovery, optional).
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:minio", Kind: graph.EdgeDependsOn,
		Dst: "service:node-agent", Phase: "bootstrap_recovery", Required: false,
	})

	return g
}

// writeAwarenessYAML writes a YAML string to <dir>/awareness.yaml.
func writeAwarenessYAML(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "awareness.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writeAwarenessYAML: %v", err)
	}
	return dir
}

// ---- tests ----

// Test 1: Infrastructure package without awareness.yaml → BLOCK.
func TestAdmissionInfraWithoutContractBlocks(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	result, err := analysis.ValidatePackage(ctx, nil, "INFRASTRUCTURE", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionBlock {
		t.Errorf("expected BLOCK, got %s", result.Status)
	}
	if len(result.Reasons) == 0 {
		t.Error("expected at least one reason")
	}
}

// Test 2: Application package without awareness.yaml → WARN.
func TestAdmissionApplicationWithoutContractWarns(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	result, err := analysis.ValidatePackage(ctx, nil, "APPLICATION", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionWarn {
		t.Errorf("expected WARN, got %s", result.Status)
	}
}

// Test 3: Package with required recovery cycle → BLOCK.
func TestAdmissionRequiredRecoveryCycleBlocks(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	// node-agent is already seeded.
	// Propose: node-agent depends_on repository during recovery (required).
	// Existing: repository → minio (blob_read, required).
	// But we want a cycle: new-svc → repository → minio → new-svc (recovery, required).
	// Build: new-svc depends on repository (recovery, required).
	// repository depends on minio (blob_read, required) — already in graph.
	// minio depends on new-svc (recovery, required) — new edge from the contract.

	// Easier: just create the full required cycle via the contract.
	// new-infra depends on svc-a (recovery, required).
	// svc-a depends on new-infra (recovery, required) — already in graph.
	_ = g.AddNode(ctx, graph.Node{ID: "service:svc-cycle-a", Type: graph.NodeTypeGlobularService, Name: "svc-cycle-a"})
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:svc-cycle-a", Kind: graph.EdgeDependsOn,
		Dst: "service:new-infra", Phase: "recovery", Required: true,
	})

	contract := &packages.AwarenessContract{
		Package:     "new-infra",
		Service:     "new-infra",
		PackageKind: "INFRASTRUCTURE",
		DependsOn: []packages.ContractDependency{
			{Service: "svc-cycle-a", Phase: "recovery", Required: true},
		},
	}

	result, err := analysis.ValidatePackage(ctx, contract, "INFRASTRUCTURE", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionBlock {
		t.Errorf("expected BLOCK for required recovery cycle, got %s", result.Status)
		for _, r := range result.Reasons {
			t.Logf("  reason: [rule %d] %s: %s", r.Rule, r.Status, r.Message)
		}
	}

	var hasDangerousCycle bool
	for _, c := range result.DependencyCycles {
		if c.Classification == analysis.CycleDangerous {
			hasDangerousCycle = true
			break
		}
	}
	if !hasDangerousCycle {
		t.Error("expected DependencyCycles to contain a DANGEROUS cycle")
	}
}

// Test 4: Package with optional cycle → ADMIT or WARN (not BLOCK).
func TestAdmissionOptionalCycleNotBlocked(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	// Create an optional cycle: opt-a ↔ opt-b (both optional).
	_ = g.AddNode(ctx, graph.Node{ID: "service:opt-a", Type: graph.NodeTypeGlobularService, Name: "opt-a"})
	_ = g.AddNode(ctx, graph.Node{ID: "service:opt-b", Type: graph.NodeTypeGlobularService, Name: "opt-b"})
	_ = g.AddEdge(ctx, graph.Edge{
		Src: "service:opt-a", Kind: graph.EdgeDependsOn,
		Dst: "service:opt-b", Phase: "startup", Required: false,
	})

	// Contract for opt-b depending on opt-a (optional) — creates the cycle.
	contract := &packages.AwarenessContract{
		Package:     "opt-b-pkg",
		Service:     "opt-b",
		PackageKind: "APPLICATION",
		DependsOn: []packages.ContractDependency{
			{Service: "opt-a", Phase: "startup", Required: false},
		},
	}

	result, err := analysis.ValidatePackage(ctx, contract, "APPLICATION", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status == analysis.AdmissionBlock {
		t.Errorf("optional cycle must not result in BLOCK, got %s", result.Status)
		for _, r := range result.Reasons {
			t.Logf("  reason: [rule %d] %s: %s", r.Rule, r.Status, r.Message)
		}
	}
}

// Test 5: Package writing protected etcd key without declared invariant → BLOCK.
func TestAdmissionProtectedKeyWriteBlocks(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	// The graph has: invariant:install.result.atomic_commit protects
	//   etcd_key:/globular/nodes/{node_id}/packages/{kind}/{name}
	//
	// Contract writes that key but does NOT declare the invariant.
	contract := &packages.AwarenessContract{
		Package:     "bad-installer",
		Service:     "bad-installer",
		PackageKind: "INFRASTRUCTURE",
		Writes: packages.ContractState{
			EtcdKeys: []string{"/globular/nodes/{node_id}/packages/{kind}/{name}"},
		},
		// Intentionally omitting Invariants: install.result.atomic_commit
	}

	result, err := analysis.ValidatePackage(ctx, contract, "INFRASTRUCTURE", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionBlock {
		t.Errorf("expected BLOCK for undeclared protected key write, got %s", result.Status)
	}
	var hasRule7 bool
	for _, r := range result.Reasons {
		if r.Rule == 7 {
			hasRule7 = true
			break
		}
	}
	if !hasRule7 {
		t.Error("expected Rule 7 reason for protected-key violation")
	}
}

// Test 6: Package requesting a globally forbidden fix → BLOCK.
func TestAdmissionForbiddenWorkflowBlocks(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	// The graph has: forbidden_fix:blind_retry_workflow node.
	// Contract lists this as a remediation workflow.
	contract := &packages.AwarenessContract{
		Package:              "bad-pkg",
		Service:              "bad-svc",
		PackageKind:          "APPLICATION",
		RemediationWorkflows: []string{"blind_retry_workflow"},
	}

	result, err := analysis.ValidatePackage(ctx, contract, "APPLICATION", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionBlock {
		t.Errorf("expected BLOCK for forbidden workflow, got %s", result.Status)
	}
	if len(result.ForbiddenFixesFound) == 0 {
		t.Error("expected ForbiddenFixesFound to be populated")
	}
}

// Test 7: Valid repository-like package → ADMIT.
func TestAdmissionValidRepositoryPackageAdmits(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	// Add the test and workflow nodes the contract references.
	_ = g.AddNode(ctx, graph.Node{
		ID: "test:TestRepositoryMetadataFirstWhenMinIODown",
		Type: graph.NodeTypeTest, Name: "TestRepositoryMetadataFirstWhenMinIODown",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID: "workflow:remediate.repository_unreachable",
		Type: graph.NodeTypeWorkflow, Name: "remediate.repository_unreachable",
	})

	contract := &packages.AwarenessContract{
		Package:     "repository",
		Service:     "repository",
		PackageKind: "INFRASTRUCTURE",
		Summary:     "Stores package metadata and artifact references.",
		Owns: packages.ContractOwns{
			EtcdKeys:     []string{"/globular/repository/config"},
			ScyllaTables: []string{"repository_artifacts", "repository_manifests"},
			MinioBuckets: []string{"artifacts"},
		},
		Invariants: []string{"repository.metadata_first"},
		DependsOn: []packages.ContractDependency{
			{Service: "scylladb", Phase: "metadata_read", Required: true},
			{Service: "minio", Phase: "blob_read", Required: true},
			{Service: "minio", Phase: "metadata_read", Required: false},
			{Service: "etcd", Phase: "startup", Required: true},
		},
		Emits:         []string{"repository.artifact.published"},
		SafeDegradedModes: []string{"DEGRADED", "READ_ONLY"},
		ForbiddenFixes: []string{"block_metadata_read_on_minio_down"},
		RemediationWorkflows: []string{"remediate.repository_unreachable"},
		RequiredTests: []string{"TestRepositoryMetadataFirstWhenMinIODown"},
	}

	result, err := analysis.ValidatePackage(ctx, contract, "INFRASTRUCTURE", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionAdmit {
		t.Errorf("expected ADMIT for valid repository contract, got %s", result.Status)
		for _, r := range result.Reasons {
			t.Logf("  reason: [rule %d] %s: %s", r.Rule, r.Status, r.Message)
		}
	}
	// Impacted invariants should include repository.metadata_first.
	var hasMetadataFirst bool
	for _, inv := range result.ImpactedInvariants {
		if inv == "repository.metadata_first" {
			hasMetadataFirst = true
		}
	}
	if !hasMetadataFirst {
		t.Error("expected repository.metadata_first in impacted invariants")
	}
}

// Test 8: Admission preview does not mutate the main graph.
func TestAdmissionPreviewDoesNotMutateGraph(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	statsBefore, err := g.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	contract := &packages.AwarenessContract{
		Package:     "preview-test-pkg",
		Service:     "preview-test-svc",
		PackageKind: "APPLICATION",
		Owns: packages.ContractOwns{
			EtcdKeys: []string{"/globular/preview/test"},
		},
		DependsOn: []packages.ContractDependency{
			{Service: "etcd", Phase: "startup", Required: true},
		},
		Invariants:    []string{"preview.invariant"},
		RequiredTests: []string{"TestPreviewDoesNotPersist"},
	}

	// Run validation twice.
	for i := 0; i < 2; i++ {
		_, err := analysis.ValidatePackage(ctx, contract, "APPLICATION", g)
		if err != nil {
			t.Fatalf("ValidatePackage attempt %d: %v", i, err)
		}
	}

	statsAfter, err := g.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statsBefore.Nodes != statsAfter.Nodes {
		t.Errorf("graph mutated: nodes before=%d, after=%d", statsBefore.Nodes, statsAfter.Nodes)
	}
	if statsBefore.Edges != statsAfter.Edges {
		t.Errorf("graph mutated: edges before=%d, after=%d", statsBefore.Edges, statsAfter.Edges)
	}
}

// Additional: Rule 4 — required dep without phase → BLOCK.
func TestAdmissionRequiredDepWithoutPhaseBlocks(t *testing.T) {
	g := seedMainGraph(t)
	ctx := context.Background()

	contract := &packages.AwarenessContract{
		Package:     "no-phase-pkg",
		Service:     "no-phase-svc",
		PackageKind: "SERVICE",
		DependsOn: []packages.ContractDependency{
			{Service: "etcd", Phase: "", Required: true}, // missing phase
		},
	}

	result, err := analysis.ValidatePackage(ctx, contract, "SERVICE", g)
	if err != nil {
		t.Fatalf("ValidatePackage: %v", err)
	}
	if result.Status != analysis.AdmissionBlock {
		t.Errorf("expected BLOCK for required dep without phase, got %s", result.Status)
	}
}

// Additional: LoadAwarenessContract from file.
func TestLoadAwarenessContractFromFile(t *testing.T) {
	dir := t.TempDir()
	content := `
package_kind: INFRASTRUCTURE
service: test-svc
package: test-pkg
summary: Test package.
depends_on:
  - service: etcd
    phase: startup
    required: true
invariants:
  - install.result.atomic_commit
`
	yamlPath := filepath.Join(dir, "awareness.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := packages.LoadAwarenessContractFromFile(yamlPath)
	if err != nil {
		t.Fatalf("LoadAwarenessContractFromFile: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil contract")
	}
	if c.PackageKind != "INFRASTRUCTURE" {
		t.Errorf("kind: got %s, want INFRASTRUCTURE", c.PackageKind)
	}
	if c.Service != "test-svc" {
		t.Errorf("service: got %s, want test-svc", c.Service)
	}
	if len(c.DependsOn) != 1 || c.DependsOn[0].Phase != "startup" {
		t.Error("depends_on not parsed correctly")
	}
}

// Additional: ContractGraphPreview does not write to any graph.
func TestContractGraphPreviewIsReadOnly(t *testing.T) {
	contract := &packages.AwarenessContract{
		Package:     "preview-pkg",
		Service:     "preview-svc",
		PackageKind: "APPLICATION",
		Owns: packages.ContractOwns{
			EtcdKeys: []string{"/globular/preview"},
		},
	}

	nodes, edges := packages.ContractGraphPreview(contract)
	if len(nodes) == 0 {
		t.Error("expected nodes in preview")
	}
	if len(edges) == 0 {
		t.Error("expected edges in preview")
	}
	// Verify no graph was needed.
	t.Logf("preview: %d nodes, %d edges", len(nodes), len(edges))
}

// Additional: RenderAdmissionMarkdown produces meaningful output.
func TestRenderAdmissionMarkdown(t *testing.T) {
	result := &analysis.AdmissionResult{
		Status: analysis.AdmissionBlock,
		Reasons: []analysis.AdmissionReason{
			{Rule: 1, Status: analysis.AdmissionBlock, Message: "missing awareness.yaml"},
		},
	}
	md := analysis.RenderAdmissionMarkdown(nil, result)
	if !strings.Contains(md, "BLOCK") {
		t.Error("markdown missing BLOCK status")
	}
	if !strings.Contains(md, "missing awareness.yaml") {
		t.Error("markdown missing reason text")
	}
}

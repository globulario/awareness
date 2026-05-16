package manual_test

import (
	"context"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

const implYAML = `
invariants:
  - id: test.auth.token_validation
    title: Token must be validated before use
    severity: critical
    status: active
    summary: Every inbound token must be verified against the cluster CA before granting access.
    protects:
      files:
        - golang/auth/auth_server.go
      state:
        - /globular/auth/keys
    forbidden_fixes:
      - skip_token_validation_on_internal_call
    required_tests:
      - TestTokenValidated
    related_failure_modes:
      - auth.token.bypass
    implemented_by:
      - file: golang/auth/validate.go
        function: ValidateToken
        trust: strict_verified
        reads_authority:
          - /globular/auth/keys
        writes_state:
          - /globular/auth/sessions
        guards_action:
          - rpc.Authenticate
    authority:
      - source: /globular/auth/keys
        kind: etcd_key
        confidence: high
    verified_by:
      - TestTokenValidated_Proof
    violated_by:
      - auth.token.replay_attack
    decision_guidance:
      - "Always verify token signature before checking claims."
`

func TestInvariantLoader_VerifiesEdge(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// required_tests → test→verifies→invariant
	edges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeVerifies && e.Src == "test:TestTokenValidated" {
			found = true
		}
	}
	if !found {
		t.Error("expected test:TestTokenValidated -[verifies]-> invariant:test.auth.token_validation")
	}
}

func TestInvariantLoader_BlocksForbiddenActionEdge(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// forbidden_fixes → forbidden_fix→blocks_forbidden_action→invariant
	edges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeBlocksForbiddenAction &&
			e.Src == "forbidden_fix:skip_token_validation_on_internal_call" {
			found = true
		}
	}
	if !found {
		t.Error("expected forbidden_fix -[blocks_forbidden_action]-> invariant")
	}
}

func TestInvariantLoader_ViolatesEdge(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// related_failure_modes → failure_mode→violates→invariant
	edges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgeViolates && e.Src == "failure_mode:auth.token.bypass" {
			found = true
		}
	}
	if !found {
		t.Error("expected failure_mode:auth.token.bypass -[violates]-> invariant")
	}
}

func TestInvariantLoader_PartiallyImplementsEdge(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// protects.files → file→partially_implements→invariant (backward compat)
	edges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.Kind == graph.EdgePartiallyImplements &&
			e.Src == "source_file:golang/auth/auth_server.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected source_file -[partially_implements]-> invariant (from protects.files)")
	}
}

func TestInvariantLoader_ImplementedByEdgesWithSubEdges(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	implFile := "source_file:golang/auth/validate.go"
	outEdges, err := g.Neighbors(ctx, implFile, "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	kindSeen := make(map[string]bool)
	for _, e := range outEdges {
		kindSeen[e.Kind] = true
	}

	// implemented_by file → implements → invariant
	if !kindSeen[graph.EdgeImplements] {
		t.Error("expected implements edge from implemented_by file")
	}
	// reads_authority sub-edge
	if !kindSeen[graph.EdgeReadsAuthority] {
		t.Error("expected reads_authority edge from implemented_by file")
	}
	// writes_state sub-edge
	if !kindSeen[graph.EdgeWritesState] {
		t.Error("expected writes_state edge from implemented_by file")
	}
	// guards_action sub-edge
	if !kindSeen[graph.EdgeGuardsAction] {
		t.Error("expected guards_action edge from implemented_by file")
	}
}

func TestInvariantLoader_AuthorityEdge(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// invariant → reads_authority → authority node
	outEdges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range outEdges {
		if e.Kind == graph.EdgeReadsAuthority && e.Dst == "authority:/globular/auth/keys" {
			found = true
		}
	}
	if !found {
		t.Error("expected invariant -[reads_authority]-> authority node")
	}
}

func TestInvariantLoader_VerifiedByHighTrust(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// verified_by → test→verifies→invariant with trust=verified
	inEdges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range inEdges {
		if e.Kind == graph.EdgeVerifies && e.Src == "test:TestTokenValidated_Proof" {
			found = true
		}
	}
	if !found {
		t.Error("expected test:TestTokenValidated_Proof -[verifies]-> invariant from verified_by")
	}
}

func TestInvariantLoader_ViolatedByDeclarativeOverride(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", implYAML)

	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// violated_by → failure_mode→violates→invariant
	inEdges, err := g.Neighbors(ctx, "invariant:test.auth.token_validation", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range inEdges {
		if e.Kind == graph.EdgeViolates && e.Src == "failure_mode:auth.token.replay_attack" {
			found = true
		}
	}
	if !found {
		t.Error("expected failure_mode:auth.token.replay_attack -[violates]-> invariant from violated_by")
	}
}

func TestInvariantLoader_BackwardCompatProtectsFiles(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	// Minimal invariant with only protects.files — old schema, no new fields.
	p := writeYAML(t, dir, "inv.yaml", `
invariants:
  - id: old.style.invariant
    title: Old style
    severity: high
    status: active
    summary: Old invariant with only protects.files.
    protects:
      files:
        - golang/service/server.go
    forbidden_fixes:
      - some_bad_fix
    required_tests:
      - TestOldStyle
`)
	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	// Old behavior preserved: protects edge from invariant → file.
	outEdges, err := g.Neighbors(ctx, "invariant:old.style.invariant", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	hasProtects := false
	for _, e := range outEdges {
		if e.Kind == graph.EdgeProtects {
			hasProtects = true
		}
	}
	if !hasProtects {
		t.Error("backward compat: expected protects edge from invariant to file")
	}

	// New behavior: file→partially_implements→invariant also emitted.
	inEdges, err := g.Neighbors(ctx, "invariant:old.style.invariant", "in")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	hasPartial := false
	for _, e := range inEdges {
		if e.Kind == graph.EdgePartiallyImplements {
			hasPartial = true
		}
	}
	if !hasPartial {
		t.Error("expected partially_implements edge added to protects.files (new behavior)")
	}
}

func TestInvariantLoader_NoNewEdgesOnEmptyNewFields(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	// Invariant with no new schema fields — should not produce new edge types.
	p := writeYAML(t, dir, "inv.yaml", `
invariants:
  - id: minimal.invariant
    title: Minimal
    severity: low
    status: active
    summary: Minimal invariant.
    forbidden_fixes:
      - bad_fix
    required_tests:
      - TestMinimal
`)
	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	outEdges, err := g.Neighbors(ctx, "invariant:minimal.invariant", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	for _, e := range outEdges {
		if e.Kind == graph.EdgeReadsAuthority {
			t.Error("unexpected reads_authority edge on invariant with no authority[] field")
		}
		if e.Kind == graph.EdgeImplementedBy {
			t.Error("unexpected implemented_by edge on invariant with no implemented_by[] field")
		}
	}
}

// TestInvariantLoader_AuthorityShorthands verifies that authority[] entries using
// file: and etcd: shorthand fields produce correct authority_source nodes and
// reads_authority edges (rather than requiring source: + kind:).
func TestInvariantLoader_AuthorityShorthands(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", `
invariants:
  - id: test.authority.shorthands
    title: Authority shorthand test
    severity: high
    status: active
    summary: "Tests file and etcd shorthand fields in authority list."
    forbidden_fixes: []
    required_tests: []
    authority:
      - file: docs/awareness/knowledge/path_weights.yaml
      - etcd: /globular/services/{service_id}/config
`)
	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	outEdges, err := g.Neighbors(ctx, "invariant:test.authority.shorthands", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	var fileAuth, etcdAuth bool
	for _, e := range outEdges {
		if e.Kind == graph.EdgeReadsAuthority {
			if e.Dst == "authority:docs/awareness/knowledge/path_weights.yaml" {
				fileAuth = true
			}
			if e.Dst == "authority:/globular/services/{service_id}/config" {
				etcdAuth = true
			}
		}
	}
	if !fileAuth {
		t.Error("expected reads_authority to authority:docs/awareness/knowledge/path_weights.yaml (file: shorthand)")
	}
	if !etcdAuth {
		t.Error("expected reads_authority to authority:/globular/services/{service_id}/config (etcd: shorthand)")
	}
}

// TestInvariantLoader_FunctionsListInMetadata verifies that implemented_by entries
// using the functions: list (not the singular function: field) store all function
// names in edge metadata.
func TestInvariantLoader_FunctionsListInMetadata(t *testing.T) {
	g := openGraph(t)
	ctx := context.Background()
	dir := t.TempDir()
	p := writeYAML(t, dir, "inv.yaml", `
invariants:
  - id: test.impl.functions_list
    title: Functions list test
    severity: medium
    status: active
    summary: "Tests functions list field in implemented_by."
    forbidden_fixes: []
    required_tests: []
    implemented_by:
      - file: golang/mcp/tools_awareness_decision.go
        functions:
          - decisionContextNoGraph
          - buildDecisionContext
          - buildDecisionBlindSpots
        trust: strict_verified
`)
	if err := manual.LoadInvariants(ctx, g, p); err != nil {
		t.Fatalf("LoadInvariants: %v", err)
	}

	outEdges, err := g.Neighbors(ctx, "source_file:golang/mcp/tools_awareness_decision.go", "out")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}

	var implEdge *graph.Edge
	for i, e := range outEdges {
		if e.Kind == graph.EdgeImplements && e.Dst == "invariant:test.impl.functions_list" {
			implEdge = &outEdges[i]
			break
		}
	}
	if implEdge == nil {
		t.Fatal("expected implements edge from source_file to invariant")
	}

	// JSON round-trip: []string serializes as []interface{} on retrieval.
	fnsRaw, _ := implEdge.Metadata["functions"].([]interface{})
	if len(fnsRaw) != 3 {
		t.Errorf("expected 3 functions in metadata, got %d: %v", len(fnsRaw), fnsRaw)
	}
}

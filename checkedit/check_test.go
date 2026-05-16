package checkedit_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/checkedit"
	"github.com/globulario/awareness/graph"
)

func openTestGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("graph.Open: %v", err)
	}
	t.Cleanup(func() { g.Close() })
	return g
}

// seedCheckeditGraph builds a minimal graph:
//
//	source_file → (defines) → invariant → (forbids) → forbidden_fix
//	pattern → (requires) → invariant  (pattern has code_smells)
func seedCheckeditGraph(t *testing.T) *graph.Graph {
	t.Helper()
	ctx := context.Background()
	g := openTestGraph(t)

	const file = "golang/cluster_controller/convergence.go"

	_ = g.AddNode(ctx, graph.Node{
		ID:   "source_file:" + file,
		Type: graph.NodeTypeSourceFile,
		Name: "convergence.go",
		Path: file,
	})
	_ = g.UpsertInvariant(ctx, graph.Invariant{
		ID:       "infra.desired_hash_consistency",
		Title:    "Desired hash must be stable",
		Severity: "critical",
		Status:   "active",
	})
	_ = g.AddNode(ctx, graph.Node{
		ID:   "invariant:infra.desired_hash_consistency",
		Type: graph.NodeTypeInvariant,
		Name: "infra.desired_hash_consistency",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "source_file:" + file,
		Kind: graph.EdgeEnforces,
		Dst:  "invariant:infra.desired_hash_consistency",
	})

	_ = g.AddNode(ctx, graph.Node{
		ID:   "forbidden_fix:use_raw_digest",
		Type: graph.NodeTypeForbiddenFix,
		Name: "use_raw_digest",
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "invariant:infra.desired_hash_consistency",
		Kind: graph.EdgeForbids,
		Dst:  "forbidden_fix:use_raw_digest",
	})

	_ = g.AddNode(ctx, graph.Node{
		ID:   "pattern:raw_digest_as_hash",
		Type: graph.NodeTypePattern,
		Name: "raw_digest_as_hash",
		Metadata: map[string]any{
			"code_smells": []any{"raw_artifact_digest_as_desired_hash"},
		},
	})
	_ = g.AddEdge(ctx, graph.Edge{
		Src:  "pattern:raw_digest_as_hash",
		Kind: graph.EdgeRequires,
		Dst:  "invariant:infra.desired_hash_consistency",
	})

	return g
}

func TestRunReturnsIssuesForAnnotatedFile(t *testing.T) {
	g := seedCheckeditGraph(t)

	r, err := checkedit.Run(context.Background(), g, checkedit.Options{
		File: "golang/cluster_controller/convergence.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !r.HasIssues {
		t.Error("expected HasIssues=true")
	}
	if len(r.ForbiddenFixes) == 0 {
		t.Error("expected forbidden fixes")
	}
	if len(r.CodeSmells) == 0 {
		t.Error("expected code smells via pattern→invariant link")
	}
}

func TestRunNoGraphNodeReturnsWarning(t *testing.T) {
	g := openTestGraph(t)

	r, err := checkedit.Run(context.Background(), g, checkedit.Options{
		File: "golang/some/unknown/file.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Warnings) == 0 {
		t.Error("expected warning for file with no graph node")
	}
	warnFound := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "no graph node") {
			warnFound = true
		}
	}
	if !warnFound {
		t.Errorf("expected 'no graph node' warning, got %v", r.Warnings)
	}
}

func TestRunNilGraphReturnsWarning(t *testing.T) {
	r, err := checkedit.Run(context.Background(), nil, checkedit.Options{
		File: "any/file.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Warnings) == 0 {
		t.Error("expected warning when graph is nil")
	}
}

func TestRenderMarkdownContainsSections(t *testing.T) {
	r := &checkedit.CheckEditResult{
		File:           "golang/foo/bar.go",
		HasIssues:      true,
		ForbiddenFixes: []string{"do_the_wrong_thing"},
		CodeSmells:     []string{"bad_pattern_usage"},
	}

	md := checkedit.RenderCheckEdit(r, "markdown")
	if !strings.Contains(md, "Forbidden fixes") {
		t.Error("markdown missing 'Forbidden fixes' section")
	}
	if !strings.Contains(md, "Code smells") {
		t.Error("markdown missing 'Code smells' section")
	}
	if !strings.Contains(md, "do_the_wrong_thing") {
		t.Error("markdown missing forbidden fix entry")
	}
}

func TestRenderAgentAlertFormat(t *testing.T) {
	r := &checkedit.CheckEditResult{
		File:           "golang/foo/bar.go",
		HasIssues:      true,
		ForbiddenFixes: []string{"inline_state_change"},
	}

	out := checkedit.RenderCheckEdit(r, "agent")
	if !strings.Contains(out, "CHECK-EDIT ALERT") {
		t.Error("agent format missing CHECK-EDIT ALERT header")
	}
	if !strings.Contains(out, "inline_state_change") {
		t.Error("agent format missing forbidden fix entry")
	}
}

func TestRenderAgentClearFormat(t *testing.T) {
	r := &checkedit.CheckEditResult{
		File:      "golang/foo/clean.go",
		HasIssues: false,
	}

	out := checkedit.RenderCheckEdit(r, "agent")
	if !strings.Contains(out, "CHECK-EDIT CLEAR") {
		t.Errorf("agent format should say CLEAR for no issues, got: %s", out)
	}
}

func TestRenderJSONValid(t *testing.T) {
	r := &checkedit.CheckEditResult{
		File:           "golang/foo/bar.go",
		HasIssues:      true,
		ForbiddenFixes: []string{"some_fix"},
		CodeSmells:     []string{"some_smell"},
	}

	out := checkedit.RenderCheckEdit(r, "json")
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := parsed["forbidden_fixes"]; !ok {
		t.Error("JSON missing 'forbidden_fixes'")
	}
	if _, ok := parsed["code_smells"]; !ok {
		t.Error("JSON missing 'code_smells'")
	}
}

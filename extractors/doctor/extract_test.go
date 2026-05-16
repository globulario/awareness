package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/graph"
)

// TestExtract_BasicRule verifies the canonical (ID/Category/Scope) triple is
// recognised and emitted as a single detector node with the expected metadata.
func TestExtract_BasicRule(t *testing.T) {
	dir := t.TempDir()
	src := `package rules

type myRule struct{}

func (myRule) ID() string       { return "test.my_rule" }
func (myRule) Category() string { return "test" }
func (myRule) Scope() string    { return "node" }
`
	if err := os.WriteFile(filepath.Join(dir, "my_rule.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	res, err := Extract(context.Background(), g, dir, dir, "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Rules) != 1 {
		t.Fatalf("got %d rules, want 1: %+v", len(res.Rules), res.Rules)
	}
	r := res.Rules[0]
	if r.ID != "test.my_rule" || r.Category != "test" || r.Scope != "node" {
		t.Errorf("rule mismatch: %+v", r)
	}

	n, err := g.FindNode(context.Background(), "detector:test.my_rule")
	if err != nil || n == nil {
		t.Fatalf("expected detector node, err=%v node=%v", err, n)
	}
	if n.Type != graph.NodeTypeDoctorEvidence {
		t.Errorf("node type = %s, want %s", n.Type, graph.NodeTypeDoctorEvidence)
	}
	if n.Metadata["kind"] != "doctor_rule" {
		t.Errorf("metadata kind = %v, want doctor_rule", n.Metadata["kind"])
	}
}

// TestExtract_PointerReceiver verifies *T receivers are handled identically
// to T receivers.
func TestExtract_PointerReceiver(t *testing.T) {
	dir := t.TempDir()
	src := `package rules

type myPtrRule struct{}

func (*myPtrRule) ID() string       { return "test.ptr_rule" }
func (*myPtrRule) Category() string { return "test" }
func (*myPtrRule) Scope() string    { return "cluster" }
`
	if err := os.WriteFile(filepath.Join(dir, "ptr_rule.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()
	res, err := Extract(context.Background(), g, dir, dir, "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Rules) != 1 || res.Rules[0].ID != "test.ptr_rule" {
		t.Fatalf("rules=%+v", res.Rules)
	}
}

// TestExtract_SkipsTestFiles: rules defined in *_test.go files must not be
// emitted (otherwise mock rules pollute the live graph).
func TestExtract_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	prod := `package rules

type prod struct{}

func (prod) ID() string       { return "real.rule" }
func (prod) Category() string { return "real" }
func (prod) Scope() string    { return "node" }
`
	test := `package rules

type mock struct{}

func (mock) ID() string       { return "mock.rule" }
func (mock) Category() string { return "mock" }
func (mock) Scope() string    { return "node" }
`
	if err := os.WriteFile(filepath.Join(dir, "rule.go"), []byte(prod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rule_test.go"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()
	res, err := Extract(context.Background(), g, dir, dir, "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d: %+v", len(res.Rules), res.Rules)
	}
	if res.Rules[0].ID != "real.rule" {
		t.Errorf("got id=%s, want real.rule", res.Rules[0].ID)
	}
}

// TestExtract_PartialMethodsStillEmit: a rule with only ID() (no Category/Scope)
// is still extracted because the doctor package may evolve toward optional
// metadata. Empty fields just mean "unknown."
func TestExtract_PartialMethodsStillEmit(t *testing.T) {
	dir := t.TempDir()
	src := `package rules

type bare struct{}

func (bare) ID() string { return "bare.rule" }
`
	if err := os.WriteFile(filepath.Join(dir, "bare.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()
	res, err := Extract(context.Background(), g, dir, dir, "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Rules) != 1 || res.Rules[0].ID != "bare.rule" {
		t.Fatalf("rules=%+v", res.Rules)
	}
}

// TestExtract_LiveDoctorRulesProduceNodes is the integration test against the
// actual cluster_doctor source. It pins the count loosely (≥30) so adding new
// rules doesn't break the test, but a regression that drops most rules will.
func TestExtract_LiveDoctorRulesProduceNodes(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("repo root not found: %v", err)
	}
	rulesDir := filepath.Join(repoRoot, "golang/cluster_doctor/cluster_doctor_server/rules")
	if _, err := os.Stat(rulesDir); err != nil {
		t.Skipf("rules dir not present: %v", err)
	}
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()
	res, err := Extract(context.Background(), g, rulesDir, repoRoot, "")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(res.Rules) < 30 {
		t.Errorf("extracted %d doctor rules, expected ≥30 — extractor regressed?", len(res.Rules))
	}
}

// TestExtract_MappingEmitsMatchesFailureModeEdge proves the mapping pass
// turns a detector_mapping.yaml entry into a real graph edge. This is the
// load-bearing piece for "did the join from doctor rules to failure_modes
// actually wire up?" If this test fails, well_covered count won't grow.
func TestExtract_MappingEmitsMatchesFailureModeEdge(t *testing.T) {
	dir := t.TempDir()
	src := `package rules

type myRule struct{}

func (myRule) ID() string       { return "etcd.quorum" }
func (myRule) Category() string { return "etcd" }
func (myRule) Scope() string    { return "cluster" }
`
	if err := os.WriteFile(filepath.Join(dir, "rule.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mapping := `detector_mappings:
  - failure_mode: etcd.leader_instability
    detectors: [etcd.quorum]
    reason: leader instability surfaces as quorum churn
  - failure_mode: nonexistent.fm
    detectors: [does.not.exist]
    reason: should be reported as skipped
`
	mappingPath := filepath.Join(dir, "detector_mapping.yaml")
	if err := os.WriteFile(mappingPath, []byte(mapping), 0o644); err != nil {
		t.Fatal(err)
	}

	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()
	res, err := Extract(context.Background(), g, dir, dir, mappingPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if res.MappingsApplied != 1 {
		t.Errorf("MappingsApplied=%d, want 1 (the etcd.leader_instability mapping)", res.MappingsApplied)
	}
	if len(res.MappingsSkipped) != 1 {
		t.Errorf("MappingsSkipped=%v, want one entry for the nonexistent rule", res.MappingsSkipped)
	}

	// Verify the edge actually landed in the graph.
	out, err := g.OutgoingEdges(context.Background(), "detector:etcd.quorum")
	if err != nil {
		t.Fatalf("OutgoingEdges: %v", err)
	}
	found := false
	for _, e := range out {
		if e.Kind == graph.EdgeMatchesFailureMode && e.Dst == "failure_mode:etcd.leader_instability" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected detector:etcd.quorum --matches_failure_mode--> failure_mode:etcd.leader_instability, got %+v", out)
	}
}

// findRepoRoot walks up looking for the docs/awareness directory, which is at
// the repo root in this layout (one level above golang/).
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "awareness")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

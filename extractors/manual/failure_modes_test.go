package manual_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/extractors/manual"
	"github.com/globulario/awareness/graph"
)

func TestLoadFailureModesWiresMitigationsDetectorsTestsAndIncidents(t *testing.T) {
	g, err := graph.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	defer g.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "failure_modes.yaml")
	yaml := `failure_modes:
  - id: fm.extractor.wiring
    title: extractor wiring test
    severity: critical
    root_cause: missing edges
    required_tests:
      - TestExtractorWiring
    mitigates:
      - pattern.extractor_guard
    detectors:
      - detector.extractor_alarm
    related_incidents:
      - INC-9999-0001
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := manual.LoadFailureModes(context.Background(), g, path); err != nil {
		t.Fatalf("LoadFailureModes: %v", err)
	}

	assertEdge := func(src, kind, dst string) {
		t.Helper()
		edges, err := g.OutgoingEdges(context.Background(), src)
		if err != nil {
			t.Fatalf("OutgoingEdges(%s): %v", src, err)
		}
		for _, e := range edges {
			if e.Kind == kind && e.Dst == dst {
				return
			}
		}
		t.Fatalf("missing edge %s -[%s]-> %s", src, kind, dst)
	}

	fmNode := "failure_mode:fm.extractor.wiring"
	assertEdge("design_pattern:pattern.extractor_guard", graph.EdgeMitigates, fmNode)
	assertEdge("detector:detector.extractor_alarm", graph.EdgeMatchesFailureMode, fmNode)
	assertEdge("incident:INC-9999-0001", graph.EdgeCausedBy, fmNode)
	assertEdge(fmNode, graph.EdgeTestedBy, "test:TestExtractorWiring")
	assertEdge("test:TestExtractorWiring", graph.EdgeVerifies, fmNode)
}

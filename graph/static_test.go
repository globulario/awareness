package graph_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/awareness/graph"
)

func TestBuild_IncludeTypeScript(t *testing.T) {
	fixtureDir := filepath.Join("..", "examples", "frontend-app", "src")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skip("frontend-app fixture not found")
	}

	result, err := graph.Build(graph.BuildInput{
		ProjectName: "test",
		SourceRoots: []string{fixtureDir},
	}, graph.BuildOptions{
		IncludeTypeScript: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.FrontendFileCount == 0 {
		t.Error("expected FrontendFileCount > 0")
	}
	if result.FrontendNodeCount == 0 {
		t.Error("expected FrontendNodeCount > 0")
	}

	// Verify source_file nodes exist for each discovered file.
	sourceFileNodes := 0
	frontendNodes := 0
	for _, n := range result.Graph.Nodes {
		switch {
		case n.Kind == "source_file":
			sourceFileNodes++
		case len(n.Kind) > 9 && n.Kind[:9] == "frontend_":
			frontendNodes++
		}
	}
	if sourceFileNodes == 0 {
		t.Error("expected source_file nodes in graph")
	}
	if frontendNodes == 0 {
		t.Error("expected frontend_* nodes in graph")
	}
	if frontendNodes != result.FrontendNodeCount {
		t.Errorf("FrontendNodeCount=%d but counted %d frontend_* nodes in graph", result.FrontendNodeCount, frontendNodes)
	}

	// Verify edges were emitted.
	if len(result.Graph.Edges) == 0 {
		t.Error("expected edges connecting source_file nodes to frontend constructs")
	}
}

func TestBuild_NoTypeScript_WhenFlagFalse(t *testing.T) {
	fixtureDir := filepath.Join("..", "examples", "frontend-app", "src")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skip("frontend-app fixture not found")
	}

	result, err := graph.Build(graph.BuildInput{
		ProjectName: "test",
		SourceRoots: []string{fixtureDir},
	}, graph.BuildOptions{
		IncludeTypeScript: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.FrontendFileCount != 0 {
		t.Errorf("expected FrontendFileCount=0 when IncludeTypeScript=false, got %d", result.FrontendFileCount)
	}
	if result.FrontendNodeCount != 0 {
		t.Errorf("expected FrontendNodeCount=0 when IncludeTypeScript=false, got %d", result.FrontendNodeCount)
	}
	for _, n := range result.Graph.Nodes {
		if len(n.Kind) > 9 && n.Kind[:9] == "frontend_" {
			t.Errorf("unexpected frontend_* node when IncludeTypeScript=false: %s", n.ID)
		}
	}
}

// Package tests extracts test nodes from *_test.go files.
// It delegates to the goast extractor's test extraction logic.
package tests

import (
	"context"

	"github.com/globulario/awareness/extractors/goast"
	"github.com/globulario/awareness/graph"
)

// Extract walks walkDir for *_test.go files and creates test nodes.
// Paths are stored relative to pathRoot (typically the repo root).
func Extract(ctx context.Context, g *graph.Graph, walkDir, pathRoot string) error {
	return goast.ExtractTests(ctx, g, walkDir, pathRoot)
}

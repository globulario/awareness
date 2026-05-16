package enforce

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
)

// AuditDrift reports graph nodes that no longer correspond to files on disk.
// It only checks source_file nodes — the most common source of drift after
// renames or deletions.
//
// Missing file → WARNING (rebuild will clean it up)
// Invariant node with no enforces/protects edge → INFO (orphaned awareness)
func AuditDrift(ctx context.Context, g *graph.Graph, srcDir string) []Finding {
	if g == nil {
		return nil
	}

	var findings []Finding

	// Check source_file nodes.
	fileNodes, err := g.FindNodesByType(ctx, graph.NodeTypeSourceFile)
	if err == nil {
		for _, n := range fileNodes {
			if n.Path == "" {
				continue
			}
			// Skip test files (may be generated/temp).
			if strings.HasSuffix(n.Path, "_test.go") {
				continue
			}
			// Graph paths are repo-relative (e.g. "golang/foo/bar.go").
			// srcDir may be the golang sub-directory or the repo root.
			// Try srcDir-relative first, then fall back to the parent (repo root)
			// so both calling conventions work correctly.
			absPath := filepath.Join(srcDir, n.Path)
			if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
				// Try repo-root-relative (parent of srcDir).
				altPath := filepath.Join(filepath.Dir(srcDir), n.Path)
				if _, altErr := os.Stat(altPath); altErr == nil {
					continue // file exists at the repo-root-relative path — not stale
				}
				findings = append(findings, Finding{
					Code:     CodeStaleSourceFileNode,
					Severity: SeverityWarning,
					File:     n.Path,
					Message:  "graph node exists for '" + n.Path + "' but the file no longer exists on disk — run 'globular awareness build' to refresh",
				})
			}
		}
	}

	// Check invariant nodes with no enforcing symbol.
	invNodes, err := g.FindNodesByType(ctx, graph.NodeTypeInvariant)
	if err == nil {
		enforcesEdges, _ := g.EdgesByKind(ctx, graph.EdgeEnforces)
		protectsEdges, _ := g.EdgesByKind(ctx, graph.EdgeProtects)

		enforced := make(map[string]bool)
		for _, e := range enforcesEdges {
			enforced[e.Dst] = true
		}
		for _, e := range protectsEdges {
			enforced[e.Dst] = true
		}

		for _, n := range invNodes {
			if !enforced[n.ID] {
				findings = append(findings, Finding{
					Code:     CodeInvariantNoEnforcer,
					Severity: SeverityInfo,
					Message:  "invariant node '" + n.Name + "' has no enforces or protects edge — it may not be annotated in source code yet",
				})
			}
		}
	}

	return findings
}

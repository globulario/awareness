package graph

import (
	"context"
	"fmt"
)

// FileAnnotationSummary contains protocol annotations explicitly declared on
// symbols (or the file itself) defined in a specific source file. Only
// direct 1-hop annotation edges are followed — no transitive traversal —
// so the result is fast and unambiguous.
type FileAnnotationSummary struct {
	Invariants       []string // invariant IDs from enforces/protects edges
	ForbiddenFixes   []string // forbidden_fix names from forbids edges
	HashSchemas      []string // hash_schema names from produces/requires edges
	StateTransitions []string // state_transition descriptions from affects edges
	RequiredTests    []string // test names from tested_by edges
	Risks            []string // risk_surface IDs from affects edges
	HasCritical      bool     // true if any attached invariant has severity="critical"
}

// AnnotationsForFile returns explicit protocol annotations attached to symbols
// defined in the file at relPath. Returns an empty summary (not an error) if
// the file has no graph node or no annotation edges.
func (g *Graph) AnnotationsForFile(ctx context.Context, relPath string) (*FileAnnotationSummary, error) {
	summary := &FileAnnotationSummary{}

	fileNodeID := "source_file:" + relPath

	// Collect symbol IDs defined by this file plus the file node itself.
	ownerIDs := []string{fileNodeID}

	rows, err := g.db.QueryContext(ctx, `
		SELECT dst FROM edges WHERE kind = ? AND src = ?
	`, EdgeDefines, fileNodeID)
	if err != nil {
		return summary, fmt.Errorf("AnnotationsForFile defines query: %w", err)
	}
	for rows.Next() {
		var dst string
		if err := rows.Scan(&dst); err != nil {
			rows.Close()
			return summary, err
		}
		ownerIDs = append(ownerIDs, dst)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return summary, err
	}

	// For each owner, follow annotation edges one hop.
	for _, ownerID := range ownerIDs {
		edges, err := g.Neighbors(ctx, ownerID, "out")
		if err != nil {
			continue
		}
		for _, e := range edges {
			switch e.Kind {
			case EdgeEnforces, EdgeProtects:
				name, critical := g.resolveInvariantName(ctx, e.Dst)
				if name != "" {
					summary.Invariants = annotAppendUnique(summary.Invariants, name)
					if critical {
						summary.HasCritical = true
					}
				}

			case EdgeForbids:
				if n, _ := g.FindNode(ctx, e.Dst); n != nil {
					summary.ForbiddenFixes = annotAppendUnique(summary.ForbiddenFixes, n.Name)
				}

			case EdgeProduces, EdgeRequires:
				if n, _ := g.FindNode(ctx, e.Dst); n != nil && n.Type == NodeTypeHashSchema {
					summary.HashSchemas = annotAppendUnique(summary.HashSchemas, n.Name)
				}

			case EdgeAffects:
				if n, _ := g.FindNode(ctx, e.Dst); n != nil {
					switch n.Type {
					case NodeTypeStateTransition:
						summary.StateTransitions = annotAppendUnique(summary.StateTransitions, n.Name)
					case NodeTypeRiskSurface:
						summary.Risks = annotAppendUnique(summary.Risks, n.Name)
					}
				}

			case EdgeTestedBy:
				if n, _ := g.FindNode(ctx, e.Dst); n != nil {
					summary.RequiredTests = annotAppendUnique(summary.RequiredTests, n.Name)
				}
			}
		}
	}

	return summary, nil
}

// resolveInvariantName returns the invariant's human-readable ID and whether
// it has severity="critical". Falls back to the node Name if no invariants
// table record exists.
func (g *Graph) resolveInvariantName(ctx context.Context, nodeID string) (name string, critical bool) {
	// Strip "invariant:" prefix to get the table ID.
	const prefix = "invariant:"
	tableID := nodeID
	if len(nodeID) > len(prefix) && nodeID[:len(prefix)] == prefix {
		tableID = nodeID[len(prefix):]
	}

	if inv, _ := g.FindInvariant(ctx, tableID); inv != nil {
		return inv.ID, inv.Severity == "critical"
	}

	// Fall back to node name.
	if n, _ := g.FindNode(ctx, nodeID); n != nil {
		return n.Name, false
	}
	return "", false
}

// annotAppendUnique appends s to slice if not already present.
func annotAppendUnique(slice []string, s string) []string {
	if s == "" {
		return slice
	}
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

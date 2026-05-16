package enforce

import (
	"context"

	"github.com/globulario/awareness/graph"
)

// ValidateContracts checks that every hash_schema node has at least one
// producer (EdgeProduces) and at least one consumer (EdgeRequires).
//
// A schema with only a producer and no consumer → WARNING (contract incomplete).
// A schema with only a consumer and no producer → ERROR (broken contract).
// A schema node that exists but has no edges at all → ERROR (orphaned node).
func ValidateContracts(ctx context.Context, g *graph.Graph) []Finding {
	if g == nil {
		return nil
	}

	schemaNodes, err := g.FindNodesByType(ctx, graph.NodeTypeHashSchema)
	if err != nil {
		return []Finding{{
			Code:     "CONTRACT_QUERY_ERROR",
			Severity: SeverityError,
			Message:  "failed to query hash_schema nodes: " + err.Error(),
		}}
	}

	producerEdges, _ := g.EdgesByKind(ctx, graph.EdgeProduces)
	consumerEdges, _ := g.EdgesByKind(ctx, graph.EdgeRequires)

	// Index by schema node ID.
	producers := make(map[string][]string) // schemaID → []srcSymbol
	consumers := make(map[string][]string) // schemaID → []srcSymbol
	for _, e := range producerEdges {
		producers[e.Dst] = append(producers[e.Dst], e.Src)
	}
	for _, e := range consumerEdges {
		consumers[e.Dst] = append(consumers[e.Dst], e.Src)
	}

	var findings []Finding
	for _, n := range schemaNodes {
		hasProducer := len(producers[n.ID]) > 0
		hasConsumer := len(consumers[n.ID]) > 0

		switch {
		case !hasProducer && !hasConsumer:
			findings = append(findings, Finding{
				Code:     "ORPHANED_HASH_SCHEMA",
				Severity: SeverityError,
				Message:  "hash_schema '" + n.Name + "' has no producer and no consumer — orphaned node",
			})

		case !hasProducer:
			findings = append(findings, Finding{
				Code:     CodeHashSchemaNoProducer,
				Severity: SeverityError,
				Message:  "hash_schema '" + n.Name + "' has consumers but no producer — add //globular:hash_schema " + n.Name + " to the producing function",
			})

		case !hasConsumer:
			findings = append(findings, Finding{
				Code:     CodeHashSchemaNoConsumer,
				Severity: SeverityWarning,
				Message:  "hash_schema '" + n.Name + "' has a producer but no consumer — add //globular:expects_hash_schema " + n.Name + " to the consuming function",
			})
		}
	}

	return findings
}

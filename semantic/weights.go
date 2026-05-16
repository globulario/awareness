package semantic

import (
	"github.com/globulario/awareness/graph"
)

// Dimension constants identify the semantic lens through which edge weight is computed.
const (
	DimensionCode     = "code"
	DimensionModule   = "module"
	DimensionService  = "service"
	DimensionPackage  = "package"
	DimensionState    = "state"
	DimensionWorkflow = "workflow"
	DimensionArch     = "architecture"
	DimensionRuntime  = "runtime"
	DimensionHistory  = "history"
	DimensionTest     = "test"
	DimensionAll      = "all"
)

// baseWeights maps edge kinds to their base traversal cost.
// Lower cost = stronger / more meaningful connection.
var baseWeights = map[string]float64{
	// Cost 1 — very strong
	graph.EdgeEnforces:      1,
	graph.EdgeProtects:      1,
	graph.EdgeForbids:       1,
	graph.EdgeViolates:      1,
	graph.EdgeTestedBy:      1,
	graph.EdgeValidatedBy:   1,
	graph.EdgeCausedBy:      1,
	graph.EdgeFixedBy:       1,
	graph.EdgeExplains:      1,
	graph.EdgeDecides:       1,
	graph.EdgeDerivedFrom:   1,
	graph.EdgePromotedTo:    1,
	graph.EdgeAliases:       1,

	// Cost 2 — strong
	graph.EdgeWrites:          2,
	graph.EdgeReads:           2,
	graph.EdgeOwns:            2,
	graph.EdgeAffects:         2,
	graph.EdgeRequires:        2,
	graph.EdgeDependsOn:       2,
	graph.EdgeRemediatedBy:    2,
	graph.EdgeDocuments:       2,
	graph.EdgeGeneralizesTo:   2,
	graph.EdgeSpecializes:     2,

	// Cost 4 — medium
	graph.EdgeCalls:           4,
	graph.EdgeProduces:        4,
	graph.EdgeEmits:           4,
	graph.EdgeSubscribes:      4,
	graph.EdgeRunsAs:          4,
	graph.EdgeControls:        4,
	graph.EdgeCurrentStatusOf: 4,
	graph.EdgeHasStateDelta:   4,

	// Cost 6 — weak
	graph.EdgeImports:     6,
	graph.EdgeMentionedIn: 6,
	graph.EdgeRecords:     6,
	graph.EdgeRecalls:     6,
}

// dimensionBoosts maps dimension → edge-kind → delta applied AFTER base lookup.
// Negative delta = cheaper (more relevant for this dimension).
var dimensionBoosts = map[string]map[string]float64{
	DimensionArch: {
		graph.EdgeEnforces:    -2,
		graph.EdgeProtects:    -2,
		graph.EdgeForbids:     -2,
		graph.EdgeExplains:    -2,
		graph.EdgeDecides:     -2,
		graph.EdgeCausedBy:    -1,
		graph.EdgeTestedBy:    -1,
		graph.EdgeValidatedBy: -1,
	},
	DimensionHistory: {
		graph.EdgeCausedBy:       -2,
		graph.EdgeFixedBy:        -2,
		graph.EdgeViolates:       -2,
		graph.EdgeAffects:        -1,
		graph.EdgeFixes:          -2,
		graph.EdgePartiallyFixes: -1,
	},
	DimensionTest: {
		graph.EdgeTestedBy:    -3,
		graph.EdgeValidatedBy: -3,
		graph.EdgeRequiresTest: -3,
		graph.EdgeEnforces:    -1,
		graph.EdgeProtects:    -1,
	},
	DimensionCode: {
		graph.EdgeDefines:  -5,
		graph.EdgeCalls:    -2,
		graph.EdgeImports:  -2,
		graph.EdgeTestedBy: -1,
	},
	DimensionModule: {
		graph.EdgeOwns:     -2,
		graph.EdgeDefines:  -3,
		graph.EdgeImports:  -2,
		graph.EdgeTestedBy: -1,
	},
	DimensionService: {
		graph.EdgeOwns:      -2,
		graph.EdgeDependsOn: -1,
		graph.EdgeRunsAs:    -2,
		graph.EdgeProtects:  -1,
	},
	DimensionState: {
		graph.EdgeReads:    -2,
		graph.EdgeWrites:   -2,
		graph.EdgeOwns:     -1,
		graph.EdgeProtects: -1,
	},
	DimensionRuntime: {
		graph.EdgeCurrentStatusOf: -3,
		graph.EdgeHasStateDelta:   -3,
		graph.EdgeRunsAs:          -2,
		graph.EdgeProtects:        -1,
	},
	DimensionWorkflow: {
		graph.EdgeOwns:      -2,
		graph.EdgeDependsOn: -1,
	},
}

// runtimeDimensions is the set of dimensions where runtime-sourced edges are welcome.
var runtimeDimensions = map[string]bool{
	DimensionRuntime: true,
	DimensionAll:     true,
}

// serviceLikeWorkflowRuntimeDimensions — dimensions where required edges are cheaper.
var requiredBoostDimensions = map[string]bool{
	DimensionService:  true,
	DimensionWorkflow: true,
	DimensionRuntime:  true,
}

// EdgeWeight returns the semantic traversal cost of edge e in the given dimension.
// Lower cost = stronger/more meaningful connection. Minimum is always 1.
func EdgeWeight(dimension string, e graph.Edge) float64 {
	// 1. Base cost from table, or 10 for unknown / fallback.
	base, ok := baseWeights[e.Kind]
	if !ok {
		base = 10
	}
	cost := base

	// 2. Dimension-specific boost.
	if boosts, ok := dimensionBoosts[dimension]; ok {
		if delta, ok := boosts[e.Kind]; ok {
			cost += delta
		}
	}

	// 3. Modifiers applied after base+dimension lookup.

	// explicit metadata reduces cost by 1.
	if v, ok := e.Metadata["explicit"]; ok {
		if b, ok := v.(bool); ok && b {
			cost -= 1
		}
	}

	// Low-confidence edges are penalised.
	if e.Confidence > 0 && e.Confidence < 0.5 {
		cost += 3
	}

	// Required edges in service/workflow/runtime dimensions are cheaper.
	if e.Required && requiredBoostDimensions[dimension] {
		cost -= 1
	}

	// Non-required depends_on edges are penalised.
	if e.Kind == graph.EdgeDependsOn && !e.Required {
		cost += 2
	}

	// Runtime-source edges in non-runtime dimensions are expensive.
	if sk, ok := e.Metadata["source_kind"]; ok {
		if skStr, ok := sk.(string); ok && skStr == "runtime" {
			if runtimeDimensions[dimension] {
				cost -= 1
			} else {
				cost += 5
			}
		}
	}

	// Clamp to minimum of 1.
	if cost < 1 {
		cost = 1
	}
	return cost
}

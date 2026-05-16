package awarectx

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// Explanation is a natural-language architectural summary of a node's role.
type Explanation struct {
	NodeID   string `json:"node_id"`
	NodeType string `json:"node_type"`
	Name     string `json:"name"`
	Role     string `json:"role"`

	Protects []string `json:"protects"`
	Risks    []string `json:"risks"`
	Warnings []string `json:"warnings"`
	Tests    []string `json:"tests"`
	Searches []string `json:"searches"`
}

// ExplainNode generates a natural-language explanation of a node's role,
// what it protects, what risks it carries, and what to watch out for.
func ExplainNode(ctx context.Context, g *graph.Graph, nodeID string, opts Options) (*Explanation, error) {
	if opts.MaxItems <= 0 {
		opts.MaxItems = 20
	}
	if opts.Depth <= 0 {
		opts.Depth = 2
	}

	node, err := g.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	ex := &Explanation{
		NodeID:   node.ID,
		NodeType: node.Type,
		Name:     node.Name,
		Role:     roleDescription(node),
	}

	// Collect what this node protects (outgoing protects/enforces edges).
	outEdges, err := g.Neighbors(ctx, nodeID, "out")
	if err != nil {
		return nil, err
	}
	for _, e := range outEdges {
		switch e.Kind {
		case graph.EdgeProtects, graph.EdgeEnforces:
			target, _ := g.FindNode(ctx, e.Dst)
			ex.Protects = appendUniq(ex.Protects, nodeNameOrID(target, e.Dst))
		case graph.EdgeTestedBy, graph.EdgeRequiresTest:
			target, _ := g.FindNode(ctx, e.Dst)
			ex.Tests = appendUniq(ex.Tests, nodeNameOrID(target, e.Dst))
		}
	}

	// Collect risks: incoming affects/violates/blocks edges and failure modes.
	// Also collect forbidden fixes from invariants that protect this node.
	inEdges, err := g.Neighbors(ctx, nodeID, "in")
	if err != nil {
		return nil, err
	}
	for _, e := range inEdges {
		src, _ := g.FindNode(ctx, e.Src)
		switch e.Kind {
		case graph.EdgeAffects:
			if src != nil && src.Type == graph.NodeTypeFailureMode {
				ex.Risks = appendUniq(ex.Risks, "failure mode: "+src.Name)
			}
		case graph.EdgeViolates:
			if src != nil {
				ex.Risks = appendUniq(ex.Risks, "violation recorded: "+src.Name)
			}
		case graph.EdgeBlocks:
			if src != nil {
				ex.Risks = appendUniq(ex.Risks, "blocked by: "+src.Name)
			}
		case graph.EdgeTestedBy, graph.EdgeRequiresTest:
			if src != nil {
				ex.Tests = appendUniq(ex.Tests, src.Name)
			}
		case graph.EdgeProtects, graph.EdgeEnforces:
			// Collect forbidden fixes from protecting/enforcing invariants.
			if src != nil && (src.Type == graph.NodeTypeInvariant || src.Type == graph.NodeTypeForbiddenFix) {
				invOut, _ := g.Neighbors(ctx, src.ID, "out")
				for _, e2 := range invOut {
					if e2.Kind == graph.EdgeForbids {
						fix, _ := g.FindNode(ctx, e2.Dst)
						if fix != nil {
							ex.Warnings = appendUniq(ex.Warnings, "do not apply: "+fix.Name)
						}
					}
				}
			}
		}
	}

	// Traverse to find deeper risks and protections.
	tr, err := g.Traverse(ctx, nodeID, opts.Depth, []string{
		graph.EdgeAffects, graph.EdgeProtects, graph.EdgeForbids, graph.EdgeTestedBy,
	})
	if err == nil {
		for _, n := range tr.Nodes {
			if n.ID == nodeID {
				continue
			}
			switch n.Type {
			case graph.NodeTypeFailureMode:
				ex.Risks = appendUniq(ex.Risks, "failure mode: "+n.Name)
			case graph.NodeTypeForbiddenFix:
				ex.Warnings = appendUniq(ex.Warnings, "do not apply: "+n.Name)
			case graph.NodeTypeTest:
				ex.Tests = appendUniq(ex.Tests, n.Name)
			}
		}
	}

	// Warnings from invariants protecting this node.
	if inv, _ := g.FindInvariant(ctx, node.ID); inv != nil {
		ex.Warnings = appendUniq(ex.Warnings, inv.Summary)
	}

	// Type-specific warnings.
	ex.Warnings = append(ex.Warnings, typeWarnings(node)...)

	// Recommended searches.
	ex.Searches = append(ex.Searches, "grep -r '"+node.Name+"' golang/")
	if node.Path != "" {
		ex.Searches = append(ex.Searches, "globular awareness impact --file "+node.Path)
	}
	ex.Searches = append(ex.Searches, "globular awareness node-context --node "+node.ID)

	// Cap lists.
	ex.Protects = capStrings(ex.Protects, opts.MaxItems, "", nil)
	ex.Risks = capStrings(ex.Risks, opts.MaxItems, "", nil)
	ex.Tests = capStrings(ex.Tests, opts.MaxItems, "", nil)

	return ex, nil
}

// roleDescription returns a one-sentence role description for a node.
func roleDescription(n *graph.Node) string {
	switch n.Type {
	case graph.NodeTypeInvariant:
		return fmt.Sprintf("Invariant that must hold at all times: %s.", n.Summary)
	case graph.NodeTypeFailureMode:
		return fmt.Sprintf("Documented failure mode describing a known way the system can break: %s.", n.Summary)
	case graph.NodeTypeForbiddenFix:
		return fmt.Sprintf("Forbidden fix pattern — this approach must never be applied: %s.", n.Summary)
	case graph.NodeTypeGlobularService:
		return fmt.Sprintf("Globular service %q — a long-running gRPC server deployed on cluster nodes.", n.Name)
	case graph.NodeTypeGoPackage:
		return fmt.Sprintf("Go package %q containing the implementation.", n.Name)
	case graph.NodeTypeSymbol:
		return fmt.Sprintf("Go symbol %q — a function, type, or variable defined in the codebase.", n.Name)
	case graph.NodeTypeSourceFile:
		return fmt.Sprintf("Source file at %q.", n.Path)
	case graph.NodeTypeTest:
		return fmt.Sprintf("Test %q — verifies a specific behavior or invariant.", n.Name)
	case graph.NodeTypeWorkflow:
		return fmt.Sprintf("Workflow %q — a multi-step cluster mutation that must reach SUCCEEDED or FAILED.", n.Name)
	case graph.NodeTypeEtcdKey:
		return fmt.Sprintf("etcd key %q — part of the cluster's single source of truth.", n.Name)
	case graph.NodeTypeSystemdUnit:
		return fmt.Sprintf("systemd unit %q — manages a service process on the node.", n.Name)
	case graph.NodeTypeProtoService:
		return fmt.Sprintf("Proto service %q — gRPC API contract.", n.Name)
	case graph.NodeTypeRPCMethod:
		return fmt.Sprintf("RPC method %q.", n.Name)
	default:
		if n.Summary != "" {
			return n.Summary
		}
		return fmt.Sprintf("%s node %q.", strings.ReplaceAll(n.Type, "_", " "), n.Name)
	}
}

// typeWarnings returns type-specific edit warnings.
func typeWarnings(n *graph.Node) []string {
	switch n.Type {
	case graph.NodeTypeGlobularService:
		return []string{"All config changes must flow through workflows — no inline state mutations."}
	case graph.NodeTypeEtcdKey:
		return []string{"etcd writes must be transactional and idempotent — never use blind overwrites."}
	case graph.NodeTypeWorkflow:
		return []string{"Workflows must reach SUCCEEDED or FAILED — no silent exits, no partial states."}
	case graph.NodeTypeSystemdUnit:
		return []string{"ExecStartPre must begin with pkill -9 guard to kill cgroup-escaped orphans."}
	case graph.NodeTypeInvariant:
		return []string{"Changing an invariant definition requires verifying all enforcing code paths still hold."}
	}
	return nil
}

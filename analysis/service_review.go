// Package analysis: ServiceDesignReview synthesizes proto, implementation,
// dependency, invariant, and runtime identity information for a named service.
// This is a read-only analytical function — it does not mutate the graph.
package analysis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/awareness/graph"
)

// ServiceDesignReview is the structured output of ReviewService.
type ServiceDesignReview struct {
	ServiceID    string
	ServiceName  string
	ProtoService string
	ProtoFile    string
	SystemdUnit  string

	APIContract     APIContract
	Dependencies    []ServiceDependency
	Invariants      ServiceInvariants
	ForbiddenFixes  []ExplainedFinding
	RequiredTests   []ExplainedFinding
	MissingLinks    []string
	Recommendations []ServiceRecommendation
}

// APIContract describes the RPC surface of the service.
type APIContract struct {
	RPCs []RPCEntry
}

// RPCEntry describes one RPC method.
type RPCEntry struct {
	Name          string
	StreamingMode string // "", "client_streaming", "server_streaming", "bidirectional_streaming"
	HasAuthz      bool
	AuthzAction   string
	AuthzResource string
	Gaps          []string
}

// ServiceDependency describes one declared or discovered dependency.
type ServiceDependency struct {
	Service  string
	Phase    string
	Required bool
	Class    string // declared_required, declared_optional, unknown_unclassified
}

// ServiceInvariants groups invariants by severity.
type ServiceInvariants struct {
	Critical []string
	High     []string
	Medium   []string
	Low      []string
}

// ServiceRecommendation is an actionable suggestion from the review.
type ServiceRecommendation struct {
	Priority  string // critical, high, medium, low
	Action    string
	Evidence  string
	GraphPath string
}

// ReviewService performs a design-level review of the named service.
// It queries the awareness graph and returns a structured report.
// The serviceID can be the service ID (e.g. "file-service") or
// the proto service name (e.g. "file.FileService").
func ReviewService(ctx context.Context, g *graph.Graph, serviceID string) (*ServiceDesignReview, error) {
	review := &ServiceDesignReview{ServiceID: serviceID}

	// Find the service node.
	svcNode, err := findServiceNodeByID(ctx, g, serviceID)
	if err != nil {
		return nil, err
	}
	if svcNode == nil {
		review.MissingLinks = append(review.MissingLinks,
			fmt.Sprintf("service node %q not found in graph — run 'globular awareness build --clean' to index it", serviceID))
		return review, nil
	}
	review.ServiceID = svcNode.Name
	review.ServiceName = svcNode.Name

	// Walk outgoing edges to find linked proto service, systemd unit, proto file.
	ownedEdges, err := g.OutgoingEdges(ctx, svcNode.ID)
	if err != nil {
		return nil, fmt.Errorf("ReviewService outgoing edges: %w", err)
	}

	var implFileIDs []string
	for _, e := range ownedEdges {
		n, err := g.FindNode(ctx, e.Dst)
		if err != nil || n == nil {
			continue
		}
		switch {
		case n.Type == graph.NodeTypeProtoService && e.Kind == graph.EdgeProvidesService:
			review.ProtoService = n.Name
		case n.Type == graph.NodeTypeProtoService && e.Kind == graph.EdgeOwns && review.ProtoService == "":
			review.ProtoService = n.Name
		case n.Type == graph.NodeTypeSystemdUnit:
			review.SystemdUnit = n.Name
		case n.Type == graph.NodeTypeSourceFile && strings.HasSuffix(n.Path, ".proto"):
			review.ProtoFile = n.Path
		case n.Type == graph.NodeTypeSourceFile && e.Kind == graph.EdgeOwns:
			implFileIDs = append(implFileIDs, n.ID)
		}
	}

	// API contract: find proto service node and enumerate its RPCs.
	if review.ProtoService != "" {
		protoSvcID := "proto_service:" + review.ProtoService
		rpcEdges, err := g.OutgoingEdges(ctx, protoSvcID)
		if err == nil {
			for _, e := range rpcEdges {
				if e.Kind != graph.EdgeOwns {
					continue
				}
				rpcNode, err := g.FindNode(ctx, e.Dst)
				if err != nil || rpcNode == nil || rpcNode.Type != graph.NodeTypeRPCMethod {
					continue
				}
				entry := RPCEntry{Name: rpcNode.Name}

				// Parse streaming mode from the node's summary field.
				if strings.Contains(rpcNode.Summary, "[") {
					s := rpcNode.Summary
					start := strings.LastIndex(s, "[")
					end := strings.LastIndex(s, "]")
					if start >= 0 && end > start {
						entry.StreamingMode = s[start+1 : end]
					}
				}

				// Find authz annotation.
				authzEdges, _ := g.OutgoingEdges(ctx, rpcNode.ID)
				for _, ae := range authzEdges {
					if ae.Kind != graph.EdgeHasAuthz {
						continue
					}
					authzNode, err := g.FindNode(ctx, ae.Dst)
					if err != nil || authzNode == nil {
						continue
					}
					entry.HasAuthz = true
					entry.AuthzAction = authzNode.Name
					entry.AuthzResource = extractResourceFromSummary(authzNode.Summary)
					break
				}
				if !entry.HasAuthz {
					entry.Gaps = append(entry.Gaps, "missing authz annotation")
				}
				review.APIContract.RPCs = append(review.APIContract.RPCs, entry)
			}
		}
		// Sort for determinism.
		sort.Slice(review.APIContract.RPCs, func(i, j int) bool {
			return review.APIContract.RPCs[i].Name < review.APIContract.RPCs[j].Name
		})
	}

	// Dependencies: find all depends_on neighbors of the service node.
	for _, e := range ownedEdges {
		if e.Kind != graph.EdgeDependsOn {
			continue
		}
		n, err := g.FindNode(ctx, e.Dst)
		if err != nil || n == nil {
			continue
		}
		class := "declared_required"
		if !e.Required {
			class = "declared_optional"
		}
		review.Dependencies = append(review.Dependencies, ServiceDependency{
			Service:  n.Name,
			Phase:    e.Phase,
			Required: e.Required,
			Class:    class,
		})
	}

	// Invariants: walk implementation files → outgoing implementation/enforces/configures edges → invariant nodes.
	invariantsSeen := make(map[string]bool)
	for _, fileID := range implFileIDs {
		fileEdges, err := g.OutgoingEdges(ctx, fileID)
		if err != nil {
			continue
		}
		for _, fe := range fileEdges {
			if fe.Kind != graph.EdgeImplements &&
				fe.Kind != graph.EdgeEnforces &&
				fe.Kind != graph.EdgeConfigures &&
				fe.Kind != graph.EdgeObserves {
				continue
			}
			invNode, err := g.FindNode(ctx, fe.Dst)
			if err != nil || invNode == nil || invNode.Type != graph.NodeTypeInvariant {
				continue
			}
			if invariantsSeen[invNode.ID] {
				continue
			}
			invariantsSeen[invNode.ID] = true

			inv, err := g.FindInvariant(ctx, invNode.Name)
			if err != nil || inv == nil {
				continue
			}

			fileNode, _ := g.FindNode(ctx, fileID)
			filePath := fileID
			if fileNode != nil {
				filePath = fileNode.Path
			}

			severity := strings.ToLower(inv.Severity)
			switch severity {
			case "critical":
				review.Invariants.Critical = append(review.Invariants.Critical, inv.ID)
			case "high":
				review.Invariants.High = append(review.Invariants.High, inv.ID)
			case "medium":
				review.Invariants.Medium = append(review.Invariants.Medium, inv.ID)
			default:
				review.Invariants.Low = append(review.Invariants.Low, inv.ID)
			}

			// Collect forbidden fixes and required tests from this invariant.
			invEdges, _ := g.OutgoingEdges(ctx, invNode.ID)
			for _, ie := range invEdges {
				switch ie.Kind {
				case graph.EdgeForbids:
					fixNode, err := g.FindNode(ctx, ie.Dst)
					if err != nil || fixNode == nil {
						continue
					}
					review.ForbiddenFixes = append(review.ForbiddenFixes, ExplainedFinding{
						NodeID:    fixNode.ID,
						NodeType:  fixNode.Type,
						NodeName:  fixNode.Name,
						Severity:  inv.Severity,
						Mandatory: true,
						EdgePath: []string{fmt.Sprintf("%s →[%s]→ %s →[forbids]→ %s",
							filePath, fe.Kind, inv.ID, fixNode.Name)},
						Source: "invariant:" + inv.ID,
					})
				case graph.EdgeTestedBy:
					testNode, err := g.FindNode(ctx, ie.Dst)
					if err != nil || testNode == nil {
						continue
					}
					review.RequiredTests = append(review.RequiredTests, ExplainedFinding{
						NodeID:    testNode.ID,
						NodeType:  testNode.Type,
						NodeName:  testNode.Name,
						Severity:  inv.Severity,
						Mandatory: true,
						EdgePath: []string{fmt.Sprintf("%s →[%s]→ %s →[tested_by]→ %s",
							filePath, fe.Kind, inv.ID, testNode.Name)},
						Source: "invariant:" + inv.ID,
					})
				}
			}
		}
	}

	// Deduplicate.
	review.ForbiddenFixes = deduplicateFindings(review.ForbiddenFixes)
	review.RequiredTests = deduplicateFindings(review.RequiredTests)

	// Missing link suggestions.
	if review.ProtoService == "" {
		review.MissingLinks = append(review.MissingLinks,
			fmt.Sprintf("service %q has no proto_service edge — add proto_service: field in docs/awareness/services.yaml", serviceID))
	}
	if len(review.APIContract.RPCs) == 0 && review.ProtoService != "" {
		review.MissingLinks = append(review.MissingLinks,
			fmt.Sprintf("proto service %q has no RPC nodes — run 'globular awareness build --clean' to index proto files", review.ProtoService))
	}
	if len(implFileIDs) == 0 {
		review.MissingLinks = append(review.MissingLinks,
			fmt.Sprintf("service %q has no implementation file edges — add implementation: list to docs/awareness/services.yaml", serviceID))
	}

	// Recommendations.
	var rpcsWithoutAuthz []string
	for _, rpc := range review.APIContract.RPCs {
		if !rpc.HasAuthz {
			rpcsWithoutAuthz = append(rpcsWithoutAuthz, rpc.Name)
		}
	}
	if len(rpcsWithoutAuthz) > 0 {
		review.Recommendations = append(review.Recommendations, ServiceRecommendation{
			Priority: "critical",
			Action:   fmt.Sprintf("Add authz annotations to: %s", strings.Join(rpcsWithoutAuthz, ", ")),
			Evidence: "RPCs without authz annotations allow unauthenticated access",
		})
	}
	for _, rpc := range review.APIContract.RPCs {
		if rpc.StreamingMode == "client_streaming" || rpc.StreamingMode == "bidirectional_streaming" {
			modeLabel := "Client-streaming"
			if rpc.StreamingMode == "bidirectional_streaming" {
				modeLabel = "Bidirectional-streaming"
			}
			review.Recommendations = append(review.Recommendations, ServiceRecommendation{
				Priority: "critical",
				Action:   fmt.Sprintf("Verify %s resolves and authorizes path BEFORE writing stream data", rpc.Name),
				Evidence: modeLabel + " RPC — path known only from first chunk",
				GraphPath: fmt.Sprintf("rpc:%s.%s →[governed_by]→ invariant:file.authz.streaming_write_path_must_be_authorized_before_data_write",
					review.ProtoService, rpc.Name),
			})
		}
	}

	return review, nil
}

// findServiceNodeByID tries to locate the GlobularService node with the given ID
// or name, trying various ID forms.
func findServiceNodeByID(ctx context.Context, g *graph.Graph, serviceID string) (*graph.Node, error) {
	// Try "service:<id>" directly.
	n, err := g.FindNode(ctx, "service:"+serviceID)
	if err == nil && n != nil && n.Type == graph.NodeTypeGlobularService {
		return n, nil
	}
	// Try proto service → owner lookup.
	protoNode, err := g.FindNode(ctx, "proto_service:"+serviceID)
	if err == nil && protoNode != nil {
		// Search all service nodes for one that owns this proto service.
		services, err := g.FindNodesByType(ctx, graph.NodeTypeGlobularService)
		if err != nil {
			return nil, err
		}
		for _, svc := range services {
			edges, _ := g.OutgoingEdges(ctx, svc.ID)
			for _, e := range edges {
				if e.Dst == protoNode.ID {
					return svc, nil
				}
			}
		}
	}
	// Try name search.
	services, err := g.FindNodesByType(ctx, graph.NodeTypeGlobularService)
	if err != nil {
		return nil, err
	}
	for _, svc := range services {
		if strings.EqualFold(svc.Name, serviceID) ||
			strings.EqualFold(svc.ID, "service:"+serviceID) {
			return svc, nil
		}
	}
	return nil, nil
}

// extractResourceFromSummary extracts the resource_type value from an authz summary.
func extractResourceFromSummary(summary string) string {
	if idx := strings.Index(summary, "resource_type="); idx >= 0 {
		rest := summary[idx+len("resource_type="):]
		if end := strings.IndexAny(rest, " ,;"); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	return ""
}

// deduplicateFindings removes duplicate findings keeping the first occurrence.
func deduplicateFindings(findings []ExplainedFinding) []ExplainedFinding {
	seen := make(map[string]bool)
	result := make([]ExplainedFinding, 0, len(findings))
	for _, f := range findings {
		if !seen[f.NodeID] {
			seen[f.NodeID] = true
			result = append(result, f)
		}
	}
	return result
}

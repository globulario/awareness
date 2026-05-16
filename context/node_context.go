package awarectx

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/graph"
)

// ConfidenceLabel indicates how the node context data was sourced.
type ConfidenceLabel string

const (
	ConfidenceExplicit  ConfidenceLabel = "explicit"
	ConfidenceExtracted ConfidenceLabel = "extracted"
	ConfidenceInferred  ConfidenceLabel = "inferred"
	ConfidenceRuntime   ConfidenceLabel = "runtime"
	ConfidenceLearned   ConfidenceLabel = "learned"
)

// Zoom controls which semantic layers are included in NodeContext output.
type Zoom string

const (
	ZoomLocal        Zoom = "local"        // node identity + direct edges + direct tests
	ZoomModule       Zoom = "module"       // + file/package/service ownership + symbols in same file
	ZoomService      Zoom = "service"      // + dependencies + workflows + proto APIs + runtime phases
	ZoomArchitecture Zoom = "architecture" // + invariants + design decisions + forbidden fixes + guardrails
	ZoomRuntime      Zoom = "runtime"      // + runtime snapshot evidence + doctor findings + state deltas
	ZoomHistory      Zoom = "history"      // + failure modes + fix cases + patterns + incidents
	ZoomAll          Zoom = "all"          // all of the above
)

// Options configures NodeContext and Explanation construction.
type Options struct {
	Zoom               Zoom // semantic zoom level (default ZoomAll)
	MaxItems           int  // per-list cap (default 20)
	Depth              int  // traversal depth (default 2)
	IncludeRuntime     bool // include runtime bridge evidence
	IncludeProvenance  bool // include source labels on each item
}

// EdgeSummary is a condensed view of a graph edge.
type EdgeSummary struct {
	Kind       string          `json:"kind"`
	TargetID   string          `json:"target_id"`
	TargetName string          `json:"target_name"`
	TargetType string          `json:"target_type"`
	Confidence float64         `json:"confidence"`
	Required   bool            `json:"required"`
	Provenance ConfidenceLabel `json:"provenance,omitempty"` // populated when IncludeProvenance=true
}

// NodeContext is the full architectural context for a single graph node.
type NodeContext struct {
	NodeID   string `json:"node_id"`
	NodeType string `json:"node_type"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Summary  string `json:"summary,omitempty"`

	DirectAnnotations []EdgeSummary `json:"direct_annotations"`
	IncomingEdges     []EdgeSummary `json:"incoming_edges"`
	OutgoingEdges     []EdgeSummary `json:"outgoing_edges"`

	RelatedInvariants   []graph.Invariant   `json:"related_invariants"`
	RelatedFailureModes []graph.FailureMode  `json:"related_failure_modes"`

	ForbiddenFixes []string `json:"forbidden_fixes"`
	StateReads     []string `json:"state_reads"`
	StateWrites    []string `json:"state_writes"`

	Package string `json:"package,omitempty"`
	Service string `json:"service,omitempty"`

	DependencyPhases []string `json:"dependency_phases"`
	RequiredTests    []string `json:"required_tests"`
	FixCases         []string `json:"fix_cases"`
	DesignPatterns   []string `json:"design_patterns"`
	AntiPatterns     []string `json:"anti_patterns"`
	CodeSmells       []string `json:"code_smells"`
	DesignDecisions  []string `json:"design_decisions"`

	SourceLabel         ConfidenceLabel `json:"source_label"`
	EditWarnings        []string        `json:"edit_warnings"`
	RecommendedSearches []string        `json:"recommended_searches"`
	RuntimeEvidence     []string        `json:"runtime_evidence"`

	Truncated map[string]int `json:"truncated,omitempty"`
}

// Build constructs a NodeContext for the node with the given ID.
func Build(ctx context.Context, g *graph.Graph, nodeID string, opts Options) (*NodeContext, error) {
	if opts.MaxItems <= 0 {
		opts.MaxItems = 20
	}
	if opts.Depth <= 0 {
		opts.Depth = 2
	}
	if opts.Zoom == "" {
		opts.Zoom = ZoomAll
	}

	node, err := g.FindNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}

	nc := &NodeContext{
		NodeID:         node.ID,
		NodeType:       node.Type,
		Name:           node.Name,
		Path:           node.Path,
		Summary:        node.Summary,
		SourceLabel:    labelFromNodeType(node.Type),
		Truncated:      make(map[string]int),
		DesignPatterns: []string{},
		AntiPatterns:   []string{},
		CodeSmells:     []string{},
	}

	// Local zoom: always included — direct edges + annotations.
	if err := nc.classifyEdges(ctx, g, node, opts); err != nil {
		return nil, err
	}

	// Module / service / architecture / history zoom: traverse related nodes.
	if opts.Zoom != ZoomLocal {
		nc.traverseRelated(ctx, g, nodeID, opts)
	}

	// Design pattern layer: use collected invariants to surface patterns/anti-patterns.
	// Done after traverseRelated so RelatedInvariants is populated.
	if opts.Zoom == ZoomArchitecture || opts.Zoom == ZoomHistory || opts.Zoom == ZoomAll {
		nc.enrichDesignContext(ctx, g)
	}

	// Runtime zoom: incoming runtime-bridge evidence.
	if opts.Zoom == ZoomRuntime || opts.Zoom == ZoomAll || opts.IncludeRuntime {
		nc.collectRuntimeEvidence(ctx, g, nodeID)
	}

	// Build edit warnings and recommended searches.
	nc.EditWarnings = buildEditWarnings(node, nc)
	nc.RecommendedSearches = buildRecommendedSearches(node, nc)

	// Apply per-list caps.
	nc.RelatedInvariants = capInvariants(nc.RelatedInvariants, opts.MaxItems, "related_invariants", nc.Truncated)
	nc.RelatedFailureModes = capFailureModes(nc.RelatedFailureModes, opts.MaxItems, "related_failure_modes", nc.Truncated)
	nc.ForbiddenFixes = capStrings(nc.ForbiddenFixes, opts.MaxItems, "forbidden_fixes", nc.Truncated)
	nc.StateReads = capStrings(nc.StateReads, opts.MaxItems, "state_reads", nc.Truncated)
	nc.StateWrites = capStrings(nc.StateWrites, opts.MaxItems, "state_writes", nc.Truncated)
	nc.RequiredTests = capStrings(nc.RequiredTests, opts.MaxItems, "required_tests", nc.Truncated)
	nc.DesignDecisions = capStrings(nc.DesignDecisions, opts.MaxItems, "design_decisions", nc.Truncated)
	nc.FixCases = capStrings(nc.FixCases, opts.MaxItems, "fix_cases", nc.Truncated)

	return nc, nil
}

func (nc *NodeContext) classifyEdges(ctx context.Context, g *graph.Graph, node *graph.Node, opts Options) error {
	allEdges, err := g.Neighbors(ctx, node.ID, "both")
	if err != nil {
		return err
	}

	var direct, incoming, outgoing []EdgeSummary

	edgeSumFn := toEdgeSummary
	if opts.IncludeProvenance {
		edgeSumFn = toEdgeSummaryWithProvenance
	}

	for _, e := range allEdges {
		if e.Src == node.ID {
			target, _ := g.FindNode(ctx, e.Dst)
			es := edgeSumFn(e, e.Dst, target)
			outgoing = append(outgoing, es)

			switch e.Kind {
			case graph.EdgeReads:
				nc.StateReads = appendUniq(nc.StateReads, nodeNameOrID(target, e.Dst))
			case graph.EdgeWrites:
				nc.StateWrites = appendUniq(nc.StateWrites, nodeNameOrID(target, e.Dst))
			case graph.EdgeDependsOn:
				if target != nil && target.Type == graph.NodeTypeDependencyPhase {
					nc.DependencyPhases = appendUniq(nc.DependencyPhases, target.Name)
				}
			case graph.EdgeForbids:
				if target != nil {
					nc.ForbiddenFixes = appendUniq(nc.ForbiddenFixes, target.Name)
				}
			case graph.EdgeTestedBy:
				if target != nil {
					nc.RequiredTests = appendUniq(nc.RequiredTests, target.Name)
				}
			}

			if isSafetyAnnotation(e.Kind) {
				direct = append(direct, es)
			}
		} else {
			src, _ := g.FindNode(ctx, e.Src)
			es := edgeSumFn(e, e.Src, src)
			incoming = append(incoming, es)

			switch e.Kind {
			case graph.EdgeProtects, graph.EdgeEnforces:
				if src != nil && src.Type == graph.NodeTypeInvariant {
					inv := findInvariantByNode(ctx, g, src)
					if inv != nil {
						nc.RelatedInvariants = appendInvariant(nc.RelatedInvariants, *inv)
					}
				}
				if src != nil && src.Type == graph.NodeTypeForbiddenFix {
					nc.ForbiddenFixes = appendUniq(nc.ForbiddenFixes, src.Name)
				}
			case graph.EdgeFixes, graph.EdgePartiallyFixes:
				if src != nil && src.Type == graph.NodeTypeFixCase {
					nc.FixCases = appendUniq(nc.FixCases, src.Name)
				}
			case graph.EdgeRequiresTest:
				if src != nil && src.Type == graph.NodeTypeTest {
					nc.RequiredTests = appendUniq(nc.RequiredTests, src.Name)
				}
			case graph.EdgeAffects:
				if src != nil && src.Type == graph.NodeTypeFailureMode {
					if zoomIncludes(opts.Zoom, ZoomHistory) || zoomIncludes(opts.Zoom, ZoomArchitecture) {
						nc.RelatedFailureModes = appendFailureMode(nc.RelatedFailureModes,
							graph.FailureMode{ID: src.ID, Title: src.Name, Summary: src.Summary})
					}
				}
			case graph.EdgeExplains, graph.EdgeDecides, graph.EdgeDocuments:
				if src != nil && (src.Type == graph.NodeTypeArchitectureDecision || src.Type == graph.NodeTypeDesignRule) {
					if zoomIncludes(opts.Zoom, ZoomArchitecture) {
						nc.DesignDecisions = appendUniq(nc.DesignDecisions, src.Name)
					}
				}
			}
		}
	}

	nc.DirectAnnotations = capEdges(direct, opts.MaxItems, "direct_annotations", nc.Truncated)
	nc.IncomingEdges = capEdges(incoming, opts.MaxItems, "incoming_edges", nc.Truncated)
	nc.OutgoingEdges = capEdges(outgoing, opts.MaxItems, "outgoing_edges", nc.Truncated)
	return nil
}

func (nc *NodeContext) traverseRelated(ctx context.Context, g *graph.Graph, nodeID string, opts Options) {
	safetyEdges := []string{
		graph.EdgeAffects, graph.EdgeProtects, graph.EdgeEnforces,
		graph.EdgeDependsOn, graph.EdgeOwns, graph.EdgeForbids,
		graph.EdgeTestedBy, graph.EdgeRequiresTest,
		graph.EdgeExplains, graph.EdgeDecides, graph.EdgeDocuments,
	}
	tr, err := g.Traverse(ctx, nodeID, opts.Depth, safetyEdges)
	if err != nil {
		return
	}

	// Phase 2: walk ancestors via incoming structural edges so that a symbol or
	// file node inherits context from its parent service/package. This lets
	// "sym:Foo" reach the invariants that protect the owning "svc:bar".
	ancestors := collectAncestors(ctx, g, nodeID, opts.Depth)
	for _, ancestorID := range ancestors {
		parentTr, err2 := g.Traverse(ctx, ancestorID, opts.Depth, safetyEdges)
		if err2 != nil {
			continue
		}
		tr.Nodes = append(tr.Nodes, parentTr.Nodes...)
	}
	// Phase 2b: also run classifyEdges logic for service-type ancestors so that
	// incoming EdgeProtects on the service are captured.
	for _, ancestorID := range ancestors {
		ancNode, _ := g.FindNode(ctx, ancestorID)
		if ancNode == nil {
			continue
		}
		ancIn, _ := g.Neighbors(ctx, ancestorID, "in")
		for _, e := range ancIn {
			if e.Kind != graph.EdgeProtects && e.Kind != graph.EdgeEnforces {
				continue
			}
			src, _ := g.FindNode(ctx, e.Src)
			if src != nil && src.Type == graph.NodeTypeInvariant {
				if inv := findInvariantByNode(ctx, g, src); inv != nil {
					nc.RelatedInvariants = appendInvariant(nc.RelatedInvariants, *inv)
				}
			}
		}
	}

	wantArchitecture := zoomIncludes(opts.Zoom, ZoomArchitecture)
	wantHistory := zoomIncludes(opts.Zoom, ZoomHistory)
	wantModule := zoomIncludes(opts.Zoom, ZoomModule)

	for _, n := range tr.Nodes {
		if n.ID == nodeID {
			continue
		}
		switch n.Type {
		case graph.NodeTypeFailureMode:
			if wantHistory || wantArchitecture {
				nc.RelatedFailureModes = appendFailureMode(nc.RelatedFailureModes,
					graph.FailureMode{ID: n.ID, Title: n.Name, Summary: n.Summary})
			}
		case graph.NodeTypeInvariant:
			if wantArchitecture {
				if inv := findInvariantByNode(ctx, g, n); inv != nil {
					nc.RelatedInvariants = appendInvariant(nc.RelatedInvariants, *inv)
				}
			}
		case graph.NodeTypeGoPackage:
			if wantModule && nc.Package == "" {
				nc.Package = n.Name
			}
		case graph.NodeTypeGlobularService:
			if wantModule && nc.Service == "" {
				nc.Service = n.Name
			}
		case graph.NodeTypeForbiddenFix:
			if wantArchitecture {
				nc.ForbiddenFixes = appendUniq(nc.ForbiddenFixes, n.Name)
			}
		case graph.NodeTypeTest:
			nc.RequiredTests = appendUniq(nc.RequiredTests, n.Name)
		case graph.NodeTypeGuardrail, graph.NodeTypeRemainingGap:
			if wantArchitecture {
				nc.AntiPatterns = appendUniq(nc.AntiPatterns, n.Name)
			}
		case graph.NodeTypeDesignPattern:
			if wantArchitecture || wantHistory {
				nc.DesignPatterns = appendUniq(nc.DesignPatterns, n.Name)
			}
		case graph.NodeTypeAntiPattern:
			if wantArchitecture || wantHistory {
				nc.AntiPatterns = appendUniq(nc.AntiPatterns, n.Name)
				// Unpack code_smells embedded in anti-pattern metadata.
				if smells, ok := n.Metadata["code_smells"]; ok {
					if items, ok := smells.([]interface{}); ok {
						for _, item := range items {
							if s, ok := item.(string); ok && s != "" {
								nc.CodeSmells = appendUniq(nc.CodeSmells, s)
							}
						}
					}
				}
			}
		case graph.NodeTypeCodeSmell:
			if wantArchitecture || wantHistory {
				nc.CodeSmells = appendUniq(nc.CodeSmells, n.Name)
			}
		case graph.NodeTypePattern:
			if wantArchitecture || wantHistory {
				nc.AntiPatterns = appendUniq(nc.AntiPatterns, n.Name)
				// Unpack code_smells from node metadata.
				if smells, ok := n.Metadata["code_smells"]; ok {
					switch v := smells.(type) {
					case []interface{}:
						for _, item := range v {
							if s, ok := item.(string); ok && s != "" {
								nc.CodeSmells = appendUniq(nc.CodeSmells, s)
							}
						}
					}
				}
			}
		case graph.NodeTypeDependencyPhase:
			nc.DependencyPhases = appendUniq(nc.DependencyPhases, n.Name)
		case graph.NodeTypeArchitectureDecision, graph.NodeTypeDesignRule:
			if wantArchitecture {
				nc.DesignDecisions = appendUniq(nc.DesignDecisions, n.Name)
			}
		case graph.NodeTypeFixCase:
			if wantHistory {
				nc.FixCases = appendUniq(nc.FixCases, n.Name)
			}
		}
	}
}

// zoomIncludes returns true if the given zoom level includes the target capability.
func zoomIncludes(zoom, target Zoom) bool {
	if zoom == ZoomAll {
		return true
	}
	return zoom == target
}

// enrichDesignContext adds design patterns, anti-patterns, and code smells using
// two complementary queries:
// 1. DesignContextForInvariants — finds patterns linked to invariants collected
//    via traverseRelated (works when the node has invariant edges).
// 2. DesignContextForNode — finds patterns that directly link to this node via
//    implements/exhibits/touches_file edges (works for source files and services
//    even without invariant edges).
func (nc *NodeContext) enrichDesignContext(ctx context.Context, g *graph.Graph) {
	applyDC := func(dc *graph.DesignContext) {
		if dc == nil {
			return
		}
		for _, p := range dc.DesignPatterns {
			nc.DesignPatterns = appendUniq(nc.DesignPatterns, p)
		}
		for _, p := range dc.AntiPatterns {
			nc.AntiPatterns = appendUniq(nc.AntiPatterns, p)
		}
		for _, s := range dc.CodeSmells {
			nc.CodeSmells = appendUniq(nc.CodeSmells, s)
		}
	}

	// Collect invariant node IDs from two sources:
	// 1. RelatedInvariants populated by traversal.
	// 2. The starting node itself, if it IS an invariant (e.g. --invariant flag).
	invNodeIDSet := make(map[string]bool)
	for _, inv := range nc.RelatedInvariants {
		invNodeIDSet["invariant:"+inv.ID] = true
	}
	if strings.HasPrefix(nc.NodeID, "invariant:") {
		invNodeIDSet[nc.NodeID] = true
	}

	if len(invNodeIDSet) > 0 {
		invNodeIDs := make([]string, 0, len(invNodeIDSet))
		for id := range invNodeIDSet {
			invNodeIDs = append(invNodeIDs, id)
		}
		// NodeTypeDesignPattern / NodeTypeAntiPattern (design_patterns.yaml).
		if dc, err := g.DesignContextForInvariants(ctx, invNodeIDs); err == nil {
			applyDC(dc)
		}
		// NodeTypePattern code smells and pattern names (patterns.yaml).
		if smells, err := g.CodeSmellsForInvariants(ctx, invNodeIDs); err == nil {
			for _, s := range smells {
				nc.CodeSmells = appendUniq(nc.CodeSmells, s)
			}
		}
		if names, err := g.PatternNamesForInvariants(ctx, invNodeIDs); err == nil {
			for _, n := range names {
				nc.DesignPatterns = appendUniq(nc.DesignPatterns, n)
			}
		}
	}

	// Pass 2: direct pattern-to-node links (implements/exhibits/touches_file).
	if dc, err := g.DesignContextForNode(ctx, nc.NodeID); err == nil {
		applyDC(dc)
	}
}

func (nc *NodeContext) collectRuntimeEvidence(ctx context.Context, g *graph.Graph, nodeID string) {
	inEdges, err := g.Neighbors(ctx, nodeID, "in")
	if err != nil {
		return
	}
	for _, e := range inEdges {
		src, _ := g.FindNode(ctx, e.Src)
		if src == nil {
			continue
		}
		if isRuntimeNode(src.Type) && src.Summary != "" {
			nc.RuntimeEvidence = appendUniq(nc.RuntimeEvidence, src.Summary)
		}
	}
}

// --- helpers ---

func labelFromNodeType(t string) ConfidenceLabel {
	switch t {
	case graph.NodeTypeInvariant, graph.NodeTypeFailureMode, graph.NodeTypeForbiddenFix:
		return ConfidenceExplicit
	case graph.NodeTypeSymbol, graph.NodeTypeGoPackage, graph.NodeTypeProtoService,
		graph.NodeTypeRPCMethod, graph.NodeTypeSourceFile, graph.NodeTypeTest:
		return ConfidenceExtracted
	case graph.NodeTypeRuntimeSnapshot, graph.NodeTypeRuntimeServiceStatus,
		graph.NodeTypeWorkflowReceipt, graph.NodeTypeSystemdStatus, graph.NodeTypeDoctorEvidence:
		return ConfidenceRuntime
	case graph.NodeTypeIncident, graph.NodeTypeAwarenessProposal, graph.NodeTypeLearningRule:
		return ConfidenceLearned
	default:
		return ConfidenceInferred
	}
}

func isSafetyAnnotation(kind string) bool {
	switch kind {
	case graph.EdgeProtects, graph.EdgeEnforces, graph.EdgeForbids,
		graph.EdgeTestedBy, graph.EdgeAffects, graph.EdgeBlocks,
		graph.EdgeSafeWhen, graph.EdgeUnsafeWhen, graph.EdgeRequiresTest:
		return true
	}
	return false
}

func isRuntimeNode(t string) bool {
	switch t {
	case graph.NodeTypeRuntimeSnapshot, graph.NodeTypeRuntimeServiceStatus,
		graph.NodeTypeWorkflowReceipt, graph.NodeTypeStateDelta,
		graph.NodeTypeDoctorEvidence, graph.NodeTypeSystemdStatus:
		return true
	}
	return false
}

func toEdgeSummary(e graph.Edge, targetID string, target *graph.Node) EdgeSummary {
	es := EdgeSummary{
		Kind:       e.Kind,
		TargetID:   targetID,
		Confidence: e.Confidence,
		Required:   e.Required,
	}
	if target != nil {
		es.TargetName = target.Name
		es.TargetType = target.Type
	}
	return es
}

func toEdgeSummaryWithProvenance(e graph.Edge, targetID string, target *graph.Node) EdgeSummary {
	es := toEdgeSummary(e, targetID, target)
	es.Provenance = labelFromEdgeMeta(e.Metadata)
	return es
}

// labelFromEdgeMeta infers a ConfidenceLabel from edge metadata.
func labelFromEdgeMeta(meta map[string]any) ConfidenceLabel {
	if meta == nil {
		return ConfidenceInferred
	}
	if v, ok := meta["source_kind"]; ok {
		switch v {
		case "documentation":
			return ConfidenceLabel("documentation")
		case "manual":
			return ConfidenceLabel("manual")
		case "runtime":
			return ConfidenceRuntime
		case "learned":
			return ConfidenceLearned
		}
	}
	if b, ok := meta["explicit"]; ok && b == true {
		return ConfidenceExplicit
	}
	extractor, _ := meta["extractor"].(string)
	switch extractor {
	case "goast", "proto", "workflows", "packages", "tests":
		return ConfidenceExtracted
	case "docs":
		return ConfidenceLabel("documentation")
	}
	return ConfidenceInferred
}

func nodeNameOrID(n *graph.Node, fallback string) string {
	if n != nil && n.Name != "" {
		return n.Name
	}
	return fallback
}

func buildEditWarnings(node *graph.Node, nc *NodeContext) []string {
	var w []string
	if len(nc.RelatedInvariants) > 0 {
		w = append(w, fmt.Sprintf("Governed by %d invariant(s) — read them before editing.", len(nc.RelatedInvariants)))
	}
	if len(nc.ForbiddenFixes) > 0 {
		w = append(w, fmt.Sprintf("%d forbidden fix pattern(s) recorded for this node — do not apply them.", len(nc.ForbiddenFixes)))
	}
	switch node.Type {
	case graph.NodeTypeGlobularService:
		w = append(w, "Service nodes: all config changes flow through workflows — no inline state mutations.")
	case graph.NodeTypeEtcdKey:
		w = append(w, "etcd key node: ensure all writes are transactional and idempotent.")
	case graph.NodeTypeWorkflow:
		w = append(w, "Workflow nodes must reach a terminal state (SUCCEEDED or FAILED).")
	case graph.NodeTypeSystemdUnit:
		w = append(w, "systemd unit: ExecStartPre must include orphan-kill guard (pkill -9 -x <binary>) as the FIRST line. Use -x (process-name match), never -f — -f matches the parent shell's argv and self-SIGKILLs.")
	}
	if len(nc.StateWrites) > 0 {
		w = append(w, fmt.Sprintf("Writes to %d state location(s) — verify idempotency.", len(nc.StateWrites)))
	}
	return w
}

func buildRecommendedSearches(node *graph.Node, nc *NodeContext) []string {
	var s []string
	s = append(s, "grep -r '"+node.Name+"' golang/")
	if node.Path != "" {
		s = append(s, "globular awareness impact --file "+node.Path)
	}
	if nc.Service != "" {
		s = append(s, "globular awareness agent-context --task 'editing "+nc.Service+"'")
	}
	for _, inv := range nc.RelatedInvariants {
		if inv.ID != "" {
			s = append(s, "invariant: "+inv.ID)
			break
		}
	}
	return s
}

// findInvariantByNode looks up an invariant for a node, trying node.ID then node.Name.
// Node IDs may carry prefixes (e.g. "inv:foo") while the invariants table uses bare IDs.
func findInvariantByNode(ctx context.Context, g *graph.Graph, n *graph.Node) *graph.Invariant {
	if n == nil {
		return nil
	}
	inv, _ := g.FindInvariant(ctx, n.ID)
	if inv != nil {
		return inv
	}
	inv, _ = g.FindInvariant(ctx, n.Name)
	return inv
}

// --- collection helpers ---

func appendUniq(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func appendInvariant(s []graph.Invariant, v graph.Invariant) []graph.Invariant {
	for _, x := range s {
		if x.ID == v.ID {
			return s
		}
	}
	return append(s, v)
}

func appendFailureMode(s []graph.FailureMode, v graph.FailureMode) []graph.FailureMode {
	for _, x := range s {
		if x.ID == v.ID {
			return s
		}
	}
	return append(s, v)
}

// --- cap helpers ---

func capEdges(s []EdgeSummary, max int, key string, trunc map[string]int) []EdgeSummary {
	if len(s) > max {
		trunc[key] = len(s) - max
		return s[:max]
	}
	return s
}

func capStrings(s []string, max int, key string, trunc map[string]int) []string {
	if len(s) > max {
		trunc[key] = len(s) - max
		return s[:max]
	}
	return s
}

func capInvariants(s []graph.Invariant, max int, key string, trunc map[string]int) []graph.Invariant {
	if len(s) > max {
		trunc[key] = len(s) - max
		return s[:max]
	}
	return s
}

func capFailureModes(s []graph.FailureMode, max int, key string, trunc map[string]int) []graph.FailureMode {
	if len(s) > max {
		trunc[key] = len(s) - max
		return s[:max]
	}
	return s
}

// collectAncestors traverses incoming structural edges (defines, owns, imports)
// upward from startID up to maxHops, returning the IDs of all ancestor nodes.
// This allows a symbol node to discover its parent file → service → package chain.
func collectAncestors(ctx context.Context, g *graph.Graph, startID string, maxHops int) []string {
	structuralKinds := map[string]bool{
		graph.EdgeDefines: true,
		graph.EdgeOwns:    true,
		graph.EdgeImports: true,
	}
	visited := map[string]bool{startID: true}
	var result []string
	queue := []string{startID}
	for hop := 0; hop < maxHops && len(queue) > 0; hop++ {
		var next []string
		for _, id := range queue {
			inEdges, _ := g.Neighbors(ctx, id, "in")
			for _, e := range inEdges {
				if !structuralKinds[e.Kind] {
					continue
				}
				if visited[e.Src] {
					continue
				}
				visited[e.Src] = true
				result = append(result, e.Src)
				next = append(next, e.Src)
			}
		}
		queue = next
	}
	return result
}

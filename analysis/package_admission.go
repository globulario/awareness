package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/awareness/extractors/packages"
	"github.com/globulario/awareness/graph"
)

// AdmissionStatus is the outcome of package admission validation.
type AdmissionStatus string

const (
	AdmissionAdmit AdmissionStatus = "ADMIT"
	AdmissionWarn  AdmissionStatus = "WARN"
	AdmissionBlock AdmissionStatus = "BLOCK"
)

// AdmissionReason captures a single validation finding.
type AdmissionReason struct {
	Rule    int    // rule number from the spec (1–10)
	Status  AdmissionStatus
	Message string
}

// AdmissionResult is the complete validation output for a package contract.
type AdmissionResult struct {
	Status AdmissionStatus

	// Human-readable reasons for each finding.
	Reasons []AdmissionReason

	// Graph nodes and edges the contract would add (preview only).
	GraphNodesAddedPreview []graph.Node
	GraphEdgesAddedPreview []graph.Edge

	// Structured findings.
	ImpactedInvariants  []string
	DependencyCycles    []Cycle
	ForbiddenFixesFound []string
	RequiredTests       []string
	MissingTests        []string
	RequiredWorkflows   []string
	MissingWorkflows    []string
}

// worstStatus returns the more severe of two statuses.
func worstStatus(a, b AdmissionStatus) AdmissionStatus {
	if a == AdmissionBlock || b == AdmissionBlock {
		return AdmissionBlock
	}
	if a == AdmissionWarn || b == AdmissionWarn {
		return AdmissionWarn
	}
	return AdmissionAdmit
}

// ValidatePackage validates a package awareness contract against the main graph.
//
// It does NOT write to the main graph. The main graph is used read-only for context
// (existing invariants, protected keys, workflow nodes, test nodes).
//
// If contract is nil (no awareness.yaml found), the kind from packageKind drives
// the blocking / warning decision.
func ValidatePackage(ctx context.Context, contract *packages.AwarenessContract, packageKind string, mainGraph *graph.Graph) (*AdmissionResult, error) {
	r := &AdmissionResult{Status: AdmissionAdmit}

	// Rule 1/2/3 — awareness.yaml presence check.
	if err := checkKindRequirement(r, contract, packageKind); err != nil {
		return nil, err
	}
	// If no contract, remaining rules cannot run.
	if contract == nil {
		return r, nil
	}

	// Preview what would be added to the graph.
	nodes, edges := packages.ContractGraphPreview(contract)
	r.GraphNodesAddedPreview = nodes
	r.GraphEdgesAddedPreview = edges

	// Rule 4 — every required dependency must declare a phase.
	checkDependencyPhases(r, contract)

	// Rule 5 — required dependency cycles in dangerous phases.
	if err := checkDependencyCycles(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: cycle check: %w", err)
	}

	// Rule 6 — package must not declare remediation workflows that are globally forbidden.
	if err := checkForbiddenFixes(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: forbidden-fix check: %w", err)
	}

	// Rule 7 — package must not write protected etcd keys without declaring the invariant.
	if err := checkProtectedKeyWrites(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: protected-key check: %w", err)
	}

	// Rule 8 — invariant impacts.
	if err := collectImpactedInvariants(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: invariant impact: %w", err)
	}

	// Rule 9 — remediation workflows must exist in the graph.
	if err := checkRemediationWorkflows(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: workflow check: %w", err)
	}

	// Rule 10 — required tests should be in the graph.
	if err := checkRequiredTests(ctx, r, contract, mainGraph); err != nil {
		return nil, fmt.Errorf("ValidatePackage: test check: %w", err)
	}

	return r, nil
}

// ---- individual rule checkers ----

// checkKindRequirement implements rules 1, 2, 3.
func checkKindRequirement(r *AdmissionResult, contract *packages.AwarenessContract, packageKind string) error {
	if packageKind == "" && contract != nil {
		packageKind = contract.PackageKind
	}

	if packages.RequiresAwarenessContract(packageKind) {
		if contract == nil {
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:    1,
				Status:  AdmissionBlock,
				Message: fmt.Sprintf("package kind %q requires awareness.yaml but none was found", packageKind),
			})
			r.Status = AdmissionBlock
		}
		return nil
	}

	// APPLICATION / AGENT / COMMAND — warn if missing.
	if contract == nil {
		r.Reasons = append(r.Reasons, AdmissionReason{
			Rule:    3,
			Status:  AdmissionWarn,
			Message: fmt.Sprintf("package kind %q may omit awareness.yaml, but providing one is recommended", packageKind),
		})
		r.Status = worstStatus(r.Status, AdmissionWarn)
	}
	return nil
}

// checkDependencyPhases implements rule 4.
func checkDependencyPhases(r *AdmissionResult, contract *packages.AwarenessContract) {
	for _, dep := range contract.DependsOn {
		if dep.Required && dep.Phase == "" {
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   4,
				Status: AdmissionBlock,
				Message: fmt.Sprintf("required dependency on %q has no phase declared; "+
					"required dependencies must specify the phase they are needed in", dep.Service),
			})
			r.Status = AdmissionBlock
		}
	}
}

// checkDependencyCycles implements rule 5.
// It merges the contract's proposed depends_on edges with existing edges from the main
// graph, then detects cycles. Required cycles in dangerous phases → BLOCK.
func checkDependencyCycles(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	// Build a temporary in-memory graph for cycle analysis.
	tmp, err := graph.OpenMemory()
	if err != nil {
		return err
	}
	defer tmp.Close()

	// Copy existing depends_on edges into the temp graph.
	existing, err := mainGraph.EdgesByKind(ctx, graph.EdgeDependsOn)
	if err != nil {
		return err
	}
	for _, e := range existing {
		_ = tmp.AddNode(ctx, graph.Node{ID: e.Src, Type: graph.NodeTypeGlobularService, Name: e.Src})
		_ = tmp.AddNode(ctx, graph.Node{ID: e.Dst, Type: graph.NodeTypeGlobularService, Name: e.Dst})
		_ = tmp.AddEdge(ctx, e)
	}

	// Add the proposed edges from the contract.
	svcID := "service:" + contract.Service
	_ = tmp.AddNode(ctx, graph.Node{ID: svcID, Type: graph.NodeTypeGlobularService, Name: contract.Service})
	for _, dep := range contract.DependsOn {
		dstID := "service:" + dep.Service
		_ = tmp.AddNode(ctx, graph.Node{ID: dstID, Type: graph.NodeTypeGlobularService, Name: dep.Service})
		_ = tmp.AddEdge(ctx, graph.Edge{
			Src:      svcID,
			Kind:     graph.EdgeDependsOn,
			Dst:      dstID,
			Phase:    dep.Phase,
			Required: dep.Required,
		})
	}

	// Detect cycles with no phase filter so we catch all phases.
	cycles, err := FindCycles(ctx, tmp, "")
	if err != nil {
		return err
	}

	r.DependencyCycles = cycles

	for _, c := range cycles {
		switch c.Classification {
		case CycleDangerous:
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   5,
				Status: AdmissionBlock,
				Message: fmt.Sprintf("required dependency cycle detected during phase %q: %s — %s",
					c.Phase, cyclePathString(c.Path), c.Reason),
			})
			r.Status = AdmissionBlock

		case CycleWarning:
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   5,
				Status: AdmissionWarn,
				Message: fmt.Sprintf("dependency cycle detected: %s — %s",
					cyclePathString(c.Path), c.Reason),
			})
			r.Status = worstStatus(r.Status, AdmissionWarn)
		}
	}

	return nil
}

// checkForbiddenFixes implements rule 6.
// A package must not reference (via remediation_workflows) a workflow that is
// registered as a globally forbidden fix in the main graph.
func checkForbiddenFixes(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	if len(contract.RemediationWorkflows) == 0 {
		return nil
	}

	for _, wfID := range contract.RemediationWorkflows {
		// Check whether this workflow ID exists as a forbidden_fix node.
		fixNode, err := mainGraph.FindNode(ctx, "forbidden_fix:"+wfID)
		if err != nil {
			return err
		}
		if fixNode != nil {
			r.ForbiddenFixesFound = append(r.ForbiddenFixesFound, wfID)
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   6,
				Status: AdmissionBlock,
				Message: fmt.Sprintf("remediation_workflow %q is registered as a forbidden fix in the global graph; "+
					"this operation is not allowed", wfID),
			})
			r.Status = AdmissionBlock
		}
	}
	return nil
}

// checkProtectedKeyWrites implements rule 7.
// A package must not write a protected etcd key unless it declares the protecting invariant.
func checkProtectedKeyWrites(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	if contract.Admission.AllowPrivilegedStateWrites {
		return nil
	}

	declaredInvariants := make(map[string]bool, len(contract.Invariants))
	for _, inv := range contract.Invariants {
		declaredInvariants[inv] = true
	}

	for _, key := range contract.AllWrittenEtcdKeys() {
		keyNodeID := "etcd_key:" + key

		// Find invariants that protect this key (invariant→protects→etcd_key).
		inEdges, err := mainGraph.Neighbors(ctx, keyNodeID, "in")
		if err != nil {
			return err
		}

		for _, e := range inEdges {
			if e.Kind != graph.EdgeProtects {
				continue
			}
			// e.Src is an invariant node.
			invID := strings.TrimPrefix(e.Src, "invariant:")
			if !declaredInvariants[invID] {
				r.Reasons = append(r.Reasons, AdmissionReason{
					Rule:   7,
					Status: AdmissionBlock,
					Message: fmt.Sprintf("package writes protected etcd key %q which is protected by invariant %q, "+
						"but that invariant is not declared in the contract's invariants list", key, invID),
				})
				r.Status = AdmissionBlock
			}
		}
	}
	return nil
}

// collectImpactedInvariants finds invariants touched by this package (rule 8 informational).
func collectImpactedInvariants(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	seen := make(map[string]bool)

	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			r.ImpactedInvariants = append(r.ImpactedInvariants, id)
		}
	}

	// Declared invariants.
	for _, inv := range contract.Invariants {
		add(inv)
	}

	// Invariants protecting the keys this package writes.
	for _, key := range contract.AllWrittenEtcdKeys() {
		inEdges, err := mainGraph.Neighbors(ctx, "etcd_key:"+key, "in")
		if err != nil {
			return err
		}
		for _, e := range inEdges {
			if e.Kind == graph.EdgeProtects && strings.HasPrefix(e.Src, "invariant:") {
				add(strings.TrimPrefix(e.Src, "invariant:"))
			}
		}
	}

	return nil
}

// checkRemediationWorkflows implements rule 9.
func checkRemediationWorkflows(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	for _, wfID := range contract.RemediationWorkflows {
		r.RequiredWorkflows = append(r.RequiredWorkflows, wfID)

		// A workflow must exist as a workflow node in the graph.
		node, err := mainGraph.FindNode(ctx, "workflow:"+wfID)
		if err != nil {
			return err
		}
		if node == nil {
			r.MissingWorkflows = append(r.MissingWorkflows, wfID)
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   9,
				Status: AdmissionWarn,
				Message: fmt.Sprintf("remediation workflow %q is not present in the awareness graph; "+
					"add it via a workflow YAML or build the graph first", wfID),
			})
			r.Status = worstStatus(r.Status, AdmissionWarn)
		}
	}
	return nil
}

// checkRequiredTests implements rule 10.
func checkRequiredTests(ctx context.Context, r *AdmissionResult, contract *packages.AwarenessContract, mainGraph *graph.Graph) error {
	for _, testName := range contract.RequiredTests {
		r.RequiredTests = append(r.RequiredTests, testName)

		node, err := mainGraph.FindNode(ctx, "test:"+testName)
		if err != nil {
			return err
		}
		if node == nil {
			r.MissingTests = append(r.MissingTests, testName)
			r.Reasons = append(r.Reasons, AdmissionReason{
				Rule:   10,
				Status: AdmissionWarn,
				Message: fmt.Sprintf("required test %q is not found in the awareness graph; "+
					"run the test extractor or add the test to the source", testName),
			})
			r.Status = worstStatus(r.Status, AdmissionWarn)
		}
	}
	return nil
}

// ---- helpers ----

func cyclePathString(path []string) string {
	// Strip "service:" prefix for readability.
	names := make([]string, len(path))
	for i, p := range path {
		names[i] = strings.TrimPrefix(p, "service:")
	}
	return strings.Join(names, " → ")
}

// RenderAdmissionMarkdown produces a human-readable admission report.
func RenderAdmissionMarkdown(contract *packages.AwarenessContract, result *AdmissionResult) string {
	var sb strings.Builder

	pkg := "<unknown>"
	kind := "<unknown>"
	if contract != nil {
		pkg = contract.Package
		kind = contract.PackageKind
	}

	sb.WriteString("# Package Admission Report\n\n")
	sb.WriteString(fmt.Sprintf("**Package**: %s\n", pkg))
	sb.WriteString(fmt.Sprintf("**Kind**: %s\n", kind))
	sb.WriteString(fmt.Sprintf("**Status**: %s\n\n", result.Status))

	if len(result.Reasons) > 0 {
		sb.WriteString("## Findings\n")
		for _, reason := range result.Reasons {
			sb.WriteString(fmt.Sprintf("- [Rule %d] [%s] %s\n", reason.Rule, reason.Status, reason.Message))
		}
		sb.WriteString("\n")
	}

	if len(result.ImpactedInvariants) > 0 {
		sb.WriteString("## Impacted invariants\n")
		for _, inv := range result.ImpactedInvariants {
			sb.WriteString("- " + inv + "\n")
		}
		sb.WriteString("\n")
	}

	if len(result.DependencyCycles) > 0 {
		sb.WriteString("## Dependency cycles\n")
		for _, c := range result.DependencyCycles {
			sb.WriteString(fmt.Sprintf("- [%s] %s (phase=%s)\n",
				c.Classification, cyclePathString(c.Path), c.Phase))
		}
		sb.WriteString("\n")
	}

	if len(result.ForbiddenFixesFound) > 0 {
		sb.WriteString("## Forbidden fixes\n")
		for _, f := range result.ForbiddenFixesFound {
			sb.WriteString("- " + f + "\n")
		}
		sb.WriteString("\n")
	}

	if len(result.MissingTests) > 0 {
		sb.WriteString("## Missing required tests\n")
		for _, t := range result.MissingTests {
			sb.WriteString("- " + t + "\n")
		}
		sb.WriteString("\n")
	}

	if len(result.MissingWorkflows) > 0 {
		sb.WriteString("## Missing remediation workflows\n")
		for _, w := range result.MissingWorkflows {
			sb.WriteString("- " + w + "\n")
		}
		sb.WriteString("\n")
	}

	if len(result.GraphEdgesAddedPreview) > 0 {
		sb.WriteString(fmt.Sprintf("## Graph preview\n"))
		sb.WriteString(fmt.Sprintf("Would add %d nodes and %d edges.\n",
			len(result.GraphNodesAddedPreview), len(result.GraphEdgesAddedPreview)))
	}

	return sb.String()
}

// RequiresAwarenessContract is re-exported so CLI and tests can call it without
// importing the packages sub-package.
func RequiresAwarenessContractForKind(kind string) bool {
	return packages.RequiresAwarenessContract(kind)
}

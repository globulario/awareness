package analysis

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/globulario/awareness/graph"
)

// AgentContextHints provides optional file/symbol/service hints to narrow matching.
type AgentContextHints struct {
	Files    []string
	Symbols  []string
	Services []string
}

// AgentContextResult holds the structured output (parallel to the Markdown).
type AgentContextResult struct {
	InvariantIDs     []string
	FailureModeIDs   []string
	ForbiddenFixes   []string
	RequiredTests    []string
	RequiredSearches []string
	ServiceNames     []string
}

// AgentContextAliases carries an optional pre-loaded alias map for agent context
// generation. Pass a non-nil map to enable alias-based matching (Task 3).
// Load from docs/awareness/context_aliases.yaml via learning.LoadContextAliases.
type AgentContextAliases map[string][]string

// GenerateAgentContext produces a Markdown context document for AI agents.
// Matching is deterministic keyword-based graph traversal — no LLM calls.
// If aliases is non-nil, task language is also matched against the alias map
// so natural phrases like "restart storm" surface the right invariants.
func GenerateAgentContext(ctx context.Context, g *graph.Graph, task string, hints AgentContextHints, aliases ...AgentContextAliases) (string, AgentContextResult, error) {
	keywords := extractKeywords(task)

	// Collect all invariants and failure modes.
	allInvs, err := g.AllInvariants(ctx)
	if err != nil {
		return "", AgentContextResult{}, fmt.Errorf("GenerateAgentContext: %w", err)
	}
	allFMs, err := g.AllFailureModes(ctx)
	if err != nil {
		return "", AgentContextResult{}, fmt.Errorf("GenerateAgentContext: %w", err)
	}

	// Match invariants by keyword.
	matchedInvs := matchInvariants(allInvs, keywords)
	// Match failure modes by keyword.
	matchedFMs := matchFailureModes(allFMs, keywords)

	// If aliases are provided, expand matched invariants/failure modes/services via alias matching.
	if len(aliases) > 0 && aliases[0] != nil {
		var extraFMs []*graph.FailureMode
		var extraServices []string
		matchedInvs, extraFMs, extraServices = expandByAliases(ctx, g, task, aliases[0], allInvs, allFMs, matchedInvs, matchedFMs)
		// Merge extra failure modes and services from alias expansion.
		fmSet := make(map[string]bool)
		for _, fm := range matchedFMs {
			fmSet[fm.ID] = true
		}
		for _, fm := range extraFMs {
			if !fmSet[fm.ID] {
				matchedFMs = append(matchedFMs, fm)
				fmSet[fm.ID] = true
			}
		}
		_ = extraServices // services are collected later via collectServices
	}

	// Expand: for each matched invariant, also include failure modes that violate it.
	matchedFMs = expandFailureModesFromInvariants(ctx, g, matchedInvs, matchedFMs)
	// Expand: for each matched failure mode, also include its invariants.
	matchedInvs = expandInvariantsFromFailureModes(ctx, g, matchedFMs, matchedInvs)

	// Collect forbidden fixes from matched invariants and failure modes.
	forbiddenFixes := collectForbiddenFixes(ctx, g, matchedInvs, matchedFMs)

	// Collect required tests.
	requiredTests := collectRequiredTests(ctx, g, matchedInvs, matchedFMs)

	// Collect services from hints + matched invariants/failure modes.
	services := collectServices(ctx, g, hints, matchedInvs, matchedFMs, keywords)

	// Required searches from invariants and task keywords.
	searches := collectSearches(matchedInvs, matchedFMs, keywords)

	result := AgentContextResult{
		InvariantIDs:     nodeNames(matchedInvs),
		FailureModeIDs:   fmIDs(matchedFMs),
		ForbiddenFixes:   forbiddenFixes,
		RequiredTests:    requiredTests,
		RequiredSearches: searches,
		ServiceNames:     services,
	}

	md := renderMarkdown(task, result)
	return md, result, nil
}

// ---- alias target classification ----

// aliasTargetKind classifies the kind of graph node a (possibly-prefixed) alias
// target ID refers to. Returns one of "invariant", "failure_mode", "service",
// or "invariant" as the default for bare IDs (backward compat).
// This is a local copy kept here to avoid an import cycle with the learning package.
func aliasTargetKind(targetID string) (kind, bareID string) {
	for _, prefix := range []string{"invariant:", "failure_mode:", "service:"} {
		if strings.HasPrefix(targetID, prefix) {
			return strings.TrimSuffix(prefix, ":"), strings.TrimPrefix(targetID, prefix)
		}
	}
	// Bare ID — backward compat: treat as invariant.
	return "invariant", targetID
}

// ---- alias-based expansion ----

// expandByAliases adds invariants, failure modes, and services to the matched
// sets based on alias targets surfaced by the task language. Alias matching is
// case-insensitive substring matching against the full task string.
//
// Target keys support optional type prefixes:
//
//	"invariant:foo"    → added to invariant set
//	"failure_mode:foo" → added to failure mode set
//	"service:foo"      → returned in extraServices
//	bare "foo"         → try invariant first, then failure_mode
//
// Returns updated invariants, any extra failure modes, and any extra service names.
func expandByAliases(ctx context.Context, g *graph.Graph, task string, aliases AgentContextAliases, allInvs []*graph.Invariant, allFMs []*graph.FailureMode, existingInvs []*graph.Invariant, existingFMs []*graph.FailureMode) ([]*graph.Invariant, []*graph.FailureMode, []string) {
	lower := strings.ToLower(task)

	existingInvIDs := make(map[string]bool)
	for _, inv := range existingInvs {
		existingInvIDs[inv.ID] = true
	}
	existingFMIDs := make(map[string]bool)
	for _, fm := range existingFMs {
		existingFMIDs[fm.ID] = true
	}

	var extraServices []string
	seenServices := make(map[string]bool)

	for targetID, phrases := range aliases {
		// Check if any phrase matches the task.
		matched := false
		for _, phrase := range phrases {
			if strings.Contains(lower, strings.ToLower(phrase)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Classify the target by prefix.
		kind, bareID := aliasTargetKind(targetID)

		switch kind {
		case "service":
			if !seenServices[bareID] {
				extraServices = append(extraServices, bareID)
				seenServices[bareID] = true
			}

		case "failure_mode":
			if existingFMIDs[bareID] {
				continue
			}
			for _, fm := range allFMs {
				if fm.ID == bareID {
					existingFMs = append(existingFMs, fm)
					existingFMIDs[bareID] = true
					break
				}
			}
			// If not in failure modes table, try graph node.
			if !existingFMIDs[bareID] {
				node, err := g.FindNode(ctx, graph.FailureModeNodeID(bareID))
				if err == nil && node != nil {
					existingFMs = append(existingFMs, &graph.FailureMode{
						ID:    bareID,
						Title: node.Name,
					})
					existingFMIDs[bareID] = true
				}
			}

		default: // "invariant" — covers both bare IDs and explicit "invariant:" prefix
			if existingInvIDs[bareID] {
				continue
			}
			// Try invariants first.
			foundInv := false
			for _, inv := range allInvs {
				if inv.ID == bareID {
					existingInvs = append(existingInvs, inv)
					existingInvIDs[bareID] = true
					foundInv = true
					break
				}
			}
			if !foundInv {
				// Try graph node for invariant.
				node, err := g.FindNode(ctx, "invariant:"+bareID)
				if err == nil && node != nil {
					existingInvs = append(existingInvs, &graph.Invariant{
						ID:    bareID,
						Title: node.Name,
					})
					existingInvIDs[bareID] = true
					foundInv = true
				}
			}
			// For bare IDs: if not found as invariant, try failure_mode.
			if !foundInv && kind == "invariant" && !strings.HasPrefix(targetID, "invariant:") {
				if !existingFMIDs[bareID] {
					for _, fm := range allFMs {
						if fm.ID == bareID {
							existingFMs = append(existingFMs, fm)
							existingFMIDs[bareID] = true
							break
						}
					}
				}
			}
		}
	}
	return existingInvs, existingFMs, extraServices
}

// ---- matching helpers ----

func extractKeywords(task string) []string {
	words := strings.FieldsFunc(strings.ToLower(task), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.'
	})
	// Deduplicate and filter short/common words.
	seen := make(map[string]bool)
	var out []string
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "in": true, "on": true,
		"at": true, "is": true, "it": true, "to": true, "of": true,
		"for": true, "and": true, "or": true, "fix": true, "that": true,
		"with": true, "when": true, "how": true, "why": true,
	}
	for _, w := range words {
		if len(w) < 3 || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

func matchInvariants(invs []*graph.Invariant, keywords []string) []*graph.Invariant {
	var matched []*graph.Invariant
	for _, inv := range invs {
		haystack := strings.ToLower(inv.ID + " " + inv.Title + " " + inv.Summary)
		if matchesAny(haystack, keywords) {
			matched = append(matched, inv)
		}
	}
	return matched
}

func matchFailureModes(fms []*graph.FailureMode, keywords []string) []*graph.FailureMode {
	var matched []*graph.FailureMode
	for _, fm := range fms {
		haystack := strings.ToLower(fm.ID + " " + fm.Title + " " + fm.RootCause + " " +
			strings.Join(fm.Symptoms, " "))
		if matchesAny(haystack, keywords) {
			matched = append(matched, fm)
		}
	}
	return matched
}

func matchesAny(haystack string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(haystack, kw) {
			return true
		}
	}
	return false
}

func expandFailureModesFromInvariants(ctx context.Context, g *graph.Graph, invs []*graph.Invariant, existing []*graph.FailureMode) []*graph.FailureMode {
	existingIDs := make(map[string]bool)
	for _, fm := range existing {
		existingIDs[fm.ID] = true
	}

	for _, inv := range invs {
		invNodeID := "invariant:" + inv.ID
		// Find failure_mode nodes that violate this invariant.
		edges, err := g.Neighbors(ctx, invNodeID, "in")
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.Kind != graph.EdgeViolates {
				continue
			}
			fmID := graph.FailureModeIDFromNode(e.Src)
			if existingIDs[fmID] {
				continue
			}
			allFMs, err := g.AllFailureModes(ctx)
			if err != nil {
				continue
			}
			for _, fm := range allFMs {
				if fm.ID == fmID {
					existing = append(existing, fm)
					existingIDs[fmID] = true
					break
				}
			}
		}
		// Also check invariant→affects→failure_mode edges.
		outEdges, err := g.Neighbors(ctx, invNodeID, "out")
		if err != nil {
			continue
		}
		for _, e := range outEdges {
			if e.Kind != graph.EdgeAffects {
				continue
			}
			if !graph.IsFailureModeNode(e.Dst) {
				continue
			}
			fmID := graph.FailureModeIDFromNode(e.Dst)
			if existingIDs[fmID] {
				continue
			}
			allFMs, err := g.AllFailureModes(ctx)
			if err != nil {
				continue
			}
			for _, fm := range allFMs {
				if fm.ID == fmID {
					existing = append(existing, fm)
					existingIDs[fmID] = true
					break
				}
			}
		}
	}
	return existing
}

func expandInvariantsFromFailureModes(ctx context.Context, g *graph.Graph, fms []*graph.FailureMode, existing []*graph.Invariant) []*graph.Invariant {
	existingIDs := make(map[string]bool)
	for _, inv := range existing {
		existingIDs[inv.ID] = true
	}
	for _, fm := range fms {
		fmNodeID := graph.FailureModeNodeID(fm.ID)
		edges, err := g.Neighbors(ctx, fmNodeID, "out")
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.Kind != graph.EdgeViolates {
				continue
			}
			invID := strings.TrimPrefix(e.Dst, "invariant:")
			if existingIDs[invID] {
				continue
			}
			allInvs, err := g.AllInvariants(ctx)
			if err != nil {
				continue
			}
			for _, inv := range allInvs {
				if inv.ID == invID {
					existing = append(existing, inv)
					existingIDs[invID] = true
					break
				}
			}
		}
	}
	return existing
}

func collectForbiddenFixes(ctx context.Context, g *graph.Graph, invs []*graph.Invariant, fms []*graph.FailureMode) []string {
	seen := make(map[string]bool)
	var fixes []string

	addFixes := func(nodeID string) {
		edges, err := g.Neighbors(ctx, nodeID, "out")
		if err != nil {
			return
		}
		for _, e := range edges {
			if e.Kind != graph.EdgeForbids {
				continue
			}
			name := strings.TrimPrefix(e.Dst, "forbidden_fix:")
			if !seen[name] {
				seen[name] = true
				fixes = append(fixes, name)
			}
		}
	}

	for _, inv := range invs {
		addFixes("invariant:" + inv.ID)
	}
	for _, fm := range fms {
		addFixes(graph.FailureModeNodeID(fm.ID))
	}
	return fixes
}

func collectRequiredTests(ctx context.Context, g *graph.Graph, invs []*graph.Invariant, fms []*graph.FailureMode) []string {
	seen := make(map[string]bool)
	var tests []string

	addTests := func(nodeID string) {
		edges, err := g.Neighbors(ctx, nodeID, "out")
		if err != nil {
			return
		}
		for _, e := range edges {
			if e.Kind != graph.EdgeTestedBy {
				continue
			}
			name := strings.TrimPrefix(e.Dst, "test:")
			if !seen[name] {
				seen[name] = true
				tests = append(tests, name)
			}
		}
	}

	for _, inv := range invs {
		addTests("invariant:" + inv.ID)
	}
	for _, fm := range fms {
		addTests(graph.FailureModeNodeID(fm.ID))
	}
	return tests
}

func collectServices(ctx context.Context, g *graph.Graph, hints AgentContextHints, invs []*graph.Invariant, fms []*graph.FailureMode, keywords []string) []string {
	seen := make(map[string]bool)
	var services []string

	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			services = append(services, name)
		}
	}

	// From explicit hints.
	for _, svc := range hints.Services {
		add(svc)
	}

	// From file hints: look up source_file node, traverse to services.
	for _, file := range hints.Files {
		res, err := g.ImpactByFile(ctx, file)
		if err != nil {
			continue
		}
		for _, n := range res.Nodes {
			if n.Type == graph.NodeTypeGlobularService {
				add(n.Name)
			}
		}
	}

	// From failure mode related services.
	for _, fm := range fms {
		edges, err := g.Neighbors(ctx, graph.FailureModeNodeID(fm.ID), "out")
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.Kind != graph.EdgeAffects {
				continue
			}
			if n, err := g.FindNode(ctx, e.Dst); err == nil && n != nil && n.Type == graph.NodeTypeGlobularService {
				add(n.Name)
			}
		}
	}

	// From keyword matching against known service names.
	svcNodes, err := g.FindNodesByType(ctx, graph.NodeTypeGlobularService)
	if err == nil {
		for _, n := range svcNodes {
			if matchesAny(strings.ToLower(n.Name), keywords) {
				add(n.Name)
			}
		}
	}

	return services
}

func collectSearches(invs []*graph.Invariant, fms []*graph.FailureMode, keywords []string) []string {
	seen := make(map[string]bool)
	var searches []string

	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			searches = append(searches, s)
		}
	}

	// Add task keywords as searches.
	for _, kw := range keywords {
		if len(kw) > 4 {
			add(kw)
		}
	}

	// Add invariant IDs as searches.
	for _, inv := range invs {
		add(inv.ID)
	}

	// Common Globular search terms related to matched invariants.
	invIDStr := ""
	for _, inv := range invs {
		invIDStr += inv.ID + " "
	}
	searchHints := map[string]string{
		"install":    "ApplyPackageRelease InstalledBuildID ActionResult PENDING_SYNC BLOCKED",
		"retry":      "retry classification classifyFailure BLOCKED PENDING_SYNC",
		"atomic":     "commitInstallResult etcd.Txn atomicCommit",
		"minio":      "ObjectStoreDesiredState enforceMinioHeld nodeIPInPool",
		"objectstore": "ObjectStoreDesiredState enforceMinioHeld minio_runtime_render",
		"workflow":   "dispatchWorkflow WorkflowService backend health gate",
		"build_id":   "resolveDesiredBuildID writeDesiredState InstalledBuildID",
		"liveness":   "systemd_unit runtime IsRunning GetRuntimeStatus",
		"convergence": "reconcileService applyDesiredState classifyFailure",
		"repository": "ListArtifacts GetArtifactVersions resolveLatestBuildNumber",
	}
	for keyword, hints := range searchHints {
		if strings.Contains(strings.ToLower(invIDStr), keyword) {
			for _, h := range strings.Fields(hints) {
				add(h)
			}
		}
	}

	return searches
}

func nodeNames(invs []*graph.Invariant) []string {
	var ids []string
	for _, inv := range invs {
		ids = append(ids, inv.ID)
	}
	return ids
}

func fmIDs(fms []*graph.FailureMode) []string {
	var ids []string
	for _, fm := range fms {
		ids = append(ids, fm.ID)
	}
	return ids
}

// ---- Markdown renderer ----

func renderMarkdown(task string, r AgentContextResult) string {
	var sb strings.Builder

	sb.WriteString("# Globular Agent Context\n\n")

	sb.WriteString("## Task\n")
	sb.WriteString(task + "\n\n")

	if len(r.ServiceNames) > 0 {
		sb.WriteString("## Relevant services\n")
		for _, s := range r.ServiceNames {
			sb.WriteString("- " + s + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Relevant state model\n")
	sb.WriteString("- Artifact (Repository layer)\n")
	sb.WriteString("- Desired (Controller layer)\n")
	sb.WriteString("- Installed (Node Agent layer)\n")
	sb.WriteString("- Runtime (systemd / health layer)\n\n")

	if len(r.InvariantIDs) > 0 {
		sb.WriteString("## Relevant invariants\n")
		for _, id := range r.InvariantIDs {
			sb.WriteString("- " + id + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.FailureModeIDs) > 0 {
		sb.WriteString("## Known failure modes\n")
		for _, id := range r.FailureModeIDs {
			sb.WriteString("- " + id + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.ForbiddenFixes) > 0 {
		sb.WriteString("## Forbidden fixes\n")
		for _, f := range r.ForbiddenFixes {
			sb.WriteString("- " + f + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.RequiredTests) > 0 {
		sb.WriteString("## Required tests\n")
		for _, t := range r.RequiredTests {
			sb.WriteString("- " + t + "\n")
		}
		sb.WriteString("\n")
	}

	if len(r.RequiredSearches) > 0 {
		sb.WriteString("## Required searches\n")
		for _, s := range r.RequiredSearches {
			sb.WriteString("- " + s + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Architecture rule\n")
	sb.WriteString("Local completion is not global convergence.\n")
	sb.WriteString("Installed-state is not runtime liveness.\n")
	sb.WriteString("Desired build_id must not mutate after resolution.\n")
	sb.WriteString("The 4 layers are independent: Repository → Desired → Installed → Runtime.\n")

	return sb.String()
}

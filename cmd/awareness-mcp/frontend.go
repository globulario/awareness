package main

// Frontend awareness helpers for MCP tools.
//
// These functions back four MCP tools:
//   frontend_trace_component  — everything known about a component
//   frontend_explain_screen   — operator-truth summary for a route
//   frontend_plan_feature     — draft awareness contracts for a new feature
//   frontend_verify_change    — pre-commit check for changed TSX files

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/project"
)

// ─── frontend_trace_component ─────────────────────────────────────────────────

type frontendTraceResult struct {
	Project          string             `json:"project"`
	Component        string             `json:"component"`
	GraphAvailable   bool               `json:"graph_available"`
	Nodes            []graphNodeResult  `json:"nodes"`
	BackendCalls     []graphNodeResult  `json:"backend_calls"`
	StateAtoms       []graphNodeResult  `json:"state_atoms"`
	PermissionChecks []graphNodeResult  `json:"permission_checks"`
	Routes           []graphNodeResult  `json:"routes"`
	Tests            []graphNodeResult  `json:"tests"`
	Contracts        frontendContracts  `json:"contracts"`
	Warnings         []string           `json:"warnings"`
}

type frontendContracts struct {
	Invariants     []InvariantEntry    `json:"invariants"`
	FailureModes   []FailureModeEntry  `json:"failure_modes"`
	ForbiddenFixes []ForbiddenFixEntry `json:"forbidden_fixes"`
}

func frontendTraceComponent(prof *project.ProjectProfile, componentName string, includeContracts bool) frontendTraceResult {
	result := frontendTraceResult{
		Project:   prof.Name,
		Component: componentName,
	}

	// Try graph lookup first.
	if gf, err := loadGraphFile(prof.Graph.CacheDir, prof.Awareness.Root); err == nil {
		result.GraphAvailable = true
		nodeByID := make(map[string]graphNode, len(gf.Nodes))
		for _, n := range gf.Nodes {
			nodeByID[n.ID] = n
		}
		for _, n := range gf.Nodes {
			if !nodeMatchesComponent(n, componentName) {
				continue
			}
			result.Nodes = append(result.Nodes, graphNodeResult{
				ID: n.ID, Kind: n.Kind, Label: n.Label, Properties: n.Properties,
			})
			// Collect typed neighbors.
			for _, e := range gf.Edges {
				var neighborID, edgeKind string
				if e.From == n.ID {
					neighborID, edgeKind = e.To, e.Kind
				} else if e.To == n.ID {
					neighborID, edgeKind = e.From, "←"+e.Kind
				}
				if neighborID == "" {
					continue
				}
				nb, ok := nodeByID[neighborID]
				if !ok {
					continue
				}
				nbr := graphNodeResult{
					ID: nb.ID, Kind: nb.Kind, Label: nb.Label,
					Properties: nb.Properties, EdgeKind: edgeKind,
				}
				switch nb.Kind {
				case "frontend_backend_call":
					result.BackendCalls = append(result.BackendCalls, nbr)
				case "frontend_state_atom":
					result.StateAtoms = append(result.StateAtoms, nbr)
				case "frontend_permission_check":
					result.PermissionChecks = append(result.PermissionChecks, nbr)
				case "frontend_route":
					result.Routes = append(result.Routes, nbr)
				case "frontend_test":
					result.Tests = append(result.Tests, nbr)
				}
			}
		}
	} else {
		result.Warnings = append(result.Warnings, "graph not available — returning YAML fallback only")
	}

	// YAML fallback: always run regardless of graph availability.
	if includeContracts {
		query := componentName + " component ui status permission workflow"
		result.Contracts.Invariants = searchInvariants(loadInvariants(prof.Awareness.Invariants), query, 5)
		result.Contracts.FailureModes = searchFailureModes(loadFailureModes(prof.Awareness.FailureModes), query, 5)
		terms := knowledgeTerms(strings.ToLower(componentName) + " component ui")
		for _, ff := range loadForbiddenFixes(prof.Awareness.ForbiddenFixes) {
			blob := strings.ToLower(ff.ID + " " + ff.Title + " " + ff.Description + " " + strings.Join(ff.Tags, " "))
			if countMatches(blob, terms) > 0 {
				result.Contracts.ForbiddenFixes = append(result.Contracts.ForbiddenFixes, ff)
				if len(result.Contracts.ForbiddenFixes) >= 5 {
					break
				}
			}
		}
	}

	if len(result.Nodes) == 0 && !result.GraphAvailable {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("no graph nodes found for component %q — YAML contracts returned as fallback", componentName))
	}

	return result
}

func nodeMatchesComponent(n graphNode, name string) bool {
	if n.Kind != "frontend_component" {
		return false
	}
	lower := strings.ToLower(name)
	return strings.ToLower(n.Label) == lower ||
		strings.Contains(strings.ToLower(n.ID), lower)
}

// ─── frontend_explain_screen ──────────────────────────────────────────────────

type frontendExplainResult struct {
	Project              string             `json:"project"`
	Route                string             `json:"route"`
	Screen               string             `json:"screen"`
	GraphAvailable       bool               `json:"graph_available"`
	TruthClaims          []string           `json:"truth_claims"`
	Authorities          []string           `json:"authorities"`
	MustShow             []string           `json:"must_show"`
	Forbidden            []string           `json:"forbidden"`
	RelatedInvariants    []InvariantEntry   `json:"related_invariants"`
	RelatedFailureModes  []FailureModeEntry `json:"related_failure_modes"`
	Warnings             []string           `json:"warnings"`
}

func frontendExplainScreen(prof *project.ProjectProfile, route string) frontendExplainResult {
	result := frontendExplainResult{
		Project: prof.Name,
		Route:   route,
		Screen:  screenNameFromRoute(route),
	}

	// Try graph lookup for route nodes.
	if gf, err := loadGraphFile(prof.Graph.CacheDir, prof.Awareness.Root); err == nil {
		result.GraphAvailable = true
		for _, n := range gf.Nodes {
			if n.Kind == "frontend_route" && routeMatches(n, route) {
				result.TruthClaims = append(result.TruthClaims, "route: "+n.ID)
			}
		}
	}

	// YAML knowledge search using route + screen terms.
	query := route + " " + result.Screen + " screen ui status permission workflow"
	result.RelatedInvariants = searchInvariants(loadInvariants(prof.Awareness.Invariants), query, 8)
	result.RelatedFailureModes = searchFailureModes(loadFailureModes(prof.Awareness.FailureModes), query, 5)

	// Extract must_show hints from invariant titles/descriptions.
	for _, inv := range result.RelatedInvariants {
		blob := strings.ToLower(inv.Title + " " + inv.Description)
		if strings.Contains(blob, "must") || strings.Contains(blob, "visible") || strings.Contains(blob, "required") {
			result.MustShow = append(result.MustShow, inv.Title)
		}
	}

	// Extract forbidden hints from failure mode descriptions.
	for _, fm := range result.RelatedFailureModes {
		if fm.Severity == "critical" {
			result.Forbidden = append(result.Forbidden, fm.WrongFixes...)
		}
	}

	// Authorities inferred from matched invariant text.
	result.Authorities = extractAuthorities(result.RelatedInvariants)

	if len(result.RelatedInvariants) == 0 {
		result.Warnings = append(result.Warnings,
			"no invariants matched route — add screen-specific invariants to .awareness/invariants.yaml")
	}

	return result
}

func screenNameFromRoute(route string) string {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) == 0 {
		return "UnknownScreen"
	}
	last := parts[len(parts)-1]
	if last == "" && len(parts) > 1 {
		last = parts[len(parts)-2]
	}
	if last == "" {
		return "HomePage"
	}
	words := strings.Split(strings.ReplaceAll(last, "_", "-"), "-")
	var out []string
	for _, w := range words {
		if len(w) > 0 {
			out = append(out, strings.ToUpper(w[:1])+w[1:])
		}
	}
	return strings.Join(out, "") + "Page"
}

func routeMatches(n graphNode, route string) bool {
	label := ""
	if n.Properties != nil {
		label = n.Properties["name"]
	}
	return strings.Contains(n.ID, route) || strings.Contains(n.Label, route) ||
		strings.Contains(label, route)
}

func extractAuthorities(invariants []InvariantEntry) []string {
	authorityTerms := []string{
		"runtime authority", "desired state", "installed state",
		"catalog authority", "rbac", "workflow", "doctor findings", "audit log",
	}
	seen := map[string]bool{}
	var out []string
	for _, inv := range invariants {
		blob := strings.ToLower(inv.Description + " " + inv.Title)
		for _, term := range authorityTerms {
			if !seen[term] && strings.Contains(blob, term) {
				seen[term] = true
				out = append(out, term)
			}
		}
	}
	return out
}

// ─── frontend_plan_feature ────────────────────────────────────────────────────

type frontendPlanResult struct {
	Project            string            `json:"project"`
	Intent             string            `json:"intent"`
	ProposedContracts  proposedContracts `json:"proposed_contracts"`
	AuthorityQuestions []string          `json:"authority_questions"`
	RequiredTests      []string          `json:"required_tests"`
	ForbiddenPatterns  []string          `json:"forbidden_patterns"`
}

type proposedContracts struct {
	ScreenContract     string   `json:"screen_contract"`
	ComponentContracts []string `json:"component_contracts"`
	JourneyContract    string   `json:"journey_contract"`
}

func frontendPlanFeature(prof *project.ProjectProfile, intent string) frontendPlanResult {
	result := frontendPlanResult{
		Project: prof.Name,
		Intent:  intent,
	}
	lower := strings.ToLower(intent)
	screenName := inferScreenName(intent)

	result.ProposedContracts.ScreenContract = buildScreenContractTemplate(screenName, intent, lower)
	result.ProposedContracts.ComponentContracts = buildComponentContractTemplates(lower)
	result.ProposedContracts.JourneyContract = buildJourneyContractTemplate(screenName, intent)
	result.AuthorityQuestions = authorityQuestionsFromIntent(lower)

	items := preflight.ExtendedPreflightItemsFromPaths(intent, nil, prof.Awareness)
	if items != nil {
		result.RequiredTests = items.RequiredTests
	}
	if len(result.RequiredTests) == 0 {
		result.RequiredTests = defaultFrontendTests(lower)
	}

	terms := knowledgeTerms(intent)
	for _, ff := range loadForbiddenFixes(prof.Awareness.ForbiddenFixes) {
		blob := strings.ToLower(ff.ID + " " + ff.Title + " " + ff.Description + " " + ff.AppliesWhen)
		if countMatches(blob, terms) > 0 {
			result.ForbiddenPatterns = append(result.ForbiddenPatterns, ff.ID+": "+ff.Description)
			if len(result.ForbiddenPatterns) >= 5 {
				break
			}
		}
	}

	return result
}

func inferScreenName(intent string) string {
	for _, w := range strings.Fields(intent) {
		w = strings.Trim(w, ".,;:")
		if len(w) > 3 && strings.ToUpper(w[:1]) == w[:1] {
			return w + "Screen"
		}
	}
	return "NewScreen"
}

func buildScreenContractTemplate(screenName, intent, lower string) string {
	var authorities []string
	if strings.Contains(lower, "install") || strings.Contains(lower, "package") {
		authorities = append(authorities, "  installed_state: InstalledState authority")
	}
	if strings.Contains(lower, "workflow") || strings.Contains(lower, "progress") {
		authorities = append(authorities, "  workflow_state: Workflow receipt authority")
	}
	if strings.Contains(lower, "runtime") || strings.Contains(lower, "health") || strings.Contains(lower, "status") {
		authorities = append(authorities, "  runtime_health: Runtime health authority")
	}
	if strings.Contains(lower, "permission") || strings.Contains(lower, "rbac") {
		authorities = append(authorities, "  permission: RBAC authority")
	}
	if len(authorities) == 0 {
		authorities = append(authorities, "  # TODO: identify and declare state authorities")
	}
	return fmt.Sprintf("screen: %s\nintent: %s\nauthority:\n%s\nmust_show:\n  - # TODO: list required items\nforbidden:\n  - show healthy/success/installed without authoritative confirmation\n  - hide failure or blocked reason\n",
		screenName, intent, strings.Join(authorities, "\n"))
}

func buildComponentContractTemplates(lower string) []string {
	var contracts []string
	if strings.Contains(lower, "button") || strings.Contains(lower, "install") || strings.Contains(lower, "action") {
		contracts = append(contracts, "component: ActionButton\nrole: Triggers mutating workflow action.\nforbidden:\n  - enabled without RBAC confirmation\n  - mark success before backend confirmation\n  - retry permission denied\n")
	}
	if strings.Contains(lower, "progress") || strings.Contains(lower, "workflow") {
		contracts = append(contracts, "component: WorkflowProgressPanel\nrole: Displays live workflow state.\nforbidden:\n  - show success before terminal workflow state\n  - hide failure or blocked reason\n  - replace structured reason with generic message\n")
	}
	if strings.Contains(lower, "status") || strings.Contains(lower, "health") || strings.Contains(lower, "badge") {
		contracts = append(contracts, "component: StatusBadge\nrole: Displays runtime health status.\nforbidden:\n  - derive green from desired.enabled\n  - show healthy when runtime authority is unknown\n")
	}
	if len(contracts) == 0 {
		contracts = append(contracts, "component: # TODO: name\nrole: # TODO: describe operator-truth role\nforbidden:\n  - show incorrect authority\n  - hide failure reason\n")
	}
	return contracts
}

func buildJourneyContractTemplate(screenName, intent string) string {
	return fmt.Sprintf("journey: %s\nintent: %s\nsteps:\n  - authenticate\n  - navigate to screen\n  - observe correct authoritative state\n  - perform action (if applicable)\n  - observe authoritative state update\nforbidden:\n  - success before backend confirmation\n  - failure without reason\n  - permission error hidden\nproof:\n  - e2e success test\n  - e2e error/denial test\n",
		screenName, intent)
}

func authorityQuestionsFromIntent(lower string) []string {
	q := []string{
		"What state does this screen display, and which backend layer owns it?",
		"What actions can the user take, and what RBAC permission protects each?",
		"What failure, blocked, degraded, or destructive state must remain visible?",
		"What test or screenshot proves the operator-truth contract is met?",
	}
	if strings.Contains(lower, "install") || strings.Contains(lower, "deploy") {
		q = append(q, "Does success require a workflow receipt, or only a local optimistic update?")
	}
	if strings.Contains(lower, "topology") || strings.Contains(lower, "storage") || strings.Contains(lower, "objectstore") {
		q = append(q, "Is there a destructive-risk transition that must block the action or warn the operator?")
	}
	return q
}

func defaultFrontendTests(lower string) []string {
	var tests []string
	if strings.Contains(lower, "status") || strings.Contains(lower, "health") || strings.Contains(lower, "badge") {
		tests = append(tests,
			"badge shows unknown when runtime authority is unavailable",
			"badge does not show healthy from desired state alone")
	}
	if strings.Contains(lower, "install") || strings.Contains(lower, "action") || strings.Contains(lower, "button") {
		tests = append(tests,
			"action button disabled without RBAC permission",
			"success state not shown before backend confirmation",
			"backend error reason displayed on failure")
	}
	if strings.Contains(lower, "workflow") || strings.Contains(lower, "progress") {
		tests = append(tests,
			"workflow failure reason visible to operator",
			"blocked reason not replaced by generic message")
	}
	if len(tests) == 0 {
		tests = append(tests, "operator truth contract test for this screen")
	}
	return tests
}

// ─── frontend_verify_change ───────────────────────────────────────────────────

type frontendVerifyResult struct {
	Project           string             `json:"project"`
	Files             []string           `json:"files"`
	Task              string             `json:"task"`
	RelatedComponents []string           `json:"related_components"`
	RelatedRoutes     []string           `json:"related_routes"`
	Invariants        []InvariantEntry   `json:"invariants"`
	FailureModes      []FailureModeEntry `json:"failure_modes"`
	ForbiddenFixes    []ForbiddenFixEntry `json:"forbidden_fixes"`
	RequiredTests     []string           `json:"required_tests"`
	VisualContracts   []string           `json:"visual_contracts"`
	Verdict           string             `json:"verdict"`
	Confidence        string             `json:"confidence"`
	Warnings          []string           `json:"warnings"`
}

func frontendVerifyChange(prof *project.ProjectProfile, files []string, task string) frontendVerifyResult {
	result := frontendVerifyResult{
		Project: prof.Name,
		Files:   files,
		Task:    task,
		Verdict:    "allow",
		Confidence: "confirmed",
	}

	// Build combined query from task + file names.
	queryParts := []string{task}
	for _, f := range files {
		queryParts = append(queryParts, filepath.Base(f))
		queryParts = append(queryParts, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
	}
	query := strings.Join(queryParts, " ")

	rawMatches := preflight.RawKnowledgeFallbackFromPaths(query, files, prof.Awareness)

	var invIDs, fmIDs, ffIDs []string
	for _, m := range rawMatches {
		if m.Score < 1 {
			continue
		}
		switch m.Kind {
		case "invariant":
			invIDs = append(invIDs, m.ID)
		case "failure_mode":
			fmIDs = append(fmIDs, m.ID)
		case "forbidden_fix":
			ffIDs = append(ffIDs, m.ID)
		}
	}

	idSet := func(ids []string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	invSet, fmSet, ffSet := idSet(invIDs), idSet(fmIDs), idSet(ffIDs)

	for _, inv := range loadInvariants(prof.Awareness.Invariants) {
		if invSet[inv.ID] {
			result.Invariants = append(result.Invariants, inv)
		}
	}
	for _, fm := range loadFailureModes(prof.Awareness.FailureModes) {
		if fmSet[fm.ID] {
			result.FailureModes = append(result.FailureModes, fm)
		}
	}
	for _, ff := range loadForbiddenFixes(prof.Awareness.ForbiddenFixes) {
		if ffSet[ff.ID] {
			result.ForbiddenFixes = append(result.ForbiddenFixes, ff)
		}
	}

	items := preflight.ExtendedPreflightItemsFromPaths(query, files, prof.Awareness)
	if items != nil {
		result.RequiredTests = items.RequiredTests
	}

	// Graph: extract component/route names for changed files.
	if gf, err := loadGraphFile(prof.Graph.CacheDir, prof.Awareness.Root); err == nil {
		for _, f := range files {
			base := strings.ToLower(filepath.Base(f))
			for _, n := range gf.Nodes {
				fileProp := ""
				if n.Properties != nil {
					fileProp = strings.ToLower(n.Properties["file"])
				}
				if n.Kind == "frontend_component" && strings.Contains(fileProp, base) {
					result.RelatedComponents = append(result.RelatedComponents, n.Label)
				}
				if n.Kind == "frontend_route" && strings.Contains(fileProp, base) {
					result.RelatedRoutes = append(result.RelatedRoutes, n.Label)
				}
			}
		}
	}

	// Layout-sensitive file warnings.
	for _, f := range files {
		if isTSXLayoutFile(f) {
			result.VisualContracts = append(result.VisualContracts,
				"layout change in "+filepath.Base(f)+" — verify critical warning visibility on desktop and mobile")
		}
	}

	// Frontend-specific heuristic warnings.
	result.Warnings = frontendChangeWarnings(files, task, result)

	result.Verdict, result.Confidence = computeFrontendVerdict(result)
	return result
}

func frontendChangeWarnings(files []string, task string, r frontendVerifyResult) []string {
	var warnings []string
	taskLower := strings.ToLower(task)
	for _, f := range files {
		base := strings.ToLower(filepath.Base(f))
		fl := strings.ToLower(f)

		if isFrontendSourceFile(f) && len(r.Invariants) == 0 {
			warnings = append(warnings, base+": no frontend awareness invariants matched — confirm no operator-truth contracts exist")
		}
		if isStatusOrHealthFile(base) && !hasStatusInvariant(r.Invariants) {
			warnings = append(warnings, base+": status/health component changed — verify runtime badge authority (ui.status.runtime_badge_authority)")
		}
		if isMutatingActionFile(base) && !hasRBACInvariant(r.Invariants) {
			warnings = append(warnings, base+": mutating action component changed — verify RBAC gate (ui.action.rbac_gate_required)")
		}
		if isWorkflowFile(base) || strings.Contains(taskLower, "workflow") {
			if !hasWorkflowInvariant(r.Invariants) {
				warnings = append(warnings, base+": workflow-related change — verify failure reason visibility (ui.workflow.failure_reason_visible)")
			}
		}
		if isLayoutFile(fl) && !hasVisualInvariant(r.Invariants) {
			warnings = append(warnings, base+": layout/CSS change — verify critical warning visibility (ui.visual.critical_warning_visible)")
		}
	}
	return warnings
}

func isFrontendSourceFile(f string) bool {
	ext := strings.ToLower(filepath.Ext(f))
	return ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx"
}

func isStatusOrHealthFile(base string) bool {
	return strings.Contains(base, "status") || strings.Contains(base, "health") ||
		strings.Contains(base, "badge") || strings.Contains(base, "indicator")
}

func isMutatingActionFile(base string) bool {
	return strings.Contains(base, "button") || strings.Contains(base, "action") ||
		strings.Contains(base, "install") || strings.Contains(base, "deploy") ||
		strings.Contains(base, "delete") || strings.Contains(base, "approve")
}

func isWorkflowFile(base string) bool {
	return strings.Contains(base, "workflow") || strings.Contains(base, "progress") ||
		strings.Contains(base, "pipeline")
}

func isLayoutFile(f string) bool {
	return strings.HasSuffix(f, ".css") || strings.HasSuffix(f, ".scss") ||
		strings.Contains(f, "layout") || strings.Contains(f, "theme") || strings.Contains(f, "skin")
}

func isTSXLayoutFile(f string) bool {
	base := strings.ToLower(filepath.Base(f))
	ext := strings.ToLower(filepath.Ext(f))
	return (ext == ".tsx" || ext == ".ts" || ext == ".css" || ext == ".scss") &&
		(strings.Contains(base, "layout") || strings.Contains(base, "theme") ||
			strings.Contains(base, "card") || strings.Contains(base, "page") ||
			strings.Contains(base, "panel") || strings.Contains(base, "modal"))
}

func hasStatusInvariant(invs []InvariantEntry) bool {
	for _, inv := range invs {
		if strings.Contains(inv.ID, "status") || strings.Contains(inv.ID, "badge") || strings.Contains(inv.ID, "runtime") {
			return true
		}
	}
	return false
}

func hasRBACInvariant(invs []InvariantEntry) bool {
	for _, inv := range invs {
		if strings.Contains(inv.ID, "rbac") || strings.Contains(inv.ID, "permission") || strings.Contains(inv.ID, "action") {
			return true
		}
	}
	return false
}

func hasWorkflowInvariant(invs []InvariantEntry) bool {
	for _, inv := range invs {
		if strings.Contains(inv.ID, "workflow") || strings.Contains(inv.ID, "failure") {
			return true
		}
	}
	return false
}

func hasVisualInvariant(invs []InvariantEntry) bool {
	for _, inv := range invs {
		if strings.Contains(inv.ID, "visual") || strings.Contains(inv.ID, "warning") || strings.Contains(inv.ID, "layout") {
			return true
		}
	}
	return false
}

func computeFrontendVerdict(r frontendVerifyResult) (verdict, confidence string) {
	for _, ff := range r.ForbiddenFixes {
		if strings.Contains(ff.ID, "derive_runtime") || strings.Contains(ff.ID, "enable_action_without") ||
			strings.Contains(ff.ID, "ai_invents") || strings.Contains(ff.ID, "optimistic_success") {
			return "block", "confirmed"
		}
	}
	if len(r.FailureModes) > 0 || len(r.Warnings) > 0 {
		return "warn", "suspected"
	}
	if len(r.Invariants) > 0 && len(r.RequiredTests) > 0 {
		return "warn", "insufficient_evidence"
	}
	return "allow", "confirmed"
}

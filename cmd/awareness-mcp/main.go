// awareness-mcp is the standalone Awareness MCP server.
//
// It serves generic Awareness tools over the Model Context Protocol (MCP)
// JSON-RPC 2.0 stdio transport. The server is project-agnostic: all tools
// work without a Globular cluster.
//
// Projects with runtime.enabled=false (adapter: null) receive NullAdapter,
// which returns runtime_disabled for runtime-only calls. No Globular
// infrastructure is required.
//
// Usage:
//
//	awareness-mcp [--project-root PATH]
//
// The server resolves the project profile from .awareness.yaml by walking up
// from PATH (or the current directory when --project-root is omitted).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/globulario/awareness/bundle"
	"github.com/globulario/awareness/knowledge"
	"github.com/globulario/awareness/preflight"
	"github.com/globulario/awareness/project"
	"github.com/globulario/awareness/runtime"
)

// ─── JSON-RPC 2.0 ────────────────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ─── MCP protocol types ───────────────────────────────────────────────────────

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string               `json:"type"`
	Properties map[string]propSchema `json:"properties,omitempty"`
	Required   []string             `json:"required,omitempty"`
}

type propSchema struct {
	Type        string      `json:"type,omitempty"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

type toolResult struct {
	Content []toolResultContent `json:"content"`
	IsError bool                `json:"isError,omitempty"`
}

type toolResultContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ─── Server ───────────────────────────────────────────────────────────────────

type toolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)

type registeredTool struct {
	def     toolDef
	handler toolHandler
}

type mcpServer struct {
	prof    *project.ProjectProfile
	adapter runtime.Adapter
	mu      sync.RWMutex
	tools   map[string]*registeredTool
	order   []string
	sem     chan struct{}
}

func newServer(prof *project.ProjectProfile, adapter runtime.Adapter) *mcpServer {
	return &mcpServer{
		prof:    prof,
		adapter: adapter,
		tools:   make(map[string]*registeredTool),
		sem:     make(chan struct{}, 8),
	}
}

func (s *mcpServer) register(def toolDef, handler toolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[def.Name] = &registeredTool{def: def, handler: handler}
	s.order = append(s.order, def.Name)
}

func (s *mcpServer) serveStdio(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := readStdioMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			log.Printf("invalid JSON-RPC: %v", err)
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if resp != nil {
			data, _ := json.Marshal(resp)
			fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(data))
			writer.Write(data)
			writer.Flush()
		}
	}
}

// readStdioMessage supports both LSP-style Content-Length framing and
// newline-delimited JSON for compatibility with early MCP clients.
func readStdioMessage(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		// Newline-delimited JSON fallback.
		if strings.HasPrefix(trimmed, "{") {
			return []byte(trimmed), nil
		}
		// LSP-style headers.
		headers := map[string]string{}
		for {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
			}
			line, err = r.ReadString('\n')
			if err != nil {
				return nil, err
			}
			trimmed = strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				break
			}
		}
		cl := headers["content-length"]
		if cl == "" {
			return nil, fmt.Errorf("missing Content-Length header")
		}
		n, err := strconv.Atoi(cl)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid Content-Length: %q", cl)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
}

func (s *mcpServer) handleRequest(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized", "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"resources": []interface{}{}}}
	case "prompts/list":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"prompts": []interface{}{}}}
	case "ping":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}}
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *mcpServer) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]interface{}{
				"authentication": map[string]interface{}{"methods": []string{"none"}, "required": false},
				"tools":          map[string]interface{}{"listChanged": false},
				"resources":      map[string]interface{}{"listChanged": false, "subscribe": false},
				"prompts":        map[string]interface{}{"listChanged": false},
			},
			"serverInfo": map[string]interface{}{"name": "awareness-mcp", "version": ""},
		},
	}
}

func (s *mcpServer) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]toolDef, 0, len(s.order))
	for _, name := range s.order {
		if t, ok := s.tools[name]; ok {
			tools = append(tools, t.def)
		}
	}
	return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": tools}}
}

func (s *mcpServer) handleToolsCall(ctx context.Context, req *jsonRPCRequest) *jsonRPCResponse {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jsonRPCError{Code: -32602, Message: "invalid tool call params"},
		}
	}
	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()
	if !ok {
		return &jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jsonRPCError{Code: -32602, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return &jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &jsonRPCError{Code: -32000, Message: "server busy"},
		}
	}
	result, err := tool.handler(ctx, params.Arguments)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: toolResult{
				Content: []toolResultContent{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return &jsonRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: toolResult{Content: []toolResultContent{{Type: "text", Text: string(data)}}},
	}
}

// ─── Tool registration ────────────────────────────────────────────────────────

func registerTools(srv *mcpServer) {
	prof := srv.prof
	adapter := srv.adapter

	srv.register(toolDef{
		Name:        "awareness_profile_doctor",
		Description: "Run a static health check on the project Awareness profile. Returns resolved paths, invariant/failure-mode counts, runtime status, and warnings for missing files. Never requires a live cluster.",
		InputSchema: inputSchema{Type: "object"},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		report := project.Doctor(prof)
		runtimeReport, _ := adapter.Doctor(ctx, prof)
		return map[string]interface{}{
			"project":        report.Project,
			"kind":           report.Kind,
			"root":           report.Root,
			"config_path":    report.ConfigPath,
			"runtime_status": report.RuntimeStatus,
			"graph_cache":    report.GraphCache,
			"ok":             report.OK,
			"checks":         report.Checks,
			"runtime":        runtimeReport,
			"awareness": map[string]interface{}{
				"root":            prof.Awareness.Root,
				"invariants":      prof.Awareness.Invariants,
				"failure_modes":   prof.Awareness.FailureModes,
				"forbidden_fixes": prof.Awareness.ForbiddenFixes,
				"decisions_dir":   prof.Awareness.DecisionsDir,
			},
		}, nil
	})

	srv.register(toolDef{
		Name:        "awareness_runtime_status",
		Description: "Return the runtime adapter status. For projects with runtime.enabled=false this returns runtime_disabled. No cluster connection is required.",
		InputSchema: inputSchema{Type: "object"},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		runtimeReport, _ := adapter.Doctor(ctx, prof)
		return map[string]interface{}{
			"project": prof.Name,
			"adapter": adapter.Name(),
			"enabled": adapter.Enabled(),
			"status":  runtimeReport.Status,
			"runtime_config": map[string]interface{}{
				"enabled": prof.Runtime.Enabled,
				"adapter": prof.Runtime.Adapter,
			},
		}, nil
	})

	srv.register(toolDef{
		Name:        "awareness_preflight",
		Description: "Run a lightweight preflight check for a task or changed files. Returns matched invariants, failure modes, forbidden fixes, and task classification. Works with NullAdapter — no cluster required.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"task": {
					Type:        "string",
					Description: "Short description of the task you are about to perform.",
				},
				"changed": {
					Type:        "boolean",
					Description: "If true, detect git-changed files from the project root and include them in the analysis.",
					Default:     false,
				},
				"files": {
					Type:        "string",
					Description: "Comma-separated file paths to analyse (alternative to changed=true).",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		task, _ := args["task"].(string)
		changedFlag, _ := args["changed"].(bool)
		filesStr, _ := args["files"].(string)

		var changedFiles []string
		var warnings []string

		if changedFlag {
			changedFiles, warnings = collectChangedFiles(prof.Root)
		}
		if filesStr != "" {
			for _, f := range strings.Split(filesStr, ",") {
				if f = strings.TrimSpace(f); f != "" {
					changedFiles = append(changedFiles, f)
				}
			}
			changedFiles = preflight.UniqueStrings(changedFiles)
		}

		var classification []preflight.TaskClass
		if task != "" {
			classification = preflight.ClassifyTask(task)
		}

		rawMatches := preflight.RawKnowledgeFallbackFromPaths(task, changedFiles, prof.Awareness)

		var (
			invariants           []string
			failureModes         []string
			forbiddenFixes       []string
			incidentPatterns     []string
			decisions            []string
			forbiddenAssumptions []string
			authorityRules       []string
			remediationContracts []string
		)
		for _, m := range rawMatches {
			if m.Score < 2 {
				continue
			}
			switch m.Kind {
			case "invariant":
				invariants = append(invariants, m.ID)
			case "failure_mode":
				failureModes = append(failureModes, m.ID)
			case "forbidden_fix":
				forbiddenFixes = append(forbiddenFixes, m.ID)
			case "incident_pattern":
				incidentPatterns = append(incidentPatterns, m.ID)
			case "decision":
				decisions = append(decisions, m.ID)
			case "forbidden_assumption":
				forbiddenAssumptions = append(forbiddenAssumptions, m.ID)
			case "authority_rule":
				authorityRules = append(authorityRules, m.ID)
			case "remediation_contract":
				remediationContracts = append(remediationContracts, m.ID)
			}
		}

		var requiredTests, preflightQuestions, questions []string
		if items := preflight.ExtendedPreflightItemsFromPaths(task, changedFiles, prof.Awareness); items != nil {
			requiredTests = items.RequiredTests
			preflightQuestions = items.PreflightQuestions
			questions = items.Questions
			decisions = preflight.UniqueStrings(append(decisions, items.Decisions...))
			forbiddenAssumptions = preflight.UniqueStrings(append(forbiddenAssumptions, items.ForbiddenAssumptions...))
			authorityRules = preflight.UniqueStrings(append(authorityRules, items.AuthorityRules...))
			remediationContracts = preflight.UniqueStrings(append(remediationContracts, items.RemediationContracts...))
		}

		runtimeStatus := "disabled"
		if adapter.Enabled() {
			_, sigErr := adapter.CollectSignals(ctx, prof, runtime.SignalOptions{})
			if sigErr == nil {
				runtimeStatus = "ok"
			} else {
				warnings = append(warnings, fmt.Sprintf("collect signals: %v", sigErr))
			}
		}

		verdict, confidence := mcpComputeVerdict(failureModes, forbiddenAssumptions, questions)

		return preflight.PreflightResult{
			ProjectName:          prof.Name,
			Task:                 task,
			ChangedFiles:         changedFiles,
			Classification:       classification,
			Invariants:           preflight.UniqueStrings(invariants),
			FailureModes:         preflight.UniqueStrings(failureModes),
			ForbiddenFixes:       preflight.UniqueStrings(forbiddenFixes),
			IncidentPatterns:     preflight.UniqueStrings(incidentPatterns),
			Decisions:            decisions,
			ForbiddenAssumptions: forbiddenAssumptions,
			AuthorityRules:       authorityRules,
			RequiredTests:        requiredTests,
			PreflightQuestions:   preflightQuestions,
			Questions:            questions,
			RemediationContracts: remediationContracts,
			Verdict:              verdict,
			Confidence:           confidence,
			RawMatches:           rawMatches,
			RuntimeStatus:        runtimeStatus,
			Warnings:             warnings,
			OK:                   true,
		}, nil
	})

	srv.register(toolDef{
		Name:        "awareness_context",
		Description: "Search the project knowledge files (invariants, failure modes, forbidden fixes) for items relevant to a query. Returns scored matches from hand-authored awareness YAML files.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {
					Type:        "string",
					Description: "Query string — task description, file names, or keywords to match against knowledge files.",
				},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		matches := preflight.RawKnowledgeFallbackFromPaths(query, nil, prof.Awareness)
		return map[string]interface{}{
			"project":        prof.Name,
			"awareness_root": prof.Awareness.Root,
			"query":          query,
			"matches":        matches,
			"match_count":    len(matches),
		}, nil
	})

	// ── awareness_graph_query ──────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_graph_query",
		Description: "Query awareness graph nodes when a compiled graph exists. Returns matching nodes with their kind, label, and properties. Returns {ok:false, reason:\"graph_not_available\"} when no graph file is found in the project graph cache.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {
					Type:        "string",
					Description: "Keywords to match against node IDs, kinds, labels, and properties.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of results to return. Default 20.",
					Default:     20,
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		cacheDir := prof.Graph.CacheDir

		if p := graphJSONPath(cacheDir); p != "" {
			nodes, err := queryGraphJSON(p, query, limit)
			if err != nil {
				return map[string]interface{}{
					"ok":     false,
					"reason": "graph_read_error",
					"detail": err.Error(),
				}, nil
			}
			return map[string]interface{}{
				"ok":         true,
				"source":     p,
				"query":      query,
				"nodes":      nodes,
				"node_count": len(nodes),
			}, nil
		}

		if p := graphDBPath(cacheDir); p != "" {
			return map[string]interface{}{
				"ok":     false,
				"reason": "graph_binary_format",
				"detail": "graph.db found at " + p + " but binary graph format requires the services-side graph engine. Use awareness_context for YAML-based knowledge queries.",
			}, nil
		}

		return map[string]interface{}{
			"ok":         false,
			"reason":     "graph_not_available",
			"cache_dir":  cacheDir,
			"suggestion": "Run 'awareness graph build' (services-side) to compile a graph, or use awareness_context for YAML-based knowledge queries.",
		}, nil
	})

	// ── awareness_node_context ─────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_node_context",
		Description: "Return awareness context for a specific file path or knowledge node ID. Returns the node itself (when found in the graph), direct graph neighbors, related invariants, failure modes, and forbidden fixes. Falls back to YAML keyword search when no graph is available.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"path": {
					Type:        "string",
					Description: "File path (relative or absolute) to look up context for.",
				},
				"node_id": {
					Type:        "string",
					Description: "Awareness node ID (e.g. 'invariant:process.state.determinism') to look up.",
				},
				"max_neighbors": {
					Type:        "number",
					Description: "Maximum neighbor nodes to return. Default 10.",
					Default:     10,
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		path, _ := args["path"].(string)
		nodeID, _ := args["node_id"].(string)
		maxNeighbors := 10
		if v, ok := args["max_neighbors"].(float64); ok && v > 0 {
			maxNeighbors = int(v)
		}

		ref := strings.TrimSpace(path + " " + nodeID)
		if ref == "" {
			return nil, fmt.Errorf("one of 'path' or 'node_id' is required")
		}

		var warnings []string

		// Try graph lookup first.
		var graphNode *graphNodeResult
		var graphNeighbors []graphNodeResult

		if gf, loadErr := loadGraphFile(prof.Graph.CacheDir, prof.Awareness.Root); loadErr == nil {
			graphNode, graphNeighbors = lookupNodeInGraph(gf, nodeID, path, maxNeighbors)
		}

		// Always do YAML keyword search for invariants/failure_modes/forbidden_fixes.
		invariants := loadInvariants(prof.Awareness.Invariants)
		failureModes := loadFailureModes(prof.Awareness.FailureModes)
		forbiddenFixes := loadForbiddenFixes(prof.Awareness.ForbiddenFixes)

		matchedInvariants := searchInvariants(invariants, ref, 10)
		matchedFailureModes := searchFailureModes(failureModes, ref, 10)

		terms := knowledgeTerms(ref)
		var matchedFixes []ForbiddenFixEntry
		for _, f := range forbiddenFixes {
			blob := strings.ToLower(strings.Join([]string{
				f.ID, f.Title, f.Summary, f.Description,
				f.SafeAlternative, f.CorrectApproach,
				strings.Join(f.RelatedInvariants, " "),
				strings.Join(f.RelatedFailureModes, " "),
				strings.Join(f.Tags, " "),
			}, " "))
			if countMatches(blob, terms) > 0 {
				matchedFixes = append(matchedFixes, f)
				if len(matchedFixes) >= 10 {
					break
				}
			}
		}

		if len(prof.Awareness.Invariants) == 0 {
			warnings = append(warnings, "no invariants files configured in profile")
		}

		result := map[string]interface{}{
			"project":             prof.Name,
			"ref":                 ref,
			"invariants":          matchedInvariants,
			"failure_modes":       matchedFailureModes,
			"forbidden_fixes":     matchedFixes,
			"invariant_count":     len(matchedInvariants),
			"failure_mode_count":  len(matchedFailureModes),
			"forbidden_fix_count": len(matchedFixes),
			"warnings":            warnings,
		}

		if graphNode != nil {
			result["graph_node"] = graphNode
			result["graph_neighbors"] = graphNeighbors
			result["graph_neighbor_count"] = len(graphNeighbors)
		} else {
			result["graph_available"] = false
		}

		return result, nil
	})

	// ── awareness_invariant_lookup ─────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_invariant_lookup",
		Description: "Search project invariants by keyword. Returns matching invariants with ID, title, description, severity, tags, and source path.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {
					Type:        "string",
					Description: "Keywords to search for in invariant IDs, titles, descriptions, and tags.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum results to return. Default 10.",
					Default:     10,
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		all := loadInvariants(prof.Awareness.Invariants)
		matched := searchInvariants(all, query, limit)

		return map[string]interface{}{
			"project":     prof.Name,
			"query":       query,
			"invariants":  matched,
			"total_loaded": len(all),
			"match_count": len(matched),
			"source_files": prof.Awareness.Invariants,
		}, nil
	})

	// ── awareness_failure_mode_lookup ──────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_failure_mode_lookup",
		Description: "Search project failure modes by keyword. Returns matching failure modes with ID, title, description, symptoms, severity, tags, and source path.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {
					Type:        "string",
					Description: "Keywords to search for in failure mode IDs, titles, descriptions, symptoms, and tags.",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum results to return. Default 10.",
					Default:     10,
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		all := loadFailureModes(prof.Awareness.FailureModes)
		matched := searchFailureModes(all, query, limit)

		return map[string]interface{}{
			"project":      prof.Name,
			"query":        query,
			"failure_modes": matched,
			"total_loaded": len(all),
			"match_count":  len(matched),
			"source_files": prof.Awareness.FailureModes,
		}, nil
	})

	// ── awareness_bundle_inspect ───────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_bundle_inspect",
		Description: "Inspect a generated Awareness bundle directory. Reads bundle.json, validates the manifest, and returns a summary including schema version, project name, file counts, and any warnings.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"path": {
					Type:        "string",
					Description: "Absolute or relative path to the bundle directory (the directory containing bundle.json).",
				},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		bundlePath, _ := args["path"].(string)
		if strings.TrimSpace(bundlePath) == "" {
			return nil, fmt.Errorf("path is required")
		}

		manifestPath := bundlePath + "/bundle.json"
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return map[string]interface{}{
				"ok":     false,
				"reason": "manifest_not_found",
				"path":   bundlePath,
				"detail": err.Error(),
			}, nil
		}

		var m bundle.BundleManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return map[string]interface{}{
				"ok":     false,
				"reason": "manifest_parse_error",
				"path":   bundlePath,
				"detail": err.Error(),
			}, nil
		}

		var warnings []string
		if err := m.Validate(); err != nil {
			warnings = append(warnings, "validation: "+err.Error())
		}

		// Count files in bundle directory.
		entries, _ := os.ReadDir(bundlePath)
		var fileList []string
		for _, e := range entries {
			if !e.IsDir() {
				fileList = append(fileList, e.Name())
			}
		}

		return map[string]interface{}{
			"ok":                       len(warnings) == 0,
			"path":                     bundlePath,
			"schema_version":           m.SchemaVersion,
			"project_name":             m.ProjectName,
			"project_kind":             m.ProjectKind,
			"source_revision":          m.SourceRevision,
			"generated_at":             m.GeneratedAt,
			"generator_version":        m.GeneratorVersion,
			"invariants_paths":         m.InvariantsPaths,
			"failure_modes_paths":      m.FailureModesPaths,
			"forbidden_fixes_paths":    m.ForbiddenFixesPaths,
			"runtime_signals_included": m.RuntimeSignalsIncluded,
			"invariants_count":         len(m.InvariantsPaths),
			"failure_modes_count":      len(m.FailureModesPaths),
			"forbidden_fixes_count":    len(m.ForbiddenFixesPaths),
			"bundle_files":             fileList,
			"warnings":                 warnings,
		}, nil
	})

	// ── awareness_decision_lookup ──────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_decision_lookup",
		Description: "Look up architectural decision records that explain why a rule or constraint exists. Useful before modifying a design to understand the reasoning behind the current structure.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {Type: "string", Description: "Task description or keywords to match against decision records."},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		items := preflight.ExtendedPreflightItemsFromPaths(query, nil, prof.Awareness)
		if items == nil {
			return map[string]interface{}{"decisions": []string{}, "count": 0}, nil
		}
		return map[string]interface{}{
			"project":   prof.Name,
			"query":     query,
			"decisions": items.Decisions,
			"count":     len(items.Decisions),
		}, nil
	})

	// ── awareness_forbidden_assumption_lookup ──────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_forbidden_assumption_lookup",
		Description: "Look up forbidden assumptions — beliefs that are provably wrong and have caused past failures. Call this before making a design decision that involves implicit assumptions about system state.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {Type: "string", Description: "Task description or keywords to match against forbidden assumptions."},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		items := preflight.ExtendedPreflightItemsFromPaths(query, nil, prof.Awareness)
		if items == nil {
			return map[string]interface{}{"forbidden_assumptions": []string{}, "count": 0}, nil
		}
		return map[string]interface{}{
			"project":              prof.Name,
			"query":                query,
			"forbidden_assumptions": items.ForbiddenAssumptions,
			"count":                len(items.ForbiddenAssumptions),
		}, nil
	})

	// ── awareness_authority_lookup ─────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_authority_lookup",
		Description: "Look up authority rules that name which layer or source owns the answer to a specific question. Prevents mixing layers (desired vs installed vs runtime vs inventory).",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {Type: "string", Description: "Task description or question to match against authority rules."},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		items := preflight.ExtendedPreflightItemsFromPaths(query, nil, prof.Awareness)
		if items == nil {
			return map[string]interface{}{"authority_rules": []string{}, "count": 0}, nil
		}
		return map[string]interface{}{
			"project":        prof.Name,
			"query":          query,
			"authority_rules": items.AuthorityRules,
			"count":          len(items.AuthorityRules),
		}, nil
	})

	// ── awareness_required_tests ───────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_required_tests",
		Description: "Return the tests that must be run before completing a task or change. Matches by task description and changed file paths.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"task":  {Type: "string", Description: "Task description to match against test requirements."},
				"files": {Type: "string", Description: "Comma-separated changed file paths."},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		task, _ := args["task"].(string)
		filesStr, _ := args["files"].(string)
		var files []string
		for _, f := range strings.Split(filesStr, ",") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		items := preflight.ExtendedPreflightItemsFromPaths(task, files, prof.Awareness)
		if items == nil {
			return map[string]interface{}{"required_tests": []string{}, "count": 0}, nil
		}
		return map[string]interface{}{
			"project":        prof.Name,
			"task":           task,
			"required_tests": items.RequiredTests,
			"count":          len(items.RequiredTests),
		}, nil
	})

	// ── awareness_remediation_lookup ───────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_remediation_lookup",
		Description: "Look up safe remediation guidance for a failure mode or incident. Returns allowed actions, forbidden actions, and actions requiring human approval.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"query": {Type: "string", Description: "Failure mode ID, failure description, or task description to match against remediation contracts."},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		query, _ := args["query"].(string)
		items := preflight.ExtendedPreflightItemsFromPaths(query, nil, prof.Awareness)
		if items == nil {
			return map[string]interface{}{"remediation_contracts": []string{}, "count": 0}, nil
		}
		return map[string]interface{}{
			"project":              prof.Name,
			"query":                query,
			"remediation_contracts": items.RemediationContracts,
			"count":                len(items.RemediationContracts),
		}, nil
	})

	// ── awareness_assurance ──────────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_assurance",
		Description: "Report Awareness knowledge coverage: counts of invariants, failure modes, forbidden fixes, decisions, and other knowledge types. Shows blind spots and uncovered areas.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]propSchema{}},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		base, err := knowledge.LoadFromPaths(
			prof.Awareness.Invariants,
			prof.Awareness.FailureModes,
			prof.Awareness.ForbiddenFixes,
			prof.Awareness.IncidentPatterns,
			prof.Awareness.Root,
		)
		if err != nil || base == nil {
			return map[string]interface{}{"error": fmt.Sprintf("load knowledge: %v", err)}, nil
		}
		report := knowledge.Assurance(base)
		return map[string]interface{}{
			"project": prof.Name,
			"report":  report,
			"lines":   report.Lines(),
		}, nil
	})

	// ── awareness_selfcheck ──────────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "awareness_selfcheck",
		Description: "Validate the health of the Awareness knowledge base. Detects empty, stale, orphaned, or disconnected knowledge entries. Returns ok=true when the knowledge base is healthy.",
		InputSchema: inputSchema{Type: "object", Properties: map[string]propSchema{}},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		base, err := knowledge.LoadFromPaths(
			prof.Awareness.Invariants,
			prof.Awareness.FailureModes,
			prof.Awareness.ForbiddenFixes,
			prof.Awareness.IncidentPatterns,
			prof.Awareness.Root,
		)
		if err != nil || base == nil {
			return map[string]interface{}{"ok": false, "error": fmt.Sprintf("load knowledge: %v", err)}, nil
		}
		report := knowledge.Selfcheck(base)
		return map[string]interface{}{
			"project": prof.Name,
			"ok":      report.OK,
			"report":  report,
			"summary": report.String(),
		}, nil
	})

	// ── frontend_trace_component ───────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "frontend_trace_component",
		Description: "Return everything awareness knows about a frontend component: graph nodes, backend calls, state atoms, permission checks, and matched invariants/failure modes/forbidden fixes. Works without a compiled graph — falls back to YAML knowledge.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"component": {
					Type:        "string",
					Description: "Component name to look up (e.g. 'ServiceStatusCard', 'InstallButton').",
				},
				"include_contracts": {
					Type:        "boolean",
					Description: "If true, include matched invariants, failure modes, and forbidden fixes. Default true.",
					Default:     true,
				},
			},
			Required: []string{"component"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		component, _ := args["component"].(string)
		if strings.TrimSpace(component) == "" {
			return nil, fmt.Errorf("component is required")
		}
		includeContracts := true
		if v, ok := args["include_contracts"].(bool); ok {
			includeContracts = v
		}
		return frontendTraceComponent(prof, component, includeContracts), nil
	})

	// ── frontend_explain_screen ────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "frontend_explain_screen",
		Description: "Explain what a route/screen is supposed to display and what it must not lie about. Returns truth claims, state authorities, must-show items, forbidden behaviors, and matched invariants. Call before editing a page component.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"route": {
					Type:        "string",
					Description: "Route path to explain (e.g. '/admin/objectstore/topology').",
				},
			},
			Required: []string{"route"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		route, _ := args["route"].(string)
		if strings.TrimSpace(route) == "" {
			return nil, fmt.Errorf("route is required")
		}
		return frontendExplainScreen(prof, route), nil
	})

	// ── frontend_plan_feature ──────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "frontend_plan_feature",
		Description: "Given a feature intent, generate proposed awareness contracts (screen, component, journey) before any code is written. Returns contract drafts, authority questions, required tests, and applicable forbidden patterns. Does not write files.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"intent": {
					Type:        "string",
					Description: "Feature intent description (e.g. 'Add package install screen with workflow progress and RBAC-gated install button').",
				},
			},
			Required: []string{"intent"},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		intent, _ := args["intent"].(string)
		if strings.TrimSpace(intent) == "" {
			return nil, fmt.Errorf("intent is required")
		}
		return frontendPlanFeature(prof, intent), nil
	})

	// ── frontend_verify_change ─────────────────────────────────────────────────

	srv.register(toolDef{
		Name:        "frontend_verify_change",
		Description: "Check changed frontend files against frontend awareness before committing. Returns matched invariants, failure modes, forbidden fixes, required tests, and an allow|warn|block verdict. Call after editing TypeScript/React files.",
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]propSchema{
				"files": {
					Type:        "string",
					Description: "Comma-separated changed file paths (e.g. 'src/pages/ObjectStoreTopologyPage.tsx,src/components/StatusBadge.tsx').",
				},
				"task": {
					Type:        "string",
					Description: "Task description for context matching.",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		filesStr, _ := args["files"].(string)
		task, _ := args["task"].(string)
		var files []string
		for _, f := range strings.Split(filesStr, ",") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
		return frontendVerifyChange(prof, files, task), nil
	})
}

func mcpComputeVerdict(failureModes, forbiddenAssumptions, questions []string) (verdict, confidence string) {
	switch {
	case len(forbiddenAssumptions) > 0 || len(questions) > 0:
		return "warn", "insufficient_evidence"
	case len(failureModes) > 0:
		return "warn", "suspected"
	default:
		return "ok", "confirmed"
	}
}

// ─── Git helpers ──────────────────────────────────────────────────────────────

func collectChangedFiles(repoRoot string) ([]string, []string) {
	var files []string
	var warnings []string

	diffOut, err := runGit(repoRoot, "diff", "--name-only", "HEAD")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(diffOut), "\n") {
			if line != "" {
				files = append(files, line)
			}
		}
	}

	statusOut, err := runGit(repoRoot, "status", "--porcelain")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("git unavailable: %v — running without changed file context", err))
		return files, warnings
	}
	for _, line := range strings.Split(strings.TrimSpace(statusOut), "\n") {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		rest := strings.TrimSpace(line[2:])
		if xy == "  " {
			continue
		}
		if strings.Contains(rest, " -> ") {
			parts := strings.SplitN(rest, " -> ", 2)
			rest = strings.TrimSpace(parts[1])
		}
		if rest != "" {
			files = append(files, rest)
		}
	}
	return preflight.UniqueStrings(files), warnings
}

func runGit(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ─── Entry point ──────────────────────────────────────────────────────────────

func main() {
	projectRoot := ""
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--project-root":
			if i+1 < len(os.Args) {
				i++
				projectRoot = os.Args[i]
			}
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: awareness-mcp [--project-root PATH]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Serves generic Awareness MCP tools over JSON-RPC 2.0 / stdio.")
			fmt.Fprintln(os.Stderr, "Non-Globular projects (adapter: null) get runtime_disabled status.")
			os.Exit(0)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("awareness-mcp: %v", err)
	}

	prof, err := project.ResolveProfile(cwd, project.ResolveOptions{ProjectRoot: projectRoot})
	if err != nil {
		log.Fatalf("awareness-mcp: resolve profile: %v", err)
	}

	adapter, err := runtime.New(prof.AdapterName())
	if err != nil {
		log.Printf("awareness-mcp: runtime adapter %q not available in standalone server, using null: %v", prof.AdapterName(), err)
		adapter = runtime.NullAdapter{}
	}

	log.Printf("awareness-mcp: project=%s kind=%s adapter=%s root=%s",
		prof.Name, prof.Kind, adapter.Name(), prof.Root)

	srv := newServer(prof, adapter)
	registerTools(srv)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.serveStdio(ctx); err != nil {
		log.Fatalf("awareness-mcp: serve: %v", err)
	}
}

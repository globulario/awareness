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

		rawMatches := preflight.RawKnowledgeFallback(task, changedFiles, prof.Awareness.Root)

		var invariants, failureModes, forbiddenFixes []string
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
			}
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

		return preflight.PreflightResult{
			ProjectName:    prof.Name,
			Task:           task,
			ChangedFiles:   changedFiles,
			Classification: classification,
			Invariants:     preflight.UniqueStrings(invariants),
			FailureModes:   preflight.UniqueStrings(failureModes),
			ForbiddenFixes: preflight.UniqueStrings(forbiddenFixes),
			RawMatches:     rawMatches,
			RuntimeStatus:  runtimeStatus,
			Warnings:       warnings,
			OK:             true,
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
		matches := preflight.RawKnowledgeFallback(query, nil, prof.Awareness.Root)
		return map[string]interface{}{
			"project":        prof.Name,
			"awareness_root": prof.Awareness.Root,
			"query":          query,
			"matches":        matches,
			"match_count":    len(matches),
		}, nil
	})
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

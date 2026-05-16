# MCP Tools Reference

`awareness-mcp` exposes 9 tools over JSON-RPC 2.0 / stdio. All tools work without a Globular cluster. Projects with `runtime.enabled: false` (NullAdapter) receive `runtime_disabled` for runtime-specific fields but all knowledge tools function normally.

---

## 1. `awareness_profile_doctor`

Run a static health check on the project Awareness profile. Returns resolved paths, invariant/failure-mode counts, runtime status, and warnings for missing files. Never requires a live cluster.

### Input Schema

```json
{
  "type": "object",
  "properties": {}
}
```

No input required.

### Example Output

```json
{
  "project": "my-service",
  "kind": "application",
  "root": "/home/user/my-service",
  "config_path": "/home/user/my-service/.awareness.yaml",
  "runtime_status": "runtime_disabled",
  "graph_cache": "/home/user/my-service/.awareness/cache",
  "ok": true,
  "checks": [
    { "name": "awareness.root", "path": "/home/user/my-service/.awareness", "ok": true },
    { "name": "invariants[0]", "path": "/home/user/my-service/.awareness/invariants.yaml", "ok": true },
    { "name": "failure_modes[0]", "path": "/home/user/my-service/.awareness/failure_modes.yaml", "ok": true },
    { "name": "forbidden_fixes[0]", "path": "/home/user/my-service/.awareness/forbidden_fixes.yaml", "ok": true }
  ],
  "runtime": {
    "adapter": "null",
    "enabled": false,
    "status": "runtime_disabled",
    "findings": null
  },
  "awareness": {
    "root": "/home/user/my-service/.awareness",
    "invariants": ["/home/user/my-service/.awareness/invariants.yaml"],
    "failure_modes": ["/home/user/my-service/.awareness/failure_modes.yaml"],
    "forbidden_fixes": ["/home/user/my-service/.awareness/forbidden_fixes.yaml"],
    "decisions_dir": "/home/user/my-service/.awareness/decisions"
  }
}
```

---

## 2. `awareness_runtime_status`

Return the runtime adapter status. For projects with `runtime.enabled: false` this returns `runtime_disabled`. No cluster connection is required.

### Input Schema

```json
{
  "type": "object",
  "properties": {}
}
```

No input required.

### Example Output

```json
{
  "project": "my-service",
  "adapter": "null",
  "enabled": false,
  "status": "runtime_disabled",
  "runtime_config": {
    "enabled": false,
    "adapter": "null"
  }
}
```

For a project with a live Globular adapter:

```json
{
  "project": "globular-platform",
  "adapter": "globular",
  "enabled": true,
  "status": "ok",
  "runtime_config": {
    "enabled": true,
    "adapter": "globular"
  }
}
```

---

## 3. `awareness_preflight`

Run a lightweight preflight check for a task or changed files. Returns matched invariants, failure modes, forbidden fixes, and task classification. Works with NullAdapter — no cluster required.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "Short description of the task you are about to perform."
    },
    "changed": {
      "type": "boolean",
      "description": "If true, detect git-changed files from the project root and include them in the analysis.",
      "default": false
    },
    "files": {
      "type": "string",
      "description": "Comma-separated file paths to analyse (alternative to changed=true)."
    }
  }
}
```

### Example Output

```json
{
  "project_name": "my-service",
  "task": "fix goroutine leak in subscription handler",
  "changed_files": ["internal/subscription/handler.go"],
  "classification": ["LOCAL_CODE_CHANGE", "ARCHITECTURE_SENSITIVE"],
  "invariants": ["goroutine.bounds.enforced", "context.propagation.required"],
  "failure_modes": ["goroutine.leak.on.cancel"],
  "forbidden_fixes": [],
  "raw_matches": [
    { "kind": "invariant", "id": "goroutine.bounds.enforced", "score": 4 },
    { "kind": "failure_mode", "id": "goroutine.leak.on.cancel", "score": 4 },
    { "kind": "invariant", "id": "context.propagation.required", "score": 2 }
  ],
  "runtime_status": "disabled",
  "warnings": [],
  "ok": true
}
```

---

## 4. `awareness_context`

Search the project knowledge files (invariants, failure modes, forbidden fixes) for items relevant to a query. Returns scored matches from hand-authored awareness YAML files.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Query string — task description, file names, or keywords to match against knowledge files."
    }
  },
  "required": ["query"]
}
```

### Example Output

```json
{
  "project": "my-service",
  "awareness_root": "/home/user/my-service/.awareness",
  "query": "retry idempotency database",
  "matches": [
    {
      "kind": "invariant",
      "id": "idempotency.required",
      "score": 3,
      "title": "All mutating operations must be idempotent",
      "source_path": "/home/user/my-service/.awareness/invariants.yaml"
    },
    {
      "kind": "failure_mode",
      "id": "double.write.on.retry",
      "score": 3,
      "title": "Non-idempotent handler writes duplicate records on retry",
      "source_path": "/home/user/my-service/.awareness/failure_modes.yaml"
    }
  ],
  "match_count": 2
}
```

---

## 5. `awareness_graph_query`

Query awareness graph nodes when a compiled graph exists in the project's graph cache directory. Returns matching nodes with their kind, label, and properties. Returns `{ "ok": false, "reason": "graph_not_available" }` when no graph file is found.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Keywords to match against node IDs, kinds, labels, and properties."
    },
    "limit": {
      "type": "number",
      "description": "Maximum number of results to return. Default 20.",
      "default": 20
    }
  }
}
```

### Example Output (graph available)

```json
{
  "ok": true,
  "source": "/home/user/my-service/.awareness/cache/graph.json",
  "query": "handler payment",
  "nodes": [
    {
      "id": "func:internal/service.PaymentHandler",
      "kind": "function",
      "label": "PaymentHandler",
      "properties": { "file": "internal/service/handler.go", "line": "42" }
    }
  ],
  "node_count": 1
}
```

### Example Output (no graph)

```json
{
  "ok": false,
  "reason": "graph_not_available",
  "cache_dir": "/home/user/my-service/.awareness/cache",
  "suggestion": "Run 'awareness graph build' (services-side) to compile a graph, or use awareness_context for YAML-based knowledge queries."
}
```

---

## 6. `awareness_node_context`

Return awareness context for a specific file path or knowledge node ID. Returns related invariants, failure modes, and forbidden fixes from the project knowledge files.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "File path (relative or absolute) to look up context for."
    },
    "node_id": {
      "type": "string",
      "description": "Awareness node ID (e.g. 'invariant:process.state.determinism') to look up."
    }
  }
}
```

At least one of `path` or `node_id` is required.

### Example Output

```json
{
  "project": "my-service",
  "ref": "internal/service/handler.go",
  "invariants": [
    {
      "id": "idempotency.required",
      "title": "All mutating operations must be idempotent",
      "description": "Any RPC, HTTP handler, or background job...",
      "severity": "critical",
      "tags": ["idempotency", "rpc", "mutation"],
      "source_path": "/home/user/my-service/.awareness/invariants.yaml"
    }
  ],
  "failure_modes": [
    {
      "id": "double.write.on.retry",
      "title": "Non-idempotent handler writes duplicate records on retry",
      "severity": "critical",
      "source_path": "/home/user/my-service/.awareness/failure_modes.yaml"
    }
  ],
  "forbidden_fixes": [],
  "invariant_count": 1,
  "failure_mode_count": 1,
  "forbidden_fix_count": 0,
  "warnings": []
}
```

---

## 7. `awareness_invariant_lookup`

Search project invariants by keyword. Returns matching invariants with ID, title, description, severity, tags, and source path.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Keywords to search for in invariant IDs, titles, descriptions, and tags."
    },
    "limit": {
      "type": "number",
      "description": "Maximum results to return. Default 10.",
      "default": 10
    }
  }
}
```

### Example Output

```json
{
  "project": "my-service",
  "query": "goroutine context",
  "invariants": [
    {
      "id": "goroutine.bounds.enforced",
      "title": "Goroutines must be bounded and always terminate",
      "description": "Every goroutine must have a known termination condition...",
      "severity": "error",
      "tags": ["goroutine", "resource", "concurrency"],
      "source_path": "/home/user/my-service/.awareness/invariants.yaml"
    },
    {
      "id": "context.propagation.required",
      "title": "context.Context must be threaded through every call chain",
      "description": "Every function that performs I/O...",
      "severity": "critical",
      "tags": ["context", "goroutine", "timeout"],
      "source_path": "/home/user/my-service/.awareness/invariants.yaml"
    }
  ],
  "total_loaded": 5,
  "match_count": 2,
  "source_files": ["/home/user/my-service/.awareness/invariants.yaml"]
}
```

---

## 8. `awareness_failure_mode_lookup`

Search project failure modes by keyword. Returns matching failure modes with ID, title, description, symptoms, wrong_fixes, severity, tags, and source path.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Keywords to search for in failure mode IDs, titles, descriptions, symptoms, and tags."
    },
    "limit": {
      "type": "number",
      "description": "Maximum results to return. Default 10.",
      "default": 10
    }
  }
}
```

### Example Output

```json
{
  "project": "my-service",
  "query": "goroutine leak channel",
  "failure_modes": [
    {
      "id": "goroutine.leak.on.cancel",
      "title": "Goroutine leaks when context is cancelled before channel receive",
      "description": "A goroutine blocking on a channel receive without a context select...",
      "symptoms": [
        "goroutine count grows linearly with request rate under pprof",
        "heap profile shows channel receive stacks accumulating",
        "service becomes unresponsive after sustained traffic"
      ],
      "wrong_fixes": [
        "Increasing server timeouts — this delays the symptom but does not fix the leak",
        "Restarting the service — clears the leak temporarily but goroutines return immediately"
      ],
      "severity": "error",
      "tags": ["goroutine", "context", "channel", "leak"],
      "source_path": "/home/user/my-service/.awareness/failure_modes.yaml"
    }
  ],
  "total_loaded": 3,
  "match_count": 1,
  "source_files": ["/home/user/my-service/.awareness/failure_modes.yaml"]
}
```

---

## 9. `awareness_bundle_inspect`

Inspect a generated Awareness bundle directory. Reads `bundle.json`, validates the manifest, and returns a summary including schema version, project name, file counts, and any warnings.

### Input Schema

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or relative path to the bundle directory (the directory containing bundle.json)."
    }
  },
  "required": ["path"]
}
```

### Example Output

```json
{
  "ok": true,
  "path": "/tmp/my-project-bundle",
  "schema_version": "awareness.bundle.v1",
  "project_name": "my-service",
  "project_kind": "application",
  "source_revision": "a3f9c12",
  "generated_at": "2026-05-16T14:30:00Z",
  "generator_version": "0.1.0",
  "invariants_paths": ["invariants.yaml"],
  "failure_modes_paths": ["failure_modes.yaml"],
  "forbidden_fixes_paths": ["forbidden_fixes.yaml"],
  "runtime_signals_included": false,
  "invariants_count": 1,
  "failure_modes_count": 1,
  "forbidden_fixes_count": 1,
  "bundle_files": [
    "bundle.json",
    "profile.json",
    "invariants.yaml",
    "failure_modes.yaml",
    "forbidden_fixes.yaml"
  ],
  "warnings": []
}
```

### Error Output (bundle not found)

```json
{
  "ok": false,
  "reason": "manifest_not_found",
  "path": "/tmp/nonexistent-bundle",
  "detail": "open /tmp/nonexistent-bundle/bundle.json: no such file or directory"
}
```

---

## See Also

- [mcp/claude-code.md](claude-code.md) — Claude Code configuration
- [mcp/codex.md](codex.md) — Codex and generic stdio configuration
- [concepts/mcp.md](../concepts/mcp.md) — MCP server overview

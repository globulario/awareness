# Awareness MCP Server — Configuration Guide

The standalone `awareness-mcp` server exposes project Awareness tools over the
Model Context Protocol (MCP) JSON-RPC 2.0 stdio transport.

It works without a Globular cluster. Non-Globular projects (adapter: null)
receive `runtime_disabled` status for any runtime-only call.

---

## Installation

```bash
go install github.com/globulario/awareness/cmd/awareness-mcp@latest
```

Or build from source:

```bash
cd /path/to/awareness
go build -o ~/go/bin/awareness-mcp ./cmd/awareness-mcp
```

---

## Starting the Server

```bash
awareness-mcp --project-root /path/to/project
```

The server resolves the project profile from `.awareness.yaml` in the given
directory (or the current directory when `--project-root` is omitted).

---

## Claude Code Configuration

**.mcp.json (project-level)**

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/path/to/project"]
    }
  }
}
```

**~/.config/claude/mcp.json (global)**

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/path/to/project"]
    }
  }
}
```

---

## Available Tools

### `awareness_profile_doctor`

Static health check on the project profile. Returns resolved paths, runtime
status, and warnings for missing files.

Input: none required

```json
{}
```

Output example:

```json
{
  "project": "cadence",
  "kind": "bpmn-workflow-engine",
  "runtime_status": "disabled",
  "ok": true,
  "checks": [
    {"name": "awareness.root", "status": "ok", "detail": "/path/.awareness"},
    {"name": "invariants", "status": "ok", "detail": "/path/.awareness/invariants.yaml"}
  ]
}
```

---

### `awareness_preflight`

Lightweight pre-edit check. Returns matched invariants, failure modes,
forbidden fixes, and task classification.

Input:

```json
{
  "task": "fix exclusive gateway condition evaluation",
  "changed": true
}
```

Or with explicit file list:

```json
{
  "task": "fix token handling",
  "files": "engine/gateway.go,engine/token.go"
}
```

Output: `preflight.PreflightResult` (see preflight package for full schema).

---

### `awareness_runtime_status`

Returns the adapter status. Always `runtime_disabled` for NullAdapter projects.

Input: none required

Output:

```json
{
  "project": "cadence",
  "adapter": "null",
  "enabled": false,
  "status": "runtime_disabled"
}
```

---

### `awareness_context`

Searches the project awareness knowledge files for items relevant to a query.
Returns scored matches from invariants, failure_modes, and forbidden_fixes.

Input:

```json
{"query": "gateway condition token mutation"}
```

Output:

```json
{
  "project": "cadence",
  "query": "gateway condition token mutation",
  "match_count": 5,
  "matches": [
    {"kind": "invariant", "id": "process.state.determinism", "score": 4},
    {"kind": "forbidden_fix", "id": "no.wall.clock.in.flow.condition", "score": 3}
  ]
}
```

---

## How `runtime_disabled` Works

Projects with `runtime.enabled: false` (or `adapter: null`) use `NullAdapter`:

- `NullAdapter.Enabled()` returns `false`
- `NullAdapter.Doctor()` returns `{status: "runtime_disabled"}`
- `NullAdapter.CollectSignals()` returns empty `RuntimeSignals`
- No network connections are opened
- No Globular packages are imported

All generic awareness tools (preflight, context, profile doctor) work normally
regardless of adapter. Only live-cluster tools (which exist only in the services
Globular MCP server) are unavailable.

---

## Protocol

The server uses JSON-RPC 2.0 over stdio with Content-Length framing (LSP-style):

```
Content-Length: <N>\r\n\r\n<JSON body>
```

It also accepts newline-delimited JSON for compatibility with early MCP clients.

Methods handled: `initialize`, `tools/list`, `tools/call`, `resources/list`,
`prompts/list`, `ping`.

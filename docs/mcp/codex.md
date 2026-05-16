# Codex and Generic stdio MCP Configuration

`awareness-mcp` is a standard JSON-RPC 2.0 stdio server. Any MCP-compatible client that supports stdio transport can use it. This page covers generic stdio configuration and notes on Codex.

---

## Protocol

`awareness-mcp` uses JSON-RPC 2.0 over stdio with LSP-style Content-Length framing:

```
Content-Length: <byte count>\r\n
\r\n
<JSON body>
```

The server also accepts newline-delimited JSON as a fallback for clients that do not send Content-Length headers. Both formats are handled automatically.

---

## Generic stdio Pattern

Any MCP client that supports stdio servers can launch `awareness-mcp` with:

- **Command**: `awareness-mcp` (or the full path to the binary)
- **Args**: `["--project-root", "/absolute/path/to/your/project"]`
- **Transport**: stdio

The `--project-root` flag must be an absolute path because the subprocess working directory is not guaranteed to match the project root.

---

## Codex Configuration

Codex supports MCP servers via its configuration file. The exact configuration format may vary across Codex versions — consult the Codex documentation for your version. The generic stdio pattern below should work:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/absolute/path/to/your/project"],
      "transport": "stdio"
    }
  }
}
```

A ready-to-copy example is at [examples/mcp/codex/config.example.json](../../examples/mcp/codex/config.example.json).

If your Codex version uses a different configuration schema, the essential parameters are:

- Launch command: `awareness-mcp` (or the full binary path)
- Arguments: `--project-root /absolute/path/to/your/project`
- Transport: stdio

---

## Other MCP Clients

Any client implementing the Model Context Protocol that supports stdio servers can use `awareness-mcp`. The server:

- Responds to the `initialize` method with protocol version `2025-03-26`
- Lists tools via `tools/list`
- Executes tools via `tools/call`
- Returns empty lists for `resources/list` and `prompts/list`
- Responds to `ping` with an empty result

JSON-RPC 2.0 spec: https://www.jsonrpc.org/specification

---

## Installation

```bash
go install github.com/globulario/awareness/cmd/awareness-mcp@v0.1.0
```

Find the binary path for use in configuration:

```bash
which awareness-mcp
# typically: /home/user/go/bin/awareness-mcp
```

Use the full path in your config if the client does not inherit your interactive shell PATH.

---

## Available Tools

All 9 Awareness tools are available regardless of which client is used:

| Tool | Purpose |
|------|---------|
| `awareness_profile_doctor` | Check project profile health |
| `awareness_runtime_status` | Check runtime adapter status |
| `awareness_preflight` | Preflight check for a task or changed files |
| `awareness_context` | Keyword search across knowledge files |
| `awareness_graph_query` | Query compiled graph nodes |
| `awareness_node_context` | Context for a specific file or node ID |
| `awareness_invariant_lookup` | Search invariants by keyword |
| `awareness_failure_mode_lookup` | Search failure modes by keyword |
| `awareness_bundle_inspect` | Inspect a bundle directory |

Full reference: [tools.md](tools.md).

---

## See Also

- [mcp/claude-code.md](claude-code.md) — Claude Code configuration guide
- [mcp/tools.md](tools.md) — Full tool reference
- [concepts/mcp.md](../concepts/mcp.md) — MCP server overview
- [examples/mcp/codex/config.example.json](../../examples/mcp/codex/config.example.json) — Example config

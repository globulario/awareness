# Awareness MCP Server

The Awareness MCP server (`awareness-mcp`) exposes project knowledge — invariants, failure modes, forbidden fixes, preflight, and bundle inspection — as tools to any MCP-compatible AI agent. It does not require a Globular cluster. It works with any project that has a `.awareness.yaml` file.

---

## Protocol

The server uses JSON-RPC 2.0 over stdio with LSP-style Content-Length framing:

```
Content-Length: <byte count>\r\n
\r\n
<JSON body>
```

The server also accepts newline-delimited JSON for compatibility with early MCP clients. Both formats are handled automatically — the client does not need to configure anything.

Protocol version: `2025-03-26` (returned in the `initialize` response).

---

## Starting the Server

```bash
awareness-mcp --project-root /absolute/path/to/my-project
```

When `--project-root` is omitted, the server walks up from the current working directory looking for `.awareness.yaml`.

The server logs startup information to stderr:

```
awareness-mcp: project=my-service kind=application adapter=null root=/home/user/my-service
```

The server then blocks on stdin, ready to receive JSON-RPC 2.0 requests.

---

## Available Tools

| Tool | Purpose |
|------|---------|
| `awareness_profile_doctor` | Static health check on the project profile. Returns resolved paths, file counts, runtime status, and warnings for missing files. |
| `awareness_runtime_status` | Runtime adapter status. Returns `runtime_disabled` for NullAdapter projects — not an error. |
| `awareness_preflight` | Lightweight preflight check for a task or changed files. Returns matched invariants, failure modes, forbidden fixes, and task classification. |
| `awareness_context` | Keyword search across all knowledge files (invariants, failure modes, forbidden fixes). |
| `awareness_graph_query` | Query compiled graph nodes when a graph file exists in the cache directory. |
| `awareness_node_context` | Awareness context for a specific file path or knowledge node ID. |
| `awareness_invariant_lookup` | Search invariants by keyword. Returns full entries with severity and tags. |
| `awareness_failure_mode_lookup` | Search failure modes by keyword. Returns full entries with symptoms and wrong_fixes. |
| `awareness_bundle_inspect` | Inspect a bundle directory. Validates `bundle.json` and returns manifest fields and file list. |

Full reference: [mcp/tools.md](../mcp/tools.md).

---

## NullAdapter Projects

Projects with `runtime.enabled: false` (adapter: null) get `NullAdapter`. For these projects:

- `awareness_runtime_status` returns `{ "status": "runtime_disabled" }` — this is correct and expected
- `awareness_preflight` works fully — static knowledge matching is not affected by the adapter
- `awareness_context`, `awareness_invariant_lookup`, `awareness_failure_mode_lookup` all work fully
- `awareness_graph_query` works if a compiled graph file exists in the cache directory
- `awareness_bundle_inspect` works for any bundle path

`runtime_disabled` is not an error. It means the project does not have a live cluster — which is the correct configuration for all non-Globular projects.

---

## Installation

```bash
go install github.com/globulario/awareness/cmd/awareness-mcp@v0.1.0
```

Verify:

```bash
awareness-mcp --help
```

---

## See Also

- [mcp/tools.md](../mcp/tools.md) — Full tool reference with input schemas and example output
- [mcp/claude-code.md](../mcp/claude-code.md) — Claude Code configuration guide
- [mcp/codex.md](../mcp/codex.md) — Codex and generic stdio configuration

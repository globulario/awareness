# Claude Code MCP Configuration

This guide shows how to configure Claude Code to use the Awareness MCP server. Once configured, all 9 Awareness tools appear in Claude Code's tool list and are called automatically when relevant to the current task.

---

## Prerequisites

Install `awareness-mcp`:

```bash
go install github.com/globulario/awareness/cmd/awareness-mcp@v0.1.0
```

Verify it is on your PATH:

```bash
which awareness-mcp
awareness-mcp --help
```

If `which` returns nothing, add Go binaries to your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

---

## Config File Locations

Claude Code reads MCP server configuration from two locations:

| Location | Scope |
|----------|-------|
| `.mcp.json` in the project root | Project-level — applies when Claude Code is opened in that directory |
| `~/.config/claude/mcp.json` | Global — applies in all projects |

Use `.mcp.json` (project-level) for project-specific awareness configuration. Use `~/.config/claude/mcp.json` if you want awareness available in every project you open.

---

## Project-Level Configuration (`.mcp.json`)

Create `.mcp.json` in your project root. The `--project-root` flag must be an absolute path.

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/absolute/path/to/your/project"]
    }
  }
}
```

Replace `/absolute/path/to/your/project` with the absolute path to the directory containing your `.awareness.yaml` file.

A ready-to-copy example is at [examples/mcp/claude-code/.mcp.json](../../examples/mcp/claude-code/.mcp.json).

---

## Global Configuration (`~/.config/claude/mcp.json`)

For a global setup that works across all projects, use the `--project-root` flag pointing to each project. You can have multiple awareness server entries if you work on multiple projects:

```json
{
  "mcpServers": {
    "awareness-myservice": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/home/user/my-service"]
    },
    "awareness-platform": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/home/user/platform"]
    }
  }
}
```

---

## Dev / `go run` Version

For development or testing without installing the binary, use `go run`:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "go",
      "args": [
        "run",
        "github.com/globulario/awareness/cmd/awareness-mcp@v0.1.0",
        "--project-root",
        "/absolute/path/to/your/project"
      ]
    }
  }
}
```

This downloads and runs the binary on demand. Useful for CI or machines where you do not want a persistent install.

---

## Available Tools

Once configured, Claude Code has access to all 9 Awareness tools:

| Tool | What it does |
|------|-------------|
| `awareness_profile_doctor` | Check project profile health — all paths resolved, files present |
| `awareness_runtime_status` | Check runtime adapter status (`runtime_disabled` for non-Globular projects) |
| `awareness_preflight` | Run preflight for a task or changed files — returns invariants, failure modes, forbidden fixes |
| `awareness_context` | Keyword search across all knowledge files |
| `awareness_graph_query` | Query compiled graph nodes (when a graph file exists) |
| `awareness_node_context` | Get awareness context for a specific file path or node ID |
| `awareness_invariant_lookup` | Search invariants by keyword |
| `awareness_failure_mode_lookup` | Search failure modes by keyword |
| `awareness_bundle_inspect` | Inspect a bundle directory |

Full reference: [tools.md](tools.md).

---

## Important: `--project-root` Must Be Absolute

The `--project-root` flag must be an absolute path. Claude Code starts `awareness-mcp` as a subprocess, and the working directory of that subprocess is not guaranteed to be the project root. A relative path will resolve to the wrong directory or cause a startup error.

Correct:
```json
"args": ["--project-root", "/home/user/my-project"]
```

Wrong:
```json
"args": ["--project-root", "."]
```

---

## Troubleshooting

### Server not starting

Check that `awareness-mcp` is on the PATH that Claude Code uses. Claude Code's subprocess inherits the PATH from the shell that launched it, which may differ from your interactive shell.

Workaround — use the full binary path:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "/home/user/go/bin/awareness-mcp",
      "args": ["--project-root", "/absolute/path/to/your/project"]
    }
  }
}
```

Find the full path with:

```bash
which awareness-mcp
# or
echo "$(go env GOPATH)/bin/awareness-mcp"
```

### `runtime_disabled` in tool output

This is expected for non-Globular projects. Your `.awareness.yaml` has `runtime.enabled: false`, which means the NullAdapter is active. All knowledge tools work normally. `runtime_disabled` is not an error.

### No matches returned from preflight or context

The keyword matching algorithm requires at least 2 keyword matches for an item to appear in `invariants`, `failure_modes`, or `forbidden_fixes`. Check:

1. Do your knowledge files contain relevant `tags`? Tags are weighted in the match algorithm.
2. Is the `awareness.root` path in `.awareness.yaml` correct? Run `awareness_profile_doctor` to verify.
3. Are the invariant/failure mode YAML files listed under `awareness.invariants` and `awareness.failure_modes`? Files not listed in `.awareness.yaml` are not loaded.

To add more context, use `awareness_invariant_lookup` or `awareness_failure_mode_lookup` with a broad query to see what is loaded.

### `.awareness.yaml` not found

The server walks up from `--project-root` looking for `.awareness.yaml`. If that file does not exist, the server exits with an error. Create the file first — see [getting-started.md](../getting-started.md) for a minimal example.

---

## See Also

- [mcp/tools.md](tools.md) — Full tool reference
- [mcp/codex.md](codex.md) — Codex and generic stdio configuration
- [concepts/mcp.md](../concepts/mcp.md) — MCP server overview
- [examples/mcp/claude-code/.mcp.json](../../examples/mcp/claude-code/.mcp.json) — Ready-to-copy config

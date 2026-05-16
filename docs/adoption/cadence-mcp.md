# Cadence Agent Configuration — awareness-mcp

This guide shows how to wire an AI agent (Claude Code, Codex) to the standalone `awareness-mcp` server for the Cadence BPMN engine project.

Cadence uses `NullAdapter` — no Globular cluster is required.

---

## Server Command

```bash
awareness-mcp --project-root /path/to/cadence
```

Or during development from the awareness source:

```bash
go run github.com/globulario/awareness/cmd/awareness-mcp@latest \
  --project-root /path/to/cadence
```

---

## Claude Code Configuration

Add to your `.mcp.json` (project-level) or `~/.config/claude/mcp.json` (global):

```json
{
  "mcpServers": {
    "awareness": {
      "command": "awareness-mcp",
      "args": ["--project-root", "/path/to/cadence"]
    }
  }
}
```

If `awareness-mcp` is not on your `PATH`, use the full binary path:

```json
{
  "mcpServers": {
    "awareness": {
      "command": "/home/dave/go/bin/awareness-mcp",
      "args": ["--project-root", "/home/dave/Documents/github.com/globulario/cadence"]
    }
  }
}
```

---

## Available Tools

| Tool | Description |
|------|-------------|
| `awareness_profile_doctor` | Static profile health check — invariants, failure_modes, forbidden_fixes paths |
| `awareness_preflight` | Pre-edit preflight — task classification, matched invariants and failure modes |
| `awareness_runtime_status` | Adapter status — always `runtime_disabled` for Cadence |
| `awareness_context` | Raw knowledge search across all awareness YAML files |

---

## Expected Runtime Status

For Cadence (adapter: null), every `awareness_runtime_status` call returns:

```json
{
  "project": "cadence",
  "adapter": "null",
  "enabled": false,
  "status": "runtime_disabled",
  "runtime_config": {
    "enabled": false,
    "adapter": "null"
  }
}
```

No Globular bridge is created. No cluster connection is attempted.

---

## Example Preflight Request

Ask the agent to run a preflight before editing gateway code:

```
awareness_preflight({
  "task": "fix exclusive gateway condition evaluation",
  "changed": true
})
```

Expected result excerpt:

```json
{
  "project_name": "cadence",
  "runtime_status": "disabled",
  "invariants": ["process.state.determinism", "gateway.exclusive.single.exit"],
  "failure_modes": ["gateway.dead.branch", "process.infinite.loop"],
  "forbidden_fixes": ["no.wall.clock.in.flow.condition", "no.silent.gateway.skip"],
  "ok": true
}
```

---

## Example Context Request

Ask the agent to search for token-related knowledge:

```
awareness_context({
  "query": "token mutation identity audit"
})
```

Expected result excerpt:

```json
{
  "project": "cadence",
  "match_count": 4,
  "matches": [
    {"kind": "invariant", "id": "token.immutability", "score": 5},
    {"kind": "forbidden_fix", "id": "no.token.id.mutation", "score": 4},
    {"kind": "invariant", "id": "event.history.immutability", "score": 3},
    {"kind": "failure_mode", "id": "concurrent.token.collision", "score": 2}
  ]
}
```

---

## Troubleshooting

**`awareness-mcp` not found**
Install with: `go install github.com/globulario/awareness/cmd/awareness-mcp@latest`

**Profile resolve error**
Check that `/path/to/cadence/.awareness.yaml` exists and contains `name`, `kind`, and `runtime.enabled: false`.

**No matches from `awareness_context`**
The query must contain tokens that appear in the awareness YAML files. Try keywords from the invariant IDs (e.g. "gateway", "token", "timer").

**tools/list returns empty**
The server started but could not resolve the profile. Check stderr for `resolve profile:` errors.

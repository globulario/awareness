# Project Profile

A `ProjectProfile` is the single struct that all Awareness commands and MCP tools consume. It describes what the project is, where its knowledge files live, whether a runtime adapter is active, and where graph caches are stored.

Every project that uses Awareness carries a `.awareness.yaml` file at its root. The `awareness` CLI and `awareness-mcp` server resolve this file at startup by walking up the directory tree from the current working directory.

---

## How ResolveProfile Works

`ResolveProfile` is the Go function that turns `.awareness.yaml` into a `ProjectProfile`. Discovery rules:

1. If `--project-root PATH` is passed, load `.awareness.yaml` directly from that directory.
2. Otherwise, walk upward from the current working directory, checking each directory for `.awareness.yaml`.
3. Stop at the nearest `.git` root — never cross the repository boundary.
4. Return a clear error when no `.awareness.yaml` is found.
5. After loading, resolve all relative paths against the project root so that paths in the struct are always absolute.

This means you can run `awareness preflight` or start `awareness-mcp` from any subdirectory of a project; the tool will find the profile at the root.

---

## `.awareness.yaml` Fields

### Top-level

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Project identifier. Used in tool output, bundle manifests, and session keys. |
| `kind` | string | yes | Project classification. See [Kind Values](#kind-values) below. |
| `source_roots` | list of strings | no | Directories containing project source code (relative to project root). Informational — used by graph builders. |
| `root_markers` | list of strings | no | Supplementary signals for project root detection (e.g. `go.mod`, `package.json`). |

### `languages`

Controls which language categories awareness tooling considers when scanning for patterns.

```yaml
languages:
  go: true
  typescript: false
  proto: false
  yaml: true
  markdown: true
```

### `awareness`

Paths to the project's knowledge files. All paths are relative to the project root.

| Field | Type | Description |
|-------|------|-------------|
| `root` | string | The `.awareness/` directory. Base for relative knowledge paths. |
| `invariants` | list of strings | YAML files containing invariant definitions. |
| `failure_modes` | list of strings | YAML files containing failure mode definitions. |
| `forbidden_fixes` | list of strings | YAML files containing forbidden fix definitions. |
| `causal_rules` | list of strings | Optional YAML files containing causal rule chains. |
| `context_aliases` | list of strings | Optional alias/shortcut files for context lookups. |
| `decisions_dir` | string | Directory where agent decisions are recorded. |
| `proposals_dir` | string | Directory where change proposals are staged. |

### `runtime`

Controls the runtime adapter. For all non-Globular projects, set `enabled: false` and `adapter: null`.

```yaml
runtime:
  enabled: false
  adapter: null
```

When `enabled` is true and no `adapter` is specified, the default is `globular`. See [runtime-adapters.md](runtime-adapters.md).

### `graph`

Graph cache configuration.

| Field | Type | Description |
|-------|------|-------------|
| `cache_dir` | string | Directory where compiled graph files (`graph.db`, `graph.json`) are stored. |
| `freshness_ttl` | duration string | How long a cached graph is considered fresh (e.g. `24h`, `1h`). |
| `invalidate_on` | list of strings | File patterns that invalidate the graph cache when changed. |

### `mcp`

Controls how the MCP server resolves project context.

```yaml
mcp:
  project_aware: true
  resolve_profile_from: "."
```

---

## Example `.awareness.yaml`

This example covers a typical Go service with no Globular runtime:

```yaml
name: payment-service
kind: application

languages:
  go: true
  yaml: true
  markdown: true

source_roots:
  - internal
  - cmd

awareness:
  root: .awareness
  invariants:
    - .awareness/invariants.yaml
  failure_modes:
    - .awareness/failure_modes.yaml
  forbidden_fixes:
    - .awareness/forbidden_fixes.yaml
  decisions_dir: .awareness/decisions
  proposals_dir: .awareness/proposals

runtime:
  enabled: false
  adapter: null

graph:
  cache_dir: .awareness/cache
  freshness_ttl: 24h
  invalidate_on:
    - "*.go"
    - ".awareness/*.yaml"
```

For a project with multiple knowledge files (e.g. one per subsystem):

```yaml
name: platform
kind: distributed-platform

languages:
  go: true
  proto: true
  yaml: true

source_roots:
  - golang
  - proto

awareness:
  root: .awareness
  invariants:
    - .awareness/invariants.yaml
    - .awareness/invariants-storage.yaml
    - .awareness/invariants-network.yaml
  failure_modes:
    - .awareness/failure_modes.yaml
  forbidden_fixes:
    - .awareness/forbidden_fixes.yaml
  decisions_dir: .awareness/decisions
  proposals_dir: .awareness/proposals

runtime:
  enabled: true
  adapter: globular

graph:
  cache_dir: .awareness/cache
  freshness_ttl: 1h
```

---

## Kind Values

The `kind` field classifies the project for reasoning purposes. Awareness does not enforce a closed enum — any string is accepted — but these are the well-known values:

| Kind | Description |
|------|-------------|
| `generic` | No specific domain. Use when nothing else fits. |
| `application` | A deployable service or application. |
| `library` | A reusable library or SDK. |
| `bpmn-workflow-engine` | A BPMN or workflow orchestration engine. |
| `distributed-platform` | A multi-node distributed system (e.g. Globular). |

The kind appears in bundle manifests and MCP tool output. Agents can use it to adjust reasoning: a `distributed-platform` project warrants more caution around state mutations than a `library`.

---

## Verifying Your Profile

Run the profile doctor to check that all configured paths exist and the profile is valid:

```bash
awareness profile doctor
```

Or with an explicit project root:

```bash
awareness profile doctor --project-root /path/to/my-project
```

Example output when everything is correct:

```
project: payment-service
kind:    application
root:    /home/user/payment-service
profile: /home/user/payment-service/.awareness.yaml
runtime: disabled

status: ok
```

If a configured file is missing, the doctor will report it as a warning so you can fix the path before agents rely on it.

---

## See Also

- [invariants.md](invariants.md) — Writing invariants
- [failure-modes.md](failure-modes.md) — Writing failure modes
- [forbidden-fixes.md](forbidden-fixes.md) — Writing forbidden fixes
- [runtime-adapters.md](runtime-adapters.md) — Runtime adapter reference
- [examples/go-service](../../examples/go-service/.awareness.yaml) — Complete working example

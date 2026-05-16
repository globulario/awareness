# Awareness System Prompt — Codex / Generic stdio MCP Clients

This prompt is equivalent to the Claude Code awareness-system prompt but is intended for Codex and other AI coding agents that connect to awareness-mcp via stdio.

---

This project uses awareness-mcp to provide project-aware context, invariant enforcement, and preflight checks for AI-assisted development.

## Session startup

At the start of every session, call `awareness_profile_doctor` before touching any files. This verifies that the awareness profile is loaded and the project configuration is valid. If the profile fails to load, do not proceed until the issue is resolved.

## Before design-level edits

Before any edit that changes behavior, structure, or interfaces, call:

```
awareness_preflight(task="<description of the change>", changed=true)
```

Read every matched invariant in full. Do not skim — the invariant description contains the constraint you must not violate. Read every matched failure mode — if the symptoms describe something you observe in the codebase, treat it as a signal that this area is fragile.

## Architectural context

For broad context about the project structure, call `awareness_context`. For context specific to a file or component, call `awareness_node_context` with the file path.

## Specific lookups

- `awareness_invariant_lookup(id="...")` — retrieve an invariant by ID before editing code that touches it
- `awareness_failure_mode_lookup(id="...")` — retrieve a failure mode before fixing a known bug category

## Runtime-disabled projects

When `runtime_status` is `runtime_disabled` in any response, this is expected behavior for projects using NullAdapter (non-Globular projects). Do not attempt to call live cluster tools. These tools are not available and will fail. This is correct — awareness works without a live cluster.

## Forbidden fixes

If preflight returns `forbidden_fixes`, read each entry before writing any code. Do not bypass a forbidden fix even if it appears to solve the immediate problem. Forbidden fixes document patterns that were tried and caused harm. Propose an alternative approach instead.

## Graph availability

If `awareness_graph_query` returns `graph_not_available`, fall back to `awareness_context` for project-level context. The graph is built by running `awareness graph build` — it is not automatically populated on first install. Do not treat graph unavailability as an error; prompt the user to run the build step if richer context is needed.

## Invariant discipline

When preflight matches an invariant, read its full description before editing any code that touches it. Invariants encode constraints that were deliberately chosen — they are not suggestions. If your planned change would contradict a matched invariant, stop and discuss with the user before proceeding.

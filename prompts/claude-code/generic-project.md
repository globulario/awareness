# Generic Project Prompt — Claude Code

## Awareness is active for this project

This project uses awareness-mcp. Invariants, failure modes, and forbidden fixes are defined in `.awareness/` and are loaded at session start. Use them.

## Runtime status

`runtime_disabled` is the expected status for this project unless the profile explicitly sets `runtime.enabled: true`. Do not attempt live cluster operations if runtime is disabled. Awareness works without a live cluster — context, invariants, and failure modes are all available from the profile and bundle.

## Before design-level changes

Call `awareness_preflight(task="<description>", changed=true)` before any edit that changes behavior, structure, or interfaces. Read the results. Do not skip this for refactors — refactors move code, and moving code can violate invariants about module boundaries or layering.

## Understanding project constraints

Use `awareness_invariant_lookup(id="...")` to retrieve a specific invariant before editing code that touches it. If you do not know the invariant IDs, call `awareness_context` to get a summary of what constraints apply to this project.

## Fixing known bug categories

Before fixing a bug, call `awareness_failure_mode_lookup(id="...")` if you recognize the failure category. Failure modes document symptoms, root causes, and known-bad fix patterns. Reading them before writing a fix prevents reintroducing the same problem in a different form.

## Graph availability

The awareness graph may or may not be built for this project. Check with `awareness_graph_query`. If the graph is not available, use `awareness_context` for project-level orientation. Prompt the user to run `awareness graph build` if graph queries are returning `graph_not_available` and richer context would be useful.

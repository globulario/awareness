# Cadence Project Prompt — Claude Code

## What Cadence is

Cadence is a BPMN workflow engine. It manages process execution, token routing, and task state machines. Determinism is the core architectural constraint: a process instance must produce identical results when replayed from its event log.

## Runtime status

Cadence uses NullAdapter. `runtime_disabled` will always appear in awareness tool responses. This is correct — do not attempt Globular cluster operations. Do not call `cluster_get_health`, `live_service_health`, or any tool that requires a live cluster. The awareness graph is available; cluster state is not.

## Key invariants

- `process.state.determinism` — replay must produce the same sequence of states and token placements as the original execution. Any code that introduces non-determinism (wall clock reads, random values, external I/O) inside a flow handler violates this invariant.
- `token.lifecycle.explicit` — tokens must be explicitly created and consumed. Implicit token creation (e.g. as a side effect of a state transition) is not allowed.
- `task.state.machine.enforced` — tasks follow a strict state machine. Transitions that skip states or allow backward movement are not permitted.

## Key failure modes

- `replay.divergence.on.nondeterminism` — if a flow handler reads wall clock time, generates a random value, or performs blocking I/O, replay will diverge from the original execution. Symptoms: process instances produce different outputs on replay; test assertions fail intermittently.
- `token.leak.on.gateway.timeout` — if a gateway times out without explicitly consuming all tokens on its incoming paths, those tokens remain active and the process hangs. Symptoms: process instance never reaches a terminal state; token count grows over time.

## Forbidden fixes

- `no.wall.clock.in.flow.condition` — do not use `time.Now()`, `time.Since()`, or any wall clock call inside a flow condition or gateway handler. Use the process clock provided by the engine instead.
- `no.blocking.io.in.flow.handler` — do not perform synchronous HTTP calls, database queries, or file reads inside a flow handler. Delegate to task handlers or service tasks.

## Graph and context tools

The awareness graph is available. Use `awareness_graph_query` for navigating component relationships. Use `awareness_node_context` with a file path for invariant and failure mode context on specific files. Always call `awareness_preflight` before editing gateway or token handling code — these areas touch both `process.state.determinism` and `token.lifecycle.explicit`.

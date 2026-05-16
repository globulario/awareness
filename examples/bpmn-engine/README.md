# Example: BPMN Engine

This example shows how to add Awareness to a BPMN workflow engine.

## Invariants

- **process.state.determinism** — Execution must be deterministic.
- **token.lifecycle.explicit** — Token creation and destruction must be logged.
- **task.state.machine.enforced** — Task state transitions must be validated.
- **subscription.cleanup.guaranteed** — Event subscriptions cleaned up on completion.

## Quick start

```bash
awareness profile doctor  --project-root .
awareness preflight       --task "fix exclusive gateway routing"
awareness bundle build    --out /tmp/bpmn-engine-bundle
```

## Source structure

```
engine/gateway.go  — exclusive gateway evaluation with explicit error on no-match
```

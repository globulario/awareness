# Example: TypeScript App

This example shows how to add Awareness to a TypeScript frontend application.

## Invariants

- **state.immutability** — Component state updated only through setters.
- **api.boundary.typed** — API responses validated against TypeScript types.
- **component.data.separation** — Components are presentation-only; data logic in hooks/stores.
- **error.surface.to.user** — Every async error displayed to the user.

## Quick start

```bash
awareness profile doctor  --project-root .
awareness preflight       --task "add new TodoList component"
awareness bundle build    --out /tmp/typescript-app-bundle
```

## Source structure

```
src/components/TodoList.ts  — typed fetch, immutable state update, error surfacing
```

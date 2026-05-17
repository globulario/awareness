# Frontend Awareness

## Definition

Frontend awareness is the part of the Globular awareness system that protects **operator truth**.

Backend awareness asks:

```
Is the system correct?
```

Frontend awareness asks:

```
Is the screen telling the truth about the system?
```

A screen is correct only when it:

```
Displays the correct state
from the correct authority
with the correct permission boundary
through a usable visual layout
without hiding risk, failure, or uncertainty.
```

Core rule:

```
Frontend awareness protects operator truth.
```

---

## Architecture: What Calls Awareness

Frontend awareness is an **AI-editing safety layer**, not a browser runtime.

```
frontend code        = subject being analyzed
awareness graph      = compiled knowledge about that code
awareness MCP tools  = interface used by AI agents before edits
AI agent             = consumer of awareness
browser/frontend     = not the consumer
```

This mirrors backend awareness:

```
The cluster does not call awareness.
The AI agent calls awareness before changing cluster code.

The browser does not call awareness.
The AI agent calls awareness before changing frontend code.
```

Therefore:

```
NO frontend SDK.
NO HTTP awareness API.
NO awareness calls inside React/Svelte/Vue components.
NO browser dependency on the awareness engine.
```

---

## Why This Exists

Most dangerous frontend bugs are not rendering bugs. They are **perception bugs**:

```
A green badge is derived from desired state instead of runtime state.
A button is enabled without checking RBAC.
A loading spinner hides a real backend failure.
A success toast fires after click, before backend confirmation.
A destructive warning disappears on mobile.
A workflow failure is replaced by a generic frontend error.
An AI explanation invents state the backend never reported.
```

Frontend correctness is not only:

```
Does the component render?
```

It is:

```
Does the screen display the correct truth, from the correct authority,
in a usable and safe form?
```

---

## State Authority Rules

These authority boundaries must not be violated.

| Claim | Authority |
|---|---|
| What is available | Repository/catalog state |
| What the system intends | Desired state |
| What was installed | Installed state |
| What is actually alive | Runtime state |
| What action happened | Workflow receipts |
| What is unsafe/degraded | Doctor findings |
| What the user may do | RBAC |
| Who did what and when | Audit logs |

Forbidden UI lies:

```
Do not show runtime healthy because desired state says enabled.
Do not show installed because the repository catalog contains the package.
Do not show success because a button was clicked.
Do not hide workflow failure behind a generic frontend error.
Do not enable destructive action without permission and explicit risk state.
Do not let skin/theme/layout changes remove warnings.
Do not let AI-generated prose invent status, success, failure, or required action.
```

Primary invariant:

```
UI status must be bound to explicit state authority.
```

---

## Relationship to Existing Awareness

Frontend awareness is **coverage extension**, not a new engine.

The existing model already provides:

```
declared knowledge  → YAML invariants, failure modes, forbidden fixes
discovered reality  → scanner / graph nodes / source files
AI preflight        → awareness_preflight, awareness_context, node context
runtime optionality → NullAdapter-compatible runtime layer
project profiles    → .awareness.yaml
```

Frontend adds:

```
declared frontend contracts  → UI-specific invariants, failure modes, forbidden fixes
discovered TypeScript reality → tsast scanner, frontend graph nodes
frontend MCP tools           → frontend_trace_component, frontend_explain_screen,
                                frontend_plan_feature, frontend_verify_change
```

---

## Declared vs Discovered Graph

### Declared Graph

Written as YAML contracts. Defines what the UI *means*:

```
screen intent
state authority
forbidden UI lies
required error behavior
permission rules
visual contracts
journey expectations
AI-generated text constraints
```

Lives in `.awareness/invariants.yaml`, `.awareness/failure_modes.yaml`,
`.awareness/forbidden_fixes.yaml` — same files, frontend-scoped IDs.

### Discovered Graph

Generated from TypeScript/React source code. Defines what *exists*:

```
files
exported components
routes
backend client calls
state atoms (useState, useSelector, signals)
custom hooks
permission checks
test files
story files
layout signals (responsive CSS, aria, overflow)
```

The discovered graph is reality. The declared graph is intent. Awareness connects both.

---

## Graph Node and Edge Types

### Node kinds

```
frontend_component
frontend_route
frontend_backend_call
frontend_state_atom
frontend_hook
frontend_permission_check
frontend_test
frontend_story
frontend_visual_contract
frontend_screen_contract
frontend_journey_contract
```

### Edge kinds

```
file_defines_component
component_reads_state
component_calls_backend
component_checks_permission
route_renders_component
component_uses_hook
test_covers_component
story_renders_component
```

---

## MCP Tools

Four frontend tools are available in `awareness-mcp`.

### `frontend_trace_component`

Returns everything awareness knows about a frontend component: graph nodes,
backend calls, state atoms, permission checks, related invariants, failure modes,
and forbidden fixes.

```json
{ "component": "ServiceStatusCard", "include_contracts": true }
```

### `frontend_explain_screen`

Explains what a route is supposed to display and what it must not lie about.
Returns truth claims, state authorities, must-show items, and forbidden behaviors.

```json
{ "route": "/admin/objectstore/topology" }
```

### `frontend_plan_feature`

Given a feature intent, generates proposed awareness contracts before any code
is written. Returns draft screen, component, journey, and visual contracts.
Does not write files.

```json
{ "intent": "Add package install screen with workflow progress and RBAC-gated install button" }
```

### `frontend_verify_change`

Checks changed frontend files against frontend awareness before committing.
Returns matched invariants, forbidden fixes, required tests, and a
`allow|warn|block` verdict.

```json
{
  "files": "src/pages/ObjectStoreTopologyPage.tsx,src/components/StatusBadge.tsx",
  "task": "Refactor objectstore topology UI"
}
```

---

## Visual and Layout Awareness

Layout bugs become operator-safety bugs when they remove visibility of risk.

Example:

```
If a destructive action warning disappears on mobile,
the UI violates operator truth even if the button still works.
```

Frontend awareness protects:

```
responsive layout
critical warning visibility
modal stacking
scroll behavior
table overflow
empty states
loading states
error banners
mobile usability
accessibility labels
```

Screenshots and visual regression tests are part of awareness coverage.

---

## AI Explanation Contracts

When AI generates user-facing text, it must bind to backend truth.

```yaml
may_generate:
  - humorous explanation
  - fictional department name
  - ceremonial wording
must_bind_to:
  - real workflow status
  - real defer_until
  - real required action
forbidden:
  - invent success
  - hide failure
  - fabricate required action
```

Core rule:

```
AI may decorate the explanation.
AI must not invent the state.
```

---

## Skin and Theme Contracts

Invariant:

```
Skins may change presentation.
Skins must not change meaning.
```

A theme that hides a warning, reduces contrast on a critical badge, or removes
an error panel violates frontend awareness even if it renders correctly.

---

## Non-Goals (v1)

```
No browser runtime awareness client.
No HTTP server for frontend awareness.
No new database.
No complete TypeScript compiler integration.
No visual regression engine.
No screenshot diff engine.
No dependency on a live cluster.
No admin UI implementation.
```

Frontend awareness v1 is the map that keeps AI from lying while editing the UI.

---

## Implementation Sequence

1. Add `.awareness.yaml` to the frontend project — same format as backend
2. Write `.awareness/invariants.yaml` — UI state authority rules
3. Write `.awareness/failure_modes.yaml` — known UI lies and perception bugs
4. Write `.awareness/forbidden_fixes.yaml` — forbidden patterns per UI kind
5. TypeScript scanner (`scan/tsast/`) — extracts components, routes, calls, state
6. Graph builder — wire scanner output as `frontend_*` nodes and edges
7. MCP tools — `frontend_trace_component`, `frontend_explain_screen`,
   `frontend_plan_feature`, `frontend_verify_change`

---

## Canonical First Screen

Start with the most dangerous screen: **ObjectStore topology**.

```
Route:  /admin/objectstore/topology
Screen: ObjectStoreTopologyPage
```

Must show:

```
desired topology
applied generation
runtime health
destructive transition approval
wipe risk
node membership
manual action required
doctor findings
```

Forbidden:

```
infer applied generation from desired state
hide destructive wipe risk
show healthy when runtime authority is unknown
enable topology apply without permission
collapse manual action required reason
hide warning on mobile
```

If AI can refactor that screen without hiding risk, breaking layout, or inventing
state, frontend awareness is proven.

---

## Final Principle

```
Create the law of the page before creating the page.
```

```
Frontend awareness protects operator truth.
```

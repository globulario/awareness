# Claude Task: Project-Scoped Awareness Implementation Plan

You are working on **Globular Awareness**, the code-awareness / operational-awareness system currently living inside or around the `services` project.

The current Awareness system scans artifacts from the `services` repo and writes/reads awareness YAML files under:

```text
docs/awareness/
```

It provides capabilities such as graph building, preflight checks, invariant discovery, forbidden-fix checks, failure-mode reasoning, context navigation, and possibly runtime overlays.

This worked while Awareness was mainly helping the `services` backend. But the platform is now expanding into multiple projects:

```text
services
cadence
lib-bpmn-engine fork
globular-admin
globular-installer
globular-quickstart
future Globular/Cadence modules
```

Each project needs different invariants, failure modes, forbidden fixes, source roots, extractors, runtime adapters, and preflight behavior.

The goal is to evolve Awareness from a `services`-specific tool into a **project-scoped, profile-driven, standalone binary**.

---

## Mission

Define an implementation plan to extract or refactor Awareness into a reusable project-aware tool.

The desired final shape is:

```text
one Awareness binary
many project configs
project-local docs/awareness artifacts
optional runtime adapters
same graph/preflight/enforce engine
```

Awareness should become portable.

Example usage:

```bash
cd ~/dev/services
awareness preflight --files repository/pkg_info_cmd.go

cd ~/dev/cadence
awareness preflight --files internal/cadence/bpmn/runtime/service_task.go

cd ~/dev/lib-bpmn-engine
awareness preflight --files pkg/bpmn_engine/engine.go
```

The binary should automatically detect the current project profile and load the correct awareness artifacts.

---

## Current Assumption

Today, Awareness appears to be tied to the `services` project and its artifact layout:

```text
services/
  docs/awareness/
    invariants.yaml
    failure_modes.yaml
    forbidden_fixes.yaml
    causal_rules.yaml
    decisions/
    proposals/
```

Your first task is to inspect the current Awareness code and confirm:

```text
where paths are hardcoded
where services-specific assumptions exist
where artifact loading happens
where source scanning happens
where preflight starts
where graph cache/output paths are defined
where runtime adapters are wired
where CLI entrypoints exist
```

Do not rewrite the system blindly. Identify the seams first.

---

## Desired New Concept: Project Profile

Introduce a project-local config file:

```text
.awareness.yaml
```

or, if you strongly prefer a visible filename:

```text
awareness.yaml
```

Recommended default: `.awareness.yaml`.

The binary should discover it by walking upward from the current working directory.

Example:

```yaml
project:
  name: cadence
  kind: application
  root: .

awareness:
  artifact_dir: docs/awareness
  graph_dir: .awareness/graph
  cache_dir: .awareness/cache
  session_dir: .awareness/sessions

sources:
  include:
    - internal
    - pkg
    - proto
    - docs
  exclude:
    - vendor
    - node_modules
    - dist
    - build
    - .git
    - gen
    - generated

languages:
  go: true
  proto: true
  yaml: true
  markdown: true
  typescript: false

artifacts:
  invariants: docs/awareness/invariants.yaml
  failure_modes: docs/awareness/failure_modes.yaml
  forbidden_fixes: docs/awareness/forbidden_fixes.yaml
  causal_rules: docs/awareness/causal_rules.yaml
  decisions: docs/awareness/decisions
  proposals: docs/awareness/proposals

preflight:
  default_mode: graph
  require_invariant_match: false
  warn_on_unknown_impact: true
  max_context_nodes: 80
  freshness_ttl: 24h

runtime:
  enabled: false
  adapters: []

output:
  format: markdown
```

For `services`, runtime can be enabled:

```yaml
runtime:
  enabled: true
  adapters:
    - doctor
    - controller
    - workflows
    - systemd
    - etcd
```

For `cadence`, `lib-bpmn-engine`, or frontend repos, static-only awareness is enough at first:

```yaml
runtime:
  enabled: false
```

---

## Desired Internal Type

Define a central `ProjectProfile` or equivalent:

```go
type ProjectProfile struct {
    Project   ProjectConfig
    Awareness AwarenessConfig
    Sources   SourceConfig
    Languages LanguageConfig
    Artifacts ArtifactConfig
    Preflight PreflightConfig
    Runtime   RuntimeConfig
    Output    OutputConfig
}
```

Every command should start by resolving this profile:

```text
1. find .awareness.yaml upward from cwd
2. resolve project root
3. resolve paths relative to project root
4. load docs/awareness artifacts
5. scan configured source roots
6. build/load graph
7. run preflight/enforce/context/doctor
```

---

## Desired CLI

Create or refactor a standalone binary:

```text
cmd/awareness/main.go
```

Commands should eventually include:

```text
awareness init
awareness doctor
awareness scan
awareness build-graph
awareness preflight --files <file1,file2>
awareness preflight --changed
awareness enforce
awareness context --symbol <symbol>
awareness context --file <path>
awareness explain --invariant <id>
```

For the first implementation, prioritize:

```text
doctor
scan
build-graph
preflight --files
```

---

## Desired Repository Split

Give a recommendation on whether Awareness should become its own repository.

Preferred direction:

```text
globulario/awareness
```

or:

```text
globulario/globular-awareness
```

The likely target shape:

```text
globulario/awareness
  cmd/awareness
  internal/config
  internal/graph
  internal/preflight
  internal/enforce
  internal/extractors
  internal/failuregraph
  internal/sessionoracle
  internal/semanticdiff
  internal/sourceroot
  internal/report
  templates
  docs
  examples

globulario/services
  .awareness.yaml
  docs/awareness/*.yaml

globulario/cadence
  .awareness.yaml
  docs/awareness/*.yaml

globulario/lib-bpmn-engine
  .awareness.yaml
  docs/awareness/*.yaml
```

But inspect the current code before recommending final extraction mechanics.

The goal is extraction, not rewrite.

---

## Project-Specific Awareness

The tool must allow each project to carry its own invariants.

Examples for `cadence`:

```yaml
- id: cadence.bpmn.definition_no_runtime_state
  statement: BPMN definitions must not contain Cadence runtime state.

- id: cadence.bpmndi.separate_from_semantics
  statement: BPMNDI diagram layout must remain separate from the BPMN semantic model.

- id: cadence.bpmn.fork_types_internal_only
  statement: Forked BPMN engine types must not cross the Cadence runtime boundary.

- id: cadence.bpmn.service_task_async
  statement: ServiceTask execution must be asynchronous through the Cadence ServiceDispatcher.

- id: cadence.bpmn.trace_required
  statement: Runtime transitions must emit deterministic TraceEvents.
```

Examples for `lib-bpmn-engine`:

```yaml
- id: bpmn.engine.no_product_api_leak
  statement: The forked engine must remain a BPMN mechanics library and not grow Cadence product APIs.

- id: bpmn.engine.boundary_events_tested
  statement: Boundary event behavior must be covered by deterministic BPMN fixture tests.

- id: bpmn.engine.manual_task_supported
  statement: ManualTask must not panic and must behave like a human-completed task.
```

Examples for `services` remain infrastructure-focused:

```yaml
- id: globular.repository.desired_installed_runtime_separation
  statement: Repository, desired, installed, and runtime state must remain separate authorities.

- id: globular.objectstore.contract_required
  statement: MinIO/object-store runtime behavior must be governed by the objectstore contract.

- id: globular.workflow.trace_required
  statement: Workflow transitions must produce bounded and auditable trace records.
```

The plan must explain how the artifact loader supports different project domains without hardcoding domain-specific logic.

---

## Required Investigation

Before writing the plan, inspect the current Awareness code and answer:

```text
1. What package currently owns artifact loading?
2. What package currently owns source root detection?
3. What package currently owns preflight execution?
4. What package currently owns graph persistence/cache?
5. What CLI entrypoints exist today?
6. Where are services-specific assumptions hardcoded?
7. What can move directly into a standalone repo?
8. What must stay in services as project-local artifacts?
9. What runtime adapters are services-specific?
10. What is the smallest extraction that proves the design?
```

---

## Implementation Plan Required Output

Produce a plan with these sections:

```text
1. Current architecture assessment
2. Services-specific coupling points
3. Proposed project profile schema
4. CLI design
5. Package refactor/extraction plan
6. Artifact loading changes
7. Source scanning changes
8. Graph/cache path changes
9. Runtime adapter strategy
10. Project templates
11. Migration plan for services
12. Bootstrap plan for cadence and lib-bpmn-engine
13. Test plan
14. CI integration plan
15. Risks and open questions
16. Phase-by-phase roadmap
```

---

## Suggested Phase Roadmap

### Phase 0: Discovery

Inspect the current code and document coupling points.

Deliverable:

```text
docs/awareness_project_scoping_assessment.md
```

### Phase 1: Config/Profile Loader

Implement:

```text
.awareness.yaml discovery
ProjectProfile struct
relative path resolution
profile validation
awareness doctor command
```

Acceptance:

```bash
awareness doctor
```

works in `services`.

### Phase 2: Source/Artifact Path Abstraction

Replace hardcoded paths with profile-driven paths.

Acceptance:

```bash
awareness scan
awareness build-graph
```

work in `services` using `.awareness.yaml`.

### Phase 3: Standalone Binary

Create:

```text
cmd/awareness
```

Acceptance:

```bash
awareness preflight --files <file>
```

works from the `services` repo.

### Phase 4: Multi-Project Proof

Create minimal `.awareness.yaml` and `docs/awareness/invariants.yaml` for:

```text
cadence
lib-bpmn-engine
```

Acceptance:

```bash
cd cadence
awareness doctor
awareness preflight --files internal/cadence/bpmn/runtime/service_task.go

cd lib-bpmn-engine
awareness doctor
awareness preflight --files pkg/bpmn_engine/engine.go
```

Both work without services-specific runtime adapters.

### Phase 5: Repo Extraction

Move generic Awareness code to a separate repo if not already done.

Keep project artifacts in their own repos.

Acceptance:

```text
services depends on awareness binary/tooling
cadence depends on awareness binary/tooling
lib-bpmn-engine depends on awareness binary/tooling
no project requires awareness code copied locally
```

---

## Test Requirements

Add tests for:

```text
profile discovery upward from cwd
relative path resolution
missing config error
missing artifact warning
bad YAML error
source include/exclude filtering
runtime disabled behavior
runtime adapter unavailable behavior
preflight uses project-local artifacts
graph/cache path changes per project
```

Also add fixture projects:

```text
testdata/projects/services-like
testdata/projects/cadence-like
testdata/projects/library-like
```

Each fixture should have its own `.awareness.yaml`.

---

## Non-Goals

Do not rewrite the graph engine.

Do not rewrite preflight logic unless necessary.

Do not merge all project artifacts into the Awareness repo.

Do not require runtime adapters for every project.

Do not make Cadence depend on `services/docs/awareness`.

Do not make project profiles global machine state.

Do not hardcode project names like `services`, `cadence`, or `lib-bpmn-engine` into the generic tool.

---

## Acceptance Criteria

The implementation plan is acceptable if it provides a concrete path to:

```text
1. Build one Awareness binary.
2. Run that binary inside services using services/.awareness.yaml.
3. Run that binary inside cadence using cadence/.awareness.yaml.
4. Run that binary inside lib-bpmn-engine using lib-bpmn-engine/.awareness.yaml.
5. Keep docs/awareness artifacts project-local.
6. Keep runtime adapters optional.
7. Avoid hardcoded services paths.
8. Preserve existing preflight behavior for services.
9. Add project profile validation and doctor output.
10. Support future CI usage with awareness preflight --changed.
```

---

## Final Principle

Awareness should become repo-local, profile-driven, and binary-distributed.

The `services` repo should become one Awareness profile, not the whole universe.

Each serious project should carry its own little nervous system in `docs/awareness`, while the binary provides the shared brainstem.

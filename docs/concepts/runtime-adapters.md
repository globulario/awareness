# Runtime Adapters

A runtime adapter is the bridge between the generic Awareness engine and a project's live running system. Awareness core knows nothing about any specific platform — all platform-specific live data flows in through the `Adapter` interface.

---

## The Adapter Interface

```go
type Adapter interface {
    // Name returns the adapter identifier ("null", "globular", etc.).
    Name() string
    // Enabled returns true when the adapter has a live system to query.
    Enabled() bool

    // Doctor returns a health report from the runtime system.
    Doctor(ctx context.Context, profile *project.ProjectProfile) (*DoctorReport, error)

    // CollectFacts gathers live operational facts from the runtime.
    CollectFacts(ctx context.Context, profile *project.ProjectProfile) ([]Fact, error)

    // CollectEvidence returns evidence matching the query.
    CollectEvidence(ctx context.Context, profile *project.ProjectProfile, query EvidenceQuery) ([]Evidence, error)

    // CollectSignals gathers an adapter-agnostic set of live runtime signals.
    // This is the primary method used by preflight and other generic paths.
    CollectSignals(ctx context.Context, profile *project.ProjectProfile, opts SignalOptions) (*RuntimeSignals, error)
}
```

All methods receive the resolved `ProjectProfile` so the adapter can scope its queries to the correct project.

---

## NullAdapter

`NullAdapter` is the default adapter for projects that do not require live runtime integration. It satisfies the `Adapter` interface with empty, non-error results.

### What NullAdapter Returns

| Method | Return value |
|--------|-------------|
| `Name()` | `"null"` |
| `Enabled()` | `false` |
| `Doctor()` | `DoctorReport{Adapter: "null", Enabled: false, Status: "runtime_disabled", Findings: nil}` |
| `CollectFacts()` | `nil, nil` |
| `CollectEvidence()` | `nil, nil` |
| `CollectSignals()` | `&RuntimeSignals{}, nil` |

`runtime_disabled` is not an error condition. It means the project is configured to run without live cluster data. All knowledge tools (preflight, invariant lookup, failure mode lookup, context, bundle inspect) work normally with `NullAdapter`.

### When to Use NullAdapter

Use `NullAdapter` — and set `runtime.enabled: false` in `.awareness.yaml` — for:

- Any project that is not a Globular cluster (all third-party projects)
- Local development without a live cluster
- CI pipelines that only need static knowledge checks
- Libraries, applications, and any project without a running cluster to query

```yaml
# In .awareness.yaml:
runtime:
  enabled: false
  adapter: null
```

---

## GlobularAdapter

`GlobularAdapter` provides live cluster signals for Globular platform projects. It lives in the Globular services repository (`services/golang/awareness/`) and is not part of the standalone `awareness` module.

The `GlobularAdapter`:
- Connects to the Globular MCP server running on the cluster
- Collects service health, workflow receipts, state deltas, systemd unit states, and objectstore status
- Returns findings that cross-reference project invariants and failure modes
- Is only available when `awareness-mcp` is run from within the Globular services repository with the appropriate cluster configuration

### How GlobularAdapter is Loaded

The standalone `awareness-mcp` binary uses a registry to look up adapters by name. When `adapter: globular` is requested but the `GlobularAdapter` is not compiled in (i.e. when running the standalone binary outside the services repo), the server logs a warning and falls back to `NullAdapter`:

```
awareness-mcp: runtime adapter "globular" not available in standalone server, using null
```

This is intentional. The core module maintains a strict import wall: it never imports Globular internals. The `GlobularAdapter` is registered into the registry by the services-side build.

---

## Configuring the Adapter

### Non-Globular Projects (NullAdapter)

```yaml
runtime:
  enabled: false
  adapter: null
```

### Globular Platform Projects (GlobularAdapter)

```yaml
runtime:
  enabled: true
  adapter: globular
```

When `enabled` is true and `adapter` is empty, the default is `globular`.

---

## What runtime_disabled Means

When `awareness_runtime_status` returns `runtime_disabled`, it means:

- The project is configured with `adapter: null` or `enabled: false`
- No live cluster data is collected
- This is the correct and expected state for all non-Globular projects

It is not an error. The MCP server is fully functional. All 9 tools work. Only the runtime-specific paths (`DoctorReport.Findings`, `RuntimeSignals`) return empty results.

```json
{
  "project": "my-service",
  "adapter": "null",
  "enabled": false,
  "status": "runtime_disabled",
  "runtime_config": {
    "enabled": false,
    "adapter": "null"
  }
}
```

---

## DoctorReport Structure

Every adapter returns a `DoctorReport` from its `Doctor()` method:

```go
type DoctorReport struct {
    Adapter  string          // adapter name
    Enabled  bool            // whether the adapter is live
    Status   string          // "ok" | "degraded" | "unavailable" | "runtime_disabled" | "error"
    Findings []DoctorFinding // health observations (empty for NullAdapter)
}
```

Status values:

| Status | Meaning |
|--------|---------|
| `ok` | Adapter is live and all checks passed |
| `degraded` | Adapter is live but some checks failed |
| `unavailable` | Adapter cannot connect to the runtime |
| `runtime_disabled` | NullAdapter — no runtime configured |
| `error` | Adapter encountered an unexpected error |

---

## RuntimeSignals Structure

`CollectSignals` returns an adapter-agnostic `RuntimeSignals` struct. For `NullAdapter` this is always empty. For `GlobularAdapter` it contains:

- `DoctorFindings` — health findings from cluster doctor
- `ServiceStatuses` — per-node service states
- `WorkflowReceipts` — recent workflow outcomes
- `StateDeltas` — desired-vs-installed mismatches
- `RepositoryStatus` — package repository health per node
- `ObjectstoreStatus` — MinIO / objectstore health per node
- `XDSStatus` — Envoy xDS config drift per node
- `SystemdUnits` — systemd unit states per node
- `MatchedInvariants` / `MatchedFailureModes` — pre-matched knowledge IDs

---

## See Also

- [project-profile.md](project-profile.md) — How to configure the runtime in `.awareness.yaml`
- [mcp/tools.md](../mcp/tools.md) — MCP tools reference, including `awareness_runtime_status`

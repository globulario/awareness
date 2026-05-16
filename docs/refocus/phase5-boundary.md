# Phase 5 — Adapter Boundary Verification

## Result: PASS

All boundary checks passed. The standalone/services split is clean.

---

## Import Wall

```
awareness standalone (github.com/globulario/awareness):
  no github.com/globulario/services imports    ✓
  no etcd imports                              ✓
  no Scylla/gocql imports                      ✓
  no MinIO imports                             ✓
  no newLiveBridge calls                       ✓
  no bridge.Snapshot calls                     ✓
  no SQLite (modernc/mattn/database-sql)       ✓
```

Script: `scripts/check-import-wall.sh` — all checks OK.

---

## Adapter Boundary

### Standalone (`github.com/globulario/awareness`)

| File | Purpose |
|------|---------|
| `runtime/adapter.go` | `Adapter` interface — the contract |
| `runtime/null_adapter.go` | `NullAdapter` — for projects without a live cluster |
| `runtime/signals.go` | `RuntimeSignals` — adapter-agnostic signal type |
| `runtime/new.go` | `New(name)` factory — returns NullAdapter by default |

The `GlobularAdapter` is deliberately absent from this repo.

### Services (`github.com/globulario/services/golang`)

| File / Package | Purpose |
|----------------|---------|
| `awareness/runtime/` | `RuntimeBridge` + live gRPC source wiring |
| `mcp/runtime_bridge_live.go` | `newLiveBridge` — etcd + gRPC source bootstrap |
| `awareness/extractors/clusterstate/` | etcd + convergence state extraction |
| `awareness/extractors/clusterspec/` | Cluster specification extraction |
| `awareness/extractors/dns/` | DNS-based cluster discovery |
| `awareness/extractors/metrics/` | Prometheus metrics |
| `awareness/livecluster/` | Live cluster snapshot collection |
| `awareness/bundlesync/` | Bundle delivery to Globular nodes (production path) |

Services does NOT import `github.com/globulario/awareness` (the standalone).
Both repos are independent. Services may import standalone in a future phase.

---

## Consumers

| Consumer | Adapter Used | Status |
|----------|-------------|--------|
| Cadence BPMN (`github.com/globulario/cadence`) | `NullAdapter` | All 20 packages green |
| Globular services MCP | Live bridge via `newLiveBridge` | Builds and 70 preflight tests green |
| `awareness` CLI / MCP (standalone) | `NullAdapter` (default) or `GlobularAdapter` by name | Builds, import wall clean |

---

## What Stays in Services (Do Not Move)

The following must remain in services because they touch Globular runtime specifics:

- etcd client calls
- Globular gRPC stubs (cluster-controller, workflow-service, node-agent, etc.)
- Prometheus metrics collection
- systemd state reading
- xDS configuration
- objectstore / repository clients
- PKI / RBAC / DNS extraction
- Bundle delivery (`bundlesync`) to Globular nodes
- Live convergence state collection (`livecluster`)

---

## Outstanding MOVE_GENERIC Items (Phase 6)

The inventory identified `fixledger/` as `MOVE_GENERIC` (YAML-only, no Globular imports).
The useful knowledge from `fixledger/` was already folded into:
- `.awareness/forbidden_fixes.yaml`
- `.awareness/failure_modes.yaml`

The package itself (YAML loader for runtime-generated `fix_cases.yaml`) can move to standalone
in Phase 6, but only after verifying no services production path depends on the runtime import.

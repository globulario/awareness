# Awareness Bundles

An Awareness bundle is a portable, self-contained directory snapshot of a project's knowledge. It packages invariants, failure modes, forbidden fixes, and the project profile into a single directory that can be distributed, stored as a CI artifact, or loaded by an agent without access to the original source tree.

---

## What a Bundle Contains

A completed bundle directory looks like this:

```
my-project-bundle/
  bundle.json          # manifest — the authoritative index of all bundle files
  profile.json         # serialised ProjectProfile
  invariants.yaml      # copy of the project's invariants file(s)
  failure_modes.yaml   # copy of the project's failure modes file(s)
  forbidden_fixes.yaml # copy of the project's forbidden fixes file(s)
  runtime_signals.json # optional — only present when built with a live adapter
```

All paths inside `bundle.json` are relative to the bundle root directory.

---

## bundle.json Schema

`bundle.json` is the manifest. Its schema version is `awareness.bundle.v1`.

```json
{
  "schema_version": "awareness.bundle.v1",
  "project_name": "my-service",
  "project_kind": "application",
  "source_root": "/home/user/my-service",
  "source_revision": "a3f9c12",
  "generated_at": "2026-05-16T14:30:00Z",
  "generator_version": "0.1.0",
  "profile_path": "profile.json",
  "invariants_paths": ["invariants.yaml"],
  "failure_modes_paths": ["failure_modes.yaml"],
  "forbidden_fixes_paths": ["forbidden_fixes.yaml"],
  "runtime_signals_included": false
}
```

### Manifest Fields

| Field | Description |
|-------|-------------|
| `schema_version` | Always `"awareness.bundle.v1"`. Readers must check this before processing. |
| `project_name` | Value of `.awareness.yaml` `name` field. |
| `project_kind` | Value of `.awareness.yaml` `kind` field. |
| `source_root` | Absolute path of the project at bundle build time. Informational — not used to resolve bundle-relative paths. |
| `source_revision` | Git SHA or tag at build time. Empty when source is not under version control. |
| `generated_at` | ISO 8601 timestamp of when the bundle was built. |
| `generator_version` | Version of the `awareness` binary that built the bundle. Empty for dev builds. |
| `profile_path` | Relative path to `profile.json` inside the bundle. |
| `invariants_paths` | Relative paths to all invariant YAML files in the bundle. |
| `failure_modes_paths` | Relative paths to all failure mode YAML files in the bundle. |
| `forbidden_fixes_paths` | Relative paths to all forbidden fix YAML files in the bundle. |
| `runtime_signals_path` | Relative path to `runtime_signals.json`. Present only when built with a live adapter. |
| `runtime_signals_included` | `true` when `runtime_signals.json` is in the bundle. |

---

## Building a Bundle

```bash
awareness bundle build --out /path/to/output-dir
```

With an explicit project root:

```bash
awareness bundle build \
  --project-root /path/to/my-project \
  --out /tmp/my-project-bundle \
  --revision $(git rev-parse --short HEAD) \
  --version 0.1.0
```

The `--revision` flag is auto-detected from `git rev-parse --short HEAD` when omitted and the project is a git repository.

Build output:

```
bundle: /tmp/my-project-bundle
schema: awareness.bundle.v1
project: my-service (application)
revision: a3f9c12
invariants: 1
failure_modes: 1
forbidden_fixes: 1
runtime_signals: false
status: ok
```

---

## Inspecting a Bundle

Use the `awareness_bundle_inspect` MCP tool:

```json
{
  "name": "awareness_bundle_inspect",
  "arguments": { "path": "/tmp/my-project-bundle" }
}
```

Example response:

```json
{
  "ok": true,
  "path": "/tmp/my-project-bundle",
  "schema_version": "awareness.bundle.v1",
  "project_name": "my-service",
  "project_kind": "application",
  "source_revision": "a3f9c12",
  "generated_at": "2026-05-16T14:30:00Z",
  "generator_version": "0.1.0",
  "invariants_paths": ["invariants.yaml"],
  "failure_modes_paths": ["failure_modes.yaml"],
  "forbidden_fixes_paths": ["forbidden_fixes.yaml"],
  "runtime_signals_included": false,
  "invariants_count": 1,
  "failure_modes_count": 1,
  "forbidden_fixes_count": 1,
  "bundle_files": ["bundle.json", "profile.json", "invariants.yaml", "failure_modes.yaml", "forbidden_fixes.yaml"],
  "warnings": []
}
```

---

## Use Cases

### CI Artifact

Build a bundle as part of every CI run and store it as a build artifact. Downstream jobs (deploy, integration tests) can load the bundle to run preflight without needing the source tree.

```yaml
# GitHub Actions example
- name: Build Awareness bundle
  run: |
    awareness bundle build \
      --project-root ${{ github.workspace }} \
      --out ${{ github.workspace }}/dist/awareness-bundle \
      --version ${{ github.ref_name }}

- name: Upload bundle
  uses: actions/upload-artifact@v4
  with:
    name: awareness-bundle
    path: dist/awareness-bundle/
```

### Distribution

Bundle your project's knowledge alongside a release. Consumers of your library or platform can load the bundle into their own awareness-mcp server to get pre-authored invariants and failure modes without maintaining them themselves.

### Offline Agent Context

An agent working in an environment without access to the source repository (e.g. a read-only cluster node) can be given a pre-built bundle. The bundle provides full knowledge context without requiring a clone of the source.

### Globular Platform Bundles

When built with the `GlobularAdapter` (in the services repository), the bundle also includes `runtime_signals.json` — a snapshot of live cluster state at build time. This lets a bundle carry both static knowledge and a point-in-time view of the cluster health for forensic analysis.

---

## Schema Version

The current schema version is `awareness.bundle.v1`. Readers must validate `schema_version` before processing a bundle. A missing or unknown `schema_version` is a bundle format error.

The `bundle.BundleManifest.Validate()` method in Go returns an error when `schema_version` is empty.

---

## See Also

- [cli/bundle.md](../cli/bundle.md) — `awareness bundle build` CLI reference
- [mcp/tools.md](../mcp/tools.md) — `awareness_bundle_inspect` MCP tool reference
- [ci/bundle-artifact.md](../ci/bundle-artifact.md) — CI integration guide

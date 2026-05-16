# awareness bundle

Build a portable Awareness bundle from a project's knowledge files.

---

## Synopsis

```bash
awareness bundle build --out PATH [--project-root PATH] [--revision SHA] [--version VER]
```

---

## Subcommands

Currently `bundle` has one subcommand: `build`. Additional subcommands may be added in future versions.

---

## `awareness bundle build`

Builds an Awareness bundle directory from the project profile and its configured knowledge files. The output directory is created if it does not exist.

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--out PATH` | yes | Output directory for the bundle. Created if it does not exist. |
| `--project-root PATH` | no | Explicit project root. Skips upward directory walk. |
| `--revision SHA` | no | VCS revision to embed in `bundle.json`. Auto-detected from `git rev-parse --short HEAD` when omitted and the project is a git repository. |
| `--version VER` | no | Generator version to embed in `bundle.json`. Normally injected via ldflags at build time. Empty for dev builds. |

### What Gets Built

1. The output directory is created.
2. `profile.json` — the serialised `ProjectProfile` — is written.
3. Each file listed under `awareness.invariants` in `.awareness.yaml` is copied into the output directory.
4. Each file listed under `awareness.failure_modes` is copied.
5. Each file listed under `awareness.forbidden_fixes` is copied.
6. `bundle.json` is written with the manifest describing all included files.

The bundle does not include runtime signals unless built with the Globular services-side tooling (`GlobularAdapter`). For all standalone builds, `runtime_signals_included` is `false`.

### Output

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

## Examples

### Minimal build

```bash
cd my-project
awareness bundle build --out /tmp/bundle
```

### With explicit project root and output

```bash
awareness bundle build \
  --project-root /home/user/my-project \
  --out /home/user/my-project/dist/bundle
```

### With revision and version

```bash
awareness bundle build \
  --project-root . \
  --out dist/awareness-bundle \
  --revision $(git rev-parse --short HEAD) \
  --version 1.2.3
```

### In a CI pipeline (GitHub Actions)

```yaml
- name: Install awareness
  run: go install github.com/globulario/awareness/cmd/awareness@v0.1.0

- name: Build awareness bundle
  run: |
    awareness bundle build \
      --project-root ${{ github.workspace }} \
      --out ${{ github.workspace }}/dist/awareness-bundle \
      --revision ${{ github.sha }} \
      --version ${{ github.ref_name }}

- name: Upload bundle artifact
  uses: actions/upload-artifact@v4
  with:
    name: awareness-bundle
    path: dist/awareness-bundle/
```

---

## bundle.json Schema

A complete `bundle.json` produced by `awareness bundle build`:

```json
{
  "schema_version": "awareness.bundle.v1",
  "project_name": "my-service",
  "project_kind": "application",
  "source_root": "/home/user/my-project",
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

When the project has multiple knowledge files (one per subsystem), each is copied and listed:

```json
{
  "schema_version": "awareness.bundle.v1",
  "project_name": "platform",
  "project_kind": "distributed-platform",
  "source_revision": "b7d2e91",
  "generated_at": "2026-05-16T14:30:00Z",
  "profile_path": "profile.json",
  "invariants_paths": [
    "invariants.yaml",
    "invariants-storage.yaml",
    "invariants-network.yaml"
  ],
  "failure_modes_paths": ["failure_modes.yaml"],
  "forbidden_fixes_paths": ["forbidden_fixes.yaml"],
  "runtime_signals_included": false
}
```

### Schema Version

The schema version `awareness.bundle.v1` is a constant defined in the Go package as `bundle.CurrentSchemaVersion`. Readers must validate this field before processing the manifest. A bundle with an empty or unrecognized `schema_version` is invalid.

---

## Bundle Directory Layout

After a successful build:

```
<output-dir>/
  bundle.json          # manifest
  profile.json         # serialised ProjectProfile
  invariants.yaml      # copied from project
  failure_modes.yaml   # copied from project
  forbidden_fixes.yaml # copied from project
```

All paths in `bundle.json` are relative to the bundle root. The bundle is self-contained — it can be moved, archived, or distributed without any reference to the original source tree.

---

## Inspecting a Bundle

After building, use the `awareness_bundle_inspect` MCP tool or read `bundle.json` directly:

```bash
cat /tmp/bundle/bundle.json | jq '{schema_version, project_name, invariants_paths}'
```

Or via MCP:

```json
{
  "name": "awareness_bundle_inspect",
  "arguments": { "path": "/tmp/bundle" }
}
```

---

## See Also

- [cli/awareness.md](awareness.md) — Top-level CLI reference
- [concepts/bundles.md](../concepts/bundles.md) — Bundle format and use cases
- [mcp/tools.md](../mcp/tools.md) — `awareness_bundle_inspect` MCP tool reference
- [ci/bundle-artifact.md](../ci/bundle-artifact.md) — CI artifact guide

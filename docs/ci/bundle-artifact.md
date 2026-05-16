# Building and Uploading an Awareness Bundle in CI

An Awareness bundle is a portable snapshot of your project's knowledge graph,
invariants, failure modes, and forbidden fixes. Building it in CI makes it
available as a release artifact or for distribution to AI agents.

---

## Build and Upload as GitHub Actions Artifact

```yaml
name: Awareness Bundle

on:
  push:
    branches: [main, master]
  release:
    types: [created]

jobs:
  awareness-bundle:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Install Awareness
        run: go install github.com/globulario/awareness/cmd/awareness@latest

      - name: Build Awareness bundle
        run: |
          awareness bundle build \
            --out /tmp/awareness-bundle \
            --revision ${{ github.sha }}

      - name: Show bundle manifest
        run: cat /tmp/awareness-bundle/bundle.json

      - uses: actions/upload-artifact@v4
        with:
          name: awareness-bundle-${{ github.sha }}
          path: /tmp/awareness-bundle/
          retention-days: 30
```

---

## Bundle Manifest Fields

The `bundle.json` produced by `awareness bundle build` contains:

| Field | Description |
|-------|-------------|
| `schema_version` | Always `"awareness.bundle.v1"` |
| `project_name` | From `.awareness.yaml` |
| `project_kind` | From `.awareness.yaml` |
| `source_revision` | Git SHA (from `--revision` or auto-detected) |
| `generated_at` | Build timestamp (UTC) |
| `invariants_paths` | Relative paths to invariant files in the bundle |
| `failure_modes_paths` | Relative paths to failure_modes files |
| `forbidden_fixes_paths` | Relative paths to forbidden_fixes files |
| `runtime_signals_included` | `false` for NullAdapter bundles |

---

## Validate the Bundle in CI

Verify the bundle is well-formed before upload:

```bash
# Check schema version
SCHEMA=$(jq -r '.schema_version' /tmp/awareness-bundle/bundle.json)
if [ "$SCHEMA" != "awareness.bundle.v1" ]; then
  echo "Invalid bundle schema: $SCHEMA"
  exit 1
fi

# Check required files exist
for f in bundle.json invariants.yaml failure_modes.yaml forbidden_fixes.yaml; do
  if [ ! -f "/tmp/awareness-bundle/$f" ]; then
    echo "Missing required bundle file: $f"
    exit 1
  fi
done
echo "Bundle validation: ok"
```

---

## For Globular Projects

Globular projects use the services-side bundle pipeline (`globular awareness bundle build`)
which adds Globular runtime signals and publishes the bundle to the repository
service. The `bundle.json` content manifest format is the same (`awareness.bundle.v1`).

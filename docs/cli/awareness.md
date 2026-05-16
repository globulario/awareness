# awareness CLI Reference

`awareness` is the command-line interface for the Awareness reasoning engine.

---

## Installation

```bash
go install github.com/globulario/awareness/cmd/awareness@v0.1.0
```

Ensure `~/go/bin` is on your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Verify:

```bash
awareness --help
```

---

## Global Flags

| Flag | Description |
|------|-------------|
| `--project-root PATH` | Explicit project root. Skips upward directory walk. Path must contain `.awareness.yaml`. |

When `--project-root` is omitted, all subcommands walk up from the current working directory looking for `.awareness.yaml`, stopping at the nearest `.git` root.

---

## Subcommands

### `awareness profile show`

Display the resolved project profile.

```bash
awareness profile show [--project-root PATH]
```

Output:

```
project: my-service
kind:    application
root:    /home/user/my-service
profile: /home/user/my-service/.awareness.yaml
runtime: enabled=false adapter=null
```

---

### `awareness profile doctor`

Run a static health check on the project profile. Checks that all configured paths exist and the profile is structurally valid.

```bash
awareness profile doctor [--project-root PATH]
```

Output when healthy:

```
project: my-service
kind:    application
root:    /home/user/my-service
profile: /home/user/my-service/.awareness.yaml
runtime: disabled

status: ok
```

If a configured invariants or failure modes file is missing, the doctor reports the problem so you can fix it before agents rely on it.

---

### `awareness preflight`

Run a lightweight preflight check. Scans project knowledge files for invariants, failure modes, and forbidden fixes relevant to a task or set of changed files.

Full reference: [cli/preflight.md](preflight.md).

```bash
awareness preflight [flags]
```

Quick examples:

```bash
# Check all git-changed files
awareness preflight --changed

# Check for a specific task
awareness preflight --task "refactor error handling in the payment handler"

# JSON output for CI
awareness preflight --changed --format json
```

---

### `awareness bundle build`

Build a portable Awareness bundle from the project's knowledge files.

Full reference: [cli/bundle.md](bundle.md).

```bash
awareness bundle build --out PATH [--project-root PATH] [--revision SHA] [--version VER]
```

Quick example:

```bash
awareness bundle build \
  --project-root /path/to/my-project \
  --out /tmp/my-project-bundle
```

---

## Usage Examples

### Verify a project profile

```bash
cd my-project
awareness profile doctor
```

### Check what knowledge items are in scope before editing a file

```bash
awareness preflight --files "internal/service/handler.go"
```

### Run preflight on all staged changes before a commit

```bash
awareness preflight --changed --format json | jq '{invariants, failure_modes, forbidden_fixes}'
```

### Build a bundle and inspect it

```bash
awareness bundle build --out /tmp/bundle
ls /tmp/bundle/
cat /tmp/bundle/bundle.json
```

### Use an explicit project root from a script

```bash
awareness profile doctor --project-root /home/ci/workspace/my-project
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error — profile not found, invalid YAML, missing `--out` flag, or unknown subcommand |

---

## See Also

- [cli/preflight.md](preflight.md) — Preflight flags and output reference
- [cli/bundle.md](bundle.md) — Bundle build flags and output reference
- [concepts/project-profile.md](../concepts/project-profile.md) — How `.awareness.yaml` works
- [getting-started.md](../getting-started.md) — Full walkthrough from zero to working setup

# awareness preflight

Run a lightweight preflight check before starting a task or committing changes. Scans the project's knowledge files — invariants, failure modes, forbidden fixes — for entries relevant to the task description or changed files.

Preflight does not require a live cluster or runtime adapter. It works with any project that has a `.awareness.yaml` file and at least one knowledge file.

---

## Synopsis

```bash
awareness preflight [--changed] [--task "description"] [--files "a.go,b.go"] [--format text|json] [--project-root PATH]
```

---

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--changed` | boolean | false | Detect git-changed files from the project root (both staged and unstaged) and include them in the analysis. Uses `git diff --name-only HEAD` and `git status --porcelain`. |
| `--task "..."` | string | — | Short description of the task. Used for keyword matching against knowledge file entries and for task classification. |
| `--files "a.go,b.go"` | string | — | Comma-separated file paths to analyse. Can be combined with `--changed`. |
| `--format text\|json` | string | text | Output format. Use `json` for CI pipelines. |
| `--project-root PATH` | string | — | Explicit project root. Skips upward directory walk. |

---

## Output Fields

### Text Format

```
project: my-service
runtime: disabled
task:    refactor error handling in payment handler
changed: 2 files
classification: [LOCAL_CODE_CHANGE, ARCHITECTURE_SENSITIVE]
raw_matches: 3
  - invariant: idempotency.required (score:3)
  - failure_mode: double.write.on.retry (score:2)
  - forbidden_fix: no.discard.error.with.underscore (score:2)
status: ok
```

### JSON Format

```json
{
  "project_name": "my-service",
  "task": "refactor error handling in payment handler",
  "changed_files": ["internal/service/handler.go", "internal/service/handler_test.go"],
  "classification": ["LOCAL_CODE_CHANGE", "ARCHITECTURE_SENSITIVE"],
  "invariants": ["idempotency.required"],
  "failure_modes": ["double.write.on.retry"],
  "forbidden_fixes": ["no.discard.error.with.underscore"],
  "raw_matches": [
    { "kind": "invariant", "id": "idempotency.required", "score": 3 },
    { "kind": "failure_mode", "id": "double.write.on.retry", "score": 2 },
    { "kind": "forbidden_fix", "id": "no.discard.error.with.underscore", "score": 2 }
  ],
  "runtime_status": "disabled",
  "warnings": [],
  "ok": true
}
```

### Output Field Descriptions

| Field | Description |
|-------|-------------|
| `project_name` | Project name from `.awareness.yaml`. |
| `task` | Task string passed via `--task`, or empty. |
| `changed_files` | Files detected as changed (from `--changed` or `--files`). |
| `classification` | Task classification labels. See [Task Classification](#task-classification) below. |
| `invariants` | IDs of matched invariants with score >= 2. |
| `failure_modes` | IDs of matched failure modes with score >= 2. |
| `forbidden_fixes` | IDs of matched forbidden fixes with score >= 2. |
| `raw_matches` | All matches with their kind, id, and score. Score < 2 means low confidence. |
| `runtime_status` | `"disabled"` (NullAdapter) or `"ok"` (live adapter). |
| `warnings` | Non-fatal warnings (e.g. git not available, adapter unavailable). |
| `ok` | Always `true` when preflight completes without a fatal error. |

---

## Task Classification

Preflight classifies the task string using deterministic keyword matching. Classification labels are used by agents to route to the appropriate knowledge and tools.

| Classification | Triggered by |
|----------------|-------------|
| `LOCAL_CODE_CHANGE` | Default when no other classification matches. |
| `ARCHITECTURE_SENSITIVE` | Keywords: retry, loop, restart, drift, convergence, desired, installed, runtime, leader, failover, build_id, checksum. |
| `CONVERGENCE_RISK` | Keywords: desired_hash, checksum mismatch, build_id mismatch, restart storm, or regression-related terms. |
| `PACKAGE_ADMISSION` | Keywords: package install, admit, awareness.yaml. |
| `RUNTIME_INCIDENT` | Keywords: incident, crash, oom, panic, fatal. |
| `RETRY_LOOP` | Keywords: retry loop, or "retry" + "loop" together. |
| `RESTART_STORM` | Keywords: restart storm, sigterm storm, start-limit-hit. |
| `STATE_MISMATCH` | Keywords: desired_hash, checksum mismatch, build_id mismatch. |
| `DEPENDENCY_CYCLE` | Keywords: dependency cycle, circular dependency, deadlock. |
| `UNKNOWN_IMPACT` | Reserved — returned by MCP tools when classification cannot be determined. |
| `STATIC_KNOWLEDGE_FALLBACK` | Reserved — returned when the graph is unavailable and static files are used. |

---

## Examples

### Check all git-changed files before a commit

```bash
awareness preflight --changed
```

### Check a specific task description

```bash
awareness preflight --task "fix goroutine leak in the subscription handler"
```

### Check specific files

```bash
awareness preflight --files "internal/service/handler.go,internal/store/payment.go"
```

### Combine task and changed files

```bash
awareness preflight --changed --task "refactor retry logic in order processor"
```

### JSON output for CI

```bash
awareness preflight --changed --format json
```

### Parse JSON output in a CI script

```bash
awareness preflight --changed --format json > preflight.json

MATCH_COUNT=$(jq '(.invariants | length) + (.failure_modes | length) + (.forbidden_fixes | length)' preflight.json)

if [ "$MATCH_COUNT" -gt 0 ]; then
  echo "Awareness: $MATCH_COUNT knowledge items in scope"
  jq '{invariants, failure_modes, forbidden_fixes}' preflight.json
fi
```

### Fail CI when critical knowledge items are in scope

```bash
awareness preflight --changed --format json > preflight.json

# Check if any matched invariant is classified as critical
# (requires jq for JSON processing)
jq -e '.invariants | length > 0' preflight.json && {
  echo "Invariants matched — review before merging:"
  jq '.invariants' preflight.json
}
```

### Use with an explicit project root from a script

```bash
awareness preflight \
  --project-root /home/ci/workspace/my-project \
  --changed \
  --format json
```

---

## Matching Algorithm

Preflight uses keyword-based matching — no LLM calls, no network requests. Matching is:

1. The task string and file paths are tokenized into lowercase keywords.
2. Each knowledge entry's `id`, `title`, `description`, and `tags` are searched for those keywords.
3. Each match increments the entry's score by 1 per keyword found.
4. Entries with score >= 2 are included in `invariants`, `failure_modes`, and `forbidden_fixes`.
5. All entries with score >= 1 appear in `raw_matches`.

This means:
- A task description with more specific keywords yields fewer, more relevant matches.
- A very short task string (one word) may yield no matches even when relevant items exist. Use `--files` to add file path context.
- Adding good `tags` to your invariants and failure modes improves match quality.

---

## See Also

- [cli/awareness.md](awareness.md) — Top-level CLI reference
- [concepts/invariants.md](../concepts/invariants.md) — Writing invariants
- [concepts/failure-modes.md](../concepts/failure-modes.md) — Writing failure modes
- [ci/preflight-json.md](../ci/preflight-json.md) — Full CI integration guide

# Parsing Preflight JSON Output

`awareness preflight --format json` emits a `PreflightResult` JSON document.
This guide shows how to parse it in CI scripts.

---

## JSON Schema

```json
{
  "project_name": "string",
  "task": "string (optional)",
  "changed_files": ["string"],
  "classification": ["string"],
  "invariants": ["string"],
  "failure_modes": ["string"],
  "forbidden_fixes": ["string"],
  "raw_matches": [
    {
      "source": "invariants.yaml",
      "kind": "invariant | failure_mode | forbidden_fix",
      "id": "string",
      "score": 5,
      "matched_terms": ["string"]
    }
  ],
  "runtime_status": "disabled | ok",
  "warnings": ["string"],
  "ok": true
}
```

---

## Fail CI on High-Score Matches

Block the PR when any raw match scores above a threshold:

```bash
awareness preflight --changed --format json > preflight.json

HIGH_SCORE=$(jq '[.raw_matches[] | select(.score >= 4)] | length' preflight.json)
if [ "$HIGH_SCORE" -gt 0 ]; then
  echo "Awareness: $HIGH_SCORE high-relevance items matched — review required"
  jq '.raw_matches[] | select(.score >= 4) | "\(.kind): \(.id) (score:\(.score))"' preflight.json
  exit 1
fi
```

---

## Fail CI on Specific Invariant Match

Fail the build when a specific invariant is matched (zero-tolerance for
certain invariants):

```bash
awareness preflight --changed --format json > preflight.json

CRITICAL_MATCH=$(jq '[.invariants[] | select(. == "process.state.determinism")] | length' preflight.json)
if [ "$CRITICAL_MATCH" -gt 0 ]; then
  echo "Awareness: critical invariant process.state.determinism is in scope"
  exit 1
fi
```

---

## Collect Matched Failure Modes

Emit matched failure modes as a GitHub Actions annotation:

```bash
awareness preflight --changed --format json > preflight.json

FAILURE_MODES=$(jq -r '.failure_modes[]' preflight.json)
if [ -n "$FAILURE_MODES" ]; then
  echo "$FAILURE_MODES" | while read -r fm; do
    echo "::warning title=Awareness::Failure mode in scope: $fm"
  done
fi
```

---

## Non-Strict Mode (Advisory Only)

Run awareness without blocking the build — emit output for human review:

```yaml
- name: Run Awareness preflight (advisory)
  run: |
    awareness preflight --changed --format json | tee preflight.json
    echo "Invariants in scope: $(jq '.invariants | length' preflight.json)"
    echo "Failure modes in scope: $(jq '.failure_modes | length' preflight.json)"
  continue-on-error: true
```

---

## Strict Mode (Block on Any Match)

Block the PR when awareness returns any invariant or failure mode match:

```yaml
- name: Run Awareness preflight (strict)
  run: |
    awareness preflight --changed --format json > preflight.json
    MATCH_COUNT=$(jq '(.invariants | length) + (.failure_modes | length)' preflight.json)
    if [ "$MATCH_COUNT" -gt 0 ]; then
      echo "Awareness: $MATCH_COUNT knowledge items in scope — review required"
      jq '{invariants, failure_modes, forbidden_fixes}' preflight.json
      exit 1
    fi
```

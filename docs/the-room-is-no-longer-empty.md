# The Room Is No Longer Empty

## The Previous Failure

The awareness graph existed — but impact analysis returned empty results for almost every file.

The reason was simple:

The invariant loader created edges in this direction:

```
invariant → protects → source_file
```

But impact traversal began from the changed file and followed **outgoing** edges:

```
source_file → ??? → ...
```

Since source_file nodes had no outgoing edges to their invariants, traversal found nothing.
The graph was full of knowledge, but the door was locked from the wrong side.

The impact result returned empty.

An empty result looked like "no rules apply here."

It was actually "the graph cannot see this file from this direction."

These are completely different things.

---

## The Fix

The invariant loader now creates **reverse edges** when processing `files:` declarations in invariant YAML:

```yaml
# invariants.yaml declares:
- id: install.result.atomic_commit
  files:
    - golang/node_agent/node_agent_server/installed_services.go
```

The loader creates:

```
source_file:golang/.../installed_services.go
  → implements
  invariant:install.result.atomic_commit
```

Now impact traversal beginning from the file can reach its invariants:

```
source_file → implements → invariant → forbids → forbidden_fix
source_file → implements → invariant → requires_test → test
source_file → implements → invariant → related_to → failure_mode
```

The room is no longer empty.

---

## Edge Semantics

Not every file relationship is `implements`. The loader uses precise relationship types:

| Edge | When to use |
|------|-------------|
| `implements` | The file contains core logic that makes the invariant true |
| `enforces` | The file prevents violation or blocks unsafe behavior |
| `observes` | The file detects, reports, or diagnoses the invariant |
| `configures` | The file defines data, YAML, or policy used by the invariant |
| `may_affect` | The file can affect the invariant indirectly |
| `documents` | The file explains the invariant but does not enforce it |

Impact ranking uses these edge types. `implements` and `enforces` produce mandatory findings.
`may_affect` and `documents` produce lower-confidence context.

---

## Ranking

Before this change, the output of impact analysis was unordered.

High-severity mandatory findings appeared after low-confidence background context.

Impact results are now ranked:

1. Mandatory items first (implements/enforces path, or ForbiddenFix node)
2. By severity: critical → high → medium → low
3. By confidence: high → medium → low
4. By path length: shorter paths (clearer evidence) ranked higher

An agent reading the impact result now sees the most important rules first.

---

## The NO_MATCH Rule

A critical insight from this work:

**NO_MATCH does not mean safe to proceed.**

When a changed file has no graph coverage, the system now returns `MissingLinks` — an explicit
explanation of why the coverage gap exists and what to do about it.

Before:
```json
{ "invariants": [], "forbidden_fixes": [], "required_tests": [] }
```
This looked like "no rules apply here."

After:
```json
{
  "invariants": [],
  "forbidden_fixes": [],
  "required_tests": [],
  "missing_links": [
    "no graph edges found from 'golang/new_package/new_file.go' — run 'globular awareness build --clean' to index this file",
    "This file is in a new package. Add it to an invariant's 'files:' list in docs/awareness/invariants.yaml..."
  ]
}
```

An agent reading this now knows: the graph is incomplete, not that the file is unimportant.

---

## Negative Tests

This phase added negative tests that prove the guardrails have teeth:

| Test | What it proves |
|------|----------------|
| `TestRankFindingsMandatoryFirst` | Mandatory findings sort before optional |
| `TestRankFindingsBySeverity` | Critical invariants rank above low-severity ones |
| `TestRejectFuzzyResultAsActionAuthority` | Unknown file produces MissingLinks, not empty-is-safe |
| `TestRejectGraphEdgeToMissingNode` | Dangling edges don't produce ghost findings |
| `TestRejectImpactResultWithoutReason` | Every finding carries an EdgePath trace |
| `TestRejectNoMatchWithoutCoverageExplanation` | NO_MATCH must explain the coverage gap |

---

## The Impact Loop

The full chain now works end-to-end:

```
changed file
  ↓ (graph edge: implements/enforces/observes)
invariant
  ↓ (graph edge: forbids)
forbidden_fix       ← "do not do X"
  ↓ (graph edge: requires_test)
required_test       ← "prove it with Y"
  ↓ (CI check)
test must pass      ← "CI enforces it"
```

The awareness graph is no longer decorative. It is a guardrail.

---

## What Still Requires Human Judgment

The graph catches known patterns. It does not catch unknowns.

A file with no `implements` edges is not necessarily safe — it may be unregistered.

A finding ranked "medium" is not necessarily unimportant — it may be context you need.

The system surfaces evidence. Judgment remains human.

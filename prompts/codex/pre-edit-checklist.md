# Pre-Edit Checklist — Codex

Run this checklist before making any significant code edit.

1. **Call preflight.** Run `awareness_preflight(task="<what you are about to do>", changed=true)`. Do not skip this step for design-level changes.

2. **Read matched invariants fully.** For each invariant returned by preflight, read the complete description. Skimming is not sufficient — the constraint is in the details.

3. **Check matched failure modes.** For each failure mode returned, compare the symptoms to what you observe in the codebase. If the symptoms match, this area is known-fragile. Slow down.

4. **Read forbidden fixes.** For each forbidden fix returned, verify that your planned change does not implement the forbidden pattern. If it does, stop and propose an alternative.

5. **Check node context for critical files.** If you are modifying a file that is listed under a critical invariant, call `awareness_node_context(path="<file path>")` to get the full invariant and failure mode context for that file before editing.

6. **Verify after editing.** After completing edits, check that no matched invariant is contradicted by your changes. If preflight flagged an invariant about immutability, schema stability, or import boundaries, re-read the changed code against that constraint explicitly.

These steps take less time than debugging a broken invariant after the fact.

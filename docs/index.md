# Awareness Documentation

Awareness is a project-aware context, preflight, invariant, bundle, and MCP toolchain for AI-assisted software development.

It is **not** a Globular component. It is a standalone, project-agnostic module. Globular is one project that uses it.

---

## Getting Started

- [Getting Started Guide](getting-started.md) — 10-minute quickstart, no Globular required

## Concepts

- [Project Profile](concepts/project-profile.md) — `.awareness.yaml` and how Awareness resolves project identity
- [Invariants](concepts/invariants.md) — laws the code must uphold
- [Failure Modes](concepts/failure-modes.md) — known failure patterns and their symptoms
- [Forbidden Fixes](concepts/forbidden-fixes.md) — fixes that look plausible but are wrong for this project
- [Runtime Adapters](concepts/runtime-adapters.md) — NullAdapter vs. GlobularAdapter
- [Bundles](concepts/bundles.md) — portable knowledge snapshots
- [MCP Server](concepts/mcp.md) — how awareness-mcp exposes tools to AI agents

## CLI Reference

- [awareness](cli/awareness.md) — top-level CLI
- [awareness preflight](cli/preflight.md) — preflight checks
- [awareness bundle](cli/bundle.md) — bundle build and inspect

## MCP Tools

- [All Tools Reference](mcp/tools.md) — all 9 MCP tools with input/output schemas
- [Claude Code Configuration](mcp/claude-code.md) — `.mcp.json` examples
- [Codex / Generic Stdio](mcp/codex.md) — generic stdio configuration

## Adoption

- [Migration Guide](adoption/migration-guide.md) — add Awareness to an existing project
- [Non-Globular Project Guide](adoption/non-globular-project.md) — standalone usage without Globular
- [Cadence MCP Setup](adoption/cadence-mcp.md) — Cadence-specific setup and workflow

## CI Integration

- [GitHub Actions](ci/github-actions.md) — build and upload bundles in CI
- [Preflight JSON Output](ci/preflight-json.md) — parse preflight JSON in CI scripts
- [Bundle Artifact](ci/bundle-artifact.md) — bundle build and validation in CI

## Releases

- [v0.1.0](releases/v0.1.0.md) — first standalone release

## Architecture

- [Architecture Overview](architecture.md) — adapter boundary, module layout, design decisions

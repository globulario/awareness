# Cadence-Like — Architecture

This is a fixture project representing a Cadence workflow engine implementation.

## Overview

The project implements a task scheduling and execution engine inspired by Uber Cadence.
It has no dependency on Globular infrastructure.

## Core Components

- `internal/model` — domain types (Task, state machine)
- `internal/runtime` — executor with resource bounds enforcement

## Awareness Configuration

- Runtime: **disabled** (`runtime.enabled: false`, `adapter: null`)
- No cluster connection is required for awareness doctor or preflight
- All invariant and failure-mode checks are static

## Key Invariants

- `workflow.idempotency` — steps must be replay-safe
- `task.state.transitions` — transitions go through the state machine
- `model.immutability` — identity fields are set once at creation

# Testing Strategy

This document defines the ideal testing strategy, tiers, and guidelines for OrbitMesh.
It is intentionally forward-looking and describes how tests should be structured and run.

## Goals

- Catch regressions early with fast, reliable tests.
- Ensure critical workflows work end-to-end with real services.
- Keep test failures actionable with rich diagnostics.
- Minimize flakiness through deterministic data and short timeouts.

## Test Tiers

1) Unit tests
- Purpose: validate small units of logic in isolation.
- Scope: pure functions, reducers, helpers, domain logic.
- Speed: sub-second per file.

2) Component/UI tests
- Purpose: validate UI behavior without real backend dependencies.
- Scope: components and views with mocked data and network.
- Tools: browser-less or browser-based component testing.

3) Integration tests (service-level)
- Purpose: validate backend handlers and service wiring with real dependencies where feasible.
- Scope: handler/transport + service + storage, using real storage where practical.

4) End-to-end (E2E) tests
- Purpose: validate user workflows across frontend and backend with real networking.
- Scope: minimal critical paths (auth/session lifecycle/task workflow/streaming).
- Requirements: start backend + frontend, use realistic seed data.

## Layout and Conventions

- `frontend/src/**/*.test.ts(x)`: unit + component tests
- `backend/**/_test.go`: unit + integration tests
- `frontend/tests/ui/**`: UI tests with mocked APIs (fast)
- `frontend/tests/e2e/**`: true E2E against real backend
- `tests/fixtures/**`: shared JSON fixtures and seed scripts
- `tests/helpers/**`: cross-test utilities and harnesses

## Guidelines

- Prefer small, stable E2E suites; keep them focused on critical workflows.
- Mock only in unit/UI tiers; E2E should use real services.
- Centralize fixtures and test helpers to avoid drift and duplication.
- Use deterministic seed data; never depend on production data.
- Keep tests independent and order-agnostic.
- Avoid `waitForTimeout` unless there is no better signal; prefer observable state.

## Timeouts and Reliability

- All tests must use short, explicit timeouts to avoid hangs.
- Default timeouts should be low; increase only when required by real workflows.
- Any helper that may fail should throw errors with actionable debugging info:
  - request/response details or relevant IDs
  - expected vs. actual values
  - captured logs or screenshots where applicable

## E2E Execution Model

- Start backend and frontend in test mode.
- Use health checks to gate test start.
- Capture artifacts on failure (logs, traces, screenshots, video).
- Provide a small smoke subset for PR checks and a full suite for scheduled runs.

## Maintenance

- Keep this document aligned with actual test tooling and layout.
- Update fixtures and helpers whenever API contracts change.
- Track flaky tests and either stabilize or quarantine them quickly.

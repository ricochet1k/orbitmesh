# Codex Provider Future Work

The initial `codex` provider is implemented as a prototype around `codex app-server` over stdio JSON-RPC.

## Completed in prototype

- `turn/plan/updated` notifications are now mapped into OrbitMesh plan events.
- `turn/diff/updated` notifications are now surfaced as metadata events.
- `commandExecution` items now include richer tool-call output metadata (`aggregated_output`, `exit_code`, `duration_ms`).
- Provider config tests now verify real CLI initialization by probing `app-server` when available and falling back to legacy `proto` initialization checks.

## High-priority follow-ups

- Persist and resume Codex threads (`thread/resume`) across OrbitMesh restarts.
- Add first-class approval UX for Codex user-input and approval-request flows.
- Surface structured plans/diffs from `turn/plan/updated` and `turn/diff/updated` in the session UI.
- Map command/tool item details into richer OrbitMesh tool-call records (args, output, exit code, duration).

## Medium-priority follow-ups

- Support detached review flows (`review/start`) and review-thread linking in OrbitMesh.
- Add model discovery (`model/list`) to populate UI model pickers dynamically.
- Add explicit thread lifecycle actions in UI (archive, unarchive, fork, compact).
- Add websocket transport support for app-server once OrbitMesh needs remote Codex hosts.

## Low-priority follow-ups

- Add telemetry/metrics mapping from thread usage notifications to OrbitMesh metrics UI.
- Add integration tests with a fake app-server harness for event-order and retry behavior.
- Add provider docs for recommended sandbox and approval configurations.

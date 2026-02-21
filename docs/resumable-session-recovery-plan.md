# Resumable Session Recovery Plan

## Goals
- Make session recovery deterministic after process restart or crash, without introducing a persisted explicit state machine.
- Keep storage append-friendly: immutable message log in JSONL plus compact JSON snapshots for fast bootstrap.
- Detect and surface interrupted runs and interrupted waits at startup so operators and clients can recover safely.
- Provide a strict resume-token lifecycle to gate resume actions and prevent stale re-entry.
- Split `AgentExecutor` into focused components so recovery logic is testable and maintainable.

## Non-Goals
- No migration to a database in this phase; file-based storage remains the source of truth.
- No protocol redesign for provider adapters beyond the minimum hooks needed for resume/recovery.
- No long-lived persisted workflow engine or explicit persisted transition table.
- No frontend redesign; only API/status semantics needed for recovery correctness.

## On-Disk Artifacts and Schemas

### `sessions/<session_id>.json` (snapshot)
Canonical per-session metadata and last known recovery checkpoint.

```json
{
  "id": "sess_123",
  "title": "Fix flaky test",
  "working_dir": "/repo",
  "provider": {"type": "native", "model": "gpt-5.3-codex"},
  "created_at": "2026-02-21T10:00:00Z",
  "updated_at": "2026-02-21T10:03:14Z",
  "last_seq": 148,
  "run": {
    "run_id": "run_9f5",
    "boot_id": "boot_20260221_1",
    "started_at": "2026-02-21T10:01:03Z",
    "ended_at": null,
    "wait": {
      "kind": "tool_result",
      "since_seq": 146,
      "resume_token_id": "rt_5f0",
      "deadline_at": "2026-02-21T10:11:03Z"
    }
  },
  "recovery": {
    "last_boot_seen": "boot_20260221_1",
    "interruption": null
  }
}
```

### `sessions/<session_id>.messages.jsonl` (append-only log)
Each line is one immutable event/message record; `seq` is strictly increasing per session.

```json
{"seq":145,"ts":"2026-02-21T10:01:10Z","kind":"message.user","message_id":"msg_u1","content":"run tests"}
{"seq":146,"ts":"2026-02-21T10:01:25Z","kind":"run.waiting","run_id":"run_9f5","wait_kind":"tool_result","resume_token_id":"rt_5f0"}
{"seq":147,"ts":"2026-02-21T10:02:02Z","kind":"run.interrupted","run_id":"run_9f5","reason":"process_exit"}
{"seq":148,"ts":"2026-02-21T10:03:14Z","kind":"status.derived","status":"interrupted_waiting"}
```

### Token index file (optional but recommended)
`sessions/<session_id>.tokens.json` stores active/consumed resume-token metadata for O(1) validation without scanning full JSONL.

## Derived Status Rules (No Persisted Explicit State Machine)
Status is computed from snapshot + latest log records, never persisted as authoritative state.

- `running`: latest run has `started_at` and no terminal event (`run.completed|run.failed|run.cancelled|run.interrupted`).
- `waiting`: latest non-terminal run has `run.waiting` and token still active.
- `idle`: latest run is terminal and no active wait token.
- `interrupted_startup`: snapshot shows in-progress run tied to previous `boot_id` with no terminal event.
- `interrupted_waiting`: run is non-terminal, `run.waiting` exists, token expired/revoked/missing or wait deadline passed.

Precedence: `interrupted_startup` > `interrupted_waiting` > `waiting` > `running` > `idle`.

## Startup Interruption Detection
On process boot (`main`):
1. Generate `boot_id` and load all session snapshots.
2. For each session with non-terminal run, compare snapshot `run.boot_id` to current `boot_id`.
3. If different and no terminal log event after `run.started_at`, append `run.interrupted` (`reason=process_restart`).
4. Recompute derived status and emit `status.derived` for SSE/API visibility.

Idempotency rule: if `run.interrupted` already exists for the same `run_id`, do not append again.

## Interrupted Waiting Detection
During boot and periodic sweeps:
- If latest run is waiting and `resume_token_id` is invalid, consumed, expired, or missing, mark `interrupted_waiting`.
- If `deadline_at` is exceeded without `run.resumed`, append `run.interrupted` (`reason=wait_timeout`) and rotate token state to revoked.
- If user posts a fresh message while waiting, consume/revoke old token and start a new run (derived status transitions to `running`).

## Resume-Token Lifecycle
- **Mint** on `run.waiting`: create `token_id`, secret/hash, expiry, scope (`session_id`, `run_id`, `wait_kind`).
- **Persist** in snapshot wait block plus token index entry (hashed secret only).
- **Validate** on resume API call: session/run/wait match, unexpired, unconsumed, unrevoked.
- **Consume** atomically on successful resume, append `run.resumed` in JSONL.
- **Rotate/Revoke** on timeout, cancellation, new run, or interruption detection.
- **Auditability**: every transition appends a JSONL record (`token.minted|consumed|revoked|expired`).

## AgentExecutor Mass Split

### Target Component Boundaries
- `ExecutionCoordinator`: start/cancel/resume orchestration and provider wiring.
- `RecoveryManager`: boot scan, interruption detection, wait reconciliation.
- `RunStateProjector`: derives runtime status from snapshot + log.
- `MessageLogWriter`: append-only JSONL writer with sequence guarantees.
- `ResumeTokenService`: mint/validate/consume/revoke tokens.

### Ownership Responsibilities
- Coordinator owns in-memory run handles and lifecycle hooks.
- Recovery owns startup and periodic repair flows.
- Projector owns all status derivation logic (single source of truth for status rules).
- Storage services own fsync/atomicity and schema encoding.
- API handlers only translate HTTP to service calls; no status inference in handlers.

### File Layout Proposal
- `backend/internal/service/executor.go` -> thin facade and wiring only.
- `backend/internal/service/execution_coordinator.go`
- `backend/internal/service/recovery_manager.go`
- `backend/internal/service/run_state_projector.go`
- `backend/internal/service/resume_token_service.go`
- `backend/internal/storage/session_snapshot_store.go`
- `backend/internal/storage/message_log_store.go`
- `backend/internal/storage/token_store.go`
- `backend/internal/domain/session.go` (snapshot/log domain structs)
- `backend/internal/domain/event.go` (new recovery/token event kinds)

### Execution Ordering
1. Introduce domain/storage schemas and backward-compatible readers.
2. Add log writer + projector behind existing executor flow.
3. Add token service and waiting/resume plumbing.
4. Add recovery manager boot sweep + interrupted detection.
5. Split executor into coordinator facade, then remove legacy mixed logic.

### Acceptance Criteria
- Restart during active run produces exactly one `run.interrupted` and derived interrupted status.
- Waiting run with expired/missing token is reported as `interrupted_waiting` and cannot be resumed.
- Valid token resume consumes token exactly once (idempotent on retries).
- `AgentExecutor` facade contains no persistence or derivation internals.
- Existing session APIs remain backward compatible at transport shape, with additive fields only.

## API and Service Touchpoints
- `backend/internal/api/handler.go`: add recovery-aware read model fields and resume-token validation errors.
- `backend/internal/api/sse.go`: emit derived status and interruption events on boot and resume transitions.
- `backend/internal/service/executor.go`: replace monolith logic with coordinator/recovery/projector wiring.
- `backend/internal/storage/storage.go`: route to snapshot/log/token stores.
- `backend/internal/domain/session.go`: represent run checkpoint + wait metadata.
- `backend/internal/domain/event.go`: define `run.interrupted`, `run.waiting`, `run.resumed`, token audit events.
- `backend/cmd/orbitmesh/main.go`: initialize `boot_id`, run recovery sweep before serving API.

## Testing Plan
- Unit: projector precedence and derivation table (all status combinations).
- Unit: token lifecycle edge cases (expired, double-consume, run/session mismatch).
- Unit: storage atomicity and sequence monotonicity for JSONL appends.
- Integration: crash/restart mid-run -> interruption record + correct API/SSE status.
- Integration: waiting timeout and stale-token resume rejection paths.
- Regression: existing session CRUD/SSE workflows remain passing with additive recovery fields.

## Rollout Notes
- Phase 1: write new artifacts in parallel, read old + new (dual-read).
- Phase 2: projector-driven status for API/SSE, with metrics on interrupted detections.
- Phase 3: default to new resume-token flow; keep compatibility shim for old sessions.
- Phase 4: remove legacy executor internals after soak period and test stability.

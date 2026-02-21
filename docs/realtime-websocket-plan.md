# Realtime WebSocket Bus Plan

## Objective
- Replace fragmented streaming paths with one global WebSocket pub/sub stream.
- Use snapshot-on-subscribe + live updates thereafter.
- Do not implement replay/cursors/history recovery. If disconnected, reconnect and re-sync via snapshot.
- Generate TypeScript payload types from Go types using `github.com/gzuidhof/tygo`.

## Non-Goals
- No replay protocol (`Last-Event-ID`, per-topic offsets, durable event cursor).
- No schema versioning layer.
- No immediate deletion of all SSE endpoints during initial rollout.

## Architecture

### Transport
- Single endpoint: `GET /api/realtime` (WebSocket upgrade).
- One connection per frontend app instance/tab.
- One protocol envelope for all messages.

### Pub/Sub Model
- Client subscribes to one or more topics.
- Server sends:
  1. `snapshot` messages for subscribed topics (current state)
  2. `event` messages for subsequent changes
- On disconnect:
  - Client reconnects.
  - Client re-subscribes.
  - Server sends fresh snapshots.

### Initial Topics
- `sessions.state`
  - Snapshot: all visible sessions with derived state.
  - Event: per-session state changes.
- `sessions.activity:<session_id>` (optional phase 2)
  - Snapshot: latest activity/messages for the session.
  - Event: new activity entries.
- `terminals.state` (optional phase 2)
  - Snapshot: terminal list and live status summary.
  - Event: terminal lifecycle/snapshot updates.

## Wire Protocol

### Client -> Server
- `subscribe`
  - `{ topics: string[] }`
- `unsubscribe`
  - `{ topics: string[] }`
- `ping` (optional keepalive)

### Server -> Client
- `snapshot`
  - `{ topic: string, payload: ... }`
- `event`
  - `{ topic: string, payload: ... }`
- `error`
  - `{ message: string }`
- `pong`

### Envelope (Go source of truth)
- Define all protocol structs in Go under a dedicated package (below).
- Generate TS types from these Go structs via tygo.

## Type Generation With Tygo

### Tooling Decision
- Use `github.com/gzuidhof/tygo` for Go -> TypeScript type generation.
- Go structs are the source of truth.

### Proposed Paths
- Go protocol types: `backend/pkg/realtime/types.go`
- Tygo runner config: `backend/cmd/typegen-realtime/main.go` (or script wrapper)
- Generated TS output: `frontend/src/types/generated/realtime.ts`

### Generation Workflow
1. Update Go structs in `backend/pkg/realtime/types.go`.
2. Run generator command (`go run ./backend/cmd/typegen-realtime`).
3. Commit updated generated file.

### CI Guardrail
- Add CI check that verifies generated types are up-to-date:
  - Run generator.
  - Fail if `git diff -- frontend/src/types/generated/realtime.ts` is non-empty.

### Developer Ergonomics
- Add scripts:
  - root `package.json` (or backend Make target): `generate:realtime-types`
  - optional `check:realtime-types`

## Backend Implementation Plan

### 1) Realtime Hub
- Add package: `backend/internal/realtime`
  - `hub.go`: client registry, topic subscriptions, broadcast fanout.
  - `client.go`: per-connection writer queue and lifecycle.
  - `topics.go`: topic constants and validation helpers.
- Behavior:
  - Bounded outbound channel per client.
  - On backpressure overflow: disconnect client.

### 2) WebSocket API Handler
- Add `backend/internal/api/realtime_ws.go`.
- Wire route in `backend/internal/api/handler.go`:
  - `GET /api/realtime`.
- Handler responsibilities:
  - Upgrade HTTP to WS.
  - Parse client envelopes.
  - Manage subscribe/unsubscribe.
  - Send snapshots immediately after subscribe.

### 3) Snapshot Providers
- Add `backend/internal/realtime/snapshot_provider.go`.
- Implement topic-specific snapshot fetchers:
  - `sessions.state` from executor/list + derived state.
  - Later topics in phase 2.

### 4) Event Publishers
- Bridge existing service events into realtime topic events.
- Hook points:
  - Session state transitions in service layer (executor/coordinator).
  - Recovery state changes during startup sweep.
- Publish normalized `sessions.state` event payloads.

### 5) Startup Recovery + Initial Sync
- Keep existing startup recovery.
- Realtime layer does not persist cursors.
- After reconnect, frontend always receives a full snapshot as source of truth.

## Frontend Implementation Plan

### 1) Shared Realtime Client
- Add `frontend/src/realtime/client.ts`.
- Responsibilities:
  - Open one WS connection.
  - Reconnect with backoff.
  - Re-send active subscriptions after reconnect.
  - Dispatch incoming envelopes to topic handlers.

### 2) Session Store Integration
- Update `frontend/src/state/sessions.ts`:
  - Subscribe once to `sessions.state`.
  - Apply snapshot to replace/refresh current sessions list.
  - Apply event deltas for fast updates.
  - Keep minimal fallback refresh only when WS unavailable.

### 3) Type Usage
- Import only generated tygo types from `frontend/src/types/generated/realtime.ts` for protocol payloads.
- Keep API-local helper types only for UI transforms.

## Rollout Phases

### Phase 1 (MVP)
- Implement WS hub, endpoint, protocol, `sessions.state` snapshot + events.
- Frontend sessions store consumes WS stream.
- Keep current SSE paths active as fallback.

### Phase 2
- Move additional streams (`sessions.activity`, terminal updates) onto same WS bus.
- Reduce legacy per-feature stream code.

### Phase 3
- Remove redundant SSE endpoints no longer used.
- Keep only endpoints still needed by external consumers (if any).

## Testing Plan

### Backend
- Unit tests (`backend/internal/realtime/*_test.go`):
  - subscribe/unsubscribe routing
  - snapshot-on-subscribe behavior
  - event fanout to multiple clients
  - overflow disconnect behavior
- API tests (`backend/internal/api/*_test.go`):
  - WS upgrade and message flow
  - malformed client message handling

### Frontend
- Unit tests:
  - reconnect + re-subscribe path in `frontend/src/realtime/client.test.ts`
  - sessions store snapshot/event apply behavior in `frontend/src/state/sessions.test.tsx`

### Integration
- E2E smoke:
  - open app -> snapshot received
  - trigger session state change -> UI updates without polling
  - force disconnect -> reconnect -> snapshot refreshes correctly

## Risks and Mitigations
- Risk: client queue overflow under high event rate.
  - Mitigation: bounded queue + disconnect/reconnect + snapshot rehydrate.
- Risk: temporary dual-stream complexity during migration.
  - Mitigation: limit phase 1 to `sessions.state`, then migrate incrementally.
- Risk: generated types drift.
  - Mitigation: tygo generation in CI and local scripts.

## Expected File Changes (Phase 1)
- Backend
  - `backend/internal/api/handler.go`
  - `backend/internal/api/realtime_ws.go` (new)
  - `backend/internal/realtime/hub.go` (new)
  - `backend/internal/realtime/client.go` (new)
  - `backend/internal/realtime/topics.go` (new)
  - `backend/internal/realtime/snapshot_provider.go` (new)
  - `backend/pkg/realtime/types.go` (new)
  - `backend/cmd/typegen-realtime/main.go` (new)
- Frontend
  - `frontend/src/realtime/client.ts` (new)
  - `frontend/src/types/generated/realtime.ts` (generated)
  - `frontend/src/state/sessions.ts`

## Success Criteria
- Frontend session list/status updates in near real time over one WS connection.
- Reconnect reliably restores state via snapshot without replay logic.
- Tygo-generated TS types are used for realtime protocol messages and validated in CI.

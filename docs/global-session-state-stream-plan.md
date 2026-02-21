# Global Session State Stream Plan

## Goal
- Add one global session-state stream so the frontend can react to session state changes immediately without per-session polling.

## Transport
- Use SSE (not websocket) to match existing backend/frontend event-stream infrastructure.

## Backend Design
- Add `GET /api/sessions/events` for global state updates.
- Stream only state-change events (`session_state`) with payload:
  - `event_id`, `timestamp`, `session_id`, `derived_state`
  - optional: `reason`, `source`, `run_attempt_id`
- Reuse `EventBroadcaster` wildcard subscribe (`sessionID == ""`) and filter to status events.
- Derive state using `AgentExecutor.DeriveSessionState(...)` at emit time.

## Replay/Reconnect
- Support `Last-Event-ID` and `last_event_id` query param on global endpoint.
- Extend broadcaster with global history ring buffer to replay cross-session events for wildcard subscribers.

## Frontend Integration
- Subscribe once globally (store-level), then fan updates into session store.
- Update session row state/updated-at on event.
- If event sequence gap detected, trigger immediate refresh.
- Keep low-frequency fallback polling only when stream unhealthy.

## Failure/Backpressure
- Keep non-blocking broadcast semantics.
- Track dropped events (counter/log) for observability.
- Use bounded replay history to cap memory.

## Testing
- Backend:
  - global endpoint emits state events for multiple sessions
  - replay from `Last-Event-ID` for wildcard subscriber
  - heartbeat/header behavior
- Frontend:
  - one global subscription updates sessions store
  - reconnect path with `last_event_id`
  - fallback refresh on sequence gap/disconnect

## Rollout
1. Add global endpoint + event contract.
2. Frontend dual path: stream-first with polling fallback.
3. Remove default frequent polling after soak.

## Expected Files
- `backend/internal/api/handler.go`
- `backend/internal/api/sse.go` (or `backend/internal/api/sse_global.go`)
- `backend/internal/service/events.go`
- `backend/internal/service/session_status_deriver.go`
- `backend/pkg/api/types.go`
- `frontend/src/api/sessions.ts`
- `frontend/src/api/client.ts`
- `frontend/src/types/api.ts`
- `frontend/src/state/sessions.ts`
- `frontend/src/utils/eventStream.ts`

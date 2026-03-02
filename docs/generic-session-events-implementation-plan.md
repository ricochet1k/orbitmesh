# Generic Session Events Implementation Plan

## Goal
- Replace provider-specific fallback metadata spam (`codex_notification`, `codex_item`) with provider-agnostic domain/API events that any provider can emit and the frontend can render without provider-specific branches.
- Preserve unknown payloads as a controlled fallback path for observability.
- Ensure persisted session history keeps combined reasoning/tool-call state without storing every raw delta event.

## Key Requirement: Delta Semantics
- [x] Define a shared delta contract in domain/API docs and types.
  - Delta events append to the last open message of the same logical stream/kind.
  - Non-delta events finalize or replace the current open message for that stream.
  - If no prior message exists, start a new message instead of dropping the delta.
  - Deltas must be scoped by stable stream identity (for example: tool-call ID, reasoning item ID, progress stream ID).
  - Out-of-order deltas (older revision/event_id) must be ignored.
- [x] Persist only combined state for high-frequency streams.
  - Store compact append/merge projections in JSONL instead of writing one full message per delta.
  - Preserve enough metadata (`target_message_id`, kind, timestamp) to rebuild deterministic history.

## Current Investigation Snapshot (2026-03-01)
- [x] Sample session analyzed: `be6da8b5d3484f5e6bb2c73ceee597ee`.
- [x] Collected unique unhandled method families instead of dumping full logs.
- [x] Confirmed high-volume fallbacks are currently persisted as system metadata messages.
- [x] Confirmed frontend session viewer path is generic today and should stay that way.

## Phase 1: Domain and API Event Model (Provider-Agnostic)
- [x] Add new generic domain events in `backend/internal/domain/event.go`.
  - `progress` (streaming intermediate text/status updates)
  - `resource_usage` (tokens/rate/cost/limits)
  - `action_request` (approval/input required)
  - `artifact_update` (diff/file-change preview)
- [x] Add strongly typed payload structs for each new event kind.
- [x] Extend API event conversion in `backend/internal/api/sse.go`.
- [x] Extend API types in `backend/pkg/api/types.go`.
- [x] Extend frontend SSE union/types in `frontend/src/types/api.ts`.
- [x] Ensure unknown events still serialize through `metadata` fallback.

## Phase 2: Provider Translation Layer (Backend Only)
- [x] Implement canonical notification normalization in Codex provider (backend only).
- [x] Map codex reasoning deltas and summaries to generic `progress` events.
- [x] Map command begin/output/end flows to `tool_call` + `progress` (tool output stream).
- [x] Map token/rate updates to `resource_usage`.
- [x] Map approval requests/file-change prompts to `action_request` (+ `artifact_update` when diff exists).
- [ ] Keep only truly unknown provider notifications as fallback metadata.

## Phase 3: Session Projection and Persistence
- [ ] Update projector logic in `backend/internal/service/run_state_projector.go` for new event types.
- [ ] Persist new event kinds in session messages with generic message kinds (not provider names).
- [ ] Ensure delta append logic is stream-aware and idempotent.
- [ ] Mark legacy provider fallback keys as internal when equivalent structured events exist.
- [ ] Avoid transcript noise from internal-only metadata keys.

### Phase 3a: JSONL Delta Efficiency Improvements
- [x] Generalize JSONL delta rewrite support beyond output-only deltas.
- [x] Add projection mode(s) for generic message-kind deltas (for example thought/reasoning and other appendable streams).
- [x] Keep legacy `append_output_delta` records backward-compatible during replay.
- [x] Ensure rebuild logic merges both legacy and new generalized delta projections.
- [x] Add targeted tests for multi-kind delta merge/replay correctness.

## Phase 4: Frontend Rendering (Still Provider-Agnostic)
- [x] Add generic handling in `frontend/src/hooks/useSessionData.ts` for new event kinds.
- [ ] Implement stream-safe delta application by logical stream identity.
- [ ] Add transcript rendering labels/styles for generic kinds in `frontend/src/components/SessionTranscript.tsx`.
- [ ] Add compact UI cards/rows for `resource_usage` and `action_request`.
- [ ] Keep unknown events visible through a generic fallback renderer (collapsed by default in dev-friendly format).
- [ ] Do not add provider-specific branches in session viewer components/hooks.

## Phase 5: Performance and Safety
- [ ] Add bounded snapshot behavior for persisted session messages in realtime snapshot provider.
- [ ] Ensure high-frequency deltas coalesce correctly without duplicating finalized content.
- [ ] Verify event ordering behavior with `event_id` and per-stream revision guards.

## Testing Plan
- [x] Backend unit tests for new domain/API event conversions.
- [x] Backend Codex mapping tests for each sampled method family.
- [ ] Backend regression tests for unknown-notification fallback behavior.
- [x] Backend JSONL storage tests for generalized delta rewrite behavior across multiple message kinds.
- [ ] Frontend unit tests for generic event parsing and delta append behavior.
- [ ] Frontend transcript rendering tests for all new generic kinds.
- [ ] End-to-end replay test from captured notification fixture to ensure no metadata flood.

## Documentation Plan
- [x] Add provider-agnostic event type reference doc.
- [x] Link code to docs from domain/api/frontend type definitions.
- [ ] Add checklist item in PR template or review notes to keep docs synced when event types change.

## Suggested Method Family Mapping Matrix (Initial)
- [ ] reasoning deltas (`reasoning_content_delta`, `summaryTextDelta`, `agent_reasoning_delta`) -> `progress` (channel `reasoning`)
- [ ] command output deltas (`exec_command_output_delta`, `outputDelta`) -> `progress` (channel `tool_output`, stream by tool call ID)
- [x] command begin/end -> `tool_call` state transitions
- [ ] token/rate updates (`token_count`, `tokenUsage`, `rateLimits`) -> `resource_usage`
- [ ] approval requests (`requestApproval`, `apply_patch_approval_request`) -> `action_request` (+ `artifact_update`)
- [ ] plan updates (`plan_update`) -> existing `plan`

## Progress Log
- [x] 2026-03-01: Investigation completed and implementation plan written.
- [x] 2026-03-01: Implemented generic delta persistence path (`append_delta`) and replay support for non-output kinds; added thought-delta plumbing and Codex command begin/end + reasoning-delta handling with tests.
- [x] 2026-03-01: Added generic event model (`progress`, `resource_usage`, `action_request`, `artifact_update`) across domain/API/frontend types and codex mapping, plus documentation links.
- [x] 2026-03-01: Phase 1 in progress.
- [x] 2026-03-01: Phase 2 in progress.
- [x] 2026-03-01: Phase 3 in progress.
- [x] 2026-03-01: Phase 4 in progress.
- [ ] 2026-03-01: Phase 5 in progress.

## Notes for Ongoing Updates
- Update this file at the start and end of each implementation slice.
- Check off completed items immediately after tests pass for that slice.
- Add brief dated notes in the Progress Log when scope changes.

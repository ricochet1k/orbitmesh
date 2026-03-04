# Session Streaming/Storage Audit (2026-03-04)

## Scope and intent

This audit covers:

- Frontend live streaming vs reload rendering behavior
- Backend streaming event pipeline and persistence model
- Provider translation paths (Claude, ClaudeWS, Codex)
- Tests claiming streaming/reload parity

The specific question was: why do rich live renders (reasoning, tool cards) degrade after reload, despite tests that appear to guard parity?

Additional design goal (added 2026-03-04):

- Session viewer should not load full transcript eagerly.
- Viewer should become a virtualized infinite list that initially loads from the end (newest-first fetch window).
- Storage should support fast backward reads of recent messages while preserving delta-merge semantics.

---

## Executive summary

The codebase currently has **multiple competing transcript models** and **multiple persistence shapes** that are merged late in the frontend. Live rendering is driven by rich typed events (`tool_call`, `progress`, etc.), but reload reconstruction often comes from lossy projections (`contents` strings) and generic activity entries. This causes structural mismatch by design, not by a small bug.

The core issue is architectural:

1. **No single canonical transcript representation** shared by live and reload paths.
2. **Different pipelines produce different data granularity**:
   - Live stream: rich typed event payloads.
   - Reload: activity extractor entries and/or compact message-log strings.
3. **Frontend merges heterogenous sources** (`entries` + `messages`) with different IDs and semantics.
4. **Tests mostly assert text presence, not structural parity**, so they pass while UX regresses.

Result: rich cards appear live, but reload rehydrates with flatter/legacy forms.

With the newest-first infinite-scroll requirement, current architecture has a second major risk: it mixes correctness and storage compaction concerns inside append-time rewriting, which increases complexity and makes paginated backward reads harder to reason about.

---

## Current architecture: what actually happens

## 1) Live event rendering path (works better)

Frontend hook `useSessionData` handles streamed event types in `applyStreamEvent` and builds rich `TranscriptMessage` objects with payloads:

- `frontend/src/hooks/useSessionData.ts:213`
- tool-call incremental merge logic: `frontend/src/hooks/useSessionData.ts:426`
- reasoning/progress delta merge logic: `frontend/src/hooks/useSessionData.ts:530`

Rich transcript cards depend on `message.payload`:

- `frontend/src/components/SessionTranscript.tsx:289`
- `getRichPayload` returns only `message.payload`: `frontend/src/components/SessionTranscript.tsx:562`

So live looks good because stream handlers construct the payload shape needed by rich UI cards.

## 2) Reload/history paths (where parity breaks)

### 2a) SSE fallback history path

When WebSocket is not used, history comes from `/api/sessions/{id}/activity`:

- `frontend/src/hooks/useSessionData.ts:653`
- `frontend/src/api/sessions.ts:132`
- backend endpoint: `backend/internal/api/activity.go:23`

That endpoint reads PTY activity logs (extractor entries), not canonical stream events:

- `backend/internal/api/activity.go:115`

Formatting for these entries is generic/lossy:

- `frontend/src/utils/activityFormatting.ts:4`

This is fundamentally different data from live event payloads.

### 2b) Realtime snapshot path

Realtime snapshots include both `entries` and `messages`:

- snapshot shape: `backend/pkg/realtime/types.go:53`
- snapshot assembly: `backend/internal/realtime/snapshot_provider.go:59`

Frontend merges both collections:

- `frontend/src/hooks/useSessionData.ts:608`
- `frontend/src/hooks/useSessionData.ts:630`

But session messages are converted with `payload: undefined`:

- `frontend/src/hooks/useSessionData.ts:951`

Therefore rich cards cannot render from snapshot messages after reload, even when message content survives.

## 3) Backend persistence model contributes to lossiness

Message log records persist `kind + contents (+ raw)` with projection flags:

- record type: `backend/internal/storage/jsonl_log_storage.go:33`
- projections and delta rewrite behavior: `backend/internal/storage/jsonl_log_storage.go:25`, `backend/internal/storage/jsonl_log_storage.go:337`

Projection at runtime often stores flattened strings for tool lifecycle:

- `backend/internal/service/run_state_projector.go:37`
- tool running state serialized as string JSON payload: `backend/internal/service/run_state_projector.go:53`
- tool completion stored as separate tool_response-like string payload: `backend/internal/service/run_state_projector.go:108`

This is not equivalent to live `tool_call` event payload transitions used by frontend rich cards.

---

## Why the parity tests give false confidence

There *are* tests, but they are not asserting what users care about.

## 1) E2E parity tests assert text presence, not DOM/model parity

File: `frontend/tests/e2e/acp-echo-transcript-parity.spec.ts`

Examples:

- Checks `article[data-kind='tool_call']` count and text contains `pwd`.
- Checks transcript contains strings like `{"content":"Hello "}`.

What is missing:

- No snapshot of transcript item JSON/shape before vs after reload.
- No assertion that rich card type (bash/read/edit/generic/progress/action) remains identical.
- No assertion that message IDs/merge behavior are preserved across reload.
- No assertion that `payload` survives reload.

Important: tests disable WebSocket (`window.WebSocket = undefined`), forcing SSE path:

- `frontend/tests/e2e/acp-echo-transcript-parity.spec.ts:87`

So realtime snapshot parity is largely untested.

## 2) Hook tests validate buffering mechanics, not presentation equivalence

File: `frontend/src/hooks/useSessionData.test.ts`

Strengths:

- good coverage of buffer-before-history and dedupe intent
- good coverage of event-type handling branches

Gaps:

- no golden parity test: live event stream result vs reconstructed reload result
- no assertion that reconstructed messages keep rich payloads/cards
- mocked `event_id` values in activity entries don't match backend reality (backend activity conversion currently omits `event_id`; see below)

## 3) Backend realtime tests validate transport, not semantic parity

File: `backend/internal/api/realtime_ws_test.go`

Covers subscribe/snapshot/event flow, but not equality of rendered message model after hydration.

---

## Concrete mismatches and defects found

## A. Activity entries carry `event_id` in API type but backend does not populate it

- API type includes field and frontend dedupe logic relies on it:
  - `backend/pkg/api/types.go:314`
  - `frontend/src/hooks/useSessionData.ts:888`
- Actual conversion omits it:
  - `backend/internal/api/activity.go:155`
  - `backend/internal/realtime/snapshot_provider.go:174`

Implication: watermark dedupe logic is partially inert/misleading in real runs.

## B. Realtime generated TS types lose fields needed for parity

Generated realtime `SessionMessage` has only `id/kind/contents/timestamp`:

- `frontend/src/types/generated/realtime.ts:64`

No payload/raw fields are typed there; frontend conversion drops payload by design (`payload: undefined`), blocking rich card rehydration.

## C. Two transcript stores are merged without unified identity semantics

Frontend merges:

- activity entries as `id: activity:<entry-id>`
- message log messages as `id: message:<message-id-or-ts>`

See:

- `frontend/src/hooks/useSessionData.ts:964`
- `frontend/src/hooks/useSessionData.ts:951`

This guarantees no natural dedupe between the two sources and invites duplication/order drift.

## D. Huge frontend orchestrator hook indicates high accidental complexity

`useSessionData` is ~1200 lines with high complexity (roam reports cognitive 395).

- `frontend/src/hooks/useSessionData.ts`

It handles transport, buffering, hydration, event parsing, projection, UI filtering, and session intel. That breadth makes regressions likely and parity invariants hard to reason about.

## E. Delta rewrite in JSONL storage is overcomplicated and high risk

`JSONLLogStorage.Append` can rewrite entire log files to merge deltas:

- `backend/internal/storage/jsonl_log_storage.go:337`

This is sophisticated but brittle (offset/index maintenance, temp rewrite, shifting lines) and still does not preserve rich structured replay shape used by frontend cards.

## F. Provider-specific logic leaked into generic service layer

`isInternalMetadataKey` in service contains Codex-specific keys:

- `backend/internal/service/run_state_projector.go:202`

This violates the project’s provider-boundary guidance (provider specifics should stay under provider directories/settings UI/tests), and increases coupling/entropy.

## G. Live/reload contracts are implicit and undocumented in code

There is no explicit “render model contract” type shared by:

- stream handlers,
- storage projection,
- snapshot/API history,
- frontend renderer.

Instead, each layer has ad-hoc mapping logic.

---

## Architecture quality assessment

## Poor architecture / overengineering patterns

1. **Multiple sources of truth** for transcript semantics:
   - domain event stream
   - message projection log
   - PTY activity extractor log
   - frontend local merge model

2. **Leaky abstractions**:
   - generic layers know provider-noise keys
   - frontend presentation behavior depends on transport path

3. **Projection mismatch**:
   - persistence stores flattened human strings where renderer expects structured payload.

4. **Complexity concentration**:
   - monolithic hook orchestrates too many responsibilities.

5. **Test oracle weakness**:
   - tests assert “contains text” instead of canonical-model equality.

6. **Mutation-heavy storage internals**:
   - in-place delta rewriting increases fragility without solving parity contract.

---

## Missing design principles (root cause)

These principles are currently missing or inconsistently applied:

1. **Single Source of Truth**
   - There should be one canonical transcript event/message model used for both live and reload.

2. **Deterministic Replay / Event Sourcing discipline**
   - Reload should be a pure replay of persisted canonical events/projections, yielding identical view state.

3. **Separation of Concerns**
   - Transport (SSE/WS), projection, persistence, and view rendering are currently entangled.

4. **Schema-first contract boundaries**
   - Rich render payload schema is not persisted/rehydrated as a first-class contract.

5. **Provider isolation (anti-leakage)**
   - Provider-specific noise handling exists in generic service code.

6. **Invariants-driven testing**
   - No hard invariant test that `hydrate(replay(events)) == reduce(live_stream(events))`.

---

## What to do: robust simplification plan

## Design adjustment for newest-first virtualized transcript

The plan should explicitly optimize for:

- deterministic rendering parity,
- append throughput,
- cheap "tail" reads,
- cheap pagination backward,
- no full-log rewrite on every delta.

Recommended shape:

1. Keep raw event log append-only (immutable).
2. Maintain a materialized "render-item" log where each line is one renderable transcript item.
3. Maintain a small sidecar index for byte offsets/page checkpoints to support reverse paging without reading the whole file.
4. Keep per-stream merge state in memory + durable checkpoint (stream_id/message_id -> current render-item id), then flush finalized item updates as new records (logical upsert), not in-place file edits.

This preserves your goal (one renderable line per item) while avoiding fragile whole-file rewrites.

## Phase 1 (highest ROI): establish one canonical transcript model and paging contract

Define a provider-agnostic **TranscriptEvent/TranscriptItem** shape that includes structured payload for rich cards (tool/progress/action/artifact/thought/output).

- Stream handlers produce this model.
- Persistence stores this model (or deterministic projection input sufficient to re-create it exactly).
- History/snapshot endpoints return this model directly.
- Frontend renderer consumes this model without path-specific branching.
- Add paging contract now: `before`, `after`, `limit`, stable sort key (`seq`), and guaranteed deterministic order.

## Phase 2: stop mixing unrelated history channels and add newest-first API

Do not merge PTY extractor entries with session message projections in the same transcript feed unless they are normalized into the canonical item schema first.

If PTY activity remains useful, expose it as a separate panel/feed.

Implement transcript endpoint for virtual scrolling:

- `GET /api/sessions/{id}/transcript?before=<seq>&limit=<n>` -> returns newest page before cursor.
- Initial load uses `before` absent and returns last `n` items.
- Include `next_before` cursor for older pages.

## Phase 3: replace stringified tool lifecycle persistence and remove in-place delta rewrites

Persist structured tool lifecycle records (`id/name/status/input/output`) rather than encoded string payloads.

This directly enables rich card parity on reload.

For delta merging, switch from "rewrite previous JSONL line" to one of:

1. **Preferred**: append-only logical upsert records (`item_update` with `target_item_id` + `revision`), resolve to latest revision at query time with short-range index assistance.
2. **Alternative**: periodic compaction job that rewrites cold segments offline, never on hot append path.

This keeps append path simple and makes reverse pagination/indexing more robust.

## Phase 4: simplify frontend hook architecture for virtualization

Split `useSessionData` into:

- transport adapters (SSE/WS)
- event reducer (pure)
- hydration/replay reducer (pure, same reducer)
- UI selectors (filtering/todo/intel)
- virtual list data source (`loadRecent`, `loadBefore`, window cache)

Then parity invariant becomes straightforward to test.

## Phase 5: strengthen tests with canonical parity oracle

Add tests that compare normalized transcript items pre/post reload:

- same item count/order
- same stable IDs
- same `kind/type/open`
- same structured payload keys/values

Add one strict E2E test that serializes transcript item models from DOM data attributes or debug endpoint before and after reload and compares deep equality.

## Phase 6: enforce provider boundary rule

Move provider-specific metadata-key filtering from `backend/internal/service/**` into provider translators.

Service layer should consume only provider-agnostic event kinds.

---

## Specific test improvements (actionable)

1. In `frontend/tests/e2e/acp-echo-transcript-parity.spec.ts`, replace `toContainText`-only checks with structured parity assertions.
2. Add a hook-level golden test:
   - feed deterministic event sequence to live reducer,
   - feed persisted replay/snapshot sequence to hydration reducer,
   - deep-compare resulting message arrays.
3. Add backend contract tests that snapshot/history payloads contain enough fields for frontend rich cards.
4. Add regression test for activity `event_id` propagation or remove that dedupe path if event IDs are unavailable.
5. Add backend pagination tests proving tail-first transcript reads do not require full file scans.
6. Add performance test for large transcripts (for example 100k messages) validating bounded-memory first paint and older-page fetch latency.

---

## Final diagnosis

The live-vs-reload discrepancy is not a small rendering bug; it is a **model consistency failure across system boundaries**.

Until the system adopts one canonical transcript schema and validates strict replay parity in tests, regressions will continue even if individual handlers are patched.

The fastest path out is:

- canonical transcript schema,
- single reducer semantics for live + replay,
- structured append-only persistence with paging index,
- strict parity tests.

For your specific storage concern: the current JSONL rewrite strategy is understandable, but it is coupling hot-path writes with compaction. A two-log + index design (event log + render-item log with logical upserts and offline compaction) better satisfies your stated goals: exact rendering parity, one-line render items, and efficient newest-first virtual scrolling.

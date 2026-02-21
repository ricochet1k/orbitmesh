# Plan: `useSessionData`

## Problem

The stream and history fetch are currently separate concerns managed by separate hooks
(`useSessionStream` + `useSessionTranscript`) called from separate effects in two different
components (`AgentDock.tsx` and `$sessionId.tsx`). This causes:

- **A race condition**: stream events arrive while the history fetch is still in-flight. There
  is no coordination, so the same content can appear twice — once from REST history and once
  from the stream.
- **Duplication of stream setup**: both components independently wire up `createEffect` +
  `useSessionStream` with nearly identical handler logic.
- **Invisible coupling**: `useSessionTranscript` handles activity history *and* stream events,
  but the stream is set up by the caller, making the dependency between the two implicit and
  easy to break silently.

## Solution

Replace both hooks with a single `useSessionData` that owns the full session data lifecycle:
open the stream, fetch history, coordinate the two so duplicates are impossible, and expose a
clean signal-based API to UI components.

---

## Interface

```ts
// src/hooks/useSessionData.ts

export type StreamStatus =
  | "idle"          // no sessionId yet
  | "connecting"
  | "live"
  | "reconnecting"
  | "disconnected"
  | "error"

export interface SessionDataOptions {
  sessionId: Accessor<string>
  /**
   * Whether the current user can inspect session history.
   * null = permissions still loading (do nothing yet).
   * false = loaded but denied (open stream, skip history fetch).
   * true = open stream and fetch history.
   */
  canInspect: Accessor<boolean | null>
  eventsUrl: Accessor<string>
  streamOptions?: Pick<
    StreamOptions,
    "connectionTimeoutMs" | "preflight" | "trackHeartbeat" | "heartbeatTimeoutMs"
  >
  onStatusChange?: (state: SessionState) => void
  onSessionRefetchNeeded?: () => void
}

export interface SessionData {
  // Transcript
  messages: Accessor<TranscriptMessage[]>
  filteredMessages: Accessor<TranscriptMessage[]>
  filter: Accessor<string>
  setFilter: (v: string) => void
  autoScroll: Accessor<boolean>
  setAutoScroll: (v: boolean) => void
  historyLoading: Accessor<boolean>
  historyCursor: Accessor<string | null>  // null = no more pages
  loadEarlier: () => void

  // Stream
  streamStatus: Accessor<StreamStatus>
}
```

`canInspect` being `null` (not yet loaded) vs `false` (loaded, denied) matters: `null` means
wait; `false` means connect the stream but skip the history fetch.

---

## Internal structure

Three private sections inside a single hook function. No exported sub-hooks.

### Section 1 — Message state

Pure signal manipulation with no API calls or reactive effects. Contains:

- `messages` signal and `setMessages`
- `mergeMessages(incoming, { sort? })` — upsert by ID with revision ordering
- `pushMessage(msg)` — append
- `applyStreamEvent(sseType, rawEvent)` — calls `parseSSEEvent`, then dispatches to the
  appropriate mutation (the current `handleEvent` switch, moved here verbatim)
- `toActivityMessage(entry)` and `mapActivityKindToType(kind)` — unchanged from current code

### Section 2 — Stream + history coordination

One `createEffect` tracking `sessionId()` and `canInspect()`. Runs when both are non-null.

```
onCleanup:
  close stream
  clear event buffer
  reset historySettled flag

On each run:
  1. Reset message state: setMessages([])
  2. Start stream via useSessionStream.
       onEvent: if historySettled → applyStreamEvent(type, event)
                else              → buffer.push({ type, event })
  3. If canInspect() === true:
       kick off history fetch by setting paginationCursor(null)
     else:
       mark historySettled = true immediately (no history to wait for)
  4. When history resource resolves (a separate createEffect watching activityPage()):
       watermark = highest event_id seen across loaded entries (0 if none carry event_id yet)
       mergeMessages(entries.map(toActivityMessage), { sort: true })
       historySettled = true
       for each { type, event } in buffer:
         if event_id(event) > watermark → applyStreamEvent(type, event)
       buffer = []
```

`historySettled` and `buffer` are plain `let` variables inside the effect closure — not
signals. They only need to be read synchronously inside the `onEvent` callback; they are reset
by `onCleanup` on every re-run, so stale values from a previous session cannot leak.

### Section 3 — Pagination

`loadEarlier` sets `paginationCursor` to `historyCursor()`, which triggers `createResource` to
fetch the next (older) page. Newly loaded entries are merged with `sort: true`, identical to
the current behaviour.

---

## Watermark and deduplication

The watermark prevents stream events from duplicating content already in the history response.
It is computed as `max(entry.event_id)` across all entries in the history page.

This requires `ActivityEntry` to carry the `event_id` of the SSE event that created it. See
the backend change below. Without that field the watermark falls back to `0`, which means all
buffered events are replayed — safe but potentially duplicating content if an event arrived
and was persisted *and* buffered before history loaded. The `event_id`-based watermark is the
correct long-term solution.

---

## Backend change required

Add `EventID int64 json:"event_id,omitempty"` to `ActivityEntry` in `pkg/api/types.go` and
populate it wherever entries are written to storage. This is a one-field, additive, backwards-
compatible change — old clients ignore the field, new clients use it for watermarking.

---

## Caller shape after the change

**`$sessionId.tsx`:**
```ts
const data = useSessionData({
  sessionId,
  canInspect,
  eventsUrl: () => apiClient.getEventsUrl(sessionId()),
  streamOptions: {
    connectionTimeoutMs: TIMEOUTS.STREAM_CONNECTION_MS,
    preflight: !isTestEnv(),
    trackHeartbeat: true,
  },
  onStatusChange: (state) => setSessionStateOverride(state),
  onSessionRefetchNeeded: () => void refetchSession(),
})
```

All remaining state in the component is pure UI: `composerError`, `composerPending`,
`terminalStatus`, `actionNotice`, the scroll handler.

**`AgentDock.tsx`:**
```ts
const data = useSessionData({
  sessionId,
  canInspect,
  eventsUrl: () => apiClient.getEventsUrl(sessionId()),
  streamOptions: {
    connectionTimeoutMs: TIMEOUTS.DOCK_STREAM_CONNECTION_MS,
    preflight: false,
  },
  onStatusChange: (state) => setSessionStateOverride(state),
  onSessionRefetchNeeded: () => void refetchSession(),
})
```

The `lastAction` summary (dock-specific UI state) stays in `AgentDock`. It currently parses
raw SSE events a second time; after this change it reads from `data.streamStatus` and a
`data.lastEvent: Accessor<SSEEvent | null>` accessor that `useSessionData` exposes alongside
the transcript signals.

---

## What gets deleted

| File / symbol | Disposition |
|---|---|
| `useSessionTranscript.ts` | Deleted — fully absorbed |
| `createEffect(() => useSessionStream(...))` in `AgentDock` | Deleted |
| `createEffect(() => useSessionStream(...))` in `$sessionId` | Deleted |
| `"activity_entry"` in `STREAM_EVENT_TYPES` | Deleted — backend never emits this on the SSE stream |
| `sessionReady` signal in `AgentDock` | Deleted — `streamStatus` from `useSessionData` replaces it |
| `dockLoadState` in `AgentDock` (partial) | Simplified — derived from `streamStatus` + `sessionId()` |

---

## Tests

The single coordination point makes the race scenario directly testable. All tests live in
`useSessionData.test.ts`.

```ts
it("buffers stream events that arrive before history loads, replays them without duplication")
// 1. Hold getActivityEntries pending
// 2. Emit two stream events via mock EventSource
// 3. Resolve history with one entry overlapping event 1 (same event_id)
// 4. Assert messages = [history entry, event 2] — not [entry, duplicate, event 2]

it("does not open stream when canInspect is null (permissions still loading)")
it("opens stream but skips history when canInspect is false")
it("resets messages and re-fetches history when sessionId changes")
it("drains buffer on cleanup so stale events do not leak to next session")
it("applies loadEarlier correctly: fetches previous page and merges in timestamp order")
it("marks stream as disconnected on heartbeat timeout")
```

The existing `useSessionTranscript.test.ts` error-path tests migrate to
`useSessionData.test.ts` with minimal changes since `applyStreamEvent` has the same logic.

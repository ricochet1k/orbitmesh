# Transcript Paging Contract

This contract defines how session transcript history and live updates compose in the session viewer.

## `GET /api/sessions/{id}/messages`

- Response `messages` are ordered newest-first within each page.
- `before` is an exclusive cursor over the monotonic message/event sequence.
  - Omit `before` to fetch the newest page.
  - Include `before=<n>` to fetch older messages with sequence strictly less than `n`.
- Response `next_before` is the cursor for the next older page.
  - `next_before = null` means no more older pages are available.

## Rich Rendering Parity Requirements

Every history message must include enough data to render the same transcript cards as live events:

- `kind` and `contents` are required for base rendering.
- `payload` must be preserved for rich cards (tool calls, progress, action/artifact cards, metadata detail).
- `open` must be preserved so in-progress cards match live streaming state.

The frontend normalizes both history and live payloads into one `TranscriptMessage` shape and merges by message `id`.

## WebSocket Snapshot + Event Relationship

The session viewer subscribes to realtime topics over `/api/realtime`:

- `sessions.activity:{id}`
  - `snapshot` delivers the latest activity/message state for reconnect and fast catch-up.
  - `event` delivers incremental updates.
- `sessions.state`
  - `event` delivers derived session state changes.

When initial history is loading, realtime activity events are buffered. After the first page settles, buffered events are replayed, skipping any with `event_id` at or below the loaded history watermark. This prevents duplicate transcript entries while keeping late realtime events.

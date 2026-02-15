# Termemu PTY Runtime, Browser Bridge, and Persistent Activity Feed

## Status
- Proposed
- Author: OpenCode
- Date: 2026-02-07
- Scope: PTY provider stack, terminal streaming transport, extraction pipeline, and durable feed storage

## Why this change

The current PTY path treats terminal output as plain text chunks (`EventTypeOutput`), which causes major problems:

1. Terminal state is not modeled. ANSI control sequences and screen rewrites are flattened into text.
2. Raw PTY bytes are pushed into the transcript/activity stream, which is noisy and not semantically meaningful.
3. Browser terminal interaction is one-way and SSE-based, with no proper PTY input channel (keyboard, mouse, resize) over WebSocket.
4. Extraction is output-string based and does not operate on screen regions/diffs.
5. Event replay and recovery are limited to in-memory broadcaster history; durable activity history is missing.

The requested direction is to make `github.com/ricochet1k/termemu` the terminal state engine for PTY providers, add a browser WebSocket bridge, and separate raw terminal bytes from user-facing activity feed records.

## Current code baseline (relevant)

- PTY provider reads from `*os.File` and emits output strings directly: `backend/internal/provider/pty/pty.go`.
- Extractor only receives accumulated output string: `backend/internal/provider/pty/extractor.go`.
- Session stream uses SSE only (`/api/sessions/{id}/events`): `backend/internal/api/sse.go`.
- Session persistence stores snapshot JSON, not event ledger: `backend/internal/storage/storage.go`.
- Browser terminal (`xterm`) is sink-only from SSE chunks: `frontend/src/components/TerminalView.tsx`, `frontend/src/routes/sessions/$sessionId.tsx`.

## Goals

1. Use `termemu` as the canonical PTY terminal emulator in the provider.
2. Add bidirectional WebSocket terminal bridge to/from browser clients.
3. Never write raw PTY bytes into the human activity feed.
4. Continuously extract semantic activity entries from terminal screen updates (region-aware).
5. Persist both:
   - raw PTY output byte stream for deterministic replay through extractor,
   - activity feed entries in JSONL for append/read-last-N workflows.
6. Keep session lifecycle behavior and provider abstraction compatible with existing API/service layers.
7. Prefer a lightweight custom browser terminal renderer over xterm.js, using semantic line/style data from backend.

## Termemu capabilities confirmed from docs

The design below assumes and explicitly uses these `termemu` APIs:

1. `PTYBackend.StartCommand(*exec.Cmd)` and `PTYBackend.Open()` can own PTY setup.
2. `TeeBackend` can duplicate all PTY read bytes to an `io.Writer` (`SetTee`) without extra PTY readers.
3. `Frontend` callbacks (`RegionChanged`, `CursorMoved`, `ScrollLines`, etc.) provide precise render-change signals.
4. `Terminal.SendKey(KeyEvent)` and `Terminal.SendMouseRaw(...)` provide structured input encoding.
5. `Terminal.Resize(w, h)` propagates resize to backend via `SetSize`.

Important caveat: `TeeBackend` mirrors reads (PTY output), not writes (client input), so input capture must be recorded separately in provider write path if full replay fidelity is required.

Design decision: extraction replay is defined over PTY output only. Input capture is optional and debug-oriented.

## Non-goals

1. Replacing SSE for non-terminal events immediately (SSE remains for status/metrics/activity).
2. Building full OLAP analytics on top of activity logs in this phase.
3. Implementing AI-assisted extraction in phase 1 (plugin hook is included, not required to ship initially).

## Proposed architecture

### 1) PTY runtime split into explicit pipelines

Introduce a PTY runtime composed of four independent but connected stages:

1. **Byte capture layer**
   - Starts process with `termemu.PTYBackend` (`StartCommand`).
   - Wraps backend with `termemu.NewTeeBackend(...)` and `SetTee(rawLogWriter)`.
   - Uses tee for output-byte capture with near-zero extra plumbing.
   - Optionally records input bytes/events in provider write path for debugging (tee does not capture writes).

2. **Terminal state layer (`termemu`)**
   - Maintains canonical screen/cursor/attributes.
   - Emits render-affecting frontend callbacks (region changes, scroll, cursor, style, view flags/ints/strings).
   - Produces snapshots and diffs on demand.

3. **Frontend bridge layer**
   - Broadcasts terminal updates to connected browser WebSocket clients.
   - Accepts browser input events (keyboard/mouse/paste/resize) and writes translated bytes/control calls into PTY.

4. **Extraction layer**
   - Consumes termemu diffs/snapshots, not raw byte text.
   - Applies configurable extraction rules.
   - Emits semantic activity events (message/tool/task/status) and updates rolling recent entries.

### 2) New provider-internal interfaces

Add internal interfaces under `backend/internal/provider/pty/`:

```go
type TerminalRuntime interface {
    ApplyOutput(data []byte) error
    Snapshot() ScreenSnapshot
    SubscribeDiffs(buffer int) (<-chan ScreenDiff, func())
}

type BrowserBridge interface {
    Attach(sessionID string, conn BrowserConn) error
    Detach(sessionID, clientID string)
    PublishDiff(sessionID string, diff ScreenDiff)
    PublishSnapshot(sessionID string, snap ScreenSnapshot)
    HandleInput(sessionID string, input BrowserInputEvent) error
}

type ActivityExtractor interface {
    Process(diff ScreenDiff, snap ScreenSnapshot) ([]ActivityMutation, error)
}
```

These are internal composition boundaries only; external `provider.Provider` remains unchanged.

### 3) WebSocket terminal channel

Add a dedicated endpoint (per session):

- `GET /api/sessions/{id}/terminal/ws`

Behavior:
- Upgrades to WebSocket.
- Sends initial terminal snapshot after connect.
- Streams incremental terminal updates.
- Receives browser input and PTY control events.
- Supports multiple watchers and at most one active writer by default (configurable lock mode).

Message envelope:

```json
{
  "v": 1,
  "type": "terminal.diff",
  "session_id": "...",
  "seq": 123,
  "ts": "2026-02-07T20:12:00.000Z",
  "data": {}
}
```

Outbound message types:
- `terminal.snapshot`
- `terminal.diff`
- `terminal.cursor`
- `terminal.bell`
- `terminal.mode`
- `terminal.ack`
- `terminal.error`

Inbound message types:
- `input.key` (structured key info)
- `input.text` (paste/text)
- `input.mouse` (button, coords, modifiers, action)
- `input.resize` (cols, rows, px)
- `input.control` (interrupt, eof, suspend)
- `input.raw` (base64 bytes; restricted to trusted clients)

Transport rules:
- Sequence numbers per session for loss detection.
- Optional compression for diff payloads.
- Backpressure policy: bounded queue + snapshot resync on overflow.

### 4) Activity feed model (semantic, mutable recent window)

Define feed entry semantics separate from raw terminal output:

```json
{
  "id": "act_01J...",
  "session_id": "...",
  "kind": "agent_message",
  "ts": "2026-02-07T20:12:01.123Z",
  "rev": 3,
  "open": true,
  "data": {
    "text": "Running tests...",
    "tool": null,
    "region": {"top": 38, "left": 0, "bottom": 44, "right": 120},
    "confidence": 0.92
  }
}
```

Key properties:
- `id` stable across updates.
- `rev` increments when extractor refines same logical entry.
- `open=true` means entry may still change (streaming line/tool output).
- `open=false` means finalized.

The extractor updates only the last `N` open entries (default `N=8`) to avoid expensive historical rewrites.

### 5) Persistent storage

Per session directory under base storage (for example: `~/.orbitmesh/sessions/<id>/`):

1. `session.json`
   - Existing snapshot file (current behavior retained).

2. `raw.ptylog`
   - Append-only raw PTY output byte ledger (canonical replay source for extraction).
   - Output frames are sourced from `termemu.TeeBackend` (`Read` side).
   - Framed binary format:
     - varint length
     - uint8 direction (`0=out`)
     - int64 unix nanos
     - payload bytes
   - Designed for replay speed and compactness.

2a. `input.debug.jsonl` (optional)
   - Debug-only append log for browser/TTY input events.
   - Not required for extractor replay correctness.
   - Useful for reproducing interaction bugs and UI/input encoding issues.

3. `activity.jsonl`
   - Append-only feed events for fast tail reads.
   - Each line is either:
     - `entry.upsert` (new or revision),
     - `entry.finalize`,
     - `entry.delete` (rare; parser correction),
     - `checkpoint`.

4. `extractor.state.json` (optional)
   - Last applied raw offset/frame index.
   - Last emitted entry revisions for recovery.

### 6) Replay and recovery pipeline

On restart or extractor config change:

1. Load `extractor.state.json`.
2. Replay `raw.ptylog` through `termemu` from requested offset.
3. Re-run extractor to rebuild/repair recent activity window.
4. Append correction revisions to `activity.jsonl` (never rewrite existing lines).

This enables deterministic extraction iteration while preserving an auditable log.

## Extraction configuration system

### 1) Config file

Add global config path:
- `~/.orbitmesh/extractors/pty-rules.v1.json`

Schema highlights:

```json
{
  "version": 1,
  "profiles": [
    {
      "id": "claude-default",
      "match": {
        "command_regex": "(^|/)claude$",
        "args_regex": ".*"
      },
      "rules": [
        {
          "id": "assistant-message-block",
          "enabled": true,
          "trigger": {"region_changed": {"top": 10, "bottom": 40}},
          "extract": {
            "type": "region_regex",
            "region": {"top": 10, "bottom": 40, "left": 0, "right": 120},
            "pattern": "(?ms)^Assistant:\\s*(?P<text>.+?)$"
          },
          "emit": {"kind": "agent_message", "update_window": "recent_open"}
        }
      ]
    }
  ]
}
```

Rule capabilities (phase 1):
- Region change triggers.
- Region text extraction.
- Regex with named captures.
- Entry identity key function (for stable `id`).
- Upsert/finalize behavior.

Rule capabilities (phase 2):
- Multi-step parser graph.
- Tool call boundary detection.
- Confidence scoring and fallback chains.

### 2) Runtime config reload

- FS watcher or explicit API reload endpoint.
- On invalid config: keep last good config, emit warning event.

### 3) Config management UI

Add frontend "Extraction Rules" view:

- List profiles and active profile per session.
- Create/edit/disable rules.
- Region picker preview over a rendered terminal snapshot.
- Test rule against replay sample with diffed expected output.
- Publish config with validation errors inline.

Suggested backend endpoints:
- `GET /api/v1/extractor/config`
- `PUT /api/v1/extractor/config`
- `POST /api/v1/extractor/validate`
- `POST /api/v1/sessions/{id}/extractor/replay?from=...`

## Event model updates

Current `EventTypeOutput` should no longer carry raw PTY bytes for PTY sessions.

Add/repurpose metadata/event types:
- `terminal_status` (bridge state, client count)
- `activity_entry` (semantic upsert/finalize events)
- `extractor_warning`

Compatibility strategy:
- Keep `EventTypeOutput` for native providers.
- For PTY providers, output event can be disabled or reserved for human-readable summary lines only.

## API and frontend integration plan

### Backend

1. Add WebSocket handler in `backend/internal/api/` and mount route.
2. Introduce session-scoped terminal hub in service layer.
3. Extend PTY provider internals with termemu runtime and extractor worker.
4. Add storage appenders/readers for `raw.ptylog` and `activity.jsonl`.
5. Expose activity tail endpoint:
   - `GET /api/sessions/{id}/activity?limit=100&cursor=...`

### Frontend

1. Replace `xterm.js` usage in `TerminalView` with a custom HTML renderer:
   - Connect to terminal WebSocket.
   - Render snapshots/diffs as line + span data (text + style runs).
   - Send keyboard/mouse/resize events.
   - Preserve native browser text selection/copy behavior.
2. Session viewer:
   - Read activity feed from SSE `activity_entry` and paged history endpoint.
   - Stop treating terminal byte chunks as transcript messages.
3. New extraction-config UI route.

## Custom renderer design (HTML-first)

### Why custom renderer

`termemu` already provides structured terminal state (line text, style changes, changed regions), so a browser-side VT parser is unnecessary for this flow. A custom renderer reduces complexity and preserves native text selection.

### Rendering model

1. Backend sends either full snapshot or line patches containing style spans.
2. Frontend stores terminal state as an array of lines:
   - each line contains ordered spans `{ text, class/style }`.
3. DOM renders each line as a block container with inline spans.
4. Only changed lines/regions are re-rendered.

### Selection and copy

1. Use normal HTML text nodes (not canvas) to allow native selection.
2. Ensure whitespace and wrapping fidelity via CSS (`white-space: pre`).
3. Optional copy helper can normalize wrapped selections for clipboard.

### Input handling

1. Focusable terminal root captures keyboard events and maps them to websocket `input.key`/`input.text`.
2. Mouse events map to `input.mouse` with row/column translation from DOM metrics.
3. Resize observer emits `input.resize` with cols/rows derived from measured cell size.

### Performance constraints

1. Keep a bounded scrollback window in DOM (virtualize older lines).
2. Batch line patch applies per animation frame.
3. Allow periodic full snapshot resync when diff drift is detected.

## Concurrency and backpressure

1. Separate goroutines/channels for:
   - PTY read,
   - termemu apply,
   - bridge broadcast,
   - extractor,
   - storage append.
2. Bounded channels with explicit drop/resync policies.
3. Never block PTY read on browser slow consumer.
4. If bridge queue overflows, emit `terminal.error` + force snapshot refresh.
5. `termemu.Frontend` callbacks run with terminal lock held; callback handlers must be non-blocking and push work onto channels to avoid lock contention/deadlocks.

## Security and safety

1. WebSocket auth/CSRF parity with existing session controls.
2. Session-scoped authorization (`can_inspect_sessions`, plus manage action permissions for write input).
3. Input sanitization for `input.raw` and optional disable via config.
4. Log and rate-limit abusive input events.
5. Avoid shell interpretation in backend; pass bytes directly to PTY.

## Migration strategy

### Phase 1: Infrastructure
- Add termemu runtime in PTY provider while preserving current SSE stream.
- Capture `raw.ptylog` and keep extractor simple.

### Phase 2: Browser bridge
- Add WebSocket terminal endpoint and update `TerminalView` to use it.
- Keep SSE transcript untouched for non-PTY providers.

### Phase 3: Activity extraction
- Introduce configurable extractor engine and `activity.jsonl` persistence.
- Switch session viewer activity feed to semantic entries.

### Phase 4: Config UI and replay tooling
- Deliver extraction rules UI and replay validation path.
- Add operational metrics and health dashboards for extractor quality.

## Testing strategy

1. **Unit**
   - Rule parsing/validation.
   - Region extraction and mutation semantics (`rev`, `open/close`).
   - PTY log framing and recovery from partial frames.
   - Tee capture behavior (output captured once).
   - Optional debug input log behavior (disabled/enabled).

2. **Integration**
   - PTY -> termemu -> extractor -> activity JSONL end-to-end.
   - WS input roundtrip (keyboard/mouse/resize).
   - Multi-client watch + single-writer lock behavior.
   - Custom HTML renderer fidelity for styles, cursor, and selection behavior.

3. **Replay regression**
   - Golden `raw.ptylog` files with expected activity JSONL outputs.
   - Determinism checks across repeated replays.

4. **Performance**
   - Sustained high-output sessions without dropped PTY reads.
   - Tail-load of last 100 activity entries under target latency.

## Open decisions

1. Should PTY input ownership be single-writer hard lock or role-based concurrent merge?
2. Do we expose `input.raw` publicly or keep it internal/debug-only?
3. Should activity feed revisions be compacted periodically into snapshots?
4. How much of terminal history should snapshot include for reconnect (`scrollback` limits)?

## Recommended defaults

1. Single-writer lock with explicit take/release actions.
2. Disable `input.raw` by default.
3. Keep append-only JSONL; add optional offline compaction tool later.
4. Send full snapshot on connect, diffs thereafter, and periodic snapshot every 10 seconds.

## Immediate code impact map

- `backend/internal/provider/pty/pty.go`
  - replace direct output-string handling with termemu pipeline hooks.
- `backend/internal/provider/pty/extractor.go`
  - evolve from string extractor to screen-diff extraction engine.
- `backend/internal/api/handler.go`
  - mount WebSocket terminal route and activity endpoints.
- `backend/internal/api/sse.go`
  - include semantic activity events and extractor warnings.
- `backend/internal/storage/storage.go`
  - add session-scoped append-only stores (`raw.ptylog`, `activity.jsonl`).
- `frontend/src/components/TerminalView.tsx`
  - switch from SSE chunk write model to WebSocket bidirectional model.
- `frontend/src/routes/sessions/$sessionId.tsx`
  - consume semantic activity entries; remove raw-byte transcript coupling.

---

This design intentionally separates three concerns that are currently conflated: terminal bytes, rendered terminal state, and user-facing activity semantics. That separation is the key enabler for reliable browser interaction, replayable extraction, and clean persistent activity history.

# Session and Terminal Dashboard Design

## Context
The current dashboard and session views surface sessions and PTY streams, but the
experience is inconsistent across pages and does not fully align session state,
stream health, and terminal history. Terminals are not represented as first-class
entities, which prevents clear ownership, lifecycle tracking, and last-snapshot
replay when a session ends.

This document defines a detailed UX and API design to unify session and terminal
management, ensure consistent status reporting, and enable terminal interaction
from the dashboard.

## Existing Design References
- `DESIGN_SYSTEM.md` outlines global UI tokens and component styling.
- `docs/mcp-agent-design.md` covers MCP components and the dock agent box.
- There is no dedicated design spec for sessions/terminals or their dashboard UX.

## Goals
- Provide a consistent session list and detail experience across dashboard and
  sessions routes.
- Separate terminal lifecycle from session lifecycle while preserving linkage.
- Preserve and display the last terminal snapshot after a terminal closes.
- Add interactive terminal controls (open/view/input/resize/kill).
- Make stream connectivity explicit and separate from session state (no more
  "running but cannot connect" ambiguity).

## Non-Goals
- Full redesign of the entire dashboard layout or design system.
- Replacing existing provider/session backends in one pass.
- Building a full multi-terminal multiplexing UI in the first iteration.

## Current State Review (Observed)
- Session lists exist in dashboard and sessions routes but are not synchronized.
- Session list data is cached in localStorage and can become stale.
- The session viewer uses SSE for activity but does not update session state in
  list views.
- Terminal view is read-only and depends on a websocket; no input support exists
  in UI.
- When the terminal stream closes, the last snapshot is not shown.
- Session state badges conflate provider state with stream connectivity.

## Design Principles
- **State separation:** session lifecycle, stream connectivity, and terminal
  lifecycle are distinct and must be displayed independently.
- **Single source of truth:** all list views use the same session feed, with
  explicit freshness/latency indicators.
- **Terminal as first-class entity:** terminals can exist independently and can
  be linked to sessions.
- **PTY continuity:** PTY providers run continuous extractors and must be tracked
  separately from ad-hoc terminals.
- **Predictable recovery:** snapshot fallback when streams drop or close.

## Data Model

### Session
```
Session
  id: string
  provider_type: string
  session_kind?: string
  state: created | starting | running | paused | stopping | stopped | error
  working_dir: string
  current_task?: string
  error_message?: string
  metrics: { tokens_in, tokens_out, request_count, last_activity_at? }
  terminals: TerminalRef[]
  updated_at: timestamp
```

### Terminal
```
Terminal
  id: string
  session_id?: string
  terminal_kind: pty | ad_hoc
  state: opening | live | closed | error
  last_snapshot?: TerminalSnapshot
  last_seq?: number
  last_updated_at: timestamp
  capabilities: {
    can_view: boolean
    can_write: boolean
    can_resize: boolean
    can_kill: boolean
    can_send_raw: boolean
  }
  transport: {
    status: connecting | live | reconnecting | closed | error
    last_heartbeat_at?: timestamp
  }
```

### TerminalRef
```
TerminalRef
  id: string
  terminal_kind: pty | ad_hoc
  state: string
  last_updated_at: timestamp
```

### PTY Terminal Behavior
- PTY terminals are created and managed by PTY providers.
- They must keep extractors running continuously, even when no UI is attached.
- PTY terminals should not be created/destroyed based on viewer open/close.

## API Design

### Sessions
- `GET /api/sessions` -> include `terminals` array and `updated_at` for all
  sessions.
- `GET /api/sessions/{id}` -> include `terminals` and optional last snapshot
  metadata.
- `GET /api/sessions/{id}/events` -> unchanged; add session-state updates to UI
  caches.

### Terminals
- `GET /api/v1/terminals` -> list all terminals (including detached or closed).
- `GET /api/v1/terminals/{id}` -> terminal detail with snapshot metadata.
- `POST /api/v1/terminals` -> create a terminal (optionally linked to a session).
- `DELETE /api/v1/terminals/{id}` -> kill terminal.

### Terminal Streaming + Input
- Keep `GET /api/sessions/{id}/terminal/ws` for backwards compatibility.
- Add `GET /api/v1/terminals/{id}/ws` for terminal-first access.
- Websocket write mode uses existing `write=true`.
- New websocket input messages should follow existing `input.*` schema.

### Terminal Snapshot
- `GET /api/v1/terminals/{id}/snapshot` -> returns last stored snapshot, even
  after closed.
- `GET /api/v1/sessions/{id}/terminal/snapshot` remains but becomes an alias
  to the primary terminal endpoint.

## Storage and Backend Updates
- Persist terminal snapshots on:
  - First connect (initial snapshot)
  - Each snapshot update
  - Terminal close event
- Store terminal metadata alongside sessions in storage (or a new table/collection).
- Track terminal -> session relationship, but allow terminals without sessions.

## UI/UX Design

### Global Session List (Dashboard and Sessions Index)
- Unified session list component and data source.
- Show **Session state** and **Stream status** separately:
  - State badge: running/paused/stopped/error
  - Stream pill: live/reconnecting/disconnected
- Add a "Last update" timestamp or "stale" badge if data older than threshold.
- Provide filters for State, Provider, and Stream status.

### Session Viewer
- Header badges:
  - Session state
  - Activity stream status
  - Terminal stream status (if terminal exists)
- Terminal panel includes:
  - Primary view: live terminal
  - Fallback: last snapshot with timestamp
  - Controls: connect, disconnect, send input line, send key (Enter, Ctrl+C),
    resize, kill terminal
- Show explicit messaging when session is running but stream is disconnected.

### Terminal List (New)
- Dedicated route `/terminals` with:
  - Terminal ID
  - Linked session (if any)
  - Terminal state + transport status
  - Last snapshot timestamp
  - Actions: view, kill

### Terminal Detail (New)
- Standalone terminal viewer, identical to embedded session terminal panel.
- Show session link if present.

## Interaction Flows

### Connect Terminal
1. User clicks "Connect" -> websocket opens.
2. Backend sends snapshot immediately.
3. UI updates to live and enables input.

### Terminal Close
1. Websocket closes or provider stops.
2. UI marks terminal as closed and displays last snapshot.
3. List view shows last snapshot timestamp and closed state.

### Send Input
1. User types text and sends.
2. UI sends `input.text` over websocket.
3. Terminal updates via diff or snapshot.

## Error States and Messaging
- Distinguish "session running" from "terminal disconnected".
- For disconnected streams, display the last snapshot and a reconnect action.
- For missing terminal support, show a clear "terminal not supported" notice.

## Implementation Plan (Phased)

### Phase 1: Data + UX Consistency
- Unify session list data source and refresh strategy.
- Add stream status indicators and stale markers.
- Ensure session viewer uses unified session metadata.

### Phase 2: Terminal Entity + Snapshot Persistence
- Add terminal model storage and list endpoints.
- Persist snapshots and surface fallback in UI.

### Phase 3: Terminal Interaction
- Add write-mode websocket in UI (text, key, control, resize).
- Add terminal controls.

### Phase 4: Terminal List + Detail
- New routes and UI for terminal list/detail.

## Open Questions
- Should terminals be created automatically for all PTY providers or only when
  a user opens a terminal view (non-PTY only)?
- Do we allow multiple terminals per session (e.g., multiplexer) in v1?
- How should we handle "detached" terminals without sessions (CLI-run)?

## Success Criteria
- Session list is consistent and updates predictably across routes.
- Session running vs stream disconnected is clearly visible.
- Terminal snapshots are visible after terminal close.
- Users and agents can open, view, input, resize, and kill terminals.

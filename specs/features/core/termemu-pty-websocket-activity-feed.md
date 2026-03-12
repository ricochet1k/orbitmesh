# TermEmu PTY Websocket Activity Feed

## 1. Summary
The TermEmu PTY Websocket Activity Feed is a real-time, bidirectional communication channel that streams the grid state of server-side Pseudo-Terminals (PTYs) to the frontend, while simultaneously receiving client input (stdin, mouse, keyboard). It integrates natively with the platform's global pubsub infrastructure and relies on the backend `termemu` library to compute and emit full snapshots and region-based grid diffs.

## 2. Motivation
In complex agentic environments, providing a rich, high-fidelity view of command executions is critical. Transmitting raw terminal output (like ANSI escape codes) requires the frontend to run a heavy, full terminal emulator (e.g., xterm.js). By managing the PTY state on the backend using `termemu` and only sending parsed grid state changes (snapshots and diffs) to the frontend, we enable the use of a lightweight terminal renderer. Integrating this with the existing global pubsub reduces architectural complexity, allowing a unified websocket connection to handle both generic session events and high-fidelity terminal interactions.

## 3. Scope
* **In Scope**:
    * Transmission of full terminal snapshots (2D grid state) upon initial connection.
    * Real-time streaming of region-based updates (grid diffs) as the PTY state changes.
    * Bidirectional IO routing: Client input (keyboard, text strings, mouse events, resize events, control signals) sent over the websocket back to the correct backend PTY instance.
    * Integration with the existing global pubsub architecture.
* **Out of Scope**:
    * Support for `last_event_id` or sequential event replay mechanisms (since terminal rendering relies on a full initial snapshot rather than a historical event log).
    * Specific rate-limiting or frame batching optimizations (e.g., 60fps locking) for high-throughput terminal output at this time.
    * Custom authentication or access control for terminal views beyond standard session-level security.
    * Use of xterm.js or other heavyweight frontend emulators.

## 4. Requirements & User Experience (UX)
* **Frontend Rendering**: The user will interact with a lightweight terminal renderer component (`TerminalView.tsx`). The view will display a highly accurate, structured grid representing the backend PTY, including text spans with classes/styles for coloring and formatting.
* **Connection Flow**:
  * When a user views a terminal, the client establishes a connection to the global pubsub feed.
  * Instead of providing a `last_event_id` to resume state, the client immediately receives a `terminal.snapshot` event containing the full grid size (`cols`, `rows`) and the current state of all lines.
  * Subsequent updates arrive as `terminal.diff` events, specifying a `region` (`x, y, x2, y2`) and the new lines for that region.
* **Input**: The user can type, click, and resize the terminal view. These actions are packaged into strongly-typed input events (`input.key`, `input.text`, `input.mouse`, `input.resize`, `input.control`) and sent back over the websocket.

## 5. System Design & Architecture
* **Integration**: The feed operates as part of the generic real-time websocket feed infrastructure but uses specialized payloads.
* **Backend Grid State (`termemu`)**: The backend allocates a `termemu.Terminal` for the PTY. It monitors changes and constructs snapshot and diff structures (`backend/internal/terminal/types.go`).
* **Payload Structure**:
    * **Downstream (Backend to Frontend)**: Envelopes include a `type` (e.g., `terminal.snapshot`, `terminal.diff`, `terminal.cursor`, `terminal.error`), `session_id`, `seq`, and a `data` object containing grid geometry and line arrays.
    * **Upstream (Frontend to Backend)**: Envelopes specify the input type (`input.key`, `input.text`, etc.) and the structured payload (e.g., `{ text: "ls\n" }` or `{ code: "enter", mod: ["shift"] }`).
* **Snapshot Initialization**: Since terminals are stateful grids rather than append-only logs, attempting to replay historical text events is inefficient and prone to desync. The architecture sidesteps this by taking an immediate lock on the `termemu` instance upon connection, serializing its entire grid (`SnapshotFromTerminal`), and emitting it as the first message.

## 6. Security & Privacy
* The terminal feed falls under the same authorization bounds as standard session data. No special authentication layer is needed for the websocket connection at this time.
* PTY input handling must safely parse incoming JSON payloads and gracefully reject malformed inputs (e.g., invalid key codes or missing data fields) without crashing the PTY backend.
* **Security Note**: PTYs execute commands in the environment's context. Standard isolation boundaries apply to the processes running inside the PTY, independent of this visualization feed.

## 7. Testing Plan
* **Unit Tests**:
  * Verify `SnapshotFromTerminal` and `BuildDiffFrom` correctly translate `termemu` state to the expected JSON-serializable structs.
  * Ensure the input parsing functions (`parseKeyInput`, `parseMouseInput`, etc.) correctly handle valid payloads and gracefully reject invalid data.
* **Integration Tests**:
  * Verify the websocket handler correctly drops `last_event_id` logic for terminal topics and successfully dispatches an initial snapshot.
* **E2E Tests**:
  * Utilize Playwright to test the `TerminalView` component by rendering a backend PTY stream, ensuring text rendering, resizing, and input transmission (e.g., typing a command and seeing the output) work as expected.

## 8. Rollout & Deployment
* **Migrations**: None required.
* **Deployment**: Ships as part of the standard backend/frontend application binaries. No feature flags are necessary for this core infrastructure component.
* **Monitoring**: Monitor websocket connection stability and track potential backend CPU spikes or OOMs caused by very high-throughput terminal processes generating massive diff streams.

## 9. Alternatives Considered
* **Raw Text Streaming + xterm.js**: Instead of using `termemu` on the backend, we could have streamed raw ANSI escape codes over the websocket to an xterm.js instance on the frontend. *Rejected* because xterm.js is exceptionally heavy, and keeping the grid state authoritative on the backend simplifies the frontend architecture and allows multiple clients to easily synchronize by requesting a snapshot.
* **Standalone WebSocket Endpoint**: We could have built a completely separate websocket endpoint solely for PTY traffic. *Rejected* because integrating with the global pubsub feed reduces connection overhead and allows developers to consume terminal events alongside other generic session events.

## 10. Implementation Plan
* [x] Define `terminal.Snapshot`, `terminal.Diff`, and input types in `backend/internal/terminal/types.go`.
* [x] Implement backend functions to lock `termemu` and extract snapshots/diffs (`backend/internal/terminal/helpers.go`).
* [x] Implement JSON parsing logic for incoming websocket input events (`backend/internal/api/terminal_ws.go`).
* [x] Ensure the global pubsub feed routes terminal inputs to the correct PTY manager.
* [x] Develop a lightweight `TerminalView.tsx` component on the frontend to render snapshots and apply diffs.
* [ ] Verify the backend emits the initial snapshot correctly without relying on `last_event_id`.

## 11. Open Questions
* In the future, if terminal output becomes extremely dense (e.g., `cat /dev/urandom`), will the lack of explicit rate-limiting or frame batching overwhelm the websocket connection or the frontend renderer?

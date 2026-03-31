# Realtime Websocket Feed

## 1. Summary
The Realtime Websocket Feed is a global WebSocket endpoint designed to stream system and session events from the OrbitMesh backend to the frontend UI. It utilizes the existing `EventBroadcaster` to provide a unified, multiplexed stream of `domain.Event` objects in JSON format, allowing the frontend to react to real-time agent execution states, terminal output, and system updates without polling.

## 2. Motivation
OrbitMesh manages dynamic, long-running agent executions that generate a high volume of events (e.g., log outputs, state changes, terminal interactions). Relying on HTTP polling for these updates is inefficient, introduces latency, and scales poorly. A persistent WebSocket connection is necessary to deliver these events instantly, ensuring the UI remains perfectly in sync with the backend state. Furthermore, by supporting explicit subscription and replay capabilities, the feed guarantees that transient network disconnections do not result in dropped or duplicated events.

## 3. Scope
* **In Scope**:
    * A single, global WebSocket endpoint (e.g., `/api/ws/events`) for the frontend UI.
    * Client-initiated explicit `subscribe` messages to filter events (e.g., by `sessionID`).
    * Reconnection handling and event replay using `lastEventID` included in the `subscribe` message.
    * Authentication and authorization via existing HTTP session cookies and CSRF tokens during the initial upgrade request.
    * Client-side pinging to maintain connection liveness.
    * Transmission of `domain.Event` objects serialized as JSON.
* **Out of Scope**:
    * Bidirectional control messages (e.g., sending terminal input back over this specific feed; that is handled by other mechanisms).
    * Server-side pinging/keepalive mechanisms.
    * Support for external API clients or third-party consumers (this feed is strictly for the internal OrbitMesh frontend).
    * Persistent, durable storage of all historical events beyond the immediate in-memory replay buffer provided by `EventBroadcaster`.

## 4. Requirements & User Experience (UX)
* **Connection**: The frontend establishes a single WebSocket connection to the global feed upon loading.
* **Subscription**: Once connected, the client sends a `subscribe` message. This message may include a `sessionID` (to filter events for a specific session viewer) and a `lastEventID` (to request a replay of missed events since the last known state).
* **Event Handling**: As events occur in the backend (broadcasted via `EventBroadcaster`), they are routed to the appropriate WebSocket connections based on active subscriptions and serialized to JSON. The frontend parses these JSON objects back into its internal event representations and updates the UI accordingly (e.g., appending terminal output, updating task status).
* **Resilience**: If the connection drops, the frontend automatically attempts to reconnect. Upon successful reconnection, it sends a new `subscribe` message containing the highest `lastEventID` it processed, ensuring it receives any events that occurred during the outage without duplicates.

## 5. System Design & Architecture
* **Endpoint**: A new HTTP handler will be added (e.g., `GET /api/ws/events`) that upgrades the connection to a WebSocket.
* **Integration with `EventBroadcaster`**: The WebSocket handler will act as a bridge to the existing `service.EventBroadcaster`.
    * It will maintain a read loop to parse incoming client messages (specifically `subscribe` and client-side `ping` messages).
    * It will maintain a write loop to push events to the client.
    * When a `subscribe` message is received, it will call `EventBroadcaster.SubscribeWithReplay(subscriberID, sessionID, lastEventID)`. The returned replay events will be immediately flushed to the client, followed by new real-time events.
* **Protocol Definition**:
    * **Client -> Server (Subscribe Request)**:
      ```json
      {
        "type": "subscribe",
        "sessionID": "optional-session-id",
        "lastEventID": 12345
      }
      ```
    * **Server -> Client (Event Message)**: JSON serialization of `domain.Event`.
* **Scalability**: The architecture relies on the current in-memory `EventBroadcaster`. Since this is intended for single-node deployment currently, memory buffering is sufficient. The buffer size should be configurable to manage memory usage.

## 6. Security & Privacy
* **Authentication**: The initial HTTP upgrade request will pass through the standard API middleware stack. It will require a valid session cookie and validate the CSRF token, ensuring only authenticated dashboard users can connect.
* **Authorization**: The feed currently broadcasts domain events. If multi-tenancy or strict per-session access controls are introduced in the future, the `subscribe` handler must verify that the authenticated user is authorized to view events for the requested `sessionID`.

## 7. Testing Plan
* **Unit Tests**:
    * Test the WebSocket connection upgrade and authentication middleware.
    * Test the parsing of the `subscribe` message payload.
    * Test the translation of `domain.Event` into JSON and successful transmission to the write channel.
* **Integration Tests**:
    * Create a mock `EventBroadcaster`. Connect a test client, send a `subscribe` message with a `lastEventID`, and assert that the correct replay events are received followed by new broadcasted events.
    * Test client disconnection and cleanup (ensuring `Unsubscribe` is called on the broadcaster to prevent goroutine leaks).
* **E2E Tests**:
    * Using Playwright, simulate a network disconnect in the frontend, generate a backend event, reconnect, and verify the UI eventually displays the missed event without duplication.

## 8. Rollout & Deployment
* No database migrations are required as this leverages in-memory state.
* Rollout can be immediate. The frontend will be updated to use the new WebSocket endpoint instead of (or alongside, if phasing out) any existing polling mechanisms.
* **Monitoring**: Monitor the number of active WebSocket connections, the rate of dropped events (via `EventBroadcaster.DroppedEventCount()`), and the frequency of reconnects to ensure the feed is stable.

## 9. Alternatives Considered
* **Server-Sent Events (SSE)**: Considered as an alternative for one-way streaming. While SSE is simpler for pure unidirectional data, WebSockets were chosen because they allow the client to send explicit `subscribe` messages *after* the connection is established, which is cleaner for handling dynamic filtering and passing the `lastEventID` for replay without needing to encode complex state into the initial URL query parameters.
* **Per-Session WebSockets**: Considered opening a separate WebSocket for every session viewed (e.g., `/api/sessions/{id}/ws/events`). Rejected in favor of a single global multiplexed connection to reduce resource overhead (fewer connections) and simplify frontend connection management.

## 10. Implementation Plan
* [ ] Implement the WebSocket HTTP upgrade handler (`/api/ws/events`).
* [ ] Implement the read loop for parsing client `subscribe` messages.
* [ ] Connect the handler to `service.EventBroadcaster` using `SubscribeWithReplay`.
* [ ] Implement the write loop to serialize `domain.Event` to JSON and send to the client.
* [ ] Add unit and integration tests for the WebSocket handler.
* [ ] Update the frontend API client to establish the connection, handle reconnections, and send explicit `subscribe` messages with `lastEventID`.
* [ ] Wire the frontend WebSocket listener to the UI state management to reflect events in real-time.

## 11. Open Questions
* None at this time.

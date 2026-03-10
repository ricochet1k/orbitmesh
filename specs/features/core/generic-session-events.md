# Generic Session Events

## 1. Summary
This specification documents the "Generic Session Events" system in OrbitMesh, primarily encapsulated by the `domain.Event` structure. This system serves as the common event schema that all LLM providers (e.g., Claude, Codex) must produce and that the execution engine and frontend consume. It provides a unified, provider-agnostic representation of agent outputs, status changes, and execution metadata.

## 2. Motivation
Different LLM providers emit execution data, streams, and tool calls in disparate formats. To provide a cohesive, reliable, and observable environment for executing autonomous agents, the OrbitMesh backend must normalize these disparate provider payloads into a consistent, domain-driven structure. Without this normalization, the frontend, logging systems, and downstream services would need to implement provider-specific logic, leading to a brittle and unmaintainable architecture. The `domain.Event` structure guarantees uniformity across the system.

## 3. Scope
* **In Scope**:
  * The structure, payload format, and common metadata of `domain.Event`.
  * The translation boundary where provider-specific outputs become generic events.
  * The routing of these events through the system (via the global `EventBus`).
  * The general persistence mechanism passing through the Entity storage system.
* **Out of Scope**:
  * The exhaustive schema of individual event payloads (this is covered in [Session Event Types](session-event-types.md)).
  * The low-level implementation details of the Entity storage system, specifically the resolution of the delta compaction problem (this will be covered in the [Entity Storage Architecture](entity-storage-architecture.md) spec).
  * The UI rendering components themselves.

## 4. Requirements & User Experience (UX)
* **Frontend Consumption**:
  * **Live Stream**: The frontend requires a real-time, low-latency stream of events as they occur (e.g., streaming text generation, tool call execution) to drive live agent session viewers and terminal dashboards.
  * **History/Log Viewer**: The frontend requires the ability to fetch historical events for completed or paused sessions. In this view, raw stream deltas should ideally be compacted or omitted in favor of the final concatenated message blocks to improve performance and readability.
* **Provider Conformance**: All integrated LLM providers must accurately translate their native responses into the appropriate `domain.Event` types (e.g., converting an OpenAI delta chunk or a Claude block update into an `EventTypeOutput` or `EventTypeThought` with the `IsDelta` flag).

## 5. System Design & Architecture
* **The `domain.Event` Structure**:
  Every event contains common metadata:
  * `ID`: A unique identifier (often sequential) for ordering.
  * `Type`: An enumeration (`EventType`) categorizing the event (e.g., `status_change`, `output`, `thought`, `tool_call`).
  * `Timestamp`: When the event occurred.
  * `SessionID`: The execution session this event belongs to.
  * `Data`: A generic payload struct specific to the `Type` (e.g., `OutputData`, `ToolCallData`).
  * `Raw`: The original, unaltered JSON payload from the provider, retained for debugging and auditability.
* **Data Flow**:
  1. **Generation**: An LLM provider execution stream (e.g., Claude API) emits a raw chunk.
  2. **Translation**: The provider implementation in OrbitMesh parses the chunk and constructs a `domain.Event` (e.g., using `NewDeltaOutputEvent`).
  3. **Broadcasting & Storage**: The event is handed to the execution engine. The engine simultaneously:
     * Publishes the event to the global `EventBus` (specifically `EventBroadcaster`), which routes it to connected WebSocket clients for live UI updates.
     * Passes the event to the `LogEntity` system (built on the core Entity `Store`+`Handle` pattern) for durable storage.
* **Storage and Compaction**:
  * While broadcasting deltas is critical for the live UI, storing thousands of individual character deltas is inefficient. The system design acknowledges the need for delta compaction in storage—merging `IsDelta=true` events into their parent messages before or during persistence. The exact mechanism for this compaction remains a known architectural challenge to be detailed in the Entity spec.

## 6. Security & Privacy
* Events intrinsically carry the input prompts, reasoning (`thought` events), and outputs of the LLM, which may contain highly sensitive proprietary code, API keys, or PII.
* The system must ensure that event streams (via WebSocket) and historical event logs (via API) are protected by strict authorization controls. Only users with explicit read access to a given session (or its parent project) should be able to subscribe to or query its events.
* Retaining `Raw` provider payloads must be done carefully to ensure it does not bypass data scrubbing mechanisms that might eventually be applied to the structured `Data` fields.

## 7. Testing Plan
* **Unit Testing**:
  * Verify the correct instantiation of all event types (e.g., `NewOutputEvent`, `NewToolCallEvent`) in `backend/internal/domain/event_test.go`.
  * Ensure provider adapters correctly map edge cases (e.g., unexpected chunk formats) into `EventTypeUnknown` or proper error events.
* **Integration Testing**:
  * Verify that an event emitted by a mock provider is successfully received by a subscriber on the `EventBroadcaster`.
  * Verify that an event is successfully persisted and retrievable via the `LogEntity` storage mechanism.

## 8. Rollout & Deployment
* This specification documents the *current* state of the generic eventing system. No immediate rollout is required.
* Future changes to the event schema must be backwards-compatible or involve data migrations for historical sessions stored in the database.
* The introduction of robust delta compaction will require careful rollout and potential backfilling of existing uncompacted event logs.

## 9. Alternatives Considered
* **Provider-Specific Streams**: We could pipe raw provider JSON directly to the frontend and handle normalization in the UI. **Rejected**: This tightly couples the frontend to specific LLM providers, dramatically increases frontend complexity, and makes backend analysis or storage of execution state nearly impossible.
* **Separating Status from Output**: We could have distinct streams for system status vs. LLM output. **Rejected**: Multiplexing all execution activity into a single ordered timeline (`domain.Event` stream) simplifies chronological replay and storage.

## 10. Implementation Plan
* [x] Define `domain.Event` and associated payload structs in `backend/internal/domain/event.go`.
* [x] Implement the `EventBroadcaster` for pub/sub routing in `backend/internal/service/events.go`.
* [ ] Finalize delta compaction logic within the Entity storage layer (Tracked as a separate feature/issue).
* [x] Document the system (this spec).

## 11. Open Questions
* **Delta Compaction**: What is the most performant way to implement delta compaction in the `LogEntity` system without blocking the fast-path of the `EventBus`?
* **Typed Unknowns**: How quickly can we deprecate `EventTypeUnknown` as new provider features (like specific prompt caching metrics) are introduced?

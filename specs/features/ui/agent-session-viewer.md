# Feature Spec: Agent Session Viewer

This document specifies the requirements, design, and scope of the Agent Session Viewer feature for the OrbitMesh platform.

## 1. Summary
The Agent Session Viewer is a primary user interface within OrbitMesh designed for observing and interacting with autonomous agent sessions. It serves a dual purpose: providing real-time, streaming updates of active sessions and allowing users to view and audit historical, completed sessions. The viewer presents a cohesive transcript of LLM interactions, tool executions, and system events, supplemented by a details panel for metrics like token usage and provider limits.

## 2. Motivation
OrbitMesh manages complex developer workflows executed by autonomous agents. To maintain visibility and trust, users need a robust interface to monitor what the agents are doing in real-time and to debug or audit their behavior post-execution. Without a dedicated session viewer, tracking long-running tasks, understanding an agent's reasoning, or diagnosing failures becomes incredibly difficult. This feature ensures observability and enables human-in-the-loop interventions when supported by the underlying provider.

## 3. Scope
* **In Scope**:
  * Real-time viewing of session transcripts with streaming delta updates.
  * Loading and viewing historical session data via a scrollable interface with infinite loading (load more when reaching scroll top).
  * Collapsible details for reasoning and tool executions to prevent information overload.
  * Interactive capabilities (pausing, intervening, stepping, sending messages) if supported by the provider.
  * A separate details tab/panel for session metrics (token usage, request counts, limits, provider info).
  * Basic display settings (e.g., auto-scroll toggle).
  * Ability to export the transcript (JSON, Markdown).

* **Out of Scope**:
  * Advanced event filtering within the viewer itself (this is delegated to a separate global settings page).
  * Integration with the CodeFlow node graph visualization (this remains a separate interface).
  * Multi-user authorization controls (OrbitMesh assumes single-user access at this stage).

## 4. Requirements & User Experience (UX)
* **Layout**: The main interface consists of the session transcript occupying the majority of the page. A dedicated details panel (or "Session Intel" tab on mobile) displays metadata, token usage (In/Out), request counts, and session state.
* **Transcript View**:
  * Presents a chronological list of events (messages, tool calls, errors).
  * Lengthy or complex data, such as an agent's internal reasoning or raw tool input/output, must be collapsed by default behind summary lines. Users can expand these lines for deeper inspection.
  * Supports auto-scrolling to the latest event for active sessions.
  * For historical viewing, the interface handles large amounts of data by initially loading a subset and dynamically loading older events as the user scrolls to the top of the view.
* **Interactivity**:
  * If the active session provider supports it, the user can interrupt/cancel the current action.
  * Users can input text to respond to action requests or provide manual guidance.
* **Header/Actions**: The header displays the session title, state badge, provider type, and an overflow menu for session actions (e.g., changing providers/models for the session, exporting data, canceling the session).

## 5. System Design & Architecture
* **Frontend**: Built with SolidJS. The main component resides in `frontend/src/routes/sessions/$sessionId.tsx`.
* **State Management**: The viewer subscribes to real-time WebSocket feeds for active session updates and uses REST API polling/fetching for historical data retrieval.
* **Data Flow**:
  * **Active**: Subscribes to the global session state stream (or specific session events) via WebSocket to receive streaming delta updates.
  * **Historical**: Retrieves paginated transcript events from the backend API, utilizing cursor-based pagination to load earlier events when the user scrolls to the top.
* **Performance**: Utilizing SolidJS's reactive primitives to efficiently render large lists of transcript events without significant DOM overhead. Collapsing detailed views minimizes initial rendering load.

## 6. Security & Privacy
* **Access Control**: Currently designed for a single-user environment; no complex RBAC or authorization checks are implemented beyond verifying application access.
* **Data Handling**: Transcripts may contain sensitive source code or credentials depending on the agent's tasks. Standard local or secure remote storage mechanisms provided by the backend should be relied upon. The UI avoids unnecessary local caching of sensitive transcript data beyond the active session view.

## 7. Testing Plan
* **UI/Component Tests**: Vitest tests for the `SessionViewer`, `SessionTranscript`, and `SessionComposer` components to verify rendering, state transitions (e.g., auto-scroll behavior), and collapsing/expanding logic.
* **E2E Tests**: Playwright tests to verify the full flow:
  1. Opening a historical session and verifying scroll-to-load functionality.
  2. Opening an active session, verifying WebSocket connections, and confirming that new events stream into the UI.
  3. Testing interactive features like sending an interruption or a message.
* **Mocking**: Utilize backend mock providers (e.g., `acp-echo`) to simulate active sessions with deliberate pauses, allowing for human-in-the-loop intervention testing without actual LLM provider costs.

## 8. Rollout & Deployment
* **Current Status**: The core viewer is mostly implemented and integrated into the main frontend application.
* **Future Enhancements**: Planned additions include deeper integration with provider-specific interactive capabilities (like explicit stepping) and refined global display settings for event filtering.

## 9. Alternatives Considered
* **Split Pane Design**: Considered a hard split between the transcript and terminal/code views. Ultimately decided to prioritize the transcript as the primary full-page view, relegating metrics to a side panel/tab, and keeping complex visualizations (CodeFlow) separate to avoid clutter.
* **Full Transcript Loading**: Considered loading the entire history at once for completed sessions. Rejected due to performance concerns with extremely long sessions; opted for a "load more" scrolling mechanism.

## 10. Implementation Plan
* [x] Core Transcript UI (SolidJS components).
* [x] WebSocket integration for streaming delta updates.
* [x] Collapsible reasoning and tool execution blocks.
* [x] Session Intel/Metrics side panel.
* [x] Export functionality (JSON/Markdown).
* [ ] (Future) Integrate advanced event filtering controls via the Settings page.
* [ ] (Future) Enhance interactive controls (stepping, pausing) based on specific provider capabilities.

## 11. Open Questions
* How granular should the "stepping" capabilities be if a provider supports it (e.g., stepping per tool call vs. per LLM generation)?

# Feature Spec: Resumable Session Recovery

## 1. Summary
OrbitMesh executes agentic sessions that perform complex, multi-step tasks. In a distributed environment, these sessions can span hours, days, or even weeks. During this time, the backend process might crash, network partitions can occur, or a user might explicitly pause the session. **Resumable Session Recovery** provides the capability to transparently suspend and later resume an agent's session without losing the execution state or conversation context. This feature ensures fault tolerance, enabling resilient, long-running agent workflows.

## 2. Motivation
Autonomous agents performing development or operational tasks cannot assume an uninterrupted runtime. If an OrbitMesh backend server restarts or a long-running process outlives a single compute instance, losing the entire session state is unacceptable. Without session recovery, users would be forced to restart complex agents from scratch, wasting time and compute resources (LLM API costs). Furthermore, enabling users to pause and resume sessions provides essential control over resource usage and manual intervention points.

## 3. Scope
* **In Scope**:
    * Automatic recovery of session state upon backend restart (crash recovery).
    * Explicit user-initiated pause and resume of sessions.
    * Transparent restoration of the LLM context window/transcript.
    * Restoration of attached resources (e.g., Terminals, PTYs, Websockets) where possible.
    * Injection of state-loss messages (e.g., "Terminal disconnected") into the agent's context when resource restoration fails.
    * Supporting provider-agnostic transcripts to enable resuming a session with a *different* LLM provider (falling back to context truncation if needed).
    * Leveraging built-in provider session persistence (e.g., internal session IDs) as the primary restoration strategy.
* **Out of Scope**:
    * Execution scheduling for long-running asynchronous tools (e.g., tools that sleep for a week). This will be handled in a separate feature spec: **Long-Running Async Tools**.

## 4. Requirements & User Experience (UX)

### User Stories
* **As an OrbitMesh user**, if the backend crashes while my agent is midway through refactoring a codebase, I want the agent to automatically resume its work from the exact point of interruption when the server restarts.
* **As an OrbitMesh user**, I want to click a "Pause" button on the UI to temporarily halt an agent, and later click "Resume" to continue its work, even if days have passed (no time limit on suspension).
* **As an OrbitMesh user**, if I resume a session but the original terminal it was using has been destroyed, I expect the agent to be gracefully informed of this loss so it can re-initialize a new terminal without crashing the session.
* **As an OrbitMesh user**, if the LLM provider I was using goes offline, I want to resume the session using a different provider (e.g., switching from Claude to Codex), and the system should attempt to translate the transcript to the new provider's format.

### Functional Requirements
1. **Durable State Storage**: Every turn, tool call, and response must be durably committed using the existing session storage mechanisms (e.g., the filesystem-based snapshot manager and message logs, *not* `goraphdb`, which is reserved exclusively for CodeFlow).
2. **Provider-Led Strategy Selection**: The system must persist the specific provider's internal session ID along with other metadata. Upon resume, the *provider implementation* itself chooses the restoration strategy based on this data, metadata, and provider-specific custom data in the stored session State.
3. **Transcript Replay Fallback**: If native provider persistence is unavailable or provider-switching occurs, the system must support replaying the provider-agnostic transcript to rebuild the context window.
4. **Clean Text Rendering Fallback**: If replay fails (e.g., context window limits are exceeded), the system must fallback to rendering the historical transcript as clean text to summarize past events, dropping older context as necessary.
5. **Transparent Recovery**: The act of resuming should be transparent to the agent unless specific state is permanently lost.
6. **Lost State Injection**: If an active resource (e.g., a PTY) cannot be re-attached during recovery, the system must inject a notification into the transcript. The preference order for this injection is:
    1. A simulated Tool Response (e.g., for a previously pending tool call interacting with that terminal).
    2. A System Message.
    3. A User Message.
7. **Infinite Suspension**: Suspended sessions have no maximum time limit. They remain recoverable indefinitely.

## 5. System Design & Architecture

### Architectural Components
* **Session Manager & Snapshots**: The component responsible for orchestrating the session lifecycle. Recovery relies on the existing `SnapshotManager` (`internal/session/snapshot_manager.go`) and filesystem store, which serialize session state to disk. *Note: `goraphdb` is not used for sessions.*
* **Event Stream (Transcript)**: The durable log of all agent interactions (e.g., `internal/storage/session_messages_store.go`). This is stored in a provider-agnostic format to support cross-provider resumes.
* **Provider Adapters**: Each provider adapter must implement a resume mechanism (e.g., as outlined in the ACP `SESSION_PERSISTENCE_PLAN.md`). The provider analyzes the restored snapshot's metadata and custom state to determine whether to use native resumption (like ACP's `LoadSessionRequest`), transcript replay, or text summarization.
* **Resource Reconciler**: During the `Resuming` phase, this component checks the expected state of external resources (Terminals/PTYs, open Websockets) against the actual system state, attempting reconnection and generating "Lost State" messages if reconnection fails.

### Data Flow for Recovery
1. **Initialization**: On backend startup, the Session Manager queries the filesystem store for all session snapshots that indicate an interrupted or paused state.
2. **Reconciliation**: For each recovering session, the Resource Reconciler attempts to bind to existing Terminals/PTYs. If a Terminal defined in the session state cannot be found, a `ResourceLostEvent` is generated.
3. **Transcript Assembly**: The system retrieves the durable provider-agnostic message history. If a `ResourceLostEvent` exists, it is appended to the transcript as a Tool/System/User message.
4. **Provider Handoff**: The Session Manager loads the snapshot and passes control to the specific Provider. The provider evaluates the snapshot's custom data and metadata to execute its chosen restoration strategy:
    * **Strategy A (Native)**: E.g., sending a `LoadSessionRequest` if the agent supports it and a valid provider session ID exists.
    * **Strategy B (Replay)**: Replaying the structured agnostic transcript.
    * **Strategy C (Summarized Text)**: Summarizing the transcript if context limits are an issue.
5. **Execution Resumes**: The agent is prompted to continue its execution loop.

## 6. Security & Privacy
* **Context Leakage**: When switching providers during a resume, ensure that provider-specific internal metadata (e.g., proprietary system prompts from Provider A) is stripped before replaying the transcript to Provider B.
* **Authentication**: Resuming a session explicitly via the UI requires the same authorization checks as creating or interacting with a session. Recovered automated sessions must execute under the original user's identity context.

## 7. Testing Plan
* **Unit Tests**:
    * Test the conversion of provider-agnostic transcripts to specific provider formats (Claude, Codex).
    * Test the fallback logic in `Resume` (Native -> Replay -> Text).
    * Test the injection of "Lost State" messages into the transcript based on priority (Tool > System > User).
* **Integration Tests**:
    * Simulate a backend crash (SIGKILL) mid-turn. Verify that restarting the backend successfully resumes the session and completes the turn using the `mockProvider` (acp-echo mode).
    * Test provider switching: Start a session with Provider A, pause it, switch the configuration to Provider B, and resume.
* **E2E Tests**:
    * Use Playwright to verify the UI flows for manually pausing and resuming a session, ensuring the terminal output correctly reflects the resumed state.

## 8. Rollout & Deployment
* **Storage Migrations**: We will likely need new state enum values for sessions (e.g., `StatusPaused`, `StatusResuming`) in the snapshot schema. Snapshot versioning (e.g., `Version: 1` -> `Version: 2`) will handle these updates.
* **Monitoring**: Add new metrics:
    * `session_recovery_attempts_total`
    * `session_recovery_success_total`
    * `session_recovery_fallback_replay_total`
    * `session_recovery_fallback_text_total`

## 9. Alternatives Considered
* **Enforcing In-Memory Only Sessions**: Rejected. This limits the platform to short-lived tasks and provides zero fault tolerance, severely undermining the value of autonomous agents.
* **Relying Solely on Provider Context**: Rejected. Providers do not guarantee indefinite session persistence, and we cannot lock users into a single provider if they wish to switch mid-task. Storing a provider-agnostic transcript is mandatory.

## 10. Implementation Plan
* [x] Implement the foundational `SnapshotManager` and `FilesystemStore` logic to support interval-based and manual snapshots.
* [x] Update the Provider Interface and existing implementations (e.g., `acp/session.go`, `LoadSession`) to execute resumption logic based on the restored snapshot's state.
* [ ] Expand the `SnapshotManager` to robustly handle explicit `Paused` and `Resuming` states, ensuring all metadata (like terminal identifiers) is serialized.
* [ ] Implement the `Resource Reconciler` to verify and attempt to re-attach Terminals/PTYs on backend startup or manual resume.
* [ ] Implement the "Lost State" message injection logic (prioritizing Tool Response -> System Message -> User Message) for failed reconciliations.
* [ ] Implement backend APIs for explicit Pause and Resume actions.
* [ ] Implement cross-provider transcript translation logic for when native provider IDs cannot be used.
* [ ] Add frontend UI controls for explicitly Pausing and Resuming sessions.
* [ ] Write unit and integration tests covering crash recovery, provider switching, and lost state injection.

## 11. Open Questions
* *Are there specific limits on the size of the summarized text fallback we should enforce before completely failing a resume?*
* *How should we handle pending, unresolved Tool Calls that were dispatched *before* a crash but completed *during* the downtime?* (Likely involves reconciling the durable event stream with the current state of the long-running async tool scheduler, to be defined in that spec).

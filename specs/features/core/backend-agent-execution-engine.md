# Feature Spec: Backend Agent Execution Engine

## 1. Summary
The Backend Agent Execution Engine is the core component of the OrbitMesh Go backend responsible for managing the lifecycle of autonomous agent sessions. It orchestrates starting, stopping, resuming, and tracking the state of multiple concurrent agents running on a single instance. It operates by delegating direct interactions with LLMs and runtime environments to specific provider abstractions.

## 2. Motivation
To build a reliable platform for autonomous agents, we need a robust, centralized engine that can handle the complex lifecycle of an agent session independently of the user interface. Without this engine, state management, error recovery, event broadcasting, and concurrency would be unmanageable, leading to inconsistent behavior, dropped events, and a fragile architecture that cannot scale to multiple concurrent agents or advanced task executions.

## 3. Scope
* **In Scope**:
  * Creation, tracking, state management, and lifecycle execution of agent sessions (`AgentExecutor`).
  * In-memory concurrent management of multiple sessions running simultaneously on a single instance.
  * Delegation of protocol-specific execution logic to Provider implementations (e.g., ACP, Claude, etc.) via the `session.Session` interface.
  * Capturing and broadcasting domain events (e.g., status changes, metrics, output).
  * Providing options for isolated execution environments (e.g., sandbox, containers, VMs).
  * Persistent storage integration for basic run attempts, session state, and usage metrics.
* **Out of Scope**:
  * Direct API calls to LLMs (this is the responsibility of specific Providers).
  * Complex Task Scheduling and dependencies (this will be handled by a separate Task Scheduler feature).
  * Direct rendering of the UI.

## 4. Requirements & User Experience (UX)
The primary user interface for the Execution Engine is the Session List (dashboard), where users can view and manage all active, suspended, and completed agents.
* **Session Dashboard**: Users must be able to see a real-time list of all agents running on the instance, their current status, and basic metrics (like duration or token usage).
* **Session Lifecycle Control**: Users (via the dashboard and API) must be able to create, start, stop, kill, and resume sessions reliably.
* **Concurrency**: The user should be able to run multiple agent sessions in parallel without them interfering with each other's state or execution.
* **Observability**: As an agent runs, the engine must accurately emit state changes and outputs so the UI can reflect the agent's progress in real-time.
* **Graceful Degradation**: If an agent crashes or times out, the engine must catch the failure, clean up resources, and emit a final error state, preventing zombie processes or memory leaks.

## 5. System Design & Architecture
* **AgentExecutor**: The central coordinating struct in `backend/internal/service/executor.go`. It maintains an in-memory map of active `sessionContext` instances and handles concurrency using mutexes and an `ActiveStore`.
* **Provider Abstraction**: The engine interacts with agents purely through the `session.Session` interface (`backend/internal/session/session.go`), sending input and receiving a channel of `domain.Event`s. This decoupling allows new LLM models or execution protocols to be added without changing the core engine.
* **Event Broadcasting**: The executor listens to the provider's event channel and forwards events to an `EventBroadcaster`, making them available to websocket clients and other internal subscribers.
* **Concurrency & Scaling**: The engine leverages Go's concurrency model (goroutines and channels) to run multiple sessions asynchronously. Currently designed for a single instance, it maintains state in memory, synchronized with a persistent `Storage` backend for recovery.
* **Isolation Options**: Future iterations will formalize the boundaries for how the engine launches agents in isolated environments (containers, sandboxes) to ensure security and prevent hostile interference with the host machine.

## 6. Security & Privacy
* **Execution Isolation**: The primary security concern is untrusted code execution by the agents. The engine must support running agents within secure boundaries (e.g., Docker containers or isolated VMs) to prevent agents from accessing unauthorized host files or networks.
* **Resource Limits**: The engine needs mechanisms to bound the resources (CPU, Memory, time) an agent can consume to prevent noisy neighbor issues in concurrent setups.
* **Data Privacy**: The engine handles sensitive inputs and environment variables (like API keys). These must be managed securely in memory and not leaked into unencrypted logs or unprivileged event streams.

## 7. Testing Plan
* **Unit Tests**: Verify the `AgentExecutor` correctly handles all session state transitions (Created -> Starting -> Running -> Stopped/Error). Verify correct behavior when a provider channel closes unexpectedly or errors.
* **Integration Tests**: End-to-end tests using mock providers to ensure the engine correctly delegates inputs, handles the resulting event stream, and updates persistent storage.
* **Concurrency Tests**: Spin up numerous mock sessions simultaneously to verify thread safety, absence of deadlocks, and correct resource cleanup.

## 8. Rollout & Deployment
* **Migrations**: No immediate database migrations are needed for the core engine logic, though future iterations of the event log or session state may require them.
* **Monitoring**: Essential to monitor goroutine counts, active session counts, and memory usage. High error rates or sudden drops in active sessions should trigger alerts.

## 9. Alternatives Considered
* **Client-side Execution (Frontend-only)**: Running the core logic and provider management directly in the browser. Rejected because it cannot run reliably in the background, cannot utilize secure backend secrets without exposing them to the client, and makes containerized execution impossible.
* **Serverless Functions per Agent Step**: Spinning up a stateless lambda for every turn in an agent's conversation. Rejected due to the stateful, long-running nature of PTY/Terminal access and the high latency of cold starts for complex interactive sessions.

## 10. Implementation Plan
* [x] Define `AgentExecutor` and `SessionContext` structures for concurrency management.
* [x] Implement core session lifecycle methods (`CreateSession`, `StopSession`, `KillSession`).
* [x] Create Provider `Session` interface abstraction for delegation.
* [x] Implement event streaming loop from Provider to Broadcaster.
* [x] Add graceful shutdown and active session draining logic.
* [x] Integrate basic SQLite/JSONL persistent storage for session recovery.
* [ ] Formalize Sandbox/Container execution options for Providers.

## 11. Open Questions
* How will the engine transition from single-instance in-memory concurrency to a distributed multi-node architecture if scaling demands it?
* What specific mechanisms will be used to enforce container/VM isolation for local runs versus cloud-hosted runs?

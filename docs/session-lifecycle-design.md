# Session Lifecycle Design

This document describes the intended design for Sessions in OrbitMesh — covering the state model, persistence, provider/model flexibility, and error handling.

## What Is a Session?

A **Session** is a conversation with an agent. It has a message history, a current state, and (when active) a connection to a provider that executes the agent.

Sessions are first-class, durable objects. They can outlive any individual provider connection, survive server restarts, and resume on a different provider or model from where they left off.

---

## State Model

A session is always in one of three logical states:

| State | Meaning |
|---|---|
| **idle** | No active provider connection. The session is fully persisted on disk and can receive a new message at any time, which will restart a provider. |
| **running** | Actively connected to a provider. The provider is streaming tokens or executing tool calls. |
| **suspended** | Waiting for an external response — a tool call result, a subscribed event, or a human-in-the-loop confirmation. The session is persisted on disk and will resume automatically when the awaited response arrives. |

These three states are the complete lifecycle. There is no `paused`, `stopping`, `stopped`, `starting`, `created`, or `error` state at the session level. Those are implementation details of how an individual provider run behaves, and they do not belong in the session model.

### State Transitions

```
         ┌──────────────────────────────────────────┐
         │                                          │
         ▼                                          │
      [ idle ] ──── message received ────► [ running ] ──── awaits external ────► [ suspended ]
         ▲                  │                   │                                       │
         │                  │                   │                                       │
         └── run ends ──────┘                   └── error/done ───────────────────────┘
                                                                          │
                                                                          ▼
                                                                       [ idle ]
```

Concretely:

- `idle → running`: a new message is sent to an idle session, which causes a provider run to begin.
- `running → suspended`: the running provider reaches a point where it must wait (e.g., a tool call that requires an external response).
- `suspended → running`: the awaited response arrives and the session resumes.
- `running → idle`: the provider run completes normally.
- `running → idle` (on error): a provider error is recorded to the message history and the session becomes idle again, ready to receive a new message.
- `suspended → idle`: a suspended session can be manually released (e.g., user cancels waiting for a tool result).

### What About "Pausing"?

In this model, "pausing" a session means preventing new messages from being delivered while the session is idle or suspended. This is a message-delivery policy, not a session state. Implementations may choose to queue or reject incoming messages, but the session itself does not change state.

### What About "Start" and "Stop"?

These concepts are removed. Sessions do not start or stop — they run when a message arrives and become idle when the run ends. There is no lifecycle call that creates a running session. There is no way to "stop" a session as a distinct operation; you simply stop sending it messages.

If a running provider needs to be interrupted (e.g., user cancels generation), that is a cancellation of the current run, which returns the session to idle state.

---

## Persistence

**Sessions in idle or suspended state must be fully serialized to disk.**

The serialized form includes:
- Session metadata (ID, title, working directory, project, timestamps)
- Full message history
- Current state (`idle` or `suspended`)
- If suspended: the pending suspension context (what is being awaited, any partial state the provider needs to resume)
- Provider preferences (which provider/model was last used, or which was requested)

Sessions in `running` state should also be checkpointed periodically so that a server crash does not lose significant history.

### Resumption

A session in `idle` or `suspended` state can be resumed:
- On the **same provider and model** as before (default behavior)
- On a **different provider or model**, specified by the client at the time the resuming message is sent

The session passes its message history to the new provider so it can reconstruct context. It is the responsibility of the provider adapter to translate the stored message history into whatever format the new provider expects.

This means provider selection is **per-run**, not per-session. The session stores a preferred provider but this can be overridden on any given message send.

---

## Error Handling

Provider errors do not block the session. Specifically:

1. **Errors during a run** are appended to the session's message history as a system message (or error event), and the session transitions to `idle`.
2. The session is fully functional after an error. The user (or orchestrator) can send a new message, possibly on a different provider.
3. Transient provider errors (e.g., network timeout, API rate limit) may be retried internally before being surfaced, but ultimately the session absorbs the error gracefully rather than entering a terminal state.
4. There is no session-level `error` state. Errors are data in the message history, not a lifecycle state.

---

## Message Delivery Semantics

- Messages sent to a **running** session: may be queued for delivery after the current run completes, or rejected with an appropriate error depending on the implementation. The session does not change state as a result.
- Messages sent to a **suspended** session: should be queued and delivered after the suspension resolves (or the suspension is cancelled), unless the implementation chooses to reject them.
- Messages sent to an **idle** session: immediately trigger a new run with a provider.

---

## What This Means for the Current Implementation

The current implementation has the following gaps relative to this design:

### State model gaps

- The seven-state enum (`created`, `starting`, `running`, `paused`, `stopping`, `stopped`, `error`) conflates session lifecycle with provider lifecycle. These should be separated.
- `stopped` and `error` are currently terminal — a session in these states cannot receive new messages or switch providers. Under this design, they do not exist at the session level.
- `paused` is currently a provider state (it pauses token delivery), not a session state. Remove it from the session model.

### Provider coupling gaps

- The provider is currently selected at session creation and is immutable. Under this design, the provider is selected per-run and can be overridden on each message.
- Provider-level errors currently transition the session to `error` state permanently. Under this design, errors are absorbed into the message history and the session returns to `idle`.

### Persistence gaps

- Only session metadata is persisted, not the full message history (which is reconstructed from SSE event logs). The message history must be a first-class persisted artifact.
- Provider-level snapshots (ACP only) are the closest thing to the suspension context described here. This mechanism should be generalized to all providers and tied to the `suspended` state.

### Start/Stop gaps

- `POST /api/sessions` creates and immediately starts a session. Under this design, sessions are created idle and a run begins when the first message is sent. The create and send operations may be combined in a single API call for convenience, but they are conceptually distinct.
- `DELETE /api/sessions/{id}` stops the session. Under this design, deletion is possible from any state (it removes the session and its history). There is no "stop" operation — you cancel a run or you delete the session.

---

## API Shape (Sketch)

```
POST   /api/sessions                     Create a new idle session
GET    /api/sessions                     List sessions
GET    /api/sessions/{id}                Get session (metadata + state)
DELETE /api/sessions/{id}                Delete session permanently

POST   /api/sessions/{id}/messages       Send a message (starts a run if idle)
                                         Body may include provider_id/model override
GET    /api/sessions/{id}/messages       Get message history
GET    /api/sessions/{id}/events         SSE stream of live events

POST   /api/sessions/{id}/cancel         Cancel the current run (→ idle)
POST   /api/sessions/{id}/resume         Deliver a suspended tool result (→ running)
```

The `pause` and `stop` endpoints are removed. The `start` endpoint is replaced by `POST /messages`.

---

## Frontend Implications

- The session viewer should reflect three states: idle, running, suspended. All other states should be mapped to one of these or hidden.
- The message composer should be available in **idle** and **suspended** states (with appropriate labels), not just in `running` state.
- Provider/model selection should be available per-message, not only at session creation.
- Error messages from a failed run appear in the transcript as system messages, not as a session-level error banner that prevents further interaction.
- "Stop" and "pause" buttons are replaced by a "Cancel" button that is only active when the session is running.

# Tool Call Execution — Async-First Design

## Core Concept: `ToolCallEval`

Introduce a first-class **`ToolCallEval`** entity (analogous to a `Session` run) that has its own
lifecycle: `pending → running → suspended | done | error`. Sessions and tool call evals can both
suspend waiting on one or more other evals or sessions. The dependency graph is explicit, stored,
and used to trigger resumption when dependencies resolve.

This replaces the "agentic loop" with a **dependency-driven wake-up** system.

---

## New Types

**`toolcall.Eval`** — a running or pending tool invocation

```go
// Package: backend/internal/toolcall/

type Eval struct {
    ID          string          // stable opaque ID — used as the resume/completion token
    ToolName    string
    Input       json.RawMessage
    SessionID   string          // which session originated this call
    AttemptID   string          // which run attempt originated this call
    State       EvalState       // pending | running | suspended | done | error
    Result      string          // final output (on done)
    Error       string          // error message (on error)
    DepsWaiting []Dependency    // IDs of evals/sessions this eval is waiting on
    // HandlerState is an opaque blob persisted by the handler when it suspends.
    // The framework stores it and passes it back to the handler on resume.
    // Handlers that do not suspend may leave this nil.
    HandlerState json.RawMessage `json:"handler_state,omitempty"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type EvalState string

const (
    EvalStatePending   EvalState = "pending"
    EvalStateRunning   EvalState = "running"
    EvalStateSuspended EvalState = "suspended"
    EvalStateDone      EvalState = "done"
    EvalStateError     EvalState = "error"
)
```

**Resume tokens are just `Eval.ID`s** — no new storage type needed. An eval ID is the opaque token
handed back to the session and to the tool handler; it is sufficient to look up, resume, or
complete the eval at any later point.

---

## Dependency Tracking

`SuspensionContext` gains a `[]Dependency` list instead of a single `ToolCallID`:

```go
// In backend/internal/session/snapshot.go

type Dependency struct {
    Kind string // "eval" | "session"
    ID   string
}

type SuspensionContext struct {
    Reason        string
    Dependencies  []Dependency  // replaces the single ToolCallID field
    PendingInput  []string
    ProviderState []byte
    Timestamp     time.Time
}
```

A session or eval is woken when **all** of its dependencies reach a terminal state. If any one
resolves (or errors), the wake-up logic checks and resumes if all are satisfied.

A global **`WakeupRegistry`** in the executor watches for state transitions and fires resumptions:

```go
// In backend/internal/service/

type WakeupRegistry interface {
    // Watch registers that waiter (a session or eval) is blocked on deps.
    // Returns ErrCyclicDependency if registering this watch would create a cycle.
    Watch(waiterKind, waiterID string, deps []Dependency) error
    // Notify is called when dep reaches a terminal state.
    // Triggers waiter resumption if all of its deps are now resolved.
    Notify(dep Dependency)
    // Cancel removes all watches for the given waiter.
    Cancel(waiterKind, waiterID string)
}
```

---

## Tool Handler Interface

Two modes, both available on `ToolDef`:

```go
// In backend/internal/tools/

// Handler is the synchronous "easy mode" — a plain Go function.
// The framework runs it in a goroutine, waits for it, and automatically
// resolves the ToolCallEval and wakes the waiting session.
// No serialization or resumption is needed for sync handlers.
type Handler func(ctx context.Context, input json.RawMessage) (string, error)

// AsyncHandler is for long-running or cross-session tools.
//
// The handler receives an EvalHandle it can use to:
//   - suspend the eval (with optional serialized state and declared dependencies)
//   - complete it from any goroutine at any time, including after a process restart
//   - fail it
//
// Returning nil means "I'm running in the background; don't auto-resolve."
// Returning an error immediately fails the eval.
//
// When the eval is resumed after a suspend (e.g. after a restart), the framework
// reconstructs the EvalHandle from persisted state and calls ResumeFunc instead
// of the main handler function. If ResumeFunc is nil, the main Handler is called
// again with the persisted HandlerState available via EvalHandle.State().
type AsyncHandler struct {
    Run        func(ctx context.Context, input json.RawMessage, h EvalHandle) error
    ResumeFunc func(ctx context.Context, h EvalHandle) error // optional
}

// EvalHandle is the interface passed to AsyncHandler.Run and ResumeFunc.
// All methods are safe to call from any goroutine, including after the
// original Run call has returned.
type EvalHandle interface {
    // EvalID returns the stable eval ID (== resume token).
    EvalID() string

    // State returns the HandlerState blob that was passed to Suspend,
    // or nil if this is a fresh invocation.
    State() json.RawMessage

    // Complete resolves the eval successfully. Idempotent after the first call.
    Complete(result string)

    // Fail marks the eval as failed. Idempotent after the first call.
    Fail(err error)

    // Suspend checkpoints the eval:
    //   - handlerState is an opaque blob the handler can use to reconstruct its
    //     position on resume (must be JSON-serializable; may be nil).
    //   - deps are the evals/sessions this eval is now waiting on.
    // The eval transitions to EvalStateSuspended. The framework will call
    // ResumeFunc (or Run again if ResumeFunc is nil) when all deps resolve.
    Suspend(handlerState json.RawMessage, deps []Dependency) error

    // Cancel registers a function to call if the eval is cancelled externally.
    // Only the most recently registered function is kept.
    OnCancel(fn func())
}

// ToolDef gains both handler fields; set exactly one.
type ToolDef struct {
    Name         string
    Description  string
    InputSchema  json.RawMessage
    Handler      Handler      // sync easy-mode; set Handler OR AsyncHandler, not both
    AsyncHandler *AsyncHandler
}
```

The executor wraps a sync `Handler` into an `AsyncHandler` automatically at registration time:

```go
// In backend/internal/tools/wrap.go

func WrapSync(h Handler) *AsyncHandler {
    return &AsyncHandler{
        Run: func(ctx context.Context, input json.RawMessage, handle EvalHandle) error {
            go func() {
                result, err := h(ctx, input)
                if err != nil {
                    handle.Fail(err)
                } else {
                    handle.Complete(result)
                }
            }()
            return nil
        },
    }
}
```

---

## Cancellation

Evals are cancellable at any time via `EvalManager.Cancel(evalID)`. Cancellation:

1. Sets `Eval.State = error` with `Eval.Error = "cancelled"`.
2. Calls the `OnCancel` function registered via `EvalHandle.OnCancel()` (if any), allowing the
   handler to abort in-flight work (e.g. cancel a child context, stop a subprocess).
3. Calls `WakeupRegistry.Cancel(waiterKind="eval", waiterID=evalID)` to drop any watches this
   eval had registered.
4. Calls `WakeupRegistry.Notify(Dependency{Kind:"eval", ID:evalID})` so that anything waiting on
   *this* eval wakes up. The woken session/eval receives an error result for the cancelled tool
   call and can decide how to proceed (fail, retry, or continue without the result).

If a session is cancelled while suspended waiting on evals, `EvalManager.Cancel` is called for
each eval in `SuspensionContext.Dependencies` whose origin session matches the cancelled session.

---

## Execution Flow

```
Provider (OpenAI/etc.) emits ToolCallEvent{status: "running"}
         │
         ▼
AgentExecutor.handleEvents() detects EventTypeToolCall with status "running"
         │
         ▼
EvalManager.Dispatch(ctx, DispatchOptions{ToolName, Input, SessionID, AttemptID})
   → looks up ToolDef (wrapping sync Handler if needed)
   → creates Eval{ID: newID, State: running}
   → persists Eval to EvalStorage
   → constructs evalHandle{evalID: newID, manager: m}
   → launches goroutine: toolDef.AsyncHandler.Run(ctx, input, evalHandle)
   → returns evalID
         │
         ▼
AgentExecutor suspends the session:
   SuspensionContext.Dependencies = []Dependency{{Kind:"eval", ID:evalID}}
   WakeupRegistry.Watch("session", sessionID, deps)
         │
         ┌─────────────────────────────────────────────────────────────┐
         │  Possible handler paths:                                    │
         │                                                             │
         │  A) Sync-wrapped handler finishes immediately:              │
         │     goroutine calls handle.Complete(result)                 │
         │                                                             │
         │  B) AsyncHandler suspends waiting on another session:       │
         │     handle.Suspend(handlerState, []Dependency{              │
         │         {Kind:"session", ID: s2}})                          │
         │     → Eval.HandlerState = handlerState, State = suspended   │
         │     → WakeupRegistry.Watch("eval", evalID, deps)            │
         │     → when s2 finishes a run, WakeupRegistry.Notify fires   │
         │     → EvalManager calls AsyncHandler.ResumeFunc(ctx, h)     │
         │       (or Run again if ResumeFunc is nil)                   │
         │                                                             │
         │  C) External cancellation:                                  │
         │     EvalManager.Cancel(evalID)                              │
         │     → OnCancel fn called, state = error/"cancelled"         │
         │     → WakeupRegistry.Notify wakes waiting session           │
         └─────────────────────────────────────────────────────────────┘
         │
         ▼
handle.Complete(result) — from any goroutine, any time
   → Eval.State = done, Eval.Result = result
   → persists Eval
   → WakeupRegistry.Notify(Dependency{Kind:"eval", ID:evalID})
         │
         ▼
WakeupRegistry finds all waiters watching this eval
   → for each waiter, checks if ALL deps are now in a terminal state
   → if yes, for session waiters:
       AgentExecutor.resumeSessionWithToolResults(sessionID, []ToolResult{...})
     for eval waiters:
       EvalManager.resumeEval(evalID)
         │
         ▼
Provider resumes: appends tool_result message(s) to history, re-calls the model
```

---

## Circular Session → Eval → Session Handling

A tool's `AsyncHandler.Run` can call:

```go
handle.Suspend(handlerState, []Dependency{{Kind: "session", ID: targetSessionID}})
```

This means the eval suspends waiting on `targetSessionID` to finish a run. The target session runs
independently; when it reaches idle, `WakeupRegistry.Notify` fires, which resumes the eval. The
eval's `ResumeFunc` can then collect the session's last output as its result.

**Cycle detection**: when `WakeupRegistry.Watch` is called, a DFS traverses the current dependency
graph. If the new watch would create a cycle, `Watch` returns `ErrCyclicDependency`. The
`EvalManager` then calls `handle.Fail(ErrCyclicDependency)`, which wakes the waiting session with
an error tool result. The session can surface the error to the model and continue.

---

## Parallel Tool Calls

When the model requests N tools simultaneously, the provider emits N `ToolCallEvent{status:
"running"}` events. The executor dispatches N evals and suspends the session with all N as
dependencies:

```go
SuspensionContext.Dependencies = []Dependency{
    {Kind: "eval", ID: eval1ID},
    {Kind: "eval", ID: eval2ID},
    {Kind: "eval", ID: eval3ID},
}
WakeupRegistry.Watch("session", sessionID, allThreeDeps)
```

The `WakeupRegistry` resumes the session only when all three evals reach a terminal state
(done or error). `resumeSessionWithToolResults` collects results from all three and injects them
together, satisfying the model's expectation of a `tool_result` for each outstanding `tool_use`.

---

## `EvalManager` (new service)

```go
// backend/internal/service/eval_manager.go

type EvalManager struct {
    tools   tools.Registry
    evals   map[string]*toolcall.Eval  // in-memory; also persisted
    storage EvalStorage
    wakeup  WakeupRegistry
    mu      sync.RWMutex
}

func (m *EvalManager) Dispatch(ctx context.Context, opts DispatchOptions) (evalID string, err error)
func (m *EvalManager) Complete(evalID, result string) error
func (m *EvalManager) Fail(evalID string, err error) error
func (m *EvalManager) Suspend(evalID string, handlerState json.RawMessage, deps []Dependency) error
func (m *EvalManager) Cancel(evalID string) error
func (m *EvalManager) Get(evalID string) (*toolcall.Eval, error)
func (m *EvalManager) resumeEval(evalID string)             // internal; called by WakeupRegistry
```

`EvalManager` is injected into `AgentExecutor`. A reference to it is also made available to tool
handlers via a context value (`toolcall.ManagerFromContext(ctx)`), so async tools can call
`Complete()` or `Fail()` from entirely separate goroutines or even a separate HTTP request handler.

---

## Provider-Side: Resumption With Tool Results

Session providers need one new method on `Suspendable`:

```go
// backend/internal/session/snapshot.go

type ToolResult struct {
    ToolCallID string
    Result     string
    IsError    bool
}

type Suspendable interface {
    Suspend(ctx context.Context) (*SuspensionContext, error)
    Resume(ctx context.Context, sc *SuspensionContext) error
    // ResumeWithToolResults resumes the provider and injects tool results into
    // the conversation history before re-running the model.
    ResumeWithToolResults(ctx context.Context, sc *SuspensionContext, results []ToolResult) error
}
```

Provider implementations:

- **OpenAI**: appends `oai.ToolMessage(result, toolCallID)` entries then re-calls the API.
- **Anthropic direct-API** (future): appends `tool_result` content blocks then re-calls the API.
- **Claude Code subprocess**: writes a JSON `tool_result` object to stdin.

---

## Storage

New interface alongside `ResumeTokenStorage`:

```go
// backend/internal/storage/eval_storage.go

type EvalStorage interface {
    SaveEval(e *toolcall.Eval) error
    LoadEval(evalID string) (*toolcall.Eval, error)
    ListEvalsForSession(sessionID string) ([]*toolcall.Eval, error)
    DeleteEval(evalID string) error
}
```

On-disk layout: `sessions/<sessionID>/evals/<evalID>.json` — keeps evals co-located with their
origin session for easy cleanup and recovery on restart.

On startup, `EvalManager` loads all non-terminal evals and re-registers their watches in the
`WakeupRegistry` so that restarts don't permanently orphan suspended evals.

---

## Files to Create / Modify

| File | Change |
|---|---|
| `backend/internal/toolcall/eval.go` | New — `Eval`, `EvalState`, `Dependency`, `EvalHandle`, `EvalManager` interface |
| `backend/internal/tools/registry.go` | Add `Handler`, `AsyncHandler`, `EvalHandle` to `ToolDef`; keep `Register`/`Lookup`/`List` |
| `backend/internal/tools/wrap.go` | New — `WrapSync()` adaptor; `wrapAtRegistration()` internal helper |
| `backend/internal/session/snapshot.go` | `SuspensionContext.Dependencies []Dependency` (replaces `ToolCallID`); add `ToolResult` |
| `backend/internal/service/eval_manager.go` | New — `EvalManager`, concrete `WakeupRegistry`, `evalHandle` impl |
| `backend/internal/service/execution_coordinator.go` | Wire `EvalManager.Dispatch()` on tool call events; add `resumeSessionWithToolResults()` |
| `backend/internal/storage/eval_storage.go` | New — `EvalStorage` interface + `JSONFileStorage` impl |
| `backend/internal/provider/common/openai/session.go` | Implement `ResumeWithToolResults` |
| `backend/internal/provider/common/claude/claudecode.go` | Implement `ResumeWithToolResults` |
| `backend/cmd/orbitmesh/main.go` | Wire `EvalManager` into executor; register real `read_file`/`write_file` handlers |

---

## Explicitly Out of Scope (for this pass)

- **Timeout / TTL on evals** — add later via a background reaper goroutine in `EvalManager`.
- **Retry policy** — evals that error can be retried by the model on the next turn; no automatic
  retry in the framework.
- **Eval visibility in the API** — evals are internal; expose via a `/sessions/:id/evals` endpoint
  in a follow-up task.

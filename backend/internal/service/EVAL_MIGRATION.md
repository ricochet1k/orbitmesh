# EvalManager → entity.Store Migration Plan

## Goal

Replace `EvalManager` with `EvalCoordinator`, built on top of `entity.Store`
(not `ActiveStore`). The migration should make the code simpler and harder to
get wrong, not just differently organised. The measure of success is: after the
migration, it should be structurally impossible to hit the wake-before-suspend
race, the `context.Background()` cancellation gap, and the circular
`AgentExecutor ↔ EvalManager` callback dependency — not just fixed by careful
ordering.

**Serialisability invariant**: the system must be able to stop accepting new
work and restart without losing anything. This requires that no durable state
is encoded only in goroutines or channels. In particular, a suspended eval
waiting on deps must not require a goroutine to be parked for the duration of
the wait — a tool that waits a day must use zero goroutines during that day.

---

## What `EvalManager` currently does (three tangled concerns)

1. **Cache + lock** — `evals map[string]*Eval` guarded by `mu sync.RWMutex`
2. **Wakeup graph** — `WakeupRegistry`, `sessionDeps map`, `inMemoryWakeupRegistry`
3. **Goroutine launcher** — `Dispatch`, `resumeEval`, `pendingDispatches`

These three concerns are tangled in ways that force the current two-phase
`PrepareDispatches` / `suspendSession` / `Launch` protocol and the
`OnSessionWake` callback chain. The entity store owns concerns 1 and 2
natively; concern 3 becomes a simple `scheduleRun` helper (not `ActiveStore`).

---

## Bugs eliminated structurally by this migration

| Bug | Current mitigation | After migration |
|---|---|---|
| Wake fires before `Watch` registered | Three-phase `PrepareDispatches`/`Launch` protocol | `Watch` on session before any eval `Create`; store guarantees `MarkDone` fires after storage write only |
| `context.Background()` in `resumeEval` | None (bug is live) | Goroutine created fresh per dispatch/resume with executor's cancellable context |
| Circular `AgentExecutor ↔ EvalManager` callback | `OnSessionWake` function pointer set at wiring time | Deleted; coordinator calls `onDone` callback directly — no back-reference into executor at all for session wakeup |
| `Cancel` has no idempotence gate (unlike `Complete`/`Fail`) | None | `MutateWhen` returning `(false, nil)` for already-terminal evals |
| Goroutine parked for entire suspension duration | None (bug is live — "wait a day" = goroutine for a day) | Goroutine exits after `Suspend()`; `onDone` reschedules a new one when deps fire |
| `sessionWatchState` lost on restart (sessions never wake) | None (bug is live) | `OnRestart` re-registers session watches from persisted eval `DepsWaiting`; `onDone` fires `resumeSessionWithToolResults` |

---

## Design: goroutine-free suspension

The key insight is that `entity.Store` (not `ActiveStore`) is the right base.
`ActiveStore` keeps a goroutine alive per entity for the entity's entire life.
That is correct for entities that are always doing I/O (e.g. a terminal). It
is wrong for evals, which are idle during suspension.

Instead, `EvalCoordinator` manages goroutines manually:

- **On `Create` (new dispatch):** spawn one goroutine that calls `handler.Run`.
- **When the handler calls `handle.Suspend()`:** `Suspend` persists state + deps
  and **returns normally**. The goroutine then returns from `handler.Run` and
  exits. No goroutine is parked.
- **When all deps fire `MarkDone`:** the store calls `onDone("eval", id)`,
  which the coordinator has registered as a callback. The coordinator looks up
  the eval, and if it is suspended (not cancelled), spawns a new goroutine
  calling `handler.ResumeFunc` (or `handler.Run` again).
- **On `OnRestart`:** the coordinator iterates all persisted evals. Suspended
  evals re-register their watches via `DepSource.Deps()`. Running evals (those
  interrupted mid-flight) are marked as errored so the session can be woken
  with an error result. The coordinator relies on `Store.Get`'s built-in
  dep re-registration, so no special recovery code is needed beyond iterating.

This makes the system fully restartable with zero parked goroutines during
suspension. A "wait a day" tool uses zero goroutines during that day.

### `onDone` callback

`entity.Store` needs a way to notify the coordinator when a dep fires. Rather
than adding a global callback bus, add an `OnDone` option to `StoreOptions`:

```go
// In entity/entity.go:
type StoreOptions[T Snapshotter[S], S any] struct {
    Kind           string
    FromSnapshot   func(s S) T
    IDFromSnapshot func(s S) string
    IsDone         func(s S) bool

    // OnDone is called (outside any lock) after MarkDone fires and all revdep
    // watchers have been notified. id is the entity that became done.
    // Used by coordinators to trigger reschedule logic without polling.
    OnDone func(id string)
}
```

`fireDone` in `store.go` calls `s.opts.OnDone(id)` after walking revdeps.
This is a single-line addition to the existing `fireDone` function.

---

## Step 1 — Make `*Eval` implement entity interfaces (~10 lines)

In `toolcall/eval.go`, add four methods to `*Eval`:

```go
// EvalSnapshot is the serialisable projection of Eval.
// Because Eval contains only value types, the snapshot type is identical.
type EvalSnapshot = Eval

func (e *Eval) Snapshot() EvalSnapshot { return *e }
func (e *Eval) EntityID() string       { return e.ID }

// IsDone satisfies entity.DepSource. A terminal eval never un-terminals.
func (e *Eval) IsDone() bool {
    return e.State == EvalStateDone || e.State == EvalStateError
}

// Deps satisfies entity.DepSource. Returns the eval's current waiting deps
// so the store can re-register watches automatically after a crash restart.
func (e *Eval) Deps() []entity.Dep {
    deps := make([]entity.Dep, len(e.DepsWaiting))
    for i, d := range e.DepsWaiting {
        deps[i] = entity.Dep{Kind: d.Kind, ID: d.ID}
    }
    return deps
}
```

`*Eval` now satisfies:
- `entity.Snapshotter[EvalSnapshot]` (required by `Store`)
- `entity.IDer` (used by `Store.OnRestart` / `List`)
- `entity.DepSource` (used by `Store.Get` for crash-recovery dep re-registration)

No other changes to `toolcall/eval.go`.

---

## Step 2 — Add `OnDone` to `StoreOptions` and wire it in `fireDone`

In `entity/entity.go`, add the `OnDone` field to `StoreOptions` (shown in the
design section above).

In `entity/store.go`, at the end of `fireDone`, after walking revdeps:

```go
func (s *Store[T, S]) fireDone(id string) {
    key := s.opts.Kind + ":" + id
    s.depMu.RLock()
    watchers := make([]string, len(s.revdeps[key]))
    copy(watchers, s.revdeps[key])
    s.depMu.RUnlock()

    for _, watcherID := range watchers {
        if s.allDepsDone(watcherID) {
            s.closeWakeC(watcherID)
        }
    }

    // Notify the coordinator that this entity is done.
    // Called outside any lock; safe for the callback to re-enter the store.
    if s.opts.OnDone != nil {
        s.opts.OnDone(id)
    }
}
```

No other changes to `store.go`.

---

## Step 3 — Write a `TypedStorage[EvalSnapshot]` adapter

New file: `storage/eval_store_adapter.go`

Wraps the existing `EvalStorage` interface behind
`entity.TypedStorage[toolcall.EvalSnapshot]`. The dual-write
(`evals/<id>.json` and `sessions/<sid>/evals/<id>.json`) stays entirely
inside the adapter — the store never sees it.

```go
type EvalStorageAdapter struct{ inner EvalStorage }

func (a *EvalStorageAdapter) Save(s toolcall.EvalSnapshot) error {
    return a.inner.SaveEval(&s)
}
func (a *EvalStorageAdapter) Load(id string) (toolcall.EvalSnapshot, error) {
    e, err := a.inner.LoadEval(id)
    if err != nil { return toolcall.EvalSnapshot{}, err }
    return *e, nil
}
func (a *EvalStorageAdapter) Delete(id string) error {
    return a.inner.DeleteEval(id)
}
func (a *EvalStorageAdapter) List() ([]toolcall.EvalSnapshot, error) {
    // Used only by OnRestart; loads all evals across all sessions.
    // Acceptable cost at startup; not called during normal operation.
    panic("use ListEvalsForSession for session-scoped listing")
}
func (a *EvalStorageAdapter) ListIDs() ([]string, error) {
    // Directory scan of evals/ flat index — no file reads.
    return a.inner.ListEvalIDs()
}
```

`ListEvalsForSession` remains a method on the adapter (not on `TypedStorage`)
for the session-scoped listing that `OnRestart` uses during recovery.

Add `ListEvalIDs() ([]string, error)` to `EvalStorage` and implement it in
`JSONFileStorage` as a `ReadDir` on `evals/` — no JSON parsing.

---

## Step 4 — Build `EvalCoordinator` on `entity.Store`

New file: `service/eval_coordinator.go`

```go
type EvalCoordinator struct {
    evals   *entity.Store[*toolcall.Eval, toolcall.EvalSnapshot]
    tools   tools.Registry
    ctx     context.Context  // executor's long-lived cancellable context

    // onSessionDone is called when all evals for a session have completed.
    // The coordinator calls it with the collected ToolResults.
    // Set once at construction; not changed afterwards.
    onSessionDone func(sessionID string, results []session.ToolResult)
}
```

No `mu`, no `evals map`, no `sessionDeps map`, no `WakeupRegistry`, no
`OnSessionWake` callback, no `ActiveStore`, no parked goroutines.

### Construction

```go
func NewEvalCoordinator(
    ctx context.Context,
    toolRegistry tools.Registry,
    evalStorage storage.EvalStorage,
    onSessionDone func(string, []session.ToolResult),
) *EvalCoordinator {
    c := &EvalCoordinator{
        tools:         toolRegistry,
        ctx:           ctx,
        onSessionDone: onSessionDone,
    }
    c.evals = entity.NewStore[*toolcall.Eval, toolcall.EvalSnapshot](
        &storage.EvalStorageAdapter{Inner: evalStorage},
        nil, // no event bus needed for evals
        entity.StoreOptions[*toolcall.Eval, toolcall.EvalSnapshot]{
            Kind:         "eval",
            FromSnapshot: func(s toolcall.EvalSnapshot) *toolcall.Eval { e := s; return &e },
            IDFromSnapshot: func(s toolcall.EvalSnapshot) string { return s.ID },
            IsDone: func(s toolcall.EvalSnapshot) bool {
                return s.State == toolcall.EvalStateDone || s.State == toolcall.EvalStateError
            },
            OnDone: func(id string) { c.onEvalDone(id) },
        },
    )
    return c
}
```

### `scheduleRun` — spawns a goroutine for one eval, exits when done or suspended

This replaces both `Dispatch`'s goroutine and `resumeEval`'s goroutine. The
goroutine calls `Run` (first dispatch) or `ResumeFunc` (after deps fire), then
exits regardless of whether the handler calls `Complete`, `Fail`, or `Suspend`.

```go
func (c *EvalCoordinator) scheduleRun(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], isSuspended bool) {
    var toolName string
    var input json.RawMessage
    h.Read(func(e **toolcall.Eval) {
        toolName    = (*e).ToolName
        input       = (*e).Input
    })

    def, ok := c.tools.Lookup(toolName)
    if !ok {
        _ = h.Mutate(func(e **toolcall.Eval) {
            (*e).State = toolcall.EvalStateError
            (*e).Error = fmt.Sprintf("tool %q not found", toolName)
        })
        h.MarkDone()
        return
    }
    asyncHandler := tools.WrapAtRegistration(def)
    handle := newEvalHandle(h, c)

    go func() {
        var err error
        if isSuspended && asyncHandler.ResumeFunc != nil {
            err = asyncHandler.ResumeFunc(c.ctx, handle)
        } else {
            err = asyncHandler.Run(c.ctx, input, handle)
        }
        // If the handler returned an error without calling Complete/Fail/Suspend,
        // fail the eval now. If it already called one of those, this is a no-op
        // because evalHandle.done is already set.
        if err != nil {
            handle.Fail(err)
        }
        // Goroutine exits here regardless of whether the handler suspended.
        // If it suspended, onEvalDone will reschedule when deps fire.
    }()
}
```

**Key difference from the old design:** there is no `for { select { case <-WakeC() } }`
loop. The goroutine simply returns. Suspension is encoded entirely in the
persisted `Eval.State` + `Eval.DepsWaiting`, not in any in-memory channel.

### `onEvalDone` — called by `Store.OnDone` when an eval becomes terminal

```go
func (c *EvalCoordinator) onEvalDone(id string) {
    h, err := c.evals.Get(id)
    if err != nil {
        return
    }

    var state toolcall.EvalState
    var sessionID string
    h.Read(func(e **toolcall.Eval) {
        state     = (*e).State
        sessionID = (*e).SessionID
    })

    // If this eval itself is suspended (waiting on sub-deps), this callback
    // fires when those sub-deps are done. Re-schedule its resume goroutine.
    if state == toolcall.EvalStateSuspended {
        c.scheduleRun(h, true)
        return
    }

    // The eval is terminal (done or error). Check whether the owning session
    // is now fully unblocked.
    c.maybeWakeSession(sessionID)
}
```

### `maybeWakeSession` — collects results and fires session resume if all evals are done

```go
func (c *EvalCoordinator) maybeWakeSession(sessionID string) {
    if sessionID == "" || c.onSessionDone == nil {
        return
    }

    ids, err := c.evals.ListIDs()
    if err != nil {
        return
    }

    var results []session.ToolResult
    for _, id := range ids {
        h, err := c.evals.Get(id)
        if err != nil {
            continue
        }
        var e toolcall.Eval
        h.Read(func(ep **toolcall.Eval) { e = **ep })

        if e.SessionID != sessionID {
            continue
        }
        if e.State != toolcall.EvalStateDone && e.State != toolcall.EvalStateError {
            // At least one eval for this session is still running or suspended.
            return
        }
        toolCallID := e.ProviderToolCallID
        if toolCallID == "" {
            toolCallID = e.ID
        }
        tr := session.ToolResult{
            ToolCallID: toolCallID,
            IsError:    e.State == toolcall.EvalStateError,
        }
        if e.State == toolcall.EvalStateError {
            tr.Result = e.Error
        } else {
            tr.Result = e.Result
        }
        results = append(results, tr)
    }

    // All evals for this session are terminal.
    c.onSessionDone(sessionID, results)
}
```

Note: `maybeWakeSession` iterates all eval IDs. At typical batch sizes (1–10
parallel tool calls per session turn) this is negligible. If the eval count
grows large, a session→evalID reverse index can be added later without changing
the interface.

### `DispatchBatch` — replaces `PrepareDispatches` + `Launch`

```go
// DispatchBatch creates evals for all calls, registers the session watch
// BEFORE starting any goroutine, and starts goroutines.
// The caller suspends the session after this returns.
func (c *EvalCoordinator) DispatchBatch(
    sessionID string,
    calls []DispatchOptions,
) ([]toolcall.Dependency, error) {

    now := time.Now().UTC()
    handles := make([]entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot], len(calls))
    deps := make([]toolcall.Dependency, len(calls))

    // Create all evals (persisted) before starting any goroutine.
    for i, opts := range calls {
        id := newEvalID()
        e := &toolcall.Eval{
            ID: id, ToolName: opts.ToolName, Input: opts.Input,
            SessionID: opts.SessionID, AttemptID: opts.AttemptID,
            ProviderToolCallID: opts.ProviderToolCallID,
            State: toolcall.EvalStateRunning,
            CreatedAt: now, UpdatedAt: now,
        }
        h, err := c.evals.Create(id, e)
        if err != nil {
            return nil, err
        }
        handles[i] = h
        deps[i] = toolcall.Dependency{Kind: "eval", ID: id}
    }

    // Now start goroutines. Even if one completes instantly and calls
    // onEvalDone → maybeWakeSession, the session is not yet suspended so
    // onSessionDone will check and find nothing to do (session still running).
    // suspendSession is called by the caller after this returns.
    for _, h := range handles {
        c.scheduleRun(h, false)
    }

    return deps, nil
}
```

**Why no explicit session watch registration here?** With goroutine-free
suspension, there is no `WakeC` channel to register. The session watch is
implicit: `maybeWakeSession` is called every time any eval for the session
becomes terminal, and it checks whether all evals for that session are done
before firing `onSessionDone`. No pre-registration required, no channel to
reconstruct on restart.

### `CancelEvalsForSession` — cancellation

```go
func (c *EvalCoordinator) CancelEvalsForSession(sessionID string) {
    ids, _ := c.evals.ListIDs()
    for _, id := range ids {
        h, err := c.evals.Get(id)
        if err != nil {
            continue
        }
        var sid string
        var done bool
        h.Read(func(e **toolcall.Eval) {
            sid  = (*e).SessionID
            done = (*e).IsDone()
        })
        if sid != sessionID || done {
            continue
        }
        _, _ = h.MutateWhen(func(e **toolcall.Eval) (bool, error) {
            if (*e).IsDone() {
                return false, nil // already terminal; skip
            }
            (*e).State = toolcall.EvalStateError
            (*e).Error = "cancelled"
            return true, nil
        })
        h.MarkDone()
    }
}
```

Running goroutines see `c.ctx.Done()` if the executor shuts down, or they call
`Complete`/`Fail`/`Suspend` naturally. There is no goroutine to kill for
suspended evals because they have no parked goroutine. The persisted error
state is what matters.

---

## Step 5 — `flushPendingToolCalls` — replaces the three-phase protocol

In `execution_coordinator.go`, `flushPendingToolCalls` becomes:

```go
func (e *AgentExecutor) flushPendingToolCalls(ctx context.Context, sc *sessionContext, calls []DispatchOptions) {
    if e.evalCoordinator == nil || len(calls) == 0 {
        return
    }

    deps, err := e.evalCoordinator.DispatchBatch(sc.session.ID, calls)
    if err != nil {
        log.Printf("DispatchBatch failed for session %s: %v", sc.session.ID, err)
        return
    }

    lastToolCallID := calls[len(calls)-1].ProviderToolCallID
    e.suspendSession(sc, lastToolCallID, deps)
    // No waiter goroutine needed. When all evals complete, onSessionDone
    // (set at coordinator construction) calls resumeSessionWithToolResults
    // directly. No channel, no blocked goroutine.
}
```

The `onSessionDone` callback passed to `NewEvalCoordinator` is:

```go
func(sessionID string, results []session.ToolResult) {
    e.resumeSessionWithToolResults(sessionID, results)
}
```

This is set once at wiring time; the coordinator holds no other reference into
the executor. The circular dependency is broken because `EvalCoordinator` does
not import `AgentExecutor` — it only calls a `func(string, []ToolResult)`.

---

## Step 6 — `OnRestart` — crash recovery

```go
func (c *EvalCoordinator) OnRestart(ctx context.Context) error {
    return c.evals.OnRestart(ctx, func(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) error {
        var state toolcall.EvalState
        h.Read(func(e **toolcall.Eval) { state = (*e).State })

        switch state {
        case toolcall.EvalStateRunning:
            // Was interrupted mid-flight. Mark as error so the session can wake.
            _ = h.Mutate(func(e **toolcall.Eval) {
                (*e).State = toolcall.EvalStateError
                (*e).Error = "interrupted by restart"
            })
            h.MarkDone()
            // MarkDone → onEvalDone → maybeWakeSession fires for each eval.
            // Once all running evals for a session are errored, the session wakes.

        case toolcall.EvalStateSuspended:
            // Deps are re-registered automatically by Store.Get via DepSource.Deps().
            // The Store called watch() during OnRestart's Get call.
            // No goroutine is started here; scheduleRun is called by onEvalDone
            // when the deps fire (or immediately below if they are already done).
            //
            // Check now whether all deps are already terminal (e.g. they were
            // completed before the restart and their terminal state is persisted).
            var depsDone bool
            var alreadyDone bool
            h.Read(func(e **toolcall.Eval) {
                alreadyDone = (*e).IsDone()
            })
            if !alreadyDone {
                depsDone = c.checkDepsDone(h)
            }
            if depsDone {
                c.scheduleRun(h, true)
            }
            // If deps are not done, scheduleRun is called by onEvalDone when they fire.

        case toolcall.EvalStateDone, toolcall.EvalStateError:
            // Already terminal — no action needed. Store.Get registered no watch.
        }

        return nil
    })
}

// checkDepsDone reports whether all DepsWaiting for h are currently terminal.
func (c *EvalCoordinator) checkDepsDone(h entity.Handle[*toolcall.Eval, toolcall.EvalSnapshot]) bool {
    var deps []toolcall.Dependency
    h.Read(func(e **toolcall.Eval) { deps = (*e).DepsWaiting })
    for _, dep := range deps {
        if dep.Kind != "eval" {
            continue // cross-kind deps not checked here; session migration handles those
        }
        dh, err := c.evals.Get(dep.ID)
        if err != nil {
            return false
        }
        var done bool
        dh.Read(func(e **toolcall.Eval) { done = (*e).IsDone() })
        if !done {
            return false
        }
    }
    return true
}
```

**Session recovery during restart:** Running evals are immediately marked as
errors and `MarkDone` is called. `onEvalDone` fires, `maybeWakeSession` checks
whether all evals for the session are done, and if so calls `onSessionDone`
(i.e. `resumeSessionWithToolResults`). This means sessions that were suspended
waiting for tool calls will automatically be woken with error results on
restart — no special session-level recovery code is needed.

---

## Step 7 — Delete `EvalManager` and `sessionWatchState`

In `eval_manager.go`, delete:
- `WakeupRegistry` interface and `inMemoryWakeupRegistry` struct
- `EvalManager` struct and all methods
- `pendingDispatches` / `PrepareDispatches` / `Launch`
- `handleWake` / `handleSessionWake` / `handleSessionWakeWithDeps`
- `isTerminalDep`
- `NotifySessionTerminal`

In `execution_coordinator.go`:
- `flushPendingToolCalls` shrinks to ~10 lines (shown in Step 5)
- No `sessionWatchState` struct needed — removed entirely
- No waiter goroutine in `flushPendingToolCalls`

In `executor.go`:
- `evalManager *EvalManager` → `evalCoordinator *EvalCoordinator`
- `evalManager.OnSessionWake = ...` wiring → `onSessionDone` func at construction
- No `sessionWatches` field

`sessionWatchState` is not replaced by anything — it simply disappears. When
sessions are later migrated to `entity.Store`, the session entity will declare
its own `Deps()` and the store's dep graph will handle wakeup natively. Until
then, `maybeWakeSession`'s poll-on-done approach is correct and requires no
pre-registration.

---

## Files changed

| File | Change |
|---|---|
| `toolcall/eval.go` | Add `Snapshot()`, `EntityID()`, `IsDone()`, `Deps()` |
| `entity/entity.go` | Add `OnDone func(id string)` to `StoreOptions` |
| `entity/store.go` | Call `opts.OnDone(id)` at end of `fireDone` |
| `storage/eval_store_adapter.go` | **New** — `TypedStorage[EvalSnapshot]` wrapping `EvalStorage` |
| `storage/eval_storage.go` | Add `ListEvalIDs() ([]string, error)` to interface + impl |
| `service/eval_coordinator.go` | **New** — `EvalCoordinator` replacing `EvalManager` |
| `service/eval_manager.go` | **Delete** (or keep as empty shim during transition) |
| `service/execution_coordinator.go` | Rewrite `flushPendingToolCalls` (~10 lines); delete waiter goroutine; delete `sessionWatchState` usage |
| `service/executor.go` | Swap `evalManager` for `evalCoordinator`; delete `OnSessionWake` wiring; delete `sessionWatches` field |
| `service/executor_test.go` | Tests drive the existing behaviour; run to verify, fix failures |

---

## What disappears

| Deleted | Replaced by |
|---|---|
| `WakeupRegistry` interface + `inMemoryWakeupRegistry` | `entity.Store` dep graph + `OnDone` callback |
| `EvalManager.mu` + `evals map` | `entity.Store` |
| `EvalManager.sessionDeps map` | Removed entirely (`maybeWakeSession` polls on each `onEvalDone`) |
| `EvalManager.OnSessionWake` function pointer | `onSessionDone func` passed at construction |
| `sessionWatchState` struct + `wakeCs map` | Removed entirely |
| `pendingDispatches` / `PrepareDispatches` / `Launch()` | `DispatchBatch` (create-then-run, 2 sequential loops) |
| `resumeEval` | `scheduleRun(h, true)` called from `onEvalDone` |
| `handleWake` / `handleSessionWake` / `handleSessionWakeWithDeps` | `onEvalDone` + `maybeWakeSession` |
| `isTerminalDep` callback | `StoreOptions.IsDone` |
| `NotifySessionTerminal` | Stubbed; not needed until eval-on-session deps are used |
| `context.Background()` in `resumeEval` | `c.ctx` (executor's long-lived cancellable context) |
| Waiter goroutine in `flushPendingToolCalls` | Deleted — `onSessionDone` fires directly |
| Parked goroutine during suspension | Deleted — goroutine exits after `Suspend()` returns |

## What is explicitly NOT touched

- `toolcall.Eval` field shapes — no JSON schema changes
- `storage.EvalStorage` / `JSONFileStorage` internals — wrapped, not rewritten
- `tools.AsyncHandler` / `EvalHandle` interface — tool handlers see no change
- `suspendSession` / `resumeSessionWithToolResults` — logic unchanged
- `domain.Session` — not migrated in this pass
- `entity/active.go` — `ActiveStore` is not used for evals; left unchanged for
  other potential users

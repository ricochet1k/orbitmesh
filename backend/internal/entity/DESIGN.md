# Entity Design

This document proposes the `entity` package: a single, reusable abstraction for
durable objects in this codebase. It replaces the seven independent lock/store/
notify patterns currently scattered across `service`, `storage`, and `toolcall`.

---

## Problem statement

Every durable object in the codebase re-implements the same four-step pattern
manually, and each copy gets the ordering subtly wrong in different ways:

1. Lock the in-memory object
2. Mutate it
3. Unlock
4. Write to storage (now outside the lock — window for races)
5. Notify waiters / publish to frontend (also outside the lock — ordering hazard)

Current objects following this pattern:

| Object | Own lock | Storage interface | Notifications |
|---|---|---|---|
| `domain.Session` | `session.mu` | `Storage` | `EventBroadcaster.Broadcast` |
| `sessionContext` (run + attempt) | `executor.mu` + `runMu` + `amMu` | `Storage` + `RunAttemptStorage` | same broadcaster |
| `toolcall.Eval` | `EvalManager.mu` | `EvalStorage` | `WakeupRegistry.Notify` |
| `storage.RunAttemptMetadata` | `sc.amMu` | `RunAttemptStorage` | (none) |
| `storage.ResumeTokenMetadata` | implicit via `executor.mu` | `ResumeTokenStorage` | (none) |
| `TerminalHub` | `hub.mu` | `TerminalStorage` | observer callbacks |
| `WakeupRegistry` | `registry.mu` | (in-memory only) | `onWake` callbacks |

The dep/notification graph (`WakeupRegistry`) is bolted on the side rather than
being part of the object model, forcing `EvalManager` to maintain a parallel
`sessionDeps` map that mirrors what the registry already knows.

---

## Core design: `Store[T]` and `Handle[T]`

### The lock-per-entity approach (rejected)

One obvious approach is an `Entity[T]` that carries its own mutex. The problem is
that every mutation then requires: acquire entity lock → mutate → release → write
storage → notify. The last two steps happen outside any lock, which is exactly the
source of the current races. Storing closures for storage/notify on each entity
instance (as in the initial sketch) just moves the wiring problem without fixing it.

### Lock-per-store with fine-grained entity locks (this proposal)

The `Store[T]` owns the collection and a **per-entity** `sync.RWMutex` embedded
in a thin wrapper. A `Handle[T]` is an opaque reference `(id string, store *Store[T])`.
The only way to read or write `T` is through the handle. The lock is never exposed
to callers.

**Per-entity locks vs. one store-wide lock**: a single store-wide lock would
serialize all reads across all entities — correct but unnecessarily contended for
reads. Per-entity locks allow concurrent reads of different entities while still
serializing writes to the same entity. The store-level lock is only held during
map lookup (microseconds), not during storage I/O.

### Contention analysis

The contention concern is real but the workload here is low-traffic:

- Sessions: created once, mutated on state changes (~5–20 per session lifetime)
- Evals: mutated twice (create → complete/fail)
- Run attempts: mutated ~4 times per run (start, heartbeats, finalize)
- Resume tokens: mutated twice (mint, consume)

None of these are high-frequency. The per-entity lock granularity is correct;
the risk is lock ordering (A waits for B while B waits for A). The design avoids
this by **never holding two entity locks simultaneously** — cross-entity operations
are sequenced, not atomic.

---

## API sketch

```go
package entity

// Snapshot is the serialisable, deep-copy-safe view of an entity's state.
// T is the live (possibly pointer-rich) type; S is its serialisable projection.
// If T is already value-safe (no unexported fields, no live pointers), S == T.
type Snapshot[T any] interface {
    // Snapshot returns a serialisable copy safe to pass outside the lock.
    // Called while the entity write-lock is held, so it must not block.
    Snapshot() T
}

// Store holds a collection of live entities of type T with snapshot type S.
// It owns persistence, pub/sub notification, and dep/revdep tracking.
type Store[T Snapshot[S], S any] struct { ... }

// NewStore constructs a store with pluggable persistence and event bus.
// storage and bus may be nil (no persistence / no events).
func NewStore[T Snapshot[S], S any](
    storage  TypedStorage[S],
    bus      EventBus,
    opts     StoreOptions,
) *Store[T, S]

// Handle is an opaque reference to a single live entity.
// Obtain one via Store.Create, Store.Load, or Store.Get.
type Handle[T Snapshot[S], S any] struct {
    id    string
    store *Store[T, S]
}

// ID returns the entity's stable identifier.
func (h Handle[...]) ID() string

// Read calls fn with a read lock held. fn must not call back into the store.
func (h Handle[...]) Read(fn func(*T))

// Mutate calls fn under a write lock, then (outside the lock) persists the
// snapshot and publishes a change event. Returns any error from persistence.
// fn must not call back into the store.
func (h Handle[...]) Mutate(fn func(*T)) error

// MutateWhen is like Mutate but fn returns (changed bool, err error).
// If changed is false, no persistence or notification happens.
func (h Handle[...]) MutateWhen(fn func(*T) (bool, error)) error

// Delete removes the entity from the store and from storage.
func (h Handle[...]) Delete() error
```

### Why `MutateWhen`?

Some mutations are conditional — e.g. a heartbeat touch should not trigger a
frontend pubsub event or a dep-graph notification. Callers return `(false, nil)`
to suppress persistence and notification for that call.

### Why no `Get() T`?

Returning a naked `T` is unsafe when `T` contains slices or pointers, because the
caller can hold a reference and mutate without the lock. `Read(fn func(*T))` passes
a pointer under lock so the compiler can't smuggle it out through the return value.
For callers that genuinely need a snapshot (e.g. API serialisation), the store
exposes `Snapshot() S` on the handle, which calls `T.Snapshot()` under the read
lock and returns the safe copy.

---

## Persistence

### `TypedStorage[S]` interface

```go
type TypedStorage[S any] interface {
    Save(s S) error
    Load(id string) (S, error)
    Delete(id string) error
    List() ([]S, error)
}
```

This replaces the seven ad-hoc storage interfaces. Each entity type provides an
adapter that wraps the existing `JSONFileStorage` methods. Crucially, `Save` and
`Load` deal in snapshot types `S` (value-safe, JSON-serialisable), never in the
live `T`. The store calls `h.entity.Snapshot()` under lock to get `S`, then calls
`storage.Save(s)` outside the lock — so storage I/O never holds the entity lock.

### Lazy loading

`Store.Get(id)` checks the in-memory map first. On a miss it calls
`storage.Load(id)`, reconstructs a live `T` from the snapshot via a
`FromSnapshot(S) *T` factory registered at store construction, and inserts it
into the map. All of this happens under the store's map-level lock (short
critical section — just the map insert, not the I/O). Concurrent `Get` calls
for the same id on a cold store: the first acquires the map lock, loads, inserts,
releases; the second acquires, finds the entity, returns. No double-load.

```go
func (s *Store[T, S]) Get(id string) (Handle[T, S], error)
func (s *Store[T, S]) Create(id string, initial T) (Handle[T, S], error)
func (s *Store[T, S]) List() ([]Handle[T, S], error) // loads all from storage if not cached
```

---

## Dependency tracking and wakeup

The dep/revdep graph moves inside the store. Every `Handle` can declare
dependencies on other handles (in the same or different stores, because deps are
identified by `(kind, id)` string pairs, the same scheme as `toolcall.Dependency`).

```go
// Watch registers that this handle is waiting on deps.
// When all deps reach a done state, WakeC() is closed on this handle.
// Returns ErrCyclicDependency if adding these edges would create a cycle.
func (h Handle[...]) Watch(deps []Dep) error

// MarkDone declares this handle's current state done, notifying all
// reverse-dep waiters whose full dep set is now satisfied.
// Typically called inside Mutate:
//   h.Mutate(func(e *Eval) { e.State = EvalStateDone; h.MarkDone() })
// (MarkDone is safe to call from inside Mutate's fn — it enqueues the
// notification but defers firing until after the lock is released.)
func (h Handle[...]) MarkDone()

// Unwatch removes all dep registrations for this handle without firing
// notifications. Used when a session or eval is cancelled mid-wait.
func (h Handle[...]) Unwatch()
```

### Why inside the store rather than a separate `WakeupRegistry`?

The `WakeupRegistry` is currently a parallel graph that must be kept in sync with
the entity states via `isTerminalDep` callbacks. Moving it inside the store means:

- Done-state detection is authoritative: the store already holds the live
  entity, so `isDone` is just `h.Read(func(e *T) { done = isDone(e) })`
- The dep index lives alongside the entity cache — no separate map to keep in sync
- `Watch` and the entity state mutation can be made atomic with respect to each
  other by enqueuing the dep-notification as part of `Mutate`'s post-lock work

### Dep notification ordering guarantee

The critical race in the current code: `Suspend` releases `m.mu`, writes to
storage, then calls `wakeup.Watch` — a concurrent `Complete` can fire `Notify`
before `Watch` is registered.

Under the new model, `Watch` and `Mutate` use the same per-entity lock.
The call sequence inside `flushPendingToolCalls` becomes:

```
sessionHandle.Watch(evalDeps)      // registers under session's lock
sessionHandle.Mutate(suspend)      // transitions state under same lock
evalHandle.Mutate(launch)          // starts handler; if it completes
                                   // synchronously, MarkDone() is enqueued
                                   // but fires after Mutate returns
```

`MarkDone()` called inside `Mutate`'s `fn` enqueues the notification. The store
fires it after the write-lock is released and after storage write completes.
`Watch` called on the session before `Mutate` on the eval means the watch is
already registered when the notification fires. No more race.

---

## Event publishing (frontend pubsub)

The store calls `bus.Publish(event)` after every successful `Mutate` (or not at
all if `MutateWhen` returned `changed = false`). The event carries the entity kind,
id, and snapshot.

### Subscriber check before snapshot

Calling `T.Snapshot()` is not free for large types (e.g. `domain.Session` with
hundreds of messages). The store checks `bus.HasSubscribers(entityKind, entityID)`
before calling `Snapshot()`. If there are no subscribers, the mutation still
persists to storage but skips the snapshot and publish. This is safe because the
frontend re-fetches state on reconnect via SSE replay.

```go
type EventBus interface {
    HasSubscribers(kind, id string) bool
    Publish(event EntityEvent)
}

type EntityEvent struct {
    Kind      string
    ID        string
    Snapshot  any   // nil if HasSubscribers was false
    Timestamp time.Time
}
```

The existing `EventBroadcaster` implements `EventBus` with a thin adapter.
`HasSubscribers` maps to `SessionSubscriberCount` for session entities.

---

## `RunHandle[T]` — entities with a running goroutine

Some entities own a live process (a provider session, an eval handler goroutine).
These need:

1. All the persistence/dep/notification properties of `Handle`
2. A goroutine whose lifetime is tied to the entity
3. Cancel forwarding when the entity is stopped

`RunHandle` is obtained exclusively from `ActiveStore.Create` or
`ActiveStore.Load` — the goroutine body is provided at construction time, so
there is no separate "start" step and no window where the handle exists but the
goroutine has not been launched.

```go
// ActiveStore is a Store whose entities each own a goroutine.
// Use this instead of Store when the entity's lifecycle requires a long-running
// background process (provider I/O, event loop, etc.).
type ActiveStore[T Snapshot[S], S any] struct { ... }

// NewActiveStore constructs an ActiveStore. The body factory is called once per
// entity at creation (and once on recovery at restart) to produce the goroutine
// body for that entity.
func NewActiveStore[T Snapshot[S], S any](
    storage     TypedStorage[S],
    bus         EventBus,
    makeBody    func(h Handle[T, S]) func(ctx context.Context),
    opts        StoreOptions,
) *ActiveStore[T, S]

// Create inserts a new entity and immediately starts its goroutine.
// Returns a RunHandle whose goroutine is already running.
func (s *ActiveStore[T, S]) Create(id string, initial T) (RunHandle[T, S], error)

// Load retrieves an entity from storage (or the in-memory cache) and
// starts its goroutine if it is not already running.
func (s *ActiveStore[T, S]) Load(id string) (RunHandle[T, S], error)

// RunHandle is an opaque reference to an entity whose goroutine is running.
// Embed or hold this instead of Handle when you need lifecycle control.
// The underlying Handle is accessible via RunHandle.Handle for reads/mutations.
type RunHandle[T Snapshot[S], S any] struct {
    Handle[T, S]
    // unexported: ctx, cancel, done
}

// Stop cancels the entity's context, causing the goroutine's ctx.Done() to fire.
// The store also walks the dep revdep index: any handle that declared a dep on
// this handle is also stopped (so eval goroutines whose session is stopped are
// also stopped).
func (h RunHandle[...]) Stop()

// Wait blocks until the goroutine exits or ctx is cancelled.
func (h RunHandle[...]) Wait(ctx context.Context) error
```

### Why the body factory lives on the store, not the handle

Providing the body at `Create`/`Load` time rather than as a method call on the
returned handle has two benefits:

1. **No two-step mistake**: there is no `RunHandle` in existence before the
   goroutine starts. Callers cannot forget to call `Spawn` or call it twice.
2. **Recovery is automatic**: `ActiveStore.Load` (called by `OnRestart`) uses
   the same `makeBody` factory to restart goroutines for recovered entities —
   no special recovery code path.

The factory signature `func(h Handle[T, S]) func(ctx context.Context)` lets
the store capture the handle at creation time and inject it into the body
closure, so the body always operates on the right entity without needing to
thread `h` through the call.

### Design rationale

There is no command channel because none is needed. The per-entity lock already
serialises concurrent writers — the goroutine's calls to `Mutate` are
serialised with any external caller's calls to `Mutate` by the same lock.
"Delivering work to the goroutine" is not a requirement: the goroutine drives
the process (provider I/O, event loop), and it calls `Mutate` when state
changes. External callers that need to change entity state call `Mutate`
directly — they do not need to go through the goroutine.

The only reason to route through a channel would be if the goroutine held private
in-memory state that only it can access. In this codebase, all entity state
that matters is persisted through `Handle.Mutate`. The goroutine's local
variables (the in-flight `*Run`, the event channel) are transient and not shared.
If the goroutine needs to be re-entered (e.g. resume after tool results), it
simply loops:

```go
sessions := entity.NewActiveStore(storage, bus, func(h entity.Handle[Session, SessionSnapshot]) func(ctx context.Context) {
    return func(ctx context.Context) {
        for {
            run, err := startRun(ctx, h)
            if err != nil || ctx.Err() != nil {
                h.Mutate(func(s *Session) { s.State = StateFailed })
                return
            }
            suspended, err := driveRun(ctx, h, run)
            if err != nil || !suspended {
                return
            }
            // Session suspended waiting on tool evals.
            // Watch was registered inside driveRun.
            // Block until woken or stopped.
            select {
            case <-ctx.Done():
                return
            case <-h.WakeC(): // closed by the dep graph when all deps are done
                // loop: startRun will pick up resume token from state
            }
        }
    }
}, opts)
```

This is the same loop that `execution_coordinator.go` runs today, just with the
locking and lifecycle owned by the store instead of scattered across
`executor.go` and `eval_manager.go`.

### The goroutine wrapping contract

```
External callers:            session goroutine:
  h.Mutate(fn)   ──lock──►  h.Mutate(fn)
                               │
                     store lock → storage → bus

Dep completion (WakeC):
  evalHandle.MarkDone() fires → WakeC closed → goroutine loop resumes → driveRun(...)
```

No messages transit between external callers and the goroutine. The lock is the
only coordination primitive.

### `WakeC()` — blocking the goroutine on deps

```go
// WakeC returns a channel that is closed exactly once when all of this
// handle's registered deps become done. It is reset each time Watch is called.
// WakeC is only meaningful inside an ActiveStore goroutine body;
// calling it from outside returns a closed channel.
func (h Handle[...]) WakeC() <-chan struct{}
```

The session goroutine calls `h.Watch(evalDeps)` before suspending, then
blocks on `h.WakeC()`. When all evals complete (their `MarkDone()` fires), the
store closes the channel and the goroutine resumes — no callback, no separate
`OnSessionWake` function in `EvalManager`.

### Stop forwarding

When `AgentExecutor.StopSession` is called:

1. `sessionHandle.Stop()` cancels the entity's context
2. The session goroutine's `ctx.Done()` fires; it exits `driveRun`, stops the
   provider, and returns from the body
3. The store walks the dep revdep index: any handle that declared a dep on this
   session is also stopped (its `RunHandle.Stop()` is called)
4. Eval goroutines whose `ctx` is derived from (or forwarded by) the session
   context also exit

This replaces the per-handle `evalHandle.OnCancel` registration: the dep graph
already knows which evals are waiting on the session, so stop forwarding is
automatic.

---

## Restart recovery and wakeup

On restart, two classes of entities need special handling:

### Class 1: Entities interrupted mid-run (sessions, run attempts)

The current `recoveryManager` walks all sessions, finds open run attempts
(attempts with no `EndedAt`), marks them interrupted, and appends a recovery
message. This logic belongs in a `Store.OnRestart` hook:

```go
type RestartHook[T Snapshot[S], S any] func(h Handle[T, S]) error

func (s *Store[T, S]) OnRestart(ctx context.Context, hook RestartHook[T, S]) error
```

Called once at startup, it iterates all persisted entities (loading lazily from
storage, not into full memory) and calls `hook` for each. The session store's hook
replicates what `recoveryManager.OnStartup` does today, but without needing to
call back into `AgentExecutor`.

### Class 2: Entities suspended waiting on deps (evals, sessions with tool calls)

An eval that was `EvalStateSuspended` at crash time needs its deps re-evaluated on
restart. The store rebuilds the dep graph from persisted state by calling
`T.Deps() []Dep` (an optional interface on `T`) during lazy load. If all deps are
already done (loaded from their own stores), `WakeC()` fires immediately.
If some are not done and have their own goroutines (e.g. another eval
that is also being recovered), the graph unwinds naturally as they complete.

```go
// DepSource is an optional interface for entities that declare dependencies.
// If T implements DepSource, the store calls Deps() after loading and
// re-registers the wakeup watch automatically.
type DepSource interface {
    Deps() []Dep
    IsDone() bool
}
```

This replaces `isTerminalDep` in `EvalManager` and the scattered
`NotifySessionTerminal` calls — the store knows if an entity is done because
it loaded it.

---

## What replaces `WakeupRegistry`

The `inMemoryWakeupRegistry` becomes internal to the store, not a separately
injected interface. The store maintains:

```
deps    map[string][]Dep    // entity id → deps it is waiting on
revdeps map[string][]string // dep (kind:id) → entity ids watching it
```

Both maps are protected by a single `depMu sync.RWMutex` on the store (separate
from the per-entity locks). `Watch` writes to both maps. `MarkDone` (called
from `Mutate`'s post-lock phase) reads `revdeps` and closes the `WakeC` channel
for any waiter whose full dep set is now satisfied. Cycle detection (DFS) runs
under `depMu` during `Watch`.

The key improvement over `WakeupRegistry`: because `MarkDone` is always called
from the post-lock phase of `Mutate`, it is guaranteed to fire *after* the storage
write that put the entity into done state completes. The notify-before-save
race is structurally impossible.

---

## What replaces `EvalManager`

With `Store[Eval, EvalSnapshot]` carrying persistence, dep tracking, and wakeup,
`EvalManager` reduces to a thin coordinator:

```go
type EvalCoordinator struct {
    evals    *entity.ActiveStore[Eval, EvalSnapshot]
    tools    tools.Registry
    sessions *entity.ActiveStore[Session, SessionSnapshot] // for wakeup/stop
}

func (c *EvalCoordinator) Dispatch(ctx context.Context, opts DispatchOptions) (entity.RunHandle[Eval, EvalSnapshot], error) {
    // Create inserts the entity and starts the goroutine in one step.
    // The body was registered with the store at construction time via makeBody.
    return c.evals.Create(newEvalID(), Eval{...})
}
```

`OnSessionWake` disappears: the session goroutine blocks on `sessionHandle.WakeC()`
after registering its eval deps with `Watch`. When all eval handles call
`MarkDone()` inside their final `Mutate`, the store closes `WakeC` and the session
goroutine resumes — no callback, no circular dependency from `EvalManager` back
into `AgentExecutor`.

---

## Migration path

The design is additive. Nothing in the current codebase needs to change before
the first entity type is migrated. Suggested order:

1. **`RunAttemptMetadata`** — simplest, no deps, no goroutine, only used by
   `AgentExecutor`. Proves the `Store[T, S]` + `TypedStorage[S]` plumbing works.

2. **`toolcall.Eval`** — has deps, no goroutine of its own (handler goroutine is
   external). Proves dep tracking and wakeup. Replaces `EvalManager` internals.

3. **`domain.Session`** (state + messages only, not the run) — has deps (on evals),
   may have subscribers. Proves the pubsub subscriber-check optimisation.

4. **`sessionContext`** (the full session + run + attempt) — adds `ActiveStore`
   with `RunHandle` + `WakeC`. Replaces the largest tangle of locks in `AgentExecutor`.
   The `AgentExecutor` goroutine-per-session becomes a body registered with the
   `ActiveStore` that loops on `WakeC` instead of `OnSessionWake` callbacks.

5. Remaining small types (`ResumeToken`, `RunAttempt`) fold in once the session
   store exists, since they are logically owned by sessions.

At each step the existing storage implementations are wrapped behind
`TypedStorage[S]` adapters — no storage rewrite required.

---

## Open questions

1. **Cross-store atomic operations**: is there any case where we truly need to
   mutate a Session and an Eval atomically (not just in sequence)? Current analysis
   says no — the bugs are ordering issues, not atomicity issues. If this changes,
   a two-phase commit protocol over two store locks is possible but should be
   avoided.

2. **`MutateWhen` granularity**: should the decision to publish to the frontend bus
   be caller-specified per `Mutate` call (via an option), or inferred from whether
   the entity state actually changed? The latter is cleaner but requires the store
   to compare snapshots, which may be expensive for large types.

3. **Per-store or global dep graph**: deps currently cross type boundaries (a
   session waits on evals, an eval waits on another session). Should the dep graph
   live in a single global registry (typed by `(kind, id)`) rather than per-store?
   A global registry is simpler for cross-type deps but reintroduces a global lock.
   The per-store approach with `(kind, id)` string keys delegates done-state
   lookup to a registered `IsDone(kind, id string) bool` callback, avoiding
   the global lock while supporting cross-type deps.

4. **Eval handler goroutine ownership**: the eval `RunHandle` owns the goroutine
   via the `ActiveStore`'s `makeBody` factory. The coordinator creates the entity
   with `Create`. Stopping the handle (via `Stop()` forwarded from the session)
   cancels the goroutine's context — handle lifecycle equals goroutine
   lifetime, which is the right behaviour.

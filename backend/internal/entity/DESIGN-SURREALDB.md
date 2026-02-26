# Alternate Proposal: SurrealDB as the persistence and event layer

This document evaluates replacing the seven hand-rolled JSON-file storage backends
and the `EventBroadcaster` / `WakeupRegistry` machinery with SurrealDB, and gives
an honest assessment of where it helps, where it does not, and what it cannot do
at all today.

---

## What SurrealDB actually offers

SurrealDB is a document/graph database with a SQL-like query language (SurrealQL).
Relevant features for this codebase:

- **Graph relations**: first-class `RELATE` edges between records, so
  `session -> eval` dependencies can be stored as `session:x ->waited_on-> eval:y`
  and queried with graph traversal
- **LIVE SELECT**: push notifications to connected clients when records change;
  the Go SDK receives these as a channel of `LiveNotification` values
- **Transactions**: `BEGIN/COMMIT` across multiple record writes in one atomic unit
- **SurrealKV on-disk storage**: single-node file-backed persistence (beta as of
  v3.x, labelled "beta" in their docs)
- **Embedded mode**: available in Rust, JavaScript, Python, and .NET — **not Go**

The last point is the most important one for this codebase.

---

## The Go embedding gap

**SurrealDB cannot be embedded in a Go binary.** The embedding docs list Rust,
JavaScript (via WASM), Python, and .NET. The Go SDK connects to SurrealDB over
WebSocket or HTTP only — it requires a separate running SurrealDB process.

That means replacing the current `JSONFileStorage` with SurrealDB would require:

1. Shipping a SurrealDB binary alongside the orbitmesh binary (or as a sidecar)
2. Starting and supervising that process at runtime
3. Handling the case where SurrealDB is not installed / fails to start
4. Accepting a WebSocket round-trip (localhost, but still) for every persistence
   operation that is currently a file write

This is not a theoretical concern. It is the primary operational difference between
"SQLite-style embedded database" and "SurrealDB". For a tool that currently
installs as a single binary with no dependencies, this is a significant regression
in deployment simplicity.

Alternatives within the SurrealDB ecosystem that do embed (Rust via FFI, WASM)
add CGO or WASM runtime dependencies that introduce their own complexity.

---

## What SurrealDB replaces well

If the deployment constraint is accepted (i.e. SurrealDB runs as a sidecar or
is pre-installed), the following current code collapses entirely:

### Storage layer

All seven `TypedStorage` implementations become SurrealQL UPSERT/SELECT/DELETE
calls. The schema for the entities this codebase needs:

```surql
DEFINE TABLE session SCHEMAFULL;
DEFINE FIELD state        ON session TYPE string;
DEFINE FIELD provider     ON session TYPE string;
DEFINE FIELD messages     ON session TYPE array;
DEFINE FIELD created_at   ON session TYPE datetime;
DEFINE FIELD updated_at   ON session TYPE datetime;

DEFINE TABLE eval SCHEMAFULL;
DEFINE FIELD tool_name    ON eval TYPE string;
DEFINE FIELD state        ON eval TYPE string;
DEFINE FIELD input        ON eval TYPE object;
DEFINE FIELD result       ON eval TYPE option<string>;
DEFINE FIELD error        ON eval TYPE option<string>;
DEFINE FIELD created_at   ON eval TYPE datetime;
DEFINE FIELD updated_at   ON eval TYPE datetime;

DEFINE TABLE run_attempt SCHEMAFULL;
-- ... etc

-- Dependencies as graph edges:
DEFINE TABLE waited_on SCHEMAFULL TYPE RELATION;
-- session:x ->waited_on-> eval:y
```

Querying "which evals is this session waiting on" becomes:
```surql
SELECT ->waited_on->eval FROM session:abc;
```

This is genuinely simpler than the parallel `sessionDeps` map + `WakeupRegistry`
dep graph. The graph is the storage; there is no secondary index to keep in sync.

### Dep-graph wakeup

SurrealDB `DEFINE EVENT` can fire SurrealQL on record change:

```surql
DEFINE EVENT eval_completed ON TABLE eval WHEN
    $before.state != $after.state
    AND ($after.state = 'done' OR $after.state = 'error')
THEN {
    -- find sessions waiting on this eval and mark them ready
    LET $waiting = SELECT <-waited_on<-session FROM eval WHERE id = $after.id;
    -- check if ALL their deps are terminal
    FOR $s IN $waiting {
        LET $pending = SELECT count() FROM ->waited_on->eval
            WHERE state NOT IN ['done', 'error'] GROUP ALL;
        IF $pending[0].count = 0 {
            UPDATE $s SET ready_to_resume = true;
        };
    };
};
```

This pushes the wakeup logic into the database, eliminating `WakeupRegistry`
entirely. The Go side registers a `LIVE SELECT` on sessions where
`ready_to_resume = true` and reacts to those notifications.

### Frontend pubsub (EventBroadcaster replacement)

The existing `EventBroadcaster` — with its in-memory subscriber map, history
buffer, and SSE replay — could be partially replaced by SurrealDB `LIVE SELECT`:

```surql
LIVE SELECT * FROM session WHERE id = 'abc';
```

However this is only a partial replacement:
- `LIVE SELECT` delivers whole-record diffs, not the domain `Event` stream
  (output deltas, thought events, tool call events) that the frontend currently
  consumes. Those events are not stored in the session record; they are emitted
  ephemerally as the provider runs.
- The current SSE replay (delivering buffered events to a reconnecting client) 
  would need to be reimplemented via a separate `events` table that accumulates
  the ephemeral stream, which is write-heavy and would need TTL cleanup.
- `LIVE SELECT` ordering consistency is explicitly documented as best-effort for
  multi-client scenarios ("some messages may be received out of order").

**Verdict**: SurrealDB live queries can replace state-change notifications
(session state, eval completion) but not the high-frequency ephemeral event
stream from provider runs. `EventBroadcaster` stays for that stream.

---

## What SurrealDB does not help with

### In-memory caching / lazy loading

SurrealDB is a remote process. Every `Get(id)` call that misses the in-memory
cache now incurs a network round-trip (localhost, ~0.1–1ms) instead of a file
read (~0.01ms). For the current workload this is probably fine, but the lazy-
loading cache layer from the `entity` design is still required in front of
SurrealDB — you do not want to query the database on every read of a session's
current state.

The net result: the `Store[T,S]` in-memory cache layer is still needed; SurrealDB
replaces the file-I/O tier beneath it, not the caching tier.

### Locking and mutation serialization

SurrealDB has transactions, but they are optimistic and operate at the database
level. They do not replace the need to serialize mutations to a single in-memory
entity from multiple goroutines. The `Handle.Mutate` pattern (or equivalent) is
still required in application code to prevent two goroutines from racing to update
the same session record simultaneously.

SurrealDB transactions are useful for cross-entity atomicity (writing the session
state + creating the eval + inserting the dep edge in one commit), which is a
genuine improvement over the current code. But they are not a substitute for the
in-process locking that prevents concurrent mutation of the same in-memory object.

### Context propagation and goroutine lifecycle

SurrealDB has no concept of a running goroutine, a cancelled context, or a
suspended session that needs to be woken. The `ActiveHandle` concern (routing
`resumeSessionWithToolResults` through the session's own goroutine rather than
an arbitrary tool goroutine) is entirely an application-level concern. SurrealDB
does not help here.

### Recovery / restart wakeup

The current `recoveryManager` walks persisted sessions and open run attempts on
startup. With SurrealDB this becomes a query:

```surql
SELECT * FROM run_attempt WHERE ended_at IS NONE;
```

This is genuinely simpler. However, the logic of "re-register dep watches for
suspended evals" is still application code — SurrealDB stores the graph edges
but does not automatically re-fire `DEFINE EVENT` triggers on startup for
pre-existing state.

---

## Schema design for this codebase

```surql
-- Sessions
DEFINE TABLE session SCHEMAFULL;
DEFINE FIELD state             ON session TYPE string;
DEFINE FIELD provider_type     ON session TYPE string;
DEFINE FIELD working_dir       ON session TYPE string;
DEFINE FIELD project_id        ON session TYPE option<string>;
DEFINE FIELD agent_id          ON session TYPE option<string>;
DEFINE FIELD messages          ON session TYPE array;  -- append-only log
DEFINE FIELD suspension_ctx    ON session TYPE option<object>;
DEFINE FIELD ready_to_resume   ON session TYPE bool DEFAULT false;
DEFINE FIELD created_at        ON session TYPE datetime DEFAULT time::now();
DEFINE FIELD updated_at        ON session TYPE datetime DEFAULT time::now();

-- Evals
DEFINE TABLE eval SCHEMAFULL;
DEFINE FIELD tool_name         ON eval TYPE string;
DEFINE FIELD session_id        ON eval TYPE record<session>;
DEFINE FIELD state             ON eval TYPE string;
DEFINE FIELD input             ON eval TYPE object;
DEFINE FIELD result            ON eval TYPE option<string>;
DEFINE FIELD error             ON eval TYPE option<string>;
DEFINE FIELD handler_state     ON eval TYPE option<object>;
DEFINE FIELD provider_call_id  ON eval TYPE option<string>;
DEFINE FIELD created_at        ON eval TYPE datetime DEFAULT time::now();
DEFINE FIELD updated_at        ON eval TYPE datetime DEFAULT time::now();

-- Run attempts
DEFINE TABLE run_attempt SCHEMAFULL;
DEFINE FIELD session_id        ON run_attempt TYPE record<session>;
DEFINE FIELD provider_type     ON run_attempt TYPE string;
DEFINE FIELD started_at        ON run_attempt TYPE datetime;
DEFINE FIELD ended_at          ON run_attempt TYPE option<datetime>;
DEFINE FIELD terminal_reason   ON run_attempt TYPE option<string>;
DEFINE FIELD wait_kind         ON run_attempt TYPE option<string>;
DEFINE FIELD wait_ref          ON run_attempt TYPE option<string>;
DEFINE FIELD heartbeat_at      ON run_attempt TYPE datetime;
DEFINE FIELD boot_id           ON run_attempt TYPE string;

-- Resume tokens  
DEFINE TABLE resume_token SCHEMAFULL;
DEFINE FIELD session_id        ON resume_token TYPE record<session>;
DEFINE FIELD attempt_id        ON resume_token TYPE record<run_attempt>;
DEFINE FIELD expires_at        ON resume_token TYPE datetime;
DEFINE FIELD consumed_at       ON resume_token TYPE option<datetime>;
DEFINE FIELD revoked_at        ON resume_token TYPE option<datetime>;

-- Dependency graph (graph edges)
DEFINE TABLE waited_on SCHEMAFULL TYPE RELATION
    FROM session | eval
    TO eval;

-- Wakeup event (fires when eval reaches terminal state)
DEFINE EVENT eval_terminal ON TABLE eval
    WHEN $before.state NOT IN ['done', 'error']
      AND $after.state IN ['done', 'error']
THEN {
    LET $waiters = SELECT <-waited_on<-session AS id FROM $after.id;
    FOR $waiter IN $waiters.id {
        LET $pending = (
            SELECT count() AS n FROM ->waited_on->eval
            WHERE id = $waiter AND state NOT IN ['done', 'error']
        )[0].n;
        IF $pending = 0 {
            UPDATE $waiter SET ready_to_resume = true, updated_at = time::now();
        };
    };
};
```

---

## Go integration shape

```go
// db is a *surrealdb.DB connected via WebSocket to the sidecar process.

// Upsert a session after mutation (replaces JSONFileStorage.Save):
_, err = surrealdb.Upsert[SessionRecord](db, models.RecordID{Table: "session", ID: id}, snap)

// Atomic suspend + dep registration (replaces the PrepareDispatches race):
_, err = db.Query(`
    BEGIN TRANSACTION;
    UPDATE $session SET state = 'suspended', suspension_ctx = $ctx, updated_at = time::now();
    FOR $eval_id IN $eval_ids {
        RELATE $session->waited_on->$eval_id;
    };
    COMMIT TRANSACTION;
`, map[string]any{
    "session":  models.RecordID{Table: "session", ID: sessionID},
    "eval_ids": evalIDs,
    "ctx":      suspCtx,
})

// Live query for sessions that are ready to resume:
liveID, err := surrealdb.Live[SessionRecord](db, models.Table("session"))
notifs, err := surrealdb.LiveNotifications[SessionRecord](db, liveID)
go func() {
    for notif := range notifs {
        if notif.Action == surrealdb.Update && notif.Result.ReadyToResume {
            executor.resumeSessionWithToolResults(notif.Result.ID, ...)
        }
    }
}()
```

The atomic `BEGIN/COMMIT` wrapping session suspend + dep edge creation is the
most concrete improvement over the current code: the race between `suspendSession`
and an eval completing before `wakeup.Watch` is registered goes away because the
dep edge and the suspended state are written in one transaction.

---

## Honest comparison

| Concern | Entity/Store design | SurrealDB sidecar |
|---|---|---|
| Deployment complexity | Single binary, no deps | Requires SurrealDB binary |
| Storage boilerplate | Eliminated by `TypedStorage[S]` | Eliminated by SurrealQL |
| In-memory caching | Built into `Store[T]` | Still needed (same `Store[T]`) |
| Mutation serialization | `Handle.Mutate` lock | Still needed (same) |
| Dep graph | In-store dep index | Graph edges + DEFINE EVENT |
| Atomic suspend + dep | Sequenced (still a protocol) | Single transaction (better) |
| Frontend events | `EventBroadcaster` (stays) | `EventBroadcaster` (stays) |
| Restart recovery | `OnRestart` hook | Simple query + app logic |
| ActiveHandle / goroutines | Application layer | Application layer (unchanged) |
| Go embedding | N/A | **Not available** |
| LIVE SELECT ordering | N/A | Best-effort (documented caveat) |
| Maturity (Go SDK) | N/A | v1.3.0, 303 stars, beta status on some backends |

---

## Recommendation

SurrealDB does not make sense as a replacement **right now** for this codebase,
primarily because:

1. **No Go embedding.** Requiring a sidecar process is a deployment regression
   for a single-binary tool. This is a hard constraint, not a preference.

2. **The caching layer is still required anyway.** Because SurrealDB is a remote
   process, the `Store[T]` in-memory cache described in DESIGN.md is still needed
   in front of it. The entity design eliminates the lock/store/notify boilerplate
   regardless of what sits beneath; SurrealDB only changes what `TypedStorage[S]`
   calls into.

3. **Go SDK maturity.** 303 stars, no embedded mode, SurrealKV marked beta —
   this is not yet the kind of dependency you want load-bearing in production.

**Where SurrealDB does add real value** is the atomic transaction for the
suspend + dep registration, and the graph-native dep edges replacing the parallel
`WakeupRegistry` map. Both of these benefits can be captured today with SQLite
(via `modernc.org/sqlite`, which is pure Go, no CGO) as the `TypedStorage[S]`
backend: SQLite gives you transactions and foreign-key graph queries without a
sidecar, and the entity design makes it straightforward to swap the backend later
if SurrealDB gains Go embedding support.

**The right path**: implement the `entity` design with the file-based storage as
the initial backend (least disruption, already tested), then evaluate replacing
`TypedStorage[S]` with SQLite to get transactions for the atomic suspend+dep case.
Revisit SurrealDB when Go embedding exists or if the deployment model changes to
accept a sidecar (e.g. if orbitmesh gains a server mode with a persistent process).

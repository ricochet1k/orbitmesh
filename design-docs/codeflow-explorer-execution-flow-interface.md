# CodeFlow Explorer — Goroutine and Execution Flow Interface

**Status**: Proposed  
**Date**: 2026-02-22  
**Area**: UI / Analysis Views — Execution Flow Lens

---

## 1. The Concurrency Model

The execution flow view models Go concurrency as a **static actor graph** derived from SSA (Static Single Assignment) form via `golang.org/x/tools/go/ssa` and its callgraph construction (`rta`, `cha`, or `vta` depending on precision budget). Goroutines are first-class nodes — not callstack frames, not log lines, but persistent actors with typed communication edges and shared resource dependencies.

**What the model represents:**

- **Goroutines** as stateful actors with an identity derived from their spawn site (`go` statement location + enclosing function). Each goroutine carries a set of channels it reads or writes, mutexes it may acquire, and a modeled lifetime.
- **Channels** as typed directed edges between producer and consumer goroutines. Channel type (element type, direction, buffer capacity) is fully known statically when the channel is created with a literal `make(chan T, n)`.
- **Mutexes and RWMutexes** as shared resource nodes. The analysis tracks `Lock`/`Unlock`/`RLock`/`RUnlock` calls as entry and exit events bracketing a protected code region.
- **WaitGroups** as synchronization barriers: `Add(n)`, `Done()`, and `Wait()` are matched symbolically when they appear in the same function scope or can be resolved via interprocedural analysis.
- **`select` statements** as multiplexing fan-in points where multiple channel operations compete; modeled as a set of guarded edges from the select node to each case's continuation.

**What cannot be known statically and how uncertainty is communicated:**

| Construct | Limitation | Visual Signal |
|-----------|-----------|---------------|
| Channel passed as `interface{}` or via reflection | Element type unknown | Edge labeled `?T`, orange dashed stroke |
| `sync.WaitGroup.Add(n)` where `n` is dynamic | Counter bound unknown | WaitGroup node annotated `±n`, amber halo |
| Goroutine spawned inside an external library call | Goroutine identity unknown | Ghost node: hollow circle, label "external goroutine" |
| `make(chan T)` stored in a struct field then distributed | Channel aliasing | Dotted edge with a union label listing possible aliases |
| Mutex stored in a global or passed by pointer | Lock set approximation | Lock region shown with hatched overlay indicating over-approximation |

Uncertainty is not hidden. Every edge and node in the graph has a **confidence band** (certain / probable / possible / unknown) rendered as stroke solidity (solid → dashed → dotted → gray ghost). The legend is permanently anchored in the bottom-left of the canvas.

---

## 2. Goroutine Spawn Tree View

The spawn tree is a **rooted hierarchy**, not a flat graph. The root is `main.main`'s implicit goroutine. Every `go` statement produces a child node connected to its parent by a spawn edge.

**Naming anonymous goroutines:** Anonymous functions passed to `go` are named by concatenating the enclosing named function and the `go` statement's line number: `processRequests:142`. When a named function is passed directly (`go worker(ch)`), the function name is used verbatim. If a closure captures variables, the captured variable names are listed in a tooltip as `captures: [ctx, wg, ch]`.

**Spawn edge styles:**

- **Solid amber arrow**: goroutine is always spawned on this code path.
- **Dashed amber arrow**: goroutine is spawned conditionally (inside an `if`, `switch`, or early-return path). A condition summary label appears on the edge: `if err == nil`.
- **Fan-out arrow with loop badge**: goroutine spawned inside a `for` loop. The edge carries a badge `×N` where N is either the static bound (if the loop range is over a literal slice) or `×?` for unknown iteration count. A loop annotation box wraps the spawn site with a dotted boundary.

**Layout:** The tree is rendered top-down. A goroutine with many children fans out horizontally. Depth beyond four levels collapses into a summary node ("8 descendants") expandable on click. The tree layout is separate from the main canvas but linked to it — selecting a node in the tree centers the canvas execution flow view on that goroutine's subgraph.

---

## 3. Channel Topology View

Channels are directed edges connecting goroutine nodes. Each edge carries:

- **Type label**: the channel's element type (`chan *http.Request`, `chan struct{}`).
- **Buffer annotation**: `buf=0` for unbuffered, `buf=N` for buffered, `buf=?` for dynamic `make(chan T, n)` where `n` is a variable.
- **Direction markers**: a filled arrowhead at the receiving end, an open circle at the sending end. Bidirectional channels (neither sent to nor received from exclusively by one goroutine) use bidirectional arrows.

**`select` statements as fan-in nodes:** A `select` statement is rendered as a small diamond node interposed on the goroutine's incoming edges. Each `case` branch exits the diamond as a labeled outgoing edge to the continuation code region. The `default` case exits as a dashed edge labeled `default`. `time.After(d)` cases exit as a dashed edge with a clock icon and the duration expression as a label.

**Closed channel detection:** The analysis tracks `close(ch)` call sites. If a `close` is reachable on a code path where a subsequent `send` to the same channel is also reachable, the send edge is highlighted in red with a `SEND-AFTER-CLOSE` anti-pattern badge. If a channel is never closed but has consumers that block indefinitely on receive, the consumer goroutine is flagged for potential leak.

**Mismatched producer/consumer counts** are annotated on the channel edge: `1 sender → 3 receivers` (load-balanced fan-out, valid) vs `3 senders → 1 receiver` (fan-in, valid but contention risk) vs `1 sender → 0 receivers` (channel never consumed, leak risk, red edge).

---

## 4. Mutex/Lock Coverage Map

Functions are nodes in this view, colored by which locks they execute under. The color assignment is per-lock and composited for intersections:

- No lock held: **slate** (default).
- Under Lock A only: **blue**.
- Under Lock B only: **green**.
- Under Lock A and Lock B simultaneously: **teal** (additive blend, computed per lock pair).
- Under three or more locks: **purple** with a numeric badge showing lock count.

Color is computed by interprocedural analysis of lock regions: if `mu.Lock()` is called in function F and `mu.Unlock()` is called after a call to G, then G's node is colored for Lock A even though G itself does not call Lock.

**Lock order graph:** A secondary miniature graph in a collapsible panel shows the **partial order of lock acquisitions** across all goroutines. Nodes are mutex identities; a directed edge from A → B means "A is acquired while B is not yet held, then B is acquired." A cycle in this graph is a **deadlock risk** and is rendered in red with a `LOCK-CYCLE` badge. The cycle is not necessarily a proven deadlock — it is a possible acquisition order observable by the static analysis.

**Side panel — acquisition sequence:** Selecting a function node opens a side panel showing the full lock acquisition sequence for that function in each goroutine context that calls it. Each entry shows: goroutine identity, call chain leading to the function, locks held on entry, locks acquired inside, locks released on return. Multiple goroutine contexts for the same function are listed as tabs.

---

## 5. Blocking Point Analysis

Blocking points are identified as any SSA instruction or call that may suspend the goroutine indefinitely or for an unbounded duration:

| Blocking Construct | Marker Color | Notes |
|--------------------|-------------|-------|
| Unbuffered channel send/receive | Red | Blocks until partner is ready |
| Buffered channel send (full) | Orange | Blocks only when buffer is full; capacity annotated |
| `sync.Mutex.Lock()` | Orange | Blocks under contention |
| `sync.RWMutex.RLock()` / `Lock()` | Orange / Red | RLock blocks under write lock |
| `sync.WaitGroup.Wait()` | Yellow | Blocks until counter reaches zero |
| `time.Sleep(d)` | Yellow | Bounded block; duration shown |
| `syscall.*` / `os.*` I/O | Yellow | Unbounded I/O wait |
| `context.Done()` receive | Green | Controlled block; cancellable |

Markers render as colored circles on the goroutine node's border at the angular position corresponding to their order of encounter in the goroutine's execution path (clockwise from top = first blocking point encountered).

**Blocking depth metric:** The number of independent blocking points that must all resolve before the goroutine can return. Computed as the count of distinct blocking instructions on the goroutine's shortest termination path in the CFG. Displayed as a numeric label inside the goroutine node. Depth 0 = non-blocking. Depth ≥5 is highlighted as a complexity warning.

---

## 6. Lifetime and Leak Detection

A goroutine's modeled lifetime has three components: **spawn site**, **termination condition**, and **leak risk score**.

**Termination paths (known):**

- `return` or falling off the end of the goroutine function body.
- Receiving on `ctx.Done()` where `ctx` is a `context.Context` parameter or closure capture.
- A channel close cascading to a `range ch` loop exit.
- A `panic` followed by a `recover` in a deferred function (goroutine terminates, not crashes).

**Leak risk score** (0.0 – 1.0, displayed as a colored ring around the goroutine node):

| Factor | Weight |
|--------|--------|
| No `context.Context` parameter or capture | +0.30 |
| No timeout or `time.After` on any blocking receive | +0.25 |
| Receives from channel that is never closed | +0.25 |
| Spawned in a loop with no bounding condition | +0.15 |
| No `sync.WaitGroup.Done()` path | +0.05 |

Score ≤ 0.2: **green** ring (low risk). 0.2–0.6: **yellow** (moderate). > 0.6: **red** (high leak risk). Goroutines with score > 0.6 are also listed in the Anti-Pattern Findings table in the flat interface with severity `high`.

---

## 7. select Statement Visualization

`select` is rendered as a **fan-in diamond** node positioned on the goroutine's timeline. Each case branch exits the diamond as a labeled edge:

- **Channel receive case** (`case v := <-ch`): emerald edge labeled with channel name and element type.
- **Channel send case** (`case ch <- v`): emerald edge with a filled source circle.
- **`default` case**: dashed gray edge labeled `default` — indicates the select is non-blocking.
- **`time.After(d)` case**: dashed yellow edge with a clock icon; duration expression shown as edge label.
- **`ctx.Done()` case**: green edge with a checkmark; signals controlled termination.

**`select` inside a loop:** When a `select` is the body of a `for` loop, the diamond is wrapped in a loop annotation box. The iteration exit conditions are labeled on the loop boundary. If the only exit from the loop is a `ctx.Done()` case, the goroutine is annotated as **context-governed** (green lifetime). If there is no exit condition detectable, the loop is annotated `unbounded` and contributes +0.40 to the leak risk score regardless of other factors.

---

## 8. Critical Path View

The user selects a **source goroutine** and a **target goroutine** using a two-click workflow (click source, Shift-click target, or use the command palette `critical-path <A> → <B>`). The view then highlights the shortest communication path between them in the current channel topology graph.

The highlighted path shows:

- **Channels traversed** in order, with their buffer sizes and element types.
- **Mutexes crossed** on the path, with their lock colors from the coverage map.
- **Blocking points** on the path, rendered as their colored markers inline on the path.
- **Qualitative latency estimate** derived from blocking point count and type:
  - 0 blocking points: **fast** (direct memory access, no synchronization).
  - 1–2 blocking points, all bounded: **medium**.
  - 3+ blocking points or any unbounded block: **slow**.
  - Any block on a goroutine with leak risk > 0.6: **unbounded**.

The critical path is also exportable as a Markdown sequence diagram (compatible with Mermaid) for documentation.

---

## 9. Diff Mode

Diff mode compares two analysis snapshots — typically two git commits selected from a branch picker, or two saved analysis sessions. The diff is computed on the goroutine topology graph model, not on source text.

**Visual encoding:**

- **New goroutine** (present in HEAD, absent in base): node filled **green**, label prefixed with `+`.
- **Removed goroutine**: node shown as **red ghost** (hollow, dashed border), label prefixed with `−`.
- **Changed spawn condition**: spawn edge color shifts from amber to orange; a tooltip shows before/after condition expression.
- **Changed channel topology**: before-state edge shown as a thin gray stroke; after-state edge shown as the normal colored stroke. Both are visible simultaneously with a before/after legend.
- **Changed leak risk score**: the goroutine node's lifetime ring shows a gradient from old color (left half) to new color (right half). A delta badge (`Δ+0.3`) appears on the node.
- **Unchanged goroutines**: rendered at 60% opacity to reduce noise.

A **diff summary panel** lists counts of added, removed, and changed goroutines, channels, and mutex regions. Intended for AI system operators reviewing model-generated pull requests: the diff view surfaces goroutine topology regressions (a goroutine loses its `ctx.Done()` case, a new channel is added without a close path) that are invisible in a line-diff code review.

---

## 10. Integration with Flat Interface

The flat interface's **Goroutine Map** table (columns: `Spawn Site`, `Parent Goroutine`, `Lifetime Estimate`, `Channels Used`, `Mutexes Held`, `Leak Risk`) and the execution flow canvas share a **unified selection store** (Zustand, see canvas interface design doc §11).

**Bidirectional sync:**

- Hovering a row in the Goroutine Map table causes the corresponding goroutine node on the canvas to pulse with a 2px white highlight ring. No click required.
- Clicking a row selects it; the canvas pans and zooms to center the goroutine's subgraph, expanding it to L3 zoom if currently at L1 or L2.
- Clicking a goroutine node on the canvas scrolls the Goroutine Map table to the corresponding row and selects it.
- Filtering the Goroutine Map table by `Leak Risk` (e.g., showing only `high`) dims all non-matching goroutine nodes on the canvas to ghost opacity (0.08), consistent with the canvas filter model.

The flat interface's **Channel Inventory** table similarly syncs with channel edges: selecting a channel row highlights the corresponding edge on the canvas and opens its type/buffer tooltip inline.

The flat interface's **Mutex/Lock Coverage** table row selection opens the lock's coverage map overlay on the canvas, painting the affected function nodes with their lock-region colors and activating the lock order graph panel.

Both panels coexist in a split layout. The split ratio is user-configurable and remembered per session. Neither panel is modal; closing one does not affect the other's state.

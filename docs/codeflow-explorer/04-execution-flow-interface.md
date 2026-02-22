# CodeFlow Explorer — Goroutine and Execution Flow Interface

**Status:** Draft
**Audience:** Frontend engineers, concurrency auditors, performance engineers
**Scope:** Execution flow lens — goroutines, channels, mutexes, blocking analysis

---

## 1. The Concurrency Model

The execution flow lens models a Go program as a collection of concurrent actors communicating through typed channels and coordinating access to shared memory via mutexes. It is derived from static analysis (SSA) rather than runtime instrumentation.

**What is modeled:**
- **Goroutines** — stateful actors with a spawn site, closure captures, and a lifetime model
- **Channels** — typed communication edges with direction and buffer capacity
- **Mutexes and RWMutexes** — shared resources with acquisition order constraints
- **WaitGroups** — group synchronization with Add/Wait/Done relationship tracking
- **Select statements** — multi-channel multiplexing with case prioritization
- **Context propagation** — `context.Context` cancellation paths as goroutine lifetime signals

**What static analysis cannot know — and how uncertainty is communicated:**

| Construct | Limitation | Visual Representation |
|---|---|---|
| Goroutines spawned via `reflect` | Cannot enumerate statically | Warning badge on spawn site |
| Dynamic buffer sizes (`make(chan T, n)` where `n` is a variable) | Buffer size unknown | `?` annotation on channel edge |
| Channel aliasing through interfaces | Pointer analysis may be incomplete | Dashed edge (probable, not certain) |
| Goroutines spawned by external libraries | Invisible to analysis | Grey phantom node at library boundary |
| Runtime scheduler decisions | Non-deterministic by design | Not modeled; explicit disclaimer in view |

Uncertainty is communicated through edge stroke style: solid = certain, dashed = probable (pointer analysis resolved), dotted = possible (conservative over-approximation), ghost = unknown/external. Users can toggle a "confidence filter" to hide low-confidence edges.

---

## 2. Goroutine Spawn Tree View

The spawn tree is a **rooted hierarchy**, not a flat graph. The root is `main.main`. Each goroutine created by a `go` statement is a child of the goroutine that executed it. This tree view makes goroutine ownership explicit.

**Naming heuristics for anonymous goroutines:**
1. If the closure's body calls a named function `f` as its first statement: name = `goroutine:f`
2. If the spawn site is inside a named method: name = `goroutine:TypeName.MethodName:lineN`
3. Otherwise: name = `goroutine:anonymous:fileBasename:lineN`

**Spawn edge styles:**
- **Solid arrow** — unconditional spawn (outside any `if`/`switch` in the CFG)
- **Dashed arrow with condition label** — conditional spawn; label shows the branch predicate (e.g., `"if err == nil"`)
- **Fan-out arrow with ×N badge** — spawn inside a `for` loop; `N` is the upper bound if statically known, `∞` if not

**Large spawn trees:** Trees deeper than 6 levels are collapsed to summary nodes showing depth and child count. A "Expand subtree" button progressively reveals children. A "Flatten to depth N" slider adjusts the visible depth threshold.

**Search:** A search box filters the tree to goroutines whose name or spawn site matches. Non-matching goroutines are hidden (not merely ghosted) in tree mode to preserve the hierarchy's clarity.

---

## 3. Channel Topology View

Channels are rendered as directed edges between goroutine nodes. This view switches from the tree layout to a force-directed graph layout to better represent fan-in and fan-out topologies.

**Edge annotations:**
- **Element type label** — e.g., `chan *http.Request` shown inline on edge
- **Buffer size badge** — integer badge on the edge midpoint; `∞?` for dynamic size; `0` for unbuffered highlighted in amber
- **Direction markers** — filled arrowhead at receive end; open arrowhead at send end for bidirectional channels
- **Producer/consumer count** — if multiple goroutines send on the same channel, the edge is thickened and annotated with the count

**Select statement nodes:** A `select` statement with N cases is rendered as an intermediate diamond node with N incoming edge stubs. Each case is labeled with its channel and direction. The `default` case is a dashed edge to a `default` label node. `time.After` cases show a clock icon.

**Closed-channel detection:** A channel with a reachable `close()` call and a reachable send-after-close path is highlighted in red with a "Closed channel" warning badge. The send-after-close path is traced with a red dashed edge.

**Orphaned channels:** Channels with a producer but no reachable consumer (or vice versa) are rendered with the missing-end node as a grey question mark, and the channel edge is colored amber.

---

## 4. Mutex / Lock Coverage Map

This view shifts the primary node type from goroutines to **functions**. Each function is a node, colored by its lock coverage — which mutexes are held while executing it.

**Color scheme (additive per lock):**
- Unlocked: light grey `#E2E8F0`
- Under lock A only: blue `#4299E1`
- Under lock B only: green `#48BB78`
- Under both A and B: teal (additive blend of blue + green) `#319795`
- Under 3+ locks: darker blend with a "N locks" badge

Colors are assigned to locks in the order they are first encountered in the analysis, up to 8 distinct colors. Beyond 8 locks, a striped pattern is used.

**Lock order graph:** A separate small graph (inset, lower-right of view) shows the partial order of lock acquisitions. Each mutex is a node; a directed edge `A → B` means "A is acquired before B in some goroutine's execution." A cycle in this graph is a **deadlock risk** — rendered in red with a "Cycle detected" badge.

**Side panel — acquisition sequence:** Selecting a function shows a panel with the complete lock acquisition sequence for every goroutine context in which this function appears: `Goroutine G1: [Lock(A) → Lock(B) → ... → Unlock(B) → Unlock(A)]`. Each entry links to the goroutine's spawn site.

**Contention heat:** Functions that are acquired under a high-contention lock (many goroutines, high hold depth) are highlighted with a flame icon. "Contention risk" is computed as: `acquiringGoroutineCount × avgHoldDepth`.

---

## 5. Blocking Point Analysis

Blocking points are program locations where a goroutine's execution can pause, waiting for an external condition. They are rendered as colored markers positioned on the border of goroutine nodes.

**Blocking construct categories:**

| Construct | Color | Severity |
|---|---|---|
| Channel send (unbuffered) | Red | High |
| Channel receive (unbuffered) | Red | High |
| `sync.Mutex.Lock()` | Orange | Medium |
| `sync.RWMutex.RLock()` | Orange | Medium |
| `sync.WaitGroup.Wait()` | Amber | Medium |
| `time.Sleep()` | Yellow | Low |
| Syscall (blocking) | Grey | Informational |

Markers are positioned angularly around the goroutine node border, distributed evenly. Hovering a marker shows the file:line and a tooltip explaining the blocking construct and what event would unblock it.

**Blocking depth metric:** The number of independent blocking points on the shortest CFG path from goroutine entry to goroutine exit. A goroutine with blocking depth 0 runs to completion without yielding. A goroutine with blocking depth 5 must resolve 5 separate waits before it exits. High blocking depth is a complexity signal, not necessarily a bug.

**Total blocking path:** Selecting a goroutine highlights the set of blocking points on its shortest exit path in yellow. "Must unblock" (on the exit path) vs "may block" (on a non-exit branch) are visually distinguished.

---

## 6. Lifetime and Leak Detection

Goroutine lifetime is modeled as a **risk score** from 0.0 (clearly bounded) to 1.0 (almost certainly leaks).

**Risk factors (weighted sum):**

| Factor | Weight | Condition |
|---|---|---|
| No `context.Context` parameter | +0.30 | Function signature does not accept a context |
| No `ctx.Done()` receive on any `select` | +0.25 | No context cancellation path |
| Producer in `for` loop, no bound | +0.20 | Spawned or sending inside unbounded loop |
| No known termination path | +0.40 | CFG has no path to `return` or goroutine-exit |
| `time.After` but not `ctx.Done` | +0.10 | Timeout present but not propagated context |

Scores > 0.6 surface as a `high` severity finding. Scores between 0.4 and 0.6 surface as `medium`.

**Visual representation:** Each goroutine node has a lifetime ring — a colored arc around its border:
- Green (0.0–0.3): Bounded lifetime
- Yellow (0.3–0.6): Uncertain — review recommended
- Red (0.6–1.0): Probable leak

The ring arc length (not just color) encodes the score: a full circle = score 1.0.

---

## 7. select Statement Visualization

`select` statements appear as diamond-shaped multiplexer nodes interposed between goroutines and channels.

**Rendering rules:**
- Each case arm is a labeled edge entering the diamond: `recv: resultCh`, `send: workCh`, `ctx.Done()`, `time.After(5s)`, `default`
- `ctx.Done()` cases are colored green — they represent controlled termination
- `default` cases are dashed — they prevent blocking but may indicate a spin-wait
- `time.After` cases show a clock icon with the duration if statically known

**Select in a loop:** A `select` node inside a `for` loop is annotated with a loop badge. If the loop has no exit condition other than the select itself (i.e., the loop runs until a channel case matches), this is shown with a circular arrow. This pattern contributes +0.40 to the goroutine's leak risk score, regardless of other factors, because it means the goroutine's lifetime is entirely determined by channel availability.

---

## 8. Critical Path View

The critical path view is a two-click interaction: select a **source goroutine** (left-click), then shift-click a **target goroutine**. The view highlights the communication path between them.

**Highlighted path elements:**
- Channels on the path (in sequence)
- Mutexes acquired along the path
- Blocking points on the path

**Qualitative latency estimate:**
- **Fast** — 0 blocking points, all channels buffered
- **Medium** — 1–2 blocking points, channels likely buffered
- **Slow** — 3+ blocking points, or unbuffered channel
- **Unbounded** — path passes through a blocking point with no known unblocking signal

The path is also rendered as a Mermaid sequence diagram (exportable) showing goroutines as actors and channel operations as messages. This is intended for inclusion in performance review documents.

---

## 9. Diff Mode

Diff mode compares goroutine topology between two analysis snapshots (commits, branches, or manual scan pairs).

**Activation:** Drop-down in the view toolbar selects "Compare with baseline." The baseline is either the most recent CI scan or a user-selected snapshot by commit SHA.

**Visual encoding:**
- **New goroutine** (in current, not in baseline): Green fill
- **Removed goroutine** (in baseline, not in current): Red fill, dashed border
- **Changed channel topology** (same goroutines, different channel connections): Before-edge shown dashed, after-edge shown solid; both visible simultaneously with a toggle
- **Changed lifetime risk** (score delta > 0.2): Gradient ring (half old color, half new color)

**Summary panel:** A diff summary panel (top of view) shows: `+N goroutines, -M goroutines, ~K channel changes, N new findings, M resolved findings`. Each summary item is clickable to filter the view to just that category of change.

This mode is the primary interface for AI system operators reviewing model-generated code changes for concurrency regressions.

---

## 10. Integration with Flat Interface

The Goroutine Map table in the flat interface and the execution flow canvas share a unified Zustand selection store.

**Hover sync:** Hovering a row in the Goroutine Map table causes the corresponding goroutine node on the canvas to receive a selection ring and the canvas to pan to keep it visible (if outside the viewport). Debounced 200 ms.

**Click sync:** Clicking a row in the table anchors the canvas to that goroutine's 2-hop neighborhood (spawn parent, spawn children, connected channels, acquired mutexes).

**Filter propagation:** Setting a filter in the Goroutine Map table (e.g., "Only Unbounded lifetime") ghosts non-matching goroutine nodes on the canvas at 8% opacity. The canvas remains interactive — clicking a ghosted node clears the filter and re-selects it.

**Reverse sync:** Clicking a goroutine node on the canvas scrolls the Goroutine Map table to the corresponding row and briefly highlights it with an amber flash.

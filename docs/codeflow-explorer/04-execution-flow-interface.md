# CodeFlow Explorer — Execution Flow Interface

**Status:** Draft
**Audience:** Frontend engineers, concurrency auditors, performance engineers
**Scope:** Execution flow lens — execution units, message channels, synchronization, blocking analysis

---

## 1. The Concurrency Model

The execution flow lens models a program as concurrent execution units communicating through message channels and coordinating shared state via synchronization primitives. It is derived from static analysis over a Tree-sitter-backed, language-agnostic IR (not runtime instrumentation).

**What is modeled:**
- **Execution units** — stateful actors with spawn site, captured context, and lifetime model
- **Message channels** — communication edges with direction and capacity semantics
- **Sync primitives** — lock/semaphore/monitor style resources with acquisition order constraints
- **Join/barrier primitives** — group synchronization relationships
- **Select/race constructs** — multi-source wait and multiplexing behavior
- **Cancellation propagation** — cancellation/token paths as execution-unit lifetime signals

**What static analysis cannot know — and how uncertainty is communicated:**

| Construct | Limitation | Visual Representation |
|---|---|---|
| Indirect/dynamic spawns via reflection/runtime APIs | Cannot fully enumerate statically | Warning badge on spawn site |
| Dynamic queue/channel capacity | Capacity unknown | `?` annotation on channel edge |
| Alias-heavy message routing | Resolution may be incomplete | Dashed edge (probable, not certain) |
| External framework/runtime workers | Invisible to analysis | Grey phantom node at boundary |
| Runtime scheduler decisions | Non-deterministic by design | Not modeled; explicit disclaimer |

Uncertainty is communicated through edge stroke style: solid = certain, dashed = probable, dotted = possible, ghost = unknown/external. Users can toggle a confidence filter to hide low-confidence edges.

---

## 2. Spawn Tree View

The spawn tree is a rooted hierarchy, not a flat graph. The root is a detected application entrypoint. Each spawned execution unit is a child of the unit that created it.

**Naming heuristics for anonymous execution units:**
1. First statement calls named function `f`: `unit:f`
2. Spawn site inside named method: `unit:TypeName.MethodName:lineN`
3. Otherwise: `unit:anonymous:fileBasename:lineN`

**Spawn edge styles:**
- **Solid arrow** — unconditional spawn
- **Dashed arrow + condition** — conditional spawn
- **Fan-out arrow with ×N** — spawn in loop; `N` if statically known, `∞` if not

---

## 3. Message Topology View

Message channels are rendered as directed edges between execution units. The view uses force layout to expose fan-in/fan-out and bottlenecks.

**Edge annotations:**
- Payload type label
- Capacity badge (`0`, integer, or `?`)
- Direction markers
- Producer/consumer counts

**Multiplex nodes:** Multi-wait/select constructs are rendered as diamond nodes with one edge per case and a dashed edge for non-blocking default branches.

**Misuse detection:**
- Send-after-close/invalid-send paths highlighted in red
- Producer with no reachable consumer (or inverse) highlighted in amber

---

## 4. Synchronization Coverage Map

This view shifts primary nodes from execution units to functions. Each function is colored by synchronization coverage (which primitives are held while executing).

**Lock/order graph:** An inset graph shows partial order of acquisitions. A cycle implies deadlock risk.

**Acquisition sequence panel:** Selecting a function shows acquisition/release sequence per execution context.

**Contention heat:** A derived contention score highlights functions frequently reached under highly contended primitives.

---

## 5. Blocking Point Analysis

Blocking points are locations where execution can pause waiting for external conditions. They are rendered as markers on execution-unit nodes.

| Construct | Color | Severity |
|---|---|---|
| Send/receive on synchronous channel/queue | Red | High |
| Lock/semaphore acquire | Orange | Medium |
| Barrier/join wait | Amber | Medium |
| Sleep/timer wait | Yellow | Low |
| Blocking syscall/external I/O | Grey | Informational |

Markers include file:line and a tooltip describing the unblock condition.

---

## 6. Lifetime and Leak Detection

Execution-unit lifetime is modeled as risk score from 0.0 (bounded) to 1.0 (likely leak).

| Factor | Weight | Condition |
|---|---|---|
| No cancellation token in scope | +0.30 | Signature/context has no cancellation path |
| No cancellation branch in wait/select | +0.25 | No cancellation receive/branch |
| Producer in unbounded loop | +0.20 | Spawn/send inside unbounded loop |
| No known termination path | +0.40 | CFG has no path to completion |
| Timer without coordinated cancellation | +0.10 | Timeout exists, cancellation not propagated |

Scores > 0.6 surface as `high`; 0.4–0.6 as `medium`.

---

## 7. Multiplex Statement Visualization

Select/race-style constructs appear as diamond multiplexer nodes between execution units and channels.

**Rendering rules:**
- Each case arm is labeled (`recv: resultCh`, `send: workCh`, `cancel`, `timer`, `default`)
- Cancellation cases are green
- Default/non-blocking cases are dashed
- Timer cases show clock icon and duration if known

---

## 8. Critical Path View

Users select source and target execution units; the view highlights the communication path.

**Path elements:** message channels, synchronization primitives, blocking points.

**Latency estimate:** Fast / Medium / Slow / Unbounded based on blocking profile and channel semantics.

---

## 9. Diff Mode

Diff mode compares execution topology between two snapshots.

- New execution unit: green
- Removed execution unit: red dashed
- Changed channel topology: before/after overlay
- Changed lifetime risk (delta > 0.2): split ring gradient

Summary panel: `+N units, -M units, ~K channel changes, N new findings, M resolved findings`.

---

## 10. Integration with Flat Interface

The execution map table and canvas share unified selection/filter state.

- Hover sync highlights and pans to corresponding node
- Click sync anchors canvas to 2-hop neighborhood
- Filter propagation ghosts non-matching nodes
- Reverse sync from canvas to table row

---

## 11. Project Semantic Overrides

Execution flow semantics are extensible via `codeflow.semantic.yaml`.

- Map project-specific spawn APIs (for example worker-pool submit helpers) to `SPAWNS`
- Map project-specific drain/wait APIs to `JOINS`
- Map wrapper channel/queue APIs to `MessageChannel` send/receive operations

All override-derived edges include confidence and mapping provenance so teams can audit and tune behavior over time.

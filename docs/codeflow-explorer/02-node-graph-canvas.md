# CodeFlow Explorer — Node Graph Canvas Interface

**Status:** Draft
**Audience:** Frontend engineers, UX designers
**Scope:** Spatial graph canvas — design, interaction model, rendering strategy

---

## 1. Design Goals and Principles

The canvas is **not a diagram generator**. Existing tools (Graphviz, Mermaid, architecture diagram exporters) produce static images of dependency graphs. The CodeFlow Explorer canvas is an interactive query interface that happens to use spatial layout as its primary navigation metaphor.

Three commitments define the canvas:

**Spatial memory is a first-class feature.** Once a user has explored a subgraph and positioned nodes, that spatial arrangement becomes a cognitive asset. The layout engine respects user repositioning. "Where did I put the HTTP handler cluster?" is a valid mental model the tool must support, not fight.

**Lenses reframe without disorienting.** Switching from execution flow to data flow changes what the edges mean and which nodes are prominent — but the spatial positions of shared nodes remain stable. The user's spatial memory transfers across lenses. Nodes that are irrelevant in the new lens fade rather than vanish.

**Findings are navigational, not ornamental.** Anti-pattern findings are not badges pinned to a diagram. They are entry points into subgraph exploration. Clicking a finding on the canvas activates the affected subgraph, dims everything else, and opens the finding's explanation panel. The canvas becomes the finding's visualization.

---

## 2. Zoom Levels and Progressive Disclosure

The canvas has four discrete semantic zoom levels. Crossing a zoom threshold triggers a layout recalculation (animated, 400 ms) and a data fetch for the new level of detail.

| Level | Zoom Range | Nodes Represent | Edge Meaning | Typical Use |
|---|---|---|---|---|
| **L1: Module** | 0–15% | Go modules / external dependencies | Import dependency | "What modules does this service depend on?" |
| **L2: Package** | 15–40% | Packages within modules | Package imports, cross-package calls | "How are packages organized and coupled?" |
| **L3: Type/Goroutine** | 40–75% | Types, interfaces, goroutines, channels | Data flow edges, goroutine spawns, channel connections | "What types communicate? What goroutines exist?" |
| **L4: Function/CFG** | 75–100% | Functions, methods, SSA basic blocks | Call edges, control flow edges, field access | "What does this function do, and how does it connect?" |

At L1 and L2, clusters are rendered as convex hulls with a summary label (package count, finding count). Clicking a cluster expands it in place. At L3 and L4, individual nodes are rendered at full fidelity with icon, label, and state indicators.

---

## 3. Node Design

Each node type has a fixed visual signature: shape determines category, color determines state.

### Base Shapes

| Node Type | Shape | Fill |
|---|---|---|
| Package | Rounded rectangle | Blue-grey `#4A5568` |
| Struct/Type | Hexagon | Teal `#319795` |
| Interface | Dashed hexagon | Cyan `#0BC5EA` |
| Function/Method | Circle | Slate `#718096` |
| Goroutine | Diamond | Purple `#805AD5` |
| Channel (buffered) | Horizontal capsule with count badge | Green `#38A169` |
| Channel (unbuffered) | Horizontal capsule, no badge | Yellow `#D69E2E` |
| Mutex/RWMutex | Lock icon in square | Orange `#DD6B20` |
| Source (taint) | Circle with upward arrow | Orange `#ED8936` |
| Sink (taint) | Circle with downward arrow | Red `#E53E3E` |

### State Modifiers (overlays, not shape changes)

| State | Visual |
|---|---|
| **Reviewed** | Green checkmark badge, lower-right |
| **Suspect** | Amber warning triangle badge, upper-right |
| **Anchored** | Solid ring border, 3px, primary accent color |
| **Filtered-out** | 8% opacity, no border |
| **Finding attached** | Pulsing halo (see §9) |
| **Selected** | 2px white border + drop shadow |

Labels are shown below the node at L3/L4. At L2, only the first line of the label is shown (truncated). At L1, only the cluster summary label.

---

## 4. Edge Design

Edges carry semantic meaning through stroke style, color, and weight. All edges are directed (arrowhead at target).

| Edge Type | Stroke | Color | Weight | Animation |
|---|---|---|---|---|
| Package import | Solid | Grey `#A0AEC0` | 1px | None |
| Function call | Solid | Slate `#718096` | 1.5px | None |
| Goroutine spawn | Dashed | Purple `#805AD5` | 2px | Dash march on hover |
| Channel send | Solid with filled arrow | Green `#38A169` | 2px | Flow dots (active view) |
| Channel receive | Solid with open arrow | Teal `#319795` | 2px | Flow dots (active view) |
| Data flow (DFG) | Dotted | Orange `#DD6B20` | 1.5px | None |
| Tainted data flow | Solid | Red `#E53E3E` | 2.5px | Pulse on finding highlight |
| Mutex acquisition | Dash-dot | Orange `#DD6B20` | 1px | None |
| Trust boundary | Double solid | Red `#FC8181` | 3px | None |

At L1/L2, edges between clusters are bundled into a single composite edge with a count badge. Hovering unbundles them.

---

## 5. Layout Algorithm

**Base algorithm:** Force-directed (ForceAtlas2 variant) with semantic cluster constraints. Each package forms a cluster; cluster centers are constrained by inter-package import weight. Nodes within a cluster repel each other to prevent overlap. Cross-cluster edges apply a weak attractive force between cluster centers.

**Semantic constraints:** Architectural layers (defined in `codeflow.policy.yaml` as `layers: [handler, service, repository, domain]`) constrain vertical position. Handler nodes are anchored at the top of the canvas; repository nodes at the bottom. Violations (bottom-to-top edges) are immediately visually jarring — which is intentional.

**Large graph handling (1000+ nodes):**
- Nodes outside the current viewport are not rendered (virtual canvas).
- Clusters below the current zoom level are collapsed to a single representative node.
- Level-of-detail (LOD): at zoom < 20%, edges are replaced with cluster-level bundled edges.
- Background layout computation runs in a Web Worker; the main thread receives incremental position updates.

**User repositioning:** User-dragged nodes are pinned (marked `pinned: true`). The force simulation excludes pinned nodes from force calculation but continues routing edges to them. A "reset layout" button un-pins all nodes and re-runs the simulation.

---

## 6. Interaction Model

| Interaction | Behavior |
|---|---|
| **Hover node** | Tooltip: name, type, package, finding count. Connected edges brighten. |
| **Click node** | Select node. Finding panel shows findings for this node. Flat view filters to related rows. |
| **Double-click node** | Anchor: hide all nodes not within N hops. N is configurable (default: 2). |
| **Double-click anchored** | Un-anchor: restore full graph. |
| **Right-click node** | Context menu: Trace data flow, Show callers, Show callees, Mark reviewed, Mark suspect, Add comment, Copy link, Open in editor. |
| **Drag node** | Reposition and pin. |
| **Box select** | Drag on empty canvas to multi-select. |
| **Scroll** | Zoom in/out centered on cursor. |
| **Middle-drag** | Pan. |
| **Escape** | Deselect all, clear highlights. |

**Keyboard shortcuts:**

| Key | Action |
|---|---|
| `f` | Focus search (filter nodes by name) |
| `a` | Anchor selected node |
| `u` | Un-anchor |
| `r` | Mark selected as reviewed |
| `s` | Mark selected as suspect |
| `e` | Switch to execution flow lens |
| `d` | Switch to data flow lens |
| `1`–`4` | Jump to zoom level L1–L4 |
| `?` | Show keyboard shortcut overlay |

---

## 7. Lens Switching

Switching between execution flow and data flow lenses follows a choreographed transition that preserves spatial memory:

1. **(0–150 ms)** Edges fade out (opacity 1.0 → 0.0)
2. **(150–300 ms)** Node colors transition to the new lens's color scheme
3. **(300–450 ms)** New lens edges fade in (opacity 0.0 → 1.0)
4. **(450–600 ms)** Nodes relevant only to the new lens materialize; irrelevant nodes ghost (8% opacity)

Camera position and zoom level do not change during the transition. Nodes that exist in both lenses do not move.

A lens indicator pill (top-right of canvas) shows the active lens: "Execution Flow" (purple) or "Data Flow" (orange). Clicking it toggles.

---

## 8. Filter and Highlight Panel

A collapsible left sidebar (320px wide) contains:

**Filter groups:**
- **Package** — searchable tree of all packages, checkboxes to include/exclude
- **Severity** — checkboxes: Critical, High, Medium, Low, None
- **Edge type** — toggles per edge type (calls, spawns, data flow, etc.)
- **Concurrency** — toggle: "Only nodes involved in goroutine communication"
- **Taint status** — toggle: "Only tainted paths"
- **Reviewed status** — toggle: "Hide reviewed nodes"

Filters compose as AND across groups, OR within a group. Active filter chips appear below the filter header. Each chip has an ×  to remove it.

**Saved views:** Filter + zoom level + camera position are serialized to a `?view=<base64-json>` URL parameter. "Copy view link" copies the current URL. Saved views can be named and stored in the project config.

---

## 9. Findings Overlay

Nodes with attached findings show a pulsing halo. Halo properties:

- **Color:** Critical = red, High = orange, Medium = amber, Low = grey
- **Pulse period:** inversely proportional to severity (critical: 1s, low: 4s)
- **Multiple findings:** halo thickness scales with finding count (max 3 findings shown as concentric rings)

A finding badge (count) appears at the node's upper-right corner. Clicking the badge opens a popover listing all findings for this node, sorted by severity. Each finding in the popover has an "Expand" link that:
1. Activates the finding's affected subgraph (dims everything else)
2. Opens the Finding Detail panel (bottom drawer, 40% height)
3. Renders the full graph path inline in the panel's mini-canvas

A "critical path" overlay mode (toggle in findings panel) highlights the shortest path between the highest-severity finding's source and sink nodes, coloring intermediate edges red.

---

## 10. Collaborative Annotation

**Reviewed / Suspect marking:** Per-user, stored in the backend (not in git). Each node can have a set of user annotations: `{ userId, status: "reviewed"|"suspect", comment, timestamp }`. Multiple users' annotations on the same node are shown as avatar initials stacked in the node's lower-left corner.

**Comments:** Right-click → Add comment opens an inline text input anchored to the node. Comments are stored with the annotation. A comment thread icon appears on annotated nodes.

**View state sharing:** "Copy view link" encodes current camera position, zoom level, active lens, filter state, and selected nodes as a URL parameter. Opening the link in another browser restores the exact view. View links work without authentication for read-only sharing within an organization.

**PNG export:** "Export canvas" renders the current viewport to a PNG with embedded metadata (view state JSON in the PNG's `tEXt` chunk), so shared screenshots are also restorable views.

---

## 11. Technology Stack

**Graph rendering:** **Sigma.js** (WebGL renderer) for the graph body. Sigma.js handles 50,000+ nodes at 60 fps via WebGL instanced rendering. It is designed for interactive network graphs, unlike Three.js (3D-first) or plain D3+SVG (slow beyond 2,000 nodes).

**Layout computation:** **D3-force** running in a **Web Worker**, posting incremental position updates to the main thread via `postMessage`. This keeps the UI thread responsive during layout calculation on large graphs.

**SVG overlay:** Interactive controls (hover tooltips, selection rings, finding halos, annotation badges) are rendered as an SVG layer positioned absolutely over the WebGL canvas. SVG handles hit-testing more reliably than WebGL picking for small UI elements.

**State management:** **Zustand** store holding: selected node IDs, filter state, active lens, camera position, annotation data. The store is the shared source of truth between the canvas and the flat interface. Canvas and flat view both subscribe to the same store; selection in either propagates to the other without event cascades.

**Data streaming:** Initial graph load uses a paginated REST fetch. Incremental updates (new findings, analysis completion) arrive via **Server-Sent Events** (SSE) rather than WebSocket — simpler infrastructure, adequate for server-push-only updates. The canvas applies incremental patches (add node, update node, remove edge) without full re-render.

**Why not Cytoscape.js:** Cytoscape uses SVG/Canvas (not WebGL) and becomes noticeably slow beyond 3,000 nodes. Its API is also document-object-model-oriented rather than data-flow-oriented, making it a poor fit for a reactive state model. Sigma.js's WebGL renderer scales to 10× more nodes with lower latency.

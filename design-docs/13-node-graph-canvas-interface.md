# Node Graph Canvas Interface — CodeFlow Explorer

**Document:** 13  
**Status:** Draft  
**Scope:** Interactive node graph canvas for code analysis (execution flow + data flow lenses)

---

## 1. Design Goals and Principles

The canvas is an **interactive query interface**, not a static diagram. Static tools render a fixed snapshot; this canvas is a live analytical workspace where spatial position, zoom level, and node selection are all query parameters that narrow the visible state space.

**Core principles:**

- **Progressive disclosure over information overload.** The graph always renders at the current cognitive resolution. Zooming in is not scaling up pixels — it is revealing a deeper analytical layer with richer semantic content.
- **Lenses, not modes.** Execution flow and data flow are orthogonal views of the same graph model. Switching lenses re-weights edges and re-colors nodes without reloading; the user's camera position and selections persist.
- **Spatial memory as navigation.** Users build a mental map of the codebase as a spatial artifact. Clusters stay in stable positions across sessions. Findings anchor to real node positions so "where I saw that race condition" is a place you can return to.
- **The graph reflects analysis, not source layout.** Packages are not arranged by filesystem path. They are arranged by runtime relationship density — packages that talk constantly are near each other. Architectural violations appear as long cross-cluster edges, visible at a glance.
- **Findings are first-class citizens.** Anti-patterns are not a separate list. They overlay the graph as halos and badges on the exact nodes and edges they affect.

---

## 2. Zoom Levels and Progressive Disclosure

| Level | Scale | Content |
|-------|-------|---------|
| **L1 — Module Topology** | 1–10% | Go modules as labeled blobs. Edge count between modules shown as stroke weight. No individual nodes visible. |
| **L2 — Package Clusters** | 10–35% | Packages emerge as rounded rectangles grouped within module blobs. Cross-package import edges appear. Colors reflect domain (infra, business logic, transport). |
| **L3 — Type and Goroutine Topology** | 35–80% | Individual nodes for types, goroutines, channels, and mutexes appear inside package containers. Edges show data flow and spawn relationships. Finding badges visible on nodes. |
| **L4 — Function CFG and SSA Values** | 80–100% | Functions expand to show control-flow graph subgraph inline. SSA value nodes (scalars, pointers, interfaces) appear on data-flow edges. Stack frames, escape analysis annotations rendered per node. |

Semantic meaning shifts at each boundary. At L1, a thick edge means high coupling between modules — an architectural concern. At L4, a thin dashed edge between two SSA values means a data dependency with no direct call — a subtlety invisible at higher levels.

---

## 3. Node Design

**Shape vocabulary:**

| Node Type | Shape | Base Color |
|-----------|-------|------------|
| Package | Rounded rectangle (container) | Slate blue |
| Type / Struct | Hexagon | Teal |
| Function | Rectangle with rounded left cap | Indigo |
| Goroutine | Circle with dashed border | Amber |
| Channel (unbuffered) | Diamond | Emerald |
| Channel (buffered) | Diamond with fill bar indicating capacity | Emerald + orange fill |
| Mutex / RWMutex | Shield icon rectangle | Red-orange |
| Data source / sink | Trapezoid | Purple |
| Finding / anti-pattern | Star badge overlaid on host node | Yellow-red gradient |

**State modifiers applied as visual overlays:**

- **Reviewed:** Solid green checkmark badge, node border weight reduces by 30% (visually "settled").
- **Suspect:** Amber pulsing outer ring, 1.5px dashed border.
- **Highlighted:** Drop shadow + 2px solid white outline.
- **Filtered-out:** Opacity 0.08, no interaction affordance (ghost nodes preserve spatial memory of what was removed).
- **Anchored:** Blue fill on label bar, all non-connected nodes ghosted.

---

## 4. Edge Design

| Edge Type | Color | Stroke | Arrow | Animation |
|-----------|-------|--------|-------|-----------|
| Function call | Gray-700 | 1.5px solid | Forward arrowhead | None |
| Data flow | Teal | 1px solid | Forward | Dash flow at 0.3s when active |
| Goroutine spawn | Amber | 2px dashed | Fork arrow | None |
| Channel send | Emerald | 2px solid | Forward | Traveling dot when buffered |
| Channel receive | Emerald | 2px dashed | Backward | Traveling dot |
| Mutex acquire/release | Red-orange | 2px dotted | Bidirectional | Pulse on contention |
| Trust boundary crossing | Red | 3px double | Forward | Static, always rendered |

Directionality uses mid-edge arrowheads for readability when edges are short. Long cross-cluster edges include a source and target endpoint marker plus a mid-point label. Edge bundling applies at L1 and L2 — multiple edges between the same two clusters merge into a single weighted edge with a count badge.

---

## 5. Layout Algorithm

The base layout is **force-directed with semantic clustering.** Package membership creates a strong attractive force within cluster boundaries. A repulsive boundary force prevents cluster overlap. Cross-cluster edges contribute a weak pull that surfaces themselves as stretched edges — these visually signal architectural violations without requiring a separate report.

**Scaling to 1000+ nodes:**

- **Virtualized canvas:** Only nodes within 2× the current viewport are in the DOM. Off-screen nodes are culled; their edges are replaced with indicator arrows at the viewport edge.
- **Level-of-detail (LOD) substitution:** At L1 and L2, package blobs replace individual node rendering using a precomputed bounding hull. Switching to L3 dissolves hulls and materializes nodes with a 200ms stagger animation.
- **Stable layout across zoom:** Positions are computed once per session in normalized graph space. Zoom changes the camera, not node coordinates.
- **Incremental layout:** When new analysis results arrive, affected clusters re-layout locally using a Barnes-Hut approximation. The rest of the graph stays fixed to preserve spatial memory.

---

## 6. Interaction Model

**Hover:** Tooltip appears after 400ms with node summary (type, package, finding count, last-modified). Edge tooltips show relationship type, call count from profiling if available.

**Click (single):** Focuses node — highlights its direct neighbors, dims everything else. A focus panel opens on the right with full metadata.

**Click (double):** Anchors the node. Anchor mode hides all nodes with no path connecting to the anchor set. The anchor set is shown in a persistent anchor rail at the top of the canvas.

**Right-click:** Context menu with: *Copy path*, *Show callers*, *Show callees*, *Mark reviewed*, *Mark suspect*, *Add comment*, *Expand to L4*.

**Box select:** Drag on empty canvas. Selects all nodes in rectangle. Shift-box adds to selection.

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `Escape` | Clear focus / anchor |
| `F` | Fit selection to viewport |
| `L` | Toggle lens |
| `G` | Group selection into cluster |
| `/` | Open command palette |
| `Ctrl+Z` | Undo layout change |

---

## 7. Lens Switching

Switching lenses is a **morphing transition**, not a page change. The camera position is preserved. Nodes that exist in both lenses stay in place. Nodes unique to one lens dissolve (opacity 0 → 1 over 300ms). Edges re-color and re-stroke over 400ms.

The transition sequence: (1) fade out lens-specific edges, (2) re-color shared nodes, (3) fade in new edges, (4) materialize new nodes with stagger. Total: ~600ms.

A persistent lens toggle lives at the top-right of the canvas. A subtle watermark label (EXECUTION / DATA FLOW) in the canvas background confirms the current lens at a glance.

---

## 8. Filter and Highlight Panel

A collapsible left sidebar contains filter controls:

- **By package:** Multi-select checklist. Unchecked packages ghost their nodes.
- **By severity:** Slider from Info → Critical. Filters finding overlays.
- **By edge type:** Toggle chips per edge type.
- **By concurrency involvement:** Toggle to show only nodes participating in goroutine, channel, or mutex relationships.
- **Highlight pattern:** Presets (e.g., *unbuffered channel send in hot path*, *mutex held across goroutine boundary*) each apply a named highlight style.

**URL encoding:** The full filter + camera state serializes to a `?view=<base64-encoded-json>` query param. Share a URL to drop a colleague into the exact viewport, filter set, and selection. The encoded state includes: camera x/y/zoom, active lens, filter toggles, anchor set, and highlight preset name.

---

## 9. Findings Overlay

Findings attach spatially to their primary node. Visual indicators:

- **Pulsing halo:** 2px animated ring in severity color (yellow → orange → red). Pulse period inversely proportional to severity (critical: 0.8s, info: 2.5s).
- **Badge count:** Small numeric badge at top-right of node showing how many findings affect it.
- **Critical path highlighting:** When a finding is selected, the affected subgraph highlights in red-orange with increased edge weight. Non-affected nodes ghost.

Clicking a finding badge opens an inline explanation card anchored to the node (not a modal). The card shows: finding title, description, severity, affected code span, and a "Show subgraph" button that anchors the canvas to only the affected nodes.

---

## 10. Collaborative Annotation

Annotations are user-scoped metadata persisted server-side per repository analysis session.

- **Mark reviewed/suspect:** Sets a per-user status on a node. Status is visible to all collaborators as overlay indicators with avatar initials.
- **Comments:** Right-click → *Add comment* opens a threaded comment panel anchored to the node. Comment threads are visible as a small speech-bubble icon on the node.
- **View state links:** The `?view=` URL parameter encodes camera, filters, lens, anchor set, and selections. Shared links restore this exact state for any authenticated user.
- **Export snapshot:** Renders the current viewport to a PNG with overlays included. Findings badges and annotations are preserved. Exported images embed metadata in EXIF for traceability.

---

## 11. Technology Stack

**Rendering:** **Sigma.js** (WebGL-backed) for the primary graph renderer. Sigma handles 10,000+ nodes at interactive frame rates. Custom WebGL shaders handle the halo pulse animations and edge flow animations without per-frame DOM manipulation. An SVG overlay layer (managed by SolidJS) hosts tooltips, annotation cards, and finding explanation panels so they remain accessible DOM elements.

**Why not Cytoscape.js or plain D3+SVG:** Cytoscape's SVG renderer degrades beyond ~2,000 elements. D3+SVG requires manual LOD management and struggles with animated edges at scale. Three.js is viable but requires significant custom work for graph-specific interaction patterns.

**Graph model:** **Graphology** is the canonical in-memory graph data structure. It holds nodes, edges, and all attributes (positions, types, finding counts, annotation state). Sigma.js reads directly from a Graphology instance — no translation layer needed. Layout algorithms (forceAtlas2, noverlap) run off-thread as Graphology-compatible Web Workers via `graphology-layout-forceatlas2/worker`. Incremental updates from the server mutate the Graphology instance; Sigma re-renders only the changed subgraph.

**State management:** SolidJS reactive stores hold all UI state: camera position, filter toggles, active lens, selection set, anchor set, and annotation data. Stores are fine-grained signals — a filter toggle change triggers only the filter panel re-render and a Graphology attribute sweep, not a full canvas repaint. The rendering loop reads camera and selection state from signals so Sigma's `refresh()` is called only when relevant state actually changes.

**Streaming:** Graph data arrives over the existing WebSocket pub/sub system. The client subscribes to a `graph:<analysisId>` topic. Messages carry typed patch payloads (`node:add`, `node:update`, `edge:add`, `finding:attach`) that are applied directly to the live Graphology instance. The incremental layout worker receives a debounced signal to re-settle only the affected cluster, leaving the rest of the graph spatially stable.

**Canvas vs SVG:** WebGL canvas for the graph body via Sigma.js; SolidJS-rendered SVG/HTML overlay for all interactive UI elements (tooltips, finding cards, comment threads, anchor rail). This gives WebGL throughput for the graph body while keeping interactive overlays in the normal component tree — styled, accessible, and keyboard-navigable without manual canvas hit-testing.

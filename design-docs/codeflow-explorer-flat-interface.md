# CodeFlow Explorer — Flat Structured Interface

**Status**: Proposed  
**Date**: 2026-02-22  
**Area**: UI / Analysis Views

---

## 1. Design Philosophy

The canvas node graph reveals topology at a glance — you see clusters of packages, the hot paths between them, the isolated components. But topology is not enough. A security auditor does not skim a graph; they work a checklist. An architect does not eyeball dependency clusters; they iterate every cross-package import and confirm each one is intentional. A performance engineer sorts goroutines by lock hold time and works from the top down.

The flat interface exists for **systematic enumeration**: when you need to be sure you have seen everything, not just found something interesting. It shares a unified selection state with the canvas so that neither view is a dead end — you move fluidly from "show me everything" (flat) to "show me where this lives" (canvas) and back.

**Two cognitive modes, one model:**

- *Spatial exploration* (canvas): discover structure, identify anomalies visually, form hypotheses.
- *Systematic enumeration* (flat): verify exhaustively, rank by risk, confirm coverage, export evidence.

Selection in either view is reflected in both. Filtering in the flat view drives the canvas highlight state. Clicking a canvas node scrolls the flat view to the corresponding row. There is no modal switch; both panels coexist and stay synchronized.

---

## 2. Primary Views / Tables

Each view is a named table preset. Switching views resets column layout but preserves active filters that apply across views (package, severity).

### 2.1 Function Inventory

Columns: `Name`, `Package`, `Cyclomatic Complexity`, `Goroutine-Safe`, `Callers`, `Callees`, `LOC`  
Default sort: `Cyclomatic Complexity` descending  
Use case: Identify the most complex functions; audit goroutine-safety annotations; find deeply connected hubs.

### 2.2 Data Flow Paths

Columns: `Source`, `Sink`, `Path Length`, `Taint Status`, `Trust Boundaries Crossed`, `Validated`  
Default sort: `Taint Status` (tainted first), then `Trust Boundaries Crossed` descending  
Use case: Security review of every path from external input to a sensitive sink; confirm validation is applied before trust boundary crossings.

### 2.3 Goroutine Map

Columns: `Spawn Site`, `Parent Goroutine`, `Lifetime Estimate`, `Channels Used`, `Mutexes Held`, `Leak Risk`  
Default sort: `Leak Risk` descending  
Use case: Concurrency audit; find goroutines with unbounded lifetimes or unclosed channels.

### 2.4 Mutex / Lock Coverage

Columns: `Lock Name`, `Acquiring Functions`, `Max Hold Time Estimate`, `Contention Risk`, `Package`  
Default sort: `Contention Risk` descending  
Use case: Performance review; identify hotly contested locks and functions that hold them too long.

### 2.5 Channel Inventory

Columns: `Name / Spawn Site`, `Direction`, `Buffer Size`, `Producer Count`, `Consumer Count`, `Leak Risk`  
Default sort: `Leak Risk` descending, then `Producer Count` descending  
Use case: Find unbuffered channels with mismatched producer/consumer counts; identify potential deadlock sites.

### 2.6 Anti-Pattern Findings

Columns: `Rule ID`, `Severity`, `Location`, `Affected Nodes`, `Status` (`open` / `reviewed` / `false-positive`)  
Default sort: `Severity` descending, then `Rule ID`  
Use case: Primary security and quality audit workflow; bulk-review low-severity findings; escalate high-severity ones.

### 2.7 Type Lineage

Columns: `Type Name`, `Embedded Types`, `Field Count`, `Instantiation Sites`, `Flows To`  
Default sort: `Flows To` count descending  
Use case: Understand data model propagation; find types that leak across package boundaries unexpectedly.

### 2.8 Trust Boundary Crossings

Columns: `Entry Point`, `Data Type`, `Validations Applied`, `Sinks Reached`, `Risk Score`  
Default sort: `Risk Score` descending  
Use case: Targeted security review of every point where untrusted data enters a trust domain; confirm sanitization chains.

---

## 3. Column System

Columns are defined per view with a type (`string`, `int`, `enum`, `bool`, `duration`, `score`) that determines sort behavior and filter widget.

**Sortable**: Every column is sortable; click header to toggle asc/desc. Multi-column sort via shift-click.

**Filterable**: Each column exposes a per-column filter appropriate to its type — free text for strings, range sliders for numerics, checkbox lists for enums.

**Computed columns** are derived at query time and cached. Example: `Risk Score = 0.4 × taint_weight + 0.3 × complexity_normalized + 0.3 × concurrency_flag`. Computed columns are sortable and filterable like any other column.

**Column management**: columns are pinnable (frozen left), resizable by dragging the header divider, and individually show/hide via a column picker (⚙ icon, top-right). Each view ships a set of **column presets** — e.g., "Security Audit" preset for Data Flow Paths pins `Taint Status`, `Trust Boundaries Crossed`, `Validated` and hides `Path Length`.

---

## 4. Filter and Search

A collapsible sidebar (default: open) provides faceted filtering. Filters compose with AND across facets, OR within a facet (multi-select checkboxes).

- **Free-text search**: matches `Name`, `Location`, `Rule ID`; toggleable regex mode (indicated by `/…/` prefix in the input).
- **Package / module filter**: hierarchical tree of packages; check any node to include all descendants.
- **Severity filter**: `critical`, `high`, `medium`, `low`, `info` — checkbox list, OR within.
- **Concurrency involved**: boolean toggle; shows only rows involving goroutines, channels, or mutexes.
- **Taint status**: `tainted`, `sanitized`, `unknown` — checkbox list.
- **Status** (findings only): `open`, `reviewed`, `false-positive`.

Active filters are displayed as chips below the search bar. Each chip has an × to remove individually. **Saved filter sets** can be named and bookmarked; they appear in a dropdown alongside the view selector. Filter sets are per-view and stored in local profile state (exportable as JSON).

---

## 5. Row Actions

**Hover**: the corresponding node on the canvas pulses with a highlight ring. No click required — provides continuous spatial context while scanning a list.

**Single click**: selects the row; canvas centers and zooms to the corresponding node or subgraph. Selection is reflected in both panels.

**Right-click context menu**:
- Show in canvas
- Trace data flow (opens Data Flow Paths view filtered to this node as source or sink)
- Show callers / callees (opens Function Inventory filtered to this function's call graph)
- Mark reviewed
- Add comment (opens inline comment editor; persisted to local findings state)
- Copy link (deep link URL encoding view + row ID + active filters)

**Bulk select**: checkbox column (far left). Shift-click for range selection. Bulk actions appear in a floating action bar: `Mark reviewed`, `Mark false-positive`, `Export selection`.

---

## 6. Inline Expansion

Rows expand with ▶ toggle or by pressing Enter. Expanded sub-rows are indented and styled distinctly.

- **Data Flow Path row**: expands to show each intermediate hop as a sub-row — node name, edge type, transformation applied. Hovering a hop highlights that specific edge on the canvas.
- **Goroutine row**: expands to show its channel interactions in chronological/causal order — send, receive, close operations with the source location of each.
- **Anti-Pattern Finding row**: expands to show the full graph path that triggered the rule, with code snippets for each node in the path (fetched from the Code Snippet Panel model). A dismissible explanation of why the rule fired is shown at the top of the expansion.

Expansion state is preserved when re-sorting or filtering within the same view session.

---

## 7. Code Snippet Panel

A resizable side panel (default: right, 35% width) shows the source for the selected row's primary code location.

- **Syntax highlighting** via the project's shared tokenizer (same as the editor integration).
- **Line numbers** shown; the relevant line range is highlighted in amber.
- **Navigation arrows** (← →) step through every code location referenced in the row (e.g., all hops in a data flow path, all acquiring functions for a lock).
- **"Open in editor"** button emits a `vscode://file/…` URI or invokes the configured LSP `textDocument/open` command. The URI scheme is configurable in settings (VS Code, Cursor, Zed, or generic LSP).
- The panel is **collapsible**; its state is remembered per session. When collapsed, a narrow gutter shows the file name and line of the current location.

---

## 8. Cross-linking with Canvas

Selection is a shared singleton: selecting a row in the flat view is identical in effect to clicking the corresponding node in the canvas. Both panels observe the same selection store.

**Filter propagation**: when a filter is active in the flat view, the canvas dims non-matching nodes to 20% opacity. The user can toggle "apply flat filters to canvas" independently so they can compare the filtered set against full context.

**Escape hatch**: a persistent "Reset to full graph" button (top bar, always visible) clears all flat-view filters and resets canvas zoom/pan to the overview. A breadcrumb trail ("Filtered: package=auth, severity=high") lets the user remove individual filter terms without clearing everything. The last five filter states are retained as undo history (Ctrl+Z in the filter bar).

---

## 9. Export and Reporting

**Row-level export**: right-click → "Export row as JSON/Markdown".

**View export** (current filter state):
- **CSV**: column headers match the current visible column set; values are escaped per RFC 4180.
- **JSON**: array of objects; computed columns included with a `_computed: true` flag on each derived field.
- **Markdown report**: generates a structured document with a summary table, per-row detail sections, and embedded code snippets for expanded rows.

**Findings report template**: a pre-built Markdown template designed for security audit handoff. Sections: Executive Summary (finding counts by severity), Critical Findings (full expansion), Review Coverage (% of findings in `reviewed` or `false-positive` status), Appendix (full findings table). Triggered from the export menu → "Generate audit report".

**CLI export**: `codeflow export --view=findings --format=markdown --filter='severity=critical,high' > report.md`. The `--schedule` flag accepts a cron expression for automated report drops to a configured output directory.

---

## 10. Keyboard Navigation

The flat interface is fully operable without a mouse.

| Key | Action |
|-----|--------|
| `j` / `↓` | Next row |
| `k` / `↑` | Previous row |
| `Enter` | Expand / collapse row |
| `Space` | Toggle row selection |
| `Shift+Space` | Range-select to previous anchor |
| `/` | Focus free-text search |
| `Escape` | Clear search / deselect / close expansion |
| `Tab` | Cycle focus: filter sidebar → table → snippet panel |
| `Shift+Tab` | Reverse cycle |
| `]` / `[` | Next / previous code location in snippet panel |
| `r` | Mark selected row(s) reviewed |
| `c` | Open comment editor for selected row |
| `e` | Export current view (opens export dialog) |
| `?` | Open keyboard shortcut reference overlay |

Focus is visually indicated with a high-contrast outline ring. Screen reader ARIA labels are applied to all interactive elements; rows announce their primary fields on focus; sort state is announced on column header activation.

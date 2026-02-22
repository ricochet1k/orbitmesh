# CodeFlow Explorer — Flat Structured Interface

**Status:** Draft
**Audience:** Frontend engineers, UX designers
**Scope:** Tabular systematic enumeration interface

---

## 1. Design Philosophy

The flat interface serves a different cognitive mode than the canvas. The canvas is for *spatial exploration* — building a mental map of how a system is connected, discovering unexpected relationships, following a thread of curiosity through the graph. The flat interface is for *systematic enumeration* — working through every instance of a category, marking items reviewed, ensuring nothing is missed.

A security auditor reviewing all trust boundary crossings does not want a canvas. They want a sorted, filterable table they can work through row by row, with a keyboard shortcut to mark each row reviewed and move to the next. A compliance reviewer verifying that every exported function has an associated test wants a paginated list, not a graph.

**Unified selection state** ties both interfaces together without a modal switch. The flat view and canvas share a single Zustand selection store. Selecting a row in the flat view highlights and centers the corresponding node in the canvas. Selecting a node in the canvas filters the flat view's active table to rows related to that node. Switching between interfaces does not lose context — it changes *how* the same selected entity is displayed.

The two interfaces coexist in a split-pane layout (resizable, default 50/50). Users can collapse either pane to work in a single-interface mode.

---

## 2. Primary Views / Tables

Eight predefined views cover the most common auditing workflows. Each view is a named query against the semantic model, rendered as a sortable, filterable table.

### 2.1 Function Inventory

**Use case:** Architect or reviewer building a comprehensive map of what functions exist and how complex they are.

| Column | Type | Notes |
|---|---|---|
| Name | String | Fully-qualified method name |
| Package | String | Filterable by package tree |
| Cyclomatic complexity | Integer | Computed from CFG block count |
| Goroutine-safe | Boolean | True if all access under mutex or via channel |
| Caller count | Integer | Inbound call edges |
| Callee count | Integer | Outbound call edges |
| Finding count | Integer | Linked findings, colored by max severity |
| Is exported | Boolean | Public API surface |

**Default sort:** Cyclomatic complexity descending.

### 2.2 Data Flow Paths

**Use case:** Security reviewer enumerating all paths from external input to sensitive sinks.

| Column | Type | Notes |
|---|---|---|
| Source | String | Entry point (HTTP handler, env var, DB read) |
| Sink | String | Terminal operation (SQL exec, file write, HTTP response) |
| Path length | Integer | Number of hops in the flow graph |
| Taint status | Enum | Definite / Conditional / Clean |
| Trust boundaries crossed | Integer | Count of policy-declared boundaries on path |
| Sanitizers applied | Integer | Count of sanitizer nodes on path |
| Validated | Boolean | True if all paths have ≥1 sanitizer |

**Default sort:** Taint status (Definite first), then trust boundaries crossed descending.

### 2.3 Goroutine Map

**Use case:** Performance engineer or concurrency auditor reviewing goroutine lifecycle.

| Column | Type | Notes |
|---|---|---|
| Label | String | Derived name (enclosing function + spawn site line) |
| Spawn site | String | File:line of the `go` statement |
| Parent goroutine | String | Who spawned this goroutine |
| Lifetime estimate | Enum | Bounded / Context-dependent / Unbounded / Unknown |
| Channels used | Integer | Count of distinct channels accessed |
| Mutexes held | Integer | Count of distinct mutexes acquired |
| Leak risk score | Float 0–1 | Weighted factor sum |

**Default sort:** Leak risk score descending.

### 2.4 Mutex / Lock Coverage

**Use case:** Identifying lock contention hotspots and verifying lock discipline.

| Column | Type | Notes |
|---|---|---|
| Lock name | String | Variable FQN |
| Kind | Enum | Mutex / RWMutex |
| Acquiring functions | Integer | Number of functions that call Lock() |
| Max hold depth | Integer | Max CFG path length while lock held |
| Contention risk | Enum | Low / Medium / High |
| Order conflicts | Integer | Number of detected order inversion findings |
| Shared goroutines | Integer | Goroutines that can simultaneously acquire |

**Default sort:** Contention risk (High first), then order conflicts descending.

### 2.5 Channel Inventory

**Use case:** Goroutine leak detection and communication topology review.

| Column | Type | Notes |
|---|---|---|
| Allocation site | String | File:line of `make(chan ...)` |
| Element type | String | Go type of channel elements |
| Direction | Enum | Bidirectional / Send-only / Receive-only |
| Buffer size | Integer | 0 = unbuffered; -1 = dynamic |
| Producer count | Integer | Goroutines that send on this channel |
| Consumer count | Integer | Goroutines that receive from this channel |
| Leak risk | Enum | None / Producer-only / Consumer-only / Orphaned |

**Default sort:** Leak risk (Orphaned first).

### 2.6 Anti-Pattern Findings

**Use case:** The primary triage view for working through detected issues.

| Column | Type | Notes |
|---|---|---|
| Rule ID | String | e.g., `goroutine-leak`, `lock-order-inversion` |
| Severity | Enum | Critical / High / Medium / Low |
| Risk score | Float 0–10 | Base × modifier factors |
| Primary location | String | File:line of the triggering code |
| Affected node | String | Primary node in the finding's graph path |
| Status | Enum | Open / Acknowledged / Reviewed / Suppressed |
| AI-generated | Boolean | True if git blame attributes to AI author |

**Default sort:** Risk score descending.

### 2.7 Type Lineage

**Use case:** Architect reviewing type composition and emergent "god struct" patterns.

| Column | Type | Notes |
|---|---|---|
| Type name | String | Fully-qualified type name |
| Kind | Enum | Struct / Interface / Alias / Basic |
| Embedded types | Integer | Count of embedded types |
| Field count | Integer | Total field count (including embedded) |
| Packages that use it | Integer | Count of packages with a reference |
| Instantiation sites | Integer | Count of construction sites |
| Taint exposure | Boolean | True if any field receives tainted data |

**Default sort:** Packages that use it descending (surfacing god structs).

### 2.8 Trust Boundary Crossings

**Use case:** Security reviewer's primary enumeration of all external data entry points.

| Column | Type | Notes |
|---|---|---|
| Entry point | String | Function/handler where data enters |
| Boundary type | Enum | HTTP / RPC / DB / Env / File / IPC |
| Data type | String | Go type of the incoming data |
| Validations applied | Integer | Count of known validators on the path |
| Sinks reached | Integer | Count of distinct sink functions reachable |
| Max sink severity | Enum | Severity of the highest-risk sink reached |
| Fully validated | Boolean | True if all paths to all sinks have validators |

**Default sort:** Fully validated (False first), then max sink severity descending.

---

## 3. Column System

**Column types:** String (sortable, text-filterable), Integer (sortable, range-filterable), Float (sortable, range-filterable), Enum (sortable, checkbox-filterable), Boolean (toggle-filterable).

**Multi-column sort:** Click a column header to set primary sort; shift-click to add secondary sort. Sort indicator arrows show direction. Maximum 3 sort columns active simultaneously.

**Per-column filter widget:** Clicking a column header's filter icon opens an inline filter widget appropriate for the column type: text input for strings, range slider for integers, checkbox list for enums.

**Computed columns:** Columns like "risk score" and "contention risk" are computed at query time using weighted formulae defined in the backend. They can be sorted and filtered like any other column.

**Column management:** Each view has a column picker (gear icon, top-right of table). Columns can be shown/hidden, reordered by drag, and pinned to the left. Column state is saved per-view per-user. Named column presets (e.g., "Security Audit", "Performance Review") can be saved and shared.

---

## 4. Filter and Search

**Sidebar:** A collapsible left sidebar contains faceted filters. Filters compose as AND across facet groups, OR within a group.

**Free-text search:** Top of sidebar. Supports plain text and `/regex/` syntax. Matches against all string columns simultaneously. Results highlight matched substrings.

**Package/module tree:** Hierarchical checkboxes. Checking a parent includes all children. A "only" button excludes all other packages.

**Severity filter:** Checkbox group: Critical, High, Medium, Low, None. Default: all checked.

**Taint status filter:** Checkbox group: Definite, Conditional, Clean.

**Concurrency toggle:** "Only show nodes involved in goroutine communication."

**Reviewed status:** Three-way toggle: All / Unreviewed only / Reviewed only.

**Filter state display:** Active filters appear as dismissible chips below the table header. "Clear all filters" resets to defaults. Filter state is included in the view state URL parameter for sharing.

**Saved filter sets:** Named filter sets (e.g., "Unreviewed critical findings") are saved in the project config. Switching filter sets is a single click.

---

## 5. Row Actions

**Hover:** The corresponding node in the canvas receives a bright selection ring and the canvas pans to keep it visible (if not already in viewport). The hover is debounced 200 ms to avoid jitter when scanning rows.

**Click (single):** Selects the row. Canvas focuses on the corresponding node. Inline expansion collapsed by default.

**Click (double):** Anchors the canvas to the selected node (2-hop neighborhood view).

**Right-click context menu:**
- Show in canvas (centers and selects)
- Trace data flow (opens data flow lens centered on this node)
- Show callers / Show callees (filters to N-hop caller/callee subgraph)
- Mark reviewed / Mark suspect
- Add comment
- Suppress finding (findings view only; requires justification text)
- Copy link (deep link to this row)
- Open in editor (VS Code / Cursor / Zed via URI scheme)

**Bulk select:** Checkbox column at the left of each row. "Select all on page" header checkbox. Bulk selection activates a floating action bar at the bottom of the table: "Mark all reviewed", "Suppress all (with justification)", "Export selected".

---

## 6. Inline Expansion

Rows expand on click to show contextual sub-rows. Expansion state survives sort and filter changes for the duration of the session.

**Data flow path row:** Expands to show each hop as a sub-row: `Source → [transformer A] → [transformer B] → Sink`. Each sub-row has a file:line link. A mini taint indicator shows whether the hop preserves or breaks taint.

**Goroutine row:** Expands to show a chronological list of the goroutine's channel interactions (sends and receives in CFG order), then its mutex acquisitions. Each interaction links to the corresponding channel or mutex row in their respective views.

**Finding row:** Expands to show:
1. The full graph path that triggered the finding (ordered list of nodes and edges)
2. Code snippet for each node (first 5 lines surrounding the relevant line)
3. The pattern's `explain` text with actual values substituted
4. A "Trace in main canvas" button

**Type lineage row:** Expands to show the type's field list as sub-rows, each with taint exposure indicator and write/read site counts.

---

## 7. Code Snippet Panel

A right-side panel (320px, collapsible) shows source code for the selected row's primary code location.

- **Syntax highlighting:** Via Shiki (same tokenizer as VS Code), using the project's detected language
- **Line highlighting:** The relevant line(s) are highlighted with an amber background
- **Context:** 10 lines above and below the highlighted line, collapsible to show the full function
- **Multi-location navigation:** When a row has multiple code locations (e.g., a finding touching 3 functions), navigation arrows (◀ ▶) cycle through each location. A breadcrumb shows "2 of 5 locations"
- **Open in editor:** A button at the top-right opens the file at the correct line via:
  - VS Code: `vscode://file/{path}:{line}`
  - Cursor: `cursor://file/{path}:{line}`
  - Zed: `zed://{path}:{line}`
  - Configurable in project settings

---

## 8. Cross-linking with Canvas

**Bidirectional selection:** The Zustand `selectedNodeIds` set is written by both the canvas (click) and the flat view (row click). Both interfaces subscribe and re-render on change.

**Filter propagation:** Setting a filter in the flat view dims non-matching nodes on the canvas (8% opacity). The canvas remains interactive — users can click dimmed nodes to see their detail, which clears the flat-view filter.

**Reset escape hatch:** A "Reset to full graph" button (top of filter sidebar) clears all filters and restores the canvas to its un-anchored, un-filtered state. A confirmation is shown if the user has unsaved annotation state.

**Breadcrumb navigation:** The filter sidebar shows a breadcrumb of applied filter chips. Clicking an intermediate breadcrumb step restores that filter state. A 5-step undo history is maintained per-session.

---

## 9. Export and Reporting

**Row-level export:** Right-click → "Export row as JSON/CSV" exports the expanded row data including sub-rows.

**View-level export:** "Export view" (top-right of table) exports the current filtered/sorted view as:
- **CSV:** All columns, current filter applied, column headers in row 1
- **JSON:** Array of row objects with nested expansion data
- **Markdown:** A formatted table suitable for inclusion in a design document or PR description

**Findings Report template:** A "Generate Report" button in the findings view creates a structured security audit document:
```markdown
# Security Findings Report — {project} @ {commit}
Generated: {date}

## Summary
- Critical: N | High: N | Medium: N | Low: N
- New since baseline: N

## Findings

### [CRITICAL] Goroutine Leak — internal/handler/upload.go:142
...
```

**Scheduled reports:** Via CLI: `codeflow report --schedule daily --format markdown --output reports/` generates a report on each run. Intended for CI pipelines that drop finding summaries as build artifacts.

---

## 10. Keyboard Navigation

Full keyboard accessibility throughout the flat interface.

| Key | Action |
|---|---|
| `j` / `↓` | Next row |
| `k` / `↑` | Previous row |
| `Enter` | Expand / collapse inline |
| `Space` | Toggle row selection (bulk) |
| `/` | Focus search input |
| `Escape` | Clear search / close expansion / deselect |
| `Tab` | Move focus between: search, filter sidebar, table, code panel |
| `r` | Mark selected row as reviewed |
| `s` | Mark selected row as suspect |
| `c` | Add comment to selected row |
| `e` | Open selected row's primary location in editor |
| `?` | Show keyboard shortcut overlay |
| `Ctrl+A` | Select all rows on current page |
| `Ctrl+E` | Export current view |

All interactive elements have ARIA labels. Sort state is announced via `aria-live` region. Screen reader mode collapses the canvas pane and expands the code snippet panel by default.

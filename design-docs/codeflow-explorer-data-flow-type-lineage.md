# CodeFlow Explorer — Data Flow and Type Lineage Interface

**Status**: Proposed  
**Date**: 2026-02-22  
**Area**: Analysis / Security Views

---

## 1. The Data Flow Model

The data flow model is built on top of a static single-static assignment (SSA) representation of the program. Each SSA value has a unique definition point and zero or more use sites. The model tracks:

- **SSA values**: scalars, pointers, interface values, slice headers, and map descriptors — each identified by its defining instruction and package-qualified name.
- **Struct field writes and reads with field sensitivity**: `req.Body` and `req.Header` are tracked as distinct taint-carrying slots. Field accesses are resolved through pointer analysis (Andersen-style, whole-program) so that `(*T).Field` through an interface dispatch is not conflated with a direct struct access.
- **Type assertions and interface unwrapping**: `v.(ConcreteType)` transfers taint from the interface value to the concrete type. A failed assertion in a two-value form (`v, ok := x.(T)`) propagates taint only along the `true` branch.
- **Serialization boundaries**: calls to `json.Marshal`, `json.Unmarshal`, `proto.Marshal`, `proto.Unmarshal`, `gob.Encode`, `gob.Decode`, `xml.Marshal`, and registered codec wrappers are serialization nodes. Taint crosses the boundary: unmarshaled output is tainted if the encoded input is tainted; marshaled bytes are tainted if any contributing field is tainted.
- **Function arguments and return values**: inter-procedural taint propagates through call graphs. For each callee, formal parameters bind to actual arguments; return values bind at each call site. Variadic arguments are treated as a tainted slice if any element is tainted.

**Sources** are program points where untrusted data enters the analysis domain: `net/http.Request.Body`, `net/http.Request.Header`, `net/http.Request.URL`, `net/http.Request.Form`; `database/sql.Rows.Scan` output variables; `os.Getenv`; channel receive expressions; `os.ReadFile` and `io.Reader` read results.

**Transforms** are intermediate operations that may mutate, re-type, or partially sanitize tainted data: type assertions, struct field projection, `strings.Split` / `strings.Join`, `strconv.Atoi` (which both transforms and validates), `fmt.Sprintf`, `bytes.Buffer.Write`, and any function that takes a tainted value and returns a derived value.

**Sinks** are program points where tainted data can cause harm: `database/sql.DB.Exec` and `DB.Query` (SQL injection); `net/http.ResponseWriter.Write` and `fmt.Fprintf(w, ...)` (XSS / response injection); `os.Exec` and `syscall.RawSyscall` (command injection); `os.Create` and `os.WriteFile` (path traversal); `log.Printf` variants (log injection); channel send expressions carrying tainted payloads to privileged goroutines.

**Taint propagation** is flow-sensitive within a function and context-sensitive across a configurable call-depth bound (default: 5). A field write `s.F = taintedVal` marks `s.F` tainted; a subsequent `s.F = literal` clears that field. Taint is conservative: union semantics apply at join points — if either branch reaching a phi node is tainted, the phi output is tainted.

**Implicit flows** arise when a control-flow branch condition depends on a tainted value and the branch body reaches a sensitive sink regardless of the data carried through. Example: `if user.Role == "admin" { writeSecret(w) }` — if `user.Role` is tainted, the branch outcome leaks information. The model tracks taint through branch predicates separately from direct data taint and labels these paths `implicit`.

---

## 2. Type Lineage View

The Type Lineage View is a directed graph where nodes are named Go types (structs, interfaces, type aliases) and edges represent structural relationships:

- **Embedding**: `struct A embeds struct B` — rendered as a solid containment edge. The embedded type's fields are promoted and taint on promoted fields is attributed to both types.
- **Composition** (non-embedding field of another struct type): dashed containment edge.
- **Interface satisfaction**: `type X implements interface Y` — dotted upward edge from concrete type to interface. Multiple implementors of the same interface are grouped visually.
- **Type conversion**: `T(v)` where `T` and `type(v)` are distinct named types — thin orange edge.

Each type node expands to show its field list. Fields are color-coded:

- **Dead write**: field is declared but never written at any reachable program point — shown in gray with a strikethrough.
- **Dead read**: field is written but never read — shown in amber.
- **Taint-exposed**: at least one write site is reachable from a source — shown with an orange left border.
- **Sink-reaching**: at least one read site reaches a sink — shown with a red right border.

The **instantiation timeline** for a type is accessible via the detail panel: a list of every `T{...}` or `new(T)` composite literal in the program, annotated with which fields are set at that site and which are left at zero value. Sites are grouped by package. This answers: "do all callers agree on what constitutes a fully-initialized `T`?"

---

## 3. Field Provenance Panel

Selecting any struct field (from the Type Lineage View, the Struct Field Heatmap, or the flat table) opens the Field Provenance Panel.

The panel has three sections:

**Write Sites**: every SSA store instruction that targets this field, grouped by call stack depth. Each entry shows: file and line, the enclosing function, the value being stored (tainted / literal / derived), and whether this write site is reachable from a taint source. Write sites are sorted by their position in the typical request lifecycle (inferred from call graph distance from HTTP handler entry points).

**Read Sites**: every SSA load from this field. Each entry shows: file and line, the enclosing function, and whether this read site reaches a taint sink within `N` hops.

**Mini Timeline**: a horizontal lane diagram. The X-axis represents call graph depth from the HTTP entry point (or the dominant goroutine entry point for background processing). Write events appear as downward triangles; read events as upward triangles. Tainted events are orange; clean events are blue. If a read precedes any write in some execution path (use-before-initialization), that read is marked with a warning glyph.

The panel answers the operational question: "Is it possible to call `handler.ServeHTTP` and reach the sink read site without having passed through any of the sanitizing write sites?"

---

## 4. Taint Flow Visualization

Tainted data is rendered as colored directed paths overlaid on the data flow graph.

**Color encoding**:
- **Orange** node fill: taint source.
- **Red** node fill: taint sink.
- **Green** node fill: sanitizer (breaks taint — further nodes downstream of a sanitizer are not colored unless a second taint path reaches them).
- **Path edge color**: interpolated from orange (near source) to red (near sink). Color intensity encodes confidence: full saturation for definite taint (unconditional path); 50% saturation for conditional taint (taint flows only along one branch of a conditional).
- **Implicit flow paths**: rendered with a dashed edge pattern to distinguish them from explicit data flow.

**Fan-out rendering**: when one tainted value is stored to multiple struct fields, the path fans out from the store instruction to each field node. Each branch retains the full taint color. If branches later converge at a common sink (e.g., all fields are marshaled to a single JSON response), the convergent path is drawn with a thicker stroke proportional to the number of contributing branches.

A path with no green sanitizer node between source and sink is a **finding**. Findings are assigned a severity based on sink category (SQL sink → Critical, ResponseWriter sink → High, log sink → Medium) and confidence (definite → no modifier, conditional → severity downgraded one level).

The graph supports **path filtering**: click a source node to show only paths originating from that source. Click a sink to show only paths reaching that sink. The intersection (source + sink selected) shows only paths between those two points.

---

## 5. Trust Boundary Crossing View

Trust boundaries are declared in a policy file (`codeflow.policy.yaml`) with entries of the form:

```yaml
trust_boundaries:
  - name: http_ingress
    functions: [net/http.(*Request).Body, net/http.(*Request).Header]
  - name: db_read
    functions: [database/sql.(*Rows).Scan]
  - name: env_read
    functions: [os.Getenv]
```

The Trust Boundary Crossing View presents a **matrix**: rows are crossing points (the specific call sites where data enters from a declared boundary), columns are sink categories (DB, Network, File, OS, Log, Channel). Each cell is colored:

- **Red**: at least one path from this crossing to this sink category has no sanitizer.
- **Yellow**: all paths are sanitized but at least one sanitizer is from the "weak" list (configurable; e.g., `fmt.Sprintf` is not a sanitizer for SQL).
- **Green**: all paths reaching this sink category pass through a strong sanitizer, or no path exists.
- **Gray**: no path exists from this crossing to this sink category.

Clicking a cell drills down to the Taint Flow Visualization filtered to paths from that crossing point to that sink category.

A **validations applied** column shows the deduplicated set of sanitizer functions that appear on any path from this crossing point to any sink. This allows reviewers to quickly confirm that expected validators (e.g., a custom `validateUserInput()`) are present for every ingress point.

---

## 6. Transformation Chain Visualization

For any selected SSA value or struct field, the Transformation Chain shows the sequence of operations the value undergoes from its origin to its eventual use.

Nodes in the chain:

- **Type assertion** (`v.(T)`): shows the source type and target type.
- **Field projection** (`s.F`): shows the struct type and field name.
- **Serialization** (`json.Marshal(v)`): shows the codec and the resulting byte type.
- **Transmission** (`w.Write(b)`): shows the transport and the destination.
- **Arithmetic / string ops**: `strings.Join`, `strconv.Itoa`, `fmt.Sprintf` — each is a chain node.

The chain renders left-to-right. Each node has a **forward** (→) and **backward** (←) navigation control so the user can walk the chain step by step. The side panel shows the source code of the enclosing statement for the selected chain node, with the relevant operands highlighted.

When the chain branches (one value flows to two different sinks via different transforms), it renders as a DAG rather than a linear chain. Branch points are indicated by a fork glyph.

---

## 7. Validation Coverage Map

The Validation Coverage Map answers: "for each sink, what fraction of paths arriving at it pass through a declared validator?"

Validators are declared in `codeflow.policy.yaml`:

```yaml
validators:
  strong: [strconv.Atoi, html.EscapeString, net/url.QueryEscape, myapp/validate.UserID]
  weak: [strings.TrimSpace, fmt.Sprintf]
```

Sink functions are the primary nodes in this view. Each sink node is annotated with:

- **Total paths in**: the number of distinct taint paths reaching this sink.
- **Validated paths**: paths that pass through at least one strong validator.
- **Weakly validated**: paths with only weak validators.
- **Unvalidated**: paths with no validator.

Color: green if 100% of paths are strongly validated; yellow if any path is weakly validated or unvalidated but no path is completely bare; red if any path has no validator at all.

The coverage percentage is shown as a fraction inside the node. Hovering a sink node opens a popover listing the unvalidated paths grouped by their source boundary.

---

## 8. Implicit Flow Detection

Implicit flows are tracked by recording the set of tainted values that appear in branch predicates (`if`, `switch`, `select`, `for` conditions). When a tainted predicate gates access to a sensitive sink — even if no tainted data is passed directly to the sink — the path is flagged as an implicit flow finding.

The view highlights conditional branches where the branch condition is tainted. Each such branch is rendered with an orange condition node. The branches of the conditional are rendered as sub-paths; branches leading to sensitive sinks are highlighted in red.

Example: `if user.IsAdmin { writeSecret(w) }` — if `user.IsAdmin` is derived from an unvalidated HTTP header, the `writeSecret` branch is an implicit flow finding of severity High.

Implicit flows are shown in a dedicated sub-table with columns: `Condition Expression`, `Taint Source`, `Branch Target`, `Sink Category`, `Confidence`. They are also overlaid on the main Taint Flow Visualization with dashed edge styling.

---

## 9. Data Flow Diff Mode

Diff mode compares the data flow graph of two program snapshots (identified by git refs or build artifact hashes).

**Change categories and colors**:

- **New taint path** (orange): a source-to-sink path that exists in the new snapshot but not the old. Highest priority review item — indicates a potential new vulnerability introduced by the change.
- **Resolved path** (green): a path that existed in the old snapshot but not the new. Indicates a fix or refactor that eliminated a taint path.
- **Changed transformation chain** (yellow): the source and sink are the same in both snapshots, but the intermediate transformation nodes differ — e.g., a sanitizer was added, removed, or replaced.
- **Unchanged paths**: rendered at 30% opacity to provide context without dominating the view.

The diff view includes a **summary panel**: counts of new, resolved, and changed paths grouped by sink category and severity. This is designed for code review workflows: an AI-generated PR is analyzed in diff mode against the base branch, and the summary is embedded in the pull request comment as a structured findings table.

---

## 10. Struct Field Heatmap

The Struct Field Heatmap is a supplementary view: a two-dimensional grid where rows are struct types (sorted by package, then name) and columns are individual fields. Each cell represents one field of one type.

**Color dimensions** (switchable via a toggle group in the toolbar):

- **Write frequency**: number of distinct write sites, log-scaled. High write frequency may indicate a field serving as a shared accumulator — a concurrency risk.
- **Read frequency**: number of distinct read sites. Fields read many more times than written may be configuration fields; fields read rarely may be dead.
- **Taint exposure**: fraction of write sites reachable from a taint source. Full red = every write site is tainted.
- **Cross-goroutine access**: number of distinct goroutine contexts (inferred from goroutine entry points in the call graph) in which this field is accessed. High values without observed mutex protection are flagged.

A **risk score** column (rightmost, always visible) combines all four dimensions: `risk = 0.4 × taint_exposure + 0.3 × cross_goroutine_normalized + 0.2 × write_frequency_normalized + 0.1 × (dead_field_flag)`. Fields are sortable by risk score. Clicking any cell navigates to the Field Provenance Panel for that field.

The heatmap is exportable as a CSV (all four dimension values per field) for offline analysis or regulatory audit documentation.

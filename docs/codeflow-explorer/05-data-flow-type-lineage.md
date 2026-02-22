# CodeFlow Explorer — Data Flow and Type Lineage Interface

**Status:** Draft
**Audience:** Frontend engineers, security reviewers, architects
**Scope:** Data flow lens — value provenance, type lineage, taint analysis, trust boundaries

---

## 1. The Data Flow Model

The data flow lens tracks how values move through a program, from the moment they enter the system to the moment they are consumed or emitted. The foundation is SSA (Static Single Assignment) form, where every value has exactly one definition site, making provenance tracking tractable.

**Sources** — points where externally-controlled data enters the program:
- HTTP request fields (`r.Body`, `r.Header`, `r.URL.Query()`, `r.FormValue()`)
- Environment variables (`os.Getenv`, `os.LookupEnv`)
- Database reads (results from `sql.Row.Scan`, `sql.Rows.Scan`)
- File reads (`os.ReadFile`, `bufio.Scanner.Text()`)
- Channel receives (data arriving from another goroutine)
- Command-line arguments (`os.Args`)
- IPC / RPC deserialization (`json.Unmarshal`, `proto.Unmarshal` on network input)

**Transforms** — operations that may preserve, modify, or break taint:
- Struct field assignment (`s.Field = value`)
- Type assertion (`value.(ConcreteType)`)
- Interface wrapping/unwrapping
- Function call (taint propagates through arguments and return values)
- String formatting (`fmt.Sprintf` propagates taint from any tainted argument)
- Slice/map construction
- Type conversion (`string(byteSlice)`)

**Sinks** — points where values leave the program or affect system state:
- Database writes (`db.Exec`, `db.QueryRow` with user-controlled SQL)
- HTTP response writes (`w.Write`, `w.Header().Set`)
- File writes (`os.WriteFile`, `f.Write`)
- Shell execution (`exec.Command`, `syscall.Exec`)
- Log output (`log.Printf`, `zap.Logger.Info`)
- Channel sends (propagating to another goroutine)

**Field-sensitive tracking:** `req.Body` and `req.Header` are distinct taint slots. The analyzer uses `go/pointer`'s `AccessPath` representation to distinguish reads and writes at the field level within a struct. A function that taints `req.Body` does not implicitly taint `req.Header`.

**Explicit vs implicit flows:**
- *Explicit flow*: `sink(taintedValue)` — tainted data flows directly to sink.
- *Implicit flow*: `if taintedValue == "admin" { writeSensitiveData(w) }` — the branch condition is tainted, and the branch body reaches a sink. Implicit flows are tracked separately and rendered with distinct visual styling.

---

## 2. Type Lineage View

The type lineage view renders Go types as nodes and their relationships as edges. It answers: "how are types composed, and where do they live in the codebase?"

**Node types in the lineage graph:**

| Node | Appearance |
|---|---|
| Concrete struct | Filled hexagon |
| Interface | Dashed hexagon |
| Type alias | Hexagon with `≡` badge |
| Generic instantiation | Hexagon with `<T>` badge |
| Embedded relationship | Thick solid edge |
| Interface satisfaction | Dashed edge with open arrowhead |
| Type conversion | Dotted edge |

**Dead field detection:**
- *Never written* (zero-value only): field label shown in grey with strikethrough
- *Written but never read*: field label shown in amber with a write-only icon
- Both patterns are findings in the anti-pattern engine (severity: low for single instances, medium if it's a wide struct)

**Instantiation timeline:** Clicking a type node opens a side panel showing every construction site in call-graph depth order. Each site shows which fields are explicitly initialized (non-zero) and which are left at zero value. This reveals structs that are typically only partially initialized, which is often a source of subtle bugs.

**Embedding chain:** Deeply embedded types are shown with a "show embedding chain" expand button that renders the full transitive closure of embedded types as a vertical list with arrows, not just direct embeddings.

---

## 3. Field Provenance Panel

Selecting a struct field in the type lineage view or the struct field heatmap opens the field provenance panel.

**Content:**
- **Write sites** — every location in the codebase where this field is assigned, with:
  - File:line link
  - 3-line code snippet
  - Whether the assigned value is tainted (orange badge) or clean (green badge)
  - Call stack depth from the nearest HTTP entry point
- **Read sites** — every location where this field is read, with the same annotations
- **Mini timeline** — a horizontal timeline aligned to call-graph depth from the application's HTTP entry point. Write events appear above the timeline (↓ arrows), read events below (↑ arrows). This makes "use before initialization" patterns visible as a read event to the left of any write event.

**Taint provenance chain:** If the field receives tainted data, a "Show taint source" button renders the full path from the original source to this field as a breadcrumb chain: `HTTP req.Body → json.Unmarshal → UserInput.Name → User.DisplayName`.

**Multi-write conflict detection:** If the same field is written by two different goroutines without an enclosing mutex acquisition, the write sites are highlighted in red and linked to the "unguarded concurrent map/field write" finding.

---

## 4. Taint Flow Visualization

Tainted data is rendered as colored paths through the data flow graph.

**Color encoding:**
- **Source nodes**: Orange `#ED8936` with upward arrow icon
- **Tainted edges**: Orange, intensity proportional to taint confidence (definite = 100% saturation, conditional = 50% saturation)
- **Implicit flow edges**: Same color but dashed stroke
- **Sanitizer nodes**: Green `#48BB78` — a taint path that passes through a sanitizer node has its downstream edges rendered in grey (clean)
- **Sink nodes**: Red `#E53E3E` with downward arrow icon
- **Unsanitized path to sink**: Red edges, 2.5px stroke weight

**Fan-out rendering:** When a single tainted value is written to multiple struct fields, the taint edge fans out from the source node with a small distribution node (dot) and individual edges to each field. Each fan edge is labeled with the field name.

**Path highlighting:** Clicking a tainted edge activates "path mode" — all edges not on this path are dimmed. A path breadcrumb appears at the top of the canvas: `req.Body → decode → user.Name → renderTemplate → w.Write`. Each breadcrumb step is clickable and centers the canvas on that node.

**Finding annotation:** Any source-to-sink path without a sanitizer node is a finding. The path is highlighted with a red glow, and a finding badge appears at the sink node. Clicking the badge opens the trust boundary bypass finding detail.

---

## 5. Trust Boundary Crossing View

Trust boundaries are declared in `codeflow.policy.yaml`:

```yaml
trust_boundaries:
  - id: http-ingress
    type: HTTP
    packages: ["net/http"]
    functions: ["http.Handler.ServeHTTP", "http.HandlerFunc"]
  - id: db-read
    type: Database
    packages: ["database/sql"]
    functions: ["sql.Row.Scan", "sql.Rows.Scan"]
```

The trust boundary crossing view renders a **matrix**: rows are crossing points (entry functions), columns are sink categories (DB write, network send, file write, log, shell exec). Cells are colored:
- **Red**: Tainted data reaches this sink category with no sanitizer
- **Yellow**: Some paths validated, some not
- **Green**: All paths to this sink category pass through a validator
- **Grey**: No data from this entry point reaches this sink category

**Drill-down:** Clicking a red or yellow cell filters the data flow graph to show only the tainted paths between that entry point and that sink category. The canvas and flat view filter simultaneously.

**Boundary crossing detail:** Clicking a row (entry point) expands it to show each specific taint path, the data type at the boundary, which validations have been applied, and which sinks are reachable.

---

## 6. Transformation Chain Visualization

For a selected value or field, the transformation chain shows the complete sequence of operations applied to it from source to sink.

**Chain node types:**
- **Assignment** (`s.Field = v`): Square node
- **Type assertion** (`v.(T)`): Diamond node with type label
- **Function call** (passes value as argument): Circle with function name
- **Serialization** (`json.Marshal`, `proto.Marshal`): Cylinder node
- **String format** (`fmt.Sprintf`): Triangle node

**Navigation:** "Forward" steps toward the sink; "Backward" steps toward the source. The full chain is rendered as a horizontal sequence of nodes with directional arrows. The code snippet panel on the right shows the source at each selected step.

**Fork points:** When a value fans out to multiple destinations (e.g., written to two struct fields), the chain shows a fork node and the user can select which branch to follow.

---

## 7. Validation Coverage Map

The validation coverage map answers: "for all data that reaches a given sink function, what fraction arrives fully validated?"

**Known validators** are declared in `codeflow.policy.yaml`:
```yaml
validators:
  - package: "strconv"
    functions: ["Atoi", "ParseFloat", "ParseBool"]
    strength: strong
  - package: "html"
    functions: ["EscapeString"]
    strength: strong
  - pattern: "*.Validate()"
    strength: strong
  - pattern: "*.Sanitize()"
    strength: medium
```

**Sink coverage rendering:** Each sink node is rendered with a coverage arc:
- **Full green arc** (360°): All arriving taint paths pass through a strong validator
- **Partial green/red arc**: Fraction of paths validated proportional to arc length
- **Full red arc**: No validation on any arriving path

**Coverage fraction tooltip:** Hovering a sink shows: "12/15 paths validated (80%). 3 unvalidated paths from: HTTP req.Body (2), os.Args[1] (1)."

**Coverage drill-down:** Clicking an unvalidated fraction opens the flat view filtered to only the unvalidated data flow paths reaching this sink, sorted by taint confidence.

---

## 8. Implicit Flow Detection

Implicit flows occur when the *control flow* depends on a tainted value, even if the tainted value itself is not directly passed to a sink.

**Detection:** The analyzer identifies SSA values used as branch predicates (`if`, `switch`, `for` conditions). If a branch predicate is tainted and any successor block of the branch reaches a sensitive sink, that is an implicit flow.

**Visual representation:**
- Branch nodes (if/switch conditions) are rendered as orange condition nodes when tainted
- Edges from a tainted branch to a sink-reaching block are dashed orange
- The implicit flow is a distinct edge type from explicit data flow — it means "the value's truthiness affects what the sink receives," not that the value itself is passed

**Finding integration:** Implicit flows to sensitive sinks are surfaced as `medium` severity findings with a dedicated rule ID (`implicit-flow-to-sink`). The finding detail explains that the control flow itself is a channel for tainted data.

---

## 9. Data Flow Diff Mode

Diff mode compares data flow graphs between two analysis snapshots.

**Change categories:**
- **New taint path** (source-to-sink path in current, not in baseline): Orange highlighted path with "NEW" badge
- **Resolved taint path** (in baseline, not in current): Green dashed path with "RESOLVED" badge
- **Changed transformation chain** (same endpoints, different intermediate nodes): Yellow path with before/after toggle
- **Unchanged path**: Rendered at 30% opacity to reduce visual noise

**PR summary table:** Diff mode generates a machine-readable summary intended for PR comment bots:

```markdown
## Data Flow Changes

### New Taint Paths (⚠️ Review Required)
| Source | Sink | Sanitized? |
|--------|------|------------|
| req.Body → UserInput.Name | db.Exec | No |

### Resolved Taint Paths
| Source | Sink | Resolution |
|--------|------|------------|
| req.URL.Query() → sql.QueryRow | — | Validator added |
```

This summary is generated by `codeflow diff --format markdown` and is designed for automated posting to pull request comments.

---

## 10. Struct Field Heatmap

The struct field heatmap provides a bird's-eye view of data handling across all structs in the codebase.

**Four dimensions (switchable via toggle):**
- **Write frequency** — how often this field is written (proxy for "is this field actively maintained?")
- **Read frequency** — how often this field is read
- **Taint exposure** — fraction of write sites that receive tainted data (0.0 = always clean, 1.0 = always tainted)
- **Cross-goroutine access** — whether this field is written and read from different goroutines without mutex protection

**Composite risk score:**
```
risk = (taintExposure × 0.4) + (crossGoroutineAccess × 0.4) + (writeFrequency × 0.2)
```

Normalized to 0–1. Fields with risk > 0.7 are highlighted in red.

**Layout:** Structs as parent rows; fields as child rows. Sorted by struct risk score (max field risk) descending. The heatmap cells are colored using a green → yellow → red gradient.

**Export:** The heatmap exports to CSV with all four dimension values and the composite risk score per field, suitable for use in a security audit document.

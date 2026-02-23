# CodeFlow Explorer — Data Flow and Type Lineage Interface

**Status:** Draft
**Audience:** Frontend engineers, security reviewers, architects
**Scope:** Data flow lens — value provenance, type lineage, taint analysis, trust boundaries

---

## 1. The Data Flow Model

The data flow lens tracks how values move from ingress points to sinks. Analysis is performed over a Tree-sitter-backed, language-agnostic IR so provenance is consistent across languages.

**Sources** (externally controlled input):
- HTTP/API request fields and payloads
- Environment and process configuration
- Database/query results
- File and object-store reads
- Message/queue receives
- CLI arguments and job payloads
- RPC/IPC deserialization

**Transforms**:
- Field/element assignment
- Type narrowing/conversion/casting
- Wrapper/unwrapper calls
- Function argument/return flow
- String/format/template operations
- Collection construction and merge

**Sinks**:
- Database writes and dynamic query execution
- HTTP/API responses
- File/object-store writes
- Shell/process execution
- Logs/telemetry emission
- Message/queue sends

**Field-sensitive tracking:** `request.body` and `request.headers` are distinct taint slots. Taint is tracked by access path so one field does not implicitly taint siblings.

---

## 2. Type Lineage View

Type lineage renders language-level types and relationships as graph nodes/edges.

| Node | Appearance |
|---|---|
| Concrete type | Filled hexagon |
| Interface/protocol/trait | Dashed hexagon |
| Type alias | Hexagon with `≡` badge |
| Generic instantiation | Hexagon with `<T>` badge |
| Embedding/composition | Thick solid edge |
| Conformance/satisfaction | Dashed edge |
| Conversion | Dotted edge |

**Dead field detection:** never-written and write-only fields are surfaced as low/medium findings depending on breadth and risk context.

---

## 3. Field Provenance Panel

Selecting a field opens provenance details:
- Write sites with file:line, snippet, taint badge, entrypoint depth
- Read sites with same metadata
- Timeline of writes/reads aligned to call depth

If tainted, "Show source path" renders full chain from source to selected field.

**Concurrent write conflict:** highlights fields written by multiple execution units without reachable synchronization.

---

## 4. Taint Flow Visualization

Taint is rendered as colored paths:
- Source nodes: orange
- Tainted edges: orange (intensity = confidence)
- Implicit-control edges: dashed orange
- Sanitizer nodes: green
- Sink nodes: red
- Unsanitized source-to-sink path: red glow + finding badge

Clicking an edge enters path mode and dims unrelated paths.

---

## 5. Trust Boundary Crossing View

Trust boundaries are declared in `codeflow.policy.yaml` by boundary ID, category, symbol patterns, and entrypoints.

Boundary matrix:
- Rows: boundary entrypoints
- Columns: sink categories (DB, network, file, log, process)
- Cell colors: red/yellow/green/grey by validation status

Cell click filters graph + flat view to only matching paths.

---

## 6. Transformation Chain Visualization

For any selected value/field, the chain view shows operations from source to sink.

Node kinds:
- Assignment
- Type conversion/assertion
- Function call
- Serialization/deserialization
- Formatting/template operation

Supports forward/back navigation and branch selection at fan-out points.

---

## 7. Validation Coverage Map

Validators are declared in `codeflow.policy.yaml` with symbol patterns and strength (`strong`, `medium`, `weak`).

Each sink node shows validated-path coverage arc:
- Full green: all paths strongly validated
- Mixed arc: partial coverage
- Full red: no validated path

Tooltip shows exact validated/unvalidated counts and source breakdown.

---

## 8. Implicit Flow Detection

Implicit flow is tracked when tainted conditions control branches that reach sensitive sinks.

Rendering:
- Tainted condition nodes in orange
- Dashed edges from tainted branch to sink-reaching blocks
- Distinct rule ID from explicit taint flow

---

## 9. Data Flow Diff Mode

Compares two snapshots and classifies:
- New taint path
- Resolved taint path
- Changed transformation chain
- Unchanged path (de-emphasized)

Outputs markdown/JSON summaries for PR automation via `codeflow diff --format markdown`.

---

## 10. Struct/Type Field Heatmap

Heatmap dimensions:
- Write frequency
- Read frequency
- Taint exposure
- Cross-execution-unit access without synchronization

Composite score:

```text
risk = (taintExposure * 0.4) + (crossExecutionAccess * 0.4) + (writeFrequency * 0.2)
```

Fields with risk > 0.7 are highlighted in red. Export supports CSV for audit workflows.

---

## 11. API Boundary Bridges and CRUD Type Links

Projects can add request bridge mappings in `codeflow.semantic.yaml` to connect client callsites to server handlers through `REQUESTS` edges.

For simple endpoint patterns, projects can add `OPERATES_ON` mappings from handlers/endpoints to domain types, enabling end-to-end traceability from UI/API calls to affected model types.

Bridge and type-link edges include confidence metadata and are exposed in the same confidence filtering controls as other derived edges.

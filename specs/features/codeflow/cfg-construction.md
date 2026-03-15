# Intra-Procedural Control Flow Graph Construction

## 1. Summary

This feature adds intra-procedural Control Flow Graph (CFG) construction to the CodeFlow analysis engine. For every function discovered during fact extraction, the engine builds a CFG consisting of basic blocks connected by control flow edges, then persists the blocks and edges into goraphdb alongside the existing `Function`, `CallSite`, and `ExecutionUnit` nodes. Basic-block granularity is the primary unit of CFG structure, while fine-grained `NEXT_STMT` edges between individual statements enable the tag propagation engine (used by read-after-write detection) to reason about sequential ordering within a single function body. Go-specific constructs (`defer`, `select`, `go`, `panic/recover`, labeled `break`/`continue`/`goto`) and JS/TS-specific constructs (`try/catch/finally`, `async/await`, `Promise` chains, labeled `break`/`continue`) are each modeled with well-defined CFG patterns.

## 2. Motivation

The existing CodeFlow graph captures **inter-procedural** relationships (which function calls which, which goroutines are spawned) but has no representation of **intra-procedural** control flow. Without it:

- The read-after-write detection spec (`read-after-write-detection.md`) references `[:NEXT*]` traversals between statements, but these edges do not exist today.
- There is no way to query "does a WRITE always precede a READ within this handler?" because there is no ordering information between call sites inside a function.
- Complexity analysis (`complexity.go`) currently works from the tree-sitter CST alone and cannot leverage graph queries.
- Future analyses (dead code detection, exception-path coverage, resource leak detection) all require intra-procedural flow.

Building the CFG closes this gap and unblocks the tag propagation engine.

## 3. Scope

### In Scope

- Construction of basic-block-level CFGs for Go and JS/TS functions.
- `Block` nodes in goraphdb, one per basic block, with scalar properties for ordering and source range.
- `NEXT` edges between `Block` nodes carrying a scalar `condition` property.
- `NEXT_STMT` edges between individual statement-bearing nodes (`CallSite`, `ExecutionUnit`, and a new lightweight `Statement` node for non-call statements that carry effects).
- `DEFERS` edges modeling Go deferred call semantics.
- Integration into the existing scan pipeline: CFG construction runs after fact extraction, before enrichment.
- Incremental rebuild: only functions whose source file changed are re-processed.
- Configurable opt-in via `codeflow.yaml` (`cfg: { enabled: true }`).

### Out of Scope

- Inter-procedural control flow (call graph edges already exist as `CALLS`/`SPAWNS`).
- Data flow analysis or SSA construction (future feature).
- CFG construction for languages other than Go and JS/TS.
- Visual rendering of CFGs in the frontend (separate spec).
- Whole-program path enumeration or symbolic execution.

## 4. Requirements & User Experience (UX)

### 4.1 Configuration

Developers enable CFG construction in their project's `codeflow.yaml`:

```yaml
cfg:
  enabled: true           # default: false
  stmt_edges: true        # emit NEXT_STMT edges (default: true when cfg.enabled)
  max_function_lines: 500 # skip CFG for functions larger than this (default: 500)
```

### 4.2 Querying

With CFG edges in the graph, users can write Cypher queries through the existing `/api/v1/codeflow/query` endpoint:

**Find all basic blocks in a function:**
```cypher
MATCH (f:Function {name: "HandleRequest"})-[:CONTAINS_BLOCK]->(b:Block)
RETURN b ORDER BY b.block_index
```

**Find all paths between two blocks (using variable-length traversal):**
```cypher
MATCH (entry:Block {function_id: "pkg:main.HandleRequest", is_entry: true})-[:NEXT*1..20]->(exit:Block {is_exit: true})
RETURN exit
```

**Find sequential call sites within a function (for tag propagation):**
```cypher
MATCH (w:CallSite {callee_expr: "db.Write"})-[:NEXT_STMT*1..50]->(r:CallSite {callee_expr: "db.Read"})
WHERE w.caller_id = r.caller_id
RETURN w, r
```

**Find deferred calls for a function:**
```cypher
MATCH (f:Function {name: "ProcessFile"})-[:DEFERS]->(d:CallSite)
RETURN d ORDER BY d.defer_order DESC
```

### 4.3 Functional Requirements

1. Every function's CFG must have exactly one entry block and at least one exit block.
2. Every block must be reachable from the entry block (unreachable dead code blocks are marked `is_dead: true` but still stored).
3. The `NEXT` edge `condition` property must accurately reflect branch semantics (`"true"`, `"false"`, `"default"`, `"fallthrough"`, `"unconditional"`, `"panic"`, `"defer_return"`).
4. `NEXT_STMT` edges must reflect the textual order of statements within a function body, following control flow (not just line number order).
5. Go `defer` statements must generate synthetic edges from every exit point back through deferred calls in LIFO order.
6. The CFG must be deterministically reproducible: same source input always produces the same graph.

## 5. System Design & Architecture

### 5.1 Pipeline Integration

The CFG construction phase inserts into the existing pipeline between fact extraction and enrichment (rules evaluation):

```
Source Files
    |
    v
[Tree-sitter Parse] ──> CST per file
    |
    v
[Fact Extraction] ──> Functions, CallSites, Spawns (existing)
    |
    v
[CFG Construction] ──> Blocks, NEXT edges, NEXT_STMT edges, DEFERS edges  (NEW)
    |
    v
[Rules/Enrichment] ──> Findings, tag propagation
    |
    v
[Persistence] ──> goraphdb
```

CFG construction receives the tree-sitter CST (which is still available; the `boundTree` is held open during the file walk) and the extracted facts. It produces new fact types that are appended to `ExtractionSummary`.

### 5.2 CFG Node Granularity: Basic Blocks

A **basic block** is a maximal sequence of statements with no internal branching: control enters at the top and exits at the bottom. This is the standard compiler IR representation and is the right default for CFGs because:

- It compresses the graph significantly. A 50-line function with 5 `if` statements might have 30 statements but only 10-12 basic blocks. This matters at scale (see Section 5.8).
- Most analyses (dominance, reachability, loop detection) operate on basic blocks, not individual statements.
- goraphdb query performance degrades with node count; fewer nodes means faster traversals.

Each basic block becomes a `Block` node in goraphdb. Individual statements within blocks are **not** modeled as separate `Block` child nodes (this would negate the compression benefit). Instead, statements are tracked via:

1. The `stmt_count` scalar property on `Block` nodes.
2. The `block_id` and `stmt_index` scalar properties added to existing `CallSite` and `ExecutionUnit` nodes, linking them to their containing block and position within it.
3. `NEXT_STMT` edges between statement-bearing nodes for fine-grained ordering.

This hybrid approach gives us block-level compression for structural queries and statement-level precision for tag propagation, without requiring array properties (which goraphdb cannot filter on in Cypher).

### 5.3 New Fact Types

```go
type BlockFact struct {
    ID          string   `json:"id"`           // "{function_id}#block:{block_index}"
    FunctionID  string   `json:"function_id"`
    FileID      string   `json:"file_id"`
    BlockIndex  int      `json:"block_index"`  // 0-based, topological order
    StartLine   int      `json:"start_line"`
    StartColumn int      `json:"start_column"`
    EndLine     int      `json:"end_line"`
    EndColumn   int      `json:"end_column"`
    StmtCount   int      `json:"stmt_count"`
    IsEntry     bool     `json:"is_entry"`
    IsExit      bool     `json:"is_exit"`
    IsDead      bool     `json:"is_dead"`       // unreachable from entry
    BlockKind   string   `json:"block_kind"`    // "normal", "defer", "recover", "catch", "finally"
}

type CFGEdgeFact struct {
    FromBlockID string `json:"from_block_id"`
    ToBlockID   string `json:"to_block_id"`
    Condition   string `json:"condition"`       // "true", "false", "default", "fallthrough", "unconditional", "panic", "defer_return"
}

type StmtEdgeFact struct {
    FromNodeID string `json:"from_node_id"`    // ID of CallSite, ExecutionUnit, or Statement
    ToNodeID   string `json:"to_node_id"`
}

type DeferEdgeFact struct {
    FunctionID string `json:"function_id"`
    CallSiteID string `json:"call_site_id"`    // The deferred call
    DeferOrder int    `json:"defer_order"`      // LIFO ordering (0 = last deferred = first executed)
}
```

### 5.4 Edge Types

| Edge Label | From | To | Properties | Purpose |
|---|---|---|---|---|
| `NEXT` | `Block` | `Block` | `condition` (string) | Control flow between basic blocks |
| `NEXT_STMT` | `CallSite`/`ExecutionUnit`/`Statement` | `CallSite`/`ExecutionUnit`/`Statement` | (none) | Sequential ordering within a function |
| `CONTAINS_BLOCK` | `Function` | `Block` | (none) | Function-to-block containment |
| `DEFERS` | `Function` | `CallSite` | `defer_order` (int) | Deferred call registration, LIFO order |

**Interaction with existing edges:**

- `CALLS` edges remain inter-procedural (Function -> Function). They are orthogonal to CFG edges.
- `AT_CALLSITE` edges (Function -> CallSite) remain unchanged. The `CallSite` nodes gain `block_id` and `stmt_index` properties to connect them to the CFG.
- `SPAWNS` edges remain unchanged. `ExecutionUnit` nodes similarly gain `block_id` and `stmt_index`.

### 5.5 Language-Specific CFG Patterns

#### 5.5.1 Go

**`if/else`:**
```
[predecessor] --unconditional--> [condition_eval]
[condition_eval] --true--> [then_body]
[condition_eval] --false--> [else_body | successor]
[then_body] --unconditional--> [successor]
[else_body] --unconditional--> [successor]
```

**`for` / `for range`:**
```
[predecessor] --unconditional--> [loop_header]
[loop_header] --true--> [loop_body]
[loop_header] --false--> [successor]
[loop_body] --unconditional--> [loop_header]  // back edge
```

`break` within a loop targets the successor block of the enclosing loop. `continue` targets the loop header. Labeled `break`/`continue` target the corresponding labeled loop's successor/header.

**`switch/case`:**
```
[predecessor] --unconditional--> [switch_head]
[switch_head] --"case_0"--> [case_0_body]
[switch_head] --"case_1"--> [case_1_body]
[switch_head] --"default"--> [default_body]
[case_N_body] --unconditional--> [successor]     // implicit break
[case_N_body] --"fallthrough"--> [case_N+1_body] // explicit fallthrough
```

Each case clause value is encoded in the `condition` property. When a case has multiple values, each gets a separate `NEXT` edge. If the case body contains `fallthrough`, a `"fallthrough"` edge connects to the next case body.

**`select`:**
Modeled identically to `switch` but with `"case_0"`, `"case_1"`, `"default"` conditions. Each case body is a separate block.

**`return`:**
A `return` statement terminates its basic block. If the function has deferred calls, the exit block connects to a synthetic defer chain (see below). If no defers, the block is marked `is_exit: true`.

**`defer`:**
Deferred calls are modeled with two mechanisms:

1. A `DEFERS` edge from the `Function` node to the deferred `CallSite`, with a `defer_order` property indicating LIFO position (0 = last deferred call = first to execute on return).
2. Synthetic `Block` nodes of `block_kind: "defer"` inserted between every exit point and the true function exit. Each return path passes through the defer blocks in LIFO order:

```
[return_block] --"defer_return"--> [defer_2_block] --"defer_return"--> [defer_1_block] --"defer_return"--> [defer_0_block] --unconditional--> [true_exit]
```

Because deferred calls always execute regardless of which `return` is taken, each return block connects to the **same** shared defer chain. This avoids duplication while accurately modeling that defers run on every exit path.

**`go` statement:**
The `go` statement does not alter the CFG of the enclosing function. It is already modeled as a `SPAWNS` edge. Within the CFG, the `go` statement is treated as a regular statement in its containing block (it does not branch).

**`panic/recover`:**
A `panic()` call terminates its basic block. The block gets an `is_exit: true` marker and a `condition: "panic"` edge to the defer chain (since deferred calls run even on panic). A `recover()` call inside a deferred function is noted via a `block_kind: "recover"` on the containing block, but since `recover` affects the deferred function's own CFG (not the panicking function's), no special cross-function edges are created in this intra-procedural spec.

**`goto` / labeled `break` / labeled `continue`:**
`goto` creates an unconditional edge from the current block to the block containing the target label. The CFG builder maintains a label-to-block mapping during construction. Labeled `break` and `continue` resolve to the successor/header block of the labeled loop/switch, respectively.

#### 5.5.2 JS/TS

**`if/else`:**
Same pattern as Go.

**`for` / `for-in` / `for-of`:**
Same pattern as Go `for`/`for range`.

**`while`:**
```
[predecessor] --unconditional--> [loop_header]
[loop_header] --true--> [loop_body]
[loop_header] --false--> [successor]
[loop_body] --unconditional--> [loop_header]
```

**`do-while`:**
```
[predecessor] --unconditional--> [loop_body]
[loop_body] --unconditional--> [loop_test]
[loop_test] --true--> [loop_body]
[loop_test] --false--> [successor]
```

**`switch/case`:**
Same pattern as Go, but JS `switch` has implicit fallthrough by default (no `break` = falls through). Each case body without a `break` gets a `"fallthrough"` edge to the next case body.

**`try/catch/finally`:**

```
[predecessor] --unconditional--> [try_body]
[try_body] --unconditional--> [finally_body | successor]  // normal completion
[try_body] --"exception"--> [catch_body]                   // exceptional
[catch_body] --unconditional--> [finally_body | successor]
[finally_body] --unconditional--> [successor]
```

The `catch` block is of `block_kind: "catch"`. The `finally` block is of `block_kind: "finally"`. Every statement within the `try` body has an implicit exceptional edge to the `catch` block. For CFG purposes, we model this as a single `"exception"` edge from the try body block to the catch block (not per-statement, to avoid edge explosion).

**`return` / `throw`:**
Both terminate their basic block. `throw` produces a `condition: "exception"` edge to the nearest enclosing `catch` block, or marks the block as an exceptional exit if no enclosing `try`.

**`async/await`:**
`await` expressions do not create branches in the intra-procedural CFG. They are treated as regular expressions within their containing statement. The suspension/resumption semantics are a runtime concern, not a static control flow concern. The `await` keyword is noted as a property on the containing `CallSite` node (`is_await: true`) for future async-aware analysis.

**`Promise.then/catch/finally` chains:**
These are method calls, not control flow constructs. They are already captured as `CallSite` nodes. The closures passed to `.then()`, `.catch()`, and `.finally()` are separate functions and get their own CFGs. No special CFG edges are needed within the calling function.

**Labeled `break` / `continue`:**
Same as Go: resolve to the successor/header of the labeled loop.

### 5.6 goraphdb Storage Design

Given the experimentally verified goraphdb limitations (no `IN` operator, no multi-hop explicit patterns, SKIP is buggy, array property filtering does not work), the storage design uses exclusively scalar properties.

#### Block Node Schema

| Property | Type | Indexed | Description |
|---|---|---|---|
| `id` | string | unique constraint | `"{function_id}#block:{block_index}"` |
| `kind` | string | no | Always `"Block"` |
| `function_id` | string | yes (index) | Owning function ID |
| `file_id` | string | yes (index) | Source file ID |
| `block_index` | int | no | Topological order within function |
| `start_line` | int | no | First line of block |
| `start_column` | int | no | First column |
| `end_line` | int | no | Last line |
| `end_column` | int | no | Last column |
| `stmt_count` | int | no | Number of statements |
| `is_entry` | bool | no | True for function entry block |
| `is_exit` | bool | no | True for function exit blocks |
| `is_dead` | bool | no | True if unreachable from entry |
| `block_kind` | string | no | `"normal"`, `"defer"`, `"recover"`, `"catch"`, `"finally"` |
| `scan_epoch` | string | no | For incremental cleanup |
| `producer` | string | no | Producer identifier |

#### Augmented CallSite / ExecutionUnit Properties

Existing `CallSite` and `ExecutionUnit` nodes gain two new scalar properties:

| Property | Type | Description |
|---|---|---|
| `block_id` | string | ID of the containing `Block` node |
| `stmt_index` | int | 0-based position within the block's statement list |

These properties allow joining CFG structure to call-level data without array properties or sub-node hierarchies.

#### NEXT Edge Schema

| Property | Type | Description |
|---|---|---|
| `condition` | string | `"true"`, `"false"`, `"default"`, `"fallthrough"`, `"unconditional"`, `"panic"`, `"defer_return"`, or `"case_N"` |
| `scan_epoch` | string | For incremental cleanup |
| `producer` | string | Producer identifier |

#### Example Queries Validated Against goraphdb Limitations

**All blocks for a function (no multi-hop needed):**
```cypher
MATCH (b:Block) WHERE b.function_id = 'pkg:main.HandleRequest' RETURN b ORDER BY b.block_index
```

**Reachability from entry block using variable-length path:**
```cypher
MATCH (entry:Block {function_id: 'pkg:main.HandleRequest', is_entry: true})-[:NEXT*1..50]->(reachable:Block)
RETURN reachable
```

**Find conditional branches from a specific block:**
```cypher
MATCH (b:Block {id: 'pkg:main.HandleRequest#block:3'})-[e:NEXT]->(target:Block)
RETURN target.block_index, e.condition
```

**Find write-then-read within a function using NEXT_STMT (variable-length):**
```cypher
MATCH (w:CallSite {callee_expr: 'db.Write'})-[:NEXT_STMT*1..100]->(r:CallSite {callee_expr: 'db.Read'})
WHERE w.caller_id = 'pkg:main.HandleRequest' AND r.caller_id = 'pkg:main.HandleRequest'
RETURN w, r
```
Note: The `WHERE` clause uses scalar equality (which works) and the path uses variable-length traversal (which works). No `IN` operator or multi-hop explicit patterns are needed.

**Find all exit blocks for a function:**
```cypher
MATCH (b:Block) WHERE b.function_id = 'pkg:main.HandleRequest' AND b.is_exit = true RETURN b
```

**Find deferred calls in LIFO order:**
```cypher
MATCH (f:Function {id: 'pkg:main.HandleRequest'})-[d:DEFERS]->(c:CallSite)
RETURN c, d.defer_order ORDER BY d.defer_order
```

### 5.7 NEXT_STMT Edge Construction

`NEXT_STMT` edges connect individual statements in control flow order across the entire function body. They are distinct from `NEXT` edges (which connect blocks) and provide the fine-grained ordering that the tag propagation engine needs.

**Construction algorithm:**

1. Within each basic block, statements are numbered 0..N-1. Connect statement[i] -> statement[i+1] with `NEXT_STMT`.
2. At block boundaries, connect the last statement of the predecessor block to the first statement of each successor block (one `NEXT_STMT` per `NEXT` edge).
3. For blocks with zero statements (e.g., empty else branches), the `NEXT_STMT` chain skips directly from the predecessor block's last statement to the successor block's first statement.

**Trade-off analysis:**

- A 50-line Go function with 20 statements generates approximately 19 `NEXT_STMT` edges (nearly one per statement pair). For a 10,000-function codebase averaging 15 statements per function, that is approximately 140,000 `NEXT_STMT` edges.
- This is a significant number of edges but is manageable because: (a) each edge is simple (no properties other than `scan_epoch` and `producer`), (b) traversals using `[:NEXT_STMT*1..N]` with bounded depth and `WHERE` filters on `caller_id` remain fast, and (c) the feature is opt-in.
- The `stmt_edges: false` configuration option allows disabling `NEXT_STMT` edges for projects that only need block-level CFGs.

### 5.8 Performance Considerations

**Node and edge count estimates:**

| Metric | Per Function (typical) | 10K-function codebase |
|---|---|---|
| Block nodes | 5-15 | 50K-150K |
| NEXT edges | 6-20 | 60K-200K |
| NEXT_STMT edges | 10-30 | 100K-300K |
| DEFERS edges | 0-3 | ~5K |

With the existing graph containing roughly 10K Function nodes, 30K CallSite nodes, and 50K edges, adding CFGs would approximately triple the total graph size. This is a meaningful increase.

**Mitigation strategies:**

1. **Opt-in**: CFG construction is disabled by default. Projects enable it when they need intra-procedural analysis.
2. **max_function_lines**: Functions exceeding the configured line limit (default 500) are skipped. These are typically generated code or lookup tables, not useful for CFG analysis.
3. **Incremental rebuild**: Only functions in changed files are re-processed. The `retirePriorEpochFacts` mechanism already handles cleanup of stale nodes/edges by `scan_epoch`.
4. **Index on function_id**: A property index on `Block.function_id` ensures that queries scoped to a single function remain fast regardless of total block count.
5. **Bounded traversals**: All recommended query patterns use bounded variable-length paths (e.g., `*1..50`) rather than unbounded `*`, preventing runaway graph exploration.

**goraphdb capacity**: goraphdb uses an embedded key-value store (similar to BadgerDB/BoltDB). At the estimated scale of ~500K total nodes and ~800K total edges for a large codebase with CFGs enabled, this remains well within the capacity of embedded graph databases. Memory usage scales linearly with node/edge count. The primary concern is query latency on unbounded traversals, which is addressed by bounded depth and function-scoped filters.

### 5.9 CFG Builder Architecture

The CFG builder is a new package within `codeflowmvp`:

```
backend/internal/codeflowmvp/
    cfg_builder.go          // Core CFG construction algorithm
    cfg_builder_go.go       // Go-specific tree-sitter CFG patterns
    cfg_builder_jsts.go     // JS/TS-specific tree-sitter CFG patterns
    cfg_builder_test.go     // Unit tests
    cfg_model.go            // BlockFact, CFGEdgeFact, StmtEdgeFact, DeferEdgeFact
```

**Core algorithm (per function):**

1. Receive the tree-sitter subtree for the function body and the list of already-extracted `CallSiteFact` and `SpawnSiteFact` entries for this function.
2. Walk the CST top-down, identifying **leaders** (statements that begin new basic blocks):
   - The first statement of the function body.
   - Any statement that is the target of a branch (if-then, if-else, loop header, case clause, catch, finally).
   - Any statement immediately following a branch (the statement after an if/else, the statement after a loop).
3. Partition statements into basic blocks based on leaders.
4. For each block, record its source range and count of statements.
5. Build `NEXT` edges based on the control flow structure discovered during the walk.
6. Assign `block_id` and `stmt_index` to each `CallSiteFact` and `SpawnSiteFact` by matching their source positions to the containing block.
7. Build `NEXT_STMT` edges by chaining statements in control flow order.
8. For Go functions with `defer` statements, build the defer chain and `DEFERS` edges.
9. Run a reachability pass from the entry block; mark unreachable blocks as `is_dead: true`.
10. Return the `BlockFact`, `CFGEdgeFact`, `StmtEdgeFact`, and `DeferEdgeFact` slices.

### 5.10 Incremental Updates

The CFG is scoped per function. When a file changes:

1. The existing scan pipeline re-extracts facts for all functions in the changed file.
2. The CFG builder re-builds CFGs for those functions.
3. Persistence uses the `scan_epoch` mechanism to retire stale `Block` nodes, `NEXT` edges, `NEXT_STMT` edges, `CONTAINS_BLOCK` edges, and `DEFERS` edges from the prior epoch.
4. Functions in unchanged files retain their existing CFG data (their `scan_epoch` matches the current run).

This requires adding `Block` to the list of labels cleaned up in `retirePriorEpochFacts`.

## 6. Security & Privacy

No new security or privacy implications. CFG construction operates entirely on static source code within the existing CodeFlow execution sandbox. The new node and edge types are queryable through the same read-only Cypher endpoint with the same access controls.

The only consideration is **resource exhaustion**: a malicious or auto-generated source file with a pathologically complex function (thousands of nested branches) could produce an extremely large CFG. The `max_function_lines` configuration limit mitigates this. Additionally, the CFG builder should enforce an absolute ceiling (e.g., 10,000 blocks per function) and abort construction if exceeded, logging a warning.

## 7. Testing Plan

### 7.1 Unit Tests

**CFG builder core (cfg_builder_test.go):**

- **Simple linear function**: No branches. Produces 1 block, 0 `NEXT` edges.
- **Single if/else**: Produces 4 blocks (entry, then, else, exit), 4 `NEXT` edges.
- **For loop**: Produces 3 blocks (entry+header, body, exit), with a back edge.
- **Nested if inside for**: Verify correct block count and edge structure.
- **Multiple returns**: Each return terminates its block; all are exit blocks.
- **Empty function**: 1 block (entry+exit), 0 `NEXT` edges.

**Go-specific (cfg_builder_go.go):**

- **defer**: Verify defer chain blocks are created, `DEFERS` edges have correct LIFO order, all return paths pass through defer chain.
- **switch/case with fallthrough**: Verify `"fallthrough"` edges.
- **select**: Verify case blocks.
- **goto**: Verify unconditional edge to target label block.
- **labeled break/continue**: Verify edges target the correct loop's successor/header.
- **panic in function with defers**: Verify panic block connects to defer chain.
- **Multiple defers**: Verify LIFO ordering (last defer registered = first executed = `defer_order: 0`).

**JS/TS-specific (cfg_builder_jsts.go):**

- **try/catch/finally**: Verify exception edge from try body to catch, normal edge from try to finally.
- **do-while**: Verify body executes before condition check.
- **switch without break**: Verify implicit fallthrough edges.
- **throw**: Verify exceptional exit or edge to nearest catch.
- **Nested try/catch**: Verify inner catch handles inner exceptions, outer catch handles outer.

### 7.2 Property-Based Tests

Using Go's `testing/quick` or a property-based testing library:

1. **Entry block invariant**: For any function's CFG, there is exactly one block with `is_entry: true`.
2. **Exit block invariant**: For any function's CFG, there is at least one block with `is_exit: true`.
3. **Reachability invariant**: Every block with `is_dead: false` is reachable from the entry block via `NEXT` edges.
4. **NEXT_STMT connectivity**: Within a function, the set of nodes connected by `NEXT_STMT` edges forms a DAG (no cycles) unless the function contains loops (in which case cycles are expected).
5. **Block index uniqueness**: Within a function, all `block_index` values are unique.
6. **Statement index uniqueness**: Within a block, all `stmt_index` values are unique and contiguous from 0.

### 7.3 Round-Trip Tests

1. Build CFG for a Go fixture function.
2. Persist to goraphdb.
3. Query back using Cypher: `MATCH (b:Block) WHERE b.function_id = '...' RETURN b`.
4. Verify the returned blocks match the expected count, properties, and connectivity.
5. Repeat for `NEXT` edges and `NEXT_STMT` edges.
6. Modify the fixture source file, re-scan, and verify that stale CFG nodes are cleaned up and new ones are correct.

### 7.4 Fixture Files

Create test fixtures in `backend/internal/codeflowmvp/testdata/cfg/`:

- `simple.go` — linear function, if/else, for loop, switch
- `defers.go` — multiple defers, defer with panic, defer in loop
- `goto.go` — labeled statements, goto, labeled break/continue
- `simple.ts` — linear function, if/else, for loop, switch
- `trycatch.ts` — try/catch/finally, nested try, throw
- `async.ts` — async/await, Promise chains

## 8. Rollout & Deployment

### 8.1 Database Changes

- New node label: `Block` with unique constraint on `id`.
- New edge labels: `NEXT`, `NEXT_STMT`, `CONTAINS_BLOCK`, `DEFERS`.
- Existing `CallSite` and `ExecutionUnit` nodes gain `block_id` and `stmt_index` properties (non-breaking addition).
- A property index on `Block.function_id` for fast per-function queries.

No destructive migrations. Existing graphs continue to work. CFG data is additive.

### 8.2 Feature Flag

CFG construction is gated behind `cfg.enabled` in `codeflow.yaml`. Default is `false`. This means:

- Existing users see no change until they opt in.
- The new code paths are exercised only when explicitly enabled.
- If bugs are discovered, users can disable CFGs without data loss (stale CFG nodes are cleaned up on the next scan with `cfg.enabled: false`).

### 8.3 Monitoring

- Log the count of blocks and edges produced per scan at INFO level.
- Log functions skipped due to `max_function_lines` at DEBUG level.
- Log functions where CFG construction was aborted due to the absolute block ceiling at WARN level.
- Track CFG construction time as a separate metric in the scan summary (new field `cfg_build_ms` in the JSON output).

## 9. Alternatives Considered

### 9.1 Statement-Level Granularity Only (No Basic Blocks)

Considered making every statement a node and connecting them with `NEXT` edges. Rejected because:
- Dramatically increases node count (every assignment, every expression statement becomes a node).
- Most structural analyses (dominance, loop detection) would need to reconstruct basic blocks anyway.
- goraphdb query performance is better with fewer nodes and bounded traversals.

The hybrid approach (block nodes + `NEXT_STMT` edges between existing statement-bearing nodes) captures the best of both worlds.

### 9.2 CFG as Node Properties Instead of Graph Structure

Considered encoding the CFG as properties on `Function` nodes (e.g., a JSON blob with the block graph). Rejected because:
- Cannot be queried with Cypher.
- Defeats the purpose of using a graph database.
- Makes the tag propagation engine impossible to implement via graph traversals.

### 9.3 Virtual CFG Computed at Query Time

Considered not persisting CFG edges at all, instead computing them on-the-fly when a query references `NEXT` edges. Rejected because:
- goraphdb has no mechanism for virtual/computed edges.
- Would require a custom query layer that intercepts and rewrites Cypher queries.
- Unacceptable latency for interactive queries.

### 9.4 Separate CFG Database

Considered storing the CFG in a separate, specialized data structure (e.g., an adjacency list in memory). Rejected because:
- Prevents cross-cutting queries that join CFG edges with call graph edges (which is the whole point of the read-after-write detection feature).
- Adds operational complexity of managing two data stores.

## 10. Implementation Plan

* [ ] Define `BlockFact`, `CFGEdgeFact`, `StmtEdgeFact`, `DeferEdgeFact` types in `cfg_model.go`.
* [ ] Add `cfg` configuration block to `codeflow.yaml` schema and parsing.
* [ ] Implement core CFG builder: leader identification, block partitioning, edge construction (`cfg_builder.go`).
* [ ] Implement Go-specific CFG patterns: `if/else`, `for/range`, `switch`, `select`, `defer`, `go`, `panic`, `goto`, labeled `break`/`continue` (`cfg_builder_go.go`).
* [ ] Implement JS/TS-specific CFG patterns: `if/else`, `for/for-in/for-of`, `while/do-while`, `switch`, `try/catch/finally`, `return/throw`, `async/await` (`cfg_builder_jsts.go`).
* [ ] Implement `NEXT_STMT` edge construction across block boundaries.
* [ ] Implement reachability analysis to mark dead blocks.
* [ ] Integrate CFG builder into the scan pipeline (call after fact extraction, before rules evaluation).
* [ ] Add `block_id` and `stmt_index` properties to `CallSite` and `ExecutionUnit` persistence.
* [ ] Add `Block` node persistence with unique constraint and `function_id` index.
* [ ] Add `NEXT`, `NEXT_STMT`, `CONTAINS_BLOCK`, `DEFERS` edge persistence.
* [ ] Update `retirePriorEpochFacts` to include `Block` nodes and new edge types.
* [ ] Create Go test fixtures and write unit tests for Go CFG patterns.
* [ ] Create JS/TS test fixtures and write unit tests for JS/TS CFG patterns.
* [ ] Write property-based tests for CFG invariants.
* [ ] Write round-trip persistence tests.
* [ ] Add CFG build timing to scan summary output.
* [ ] Update `ExtractionSummary` and `PersistenceSummary` with CFG counts.

## 11. Open Questions

1. **Should `NEXT_STMT` edges cross block boundaries or only exist within blocks?** The current design has them crossing block boundaries (connecting the last statement of one block to the first statement of successor blocks). This is necessary for the read-after-write detection use case but means `NEXT_STMT` edges partially duplicate the information in `NEXT` edges. Is the duplication acceptable, or should the tag propagation engine be responsible for following `NEXT` edges at block boundaries and `NEXT_STMT` edges within blocks?

2. **How should we handle `init()` functions in Go?** Go's `init()` functions execute implicitly before `main()`. They get normal CFGs, but should we create a synthetic `NEXT` edge from each `init()` exit to `main()` entry to support whole-program ordering queries? This is arguably inter-procedural and out of scope, but it is a natural extension.

3. **Should the `Statement` node type be introduced for non-call statements?** The current design only has `NEXT_STMT` edges between existing `CallSite` and `ExecutionUnit` nodes. Assignments, declarations, and other non-call statements that carry no effects are invisible to `NEXT_STMT`. This is intentional (the tag propagation engine only cares about effectful operations) but means the `NEXT_STMT` chain has gaps relative to the actual statement sequence. Is this acceptable, or do we need a lightweight `Statement` node for completeness?

4. **What is the maximum reasonable `NEXT_STMT` traversal depth?** The recommended query patterns use bounded depths like `*1..50` or `*1..100`. For very large functions (approaching the 500-line limit), the actual statement count could be 200+. Should we document a recommended maximum traversal depth and enforce it in the query API?

5. **Should CFG construction be parallelized per file?** The current scan pipeline processes files sequentially. CFG construction is CPU-bound and embarrassingly parallel per function. Should we parallelize it using a worker pool, or is the sequential approach fast enough given that tree-sitter parsing (which is already sequential) dominates the runtime?

# Customizable CodeFlow Analysis Rules

## 1. Introduction

CodeFlow is a graph-based representation of a codebase, modeling structure, control flow, data flow, and side effects. Different projects and frameworks have vastly different idioms — a function that represents a thread spawn in one framework (`go func()`) looks like a simple higher-order function in another (`Promise.then()`). An HTTP framework has its own way of registering routes. Certain data types are mere wrappers that should be semantically unwrapped.

To provide maximum utility without hardcoding every framework into the core analyzer, CodeFlow uses a customizable, language-agnostic mechanism to define how specific code structures should be represented in the graph.

**Core Philosophy**: The static analyzer is a generic engine. It does not "understand" what a "Validator" or a "Taint Sink" is. Instead, it applies project-specific rules to emit nodes with specific **Tags**, connect them with custom **Edge Types**, and annotate them with **Effect Markers**. Analysis — taint tracking, read-after-write detection, concurrency analysis, resource leak detection — emerges from querying the graph. The engine provides a reusable **Tag Propagation System** that flows effect markers through the call graph, bounded by configurable propagation boundaries.

## 2. The Three-Layer Architecture

CodeFlow analysis operates in three distinct layers, each with different responsibilities:

```
┌─────────────────────────────────────────────────────────┐
│  Layer 3: Analysis Queries                               │
│  "Find bugs by querying the graph"                       │
│  Input: enriched graph   Output: findings                │
│  Format: Cypher queries + explain templates               │
└────────────────────────┬────────────────────────────────┘
                         │ reads
┌────────────────────────▼────────────────────────────────┐
│  Layer 2: Graph Enrichment & Tag Propagation              │
│  "Derive higher-level facts from the base graph"          │
│  Includes: generic tag propagation engine                 │
│  Input: base graph   Output: new nodes/edges/tags         │
│  Format: graph-match → graph-write rules + propagation    │
└────────────────────────┬────────────────────────────────┘
                         │ reads + writes
┌────────────────────────▼────────────────────────────────┐
│  Layer 1: Fact Generation Rules                           │
│  "Turn AST matches into graph nodes/edges/tags"           │
│  Input: source code (AST)   Output: base graph            │
│  Format: YAML match rules                                 │
└─────────────────────────────────────────────────────────┘
```

**Layer 1** matches AST patterns and emits base graph facts (tags, edges, properties). It cannot reason across files or functions.

**Layer 2** operates on the populated graph. It derives higher-level facts through two mechanisms:
- **Enrichment rules**: graph-match → graph-write operations (e.g., link transaction starts to reachable exits)
- **Tag propagation**: a built-in engine that flows effect tags (READ, WRITE, LockAcquire, TaintSource, etc.) through the call graph, respecting closure semantics and halting at boundaries

**Layer 3** queries the enriched graph to find patterns and emit findings. Pure reads only.

## 3. Configuration Format

The configuration resides in a project-level `codeflow.yaml` file.

```yaml
version: "2"

# Layer 1: Fact generation from AST
rules:
  # ... (Section 4)

# Layer 2: Tag propagation configuration
propagation:
  # ... (Section 6)

# Layer 2: Graph enrichment rules
enrichment:
  # ... (Section 7)

# Layer 3: Analysis queries
analyses:
  # ... (Section 8)

# Frontend presentation
visuals:
  # ... (Section 9)
```

## 4. Layer 1: Fact Generation Rules

### 4.1 Structural Target Matching

Rules identify AST targets using a combination of:

- **`match.signature`**: Glob-like method/function matching
- **`match.kind`**: AST node type matching (for non-call patterns)
- **`match.tree_sitter`**: Raw S-expression queries (power-user escape hatch)
- **`match.context.ancestor`**: Structural nesting constraints
- **`match.package`**: Package-level matching

#### Signature Syntax

A signature string: `[Package/Module]::[Target]([Arguments]) -> [Returns]`

Where Target generalizes beyond OOP receivers:
- `Receiver.Method` — Go/Java/TS method call
- `Module.function` — Python/JS module function
- `function` — free function
- `@decorator` — decorator/annotation
- `keyword expression` — go, defer, await, yield

Wildcards: `*` for any segment, `**` for recursive package matching.

**Examples:**

```yaml
rules:
  # Match any Query method in database/sql
  - id: "sql_query"
    match:
      signature: "database/sql::*DB.Query(*)"
    node:
      tags: ["Read", "SQL"]

  # Match with parameter binding
  - id: "env_read"
    match:
      signature: "os::*.Getenv($key: StringLiteral)"
    node:
      identity_expansion: "$key"
      tags: ["Source", "Environment"]

  # Match Go keywords via kind
  - id: "go_spawn"
    match:
      kind: "go_statement"
      operand: "$fn: CallExpression"
    edge:
      type: "SPAWNS"
      from: "enclosing_function"
      to: "$fn"

  # Match with ancestor context
  - id: "spawn_in_loop"
    match:
      kind: "go_statement"
      context:
        ancestor:
          kind: ["for_statement", "range_over_clause"]
    node:
      tags: ["InLoop"]
```

### 4.2 Node Tagging

Tags are stored as **separate `Tag` nodes** connected to the tagged node via `HAS_TAG` edges, rather than as array properties. This is required because goraphdb's Cypher engine cannot filter on array property values (no `IN` operator).

```yaml
rules:
  - id: "mark_sql_write"
    match:
      signature: "database/sql::*DB.Exec(*)"
    node:
      tags: ["Write", "SQL"]
      properties:
        effect_domain: "sql"     # Qualifies the effect (see Section 6)
```

**Storage model:**
```
(CallSite {callee_expr: "db.Exec"})
  -[:HAS_TAG]-> (Tag {name: "Write", rule_id: "mark_sql_write", domain: "sql"})
  -[:HAS_TAG]-> (Tag {name: "SQL", rule_id: "mark_sql_write"})
```

This enables Cypher queries like:
```cypher
MATCH (cs:CallSite)-[:HAS_TAG]->(t:Tag {name: "Write"})
RETURN cs
```

### 4.3 Edge Emission

Rules can create relationships between matched elements:

```yaml
rules:
  - id: "mark_validator"
    match:
      signature: "myapp/validate::*.User($arg1)"
    edge:
      type: "VALIDATES"
      from: "$arg1"
      to: "self"
      confidence: 0.9

  - id: "custom_worker_pool"
    match:
      signature: "myapp/pool::*Worker.Submit($fn: Function)"
    edge:
      type: "SPAWNS"
      from: "self"
      to: "$fn"
```

### 4.4 Closure Execution Semantics

Higher-order functions that accept closures need semantic annotations describing how the closure is invoked. This is critical for the tag propagation engine (Layer 2).

```yaml
rules:
  - id: "with_retry"
    match:
      signature: "myapp/retry::*.WithRetry($fn: Function)"
    closure_semantics:
      parameter: "$fn"
      execution: "called_immediately"   # Blocks and executes synchronously
    # The wrapper inherits $fn's propagated effects

  - id: "async_pool_submit"
    match:
      signature: "myapp/pool::*.Submit($fn: Function)"
    closure_semantics:
      parameter: "$fn"
      execution: "spawned"              # Async execution in a new goroutine/thread
    # The wrapper does NOT inherit $fn's synchronous effects

  - id: "register_callback"
    match:
      signature: "myapp/events::*.On($event, $fn: Function)"
    closure_semantics:
      parameter: "$fn"
      execution: "stored"               # Saved for later invocation
    # Effects are not immediately inherited
```

**Execution modes:**

| Mode | Meaning | Effect inheritance |
|---|---|---|
| `called_immediately` | Wrapper blocks on closure | Wrapper inherits closure's effects |
| `spawned` | Closure runs async | Wrapper does NOT inherit synchronous effects; `SPAWNS` edge created |
| `stored` | Closure saved for later | No immediate inheritance; `STORES_CALLBACK` edge created |

When not explicitly configured, the analyzer attempts basic inference from the function body:
- Contains `go func()` → `spawned`
- Direct call to the parameter → `called_immediately`
- Assigned to a struct field or slice → `stored`
- Inference is best-effort with low confidence; explicit config is preferred.

### 4.5 Control Flow Assertions

```yaml
rules:
  - id: "http_abort"
    match:
      signature: "github.com/gin-gonic/gin::*Context.AbortWithError(*)"
    control_flow:
      terminates_execution: true
```

### 4.6 Data Flow Semantic Annotations

```yaml
rules:
  - id: "html_sanitizer"
    match:
      signature: "html::*.EscapeString($input) -> $output"
    data_flow:
      role: "sanitizer"
      categories: ["XSS"]
      from: "$input"
      to: "$output"

  - id: "sprintf_propagates"
    match:
      signature: "fmt::*.Sprintf($fmt, ...$args) -> $result"
    data_flow:
      role: "propagator"
      from: "$args"
      to: "$result"
```

### 4.7 Scope/Lifetime Facts

```yaml
rules:
  - id: "go_defer"
    match:
      kind: "defer_statement"
      call: "$fn: CallExpression"
    edge:
      type: "DEFERRED_CLEANUP"
      from: "enclosing_function"
      to: "$fn"
      properties:
        trigger: "function_exit"
        scope: "enclosing_function"

  - id: "python_context_manager"
    match:
      kind: "with_statement"
      context_expr: "$resource"
      body: "$block"
    edge:
      type: "SCOPED_RESOURCE"
      from: "$resource"
      to: "$block"
      properties:
        has_cleanup: true
```

### 4.8 Package-Level Matching

```yaml
rules:
  - id: "handler_layer"
    match:
      package: "myapp/handlers/**"
    node:
      tags: ["Layer:Handler"]

  - id: "internal_package"
    match:
      package: "**/internal/**"
    node:
      tags: ["Visibility:Internal"]
```

### 4.9 Propagation Boundary Declarations

Boundaries are functions or scopes where tag propagation halts. Without them, effects bubble up through the entire call graph until `main()` has every tag.

```yaml
rules:
  - id: "http_handler_boundary"
    match:
      kind: "function"
      signature: "net/http::*.ServeHTTP(*)"
    node:
      tags: ["Boundary"]
      properties:
        boundary_kind: "http_handler"

  - id: "test_boundary"
    match:
      kind: "function"
      signature: "testing::*T.Run($name, $fn)"
    node:
      tags: ["Boundary"]
      properties:
        boundary_kind: "test"
```

Any function tagged with `Boundary` halts upward tag propagation — callers of a boundary function do NOT inherit its propagated tags.

## 5. Tag Storage Model (goraphdb)

### 5.1 Why Separate Tag Nodes

goraphdb experimentally does **not** support:
- `IN` operator in Cypher (`WHERE 'Read' IN n.effects` fails)
- Filtering on array property elements (indexes match whole arrays only)
- List literals in Cypher

Therefore, tags are stored as separate `Tag` nodes:

```
(:Function)-[:HAS_TAG]->(:Tag {name: "Write", domain: "sql", source: "direct", rule_id: "mark_sql_write"})
(:Function)-[:HAS_TAG]->(:Tag {name: "Read",  domain: "sql", source: "propagated", propagated_from: "fn:repo/db.QueryUser"})
```

**Tag node properties:**

| Property | Type | Description |
|---|---|---|
| `name` | string | Tag name (e.g., "Write", "Read", "TaintSource") |
| `domain` | string | Optional qualifier (e.g., "sql", "redis", "filesystem") |
| `source` | string | "direct" (from Layer 1 rule) or "propagated" (from Layer 2 propagation) |
| `rule_id` | string | Which rule created this tag |
| `propagated_from` | string | For propagated tags: the original node ID that had the direct tag |
| `confidence` | float | 0.0–1.0 confidence score |
| `scan_epoch` | string | For incremental invalidation |
| `producer` | string | "codeflow-mvp" or "propagation/<rule_id>" |

### 5.2 Querying Tags

Because tags are nodes, standard Cypher works:

```cypher
-- Find all functions that perform writes
MATCH (fn:Function)-[:HAS_TAG]->(t:Tag {name: "Write"})
RETURN fn

-- Find functions that write to SQL specifically
MATCH (fn:Function)-[:HAS_TAG]->(t:Tag {name: "Write", domain: "sql"})
RETURN fn

-- Find functions that have BOTH Read and Write tags (potential read-after-write)
MATCH (fn:Function)-[:HAS_TAG]->(tw:Tag {name: "Write"})
MATCH (fn)-[:HAS_TAG]->(tr:Tag {name: "Read"})
RETURN fn, tw, tr

-- Find propagated tags and their origin
MATCH (fn:Function)-[:HAS_TAG]->(t:Tag {name: "Write", source: "propagated"})
RETURN fn.name, t.propagated_from
```

## 6. Layer 2a: Tag Propagation Engine

The tag propagation engine is a **built-in, generic mechanism** that flows effect tags upward through the call graph. It is not specific to any particular analysis — read-after-write, taint tracking, lock analysis, and resource lifetime tracking all use the same engine with different tag names and configurations.

### 6.1 Propagation Configuration

```yaml
propagation:
  # Define which tags should be propagated
  tag_sets:
    - id: "side_effects"
      tags: ["Read", "Write"]
      # Tags in this set propagate together
      # A function calling a Write-tagged callee gets Write propagated

    - id: "taint"
      tags: ["TaintSource", "TaintSink", "Sanitizer"]
      propagate_only: ["TaintSource"]
      # Only TaintSource propagates upward; Sink and Sanitizer are leaf markers

    - id: "locks"
      tags: ["LockAcquire", "LockRelease"]

    - id: "resources"
      tags: ["ResourceOpen", "ResourceClose"]

  # Global propagation settings
  max_depth: 50              # Maximum call-chain depth for propagation
  respect_boundaries: true   # Halt at Boundary-tagged nodes (default: true)
  propagate_domains: true    # Carry domain qualifiers through propagation
```

### 6.2 Propagation Algorithm

The engine runs bottom-up after Layer 1 fact generation and CFG construction:

```
1. Collect all nodes with direct tags (from Layer 1 rules)
2. Build reverse call graph: for each function, who calls it?
3. For each tag_set in propagation.tag_sets:
   a. Initialize worklist with all directly-tagged nodes
   b. While worklist is not empty:
      i.   Pop node N from worklist
      ii.  For each caller C of N:
           - If C has tag "Boundary": skip (propagation halts)
           - Check closure semantics of the call from C to N:
             * called_immediately: propagate tags to C
             * spawned: do NOT propagate (create SPAWNS edge instead)
             * stored: do NOT propagate (create STORES_CALLBACK edge instead)
           - If C does not already have N's propagated tags:
             * Create Tag node with source="propagated", propagated_from=N.id
             * Add C to worklist
4. Persist all created Tag nodes and HAS_TAG edges
```

### 6.3 Domain-Qualified Propagation

Tags carry an optional `domain` that qualifies what system they apply to. This prevents false positives where a Redis write followed by a PostgreSQL read is flagged as a read-after-write.

```yaml
rules:
  - id: "redis_write"
    match:
      signature: "github.com/redis/go-redis::*.Set(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "redis"

  - id: "postgres_read"
    match:
      signature: "database/sql::*DB.Query(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "postgres"
```

When propagating, the domain is preserved. Analysis queries can then filter by matching domains:

```cypher
-- Only flag read-after-write within the same domain
MATCH (fn:Function)-[:HAS_TAG]->(tw:Tag {name: "Write"})
MATCH (fn)-[:HAS_TAG]->(tr:Tag {name: "Read"})
WHERE tw.domain = tr.domain
RETURN fn, tw.domain
```

### 6.4 Propagation Boundaries

Boundaries separate "propagation halting" from "query anchoring":

- **Propagation boundary**: Tags do not flow past this function to its callers. Controlled by the `Boundary` tag on function nodes.
- **Query anchor**: A node used as the starting point in analysis queries. Any node can be a query anchor regardless of boundary status.

These are distinct concepts. A function can be a query anchor without being a propagation boundary, and vice versa.

## 7. Layer 2b: Graph Enrichment Rules

Enrichment rules match patterns in the graph and write new derived nodes/edges. They run after tag propagation.

```yaml
enrichment:
  - id: "transaction_scope"
    description: "Link transaction starts to all reachable exits"
    phase: 1
    match: |
      MATCH (start:CallSite)-[:HAS_TAG]->(t:Tag {name: "TransactionStart"})
      MATCH (fn:Function)-[:AT_CALLSITE]->(start)
      MATCH path = (start)-[:NEXT_STMT*]->(exit)
      WHERE exit.is_return = true
    write: |
      MERGE (scope:TransactionScope {start_id: start.id, function_id: fn.id})
      MERGE (scope)-[:STARTS_AT]->(start)
      MERGE (scope)-[:EXITS_VIA {
        exit_type: CASE
          WHEN EXISTS((exit)-[:HAS_TAG]->(:Tag {name: "TransactionCommit"})) THEN "commit"
          WHEN EXISTS((exit)-[:HAS_TAG]->(:Tag {name: "TransactionRollback"})) THEN "rollback"
          ELSE "unhandled_exit"
        END
      }]->(exit)

  - id: "taint_propagation"
    description: "Propagate taint through data flow"
    phase: 2
    match: |
      MATCH (src)-[:HAS_TAG]->(st:Tag {name: "TaintSource"})
      MATCH (src)-[:FLOWS_TO]->(call:CallSite)
      MATCH (call)-[:CALLS]->(fn:Function)
      MATCH (fn)-[:HAS_TAG]->(:Tag {name: "Propagator"})
    write: |
      MERGE (call)-[:HAS_TAG]->(t:Tag {
        name: "Tainted",
        source: "enrichment",
        taint_origin: src.id
      })
```

**Key principles:**
- Enrichment rules declare a `phase` number; phase N reads facts from phases 1..N-1.
- Enrichment facts carry `producer: "enrichment/<rule-id>"` and `scan_epoch` metadata.
- Enrichment is idempotent (uses MERGE semantics).
- No findings in enrichment — that belongs to Layer 3.

## 8. Layer 3: Analysis Queries

Analysis queries are read-only Cypher queries that find patterns and emit findings.

```yaml
analyses:
  - id: "read_after_write_same_domain"
    severity: "medium"
    description: "Read follows write to the same data system within a single boundary"
    query: |
      MATCH (b:Function)-[:HAS_TAG]->(:Tag {name: "Boundary"})
      MATCH (b)-[:CALLS*1..10]->(wfn:Function)-[:HAS_TAG]->(wt:Tag {name: "Write"})
      MATCH (b)-[:CALLS*1..10]->(rfn:Function)-[:HAS_TAG]->(rt:Tag {name: "Read"})
      WHERE wt.domain = rt.domain
      RETURN b, wfn, rfn, wt.domain AS domain
    explain: "Function {{b.name}} performs a write (via {{wfn.name}}) then read (via {{rfn.name}}) on {{domain}}"

  - id: "transaction_leak"
    severity: "critical"
    query: |
      MATCH (scope:TransactionScope)-[ev:EXITS_VIA]->(exit)
      WHERE ev.exit_type = "unhandled_exit"
      RETURN scope, exit
    explain: "Transaction started at {{scope.start_id}} has an exit path without commit or rollback"

  - id: "goroutine_leak"
    severity: "high"
    query: |
      MATCH (fn:Function)-[:SPAWNS]->(eu:ExecutionUnit)
      OPTIONAL MATCH (eu)-[:JOINED_BY]->(j:CallSite)
      WHERE j IS NULL
      RETURN fn, eu
    explain: "Goroutine spawned in {{fn.name}} is never joined or cancelled"
```

### 8.1 Parameterized Analysis Templates

```yaml
analysis_templates:
  - id: "resource_leak"
    parameters:
      acquire_tag: string
      release_tag: string
    query: |
      MATCH (acq:CallSite)-[:HAS_TAG]->(:Tag {name: $acquire_tag})
      MATCH (acq)-[rp:RESOURCE_PATH]->(exit)
      WHERE rp.has_release = false
      RETURN acq, exit

analyses:
  - id: "transaction_leak"
    template: "resource_leak"
    params: { acquire_tag: "TransactionStart", release_tag: "TransactionCommit" }
    severity: "critical"

  - id: "file_handle_leak"
    template: "resource_leak"
    params: { acquire_tag: "FileOpen", release_tag: "FileClose" }
    severity: "high"
```

## 9. Frontend & Query Integration

```yaml
visuals:
  nodes:
    - match_tag: "Sink"
      shape: "octagon"
      color: "darkred"
    - match_tag: "Boundary"
      shape: "diamond"
      color: "blue"
  edges:
    - match_type: "SPAWNS"
      stroke_dash: "dashed"
    - match_type: "HAS_TAG"
      visible: false    # Hide tag edges by default
  queries:
    - id: "taint_paths"
      query: |
        MATCH (s)-[:HAS_TAG]->(:Tag {name: "TaintSource"})
        MATCH (t)-[:HAS_TAG]->(:Tag {name: "TaintSink"})
        MATCH p=(s)-[*1..15]->(t)
        RETURN p
      presentation:
        path_color: "red"
        highlight: true
```

## 10. Worked Example: Read-After-Write Detection

This demonstrates how all three layers work together to detect read-after-write patterns without any special-purpose code.

### Step 1: Layer 1 Rules (codeflow.yaml)

```yaml
rules:
  - id: "sql_exec_write"
    match:
      signature: "database/sql::*DB.Exec(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "sql"

  - id: "sql_query_read"
    match:
      signature: "database/sql::*DB.Query(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "sql"

  - id: "redis_set_write"
    match:
      signature: "github.com/redis/go-redis::*.Set(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "redis"

  - id: "redis_get_read"
    match:
      signature: "github.com/redis/go-redis::*.Get(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "redis"

  - id: "http_handler_boundary"
    match:
      kind: "function"
      context:
        ancestor:
          pattern: "net/http::*.ServeHTTP(*)"
    node:
      tags: ["Boundary"]
      properties:
        boundary_kind: "http_handler"

propagation:
  tag_sets:
    - id: "side_effects"
      tags: ["Read", "Write"]
  respect_boundaries: true
  propagate_domains: true
```

### Step 2: Layer 2 — Tag Propagation (automatic)

Given this call chain:
```
HandleRequest (Boundary)
  → CreateUser
    → db.Exec("INSERT ...")   # Tagged: Write, domain=sql
    → db.Query("SELECT ...")  # Tagged: Read, domain=sql
```

The propagation engine:
1. `db.Exec` has direct tag `Write(sql)` → propagate to `CreateUser`
2. `db.Query` has direct tag `Read(sql)` → propagate to `CreateUser`
3. `CreateUser` now has propagated tags `Write(sql)` and `Read(sql)`
4. `HandleRequest` is a Boundary → propagation halts

### Step 3: Layer 3 — Analysis Query

```cypher
MATCH (fn:Function)-[:HAS_TAG]->(tw:Tag {name: "Write"})
MATCH (fn)-[:HAS_TAG]->(tr:Tag {name: "Read"})
WHERE tw.domain = tr.domain
RETURN fn.name, tw.domain
```

Result: `CreateUser, sql` — flagged for read-after-write on the same data domain.

For finer-grained ordering (write *before* read within the function), the CFG's `NEXT_STMT` edges enable:

```cypher
MATCH (wcs:CallSite)-[:HAS_TAG]->(:Tag {name: "Write", domain: "sql"})
MATCH (rcs:CallSite)-[:HAS_TAG]->(:Tag {name: "Read", domain: "sql"})
MATCH (wcs)-[:NEXT_STMT*1..20]->(rcs)
RETURN wcs, rcs
```

## 11. Future Extensibility

- **Runtime Tracing**: Dynamic analysis tools emit `OBSERVED_CALL` edges overlaying the static graph. Visual rules and queries work unchanged.
- **Code Coverage**: Coverage data maps to properties on File/Function nodes.
- **Cross-Service Analysis**: API request/handler matching (already in the graph via `REQUESTS_HANDLER` edges) can extend read-after-write detection across service boundaries.
- **Starlark Configuration**: If YAML proves insufficient for complex rule libraries, Starlark can be evaluated as an alternative rule authoring format (see implementation-plan.md Phase 10).

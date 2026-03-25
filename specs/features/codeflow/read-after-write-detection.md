# CodeFlow Generic Tag Propagation Engine

## 1. Summary

The CodeFlow analysis engine includes a **generic tag propagation engine** that flows effect markers (tags) upward through the call graph. Tags such as `Read`, `Write`, `TaintSource`, `LockAcquire`, `ResourceOpen`, etc. are assigned to leaf call sites by Layer 1 rules, then automatically propagated to calling functions. The engine respects closure execution semantics (immediate, spawned, stored), halts at configurable boundaries, carries domain qualifiers to prevent cross-domain false positives, and supports incremental re-propagation when underlying facts change.

Read-after-write detection is the **first use case** that validates this engine, but the same mechanism supports taint tracking, lock-hold analysis, resource lifetime tracking, and any future tag-based analysis without additional engine changes.

## 2. Motivation

Many important code quality patterns require knowing which side effects a function transitively performs:

- **Read-after-write**: A function writes to a data store then reads from the same store (potential stale read)
- **Taint propagation**: User input flows through functions to a sensitive sink without sanitization
- **Lock ordering**: Functions that hold locks call other functions that acquire different locks
- **Resource lifetime**: A function opens a resource; all exit paths must close it

All of these follow the same structural pattern: assign semantic tags at leaves, propagate upward through the call graph, query for patterns in the propagated tags. Building separate engines for each analysis would duplicate the propagation logic, closure handling, boundary semantics, and incremental invalidation. A single generic engine eliminates this duplication.

Without automation, tracking these patterns manually through complex abstraction layers, helper wrappers, and asynchronous execution paths is error-prone and prohibitively tedious.

## 3. Scope

**In Scope:**
- Generic tag propagation engine that flows arbitrary named tags through the call graph
- Configuration syntax for declaring tag sets, propagation rules, boundaries, and closure semantics
- Domain-qualified tags to distinguish same-named effects on different data systems (e.g., `Write:sql` vs `Write:redis`)
- Tag storage as separate `Tag` nodes with `HAS_TAG` edges (required by goraphdb limitations)
- Incremental re-propagation when base tags change
- Read-after-write detection as a first-class rule pack using the generic engine
- Exposure of propagated tags for arbitrary Cypher querying

**Out of Scope:**
- Active alerting, CI failures, or UI warnings based on propagated tags (handled by Layer 3 analysis queries)
- Intra-procedural data flow analysis (track which specific variable carries the taint — deferred to full DFA engine)
- Cross-service propagation through HTTP request/response boundaries (future work, builds on existing API request/handler matching)
- Runtime/dynamic tag assignment (future work)

## 4. Requirements & User Experience

### 4.1 Configuration

Developers update their project's `codeflow.yaml` to:
1. Map specific library calls to effect tags (Layer 1 rules)
2. Declare which tags should be propagated and how (propagation config)
3. Mark boundary functions where propagation halts
4. Annotate higher-order functions with closure execution semantics

### 4.2 Tag Assignment (Layer 1)

```yaml
rules:
  # Database writes
  - id: "sql_exec_write"
    match:
      signature: "database/sql::*DB.Exec(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "sql"

  - id: "sql_exec_context_write"
    match:
      signature: "database/sql::*DB.ExecContext(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "sql"

  # Database reads
  - id: "sql_query_read"
    match:
      signature: "database/sql::*DB.Query(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "sql"

  - id: "sql_query_row_read"
    match:
      signature: "database/sql::*DB.QueryRow(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "sql"

  # Cache operations
  - id: "redis_set_write"
    match:
      signature: "github.com/redis/go-redis/v9::*.Set(*)"
    node:
      tags: ["Write"]
      properties:
        effect_domain: "redis"

  - id: "redis_get_read"
    match:
      signature: "github.com/redis/go-redis/v9::*.Get(*)"
    node:
      tags: ["Read"]
      properties:
        effect_domain: "redis"
```

### 4.3 Propagation Configuration

```yaml
propagation:
  tag_sets:
    - id: "side_effects"
      tags: ["Read", "Write"]

  max_depth: 50
  respect_boundaries: true
  propagate_domains: true
```

### 4.4 Boundary Configuration

```yaml
rules:
  - id: "http_handler_boundary"
    match:
      signature: "net/http::*.ServeHTTP(*)"
    node:
      tags: ["Boundary"]

  - id: "grpc_handler_boundary"
    match:
      kind: "function"
      context:
        implements: "google.golang.org/grpc::*.UnaryHandler"
    node:
      tags: ["Boundary"]

  - id: "test_function_boundary"
    match:
      signature: "testing::*T.Run($name, $fn)"
    node:
      tags: ["Boundary"]
```

### 4.5 Closure Semantics Configuration

```yaml
rules:
  - id: "with_transaction_immediate"
    match:
      signature: "myapp/db::*.WithTransaction($fn: Function)"
    closure_semantics:
      parameter: "$fn"
      execution: "called_immediately"

  - id: "errgroup_go_spawned"
    match:
      signature: "golang.org/x/sync/errgroup::*Group.Go($fn: Function)"
    closure_semantics:
      parameter: "$fn"
      execution: "spawned"
```

### 4.6 Querying

Users write Cypher queries in the CodeFlow Explorer to find patterns in propagated tags:

```cypher
-- Functions that both read and write to the same domain
MATCH (fn:Function)-[:HAS_TAG]->(tw:Tag {name: "Write"})
MATCH (fn)-[:HAS_TAG]->(tr:Tag {name: "Read"})
WHERE tw.domain = tr.domain
RETURN fn.name, tw.domain

-- Sequential write-then-read within a function (requires CFG NEXT_STMT edges)
MATCH (wcs:CallSite)-[:HAS_TAG]->(:Tag {name: "Write", domain: "sql"})
MATCH (rcs:CallSite)-[:HAS_TAG]->(:Tag {name: "Read", domain: "sql"})
MATCH (wcs)-[:NEXT_STMT*1..30]->(rcs)
RETURN wcs, rcs

-- Trace where a propagated Write tag originated
MATCH (fn:Function)-[:HAS_TAG]->(t:Tag {name: "Write", source: "propagated"})
RETURN fn.name, t.propagated_from, t.domain
```

## 5. System Design & Architecture

### 5.1 Tag Storage Model

Tags are stored as separate `Tag` nodes connected via `HAS_TAG` edges. This design is **required** by goraphdb's limitations:
- goraphdb has no `IN` operator in Cypher (cannot filter `WHERE 'Write' IN n.effects`)
- Array property indexes match whole arrays only, not individual elements
- goraphdb cannot parse list literals in Cypher

**Tag node schema:**

| Property | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique: `tag:<node_id>:<name>:<domain>` |
| `name` | string | yes | Tag name (e.g., "Write", "Read", "TaintSource") |
| `domain` | string | no | Effect qualifier (e.g., "sql", "redis", "filesystem") |
| `source` | string | yes | "direct" or "propagated" |
| `rule_id` | string | yes | Layer 1 rule that created the direct tag, or "propagation" |
| `propagated_from` | string | no | For propagated tags: node ID of the direct-tagged origin |
| `propagation_depth` | int | no | How many call-graph hops from the origin |
| `confidence` | float | no | 0.0–1.0; decreases with propagation depth |
| `scan_epoch` | string | yes | For incremental invalidation |
| `producer` | string | yes | "codeflow-mvp" (direct) or "propagation/<tag_set_id>" (propagated) |

**Edge schema:**

```
(:Function)-[:HAS_TAG]->(:Tag)
(:CallSite)-[:HAS_TAG]->(:Tag)
```

`HAS_TAG` edges carry `scan_epoch` and `producer` properties for stale-fact retirement.

### 5.2 Propagation Algorithm

```
PropagateTagSet(tagSet, graph, config):
  // Phase 1: Collect seeds
  seeds = graph.Query("MATCH (n)-[:HAS_TAG]->(t:Tag) WHERE t.name IN tagSet.tags AND t.source = 'direct' RETURN n, t")

  // Phase 2: Build reverse call graph
  reverseCallGraph = {}
  for each (caller, callee) in graph.Query("MATCH (c:Function)-[:CALLS]->(f:Function) RETURN c, f"):
    reverseCallGraph[callee].append(caller)

  // Phase 3: Propagate
  worklist = Queue(seeds)
  visited = Set()

  while worklist is not empty:
    (node, tag) = worklist.pop()
    key = (node.id, tag.name, tag.domain)
    if key in visited: continue
    visited.add(key)

    for each caller in reverseCallGraph[node]:
      // Check boundary
      if caller has tag "Boundary" and config.respect_boundaries:
        continue

      // Check closure semantics
      callEdge = graph.getEdge(caller, node, "CALLS")
      semantics = lookupClosureSemantics(callEdge, config)
      if semantics == "spawned" or semantics == "stored":
        continue  // Do not propagate through async/stored closures

      // Check depth
      newDepth = tag.propagation_depth + 1
      if newDepth > config.max_depth:
        continue

      // Create propagated tag
      propagatedTag = Tag {
        name: tag.name,
        domain: tag.domain,
        source: "propagated",
        propagated_from: tag.propagated_from or node.id,
        propagation_depth: newDepth,
        confidence: tag.confidence * 0.98,  // Slight decay per hop
        producer: "propagation/" + tagSet.id,
        scan_epoch: currentEpoch,
      }

      if not graph.hasTag(caller, propagatedTag.name, propagatedTag.domain):
        graph.createTag(caller, propagatedTag)
        worklist.push((caller, propagatedTag))
```

### 5.3 Closure Semantics Resolution

When the propagation engine encounters a call from function C to function N, it determines how N's closure parameters are executed:

1. **Explicit config** (highest priority): Check if any `closure_semantics` rule matches the call
2. **Body inference** (fallback): Analyze N's function body:
   - If N calls the closure parameter directly → `called_immediately`
   - If N passes closure to a `go` statement → `spawned`
   - If N assigns closure to a struct field/slice/map → `stored`
3. **Default**: `called_immediately` (conservative — propagates effects)

Inference is best-effort. When inference fails or has low confidence, the engine defaults to `called_immediately` to avoid missing real effects (at the cost of some false propagation).

### 5.4 Boundary Semantics

Boundaries serve **one purpose**: halting upward tag propagation. They do NOT affect:
- Tag assignment (a boundary function can have direct tags)
- Intra-function analysis (NEXT_STMT edges within a boundary function still work)
- Downward queries (you can query what a boundary function calls)
- Analysis queries (any node can be a query anchor)

This separation is intentional. A common need is to query across boundaries (e.g., "which HTTP handlers transitively perform writes?") while still preventing `main()` from accumulating every tag.

For cross-boundary queries, use `CALLS*` traversals in Layer 3 that explicitly cross boundaries:

```cypher
MATCH (handler:Function)-[:HAS_TAG]->(:Tag {name: "Boundary", boundary_kind: "http_handler"})
MATCH (handler)-[:CALLS*1..5]->(fn:Function)-[:HAS_TAG]->(t:Tag {name: "Write"})
RETURN handler.name, fn.name, t.domain
```

### 5.5 Domain-Qualified Propagation

Domains prevent false positives. Without domains, a function that writes to Redis and reads from PostgreSQL would be flagged as read-after-write. With domains:

```
CreateUser
  -[:HAS_TAG]-> Tag{name: "Write", domain: "sql", source: "propagated"}
  -[:HAS_TAG]-> Tag{name: "Read",  domain: "redis", source: "propagated"}
```

Analysis queries filter by matching domains:
```cypher
WHERE tw.domain = tr.domain
```

Domains propagate unchanged. A `Write:sql` tag remains `Write:sql` through the entire call chain.

When `propagate_domains: false`, domains are stripped during propagation (tags become unqualified). This is useful when domain granularity isn't needed.

## 6. Incremental Re-Propagation

When Layer 1 facts change (file edited, function modified), propagated tags may become stale.

### 6.1 Invalidation Strategy

Each propagated tag stores `propagated_from` — the node ID of the direct-tagged leaf that originated it. When a leaf's tags change:

1. Query all propagated tags with `propagated_from = <changed_node_id>`
2. Delete those propagated tags (they are stale)
3. Re-run propagation for the affected tag set, starting from the changed node

This is scoped: only the sub-graph reachable from the changed node is re-propagated, bounded by the nearest boundaries.

### 6.2 Epoch Management

Propagated tags use `producer: "propagation/<tag_set_id>"` and `scan_epoch`. The existing `retirePriorEpochFacts` mechanism handles stale propagated tags the same way it handles stale base facts.

For a full re-propagation: assign a new epoch, re-propagate all tag sets, retire tags from previous epochs.

For incremental re-propagation: assign a new epoch, re-propagate only affected sub-graphs, retire only the stale tags from those sub-graphs.

See `specs/features/codeflow/incremental-analysis.md` for the full incremental update design.

## 7. Security & Privacy

No new security or privacy implications. The engine operates entirely on static code analysis within the existing CodeFlow execution environment. Tag names, domains, and propagation rules are project-configured and contain no sensitive data.

## 8. Testing Plan

### 8.1 Unit Tests

- **Tag storage**: Create Tag nodes, query via Cypher, verify `HAS_TAG` edges work correctly
- **Propagation**: Given a mock call graph A→B→C where C has direct tag `Write:sql`:
  - Verify B gets propagated `Write:sql`
  - Verify A gets propagated `Write:sql`
  - Verify propagation_depth increments correctly
  - Verify propagated_from points to C
- **Boundary halting**: Add `Boundary` tag to B; verify A does NOT get propagated tags
- **Closure semantics**: Configure B as `spawned`; verify A does NOT get propagated tags
- **Domain preservation**: Verify `Write:sql` propagates as `Write:sql`, not unqualified `Write`
- **Max depth**: Set max_depth=2; verify propagation stops at depth 3

### 8.2 Integration Tests

Provide a mock Go project:
```go
// handlers/user.go
func HandleCreateUser(w http.ResponseWriter, r *http.Request) { // Boundary
    CreateUser(db, user)
}

// service/user.go
func CreateUser(db *sql.DB, user User) error {
    _, err := db.Exec("INSERT INTO users ...", user.Name)  // Write:sql
    if err != nil { return err }

    row := db.QueryRow("SELECT * FROM users WHERE ...")    // Read:sql
    return row.Scan(&user)
}

// service/cache.go
func InvalidateCache(rdb *redis.Client, key string) {
    rdb.Del(ctx, key)  // Write:redis
}
```

Run the analyzer and verify:
- `db.Exec` CallSite has direct tag `Write:sql`
- `db.QueryRow` CallSite has direct tag `Read:sql`
- `rdb.Del` CallSite has direct tag `Write:redis`
- `CreateUser` Function has propagated tags `Write:sql` and `Read:sql`
- `HandleCreateUser` does NOT have propagated tags (it's a Boundary)
- `InvalidateCache` has propagated tag `Write:redis` but NOT `Write:sql`

### 8.3 Analysis Query Tests

Verify that the read-after-write query:
```cypher
MATCH (fn:Function)-[:HAS_TAG]->(tw:Tag {name: "Write"})
MATCH (fn)-[:HAS_TAG]->(tr:Tag {name: "Read"})
WHERE tw.domain = tr.domain
RETURN fn.name, tw.domain
```
Returns exactly: `[CreateUser, sql]` — not `HandleCreateUser` (boundary), not cross-domain matches.

### 8.4 Incremental Tests

1. Remove the `db.Exec` call from `CreateUser`
2. Re-run incremental analysis
3. Verify `CreateUser` no longer has the propagated `Write:sql` tag
4. Verify the read-after-write query returns empty results

## 9. Rollout & Deployment

**Database Impact**: This feature adds `Tag` nodes and `HAS_TAG` edges to the graph. Existing graphs need re-analysis to populate tags. No destructive migrations required.

**Performance Impact**: Tag nodes increase the total node count. For a codebase with 10,000 functions and an average of 2 direct tags per function that propagate through an average chain depth of 5, expect ~100,000 additional Tag nodes and HAS_TAG edges. goraphdb should handle this within typical memory budgets.

**Rollout**: Deployed as an update to the CodeFlow analyzer engine. Projects opt-in by adding `propagation` and relevant `rules` to their `codeflow.yaml`. Without configuration, no tags are created and the engine is inert.

## 10. Alternatives Considered

### 10.1 Array Properties Instead of Tag Nodes

The original design stored effects as `effects: ["READ", "WRITE"]` array properties on Function/CallSite nodes. **Rejected** because goraphdb's Cypher engine has no `IN` operator and cannot filter on array elements. Array property indexes match whole arrays only.

### 10.2 Boolean Properties per Effect

Using scalar properties like `has_read: true`, `has_write: true` on each node. This works for Cypher filtering but doesn't scale: adding a new tag type requires schema changes, and domain qualification (`has_write_sql: true`) creates a combinatorial explosion of property names. Tag nodes are more flexible and self-describing.

### 10.3 Dedicated Propagation Edges

Creating `PROPAGATES_WRITE` or `HAS_EFFECT` edges from callers to the original tagged callsite. **Rejected** because it dramatically increases edge count and requires complex recursive Cypher traversals. Tag nodes on the functions themselves are faster to filter.

### 10.4 Bespoke Read-After-Write Engine

Building a special-purpose engine that only tracks READ/WRITE effects. **Rejected** because the same propagation logic is needed for taint tracking, lock analysis, resource lifetimes, etc. A generic engine avoids N separate implementations.

### 10.5 Lazy Query-Time Propagation

Computing propagated tags on-demand during Cypher queries (via recursive path traversals) instead of pre-computing them. This eliminates incremental invalidation complexity but makes queries expensive and complex. **Deferred** as a potential optimization for rarely-used tag sets, but pre-computation is the default for performance.

## 11. Implementation Plan

### Phase A: Tag Storage Infrastructure
- [ ] Define `Tag` node schema and `HAS_TAG` edge type in goraphdb constants
- [ ] Implement `createTag`, `deleteTag`, `hasTag`, `getTagsForNode` helpers in store.go
- [ ] Add unique constraint on Tag node `id` property
- [ ] Write unit tests for Tag CRUD and Cypher queries on tags

### Phase B: Layer 1 Tag Emission
- [ ] Extend the rule configuration schema to support `node.tags` and `node.properties.effect_domain`
- [ ] Implement tag emission in the rule evaluation pipeline (match rule → create Tag node + HAS_TAG edge)
- [ ] Extend the YAML parser to handle `closure_semantics` and `Boundary` tag rules
- [ ] Write fixture-based tests with the mock Go project from Section 8.2

### Phase C: Propagation Engine Core
- [ ] Implement the bottom-up propagation algorithm (Section 5.2)
- [ ] Implement closure semantics resolution (Section 5.3) — config lookup + basic body inference
- [ ] Implement boundary halting
- [ ] Implement domain-qualified propagation
- [ ] Implement max_depth limiting and confidence decay
- [ ] Write unit tests for each propagation behavior

### Phase D: Integration & Querying
- [ ] Wire propagation engine into the scan pipeline (runs after Layer 1, before Layer 2 enrichment)
- [ ] Implement the `propagation` config section parser
- [ ] Run end-to-end integration tests on the mock project
- [ ] Verify read-after-write Cypher queries produce correct results
- [ ] Add example `codeflow.yaml` snippets to documentation

### Phase E: Incremental Re-Propagation
- [ ] Implement `propagated_from` tracking and invalidation (Section 6.1)
- [ ] Integrate with epoch-based stale fact retirement
- [ ] Write incremental update tests (Section 8.4)

### Phase F: Rule Packs
- [ ] Create a `read-after-write.yaml` rule pack with common database/cache/filesystem rules
- [ ] Create a `taint-tracking.yaml` rule pack with common source/sink/sanitizer rules
- [ ] Document how to create custom rule packs

**Dependencies:**
- Phase A-D require no new infrastructure beyond existing goraphdb and rule system
- Phase D benefits from CFG `NEXT_STMT` edges (see `specs/features/codeflow/cfg-construction.md`) for intra-function sequential ordering, but function-level propagation works without them
- Phase E depends on the incremental analysis system (see `specs/features/codeflow/incremental-analysis.md`)

## 12. Open Questions

1. **Confidence decay rate**: The algorithm decays confidence by 0.98 per hop. Is this the right rate? Should it be configurable per tag set? Should it vary by closure semantics (lower confidence through inferred `called_immediately` than through explicit config)?

2. **Bidirectional propagation**: The current design only propagates upward (callee → caller). Some analyses might benefit from downward propagation (caller → callee, e.g., "this function is called in a test context"). Should the engine support configurable propagation direction?

3. **Tag conflicts**: If function F calls both G (tagged `Write:sql`) and H (tagged `Write:redis`), F gets two Write tags with different domains. Should there be a mechanism for tag merging or conflict resolution?

4. **Performance at scale**: For very large codebases (100K+ functions), the propagation algorithm's memory footprint could be significant. Should propagation be segmented by package/module to limit working set size?

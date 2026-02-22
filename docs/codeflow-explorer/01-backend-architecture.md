# CodeFlow Explorer — Backend Architecture Design

**Status:** Draft
**Audience:** Backend engineers
**Scope:** Server-side analysis pipeline, semantic model, and query API

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Client Layer                               │
│              REST (graph queries)  |  WebSocket (live findings)     │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────────┐
│                        API Gateway                                  │
│          router · auth middleware · rate limiter · paginator        │
└───────┬──────────────────┬───────────────────────┬──────────────────┘
        │                  │                       │
┌───────▼──────┐  ┌────────▼────────┐  ┌──────────▼────────┐
│  Query       │  │  Findings       │  │  Watch            │
│  Handler     │  │  Handler        │  │  Handler          │
└───────┬──────┘  └────────┬────────┘  └──────────┬────────┘
        │                  │                       │
┌───────▼──────────────────▼───────────────────────▼────────────────┐
│                     Semantic Model Store                           │
│          (graph DB — nodes, edges, properties, versions)           │
└──────────────────────────┬─────────────────────────────────────────┘
                           │
┌──────────────────────────▼─────────────────────────────────────────┐
│                     Analysis Orchestrator                           │
│  file watcher → invalidation → job queue → pipeline → writer       │
└──────┬─────────────┬────────────────┬────────────────┬─────────────┘
       │             │                │                │
┌──────▼───┐  ┌──────▼──────┐  ┌─────▼──────┐  ┌─────▼──────────┐
│  Parser  │  │  CFG/DFG    │  │  Pattern   │  │  Concurrency   │
│  & Index │  │  Builder    │  │  Engine    │  │  Analyzer      │
└──────────┘  └─────────────┘  └────────────┘  └────────────────┘
```

All analysis stages write to the Semantic Model Store. The orchestrator enforces stage ordering and parallelism budget. Clients read exclusively through the API gateway.

---

## 2. Analysis Pipeline Stages

### Stage 1 — Parse & Symbol Index

Entry point: `go/packages` loads packages with full type info (`NeedSyntax | NeedTypes | NeedDeps`). Each file produces an `*ast.File`; the type checker emits a `*types.Package`. A symbol indexer walks the typed AST and emits:

- Package, file, function, method, type, field, variable, constant nodes
- `DEFINES`, `IMPORTS`, `INSTANTIATES`, `IMPLEMENTS` edges

This stage runs per-package and is fully parallelizable across CPU cores.

### Stage 2 — SSA Construction

`golang.org/x/tools/go/ssa` converts typed AST into SSA form. `ssa.BuilderMode` is set to `SanityCheckFunctions | NaiveForm` for correctness. The SSA program is the shared substrate for CFG, DFG, and concurrency analysis.

### Stage 3 — CFG Construction

Intra-procedural CFGs are derived directly from `ssa.Function.Blocks`. Inter-procedural edges are added via a call graph built with `golang.org/x/tools/go/callgraph/rta` (Rapid Type Analysis) — fast enough for incremental builds while remaining sound for interface dispatch. Each `CALLS` edge carries call-site position and receiver type. Dynamic calls through function values use conservative pointer analysis (`go/pointer`) to resolve targets.

### Stage 4 — DFG / Taint Analysis

A backward-flow taint pass runs over SSA values. Each `ssa.Value` that escapes a trust boundary (e.g., HTTP request field, OS env) is marked as a taint source. The DFG tracks `FLOWS_TO` edges through assignments, struct field reads/writes, type assertions, and interface wrapping. Field-sensitive analysis uses `go/pointer`'s `AccessPath` to distinguish `req.Body` from `req.Header`. Taint sinks are declared in a policy file (YAML) mapping package paths to function parameters.

### Stage 5 — Anti-Pattern Detection

After CFG and DFG are written to the store, the pattern engine executes registered pattern queries as graph traversals. Findings are written as `FINDING` nodes linked to the relevant code nodes.

---

## 3. The Semantic Model

### Why a Graph DB

The semantic model is a property graph — nodes are code entities, edges are relationships, both carry typed properties. Queries span multiple hops (e.g., "find all goroutines spawned transitively from HTTP handlers that acquire a mutex before a channel send"). A relational schema forces expensive joins; a graph DB navigates these naturally. **Neo4j** (production) or **DGraph** (embedded, for single-node dev) are the primary targets, accessed via the **openCypher** query language through a thin interface so the backing store is swappable.

### Node Types

| Label | Key Properties |
|---|---|
| `Package` | path, module, checksum |
| `File` | path, hash, linesOfCode |
| `Function` | fqn, signature, isMethod, receiver |
| `Type` | fqn, kind (struct/interface/basic) |
| `Field` | name, typeRef, index |
| `SSAValue` | id, kind, position |
| `Goroutine` | spawnSite, label |
| `Channel` | direction, elementType, bufferSize |
| `Mutex` | varFqn, kind (sync.Mutex/RWMutex) |
| `Finding` | ruleID, severity, message, positions |

### Edge Types

| Type | From → To | Properties |
|---|---|---|
| `CALLS` | Function → Function | callSite, dynamic |
| `FLOWS_TO` | SSAValue → SSAValue | fieldPath, tainted |
| `SPAWNS` | Function → Goroutine | callSite |
| `SENDS_ON` / `RECEIVES_FROM` | Goroutine → Channel | position |
| `ACQUIRES` | Function → Mutex | order, position |
| `DEFINES` | Package/File → any | — |
| `FINDING_AT` | Finding → any node | — |

Schema version is stored as a graph metadata property; migrations run forward-only via versioned Cypher scripts.

---

## 4. Go-Specific Analysis

### SSA-Level Goroutine Tracking

`go` statements lower to `ssa.Go` instructions in SSA. The concurrency analyzer walks all functions reachable from package `init` and `main`, recording each `ssa.Go` as a `SPAWNS` edge. Spawn trees are built by recursive traversal of the call graph, annotated with the goroutine's lexical closure captures.

### Channel Topology

`make(chan T, n)` lowers to `ssa.MakeChan`; `n` is the buffer size (extractable as an `ssa.Const` or flagged as dynamic). Send/receive instructions (`ssa.Send`, `*ssa.UnOp` with `token.ARROW`) are linked to the channel's allocation site via pointer analysis. The resulting channel topology graph exposes unbuffered fan-in patterns and potential goroutine leaks (channel with no receiver reachable).

### Mutex Acquisition Order

Each function that contains a `sync.Mutex.Lock` or `sync.RWMutex.Lock` call has that call recorded as an `ACQUIRES` edge with a monotonically increasing `order` property scoped to the goroutine's execution context. Lock order inversion detection queries pairs of functions `(A, B)` where `A` acquires `m1` then `m2` and `B` acquires `m2` then `m1`, both reachable from distinct goroutines.

Alias analysis (`go/pointer`) resolves whether two `sync.Mutex` references at different call sites alias the same allocation, reducing false positives from conservative name-based matching.

---

## 5. The Anti-Pattern Engine

### Pattern DSL

Patterns are Go structs implementing a `Pattern` interface:

```go
type Pattern interface {
    ID() string
    Severity() Severity
    Query() CypherFragment
    Explain(match MatchResult) string
}
```

`CypherFragment` values are composable: a `GoroutineLeak` pattern composes a `SpawnsFragment` with a `NoReceiverFragment`. This allows new patterns to reuse library predicates without reimplementing graph traversal.

### Built-in Pattern Library

| Pattern | Detection Strategy |
|---|---|
| **Goroutine leak** | `SPAWNS` edge to goroutine; no reachable `RECEIVES_FROM` or context cancellation path |
| **Lock order inversion** | Pair of `ACQUIRES` sequences on the same mutex set, in different orders, from concurrent goroutines |
| **Unguarded concurrent map write** | `map` allocation reachable from ≥2 goroutines; write SSA op without enclosing mutex acquisition in dominator tree |
| **Unbounded channel** | `MakeChan` with `bufferSize = 0` where producer is in a loop with no backpressure consumer |
| **Trust boundary bypass** | Tainted `FLOWS_TO` path from source to sink crossing a `trustBoundary` edge without a sanitizer node |

---

## 6. Incremental Analysis

### File Change Detection

`fsnotify` watches the module root. Change events are debounced (150 ms window) and batched. Each changed file is hashed (SHA-256); a hash match against the stored `File.hash` skips re-analysis.

### Dependency Invalidation

A reverse dependency index maps each package to its direct importers (built during initial indexing). On change, the invalidation set is computed by BFS over this reverse graph up to a configurable depth (default: full transitive closure, capped at 500 packages for monorepos). Invalidated nodes are soft-deleted by bumping a `version` counter; queries default to `WHERE n.version = $current`.

### Partial Rebuild

Only invalidated packages re-enter the pipeline. SSA is rebuilt per-package; inter-procedural edges touching the invalidated set are deleted and recomputed. The pattern engine re-runs only patterns whose query graph overlaps the invalidated subgraph (tracked via pattern → node-label subscription).

---

## 7. API Design

### REST Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/graph/nodes` | List nodes with label/property filters |
| `GET` | `/v1/graph/nodes/:id/neighbors` | N-hop neighborhood |
| `POST` | `/v1/graph/query` | Read-only Cypher (allowlisted functions) |
| `GET` | `/v1/findings` | Paginated findings; filter by `ruleID`, `severity`, `file` |
| `GET` | `/v1/functions/:fqn/cfg` | CFG for a single function |
| `GET` | `/v1/functions/:fqn/callers` | N-hop callers |
| `GET` | `/v1/channels` | Channel topology summary |

Pagination uses cursor-based encoding (opaque base64 of last-seen node ID + sort key). Default page size: 50; max: 500.

### WebSocket Events

Clients subscribe at `/v1/ws` with a subscription message:

```json
{ "subscribe": ["findings", "graph:File:/path/to/file.go"] }
```

The server pushes on analysis completion:

```json
{ "event": "finding.new", "payload": { "ruleID": "goroutine-leak", "position": "..." } }
{ "event": "graph.updated", "payload": { "nodeIDs": ["fn:pkg.Foo", "ch:42"] } }
```

---

## 8. Performance Considerations

**Parallelism:** The orchestrator assigns one goroutine per package for parse/SSA stages, bounded by `GOMAXPROCS`. Inter-procedural passes (call graph, DFG) run sequentially after per-package stages complete. The pattern engine fans out one goroutine per pattern.

**Caching:** Parsed `*ast.File` and `*ssa.Function` objects are held in an LRU keyed by file hash; eviction threshold is 512 MB. Type-checked packages are cached by `(module, version)` tuple. The call graph is serialized as an edge list and invalidated when any reachable package changes.

**Memory Budget (1M+ LOC):** At 1M LOC, expect ~80K functions and ~400K SSA values. Each SSA value is ~200 bytes in-memory; the full SSA program fits in ~80 MB. The semantic graph stores ~2M nodes and ~8M edges; at ~150 bytes/node and ~80 bytes/edge, this is ~940 MB on disk. Set Neo4j page cache to 2 GB for sub-second traversal. Analysis workers are capped at 4 GB RSS via `runtime/debug.SetMemoryLimit`.

---

## 9. Technology Choices

| Component | Choice | Rationale |
|---|---|---|
| Analysis language | Go | First-class `go/packages`, `go/ssa`, `go/pointer` toolchain; same runtime as target code |
| Graph DB (prod) | Neo4j 5 (Community) | Mature openCypher support; APOC procedures for path algorithms; strong Go driver |
| Graph DB (dev/embed) | DGraph or in-process memgraph | Zero-ops for local runs and CI |
| File watcher | `fsnotify` | Cross-platform, battle-tested, low overhead |
| Job queue | In-process channel-based queue | Avoids external deps for MVP; replace with NATS JetStream at scale |
| API framework | `net/http` + `chi` router | Minimal, composable, no magic |
| WebSocket | `nhooyr.io/websocket` | Context-aware; avoids goroutine leaks common in `gorilla/websocket` |
| Query language | openCypher (read-only subset) | Declarative, graph-native; safer than raw Gremlin for a public API surface |
| Serialization | Protocol Buffers (internal) + JSON (API) | Compact wire format internally; human-readable externally |

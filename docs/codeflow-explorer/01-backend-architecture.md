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

Entry point: Tree-sitter parses each source file into an incremental concrete syntax tree (CST). A language-neutral symbol indexer walks CST nodes and emits:

- Package, file, function, method, type, field, variable, constant nodes
- `DEFINES`, `IMPORTS`, `INSTANTIATES`, `IMPLEMENTS` edges

This stage runs per-file and is fully parallelizable across CPU cores.

### Stage 2 — Normalized IR Construction

Tree-sitter CSTs are lowered into a language-agnostic intermediate representation (IR) with stable node kinds (`Function`, `Type`, `CallSite`, `Branch`, `Assignment`, `Spawn`, `SyncOp`). Per-language adapters only map syntax to this IR; downstream analyses consume the same IR regardless of source language.

### Stage 3 — CFG and Call Graph Construction

Intra-procedural CFGs are derived from normalized IR control nodes. Inter-procedural edges are added through a conservative call graph pass that resolves direct calls and best-effort dynamic dispatch. Each `CALLS` edge carries call-site position and receiver/type hints when available.

### Stage 4 — DFG and Taint Analysis

A backward-flow taint pass runs over IR values. Values that escape trust boundaries (e.g., HTTP request fields, environment input, external I/O) are marked as sources. The DFG tracks `FLOWS_TO` edges through assignments, field/element reads-writes, coercions, and wrapper calls. Field-sensitive tracking preserves paths such as `request.body` vs `request.headers`. Taint sinks are declared in a policy file (YAML) mapping symbol patterns to parameter positions.

### Stage 5 — Anti-Pattern Detection

After CFG and DFG are written to the store, the pattern engine executes registered pattern queries as graph traversals. Findings are written as `FINDING` nodes linked to the relevant code nodes.

---

## 3. The Semantic Model

### Why a Graph DB

The semantic model is a property graph — nodes are code entities, edges are relationships, both carry typed properties. Queries span multiple hops (e.g., "find all execution units spawned transitively from API entrypoints that acquire synchronization resources before message sends"). A relational schema forces expensive joins; a graph DB navigates these naturally. The storage layer is intentionally **DB-agnostic** behind a thin repository/query interface. For the current implementation, we standardize on **SurrealDB 3.0** as the only active backend.

### Node Types

| Label | Key Properties |
|---|---|
| `Package` | path, module, checksum |
| `File` | path, hash, linesOfCode |
| `Function` | fqn, signature, isMethod, receiver |
| `Type` | fqn, kind (struct/interface/basic) |
| `Field` | name, typeRef, index |
| `Value` | id, kind, position |
| `ExecutionUnit` | spawnSite, label, kind |
| `MessageChannel` | direction, elementType, capacity |
| `SyncPrimitive` | varFqn, kind |
| `Finding` | ruleID, severity, message, positions |

### Edge Types

| Type | From → To | Properties |
|---|---|---|
| `CALLS` | Function → Function | callSite, dynamic |
| `FLOWS_TO` | Value → Value | fieldPath, tainted |
| `SPAWNS` | Function → ExecutionUnit | callSite |
| `SENDS_ON` / `RECEIVES_FROM` | ExecutionUnit → MessageChannel | position |
| `ACQUIRES` | Function → SyncPrimitive | order, position |
| `DEFINES` | Package/File → any | — |
| `FINDING_AT` | Finding → any node | — |

Schema version is stored as a graph metadata property; migrations run forward-only via versioned migration scripts.

---

## 4. Language-Agnostic Flow Analysis

### Execution Unit Tracking

Language adapters map concurrency constructs (`go`, `async`, thread/task APIs, worker runtimes) into a shared `SPAWNS` model. The analyzer walks reachable entrypoints and records spawn relationships to build execution ownership trees.

### Message Topology

Language adapters map channel/queue/stream/message-passing constructs into `MessageChannel` nodes. Send/receive operations are linked to allocation or declaration sites. The resulting topology graph exposes fan-in/fan-out bottlenecks and potential execution-unit leaks where producers outlive reachable consumers.

### Synchronization Order

Lock/semaphore/monitor acquisition operations are recorded as `ACQUIRES` edges with a monotonically increasing `order` scoped to execution context. Order inversion detection queries pairs of functions `(A, B)` where `A` acquires `r1` then `r2` and `B` acquires `r2` then `r1`.

Alias and ownership analysis resolves whether synchronization references at different call sites can point to the same runtime resource, reducing false positives from name-only matching.

### Project Semantic Mappings

Projects can extend analysis semantics through `codeflow.semantic.yaml` without changing core engine code.

- **Tier 1: Symbol mappings** map known APIs to core semantics (`SPAWNS`, `JOINS`, `SENDS_ON`, `RECEIVES_FROM`, `ACQUIRES`)
- **Tier 2: Declarative matchers** use Tree-sitter queries plus argument extraction for wrappers and framework conventions
- **Tier 3: Optional plugins** handle highly dynamic or generated patterns that cannot be expressed declaratively

Representative mappings include:

- Treating `WaitGroup.Go` and worker-pool submit APIs as spawn points
- Treating wait/drain APIs as join points
- Treating custom channel/queue wrappers as message-channel operations
- Bridging client request callsites to server handlers with `REQUESTS` edges
- Mapping simple endpoint handlers directly to domain types with `OPERATES_ON` edges

All mapped edges include confidence metadata (`certain`, `probable`, `possible`) and provenance (`mapping_id`, `source_config`) for filtering and auditability.

---

## 5. The Anti-Pattern Engine

### Pattern DSL

Patterns implement a language-agnostic interface:

```text
interface Pattern {
  id(): string
  severity(): Severity
  query(): QueryFragment
  explain(match: MatchResult): string
}
```

`QueryFragment` values are composable: an execution-unit leak pattern composes a spawn fragment with a missing-consumer fragment. This allows new patterns to reuse library predicates without reimplementing graph traversal.

Pattern evaluation consumes only core node/edge vocabulary. Project mappings may add or annotate graph facts, but do not redefine core semantics, which keeps pattern portability intact across repositories.

### Built-in Pattern Library

| Pattern | Detection Strategy |
|---|---|
| **Execution-unit leak** | `SPAWNS` edge to execution unit; no reachable completion, cancellation, or receive path |
| **Lock order inversion** | Pair of `ACQUIRES` sequences on the same sync-resource set, in different orders, from concurrent execution units |
| **Unguarded shared-state write** | Shared allocation reachable from >=2 execution units; write op without enclosing synchronization acquisition in dominator path |
| **Unbounded message queue** | Queue/channel/stream with unbounded producer behavior and no effective backpressure consumer |
| **Trust boundary bypass** | Tainted `FLOWS_TO` path from source to sink crossing a `trustBoundary` edge without a sanitizer node |

---

## 6. Incremental Analysis

### File Change Detection

`fsnotify` watches the module root. Change events are debounced (150 ms window) and batched. Each changed file is hashed (SHA-256); a hash match against the stored `File.hash` skips re-analysis.

### Dependency Invalidation

A reverse dependency index maps each module/unit to its direct dependents (built during initial indexing). On change, the invalidation set is computed by BFS over this reverse graph up to a configurable depth (default: full transitive closure, capped for very large monorepos). Invalidated nodes are soft-deleted by bumping a `version` counter; queries default to `WHERE n.version = $current`.

### Partial Rebuild

Only invalidated modules/units re-enter the pipeline. Normalized IR is rebuilt for affected files, then inter-procedural edges touching the invalidated set are deleted and recomputed. The pattern engine re-runs only patterns whose query graph overlaps the invalidated subgraph (tracked via pattern -> node-label subscription).

Mapping configs are part of invalidation. A change to `codeflow.semantic.yaml` triggers rebuild of affected semantic edges and dependent findings.

---

## 7. API Design

### REST Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/graph/nodes` | List nodes with label/property filters |
| `GET` | `/v1/graph/nodes/:id/neighbors` | N-hop neighborhood |
| `POST` | `/v1/graph/query` | Read-only graph query (allowlisted operators) |
| `GET` | `/v1/findings` | Paginated findings; filter by `ruleID`, `severity`, `file` |
| `GET` | `/v1/functions/:fqn/cfg` | CFG for a single function |
| `GET` | `/v1/functions/:fqn/callers` | N-hop callers |
| `GET` | `/v1/channels` | Channel topology summary |
| `GET` | `/v1/bridges/requests` | Client-to-server request bridge edges with confidence |

Pagination uses cursor-based encoding (opaque base64 of last-seen node ID + sort key). Default page size: 50; max: 500.

### WebSocket Events

Clients subscribe at `/v1/ws` with a subscription message:

```json
{ "subscribe": ["findings", "graph:File:/path/to/file.ts"] }
```

The server pushes on analysis completion:

```json
{ "event": "finding.new", "payload": { "ruleID": "execution-unit-leak", "position": "..." } }
{ "event": "graph.updated", "payload": { "nodeIDs": ["fn:pkg.Foo", "ch:42"] } }
```

---

## 8. Performance Considerations

**Parallelism:** The orchestrator assigns one worker per analysis shard for parse/IR stages, bounded by available CPU. Inter-procedural passes (call graph, DFG) run after per-shard stages complete. The pattern engine fans out one worker per pattern.

**Caching:** Tree-sitter parse trees and normalized IR fragments are held in an LRU keyed by file hash; eviction threshold is 512 MB. Dependency snapshots are cached by `(module, version)` tuple. The call graph is serialized as an edge list and invalidated when any reachable dependency changes.

**Memory Budget (1M+ LOC):** At 1M LOC, expect ~80K functions and ~400K normalized values. Each value is ~200 bytes in-memory; the full IR value graph fits in ~80 MB. The semantic graph stores ~2M nodes and ~8M edges; at ~150 bytes/node and ~80 bytes/edge, this is ~940 MB on disk. Tune SurrealDB memory/cache settings for sub-second traversal on hot paths. Analysis workers are capped at 4 GB RSS.

---

## 9. Technology Choices

| Component | Choice | Rationale |
|---|---|---|
| Analysis frontend | Tree-sitter grammars | Incremental parsing across many languages with consistent node spans and parser behavior |
| Analysis core | Language-agnostic IR + adapters | Shared pipeline across languages; language-specific logic is isolated to adapter modules |
| Graph DB (current) | SurrealDB 3.0 | Single active backend now; supports graph-style relations while we keep persistence abstractions DB-agnostic |
| Graph DB (future) | Pluggable backend via storage interface | Adapter boundary keeps room for additional graph engines later without changing analysis pipeline |
| File watcher | `fsnotify` | Cross-platform, battle-tested, low overhead |
| Job queue | In-process channel-based queue | Avoids external deps for MVP; replace with NATS JetStream at scale |
| API framework | HTTP router (implementation-specific) | Keep transport surface stable while allowing implementation changes |
| WebSocket | RFC6455-compatible server library | Standard protocol support without coupling docs to a specific runtime library |
| Query language | Backend-agnostic query layer (SurrealQL adapter today) | Application uses a stable query interface; backend-specific queries are isolated to adapters |
| Serialization | Protocol Buffers (internal) + JSON (API) | Compact wire format internally; human-readable externally |
| Semantic extension format | `codeflow.semantic.yaml` | Project-level declarative mapping of local abstractions into core graph semantics |

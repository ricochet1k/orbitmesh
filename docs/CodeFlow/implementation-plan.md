# CodeFlow Custom Analysis Rules — Implementation Plan

**Status:** Proposed
**Date:** 2026-03-09
**Inputs:** `customizable-analysis-rules.md`, `customizable-analysis-rules-review.md`, MVP plan, backend architecture doc, existing `codeflowmvp` code

---

## Context & Current State

The existing codebase has a working MVP: tree-sitter parsing (Go + JS/TS adapters), a flat fact model (`FunctionFact`, `CallSiteFact`, `SpawnSiteFact`, `APIHandlerFact`, etc.), two hardcoded rules (`spawn_in_loop`, `source_to_sink_unsanitized`) evaluated procedurally in Go, and JSON/markdown output. There is no graph database yet — facts live in Go slices and findings are computed in-memory.

The design docs describe a three-layer architecture:
1. **Layer 1 — Fact Generation**: YAML rules match AST patterns and emit graph nodes/edges/tags
2. **Layer 2 — Graph Enrichment**: Graph-match → graph-write rules derive higher-level facts (transaction scopes, taint propagation, lock hold propagation)
3. **Layer 3 — Analysis Queries**: Read-only graph queries that find patterns and emit findings

The review doc identifies concrete gaps: the signature syntax is too OOP-centric, there's no support for tree-sitter queries in rules, no confidence metadata, no scope/lifetime facts, no enrichment layer, and the configuration format needs refinement.

This plan breaks the work into phases that each produce a working, testable deliverable. Some phases include research spikes because design questions remain open.

---

## Phase 1: Finalize the Rule Configuration Schema

**Goal:** Lock down the YAML schema for Layer 1 rules so implementation can begin with confidence.

**Open questions to resolve:**
- How expressive does `match.kind` need to be for the first version? The review doc proposes matching `go_statement`, `defer_statement`, `decorator`, `member_expression`, etc. — but each kind requires specific sub-fields (`operand`, `decorated`, `object`, `property`). Can we start with a fixed set of 5-6 kinds and expand later?
- Should `match.tree_sitter` (inline S-expression queries) be a first-class citizen, or a power-user escape hatch? Allowing raw tree-sitter queries gives maximum flexibility but complicates validation.
- Should `match.signature` and `match.tree_sitter` be mutually exclusive, or composable (with `match.context` for ancestor constraints)?
- How does `identity_expansion` interact with the stable-ID system from the MVP plan?

**Deliverables:**
1. A JSON Schema or Go struct definition for the full Layer 1 rule configuration, covering:
   - `match.signature` (the glob syntax, generalized beyond OOP)
   - `match.kind` (AST node type matching)
   - `match.tree_sitter` (raw S-expression queries)
   - `match.context.ancestor` (structural nesting constraints)
   - `match.package` (package-level matching)
   - `node.tags`, `node.properties`, `node.identity_expansion`
   - `edge.type`, `edge.from`, `edge.to`, `edge.properties`, `edge.confidence`
   - `data_flow.role` (source/sink/sanitizer/propagator), `data_flow.categories`, `data_flow.from/to`
   - `control_flow.terminates_execution`
2. A set of 15-20 example rules written against this schema, covering:
   - The existing hardcoded rules (spawn_in_loop, source-to-sink) expressed as YAML
   - Go-specific patterns (defer, goroutines, mutex lock, error returns)
   - At least 2-3 JS/TS patterns (express routes, promise spawns, property access)
   - Package-level layer tagging
3. A validation function (`rules.ValidateConfig`) that parses and validates `codeflow.yaml`

**Research spike:** Prototype matching a few tree-sitter S-expression queries against the Go and JS/TS tree-sitter grammars to verify the proposed `match.tree_sitter` field is actually practical. Document which tree-sitter query features work well and which are awkward.

**Estimated scope:** ~3-5 days

---

## Phase 2: YAML Rule Loader & Signature Matcher

**Goal:** Load rules from `codeflow.yaml` and match `signature`-style rules against the existing fact extraction pipeline.

**Why this comes before graph integration:** The existing MVP already extracts facts (functions, calls, spawns) into Go structs. We can match signature-based rules against these facts without needing a graph DB. This lets us validate the matching logic in isolation.

**Deliverables:**
1. `internal/codeflowmvp/rules/config.go` — YAML parsing into the schema from Phase 1
2. `internal/codeflowmvp/rules/signature.go` — Signature pattern parser and matcher
   - Parse the `Package::Target.Method(Args) -> Returns` syntax
   - Support wildcards (`*`, `**`)
   - Support parameter binding (`$key`, `$fn`, `...$args`)
   - Match against extracted `CallSiteFact` records (using the callee expression and any resolved type info available)
3. `internal/codeflowmvp/rules/match.go` — Unified match dispatcher
   - Routes `match.signature` to the signature matcher
   - Routes `match.kind` to a kind matcher (initially: `go_statement`, `defer_statement`, `call_expression`, `for_statement`)
   - Routes `match.package` to package-level matching
4. Tests: Fixture-based tests matching rules against the existing `testdata/` samples
5. Convert the hardcoded `sourceCalleeSuffixes`/`sinkCalleeSuffixes` lists in `engine.go` into YAML rules and verify identical findings

**Key constraint:** Signature matching requires some level of type/import resolution (knowing that `db.Exec` refers to `database/sql::*DB.Exec`). For MVP, do best-effort matching on callee expression strings (suffix matching, like the current code does). Track a TODO for full resolution in a later phase.

**Estimated scope:** ~5-7 days

---

## Phase 3: Tree-sitter Query Integration

**Goal:** Support `match.tree_sitter` rules that run S-expression queries directly against the parse tree, and `match.context.ancestor` for structural nesting constraints.

**Why separate from Phase 2:** Tree-sitter query matching requires access to the CST during extraction, while signature matching operates on extracted facts. Different integration points in the pipeline.

**Deliverables:**
1. Extend the Go and JS/TS scan adapters to accept a list of tree-sitter queries and return match results with captured nodes
2. `internal/codeflowmvp/rules/treesitter.go` — Bridge between tree-sitter query results and the rule action system (tag emission, edge emission)
3. Implement `match.context.ancestor` — during extraction, annotate facts with their AST ancestor chain (or at minimum, boolean flags like `InsideLoop`, `InsideDefer`, `InsideErrorCheck`). This extends the current `InsideLoop` field to a more general mechanism.
4. Tests: Write a `spawn_in_loop` rule using `match.tree_sitter` and verify it produces identical results to the hardcoded rule
5. Write at least one rule that can only be expressed via tree-sitter (e.g., "defer statement calling a method named Close" — for resource cleanup detection)

**Research spike:** Experiment with tree-sitter query performance. How expensive is it to run 50-100 queries per file? Should queries be batched or run incrementally? Does the `gotreesitter` Go binding support query cursors efficiently?

**Estimated scope:** ~4-6 days

---

## Phase 4: Tag & Edge Emission System

**Goal:** When rules match, actually emit tags and edges as structured data (not just findings). This is the transition from "rules produce findings directly" to "rules produce graph facts, which are then queried for findings."

**Deliverables:**
1. Extend the `ExtractionSummary` (or a new intermediate model) with:
   - `TagFact { NodeID string, Tag string, RuleID string, Confidence float64 }`
   - `EdgeFact { Type string, FromID string, ToID string, Properties map[string]any, RuleID string, Confidence float64 }`
2. `internal/codeflowmvp/rules/emitter.go` — Given a matched rule and its captures, produce `TagFact`/`EdgeFact` records
3. Update the rule evaluation pipeline: match rules → emit tags/edges → then evaluate analysis rules against the enriched fact set
4. Implement `identity_expansion`: when a rule specifies it, create distinct nodes for each expanded value (e.g., separate nodes for `Getenv("PORT")` vs `Getenv("SECRET")`)
5. Update findings output to include provenance: which rules produced which tags, which tags contributed to which findings
6. Tests: End-to-end test showing YAML rules → fact extraction → tag/edge emission → finding generation

**Key design decision:** At this point, tags and edges are still in-memory Go data structures, not in a graph DB. The emitter produces slices of `TagFact` and `EdgeFact`. This is intentional — we validate the emission logic before committing to a storage layer.

**Estimated scope:** ~4-5 days

---

## Phase 5: Graph Storage Integration

**Goal:** Persist facts, tags, and edges in a queryable graph store so enrichment and analysis queries can run as graph traversals rather than procedural Go code.

**Open questions to resolve:**
- SurrealDB 3.0 is named in the architecture doc. Is it mature enough? What's the Go client situation? Evaluate alternatives: embedded graph options (e.g., Cayley, or a custom in-memory property graph), or start with SurrealDB and accept its constraints.
- How does the stable-ID system (from the MVP plan) interact with graph upserts? MERGE semantics?
- What's the right granularity for scan epochs? Per-file? Per-package?

**Research spike (do first):** Stand up SurrealDB 3.0 locally, create the node/edge schema from the backend architecture doc, insert a few hundred nodes from a test scan, and run representative queries. Measure insert throughput and query latency. Evaluate whether the SurrealQL query language can express the enrichment and analysis queries from the review doc. If SurrealDB doesn't work out, evaluate the fallback options. Document the decision.

**Deliverables:**
1. `internal/codeflowmvp/store/graph.go` — Graph store interface:
   ```go
   type GraphStore interface {
       UpsertNode(ctx context.Context, node Node) error
       UpsertEdge(ctx context.Context, edge Edge) error
       SetTags(ctx context.Context, nodeID string, tags []string) error
       Query(ctx context.Context, query string, params map[string]any) ([]Record, error)
       DeleteByEpoch(ctx context.Context, producer string, beforeEpoch int64) error
   }
   ```
2. One concrete implementation (SurrealDB or fallback)
3. `internal/codeflowmvp/store/writer.go` — Translates `ExtractionSummary` + `TagFact`/`EdgeFact` into graph upserts with scan epoch metadata
4. Migrate the existing scan pipeline to write through the graph store
5. Verify existing findings still work by querying the graph instead of iterating Go slices
6. Tests: Round-trip test — insert facts, query them back, verify correctness

**Estimated scope:** ~7-10 days (including the research spike)

---

## Phase 6: Enrichment Layer (Layer 2)

**Goal:** Implement the graph enrichment layer — rules that match patterns in the graph and write new derived facts.

**Deliverables:**
1. Enrichment rule configuration schema:
   ```yaml
   enrichment:
     - id: "transaction_scope"
       phase: 1
       match: "<graph query>"
       write: "<graph mutation template>"
   ```
2. `internal/codeflowmvp/enrichment/engine.go` — Enrichment rule executor:
   - Parse enrichment rules from config
   - Execute `match` queries against the graph store
   - For each match result, execute the `write` template to create new nodes/edges
   - Respect phase ordering (phase 1 runs before phase 2, etc.)
   - All written facts carry `producer: "enrichment/<rule-id>"` and `scan_epoch` metadata
3. Implement 2-3 built-in enrichment rules to prove the system:
   - **Resource lifetime**: Trace `ResourceAcquire`-tagged nodes to all exit paths, check for `ResourceRelease` (the HTTP body close example from the review doc)
   - **Taint propagation**: Follow `FLOWS_TO` edges through `propagator`-tagged functions, stopping at `sanitizer`-tagged functions
   - **Spawn-join linkage**: Link `SPAWNS` edges to `JOINS` edges in the same scope (the goroutine leak example)
4. Incremental invalidation: When base facts change, identify which enrichment rules' input subgraphs are affected and re-run only those
5. Tests: Fixture-based tests showing enrichment rule execution and the derived graph facts

**Key design decision:** The `match` and `write` fields use the graph store's query language (SurrealQL or Cypher, depending on Phase 5). This means enrichment rules are somewhat coupled to the storage choice. Accept this for now; if we need to support multiple backends, introduce a query abstraction later.

**Open questions:**
- How to handle enrichment rules that need multiple iterations to reach a fixed point (e.g., taint propagation through a long call chain)? Simple approach: re-run until no new facts are produced, with a cap. Better approach: topological ordering of functions.
- Can enrichment queries express the control-flow path traversals needed for transaction scope analysis? This depends heavily on how the CFG is represented in the graph. The MVP currently doesn't build intra-procedural CFGs — just function→calls edges. Transaction scope analysis may need to wait until CFG nodes (`NEXT` edges between statements) are in the graph.

**Estimated scope:** ~7-10 days

---

## Phase 7: Analysis Query Layer (Layer 3)

**Goal:** Replace the hardcoded procedural rule evaluation (`rules.Evaluate`) with declarative graph queries that emit findings.

**Deliverables:**
1. Analysis rule configuration schema:
   ```yaml
   analyses:
     - id: "transaction_leak"
       severity: "critical"
       query: "<graph query>"
       explain: "Template with {{variable}} substitution"
   ```
2. `internal/codeflowmvp/analysis/engine.go` — Analysis query executor:
   - Parse analysis rules from config
   - Execute each query against the graph store
   - For each result row, instantiate the `explain` template to produce a finding message
   - Create `Finding` nodes in the graph linked to relevant code nodes
3. Parameterized analysis templates (from the review doc):
   ```yaml
   analysis_templates:
     - id: "resource_leak_template"
       parameters: { acquire_tag: string, release_tag: string }
       query: "..."
   analyses:
     - id: "transaction_leak"
       template: "resource_leak_template"
       params: { acquire_tag: "TransactionStart", release_tag: "TransactionCommit" }
   ```
4. Migrate the existing two rules to declarative analysis queries:
   - `spawn_in_loop`: Query for `SpawnSite` nodes with `InLoop` tag
   - `source_to_sink_unsanitized`: Query for `TaintPath` nodes where `sanitized = false` (depends on enrichment from Phase 6)
5. Verify that the declarative rules produce identical findings to the current procedural code on the existing test fixtures
6. Tests: End-to-end from scan → fact generation → enrichment → analysis → findings output

**Estimated scope:** ~5-7 days

---

## Phase 8: Symbol Resolution & Import Analysis

**Goal:** Enable signature matching to resolve fully-qualified names rather than relying on suffix matching.

This is a significant capability upgrade. Currently, `db.Exec` is matched by checking if the callee expression ends with `.Exec`. With resolution, we can know that `db` refers to a `*sql.DB` and match the rule `database/sql::*DB.Exec(*)` precisely.

**Deliverables:**
1. `internal/codeflowmvp/resolve/imports.go` — Import resolution:
   - Parse import declarations from the tree-sitter CST
   - Build a per-file import map: `local name → package path`
   - Handle aliased imports, dot imports, blank imports
2. `internal/codeflowmvp/resolve/types.go` — Basic type resolution:
   - For each variable referenced in a call expression receiver, look up its declaration and infer its type
   - Start with simple cases: function parameters with explicit types, `:=` assignments from typed function returns
   - Do NOT attempt full type inference — that's a rabbit hole. Use heuristics and mark confidence levels.
3. Extend the signature matcher to use resolved package paths and receiver types when available, falling back to suffix matching when resolution fails
4. Tests: Verify that previously ambiguous matches (e.g., `db.Exec` matching both `sql.DB.Exec` and some unrelated `.Exec` method) are now correctly disambiguated

**Research spike:** How much type information can we cheaply extract from tree-sitter without building a full type checker? Document the limits. Consider whether `gopls` or `go/types` could be used as an oracle for Go specifically, and what the equivalent would be for JS/TS (`tsserver`?).

**Estimated scope:** ~7-10 days

---

## Phase 9: Intra-Procedural Control Flow Graph

**Goal:** Build per-function CFG nodes and `NEXT` edges in the graph, enabling enrichment rules that reason about control flow paths within a function.

This is needed for the transaction leak and resource lifetime examples from the review doc. Without CFG edges, enrichment rules can only traverse the call graph (inter-procedural), not the control flow within a function.

**Deliverables:**
1. `internal/codeflowmvp/cfg/builder.go` — CFG construction from normalized IR (or directly from tree-sitter CST for MVP):
   - Extract basic blocks from function bodies
   - Create `Statement` nodes and `NEXT` edges (with branch conditions)
   - Handle: `if/else`, `for/range`, `switch/case`, `return`, `defer`, `go`
   - Represent error-check branches (Go `if err != nil { return }` pattern)
2. Persist CFG nodes and edges in the graph store
3. Update enrichment rules (from Phase 6) to traverse CFG edges:
   - Resource lifetime now traces `NEXT*` paths from acquire to return
   - Transaction scope analysis becomes feasible
4. Tests: Verify CFG correctness on a set of Go function fixtures. Compare against expected basic block counts and edge connectivity.

**Open questions:**
- What level of granularity for CFG nodes? Per-statement? Per-basic-block? Per-statement is more precise but produces many more nodes. Start with per-basic-block and add per-statement if enrichment queries need it.
- How does `defer` interact with the CFG? Deferred calls execute on all exit paths. Model as synthetic edges from each return statement to the deferred call, or as a separate `DEFERRED_AT` edge?

**Estimated scope:** ~7-10 days

---

## Phase 10: Configuration Format Refinement & Starlark

**Goal:** Based on experience from Phases 1-9, evaluate whether YAML is still adequate or whether Starlark (or HCL) should be introduced for rule libraries.

By this point we'll have real-world rules and know which patterns are painful in YAML. This phase is explicitly a reassessment.

**Deliverables:**
1. Audit: Catalog all rules written so far. How many are simple (YAML works fine)? How many are complex (embedded queries, repetitive patterns, conditional logic)?
2. If warranted, implement a Starlark rule loader as an alternative to YAML:
   - `internal/codeflowmvp/rules/starlark.go` — Starlark environment with `rule()`, `enrichment()`, `analysis()` built-in functions
   - Port the most painful YAML rules to Starlark and compare readability
3. If NOT warranted, document why YAML is sufficient and close this item
4. Either way: publish a "Rule Authoring Guide" with examples and best practices for each layer

**Estimated scope:** ~3-5 days (evaluation), ~5-7 days (if Starlark is implemented)

---

## Phase 11: Visual Configuration & Frontend Query Integration

**Goal:** Implement the `visuals` section of the configuration, connecting graph tags/edges to frontend presentation.

**Deliverables:**
1. Visual configuration schema:
   ```yaml
   visuals:
     nodes:
       - match_tag: "Sink"
         shape: "octagon"
         color: "darkred"
     edges:
       - match_type: "SPAWNS"
         stroke_dash: "dashed"
     queries:
       - id: "taint_paths"
         query: "MATCH p=(s:Source)-[*]->(t:Sink) RETURN p"
         presentation:
           path_color: "red"
   ```
2. API endpoint: `GET /v1/graph/visual-config` — returns the resolved visual config for the current project
3. API endpoint: `POST /v1/graph/visual-query` — executes a named visual query and returns results with presentation metadata
4. Frontend integration: extend the D3 force-directed graph to apply visual rules from the config (node colors, shapes, edge styles)
5. Tests: Verify that visual config is correctly resolved and applied

**Estimated scope:** ~5-7 days

---

## Phase 12: Incremental Analysis & Watch Mode

**Goal:** Make the full pipeline incremental — file changes trigger selective re-extraction, re-enrichment, and re-analysis, rather than full re-scan.

The MVP plan and architecture doc both describe this, and Phase 5 introduces scan epochs. This phase wires it all together.

**Deliverables:**
1. File watcher integration (`fsnotify`) with debouncing
2. Incremental fact extraction: only re-parse changed files, update their facts in the graph
3. Dependency invalidation: when a function's facts change, identify affected enrichment rules and re-run them
4. Incremental finding updates: retire stale findings, emit new ones, push WebSocket events
5. Tests: Change a source file, verify that only affected findings are updated and the rest remain stable

**Estimated scope:** ~7-10 days

---

## Dependency Graph

```
Phase 1 (Schema)
  ↓
Phase 2 (Signature Matcher)  ←──────────────────────┐
  ↓                                                   │
Phase 3 (Tree-sitter Queries)                         │
  ↓                                                   │
Phase 4 (Tag & Edge Emission)                         │
  ↓                                                   │
Phase 5 (Graph Storage) ─── research spike first      │
  ↓                                                   │
Phase 6 (Enrichment Layer) ──────────────────────────→│
  ↓                                                   │
Phase 7 (Analysis Queries) ──────────────────────────→│
  ↓                                                   │
Phase 8 (Symbol Resolution) — can start after Phase 2 │
  ↓                                                   │
Phase 9 (CFG) — can start after Phase 5               │
                                                      │
Phase 10 (Config Format) — after Phases 6-7           │
Phase 11 (Visuals) — after Phase 7                    │
Phase 12 (Incremental) — after Phase 7                │
```

Phases 8, 9, 10, 11, and 12 are relatively independent of each other and can be parallelized or reordered based on priorities.

---

## What's Explicitly Deferred

These items are mentioned in the design docs but are not included in this plan:

- **Multi-language support beyond Go + JS/TS**: The architecture is language-agnostic by design, but adding Python/Rust/Java adapters is separate work.
- **Normalized IR (Stage 2 from backend architecture doc)**: The architecture doc describes lowering tree-sitter CSTs into a language-agnostic IR. This is important for true multi-language support but not needed while we only have Go + JS/TS. Tree-sitter queries + language-specific adapters are sufficient for now.
- **Full SSA/DFA engine**: The architecture doc envisions a backward-flow taint pass. The plan uses a simpler enrichment-based taint propagation. Full DFA is a major effort that should be its own initiative.
- **Runtime tracing overlay**: Mentioned as a future extensibility point. Not in scope.
- **Custom DSL**: The review doc proposes a `tag X on Y` / `find X { ... }` syntax. Deferred unless Starlark is also insufficient.
- **Code coverage integration**: Mentioned in the architecture doc. Not in scope for analysis rules.
- **Plugin system (Tier 3 from backend architecture)**: Highly dynamic patterns that can't be expressed declaratively. Deferred until we know what can't be expressed.

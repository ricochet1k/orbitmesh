# CodeFlow Explorer MVP Plan (Go + Tree-sitter + GraphDB)

Status: Proposed
Date: 2026-03-01
Scope: Very small, fast MVP for execution and data-flow analysis slices

## 1) Goal

Deliver a CLI-first static analysis MVP that proves the end-to-end architecture:

1. Parse Go code with `github.com/odvcencio/gotreesitter`
2. Build a minimal semantic graph in `github.com/mstrYoda/goraphdb`
3. Run a few useful structural rules
4. Export findings in machine- and human-readable formats

This MVP prioritizes speed to value and incremental-ready foundations over completeness.

## 2) Non-goals

- No frontend canvas/table UI
- No full CFG/SSA/DFG engine
- No multi-language support (Go only)
- No full policy DSL in v1
- No watch mode in first cut

## 3) MVP capabilities

### 3.1 Parsing and extraction (Go only)

Use Tree-sitter queries to extract:

- files and packages
- function/method declarations
- direct call expressions
- `go` statements
- loop ancestry for each spawn/call site

### 3.2 Graph storage

Persist semantic facts in `goraphdb` as typed nodes and edges.

Core node labels:

- `File`
- `Function`
- `CallSite`
- `ExecutionUnit`
- `Finding`

Core edge labels:

- `DEFINES` (`File -> Function`)
- `CALLS` (`Function -> Function`)
- `SPAWNS` (`Function -> ExecutionUnit`)
- `AT_CALLSITE` (`Function -> CallSite`)
- `FINDING_AT` (`Finding -> Function|CallSite|ExecutionUnit`)

### 3.3 Rules (initial)

1. `spawn_in_loop` (high)
   - Trigger when `go` appears under `for` or `range`.

2. `source_to_sink_unsanitized` (high/critical)
   - Minimal source set: `r.URL`, `r.Form`, `r.Body`, env reads.
   - Minimal sink set: `db.Exec`-style, response writes, `exec.Command`.
   - Sanitizer allowlist checked in path/function context.

3. `orphan_execution_unit` (medium)
   - Spawn detected with no obvious cancellation/join signal in local context.

### 3.4 Output

- JSON findings for automation
- Markdown findings report for PR/review workflows

## 4) Incremental-ready design decisions

### 4.1 Stable IDs (no hash in identity)

Function identity must be semantic and stable across body edits.

- `Function.id`: module/package + receiver + function name (+ signature discriminator only if needed)
- No content hash in IDs
- `content_hash` and related hashes stored as node properties

Rationale: callers and dependency identity should remain stable when implementation changes.

### 4.2 Structural IDs for statement-level nodes

Statement-level nodes should avoid pure line-based identity.

- `CallSite.id`: caller function ID + structural ordinal/path within normalized AST
- `ExecutionUnit.id`: parent function ID + spawn-site structural ordinal/path

Line/column are stored as properties for display only.

### 4.3 Hashes as properties

Track change signals in properties:

- `file_hash`
- `ast_hash`
- `exports_hash`
- `analyzer_version`
- `rule_version`

These drive selective recomputation without destabilizing graph identity.

### 4.4 Graph-native reverse dependency traversal

Do not maintain a separate reverse dependency store.

Use graph edges and reverse traversals for invalidation impact:

- callers of changed functions (`CALLS` reverse)
- findings attached to touched subgraphs (`FINDING_AT` reverse)
- file-to-symbol ownership via `DEFINES`

### 4.5 Scan epoch and safe replacement

Every derived node/edge/finding carries `scan_epoch` and `producer` metadata.

Update flow:

1. Compute impacted scope
2. Write new facts with current epoch
3. Remove/retire prior-epoch facts only in impacted scope

This avoids mixed stale/fresh state during partial rebuilds.

## 5) Minimal schema

Common properties:

- `id` (stable semantic ID)
- `kind`
- `file`
- `line`
- `column`
- `confidence` (`certain|probable|possible`)
- `scan_epoch`
- `producer`
- `producer_version`

`Function` properties:

- `package`
- `name`
- `receiver`
- `signature`
- `content_hash`

`Finding` properties:

- `rule_id`
- `severity`
- `message`
- `fingerprint` (structural fingerprint, not line-only)
- `status` (`open` default)

## 6) CLI surface

- `codeflow-mvp scan <path>`
  - Parse, extract, upsert semantic graph, run rules.

- `codeflow-mvp findings --format json|markdown`
  - Emit current findings snapshot.

- `codeflow-mvp query <cypher>`
  - Dev/debug graph introspection.

- `codeflow-mvp explain-impact <symbol-or-file>`
  - Show graph-derived reverse dependencies and would-recompute scope.

## 7) Implementation layout

- `cmd/codeflow-mvp/main.go`
- `internal/parser/` (Tree-sitter setup + queries)
- `internal/extract/` (facts from AST matches)
- `internal/store/` (goraphdb schema + upsert)
- `internal/rules/` (rule engine + built-ins)
- `internal/incremental/` (impact calculation + epoch lifecycle)
- `internal/report/` (json/markdown output)

## 8) Delivery sequence (fast)

1. Parser and extractor for functions/calls/spawns
2. Graph persistence with stable IDs
3. Rule 1 (`spawn_in_loop`) and reporting
4. Rule 2 (`source_to_sink_unsanitized`) with minimal source/sink sets
5. Incremental metadata + graph-native impact query + epoch replacement
6. Rule 3 (`orphan_execution_unit`) if time permits

## 9) Acceptance criteria

1. Running `scan` on a Go project creates a queryable semantic graph.
2. Re-running `scan` after function body-only edits preserves function IDs.
3. `spawn_in_loop` findings are emitted on seeded fixtures.
4. `source_to_sink_unsanitized` emits at least one true-positive on fixture corpus.
5. Impact query returns reverse-callers/finding scope via graph traversal only.
6. Partial rebuild does not leave stale findings in impacted scope (epoch-validated).

## 10) Risks and explicit tradeoffs

- Heuristic taint is intentionally shallow and may miss deep flows.
- Direct-call-only graph misses dynamic dispatch cases in MVP.
- Structural site IDs may still churn under heavy refactors; acceptable for MVP.

These are acceptable as long as identity stability, graph queryability, and partial-rebuild correctness are demonstrated.

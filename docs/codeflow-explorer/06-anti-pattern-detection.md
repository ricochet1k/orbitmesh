# Anti-Pattern Detection and Code Quality Interface

**CodeFlow Explorer | Component: Anti-Pattern Engine & Findings Interface**
**Status:** Draft | **Audience:** Engineering, Security Audit

---

## 1. Philosophy: Structural Bugs vs Style Issues

Linters enforce convention. This system enforces correctness.

`go vet`, `staticcheck`, and `golangci-lint` are necessary and should run in every pipeline — but they operate on local syntax and type information. They cannot reason about *relationships* between code artifacts across a codebase: who calls whom, what data flows where, which goroutine owns which resource. A missed `errcheck` is a style miss. A goroutine with no reachable termination path in a service that receives unbounded requests is a latent incident.

The class of problems CodeFlow Explorer targets are **structural anti-patterns**: defects that emerge from the *shape* of the code graph, not from any single statement. These include goroutine lifecycle bugs (spawned but never joined or cancelled), lock ordering hazards (two goroutines acquire the same locks in opposite orders), unintended data flows (tainted external input reaching a sensitive sink without a sanitizing node on the path), architectural boundary violations (a repository layer importing an HTTP handler type), and emergent complexity accumulation (a struct that has become the implicit coordination point for a third of the codebase).

These problems are especially acute in AI-written code. LLM-generated code is locally coherent but globally naive: it produces plausible-looking goroutine patterns, correct-looking mutex usage, and reasonable-looking package imports, each of which may be sound in isolation while being collectively hazardous. An auditor reviewing AI-generated code needs a tool that sees the whole graph, not the current file.

---

## 2. The Built-in Pattern Library

Each pattern carries a base severity (`critical`, `high`, `medium`, `low`), a detection strategy, and a false positive (FP) risk rating.

| # | Pattern | Severity | FP Risk |
|---|---------|----------|---------|
| 1 | **Goroutine leak** — `go f()` with no reachable `return`, channel close, or context cancellation propagated to `f` | Critical | Medium — deferred cancel paths require dataflow analysis |
| 2 | **Lock order inversion** — two call paths each acquiring locks A and B in opposite order, reachable from the same entry point | Critical | Low |
| 3 | **Unguarded concurrent map write** — map write node reachable from two goroutine roots with no mutex acquisition on the shared path | Critical | Low |
| 4 | **Unbounded channel** — `make(chan T)` or `make(chan T, 0)` with a producer inside a loop and no backpressure signal | High | Medium |
| 5 | **Send to potentially-closed channel** — a send node whose target channel has a reachable close node not dominated by the send | Critical | High — channel ownership conventions vary |
| 6 | **Trust boundary bypass** — external input node (HTTP body, env var, argv) flows to a sink (SQL exec, shell exec, file write) with no sanitizer node on any path | Critical | Medium |
| 7 | **God struct** — struct with field count >25 referenced across >8 distinct packages | Medium | Low |
| 8 | **Architectural layer violation** — package in `internal/repository` importing any symbol from `internal/handler` | High | Low — requires project layer map |
| 9 | **Context not propagated** — function accepting `context.Context` calls a function that also accepts `context.Context` but passes `context.Background()` instead of the received context | Medium | Low |
| 10 | **Goroutine spawn in tight loop without bound** — `go` statement inside a `for` loop with no semaphore, `errgroup` with limit, or worker pool on the path | High | Medium |
| 11 | **WaitGroup counter misuse** — `wg.Add()` reachable after `wg.Wait()` on the same `WaitGroup` variable within a single execution scope | Critical | Medium |
| 12 | **Mutex passed by value** — `sync.Mutex` or `sync.RWMutex` field copied via value receiver or value assignment | High | Low |

---

## 3. The Pattern DSL

Patterns are Cypher-over-the-code-graph queries, composed from reusable predicate functions. The graph nodes represent functions, types, variables, goroutines, and call sites; edges represent calls, data flow, ownership, and control flow.

**Reusable predicates:**
- `isGoroutine(n)` — node `n` is a `go` statement
- `acquires(n, lock)` — node `n` contains a `lock.Lock()` call
- `reachableFrom(a, b)` — there exists a directed path from `a` to `b` in the call or control-flow graph

**Example: Lock Order Inversion**

```yaml
id: lock-order-inversion
name: Lock Order Inversion
severity: critical
query: |
  MATCH (g1:Goroutine)-[:CALLS*]->(a1:CallSite {method: "Lock", receiver: $lockA})
  MATCH (g1)-[:CALLS*]->(a2:CallSite {method: "Lock", receiver: $lockB})
  WHERE id(a1) < id(a2)  -- A acquired before B in g1
  MATCH (g2:Goroutine)-[:CALLS*]->(b1:CallSite {method: "Lock", receiver: $lockB})
  MATCH (g2)-[:CALLS*]->(b2:CallSite {method: "Lock", receiver: $lockA})
  WHERE id(b1) < id(b2)  -- B acquired before A in g2
  AND g1 <> g2
  RETURN g1, g2, a1, a2, b1, b2
match_result:
  goroutine_1: g1
  goroutine_2: g2
  lock_a: $lockA
  lock_b: $lockB
explain: |
  Goroutine {{goroutine_1.name}} acquires {{lock_a}} then {{lock_b}}.
  Goroutine {{goroutine_2.name}} acquires {{lock_b}} then {{lock_a}}.
  If both goroutines run concurrently, a deadlock is possible.
  Establish a canonical lock acquisition order and enforce it across all call sites.
```

Patterns reference shared predicates by name. The engine resolves predicates at query compilation time and inlines them as subqueries. This keeps individual pattern definitions readable while sharing expensive traversal logic.

---

## 4. User-Defined Patterns

Teams add custom patterns via YAML in `.codeflow/patterns/`:

```yaml
id: no-db-from-handler
name: No Direct Database Access from HTTP Handler Layer
severity: high
tags: [architecture]
description: >
  HTTP handlers must not import or call database packages directly.
  All persistence must go through a service or repository interface.
query: |
  MATCH (h:Package {layer: "handler"})-[:IMPORTS]->(d:Package {layer: "database"})
  RETURN h, d
explain: |
  Package {{h.path}} directly imports database package {{d.path}}.
  Introduce a repository interface in the service layer and inject it into the handler.
false_positive_notes: >
  Integration test files in handler packages may legitimately import database packages.
  Use the suppress-path directive to exclude *_integration_test.go files.
```

Custom patterns use the same DSL as built-in patterns. They are versioned in the repository alongside the code they govern. Teams share patterns via a `codeflow publish-patterns` command that pushes to an organization-scoped pattern registry, allowing other projects to import them by URI.

---

## 5. Findings Lifecycle

A finding is keyed by `(pattern_id, structural_fingerprint)` where the fingerprint is a hash of the graph path that triggered it — not file line numbers, which change with reformatting. This means a finding suppressed in a refactored codebase remains suppressed if the same structural problem exists, but a fixed-then-reintroduced anti-pattern generates a new finding with a new fingerprint.

**States:**

```
open → acknowledged → reviewed → suppressed
                              ↘ fixed (detected by re-scan showing no match)
```

Suppression requires a justification string and an optional expiry commit SHA. Silencing without justification is rejected. Bulk operations: suppress all findings in paths matching `**/generated/**` with justification `"generated code — not manually maintained"`.

---

## 6. Findings Dashboard

The dashboard is the entry point for a scan session. Layout:

- **Severity donut** — proportional breakdown of open findings by severity, clickable to filter
- **Trend line** — finding count per commit over the past 90 days, with trendline. New findings appear in red, resolved in green
- **Top affected packages** — horizontal bar chart, sorted by total weighted severity score
- **Pattern frequency ranking** — table of patterns sorted by finding count
- **New since last scan** — highlighted count with inline diff of new/resolved finding IDs
- **AI-generated filter** — toggle that restricts the view to code attributed to AI authors via git blame heuristics (commit messages matching known AI patterns) or explicit `// codeflow:ai-generated` annotations. Findings in AI-generated code receive a visual badge and score boost.

---

## 7. Finding Detail View

Clicking a finding opens a split panel:

- **Left: graph path** — a mini canvas rendering exactly the nodes and edges that matched the pattern query. Nodes are color-coded by type (goroutine, lock, sink). Hovering a node shows its declaration location.
- **Right: detail panel**
  - Code snippets for each node in the path, with the relevant line highlighted
  - Explanation text rendered from the pattern's `explain` template with actual variable values
  - Severity score with contributing factors shown as a breakdown bar
  - Similar findings (same pattern, different locations; or different pattern, same package)
  - Remediation description — a structural change description, not generated code
  - **"Trace in main canvas"** button — opens the full graph view centered on the finding's primary node, with the finding path highlighted

---

## 8. Severity and Risk Scoring

```
risk_score = base_severity
           × reachability_factor      -- 1.0–2.0, higher if reachable from external entry
           × (1 - test_coverage)      -- 0.5–1.0, lower if affected code is well-tested
           × hot_path_factor          -- 1.0–1.5, higher if on a frequently-called path
           × ai_authorship_factor     -- 1.0–1.3, higher if code is AI-attributed
```

`base_severity` is an integer: critical=8, high=5, medium=3, low=1. The final score is normalized to 0–10 and bucketed back to a display severity. A `medium` base finding on an untested, AI-generated, externally-reachable hot path may score as `critical` after modifiers.

---

## 9. Regression Mode

```bash
codeflow scan --baseline findings-main.json --threshold 3
```

In CI, the scanner loads a baseline snapshot and emits only delta findings. Exit codes: `0` — no new findings; `1` — new findings present, below `--threshold`; `2` — new findings at or above threshold (blocks merge by default).

Output formats via `--format`:
- `json` — full finding objects for downstream tooling
- `sarif` — SARIF 2.1 for GitHub Advanced Security / Security tab integration
- `markdown` — PR comment template with finding table and severity summary

The baseline file is committed to the repository and updated by a designated "accept baseline" workflow step run on the main branch after human review.

---

## 10. Pattern Catalog UI

The catalog is a browsable panel listing all registered patterns (built-in + custom + imported). Each entry shows:

- Pattern ID, severity badge, and tag chips (`concurrency`, `security`, `architecture`, `complexity`)
- Description and false-positive notes
- **User-reported FP rate** — percentage of findings users have marked as false positives, updated per-project
- **Current finding count** — live count of open findings for this pattern in the current project
- **Example trigger** — syntax-highlighted code snippet that would match the pattern
- **Example resolution** — syntax-highlighted snippet showing the corrected structure
- Enable/disable toggle, per-project, stored in `.codeflow/config.yaml`

Pattern entries link to the raw YAML source for built-in patterns and to the team registry for imported patterns. Clicking a pattern's finding count navigates to the findings dashboard pre-filtered to that pattern.

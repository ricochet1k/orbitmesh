# Anti-Pattern Detection and Code Quality Interface

**CodeFlow Explorer | Component: Anti-Pattern Engine & Findings Interface**
**Status:** Draft | **Audience:** Engineering, Security Audit

---

## 1. Philosophy: Structural Bugs vs Style Issues

Linters enforce convention. This system enforces structural correctness.

Traditional linters and static checks are necessary, but they mostly operate on local syntax/type context. They do not model deep graph relationships across a repository: transitive callers, ownership chains, spawn trees, lock order, taint paths, or architecture boundaries.

CodeFlow Explorer targets structural anti-patterns that emerge from graph shape, not single lines. Pattern evaluation runs on a Tree-sitter-backed, language-agnostic IR so rules can apply consistently across languages.

---

## 2. Built-in Pattern Library

Each pattern includes base severity (`critical`, `high`, `medium`, `low`), detection strategy, and false-positive risk.

| # | Pattern | Severity | FP Risk |
|---|---|---|---|
| 1 | **Execution-unit leak** — spawned unit has no reachable completion/cancellation path | Critical | Medium |
| 2 | **Lock order inversion** — two paths acquire sync resources in opposite order | Critical | Low |
| 3 | **Unguarded shared-state write** — shared write reachable from multiple execution units without synchronization | Critical | Low |
| 4 | **Unbounded message queue** — unbounded producer with no effective backpressure | High | Medium |
| 5 | **Send to potentially-closed channel/queue** — reachable close before send | Critical | High |
| 6 | **Trust boundary bypass** — external input reaches sensitive sink without sanitizer | Critical | Medium |
| 7 | **God type** — high field/member count with broad cross-module coupling | Medium | Low |
| 8 | **Architectural layer violation** — forbidden dependency edge across declared layers | High | Low |
| 9 | **Cancellation not propagated** — callee accepts cancellation token but caller drops it | Medium | Low |
| 10 | **Unbounded spawn in tight loop** — repeated spawn with no limit, pool, or backpressure | High | Medium |
| 11 | **Barrier/join counter misuse** — increment after wait/join on same scope | Critical | Medium |
| 12 | **Sync primitive copied by value** — lock/monitor copied by value semantics | High | Low |

---

## 3. Pattern DSL

Patterns are defined as backend-agnostic graph predicates compiled into the active store adapter (SurrealQL today).

**Reusable predicates:**
- `isExecutionUnit(n)`
- `acquires(n, resource)`
- `reachableFrom(a, b)`

**Example: Lock Order Inversion**

```yaml
id: lock-order-inversion
name: Lock Order Inversion
severity: critical
query: |
  MATCH (u1:ExecutionUnit)-[:CALLS*]->(a1:CallSite {method: "Lock", receiver: $lockA})
  MATCH (u1)-[:CALLS*]->(a2:CallSite {method: "Lock", receiver: $lockB})
  WHERE id(a1) < id(a2)
  MATCH (u2:ExecutionUnit)-[:CALLS*]->(b1:CallSite {method: "Lock", receiver: $lockB})
  MATCH (u2)-[:CALLS*]->(b2:CallSite {method: "Lock", receiver: $lockA})
  WHERE id(b1) < id(b2)
  AND u1 <> u2
  RETURN u1, u2, a1, a2, b1, b2
explain: |
  Execution unit {{u1.name}} acquires {{lock_a}} then {{lock_b}}.
  Execution unit {{u2.name}} acquires {{lock_b}} then {{lock_a}}.
  If both run concurrently, deadlock is possible.
```

---

## 4. User-Defined Patterns

Teams add custom patterns via YAML in `.codeflow/patterns/`.

```yaml
id: no-direct-db-from-handler
name: No Direct DB Access from Handler Layer
severity: high
tags: [architecture]
query: |
  MATCH (h:Package {layer: "handler"})-[:IMPORTS]->(d:Package {layer: "database"})
  RETURN h, d
```

Custom patterns are versioned in-repo and can be shared via pattern registry.

Projects can also define semantic mappings in `codeflow.semantic.yaml` so pattern predicates recognize local abstractions (worker pools, custom channel wrappers, request bridge helpers) as first-class graph constructs before rule evaluation.

---

## 5. Findings Lifecycle

A finding key is `(pattern_id, structural_fingerprint)` where fingerprint hashes the triggering graph shape (not line numbers).

State flow:

```text
open -> acknowledged -> reviewed -> suppressed
                             \-> fixed
```

Suppressions require justification (optional expiry).

When semantic mappings change, affected findings are recomputed and may re-fingerprint if the structural path changes.

---

## 6. Findings Dashboard

Dashboard elements:
- Severity donut
- 90-day trend line
- Top affected modules/packages
- Pattern frequency ranking
- New vs resolved since last scan
- AI-attributed code filter

---

## 7. Finding Detail View

Split panel:
- Left: matched graph path (typed nodes/edges)
- Right: snippets, rendered explanation, score breakdown, related findings, remediation guidance

Includes "Trace in main canvas" action to open full graph centered on the finding.

---

## 8. Severity and Risk Scoring

```text
risk_score = base_severity
           * reachability_factor
           * (1 - test_coverage)
           * hot_path_factor
           * ai_authorship_factor
```

Final score is normalized to 0-10 and mapped back to display severity.

---

## 9. Regression Mode

```bash
codeflow scan --baseline findings-main.json --threshold 3
```

CI emits deltas only; exit codes distinguish no/new/blocking findings. Output supports `json`, `sarif`, and `markdown`.

---

## 10. Pattern Catalog UI

Catalog includes built-in, custom, and imported patterns with:
- ID, severity, tags
- Description and FP notes
- Project FP rate
- Open finding count
- Trigger and resolution examples
- Per-project enable/disable toggle

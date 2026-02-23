# CodeFlow Explorer: Overview

**Status:** Design Proposal
**Date:** February 2026

---

## Problem Statement

The economics of software development are shifting. AI systems now write substantial portions of production codebases — often faster than any human reviewer can audit. The bottleneck is no longer *writing* code; it is *understanding* it.

Existing tooling is built around the assumption that humans are authors. IDEs optimize for editing. Static analyzers surface point defects. Linters enforce style. These tools answer the question "how do I write this correctly?" The question that matters now is different: **"what does this code actually do, and can I trust it?"**

Security reviewers must audit AI-generated code they did not specify and cannot fully predict. Architects need to verify that emergent structure matches intended design. Performance engineers must trace execution paths through code no single person has read end-to-end. AI system operators need to understand what changed between model versions and whether the new output is safe to ship.

CodeFlow Explorer is a response to this shift. It is a comprehension-first, analysis-second interface for understanding codebases at scale — designed for humans whose primary job is to *read and reason* about code rather than write it.

---

## Vision and Design Philosophy

CodeFlow Explorer treats a codebase as a **living system** rather than a collection of files. The primary abstractions are not files or functions but *flows*: execution flows that describe how control moves through concurrent processes, and data flows that describe how information is created, transformed, and consumed.

Three principles guide every design decision:

**1. Multiple lenses, unified model.** No single representation captures all aspects of a complex system. CodeFlow Explorer maintains a unified semantic model of the codebase and projects it through different analytical lenses depending on the question the user is asking. Switching lenses does not reload data — it reframes the same model.

**2. Progressive disclosure.** A 500,000-line codebase cannot be understood in one view. The interface begins at high-level topology and allows users to drill progressively deeper: from packages to types to functions to expressions. Every level of abstraction is explorable without losing context.

**3. Structural anomalies surface automatically.** Users should not have to know what they are looking for. The system identifies deviations from structural norms — unusual lock acquisition orders, data that crosses trust boundaries without transformation, goroutines that never terminate — and surfaces them as first-class findings.

**4. Project semantics are extensible.** Core analysis stays language-agnostic, but projects can declare framework- and library-specific semantics (for spawn/join APIs, message wrappers, boundary bridges, and endpoint-to-type mappings) without changing engine code.

---

## The Two Primary Lenses

### Execution Flow: Concurrency and Control

The execution flow lens answers: *how does this program run?*

It models goroutines (or threads, in non-Go targets) as first-class entities with lifecycle, communication topology, and synchronization dependencies. The view renders:

- **Goroutine spawn trees**: which goroutines create which children, and under what conditions
- **Channel graphs**: typed channels as edges between goroutine nodes, annotated with directionality and buffer capacity
- **Mutex/lock coverage**: which regions of code execute under which locks, where those regions overlap, and whether acquisition order is consistent across all call paths
- **Blocking point analysis**: where goroutines can block, for how long, and what unblocks them

This lens is the primary tool for concurrency auditors and performance engineers. Lock contention hotspots, deadlock-prone acquisition cycles, and goroutines that silently leak are structural facts that become visually obvious once the execution model is rendered.

### Data Flow: Types, Structs, and Transformations

The data flow lens answers: *where does this data come from, and what happens to it?*

It models values as nodes and transformations as directed edges. The view tracks:

- **Type lineage**: how concrete types are constructed, embedded, composed, and aliased
- **Field provenance**: for any struct field, the complete set of write sites and read sites
- **Transformation chains**: how a value at a trust boundary (an HTTP handler, a database read, a channel receive) is validated, sanitized, projected, or forwarded before it reaches a sink
- **Cross-boundary flows**: data that moves between privilege levels, serialization boundaries, or external interfaces

This lens is the primary tool for security reviewers and architects. Injection-class vulnerabilities, privilege escalation through implicit type conversion, and data that bypasses validation layers become traceable paths rather than implicit risks.

---

## Interface Paradigms

### Node Graph Canvas

The canvas is a spatially-arranged, zoomable graph. Nodes represent packages, types, goroutines, or functions depending on the current zoom level and active lens. Edges represent dependencies, data flows, or communication channels.

The canvas is not a static diagram — it is an interactive query interface. Users can:
- Select a node to anchor the view and hide irrelevant structure
- Filter edges by type, direction, or annotation
- Expand a collapsed cluster to inspect internal topology
- Mark nodes as reviewed, suspect, or out-of-scope

The layout algorithm respects semantic clusters (packages, modules, domains) while surfacing cross-cluster connections that may represent architectural violations.

### Flat Structured Interface

The flat view is a tabular, sortable, filterable representation of the same underlying model. Where the canvas excels at revealing topology, the flat view excels at systematic enumeration.

A security reviewer auditing all entry points can sort by boundary type, filter to unvalidated data flows, and work through findings row by row with full traceability back to source. An architect checking for layering violations can enumerate all cross-package dependencies and sort by dependency depth.

The two interfaces share selection state. Selecting a row in the flat view highlights the corresponding node in the canvas; selecting a canvas node filters the flat view to related entries. Users switch between paradigms based on the cognitive mode the task requires.

---

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Frontend (Browser)                    │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │  Graph Canvas │  │  Flat / Table│  │  Finding Panel│  │
│  └──────┬───────┘  └──────┬───────┘  └───────┬───────┘  │
│         └─────────────────┴──────────────────┘           │
│                    Unified View State                     │
└────────────────────────┬────────────────────────────────┘
                         │  Query / Event
┌────────────────────────▼────────────────────────────────┐
│                    API Gateway                           │
└──┬──────────────┬───────────────┬────────────────┬──────┘
   │              │               │                │
   ▼              ▼               ▼                ▼
┌──────┐    ┌──────────┐   ┌──────────┐    ┌───────────┐
│ AST  │    │   CFG    │   │   DFG    │    │  Pattern  │
│Index │    │ Builder  │   │ Analyzer │    │  Engine   │
└──┬───┘    └────┬─────┘   └────┬─────┘    └─────┬─────┘
   │             │              │                 │
   └─────────────┴──────────────┴─────────────────┘
                         │
              ┌──────────▼──────────┐
              │   Semantic Model DB  │
              │  (graph + indices)   │
              └─────────────────────┘
```

---

## The Five Major Subsystems

**1. AST Index** — Parses source into a queryable abstract syntax tree index. Resolves all symbols, imports, and type definitions. Serves as the authoritative source of structural identity for every other subsystem.

**2. Control Flow Graph Builder** — Constructs per-function CFGs and inter-procedural call graphs. Models goroutine spawns as CFG branches. Identifies all paths between any two program points.

**3. Data Flow Graph Analyzer** — Performs taint-style analysis across the call graph. Tracks value provenance from sources through transformations to sinks. Records type assertions, interface conversions, and serialization boundaries as transformation nodes.

**4. Pattern Engine** — Evaluates a library of structural anti-patterns against the semantic model. Patterns are composable queries over the graph; new patterns can be added without modifying the core engine. Ships with detectors for lock ordering violations, unguarded concurrent map access, unbounded goroutine growth, and trust boundary bypasses.

**5. Semantic Model DB** — A persistent, incrementally-updated graph database storing all derived facts from the four analysis subsystems. Serves the API layer with sub-second query response. Invalidated and rebuilt on file change in watch mode.

---

## Project Semantic Extensions

Projects can add semantic mappings in `codeflow.semantic.yaml` to teach the analyzer how local abstractions map to core graph primitives.

- **Spawn/join mappings** — map APIs like worker pools or wrapper helpers to `SPAWNS` and `JOINS` edges
- **Message wrapper mappings** — map custom queue/channel wrappers to `MessageChannel`, `SENDS_ON`, and `RECEIVES_FROM`
- **Boundary bridge mappings** — map client request sites to server handlers via `REQUESTS` edges
- **Endpoint type mappings** — map simple CRUD endpoints to domain types via `OPERATES_ON` edges

All project-defined edges carry confidence metadata (`certain`, `probable`, `possible`) and are visible in the UI confidence filter.

---

## Key Differentiators

| Dimension | Existing Tools | CodeFlow Explorer |
|---|---|---|
| Primary metaphor | File / function | Flow / topology |
| Concurrency model | Point warnings | Full goroutine graph |
| Data flow | Taint checkers (limited) | Cross-boundary lineage |
| Interface | Text output / IDE panel | Interactive spatial canvas |
| Anti-pattern scope | Style and correctness | Structural and architectural |
| Audience | Authors | Auditors |

---

## Target Users

**Architects** use the canvas to verify that the emergent structure of AI-generated code matches the intended domain model. They check that layering boundaries hold and that new packages do not introduce cycles.

**Security Reviewers** use the data flow lens and flat interface to enumerate trust boundary crossings, trace user-controlled input to sensitive sinks, and confirm that validation is present at every required point.

**Performance Engineers** use the execution flow lens to identify lock contention, goroutine leak patterns, and blocking paths in the critical path.

**AI System Operators** use the pattern engine findings as a regression signal across model versions. A new model that introduces a previously-absent anti-pattern is a quality signal independent of functional test results.

---

## Document Index

| Document | Subsystem |
|---|---|
| [01-backend-architecture.md](./01-backend-architecture.md) | Analysis pipeline, semantic model, API |
| [02-node-graph-canvas.md](./02-node-graph-canvas.md) | Spatial graph canvas interface |
| [03-flat-table-interface.md](./03-flat-table-interface.md) | Tabular structured interface |
| [04-execution-flow-interface.md](./04-execution-flow-interface.md) | Goroutine/thread execution view |
| [05-data-flow-type-lineage.md](./05-data-flow-type-lineage.md) | Data flow and type lineage view |
| [06-anti-pattern-detection.md](./06-anti-pattern-detection.md) | Anti-pattern engine and findings UI |

---

## Glossary

| Term | Definition |
|---|---|
| **Semantic Model** | The unified, queryable representation of all structural facts derived from static analysis |
| **Execution Flow** | The graph of goroutines, channels, and synchronization primitives that describes concurrent behavior |
| **Data Flow** | The directed graph of value creation, transformation, and consumption across the program |
| **Trust Boundary** | A point where data enters or exits a privilege domain: network ingress, IPC, deserialization |
| **CFG** | Control Flow Graph — a directed graph of basic blocks within a function, capturing all possible execution paths |
| **DFG** | Data Flow Graph — a graph capturing how values propagate and transform from definition to use |
| **Anti-pattern** | A structural configuration that is technically valid but carries elevated risk: a lock ordering violation, an unguarded concurrent write |
| **Lens** | A projection of the semantic model optimized for a specific analytical question |
| **Provenance** | The complete history of how a value arrived at a given program point: its origin, all transformations, and all prior read sites |

---

*CodeFlow Explorer is a proposal for a class of tooling that does not yet exist in complete form. The goal of this document is to establish the design vocabulary and architectural commitments that constrain future implementation decisions.*

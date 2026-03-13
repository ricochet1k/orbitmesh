# CodeFlow Read-After-Write Detection

## 1. Summary
The CodeFlow analysis engine supports the detection of "read-after-write" patterns across the codebase by tracking and propagating side-effect markers (`READ` and `WRITE`). Through project-level configuration files, developers can designate specific methods or functions as performing a read or a write. The static analysis engine propagates these markers as boolean tags upwards through the control flow graph to calling functions, intelligently handling higher-order functions (wrappers) and their closure arguments. This allows developers to construct simple, bounded Cypher queries to identify areas where a read operation sequentially follows a write operation.

## 2. Motivation
Identifying read-after-write scenarios is crucial for diagnosing consistency issues, understanding data staleness, optimizing caching strategies, and mapping out the chronological flow of side effects. Without an automated, engine-level capability, tracking these patterns manually through complex abstraction layers, helper wrappers, and asynchronous executions is error-prone and tedious. Providing a generic, graph-based querying mechanism empowers teams to proactively identify these patterns without introducing noisy, system-wide warnings for inconsequential findings.

## 3. Scope
* **In Scope**:
  * Project-level configuration syntax for marking specific function/method signatures as `READ` or `WRITE`.
  * Configuration syntax for defining the execution semantics of closure arguments (e.g., `called_immediately`, `spawned`, `stored`).
  * Static analysis engine modifications to assign and propagate these markers as properties (e.g., `effects: ["READ", "WRITE"]`) on Function and Call nodes.
  * Configuration syntax for defining propagation boundaries (e.g., HTTP request handlers or test boundaries) to stop effect inheritance.
  * Exposure of these properties for arbitrary querying via the existing `/api/v1/codeflow/query` Cypher endpoint.

* **Out of Scope**:
  * Active alerting, UI warnings, or continuous integration failures based on read-after-write findings.
  * Generating new, dedicated graph edges for effect propagation (properties are used instead for simplicity and performance).
  * Whole-program data-flow analysis or complete sequential execution path pre-computation beyond boundary limits.

## 4. Requirements & User Experience (UX)
* **Configuration:** Developers update their project's CodeFlow configuration file (e.g., `codeflow.yaml`) to map specific external library calls, database drivers, or internal core methods to `READ` or `WRITE` tags.
* **Wrapper Handling:** Developers configure higher-order helper functions (like `WithRetry(func())`) to specify how the passed closure is invoked, allowing the engine to know if the wrapper inherits the closure's side effects.
* **Querying:** Using the CodeFlow Explorer UI, users write generic Cypher queries to find sequential executions. For example: `MATCH (b:Boundary)-[:CALLS*]->(w:Call {effect: 'WRITE'})-[:NEXT*]->(r:Call {effect: 'READ'}) RETURN b, w, r`.
* **Bounded Contexts:** Developers define boundary markers (like `Handler` or `Test`) in the config. The engine halts the upward propagation of `READ`/`WRITE` tags at these boundary nodes to prevent the entire graph from becoming universally tagged, keeping queries fast and localized.

## 5. System Design & Architecture
* **Tag Propagation Mechanism:** Instead of creating new edges (which would bloat the graph and complicate queries), the engine assigns property arrays (`effects: ["WRITE"]`, `effects: ["READ"]`) to Call and Function nodes.
* **Bottom-Up Analysis:** The engine processes the AST bottom-up. When a function calls a marked `WRITE` method, the calling function node and the specific callsite node inherit the `WRITE` effect property.
* **Closure Execution Semantics:** The config supports a `closure_semantics` block for higher-order functions:
  * `called_immediately`: The wrapper blocks and executes the closure synchronously. The wrapper inherits the closure's effects.
  * `spawned`: The wrapper executes the closure asynchronously (e.g., in a goroutine). The wrapper itself does not inherit the synchronous control-flow effects, but a separate semantic link may be maintained for async tracing.
  * `stored`: The wrapper saves the closure for later execution (e.g., registering a callback). Effects are not immediately inherited.
  * **Inference:** Where possible, the static analyzer attempts to infer these semantics directly from the wrapper's function body if not explicitly configured.
* **Boundary Halting:** When the bottom-up propagation reaches a Function node marked as a `Boundary` (via config or inferred annotations), the `effects` properties are not passed to functions that call the Boundary.

## 6. Security & Privacy
There are no new security or privacy implications introduced by this feature. It operates entirely on static code analysis within the already-secured CodeFlow execution environment.

## 7. Testing Plan
* **Unit Tests:**
  * Verify the parser correctly reads the new `effects`, `boundaries`, and `closure_semantics` configuration blocks.
  * Test the bottom-up propagation logic to ensure tags correctly bubble up to callers.
  * Test boundary halting to ensure tags do not propagate past designated boundary nodes.
* **Integration Tests:**
  * Provide a mock Go project containing a `WRITE` database call, a generic `WithTransaction` wrapper (configured as `called_immediately`), a `READ` call, and an HTTP Handler boundary.
  * Run the CodeFlow analyzer on the mock project and verify the resulting `goraphdb` state contains the correct `effects` properties on the expected nodes.
* **Cypher Query Verification:** Ensure that `MATCH` queries utilizing the `effects` properties and `NEXT` sequential edges return the correct paths in the mock project.

## 8. Rollout & Deployment
* **Database Impact:** This feature adds array properties to existing node types. It does not require any destructive database migrations, but existing CodeFlow graphs will need to be re-analyzed to populate the new properties.
* **Rollout:** Deployed transparently as an update to the CodeFlow analyzer engine. Projects will opt-in by updating their configuration files.

## 9. Alternatives Considered
* **Dedicated Graph Edges for Propagation:** Considered creating `HAS_EFFECT` or `CALLS_WRITE` edges pointing from callers to the original effect nodes. Rejected because it dramatically increases edge count and requires complex, recursive Cypher traversals just to determine if a function eventually performs a write. Node properties are significantly faster for filtering.
* **Whole-Program Sequential Pathing:** Considered having the engine pre-compute exactly which reads follow which writes globally. Rejected as computationally prohibitive and unnecessary; utilizing the graph database's `[:NEXT*]` traversal bounded within specific context boundaries achieves the goal efficiently at query time.

## 10. Implementation Plan
* [ ] Update the CodeFlow project configuration schema to support `effects` (Read/Write) mappings.
* [ ] Update the configuration schema to support `closure_semantics` mapping (`called_immediately`, `spawned`, `stored`).
* [ ] Update the configuration schema to support `boundaries` definitions.
* [ ] Implement bottom-up property propagation in the static analysis engine to assign `effects` to Function and Call nodes.
* [ ] Implement static inference logic to guess closure execution semantics from function bodies when unconfigured.
* [ ] Implement propagation halting logic at designated Boundary nodes.
* [ ] Update `goraphdb` insertion logic to persist the `effects` array property on nodes.
* [ ] Write unit and integration tests verifying correct propagation and querying on a mock codebase.

## 11. Open Questions
None.
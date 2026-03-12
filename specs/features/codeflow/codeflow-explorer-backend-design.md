# CodeFlow Explorer Backend Design

## 1. Summary
The CodeFlow Explorer Backend is responsible for providing the data necessary to drive the interactive CodeFlow visualizations in the frontend. It manages the execution of a generic, customizable static analyzer against a codebase, stores the resulting structural graph (nodes and edges) into a `goraphdb` graph database, and serves this data to the frontend via high-performance REST APIs. It ensures that the visualizations reflect the current state of the codebase by executing the analyzer live and incrementally using a file watcher.

## 2. Motivation
To provide a smooth, interactive developer experience, the CodeFlow Explorer UI (built with `sigma.js` and `graphology`) requires a fast, low-latency, and reliable source of graph data. The backend must abstract away the complexities of static analysis and database querying, presenting a clean REST API that the frontend can easily consume. Furthermore, as code changes, developers need immediate visual feedback; therefore, the backend must actively monitor the codebase and incrementally update the graph data in real-time, preventing the UI from becoming stale.

## 3. Scope
* **In Scope**:
    * Execution engine for the generic static analyzer, driven by language-agnostic project-level configurations.
    * Integration with a file watcher to trigger live, incremental static analysis runs upon code changes.
    * Ingestion and storage of analyzer output (nodes and edges) into the `goraphdb` graph database.
    * Implementation of REST API endpoints to serve the current state of the codebase graph to the frontend.
    * Enforcement of generous but necessary limits on graph data retrieval to maintain performance and prevent overwhelming the frontend.
    * Conversion of graph data into a format easily consumable by `sigma.js`/`graphology`.
* **Out of Scope**:
    * Management of historical data, versioning, or snapshotting of the graph (the backend will only represent the *latest current state*).
    * Writing specific language parsers (the analyzer itself is generic and configured via rules).
    * Providing GraphQL or standard RPC (e.g., gRPC) endpoints for this specific feature.

## 4. Requirements & User Experience (UX)
* **Live Updates**: When a user modifies a file in the observed project, the backend file watcher should detect the change, trigger an incremental analysis of the affected files, update `goraphdb`, and make the updated graph available immediately.
* **REST API Surface**:
    * `POST /api/codeflow/query`: Execute an arbitrary, read-only Cypher query against `goraphdb`. The frontend crafts specific queries to fetch nodes and edges for its exact visualization context.
        * If the JSON payload includes `"live": true`, the endpoint holds the connection open as a Server-Sent Events (SSE) stream. It first sends the initial result set, and subsequently pushes updated result sets (or diffs) whenever the underlying data relevant to the query changes.
* **Performance**: The query API must execute read-only queries with low latency to support interactive exploration. The live query SSE stream ensures the frontend receives updates efficiently without polling and without being overwhelmed by global graph mutations irrelevant to the current view.
* **Limits**: The query API must implement strict execution timeouts and sensible hard limits on the number of records returned to prevent heavy arbitrary queries from crashing the backend or the frontend browser.

## 5. System Design & Architecture
* **File Watcher Component**: A continuous background process (e.g., using `fsnotify` in Go) that monitors the target project directory. It debounces file system events and queues them for processing.
* **Analyzer Pipeline**:
    * A worker pool consumes events from the file watcher queue.
    * For modified files, it invokes the generic static analyzer using the project's custom configuration rules.
    * The analyzer emits a stream of abstract nodes and edges.
* **Database Integration & Hybrid Live Query Engine**:
    * The backend translates the analyzer's output into Cypher mutation queries (upserts/MERGE for new entities, deletions for removed entities).
    * Upon successful commit to `goraphdb`, the database integration layer fires mutation events into an internal broadcaster channel.
    * **Live Query Re-evaluation**: When a `POST /query` request is made with `"live": true`, the backend parses the incoming Cypher query to extract the specific node labels (e.g., `:Function`, `:Class`) and edge relationship types (e.g., `[:CALLS]`, `[:IMPLEMENTS]`) being requested.
    * The active SSE connection subscribes *only* to internal mutation events matching those extracted types.
    * When a relevant mutation occurs, the backend debounces the trigger (e.g., 500ms) to batch rapid file changes, re-evaluates the full Cypher query, and pushes the updated results down the SSE stream.
* **REST API Layer**:
    * Built using standard Go HTTP routing (e.g., `net/http` or a lightweight router).
    * The `POST /query` handler accepts the Cypher string, parses it for safety (enforcing read-only status) and live-subscription types, sets strict context timeouts for initial and subsequent evaluations, and handles the SSE connection lifecycle.

## 6. Security & Privacy
* **Access Control**: API requests must include standard CSRF headers and rely on the existing session authentication middleware.
* **Query Safety**: Allowing arbitrary Cypher queries introduces risk. The backend MUST strictly enforce read-only transaction execution for `POST /api/codeflow/query`. The backend should intercept or block any queries containing mutation keywords (e.g., `CREATE`, `MERGE`, `DELETE`, `SET`).
* **Resource Exhaustion (DoS)**: Arbitrary queries can be computationally expensive (e.g., unbounded Cartesian products). The API must enforce strict query execution timeouts (e.g., `context.WithTimeout` in Go) and limit the maximum number of result rows returned.
* **Concurrent Live Queries**: The backend should enforce a limit on the number of concurrent "live" SSE queries per user/session to prevent connection exhaustion and database overload. While not a primary concern for the prototype phase, the architectural hook for this limit must exist.
* **Directory Traversal**: The file watcher and analyzer must be strictly sandboxed to the target project directory.

## 7. Testing Plan
* **Unit Tests**:
    * Test the REST API handlers using mocked `goraphdb` clients to verify correct JSON serialization, error handling, and limit enforcement.
    * Test the file watcher debouncing logic.
* **Integration Tests**:
    * End-to-end analyzer pipeline: Write a file, trigger the analyzer, and verify the correct nodes and edges are written to a test instance of `goraphdb`.
    * Test incremental updates: Modify an existing file and ensure the graph in `goraphdb` correctly reflects the changes (including deletions of old edges).
* **E2E Tests**: Use Playwright to verify that frontend interactions (like opening the CodeFlow viewer) correctly call the REST endpoints and render the expected graph structure.

## 8. Rollout & Deployment
* **Migrations**: Ensure `goraphdb` is properly initialized with any required indexes (e.g., on node IDs or file paths) to ensure fast lookup times for the REST APIs.
* **Feature Flags**: The file watcher and automatic analyzer execution should be configurable (e.g., enabled/disabled via a feature flag or environment variable) to prevent performance degradation on resource-constrained deployments if not in active use.
* **Monitoring**: Add logging and metrics for API response times, file watcher event volume, and analyzer execution duration to monitor system health.

## 9. Alternatives Considered
* **GraphQL / gRPC**: Considered for flexible graph querying, but REST was chosen for simplicity, ease of caching, and direct alignment with current project architectural preferences.
* **Batch vs. Incremental Analysis**: Considered running the analyzer only on demand or in large batches. Rejected in favor of live, incremental analysis via a file watcher to ensure the frontend always displays the most up-to-date representation of the code.

## 10. Implementation Plan
* [ ] Initialize REST API routing and placeholder handlers for `/api/codeflow/*`.
* [ ] Implement `goraphdb` integration for querying nodes and edges with defined safety limits.
* [ ] Build the background file watcher service using `fsnotify`.
* [ ] Integrate the generic static analyzer engine to run incrementally on file changes.
* [ ] Implement the database mutation logic (Cypher queries) to sync analyzer output to `goraphdb`.
* [ ] Write unit and integration tests for the API, watcher, and analyzer pipeline.
* [ ] Finalize the JSON response format in coordination with frontend `graphology` requirements.

## 11. Open Questions
* Are there specific directories (like `node_modules` or `vendor`) that should be globally ignored by the file watcher, or is this entirely driven by the project-level analyzer configuration?
* What is the exact schema expected by the frontend's `graphology` instance (e.g., specific attribute names for styling/layout)?

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
    * `GET /api/codeflow/nodes`: Retrieve nodes, with optional filtering (e.g., by type, directory, or search query) and pagination/limits.
    * `GET /api/codeflow/edges`: Retrieve edges, typically scoped to a set of nodes or a specific bounding context, with limits.
    * `GET /api/codeflow/graph`: Retrieve a combined payload of nodes and edges for a specific context (e.g., neighborhood of a node), formatted for direct ingestion by `graphology`.
* **Performance**: The API must respond with low latency to support interactive exploration, zooming, and panning in the frontend canvas.
* **Limits**: The API must implement sensible upper bounds on the number of nodes/edges returned in a single request to prevent browser crashes, even if these limits are fairly generous (e.g., 5,000 - 10,000 elements).

## 5. System Design & Architecture
* **File Watcher Component**: A continuous background process (e.g., using `fsnotify` in Go) that monitors the target project directory. It debounces file system events and queues them for processing.
* **Analyzer Pipeline**:
    * A worker pool consumes events from the file watcher queue.
    * For modified files, it invokes the generic static analyzer using the project's custom configuration rules.
    * The analyzer emits a stream of abstract nodes and edges.
* **Database Integration (`goraphdb`)**:
    * The backend translates the analyzer's output into Cypher queries.
    * For incremental updates, it performs upserts (MERGE) for new/modified entities and deletions for removed entities to keep the graph synced with the current file state.
* **REST API Layer**:
    * Built using standard Go HTTP routing (e.g., `net/http` or a lightweight router).
    * Handlers execute read-only Cypher queries against `goraphdb`.
    * Data is serialized into JSON. The exact structure will be negotiated with the frontend to ensure minimal transformation is needed before loading into `graphology` (e.g., an object with `nodes` and `edges` arrays containing `id`, `label`, `attributes`, `source`, `target`).

## 6. Security & Privacy
* **Access Control**: API requests must include standard CSRF headers (as required by the OrbitMesh frontend framework) and rely on the existing session authentication middleware to ensure only authorized users can view the codebase graph.
* **Directory Traversal**: The file watcher and analyzer must be strictly sandboxed to the target project directory to prevent arbitrary file reads or execution outside the permitted workspace.
* **Injection**: All inputs to the REST API (e.g., search queries, node IDs) must be sanitized and parameterized when constructing Cypher queries to prevent graph database injection attacks.

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

# Customizable CodeFlow Analysis Rules

## 1. Introduction

CodeFlow aims to be a powerful graph-based representation of a codebase, modeling both its structure and execution/data flow. However, different projects and frameworks have vastly different idioms. A function that represents a thread spawn in one framework (`go func()`) might look like a simple higher-order function in another (`Promise.then()`). An HTTP framework might have a unique way of registering routes. Certain data types might be mere wrappers that should be semantically unwrapped.

To provide maximum utility without hardcoding every framework into the core analyzer, CodeFlow requires a customizable, language-agnostic mechanism to define how specific code structures should be represented in the graph.

**Core Philosophy**: The static analyzer should be a generic engine. It does not "understand" what a "Validator" or a "Taint Sink" is. Instead, it applies a set of project-specific rules to emit nodes with specific `Tags` (labels) and connect them with custom `Edge Types`. The actual "analysis" (e.g., taint tracking, concurrency analysis) and visualization are emergent properties of how these generic graph components are queried (via Cypher) and presented in the frontend.

## 2. Configuration Format & Scope

The configuration should reside in a project-level file, for example, `codeflow.yaml`. This file serves as the bridge between the codebase's specific semantics and the generic CodeFlow graph model.

```yaml
version: "1"
# Rules define what structures to match and what graph mutations to apply
rules:
  # ... (detailed below)

# Visuals define how specific tags or edges should be presented in the frontend
visuals:
  nodes:
    - match_tag: "Validator"
      color: "green"
      icon: "shield"
  edges:
    - match_type: "VALIDATES"
      stroke_dash: "dashed"
      color: "blue"
```

## 3. Structural Target Matching

To apply rules, we need a robust, language-agnostic way to identify targets in the AST (Abstract Syntax Tree). The matcher must account for:

- **Package/Module namespace:** `github.com/gin-gonic/gin`, `express`.
- **Receiver/Class/Trait:** `*Context`, `Router`.
- **Method/Function Name:** `BindJSON`, `get`.
- **Parameters & Literals:** Matching based on specific constant values or types passed as arguments.
- **Generics/Type Parameters:** Matching generic instantiations.

We propose a **Structural Signature Matcher**, utilizing a glob-like syntax combined with structural constraints.

### 3.1 Signature Syntax

A signature string consists of: `[Package/Module]::[Receiver].[Method]([Arguments]) -> [Returns]`

*   `*` can be used as a wildcard for any segment.
*   `**` can be used for recursive wildcard matching (e.g., in package paths).

**Examples:**

*   **Match any method named `Query` in the `database/sql` package:**
    `database/sql::*DB.Query(*)`
*   **Match an express `get` route where the first argument is a string literal:**
    `express::*Router.get(StringLiteral, *)`
*   **Match any function that returns an `error` interface:**
    `*::*.*(*) -> error`

### 3.2 Advanced Parameter Matching

Sometimes, the identity or behavior of a function depends entirely on a literal parameter. For example, a generic `GetEnv(key string)` function.

```yaml
rules:
  - id: "env_read"
    match:
      signature: "os::*.Getenv($key: StringLiteral)"
    # Extract the matched parameter to define the node's identity
    node:
      identity_expansion: "$key"
      tags: ["Source", "Environment"]
```

This tells the analyzer: Do not collapse all `os.Getenv` calls into one node. Instead, create a distinct node for `os.Getenv("AWS_SECRET_KEY")` and `os.Getenv("PORT")`.

## 4. Graph Mutations: Nodes and Edges

When a rule matches a structure in the code, it instructs the analyzer to emit specific graph entities.

### 4.1 Tagging Nodes (Labels)

The simplest operation is appending tags (labels in Neo4j/GoraphDB) to the AST node (e.g., a Function, CallSite, or Type).

```yaml
rules:
  - id: "mark_sql_sinks"
    match:
      signature: "database/sql::*DB.Exec(*)"
    node:
      tags: ["Sink", "SQL"]
```

### 4.2 Emitting Custom Edges

Rules can define relationships between arguments, receivers, and returns.

```yaml
rules:
  - id: "mark_validator"
    match:
      signature: "myapp/validate::*.User($arg1)"
    edge:
      type: "VALIDATES"
      from: "$arg1" # Refers to the first argument
      to: "self"    # Refers to the matched function node
```

### 4.3 Control Flow Assertions

For functions like validators, we need to express *how* they affect control flow. Does it return a boolean indicating validity, or does it abort the thread (e.g., panic, or an HTTP 400 response helper)?

```yaml
rules:
  - id: "http_abort_helper"
    match:
      signature: "github.com/gin-gonic/gin::*Context.AbortWithError(*)"
    control_flow:
      terminates_execution: true
```

## 5. Specific Use Cases & Examples

### 5.1 Type Wrappers (Explicit Unwrapping)

Many codebases use wrapper types (e.g., `sql.NullString` in Go, or custom `Option<T>` types). Data flow analysis shouldn't track the wrapper struct; it should track the inner value. We must define this explicitly.

```yaml
rules:
  - id: "unwrap_sql_nullstring"
    match:
      type: "database/sql::NullString"
    data_flow:
      track_field: "String" # Instructs the analyzer to map flow into the wrapper directly to this field
```

### 5.2 Concurrency / Thread Spawners

Different languages spawn asynchronous tasks differently.

**Go Example (Goroutine):**
*Handled natively by language semantics, but custom pool wrappers exist.*
```yaml
rules:
  - id: "custom_worker_pool"
    match:
      signature: "myapp/pool::*Worker.Submit($fn: Function)"
    edge:
      type: "SPAWNS"
      from: "self"
      to: "$fn"
```

**JavaScript Example (Promises/SetTimeout):**
```yaml
rules:
  - id: "js_settimeout"
    match:
      signature: "global::*.setTimeout($fn: Function, *)"
    edge:
      type: "SPAWNS"
      from: "self"
      to: "$fn"
```

### 5.3 Framework-Specific API Handlers

Frameworks define routes dynamically. We need to map the registration call to the actual handler function and extract the route path.

```yaml
rules:
  - id: "express_route"
    match:
      signature: "express::*Router.$method(Literal('get'|'post'|'put'|'delete'), $path: StringLiteral, $handler: Function)"
    emit:
      node:
        label: "APIHandler"
        properties:
          method: "$method"
          path: "$path"
      edge:
        type: "HANDLES_ROUTE"
        from: "APIHandler"
        to: "$handler"
```

## 6. Frontend & Query Integration

Because the analyzer purely emits generic nodes/edges/tags, the power lies in how the frontend queries `goraphdb`.

The configuration file allows projects to map these arbitrary tags to presentation attributes.

```yaml
visuals:
  queries:
    - id: "taint_path"
      cypher: "MATCH p=(s:Source)-[*]->(t:Sink) RETURN p"
      presentation:
        path_color: "red"
        highlight: true
  node_attributes:
    - match_tag: "Sink"
      shape: "octagon"
      color: "darkred"
```

This allows the user to invent entirely new forms of analysis. For example, a user could tag nodes as `AllocatesHeavyMemory` and write a Cypher query in the frontend config to highlight paths in the execution flow that hit these nodes frequently.

## 7. Future Extensibility

While this design focuses purely on **static analysis** and AST parsing, the tagging and generic edge system is highly extensible.

*   **Runtime Tracing:** In the future, dynamic analysis tools could emit edges (e.g., `OBSERVED_CALL`) that overlay onto the static graph. Because the frontend relies on Cypher queries and generic visual rules, displaying runtime data would simply require updating the frontend queries to look for these new edge types.
*   **Code Coverage:** Coverage data could map to a `COVERAGE` property on `File` or `ExecutionUnit` nodes, which the `visuals` block could map to node opacity or color saturation.

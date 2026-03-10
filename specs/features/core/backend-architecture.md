# Backend Architecture

## 1. Summary
This specification details the core architecture of the OrbitMesh Backend execution engine. Written in Go, the backend manages autonomous agent execution, durable state persistence, tool/dependency orchestration, and real-time frontend communications. The primary goal of this architecture is to provide separation of concerns and clean abstraction boundaries across the application stack.

## 2. Motivation
As OrbitMesh scales to handle more complex developer workflows and agentic executions, the backend must be robust, scalable, and easy to reason about. A well-defined architectural blueprint prevents code coupling, simplifies testing, and provides clear boundaries for where different types of logic (e.g., business rules vs. data access vs. external APIs) should live. This reduces technical debt and accelerates feature development.

## 3. Scope
* **In Scope**: High-level structural overview of the Go backend, covering Domain, Entity, Storage, Service, Provider, API/Presentation, and Tooling layers. It defines the core data flow, dependency injection patterns, and separation of concerns.
* **Out of Scope**:
  - Deep-dive specifications into specific sub-systems (e.g., Session Lifecycle, Realtime Websockets, PTY advanced extraction, or specific MCP client behaviors). These will be detailed in their own respective sub-feature specs.
  - The `goraphdb` graph database and Cypher query integrations. These are explicitly part of the CodeFlow sub-system and will be covered in CodeFlow-specific architectural documents.

## 4. Requirements & User Experience (UX)
While this is a purely technical backend architecture specification, it directly impacts the reliability and responsiveness experienced by the user:
* **Separation of Concerns**: Different packages must have distinct, single responsibilities (e.g., `internal/api` handles HTTP parsing; `internal/service` handles business logic; `internal/storage` handles file I/O).
* **Predictability & Reliability**: The `Store[T,S] + Handle[T,S]` pattern must be used to ensure durable objects are safely mutated without race conditions, enabling a reliable execution engine.
* **Pluggability**: Interfaces must be used to decouple components, allowing for easy mocking during testing and swapping implementations (e.g., swapping a JSON file store for a remote database store).

## 5. System Design & Architecture
The backend is structured into distinct layers to enforce clean boundaries:

### Domain Layer (`internal/domain`)
The core foundation of the application. It contains pure data structures and fundamental business logic types (e.g., `Session`, `Terminal`, `Message`, `ToolCall`).
* **Rules**: Must not depend on any other internal package. Must be free of I/O, database awareness, or transport-level concepts.

### Entity Layer (`internal/entity`)
Provides the central abstraction for durable object lifecycle management, utilizing the `Store[T,S] + Handle[T,S]` pattern.
* **Responsibilities**: Manages per-entity locks, serialization/deserialization via snapshotting, in-memory caching, cross-entity dependency tracking (wakeup graphs), and publishes change events to the event bus.
* **Rules**: Isolates persistence logic from mutation logic to prevent race conditions.

### Storage Layer (`internal/storage`)
Handles all physical data persistence and retrieval.
* **Responsibilities**: Implements the `TypedStorage[S]` interface required by the Entity layer. Currently uses JSON file-based storage. Also handles log streaming, raw file access, and registry lookups.
* **Rules**: Should be ignorant of business rules and state machine transitions; it only reads/writes bytes and formats.

### Service Layer (`internal/service`)
The application logic and use case orchestrator.
* **Responsibilities**: Contains managers and coordinators like `AgentExecutor`, `EvalCoordinator`, and event broadcasting. Orchestrates the flow of data between the Entity store, Tool evaluators, and Providers.
* **Rules**: Defines the "what happens when" but delegates the "how" to the Entity layer for state mutations and Storage layer for persistence.

### Tooling & Integrations (`internal/tools`, `internal/toolcall`)
Manages the registration, parsing, and execution of tools available to agents.
* **Responsibilities**: Orchestrating tool evaluations, maintaining the registry of local and remote (MCP) tools, and managing dependency relationships between tool evaluations.

### Provider Layer (`internal/provider`)
Abstractions and implementations for external AI models and LLM providers (e.g., Claude, Codex).
* **Responsibilities**: Translating OrbitMesh's internal standard domain messages to provider-specific payloads, handling API interactions, and parsing streaming responses back into standardized events.

### API & Presentation Layer (`internal/api`, `internal/mcpws`, `internal/realtime`)
The transport boundary communicating with the SolidJS frontend and external clients.
* **Responsibilities**: HTTP routing (via `go-chi`), request validation, JSON serialization/deserialization, CSRF/CORS middleware, and managing real-time WebSocket connections for event feeds and terminal emulators.
* **Rules**: Must delegate all business operations to the Service layer; must not contain business logic.

### Dependency Injection (`cmd/orbitmesh/main.go`)
The top-level `main.go` acts as the composition root.
* **Responsibilities**: Wire together the application. It initializes storage, injects storage into services, mounts HTTP handlers, sets up the provider factory, and manages graceful shutdowns and drain contexts.

## 6. Security & Privacy
* **API Boundaries**: The API layer enforces CSRF protection and CORS policies.
* **Resource Leaks**: The architecture utilizes `context.Context` throughout the Service and Entity layers to ensure goroutines (like Agent run loops or long-running PTY sessions) are reliably canceled, preventing resource exhaustion.

## 7. Testing Plan
The layered architecture heavily facilitates testing:
* **Unit Testing**: The Domain and Entity layers can be tested in isolation. The `Store[T,S]` pattern allows for passing in-memory storage implementations, preventing slow disk I/O during tests.
* **Mocking**: The Service layer's reliance on interfaces (e.g., Provider factories, Storage adapters) enables injecting mock dependencies (e.g., `mockProvider`) to simulate LLM behavior without hitting external network endpoints.
* **Integration Testing**: The `internal/api` package contains handler tests that assemble simplified Service, Entity, and Storage layers to test end-to-end request flows.

## 8. Rollout & Deployment
* No immediate database migrations are required as this spec mostly documents the existing structure and the ongoing transition to the `entity` package model.
* Enhancements aligning with this architecture will be deployed iteratively without feature flags, as they primarily involve internal structural refactoring rather than new user-facing behavior.

## 9. Alternatives Considered
* **Self-managing Lock-Per-Entity**: An approach where each Domain object carried its own mutex and persistence logic. Rejected because it led to lock-ordering deadlocks and race conditions where state was written to disk outside of the protective lock. The current `Store[T,S]` + `Handle[T,S]` model solves this by serializing mutations and deferring I/O.
* **Global Store Lock**: Rejected because it would serialize all reads across the system, causing unnecessary contention. The chosen per-entity lock model allows concurrent reads of different entities.

## 10. Implementation Plan
- [x] Document the current backend architectural layers.
- [x] Emphasize the separation of concerns and the `entity` package pattern.
- [x] Note CodeFlow exclusions.

## 11. Open Questions
* How should we transition legacy components (like older storage routines or event broadcasters) fully into the newer `entity` model? This will likely be addressed as technical debt in a future iteration.

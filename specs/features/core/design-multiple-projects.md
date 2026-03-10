# Feature Spec: Multiple Projects Design

## 1. Summary
OrbitMesh currently operates as a single-tenant, globally scoped platform where all agents, sessions, tasks, and data reside in a single environment. The "Multiple Projects Design" feature introduces the concept of isolated "Projects" within OrbitMesh. Each project will act as a completely independent workspace with its own dedicated database, codebase configuration, and agent workflows. This allows users to manage multiple, disparate software projects and agentic workflows concurrently without any cross-contamination of data or state.

## 2. Motivation
As OrbitMesh scales to handle more complex developer workflows and diverse codebases, the single-global-state model becomes a limitation. Users need the ability to isolate different codebases and different agent workflows. For example, an agent working on a Python backend should not have access to the session history or tasks of an agent working on a separate React frontend project unless explicitly designed.

By introducing multiple projects, we solve the problem of data entanglement, improve system organization, and pave the way for future multi-tenant or team-based access control (though RBAC itself is out of scope for this initial implementation). The impact of not building this is a cluttered, unmanageable global state that cannot effectively support users working on more than one distinct codebase simultaneously.

## 3. Scope
* **In Scope**:
  * Definition and creation of "Projects" via the OrbitMesh user interface.
  * Complete data isolation: each project will be backed by its own entirely separate `goraphdb` graph database instance/file.
  * API restructuring: routing all relevant existing and future backend API endpoints to be project-scoped (e.g., `/api/v1/projects/{project_id}/sessions`).
  * UI updates: a top-level project selector dropdown that filters all subsequent views and data contexts to the selected project.
  * Tying all entities (sessions, terminals, tasks, agents) strictly to their owning project.
* **Out of Scope**:
  * Role-Based Access Control (RBAC) or specific security roles (e.g., Project Admin, Viewer).
  * Data migration scripts for existing global data (since we are in the prototyping phase, existing un-scoped data will be ignored or manually wiped).
  * Project creation via local filesystem configuration files (e.g., detecting an `orbitmesh.yaml` in a directory).

## 4. Requirements & User Experience (UX)
* **Project Creation Flow**: A user can navigate to a "Projects" management page or use a "New Project" button in the global navigation. They will provide a project name and optionally a path to the local codebase directory.
* **Global Project Context**: The main UI will feature a persistent, top-level project selector (dropdown). Once a project is selected, the entire UI (Dashboards, Agent Session Viewers, Task Trees, CodeFlow) will only display data, sessions, and tasks belonging to that specific project.
* **Total Isolation**: Users should feel like they are in a completely separate instance of OrbitMesh when they switch projects. There should be no leakage of tasks, active terminal sessions, or agent history between projects.

## 5. System Design & Architecture
* **Database Partitioning**: Instead of a single `goraphdb` connection held by the backend engine, the backend will implement a Project Database Manager. When a project is created, a new, entirely separate `goraphdb` database file/instance is provisioned on disk. The backend will route database queries to the correct connection pool based on the active `project_id`.
* **API Routing**:
  * The backend routing layer (e.g., `backend/internal/api`) will be refactored.
  * Existing generic endpoints like `GET /api/v1/sessions` will move to `GET /api/v1/projects/{project_id}/sessions`.
  * A new set of endpoints for managing the projects themselves will be introduced (e.g., `POST /api/v1/projects`, `GET /api/v1/projects`).
* **Entity Ownership**: Domain entities will implicitly belong to the project database they reside in. The backend will ensure that cross-database queries are not possible by design.
* **In-Memory State**: Active execution engines, websocket connections, and PTY terminal sessions must be tracked internally by the backend alongside their `project_id` to ensure events are broadcasted only to users viewing that specific project.

## 6. Security & Privacy
* **Isolation by Default**: By strictly separating the underlying `goraphdb` instances, we drastically reduce the risk of accidental data leakage between projects via complex graph queries.
* **Authentication/Authorization**: No new authz requirements (like RBAC) are introduced in this phase. The focus is on logical and physical data separation rather than user permission boundaries.

## 7. Testing Plan
* **Backend Unit/Integration Tests**:
  * Verify the Project Database Manager correctly initializes and routes queries to separate `goraphdb` instances.
  * Ensure that entities created in Project A cannot be retrieved via queries executed against Project B's database connection.
  * Test the new project-scoped API endpoints to ensure they return 404s or errors when attempting to access entities across project boundaries.
* **Frontend E2E Tests**:
  * Use Playwright to simulate creating two projects, generating a session in Project A, and verifying that the session does not appear in Project B's dashboard.
  * Test the top-level project selector to ensure it correctly updates the active context and fetches the correct project-scoped data.

## 8. Rollout & Deployment
* **Data Migration**: No automated data migration will be provided. Existing global database files will be considered legacy and can be manually deleted by the user/developer. The platform will boot clean and require the creation of an initial project.
* **Rollout Strategy**: Immediate rollout upon merge. Since the platform is in a prototyping phase with no public users, backwards compatibility of the API and database schema is not required.

## 9. Alternatives Considered
* **Single Database with `project_id` Properties**: We considered keeping a single `goraphdb` instance and adding a `project_id` property to every single node and relationship in the graph.
  * *Why Rejected*: This increases query complexity, requires massive rewrites of all existing Cypher queries to ensure the `project_id` filter is never forgotten, and increases the risk of accidental data leakage. Completely separate databases provide a stronger, simpler isolation boundary.
* **Project Configuration Files (`orbitmesh.yaml`)**: We considered auto-discovering projects based on configuration files located in the user's filesystem.
  * *Why Rejected*: While useful for developer experience, it introduces complexity in filesystem monitoring and state synchronization between the disk and the database. UI-driven creation provides a clearer, more explicit source of truth for the prototype phase.

## 10. Implementation Plan
* [ ] **Backend**: Create the Project domain entity and database schema for tracking available projects (likely requires a small "meta" or "master" database to track projects, or directory scanning of project DB files).
* [ ] **Backend**: Implement the Project Database Manager to handle dynamic connections to multiple `goraphdb` instances.
* [ ] **Backend**: Refactor all existing REST API routes in `backend/internal/api` to be nested under `/api/v1/projects/{project_id}/`.
* [ ] **Backend**: Refactor backend execution engine and session managers to be project-aware.
* [ ] **Frontend**: Implement the "Create Project" UI and top-level project selector dropdown.
* [ ] **Frontend**: Refactor all API client calls and tanstack-router routes to include and require the `project_id` parameter.
* [ ] **Testing**: Update all Go and Vitest/Playwright tests to accommodate the new project-scoped architecture.

## 11. Open Questions
* How should the backend store the master list of projects? Should there be a single lightweight SQLite/JSON "master" database that just holds the list of projects and paths to their respective `goraphdb` instances, or should the backend simply scan a designated directory for project database folders?
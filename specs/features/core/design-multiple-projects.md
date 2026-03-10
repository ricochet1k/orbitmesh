# Feature Spec: Multiple Projects Design

## 1. Summary
OrbitMesh operates as a multi-project platform where all agents, sessions, tasks, and data are strictly isolated into distinct "Projects". Each project acts as a completely independent workspace with its own dedicated database, codebase configuration, and agent workflows. This architecture allows users to manage multiple, disparate software projects and agentic workflows concurrently without any cross-contamination of data or state.

## 2. Motivation
As OrbitMesh handles complex developer workflows and diverse codebases, a single-global-state model is insufficient. Users need the ability to isolate different codebases and different agent workflows. For example, an agent working on a Python backend must not have access to the session history or tasks of an agent working on a separate React frontend project unless explicitly designed.

By enforcing project-level isolation, we solve the problem of data entanglement, improve system organization, and ensure that users can work on more than one distinct codebase simultaneously with a clean, unpolluted workspace for each.

## 3. Scope
* **In Scope**:
  * Definition and creation of "Projects" via the OrbitMesh user interface.
  * Complete data isolation: each project is backed by its own entirely separate `goraphdb` graph database instance/file.
  * API architecture: all relevant backend API endpoints are project-scoped (e.g., `/api/v1/projects/{project_id}/sessions`).
  * UI context: a top-level project selector dropdown filters all subsequent views and data contexts to the selected project.
  * Strict entity ownership: sessions, terminals, tasks, and agents belong entirely to their owning project.
* **Out of Scope**:
  * Role-Based Access Control (RBAC) or specific security roles (e.g., Project Admin, Viewer).
  * Data migration scripts for legacy, un-scoped global data.
  * Project creation via local filesystem configuration files (e.g., detecting an `orbitmesh.yaml` in a directory).

## 4. Requirements & User Experience (UX)
* **Project Creation Flow**: A user navigates to a "Projects" management page or uses a "New Project" button in the global navigation to provide a project name and optionally a path to the local codebase directory.
* **Global Project Context**: The main UI features a persistent, top-level project selector dropdown. Once a project is selected, the entire UI (Dashboards, Agent Session Viewers, Task Trees, CodeFlow) exclusively displays data, sessions, and tasks belonging to that specific project.
* **Total Isolation**: Users operate in a completely separate instance of OrbitMesh context when they switch projects. There is no leakage of tasks, active terminal sessions, or agent history between projects.

## 5. System Design & Architecture
* **Database Partitioning**: The backend implements a Project Database Manager rather than holding a single global `goraphdb` connection. When a project is created, a new, separate `goraphdb` database file/instance is provisioned on disk. The backend routes database queries to the correct connection pool based on the active `project_id`.
* **API Routing**:
  * The backend routing layer structure is nested under projects: `GET /api/v1/projects/{project_id}/sessions`.
  * Dedicated endpoints manage the projects themselves (e.g., `POST /api/v1/projects`, `GET /api/v1/projects`).
* **Entity Ownership**: Domain entities implicitly belong to the project database they reside in. Cross-database queries are structurally prevented by the Database Manager.
* **In-Memory State**: Active execution engines, websocket connections, and PTY terminal sessions are tracked internally by the backend alongside their `project_id` to ensure events are broadcasted only to users viewing that specific project context.

## 6. Security & Privacy
* **Isolation by Default**: By strictly separating the underlying `goraphdb` instances, the risk of accidental data leakage between projects via complex graph queries is drastically reduced.
* **Authentication/Authorization**: No specific authz requirements (like RBAC) are implemented in this phase. The focus is on logical and physical data separation.

## 7. Testing Plan
* **Backend Unit/Integration Tests**:
  * Verify the Project Database Manager correctly initializes and routes queries to separate `goraphdb` instances.
  * Ensure that entities created in Project A cannot be retrieved via queries executed against Project B's database connection.
  * Test project-scoped API endpoints to ensure they return errors when attempting to access entities without a valid project context.
* **Frontend E2E Tests**:
  * Simulate creating two projects, generating a session in Project A, and verifying that the session does not appear in Project B's dashboard.
  * Test the top-level project selector to ensure it correctly updates the active context and fetches the correct project-scoped data.

## 8. Rollout & Deployment
* **Data Migration**: No automated data migration is provided. Legacy global database files are ignored or can be manually deleted. The platform boots clean and requires the creation of an initial project.
* **Rollout Strategy**: Immediate rollout. Backwards compatibility of the API and database schema is not required during the prototyping phase.

## 9. Alternatives Considered
* **Single Database with `project_id` Properties**: We considered keeping a single `goraphdb` instance and adding a `project_id` property to every single node and relationship in the graph.
  * *Why Rejected*: This increases query complexity, requires massive rewrites of all existing Cypher queries to ensure the `project_id` filter is never forgotten, and increases the risk of accidental data leakage. Completely separate databases provide a stronger, simpler isolation boundary.
* **Project Configuration Files (`orbitmesh.yaml`)**: We considered auto-discovering projects based on configuration files located in the user's filesystem.
  * *Why Rejected*: While useful for developer experience, it introduces complexity in filesystem monitoring and state synchronization between the disk and the database. UI-driven creation provides a clearer, more explicit source of truth.

## 10. Implementation Plan
* [x] **Frontend**: Implement the basic top-level project selector dropdown UI.
* [ ] **Backend**: Create the Project domain entity and database schema for tracking available projects (a lightweight "meta" database to hold the list of projects and paths to their respective `goraphdb` instances).
* [ ] **Backend**: Implement the Project Database Manager to handle dynamic connections to multiple `goraphdb` instances based on `project_id`.
* [ ] **Backend**: Refactor all existing REST API routes in `backend/internal/api` to be nested under `/api/v1/projects/{project_id}/`.
* [ ] **Backend**: Refactor backend execution engine and session managers to associate active operations with a `project_id`.
* [ ] **Frontend**: Implement the "Create Project" UI flow.
* [ ] **Frontend**: Refactor all API client calls and tanstack-router routes to include and require the `project_id` parameter.
* [ ] **Testing**: Update all Go and Vitest/Playwright tests to accommodate the new project-scoped architecture.

## 11. Open Questions
* Should the backend "meta" database for tracking projects be an SQLite database, or a simple JSON file managed by the backend engine?
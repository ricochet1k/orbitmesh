# Design: Multiple Projects

**Status:** Draft
**Date:** 2026-02-18
**Author:** Designer role (Tb5hgbb)

---

## Problem Statement

OrbitMesh currently has no concept of a "project". Sessions carry an individual `WorkingDir` string field but are otherwise ungrouped. As users run agents against multiple codebases simultaneously, there is no way to:

- View sessions relevant only to one codebase
- Create a new session pre-scoped to a working directory
- Switch context between different projects in the UI

A first-class `Project` entity—backed by a filesystem directory—addresses this.

---

## Goals

1. Define a `Project` as a named entity with a working directory path.
2. Associate sessions (and thus transitively, terminals) with a project.
3. Provide a project-switching UI in the frontend sidebar.
4. Maintain backwards compatibility with existing sessions that have no project.

## Non-Goals

- Multi-user / permission-scoped projects (out of scope for now).
- Remote directory paths (projects are local filesystem paths).
- Nesting projects hierarchically.

---

## Data Model

### Backend: `domain.Project`

New file `backend/internal/domain/project.go`:

```go
type Project struct {
    ID        string
    Name      string
    Path      string    // absolute filesystem path (the working directory)
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Backend: `domain.Session` change

Add one field:

```go
type Session struct {
    // ... existing fields ...
    ProjectID string  // empty string = no project ("legacy" / global)
}
```

`Terminal` already links to a `SessionID`; project context is derived transitively through the session. No changes needed to `domain.Terminal`.

---

## Storage

### `ProjectStorage` (new)

Stored in `~/.orbitmesh/projects.json` (same pattern as `providers.json`):

```json
[
  {
    "id": "abc123",
    "name": "orbitmesh",
    "path": "/Users/matt/mycode/orbitmesh",
    "created_at": "2026-02-18T00:00:00Z",
    "updated_at": "2026-02-18T00:00:00Z"
  }
]
```

Interface:

```go
type ProjectStorage interface {
    List() ([]Project, error)
    Get(id string) (*Project, error)
    Save(p Project) error
    Delete(id string) error
}
```

### Session Storage change

`sessionData` in `storage.go` gains:

```go
ProjectID string `json:"project_id,omitempty"`
```

Existing session files without `project_id` unmarshal to `""` (empty = no project). No migration needed.

---

## API

### New project endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/projects` | List all projects |
| `POST` | `/api/v1/projects` | Create a project |
| `GET` | `/api/v1/projects/{id}` | Get a project |
| `PUT` | `/api/v1/projects/{id}` | Update name/path |
| `DELETE` | `/api/v1/projects/{id}` | Delete a project |

**Create request body:**
```json
{ "name": "orbitmesh", "path": "/Users/matt/mycode/orbitmesh" }
```

**Response shape:**
```json
{
  "id": "abc123",
  "name": "orbitmesh",
  "path": "/Users/matt/mycode/orbitmesh",
  "created_at": "...",
  "updated_at": "..."
}
```

### Changed session endpoints

**`GET /api/sessions`** — adds optional query param:
```
?project_id=abc123        → sessions for that project
?project_id=              → sessions with no project (legacy)
(no param)                → all sessions (existing behaviour)
```

**`POST /api/sessions`** — request body gains:
```json
{ "project_id": "abc123", ... }
```

If `project_id` is provided and resolves to a project, `working_dir` defaults to the project's `path` (can still be overridden explicitly).

---

## Frontend

### State: Active Project

A new SolidJS store `src/state/project.ts` holds:

```ts
interface ProjectStore {
  activeProjectId: string | null   // null = show all (global view)
  projects: Project[]
}
```

Active project is persisted to `localStorage` so it survives page refreshes.

### Sidebar: Project Picker

The sidebar gains a project picker above the navigation links:

```
┌─────────────────────────┐
│  OrbitMesh              │ ← brand
├─────────────────────────┤
│  ▼  orbitmesh      [+]  │ ← project picker (dropdown + new button)
├─────────────────────────┤
│  Dashboard              │
│  Tasks                  │
│  Sessions               │
│  ...                    │
└─────────────────────────┘
```

- Clicking the picker opens a dropdown listing all projects + "All projects" (global view).
- `[+]` opens a small inline form (name + path) to create a new project.
- Active project name is truncated to ~20 chars with ellipsis.

### Filtering in existing views

When an active project is set:

- **Sessions list** (`/sessions`): passes `?project_id=` to `GET /api/sessions`.
- **Dashboard** session counts: filtered to active project.
- **New session form**: `project_id` pre-filled; `working_dir` defaults to project path.

When "All projects" is active, no filtering is applied (existing behaviour).

### New route: `/settings/projects` (Settings sub-page)

A simple CRUD table for managing projects (create, rename, delete, change path). Can be embedded in `/settings` as a section rather than a top-level route to keep the sidebar clean.

---

## Migration / Backwards Compatibility

- Existing sessions with no `project_id` continue to work. They appear in the "All projects" global view.
- No data migration script is required.
- The API addition of `?project_id=` is purely additive.

---

## Open Questions / Decisions for Owner

1. **Session creation without a project** — should sessions be allowed to exist without a project? Or should the UI require one? (Recommendation: allow, for backwards compat and ad-hoc use.)
Owner decision: We can try to allow it for now, but I don't want to complicate the UI by making them pick a directory instead, it should not have file access.

2. **Default project from git** — when `gitDir` is resolved server-side, should the server auto-create/match a project for that path? Or is project management always explicit by the user?
Owner decision: Project management should be explicit, but if it's obvious enough at any point that they need a project then the user could be asked to create it with a dialog.

3. **Project deletion** — when a project is deleted, what happens to its sessions? Options:
   - Sessions are orphaned (project_id becomes dangling).
   - Sessions are also deleted.
   - Sessions are moved to "no project".
   (Recommendation: orphan / move to "no project"; deletion of sessions is a separate action.)
Owner decision: sessions should be deleted, but deleting a project should be hard. Like make them type the name before it will delete.

4. **Project path validation** — should the backend verify the path exists on disk at creation time, or accept any string and let the agent fail at runtime?
Owner decision: Verify that it exists

5. **Sidebar or top bar** — the project picker could alternatively live in a top navigation bar rather than the sidebar, which might be more discoverable on narrow screens.
Owner decision: sidebar.

---

## Implementation Sketch

Rough layer ordering for implementation:

1. `domain.Project` + `ProjectStorage` (JSON file, same pattern as providers)
2. Add `ProjectID` to `domain.Session` + storage round-trip
3. API handlers: project CRUD + session filtering
4. Frontend: `src/api/projects.ts` + `src/state/project.ts`
5. Sidebar project picker component
6. Wire session list / dashboard to active project filter
7. New session form: project_id pre-fill

Estimated touch points:
- Backend: ~5 new/modified files
- Frontend: ~6 new/modified files
- No database migrations (file-based storage)

---

## Appendix: File Change Summary

| File | Change |
|------|--------|
| `backend/internal/domain/project.go` | New — Project type |
| `backend/internal/storage/project_storage.go` | New — JSON file storage |
| `backend/internal/domain/session.go` | Add `ProjectID string` |
| `backend/internal/storage/storage.go` | Persist/load `project_id` |
| `backend/internal/api/handler.go` | New project routes, session filter |
| `backend/pkg/api/types.go` | New request/response types |
| `frontend/src/api/projects.ts` | New — project CRUD API client |
| `frontend/src/state/project.ts` | New — active project store |
| `frontend/src/components/Sidebar.tsx` | Add project picker |
| `frontend/src/api/sessions.ts` | Pass `project_id` on create/list |
| `frontend/src/routes/index.tsx` | Filter by active project |
| `frontend/src/routes/sessions/index.tsx` | Filter by active project |

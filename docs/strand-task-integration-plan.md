# Strand-Backed Tasks Integration Plan

## Goal

Populate OrbitMesh Tasks UI from real Strand task data via Strand HTTP API while preserving OrbitMesh visual design and UX. Deeply integrate tasks and sessions so that:

1. Sessions assigned to a task cannot claim any other task.
2. OrbitMesh can show all sessions that were ever attached to a task.
3. Task/session state is visible and trustworthy across API, MCP tooling, and UI.

## Current Baseline

- `GET /api/v1/tasks/tree` is wired but currently serves sample data.
- Frontend Task Tree consumes OrbitMesh task-tree API and renders hierarchy/filters/launch controls.
- Session creation already accepts `task_id` and `task_title` metadata, but data is flattened into `current_task` text.
- No persistent relational ledger exists for task-to-session attachments.
- Strand MCP tools can claim tasks but do not enforce single-active-task constraints based on OrbitMesh session binding.

## Principles

- Keep Strand as source of truth for task definitions and status.
- Keep OrbitMesh as source of truth for session lifecycle and attachment history.
- Enforce claim restrictions at multiple layers (MCP and backend API).
- Prefer additive schema/API changes with backward compatibility for existing UI and consumers.

## Architecture

### A) Strand API Adapter (backend)

- Add a dedicated client package to query Strand HTTP endpoints.
- Include timeout, retry/backoff, and explicit error mapping.
- Configure with environment variables (base URL, auth token if needed, timeout).
- Start with read operations (`tree/list/show`) then add claim/complete actions as needed.

### B) Task-Session Attachment Ledger (backend persistence)

- Add persistent storage for attachment records:
  - `task_id`
  - `session_id`
  - `attached_at`
  - `detached_at` (nullable)
  - `detach_reason` (nullable)
  - `attachment_source` (`tasks_ui`, `api`, `mcp`, etc.)
- Keep one active attachment per session at a time.
- Preserve historical records for "ever attached" queries.

### C) Policy Engine: Single Active Task per Session

- If a session has an active task attachment, block attempts to claim a different task.
- Enforce in:
  - MCP Strand tool handlers (`next_task --claim`, `claim_task`)
  - OrbitMesh API paths that may claim/attach tasks now or in future
- Return clear structured errors containing task and session context.

### D) OrbitMesh API Surface

- Replace sample task tree with Strand-backed data in `GET /api/v1/tasks/tree`.
- Extend task payloads with OrbitMesh integration metadata:
  - `active_sessions`
  - `session_history_count`
  - optional `latest_session`
- Add task/session history endpoints:
  - `GET /api/v1/tasks/{id}/sessions` (all ever attached sessions)
  - `GET /api/sessions/{id}/task-binding` (active + historical binding details)

### E) Frontend Integration

- Keep current OrbitMesh look-and-feel and layout patterns.
- Tasks page should display:
  - Active attached sessions for selected task
  - Historical session list/count for selected task
  - Lock messaging when a session is already task-bound
- Replace transient local launch-state assumptions with API-backed attachment data.

## Phased Delivery

### Phase 1: Strand Data Plumbing

1. Implement Strand client package and config.
2. Rewire `/api/v1/tasks/tree` to Strand data.
3. Add tests for mapping and failure behavior.

Exit criteria:
- Task tree in OrbitMesh reflects live Strand tasks.

### Phase 2: Attachment Ledger + APIs

1. Add storage model and repository operations for attachments.
2. Write on session creation with task metadata.
3. Add read APIs for active and historical attachments.

Exit criteria:
- OrbitMesh can answer "which sessions were ever attached to task X?"

### Phase 3: Claim Restriction Enforcement

1. Add policy checks in MCP claim paths.
2. Add equivalent backend guards.
3. Add tests for blocked double-claim scenarios.

Exit criteria:
- A task-bound session cannot claim another task through supported surfaces.

### Phase 4: UI Binding + Visibility

1. Update tasks page data flow to consume new task/session metadata.
2. Show active + historical attachments in task detail panel.
3. Add clear lock banners and action guidance.

Exit criteria:
- Operators can see active assignment and full historical task-session linkage.

### Phase 5: Realtime + Hardening

1. Emit task-binding events over existing realtime infrastructure.
2. Make Task UI update without manual refresh.
3. Add resilience behavior for temporary Strand API outages.

Exit criteria:
- Task/session integration is live, observable, and robust.

## Data Model Sketch

Proposed attachment record:

```json
{
  "task_id": "Tabc123",
  "session_id": "s_123",
  "attached_at": "2026-02-22T10:00:00Z",
  "detached_at": null,
  "detach_reason": null,
  "attachment_source": "tasks_ui"
}
```

Constraints:

- For each `session_id`, max one record with `detached_at = null`.
- Multiple records per `task_id` over time allowed.

## API Additions (Draft)

- `GET /api/v1/tasks/tree`
  - Existing shape retained, with optional augmentation fields.

- `GET /api/v1/tasks/{id}/sessions`
  - Returns active + historical sessions, most recent first.

- `GET /api/sessions/{id}/task-binding`
  - Returns active binding (if any) and historical binding list/count.

## Test Strategy

- Unit:
  - Strand client mapping and error handling
  - Attachment store invariants
  - Policy check helpers
- API:
  - Task tree passthrough and augmentation
  - History endpoints correctness
  - Claim-block behavior
- Integration:
  - Session attach then blocked re-claim
  - Detach/rebind lifecycle
- Frontend:
  - Tasks page renders active/history states
  - Clear lock UX when double-claim blocked

## Risks and Mitigations

- Strand API mismatch/version drift:
  - Add adapter normalization + contract tests.
- Partial outage of Strand server:
  - Graceful errors, stale-cache fallback where safe.
- Policy bypass through unguarded paths:
  - Centralize policy checks and cover with integration tests.

## Work Breakdown for Delegation

1. **Adapter Workstream**: Strand HTTP client, config, and task tree integration.
2. **Ledger Workstream**: Persistent task-session attachment model and queries.
3. **Policy Workstream**: Claim restrictions in MCP + backend APIs.
4. **UI Workstream**: Tasks panel integration for active/history/locks.
5. **Realtime/QA Workstream**: Event propagation, regression tests, and reliability hardening.

## Definition of Done

- Tasks UI is populated from live Strand data via OrbitMesh backend.
- Task/session attachments are persisted and queryable historically.
- Task-bound sessions are prevented from claiming additional tasks.
- UI visibly reflects active and historical task-session associations.
- Relevant backend/frontend tests pass.

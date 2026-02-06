# Management Interface Threat Model

## Scope and Context
- **Surface**: The OrbitMesh management interface (`/admin/*`, `/tasks`, `/templates`) exposes CRUD operations for agent roles, task templates, and bulk task actions.
- **Users**: Architect, developer, owner, and tester roles plus any service-to-service calls from the backend (e.g., automation jobs triggered via CLI).
- **Existing docs**: This annex builds on `design-docs/06-management-interfaces.md` by mapping sensitive data, trust boundaries, and mitigations that must inform UI and API work.

## Key Assets and Data Classification
| Asset | Description | Sensitivity | Owner |
| --- | --- | --- | --- |
| Role definitions | Role name, description, workflow steps, and permissions mapping | **High** – controls what actions agents can take and which data they can see | Security team / owner |
| Task templates | TODO steps, default roles, metadata, and references to other tasks or secrets | **Medium-High** – templates can embed indirect references to secrets or escalate capabilities | Workflow automation team |
| Bulk task actions | Mass assignments, priority changes, completions | **Medium** – mistakes impact many tasks at once | Operations team |
| Audit data | Who changed what and when inside the UI/backend | **High** – required for forensics/accountability | Security & compliance |

## Trust Boundaries
- **External client ↔ frontend**: Browsers run UI served from OrbitMesh; this boundary is protected by session cookies, CSRF tokens, and CSP.
- **Frontend ↔ backend API**: All management views call `/api/v1/roles`, `/api/v1/templates`, `/api/v1/tasks`. Backend enforces authentication, authorization, and input validation; clients must not trust data returned from server beyond what the UI displays.
- **Backend ↔ datastore/audit**: Controllers transform requests into persistence changes and audit entries. This boundary must enforce RBAC and immutability for audit events.
- **Automation/service accounts ↔ backend**: CLI or automation tools that programmatically touch the management API must obey token-based auth and be rate-limited.

## Threats and Mitigations
1. **Unauthorized role escalation** – Attackers abuse the management UI/API to add themselves to higher privileged roles.
   - Mitigate: backend enforces RBAC per endpoint (only owners can edit role permissions), UI hides controls when backend indicates insufficient permissions, and `roles` endpoints perform server-side policy checks even if requests are forged.
   - Mitigate: require additional approval (owner or reviewer) for role changes that increase privileges (e.g., `role.is_system_role`).
2. **CSRF on sensitive POST/PUT/DELETE actions** – Attackers trick authenticated users into executing management commands.
   - Mitigate: ensure every state-changing form (role, template, bulk action) submits with double-submit cookie or `SameSite=strict` session plus standard CSRF token verified server-side.
   - Mitigate: mandate `POST` body validation of the token independent of authentication tokens.
3. **Improper authorization for bulk actions** – Bulk assignment endpoints may allow acting on tasks/roles that the user cannot normally edit.
   - Mitigate: backend re-checks per-task permissions before applying bulk changes and rejects actions outside a user’s scope.
4. **Sensitive data exposure** – Task templates or audit logs leak secrets (API keys, credentials) referenced within TODOs or role descriptions.
   - Mitigate: forbid display of secrets in templates, enforce sanitization, and implement server-side redaction rules; treat `template.steps` as untrusted and limit preview data.
5. **Audit log tampering** – Attackers who can edit management settings might also tamper with logs to cover tracks.
   - Mitigate: append-only audit storage (database table with immutable timestamps), restrict audit endpoints to `owner` role, and replicate logs to an independent system (e.g., read-only analytics cluster) if feasible.
6. **Workflow replay/race conditions** – Replay or parallel requests (bulk updates) cause inconsistent state.
   - Mitigate: use optimistic locking or version attributes on templates/roles and reject stale updates with clear UI messages.

## Data Protection Expectations
- **Input validation**: All inputs must be validated server-side: template names, TODO entries, JSON mesh, UUID lists for bulk actions.
- **Rate limiting**: Sensitive endpoints (`/api/v1/roles`, `/api/v1/templates`) should have stricter rate limits than general task browsing to prevent brute-force/automation abuse.
- **Error handling**: Do not leak internal errors (stack traces or permission details) through the API; respond with sanitized codes and log details server-side.

## Authorization & CSRF Requirements
- **Authorization matrix**: Document which roles can view/edit/create each entity and enforce cluster-wide policies. For example, only `owner` may alter `architect` or `security` roles, while `developer` may edit their assigned templates.
- **CSRF policy**: All state-changing endpoints must validate a CSRF token stored in a secure cookie (`SameSite=strict`) and require `X-CSRF-Token` header on AJAX/ fetch requests.
- **Consistency**: The UI should call an endpoint (e.g., `/api/v1/me/permissions`) on load and gate navigation. Bulk actions should pre-flight-check permissions server-side before showing commit buttons.

## Audit Logging & Monitoring Requirements
- **Events**: Log creations, updates, deletions of roles/templates, bulk task actions, and privilege changes. Include actor ID, target ID, timestamp, request metadata (IP, user agent), and before/after snapshots when available.
- **Retention**: Store audit logs for at least 90 days; expose read-only endpoints to owners and compliance reviewers.
- **Alerting**: Trigger alerts when a high-privilege role is modified or when bulk actions touch more than a configurable task threshold.
- **Monitoring**: Track `api.v1.roles` failure rates, 4xx spikes, CSRF token failures, and unauthorized attempts.

## Testing Strategy
- **Static analysis**: Add unit tests covering policy evaluation for each management endpoint.
- **Integration**: Automate end-to-end flows where each role exercises the UI, verifying guarded navigation, CSRF tokens, and audit logging.
- **Chaos scenarios**: Simulate stale bulk requests to verify conflict handling.
- **Audit verification**: Build smoke tests ensuring audit entries exist for sample role/template changes and that they respect immutability.

## Tracks & Dependencies
1. **Policy Enforcement Track** (owner: developer security team)
   - Implement backend RBAC re-checks, authorization matrix, and API-level dependencies described above.
2. **Audit & Logging Track** (owner: observability team)
   - Implement structured audit records, retention rules, alerts, and immutable storage; document exposures.
3. **UI & UX Guardrails Track** (owner: frontend team)
   - Ensure CSRF tokens on forms, hide unauthorized controls, and show permission error messaging.

Each track should reference this threat model doc when raising follow-up tasks so mitigation choices stay contextualized.

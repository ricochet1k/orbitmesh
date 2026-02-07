# Guardrail Guidance Server-Side Sanitization

## Context
Guardrail guidance strings are currently sanitized only in the frontend (`frontend/src/utils/guardrailGuidance.ts`). The backend emits guardrail guidance in `/api/v1/me/permissions` (`backend/internal/api/permissions.go`) without sanitization. This leaves other consumers (API clients, logs, analytics, future services) exposed to unsanitized content.

## Goals
- Sanitize/redact guardrail guidance server-side before any response is emitted.
- Preserve frontend sanitization as defense in depth.
- Keep redaction rules aligned with the frontend to avoid mismatched displays.

## Non-Goals
- Replace or remove frontend sanitization.
- Introduce new UI copy or guardrail policy changes.
- Build a global sanitization framework for all API strings (scope is guardrail guidance only).

## Decision
Implement a server-side sanitizer in the backend API layer and apply it to guardrail guidance before encoding the permissions response. Decode HTML entities to a fixed point (with a small iteration cap) before stripping tags to prevent entity-encoded markup from being reintroduced by consumers. Keep the frontend sanitizer unchanged.

## Sanitization Rules
Mirror the frontend logic, with a bounded entity decode step:
- Decode HTML entities to a fixed point before stripping tags (cap iterations, e.g., 2-3 passes).
- Strip HTML tags.
- Replace control characters with spaces and collapse whitespace.
- Redact bearer tokens (`Bearer <token>` → `Bearer [redacted]`).
- Redact key/value secrets for labels like `api_key`, `token`, `secret`, `password`, `passphrase`, `access_key`.
- Redact AWS access keys (`AKIA...`) and GitHub tokens (`ghp_...`, `gho_...`, `ghs_...`, `ghr_...`).

## Authoring Guidance
Guardrail guidance strings are sanitized after multi-pass entity decoding. This means any text that looks like HTML tags will be stripped even if it was entity-encoded (for example, `&lt;role&gt;` or `&amp;lt;role&amp;gt;`).
- Avoid angle brackets or tag-like placeholders in guidance text.
- Prefer safe placeholder formats such as `[role]`, `{role}`, or `ROLE_NAME`.
- Use inline code formatting for placeholders or keywords when helpful.
- Do not rely on HTML entity encoding as a workaround; multi-pass decoding will still strip tag-like content.

## Implementation Plan
1. **Add sanitizer helpers** in a backend API helper file (e.g., `backend/internal/api/guardrail_sanitizer.go`) with:
   - `sanitizeGuardrailGuidance(input string) string`
   - `sanitizeGuardrailStatus(GuardrailStatus) GuardrailStatus`
   - `sanitizePermissionsResponse(PermissionsResponse) PermissionsResponse`
2. **Apply sanitization** in `backend/internal/api/permissions.go`:
   - Option A: sanitize in `guardrailStatuses` when setting `Detail`.
   - Option B: sanitize `defaultPermissions` in `mePermissions` just before encoding.
   - Prefer Option A to ensure any guardrail guidance constructed in the API layer is sanitized at source.
3. **Tests**:
    - Add Go unit tests covering HTML removal and redactions (bearer tokens, key/value secrets, AWS/GitHub tokens).
    - Include a double-encoded entity test (e.g., `&amp;lt;script&amp;gt;` becomes stripped).
   - Mirror test cases from `frontend/src/utils/guardrailGuidance.test.ts` and `frontend/src/api/client.test.ts` to keep parity.
4. **Documentation**:
   - Note server-side sanitization in this doc and reference it from any future guardrail guidance documentation.

## Files to Change (Developer)
- `backend/internal/api/permissions.go`
- `backend/internal/api/guardrail_sanitizer.go` (new)
- `backend/internal/api/permissions_test.go` (new or extend)

## Testing Strategy
- `go test ./backend/internal/api -run Guardrail` (or equivalent new tests).
- Ensure existing frontend tests continue to pass; no changes required in the frontend for this work.

## Acceptance Criteria
- `/api/v1/me/permissions` guardrail guidance is sanitized server-side before response emission.
- Backend tests cover sanitization and redaction rules.
- Frontend sanitization remains in place as defense in depth.

## Risks and Trade-offs
- **Rule drift** between frontend and backend: mitigate by mirroring tests and documenting the shared rules.
- **False positives** from regex redaction: acceptable; safer to redact overly broadly than leak secrets.
- **Multi-pass entity decode** could slightly alter benign text (e.g., literal `&amp;lt;` inputs), but this is preferable to allowing entity-encoded tags to survive; iteration cap limits worst-case cost.

## References
- `frontend/src/utils/guardrailGuidance.ts`
- `backend/internal/api/permissions.go`
- `design-docs/10-management-interface-threat-model.md`

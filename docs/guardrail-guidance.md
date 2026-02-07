# Guardrail Guidance Authoring

Guardrail guidance text is sanitized server-side (and again in the frontend) before it is displayed. Sanitization includes multi-pass HTML entity decoding and HTML tag stripping, so any tag-like text is removed even if it was entity-encoded.

## Authoring Rules
- Avoid angle brackets or tag-like placeholders such as `<role>`, `&lt;role&gt;`, or `&amp;lt;role&amp;gt;`.
- Prefer safe placeholders such as `[role]`, `{role}`, `ROLE_NAME`, or `role_name`.
- Use inline code formatting for placeholders or keywords when it improves clarity.
- Do not rely on HTML entity encoding as a workaround; multi-pass decoding will still strip tag-like content.

## Examples

Bad:
- `Request access as <role> to proceed.`
- `Use &lt;role&gt; for elevated access.`
- `Provide &amp;lt;role&amp;gt; to continue.`

Good:
- `Request access as [role] to proceed.`
- `Use {role} for elevated access.`
- `Provide ROLE_NAME to continue.`

## Local Verification

Run the guardrail sanitizer tests:

```bash
go test ./backend/internal/api -run Guardrail
```

# Claude Provider Fidelity Integration Checklist

Goal: make OrbitMesh's `claude` and `claude-ws` providers match real Claude CLI behavior with minimal information loss.

Constraints from product direction:

- Prefer strongly typed/user-meaningful events over `metadata` whenever data is likely user-visible.
- Use `metadata` only for internal details that are unlikely to be shown directly.
- Resume/continue behavior should be automatic and backed by session-level custom data (`map[string]any`).

## Gaps and Fix Plan

- [x] Stop treating `input_json_delta` (tool argument streaming) as assistant output in the `claude` provider.
- [x] Preserve tool input/output fidelity across both Claude providers (tool start/progress/completion with structured fields).
- [x] Remove semantically incorrect `plan` mapping for `claude-ws` result messages; use better typed events.
- [x] Add session-level custom data map (`map[string]any`) for runtime provider state (separate from provider config custom).
- [x] Persist/update Claude runtime session identity in session custom data when providers report it.
- [x] Auto-apply `--resume` / `--continue` behavior from session custom data when starting new runs.
- [x] Keep `metadata` only for internal/internal-debug-only payloads.
- [x] Add tests for the above behavior in `claude` + `claude-ws` + service projection.

## Notes

- Existing `ProviderCustom` in session stores provider configuration preferences; runtime session continuity data should live in a dedicated session custom-data field.
- The intended user-visible representation for Claude-specific runtime updates should be `output`, `tool_call`, `status_change`, `system_message`, `error`, and `metric` events, reserving `metadata` for hidden/internal records.

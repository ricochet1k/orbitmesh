# Session Event Types

This document defines the provider-agnostic session event model used by OrbitMesh.

Source-of-truth code links:
- Domain event enum and payloads: `backend/internal/domain/event.go`
- API event types: `backend/pkg/api/types.go`
- Frontend session stream union: `frontend/src/types/api.ts`

If you add/change event types, update all three files and this document in the same PR.

## Core Principles

- Provider adapters normalize provider-specific notifications into generic event types.
- Session viewer and transcript rendering are provider-agnostic.
- High-frequency streams use delta semantics and should persist combined state, not one standalone message per chunk.

## Event Types

- `status_change`
  - Session state transitions (`old_state`, `new_state`, optional `reason`).

- `output`
  - Primary assistant output stream.
  - Supports delta appends via `is_delta` and optional `message_id`.

- `metric`
  - Legacy compact metrics (`tokens_in`, `tokens_out`, `request_count`).

- `error`
  - Error message and optional code.

- `metadata`
  - Escape hatch/fallback for unknown provider payloads.
  - Prefer explicit typed events over metadata whenever possible.

- `tool_call`
  - Tool lifecycle and result payload (`id`, `name`, `status`, `input`, `output`).

- `thought`
  - Reasoning/thinking stream.
  - Supports delta appends via `is_delta` and optional `message_id`.

- `plan`
  - Structured plan updates (`description`, `steps`).

- `user_message`
  - User message that initiated the run.

- `system_message`
  - System-originated informational message.

- `progress`
  - Provider-agnostic streaming progress.
  - Fields: `channel`, `stream_id`, `content`, `is_delta`, `done`, `status`.
  - Typical channels: `reasoning`, `tool_output`.

- `resource_usage`
  - Provider-agnostic usage/rate/cost updates.
  - Fields: `scope`, `data`.
  - Canonical scopes: `turn`, `thread`, `session`, `models`, `capabilities`, `account`, `provider`, `global`.
  - Routed into session/provider usage stats (not appended as transcript messages).

- `action_request`
  - Provider-agnostic request for human intervention/approval.
  - Fields: `id`, `kind`, `title`, `status`, `payload`.

- `artifact_update`
  - Provider-agnostic artifact/file-change update payload.
  - Fields: `id`, `kind`, `title`, `is_delta`, `payload`.

## Delta Semantics

For delta-capable event payloads (`output`, `thought`, `progress`):

- Delta events append to the current message for the same logical stream.
- Stream identity uses explicit IDs when available:
  - `output.message_id`
  - `thought.message_id`
  - `progress.stream_id`
- If an ID is missing, append falls back to the latest open message of the same kind.
- If no target message exists, create a new message.

## Persistence Model

Message persistence uses projection records in JSONL:

- `append` / `append_raw`: create a new logical message.
- `append_output_delta`: output-specific append projection (legacy and still supported).
- `append_delta`: generic append projection for other appendable message kinds.

The log store rewrites the target record to keep combined state compact.
Replay logic must preserve backward compatibility for legacy projections.

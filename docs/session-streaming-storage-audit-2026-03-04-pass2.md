# Session Streaming/Reload Audit Pass 2 (2026-03-04)

## Trigger

User-reported failure after prior fixes:

- Session: `fafda3e6603231e047c0fce9ddce939d`
- Live stream looked good.
- Reload still looked broken.

This pass focused on concrete data from that session log and cross-checking the current provider -> projector -> storage -> API -> frontend render path.

---

## What is still broken (with evidence)

## 1) Persisted message kinds still do not match render kinds

`run_state_projector` persists `ProgressData`, `ActionRequestData`, and `ArtifactUpdateData` as `MessageKindSystem` instead of distinct kinds:

- `backend/internal/service/run_state_projector.go:152`
- `backend/internal/service/run_state_projector.go:171`
- `backend/internal/service/run_state_projector.go:174`

Frontend rich rendering is kind-driven (`progress`, `action_request`, `artifact_update`, `tool_call`), so history rows persisted as `system` do not reliably render as rich cards.

Concrete session evidence:

- Seq 635, 639, 643, ... are reasoning/progress payloads but persisted as `"kind":"system"` in `~/.orbitmesh/sessions/fafda3e6603231e047c0fce9ddce939d.jsonl`.

## 2) Persisted payload key casing is incompatible with frontend expectations

Persisted payloads currently marshal Go domain structs directly (no JSON tags), producing upper-case keys (`ID`, `Name`, `Status`, `Channel`, `StreamID`, ...).

- payload marshaling path: `backend/internal/service/message_projection_log.go:76`

Frontend renderers expect lower/snake-case keys (`id`, `name`, `status`, `channel`, `stream_id`, ...):

- `frontend/src/components/SessionTranscript.tsx:299`
- `frontend/src/components/SessionTranscript.tsx:304`

Concrete session evidence:

- Seq 636 payload has `{"ID":"...","Name":"command/exec","Status":"running",...}`.
- Seq 635 payload has `{"Channel":"reasoning","StreamID":"...",...}`.

These payloads are present but miss most rich extraction paths on reload.

## 3) Unknown provider noise is still persisted as user-visible transcript messages

Unhandled notifications are converted into `UnknownData` and then persisted as visible `system` text:

- default fallback: `backend/internal/provider/common/codex/codex.go:592`
- unknown -> persisted system line: `backend/internal/service/run_state_projector.go:137`

Concrete session evidence includes many noisy lines:

- `unknown(codex/event/agent_message_delta)...` (seq 620+)
- `unknown(codex/event/terminal_interaction)...` (seq 653, 667, 674, 684)
- `unknown(item/commandExecution/terminalInteraction)...` (seq 654, 668, 675, 685)

These make reload transcripts look much worse even if live looked reasonable.

## 4) Historical session rows were written before fixes and are not normalized/migrated

The same session contains mixed eras:

- older rows with no payload/open and heavy unknown spam,
- newer rows with payload/open but still casing/kind mismatches.

Because reload reads persisted rows as-is, legacy formatting debt is still visible.

---

## Why current tests still miss this

## A) Tests are too synthetic relative to real Codex logs

Many frontend tests feed already-normalized payloads/kinds, not raw persisted artifacts produced by actual projector storage.

Result: tests pass with idealized inputs while real sessions fail.

## B) Backend tests currently assert the wrong payload shape

Some projector tests explicitly assert upper-case keys (`"ID"`, `"Channel"`), reinforcing the serialization mismatch instead of catching it:

- `backend/internal/service/run_state_projector_test.go:321`
- `backend/internal/service/run_state_projector_test.go:324`

## C) No strict end-to-end replay contract test from real provider events

There is still no hard gate asserting:

- provider notifications -> persisted message rows -> `/messages` API -> frontend render model

matches live render model for the same turn.

## D) No toxicity/noise budget test for unknown events

There is no test that fails when unknown/system-noise messages exceed a threshold for known provider flows.

---

## Permanent fix plan (recommended)

## 1) Introduce a canonical persisted transcript item schema

Do not persist raw domain structs as message payloads. Persist a stable, frontend-facing JSON schema with explicit tags:

- `kind` in render terms: `output`, `thought`, `tool_call`, `progress`, `action_request`, `artifact_update`, `error`, `user`, `system`.
- `payload` keys in canonical lower/snake case.
- `open` explicit boolean.
- `schema_version` for migration safety.

This must be the only shape returned by `/messages` and realtime snapshots.

## 2) Align projector kinds with event semantics

Change projector persistence for:

- `ProgressData` -> `MessageKindProgress`
- `ActionRequestData` -> `MessageKindActionRequest`
- `ArtifactUpdateData` -> `MessageKindArtifactUpdate`

Add these message kinds in domain/API/frontend mapping and remove semantic overloading of `system`.

## 3) Stop persisting provider noise to user transcript

For known non-user-facing events (`terminal_interaction`, lifecycle echoes, duplicate deltas):

- suppress or route to debug/audit channel,
- do not append as transcript messages.

If unknown handling is needed for diagnostics, store in a separate diagnostics log/feed.

## 4) Finish Codex mapping coverage for still-unhandled event families

At minimum treat these as internal-suppressed (or explicitly mapped):

- `codex/event/agent_message_delta`
- `codex/event/terminal_interaction`
- `item/commandExecution/terminalInteraction`

## 5) Add migration/normalization strategy for existing prototype data

Given prototype mode, choose one explicitly:

1. one-time rewriter for existing `.jsonl` logs into canonical schema; or
2. hard session data reset policy with clear UX.

Avoid mixed-era behavior where old rows keep poisoning reload.

---

## Tests to add that would have caught this

## Contract tests (must-have)

1. **Provider replay parity fixture**
   - Input: captured Codex NDJSON fixture containing deltas/tool calls/approvals/noise.
   - Assert: live reducer output equals reload reducer output from persisted `/messages` rows.

2. **Schema conformance test for persisted rows**
   - Assert all payload keys for rich kinds match canonical casing and required fields.
   - Fail on upper-case Go-field leakage.

3. **Kind conformance test**
   - Assert progress/action/artifact events are never persisted as generic `system` kind.

4. **Noise budget test**
   - For known fixture turns, assert unknown/system-noise rows remain below threshold (ideally zero for mapped methods).

## UI tests (high value)

5. **Reload parity snapshot test with real persisted fixture**
   - Render from live events and from `/messages` fixture, compare normalized transcript card models (kind/card type/open/essential payload fields).

6. **Legacy data behavior test**
   - If no migration is chosen, assert legacy sessions show explicit degraded-state banner instead of silently rendering broken mixed content.

---

## Immediate diagnosis for `fafda3e...`

Reload is broken primarily because persisted rows in that session are a mixed, non-canonical format:

- many rows are `system` despite being semantic progress/action/tool lifecycle,
- many payloads use upper-case keys incompatible with current frontend extractors,
- many unknown-noise rows are persisted and rendered.

So current test suite can still pass while this session fails.

---

## Bottom line

This is still a contract problem, not just a rendering bug.

Until persisted transcript rows are canonicalized (kind + payload shape) and parity tests use real provider fixtures through storage/reload, this class of regression will keep reappearing.

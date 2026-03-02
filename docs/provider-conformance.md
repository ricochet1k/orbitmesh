# Provider Conformance Harness

This document defines the provider conformance harness for OrbitMesh.

## Goals

- Verify that supported providers produce stable, user-meaningful behavior for the same core flows.
- Catch protocol regressions early with deterministic replay checks and targeted live probes.
- Keep conformance runs cheap enough for frequent local execution.

## Scope

- In scope: `claude`, `claude-ws`, `codex`, `openai`, `adk`, `acp`.
- Explicitly out of scope: `pty` provider (terminal emulation behavior differs and is tracked separately).
- Focus: provider behavior at OrbitMesh event and tool boundaries, not model quality.

## Provider Matrix

| Provider | Offline replay lane | Live probe lane | In conformance scope |
| --- | --- | --- | --- |
| `claude` | yes | yes | yes |
| `claude-ws` | yes | yes | yes |
| `codex` | yes | yes | yes |
| `openai` | yes | yes | yes |
| `adk` | yes | yes | yes |
| `acp` | yes | yes | yes |
| `pty` | no | no | no (excluded) |

## Execution Lanes

1. Offline replay lane
- Replays captured provider transcripts through adapters.
- Must run without live provider credentials.
- Validates normalization, event ordering, and serialization fidelity.

2. Live probe lane
- Runs short real-provider probes against low-cost models.
- Validates wire behavior not observable from replay alone (streaming quirks, live tool handshakes, MCP edges).
- Produces the same normalized artifact shape as replay runs.

## Required Scenarios

Every in-scope provider must implement and pass these scenarios:

- `startup`: session/run startup and first event emission.
- `message_roundtrip`: user prompt to assistant output roundtrip.
- `reasoning_progress`: reasoning/progress event continuity and ordering.
- `tool_call_flow`: tool call start, argument payload, tool result, assistant continuation.
- `mcp_integration`: MCP tool registration/invocation path through provider adapter.
- `raw_fidelity`: normalized events remain traceable to raw provider payloads without lossy rewriting.

## Cost Controls

- Cheap model mapping: each provider maps conformance probes to its lowest reliable model tier.
- Terse prompts: scenario prompts are minimal and deterministic.
- Budget caps: fail fast when per-run budget limits are exceeded.
- Token caps: set strict input/output token ceilings per scenario.
- Probe minimization: one short probe per scenario unless a retry policy is explicitly enabled.

## Artifact Contract

Each scenario run must emit:

- `manifest.json`: run metadata (provider, lane, scenario, model, timestamps, budget/token usage, pass/fail).
- `normalized.ndjson`: OrbitMesh-normalized event stream used for assertions.
- `raw.ndjson`: raw provider payload stream (or replay source envelope).
- `assertions.json`: assertion results with failure details and thresholds.
- Optional diagnostics (`logs.txt`, traces) only when failures occur.

Artifact invariants:

- `manifest.json` references artifact filenames and checksums.
- `normalized.ndjson` and `raw.ndjson` share stable correlation IDs.
- Failed assertions include actionable reason and first failing event index.

## Failure Taxonomy

- `startup_failure`: provider session/run did not initialize.
- `transport_failure`: request/stream transport error before completion.
- `protocol_mismatch`: expected event shape/order differs from contract.
- `fidelity_loss`: normalized event cannot be mapped to raw payload.
- `tool_flow_failure`: tool call lifecycle incomplete or malformed.
- `mcp_failure`: MCP registration or invocation path failed.
- `budget_exceeded`: configured cost/token cap exceeded.
- `flaky_live_probe`: non-deterministic live failure requiring retry classification.

## CLI Usage Proposal

Proposed command surface:

```bash
orbitmesh providercheck [flags]
```

Proposed flags:

- `--provider <name>`: run one provider (`claude|claude-ws|codex|openai|adk|acp`).
- `--lane <offline|live|all>`: choose replay, live probes, or both.
- `--scenario <name>`: run one scenario or comma list.
- `--artifacts <dir>`: output directory for manifest and event artifacts.
- `--budget-usd <amount>`: stop when estimated spend cap is reached.
- `--token-cap <count>`: hard token ceiling per scenario.
- `--model-map <file>`: optional cheap-model override mapping.
- `--fail-fast`: stop on first failure.

Proposed examples:

```bash
orbitmesh providercheck --lane offline --provider codex --scenario message_roundtrip
orbitmesh providercheck --lane live --provider openai --budget-usd 0.25 --token-cap 4000
orbitmesh providercheck --lane all --artifacts ./tmp/providercheck
```

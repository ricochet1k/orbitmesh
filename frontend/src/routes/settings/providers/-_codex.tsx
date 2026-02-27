/**
 * Codex (app-server) provider fields.
 *
 * Uses `codex app-server` over stdio with JSON-RPC.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { FormGroup, TextField } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

export function CodexFields(props: Props) {
  const apiKey = () => String(props.provider.env?.['OPENAI_API_KEY'] ?? '')
  const model = () => String(props.provider.scratch['model'] ?? '')
  const effort = () => String(props.provider.scratch['effort'] ?? '')
  const summary = () => String(props.provider.scratch['summary'] ?? '')
  const approvalPolicy = () => String(props.provider.scratch['approval_policy'] ?? '')
  const sandboxMode = () => String(props.provider.scratch['sandbox_mode'] ?? '')
  const codexCommand = () => String(props.provider.scratch['codex_command'] ?? '')
  const codexArgs = () => String(props.provider.scratch['codex_args'] ?? '')

  return (
    <>
      <TextField
        id="codex-api-key"
        label="OpenAI API Key"
        type="password"
        value={apiKey()}
        onInput={(v) =>
          props.setProvider('env', (env) => ({ ...env, OPENAI_API_KEY: v }))
        }
        placeholder="sk-..."
        hint="Optional when Codex CLI is already authenticated. Stored as OPENAI_API_KEY."
      />

      <TextField
        id="codex-model"
        label="Model"
        value={model()}
        onInput={(v) => props.setProvider('scratch', 'model', v)}
        placeholder="e.g. gpt-5.3-codex"
        hint="Optional model override for thread and turn start."
      />

      <FormGroup id="codex-effort" label="Reasoning Effort" hint="Optional turn effort override.">
        <select
          id="codex-effort"
          value={effort()}
          onChange={(e) => props.setProvider('scratch', 'effort', e.currentTarget.value)}
        >
          <option value="">Default</option>
          <option value="low">Low</option>
          <option value="medium">Medium</option>
          <option value="high">High</option>
        </select>
      </FormGroup>

      <FormGroup id="codex-summary" label="Summary Style" hint="Optional turn summary verbosity.">
        <select
          id="codex-summary"
          value={summary()}
          onChange={(e) => props.setProvider('scratch', 'summary', e.currentTarget.value)}
        >
          <option value="">Default</option>
          <option value="concise">Concise</option>
          <option value="detailed">Detailed</option>
        </select>
      </FormGroup>

      <FormGroup id="codex-approval-policy" label="Approval Policy" hint="Controls shell/tool approval requirements.">
        <select
          id="codex-approval-policy"
          value={approvalPolicy()}
          onChange={(e) => props.setProvider('scratch', 'approval_policy', e.currentTarget.value)}
        >
          <option value="">Default</option>
          <option value="onRequest">On request</option>
          <option value="unlessTrusted">Unless trusted</option>
          <option value="never">Never</option>
        </select>
      </FormGroup>

      <FormGroup id="codex-sandbox-mode" label="Sandbox Mode" hint="Sandbox policy sent to Codex app-server.">
        <select
          id="codex-sandbox-mode"
          value={sandboxMode()}
          onChange={(e) => props.setProvider('scratch', 'sandbox_mode', e.currentTarget.value)}
        >
          <option value="">Default</option>
          <option value="readOnly">Read only</option>
          <option value="workspaceWrite">Workspace write</option>
          <option value="dangerFullAccess">Danger full access</option>
        </select>
      </FormGroup>

      <TextField
        id="codex-command"
        label="Codex Command"
        value={codexCommand()}
        onInput={(v) => props.setProvider('scratch', 'codex_command', v)}
        placeholder="codex"
        hint="Override executable path. Leave blank to use 'codex'."
      />

      <TextField
        id="codex-args"
        label="Additional Args"
        value={codexArgs()}
        onInput={(v) => props.setProvider('scratch', 'codex_args', v)}
        placeholder="--some-flag value"
        hint="Extra arguments appended after 'app-server'."
      />
    </>
  )
}

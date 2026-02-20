/**
 * Claude (stream-json) provider fields.
 *
 * Authentication: ANTHROPIC_API_KEY stored in `env`.
 *
 * Both `claude` and `claude-ws` share a large set of fields (model,
 * permission_mode, budget, tools, dangerously_skip_permissions).  This file
 * covers the `claude` type.  The `claude-ws` file imports and extends these.
 */
import type { SetStoreFunction } from 'solid-js/store'
import { TextField, CheckboxField, FormGroup } from './-_shared'
import type { ProviderEdit } from './-_shared'

interface Props {
  provider: ProviderEdit
  setProvider: SetStoreFunction<ProviderEdit>
}

/** Shared fields used by both claude and claude-ws. */
export function ClaudeSharedFields(props: Props) {
  const apiKey         = () => String(props.provider.env?.['ANTHROPIC_API_KEY'] ?? '')
  const model          = () => String(props.provider.scratch['model'] ?? '')
  const permissionMode = () => String(props.provider.scratch['permission_mode'] ?? '')
  const maxBudget      = () => String(props.provider.scratch['max_budget_usd'] ?? '')
  const allowedTools   = () => String(props.provider.scratch['allowed_tools'] ?? '')
  const disallowed     = () => String(props.provider.scratch['disallowed_tools'] ?? '')
  const skipPerms      = () => Boolean(props.provider.scratch['dangerously_skip_permissions'])

  return (
    <>
      <TextField
        id="claude-api-key"
        label="Anthropic API Key"
        type="password"
        value={apiKey()}
        onInput={(v) =>
          props.setProvider('env', (env) => ({ ...env, ANTHROPIC_API_KEY: v }))
        }
        placeholder="sk-ant-..."
        hint="Stored as the ANTHROPIC_API_KEY environment variable when sessions start."
      />

      <TextField
        id="claude-model"
        label="Model"
        value={model()}
        onInput={(v) => props.setProvider('scratch', 'model', v)}
        placeholder="e.g. claude-opus-4-5"
        hint="Optional model override. Leave blank to use the provider default."
      />

      <FormGroup id="claude-permission-mode" label="Permission Mode" hint="Controls how Claude handles tool permission prompts.">
        <select
          id="claude-permission-mode"
          value={permissionMode()}
          onChange={(e) => props.setProvider('scratch', 'permission_mode', e.currentTarget.value)}
        >
          <option value="">Default</option>
          <option value="acceptEdits">Accept edits</option>
          <option value="autoEdit">Auto-edit</option>
          <option value="bypassPermissions">Bypass permissions</option>
        </select>
      </FormGroup>

      <TextField
        id="claude-max-budget"
        label="Max Budget (USD)"
        type="number"
        value={maxBudget()}
        onInput={(v) => props.setProvider('scratch', 'max_budget_usd', v)}
        placeholder="e.g. 5.00"
        hint="Cap API spend per session. Leave blank for no limit."
      />

      <TextField
        id="claude-allowed-tools"
        label="Allowed Tools"
        value={allowedTools()}
        onInput={(v) => props.setProvider('scratch', 'allowed_tools', v)}
        placeholder="e.g. Bash, Read, Write"
        hint="Comma-separated tool names. Leave blank to allow all."
      />

      <TextField
        id="claude-disallowed-tools"
        label="Disallowed Tools"
        value={disallowed()}
        onInput={(v) => props.setProvider('scratch', 'disallowed_tools', v)}
        placeholder="e.g. Bash"
        hint="Comma-separated tool names to block. Leave blank to block none."
      />

      <CheckboxField
        id="claude-skip-perms"
        label="Dangerously skip permissions"
        checked={skipPerms()}
        onChange={(v) => props.setProvider('scratch', 'dangerously_skip_permissions', v)}
        hint="Auto-approves all tool calls without prompting. Use only in trusted, sandboxed environments."
        danger
      />
    </>
  )
}

/** claude (stream-json) — just the shared fields. */
export function ClaudeFields(props: Props) {
  return <ClaudeSharedFields {...props} />
}

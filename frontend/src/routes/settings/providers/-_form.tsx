/**
 * ProviderForm — the single entry point for creating/editing a provider.
 *
 * Owns the `createStore` for the full edit state and delegates provider-type-
 * specific fields to the per-type sub-components.  All `createSignal` calls
 * have been removed; every piece of form state lives in the store.
 */
import { createSignal, For, Show, Switch, Match, untrack } from 'solid-js'
import { createStore } from 'solid-js/store'
import type { ProviderConfigRequest, ProviderConfigResponse } from '../../../types/api'
import { PROVIDER_TYPES, initialProviderEdit, FormGroup } from './-_shared'
import type { ProviderType } from './-_shared'
import { ClaudeFields } from './-_claude'
import { ClaudeWsFields } from './-_claudews'
import { AdkFields } from './-_adk'
import { AcpFields } from './-_acp'
import { PtyFields } from './-_pty'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface ProviderFormProps {
  provider?: ProviderConfigResponse
  onSave: (config: ProviderConfigRequest) => Promise<void>
  onCancel: () => void
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function parseTools(raw: string): string[] {
  return raw.split(',').map((s) => s.trim()).filter(Boolean)
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ProviderForm(props: ProviderFormProps) {
  const [provider, setProvider] = createStore(
    untrack(() => initialProviderEdit(props.provider)),
  )

  const [error, setError]   = createSignal<string | null>(null)
  const [saving, setSaving] = createSignal(false)

  // ---------------------------------------------------------------------------
  // Env-variable editor helpers
  // ---------------------------------------------------------------------------

  /** Sorted list of current env keys for stable iteration. */
  const envKeys = () => Object.keys(provider.env ?? {})

  const addEnvEntry = () =>
    setProvider('env', (env) => ({ ...(env ?? {}), '': '' }))

  const updateEnvKey = (oldKey: string, newKey: string) => {
    setProvider('env', (env) => {
      const next = { ...(env ?? {}) }
      const val = next[oldKey]
      delete next[oldKey]
      next[newKey] = val
      return next
    })
  }

  const updateEnvVal = (key: string, val: string) =>
    setProvider('env', key, val)

  const removeEnvEntry = (key: string) =>
    setProvider('env', (env) => {
      const next = { ...(env ?? {}) }
      delete next[key]
      return next
    })

  // ---------------------------------------------------------------------------
  // Submit handler — assembles ProviderConfigRequest from store
  // ---------------------------------------------------------------------------

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)

    try {
      const t = provider.type as ProviderType

      if (!provider.name.trim()) throw new Error('Name is required')

      const config: ProviderConfigRequest = {
        name: provider.name.trim(),
        type: t,
        is_active: provider.is_active ?? false,
        custom: {},
      }

      // --- Env (filter out blank keys, inject API keys from sub-forms) ---
      const env: Record<string, string> = {}
      for (const [k, v] of Object.entries(provider.env ?? {})) {
        if (k.trim()) env[k.trim()] = v
      }
      if (Object.keys(env).length > 0) config.env = env

      // --- PTY: command (required, split on whitespace) ---
      if (t === 'pty') {
        const parts = provider.command.trim().split(/\s+/)
        if (!parts[0]) throw new Error('Command is required for PTY provider')
        config.command = parts
      }

      // --- ACP: command + optional args (stored in scratch) ---
      if (t === 'acp') {
        const cmd = String(provider.scratch['acp_command'] ?? '').trim()
        if (!cmd) throw new Error('ACP command is required')
        config.custom!['acp_command'] = cmd
        const rawArgs = String(provider.scratch['acp_args'] ?? '').trim()
        if (rawArgs) config.custom!['acp_args'] = rawArgs.split(/\s+/)
      }

      // --- Model (claude, claude-ws, adk) ---
      if (t === 'claude' || t === 'claude-ws' || t === 'adk') {
        const model = String(provider.scratch['model'] ?? '').trim()
        if (model) config.custom!['model'] = model
      }

      // --- Claude shared fields ---
      if (t === 'claude' || t === 'claude-ws') {
        const pm = String(provider.scratch['permission_mode'] ?? '').trim()
        if (pm) config.custom!['permission_mode'] = pm

        const budget = parseFloat(String(provider.scratch['max_budget_usd'] ?? ''))
        if (!isNaN(budget) && budget > 0) config.custom!['max_budget_usd'] = budget

        const allowed = parseTools(String(provider.scratch['allowed_tools'] ?? ''))
        if (allowed.length > 0) config.custom!['allowed_tools'] = allowed

        const disallowed = parseTools(String(provider.scratch['disallowed_tools'] ?? ''))
        if (disallowed.length > 0) config.custom!['disallowed_tools'] = disallowed

        if (provider.scratch['dangerously_skip_permissions']) {
          config.custom!['dangerously_skip_permissions'] = true
        }
      }

      // --- claude-ws extras ---
      if (t === 'claude-ws') {
        const turns = parseInt(String(provider.scratch['max_turns'] ?? ''), 10)
        if (!isNaN(turns) && turns > 0) config.custom!['max_turns'] = turns

        const resume = String(provider.scratch['resume_session_id'] ?? '').trim()
        if (resume) config.custom!['resume_session_id'] = resume
      }

      // --- ADK extras ---
      if (t === 'adk') {
        if (provider.scratch['use_vertex_ai']) {
          config.custom!['use_vertex_ai'] = true
          const proj = String(provider.scratch['vertex_project_id'] ?? '').trim()
          if (proj) config.custom!['vertex_project_id'] = proj
          const loc = String(provider.scratch['vertex_location'] ?? '').trim()
          if (loc) config.custom!['vertex_location'] = loc
        }
      }

      // Drop empty custom map
      if (Object.keys(config.custom!).length === 0) delete config.custom

      await props.onSave(config)
    } catch (err) {
      setError(String(err))
    } finally {
      setSaving(false)
    }
  }

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <form class="provider-form" onSubmit={handleSubmit}>
      {/* ── Name ── */}
      <div class="form-group">
        <label for="provider-name">Name</label>
        <input
          id="provider-name"
          type="text"
          value={provider.name}
          onInput={(e) => setProvider('name', e.currentTarget.value)}
          placeholder="e.g. My Claude provider"
          required
        />
      </div>

      {/* ── Type ── */}
      <FormGroup id="provider-type" label="Type">
        <select
          id="provider-type"
          value={provider.type}
          onChange={(e) => setProvider('type', e.currentTarget.value)}
          required
        >
          <option value="" disabled>Select a type…</option>
          <For each={PROVIDER_TYPES}>
            {(pt) => <option value={pt.value}>{pt.label}</option>}
          </For>
        </select>
      </FormGroup>

      {/* ── Per-provider fields ── */}
      <Switch>
        <Match when={provider.type === 'claude'}>
          <ClaudeFields provider={provider} setProvider={setProvider} />
        </Match>
        <Match when={provider.type === 'claude-ws'}>
          <ClaudeWsFields provider={provider} setProvider={setProvider} />
        </Match>
        <Match when={provider.type === 'adk'}>
          <AdkFields provider={provider} setProvider={setProvider} />
        </Match>
        <Match when={provider.type === 'acp'}>
          <AcpFields provider={provider} setProvider={setProvider} />
        </Match>
        <Match when={provider.type === 'pty'}>
          <PtyFields provider={provider} setProvider={setProvider} />
        </Match>
      </Switch>

      {/* ── Environment variables ── */}
      <div class="form-group">
        <label>Environment Variables</label>
        <div class="env-list">
          <Show when={envKeys().length === 0}>
            <p class="form-hint">No environment variables added.</p>
          </Show>
          <For each={envKeys()}>
            {(key) => (
              <div class="env-row">
                <input
                  type="text"
                  value={key}
                  onInput={(e) => updateEnvKey(key, e.currentTarget.value)}
                  placeholder="KEY"
                />
                <input
                  type="text"
                  value={provider.env?.[key] ?? ''}
                  onInput={(e) => updateEnvVal(key, e.currentTarget.value)}
                  placeholder="Value"
                />
                <button type="button" class="btn-secondary" onClick={() => removeEnvEntry(key)}>
                  Remove
                </button>
              </div>
            )}
          </For>
          <button type="button" class="btn-secondary" onClick={addEnvEntry}>
            Add variable
          </button>
        </div>
      </div>

      {/* ── Active ── */}
      <div class="form-group">
        <label class="checkbox-label">
          <input
            type="checkbox"
            checked={provider.is_active}
            onChange={(e) => setProvider('is_active', e.currentTarget.checked)}
          />
          <span>Active</span>
        </label>
      </div>

      <Show when={error()}>
        <p class="error-message">{error()}</p>
      </Show>

      <div class="form-actions">
        <button type="button" class="btn-secondary" onClick={props.onCancel}>
          Cancel
        </button>
        <button type="submit" class="btn-primary" disabled={saving()}>
          {saving() ? 'Saving…' : props.provider ? 'Update' : 'Create'}
        </button>
      </div>
    </form>
  )
}

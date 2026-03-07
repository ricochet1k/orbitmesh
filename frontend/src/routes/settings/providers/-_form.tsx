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
import { buildConfigRequest, buildTestRequest } from './-build_requests'
import { ClaudeFields } from './-_claude'
import { ClaudeWsFields } from './-_claudews'
import { AdkFields } from './-_adk'
import { AcpFields } from './-_acp'
import { PtyFields } from './-_pty'
import { OpenAiFields } from './-_openai'
import { CodexFields } from './-_codex'
import { apiClient } from '../../../api/client'

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface ProviderFormProps {
  provider?: ProviderConfigResponse
  onSave: (config: ProviderConfigRequest) => Promise<void>
  onCancel: () => void
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function ProviderForm(props: ProviderFormProps) {
  const [provider, setProvider] = createStore(
    untrack(() => initialProviderEdit(props.provider)),
  )

  const [error, setError]       = createSignal<string | null>(null)
  const [saving, setSaving]     = createSignal(false)
  const [testing, setTesting]   = createSignal(false)
  const [testResult, setTestResult] = createSignal<{ ok: boolean; message: string } | null>(null)

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
  // Test handler — validates config without saving
  // ---------------------------------------------------------------------------

  const handleTest = async (e: Event) => {
    e.preventDefault()
    setTestResult(null)
    setError(null)
    setTesting(true)
    try {
      const result = await apiClient.testProvider(buildTestRequest(provider))
      setTestResult(result)
    } catch (err) {
      setTestResult({ ok: false, message: String(err) })
    } finally {
      setTesting(false)
    }
  }

  // ---------------------------------------------------------------------------
  // Submit handler — assembles ProviderConfigRequest from store
  // ---------------------------------------------------------------------------

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)

    try {
      const config = buildConfigRequest(provider)
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
        <Match when={provider.type === 'openai'}>
          <OpenAiFields provider={provider} setProvider={setProvider} />
        </Match>
        <Match when={provider.type === 'codex'}>
          <CodexFields provider={provider} setProvider={setProvider} />
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
                <button type="button" class="btn btn-secondary" onClick={() => removeEnvEntry(key)}>
                  Remove
                </button>
              </div>
            )}
          </For>
          <button type="button" class="btn btn-secondary" onClick={addEnvEntry}>
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

      <Show when={testResult()}>
        <p class={testResult()!.ok ? 'test-result test-result--ok' : 'test-result test-result--fail'}>
          {testResult()!.ok ? '✓' : '✗'} {testResult()!.message}
        </p>
      </Show>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onClick={props.onCancel}>
          Cancel
        </button>
        <button
          type="button"
          class="btn btn-secondary"
          onClick={handleTest}
          disabled={testing() || saving() || !provider.type}
        >
          {testing() ? 'Testing…' : 'Test'}
        </button>
        <button type="submit" class="btn btn-primary" disabled={saving() || testing()}>
          {saving() ? 'Saving…' : props.provider ? 'Update' : 'Create'}
        </button>
      </div>
    </form>
  )
}

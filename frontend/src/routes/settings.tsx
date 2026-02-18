import { createFileRoute } from '@tanstack/solid-router'
import { createSignal, createResource, For, Show } from 'solid-js'
import { apiClient } from '../api/client'
import type { ProviderConfigRequest, ProviderConfigResponse } from '../types/api'

export const Route = createFileRoute('/settings')({
  component: RouteComponent,
})

// Provider types that are actually registered in the backend factory.
const PROVIDER_TYPES = [
  { value: 'claude-ws', label: 'Claude (WebSocket SDK)' },
  { value: 'claude', label: 'Claude (stream-json)' },
  { value: 'acp', label: 'ACP (generic agent)' },
  { value: 'adk', label: 'ADK (Gemini / Google)' },
  { value: 'pty', label: 'PTY (terminal)' },
] as const

type ProviderType = (typeof PROVIDER_TYPES)[number]['value']

// Types that use an Anthropic API key.
const ANTHROPIC_KEY_TYPES: ProviderType[] = ['claude-ws', 'claude']
// Types that use a Google API key.
const GOOGLE_KEY_TYPES: ProviderType[] = ['adk']
// Types that have a model field.
const MODEL_TYPES: ProviderType[] = ['claude-ws', 'claude', 'adk']

function apiKeyLabel(type: string): string {
  if (ANTHROPIC_KEY_TYPES.includes(type as ProviderType)) return 'Anthropic API Key'
  if (GOOGLE_KEY_TYPES.includes(type as ProviderType)) return 'Google API Key'
  return 'API Key'
}

function apiKeyPlaceholder(type: string): string {
  if (ANTHROPIC_KEY_TYPES.includes(type as ProviderType)) return 'sk-ant-...'
  if (GOOGLE_KEY_TYPES.includes(type as ProviderType)) return 'AIza...'
  return 'Enter API key'
}

function RouteComponent() {
  const [providers, { refetch }] = createResource(() => apiClient.listProviders())
  // null = closed, 'add' = adding new, provider id = editing that provider
  const [formMode, setFormMode] = createSignal<null | 'add' | string>(null)

  const editingProvider = () => {
    const mode = formMode()
    if (!mode || mode === 'add') return null
    return providers()?.providers.find((p) => p.id === mode) ?? null
  }

  const openAdd = () => setFormMode('add')
  const openEdit = (id: string) => setFormMode(id)
  const closeForm = () => setFormMode(null)

  return (
    <div class="settings-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Workspace settings</p>
          <h1>Settings</h1>
          <p class="dashboard-subtitle">
            Configure providers, integrations, and operational defaults.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Providers</p>
            <strong>{providers()?.providers.length ?? 0}</strong>
          </div>
          <div class="meta-card">
            <p>Integrations</p>
            <strong>Coming soon</strong>
          </div>
          <div class="meta-card">
            <p>Notifications</p>
            <strong>Coming soon</strong>
          </div>
        </div>
      </header>

      <main class="settings-layout">
        <section class="placeholder-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Configuration</p>
              <h2>Providers</h2>
            </div>
            <Show
              when={formMode() === 'add'}
              fallback={
                <button class="btn-primary" onClick={openAdd}>
                  Add Provider
                </button>
              }
            >
              <button class="btn-secondary" onClick={closeForm}>
                Cancel
              </button>
            </Show>
          </div>

          <Show when={formMode() === 'add'}>
            <ProviderForm
              onSave={async (config) => {
                await apiClient.createProvider(config)
                refetch()
                closeForm()
              }}
              onCancel={closeForm}
            />
          </Show>

          <div class="providers-list">
            <Show when={providers.loading}>
              <p class="muted">Loading providers...</p>
            </Show>
            <Show when={providers.error}>
              <p class="error-message">Failed to load providers: {String(providers.error)}</p>
            </Show>
            <Show when={providers()?.providers.length === 0 && !providers.loading}>
              <div class="empty-state">
                <p class="muted">No providers configured yet.</p>
                <p class="form-hint">
                  Add a provider to start launching agent sessions. Choose{' '}
                  <strong>Claude (WebSocket SDK)</strong> for interactive Claude sessions,{' '}
                  <strong>ACP</strong> for any ACP-compatible agent, or{' '}
                  <strong>ADK</strong> for Gemini/Google AI.
                </p>
              </div>
            </Show>
            <For each={providers()?.providers}>
              {(provider) => (
                <>
                  <div class={`provider-card${formMode() === provider.id ? ' provider-card--editing' : ''}`}>
                    <div class="provider-info">
                      <div class="provider-name-row">
                        <h3>{provider.name}</h3>
                        <span class={`status-badge ${provider.is_active ? 'active' : 'inactive'}`}>
                          {provider.is_active ? 'Active' : 'Inactive'}
                        </span>
                      </div>
                      <p class="muted provider-type-label">
                        {PROVIDER_TYPES.find((t) => t.value === provider.type)?.label ?? provider.type}
                      </p>
                      <div class="provider-meta-chips">
                        <Show when={provider.command && provider.command.length > 0}>
                          <span class="meta-chip">cmd: {provider.command!.join(' ')}</span>
                        </Show>
                        <Show when={provider.custom?.['acp_command']}>
                          <span class="meta-chip">cmd: {provider.custom!['acp_command'] as string}</span>
                        </Show>
                        <Show when={provider.custom?.['model']}>
                          <span class="meta-chip">model: {provider.custom!['model'] as string}</span>
                        </Show>
                        <Show when={provider.custom?.['permission_mode']}>
                          <span class="meta-chip">
                            permissions: {provider.custom!['permission_mode'] as string}
                          </span>
                        </Show>
                        <Show when={provider.custom?.['max_budget_usd']}>
                          <span class="meta-chip">
                            budget: ${provider.custom!['max_budget_usd'] as number}
                          </span>
                        </Show>
                        <Show when={provider.api_key}>
                          <span class="meta-chip meta-chip--key">API key configured</span>
                        </Show>
                      </div>
                    </div>
                    <div class="provider-actions">
                      <button
                        class="btn-secondary"
                        onClick={() =>
                          formMode() === provider.id ? closeForm() : openEdit(provider.id)
                        }
                      >
                        {formMode() === provider.id ? 'Cancel' : 'Edit'}
                      </button>
                      <button
                        class="btn-danger"
                        onClick={async () => {
                          if (confirm(`Delete provider "${provider.name}"?`)) {
                            await apiClient.deleteProvider(provider.id)
                            refetch()
                            if (formMode() === provider.id) closeForm()
                          }
                        }}
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                  <Show when={formMode() === provider.id}>
                    <div class="provider-edit-form">
                      <ProviderForm
                        provider={editingProvider()!}
                        onSave={async (config) => {
                          await apiClient.updateProvider(provider.id, config)
                          refetch()
                          closeForm()
                        }}
                        onCancel={closeForm}
                      />
                    </div>
                  </Show>
                </>
              )}
            </For>
          </div>
        </section>

        <section class="placeholder-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Account</p>
              <h2>User Profile</h2>
            </div>
            <span class="panel-pill neutral">Planned</span>
          </div>
          <p class="muted">User profile controls will appear here.</p>
        </section>

        <section class="placeholder-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Integrations</p>
              <h2>Connected Services</h2>
            </div>
            <span class="panel-pill neutral">Planned</span>
          </div>
          <p class="muted">Integration toggles and keys are coming soon.</p>
        </section>

        <section class="placeholder-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Preferences</p>
              <h2>Default Behaviors</h2>
            </div>
            <span class="panel-pill neutral">Planned</span>
          </div>
          <p class="muted">Preference settings will live in this area.</p>
        </section>
      </main>
    </div>
  )
}

interface ProviderFormProps {
  provider?: ProviderConfigResponse
  onSave: (config: ProviderConfigRequest) => Promise<void>
  onCancel: () => void
}

function ProviderForm(props: ProviderFormProps) {
  const custom = () => props.provider?.custom ?? {}

  const [name, setName] = createSignal(props.provider?.name ?? '')
  const [type, setType] = createSignal<string>(props.provider?.type ?? 'claude-ws')
  const [apiKey, setApiKey] = createSignal(props.provider?.api_key ?? '')

  // PTY-specific
  const [command, setCommand] = createSignal(props.provider?.command?.join(' ') ?? '')

  // ACP-specific
  const [acpCommand, setAcpCommand] = createSignal((custom()['acp_command'] as string | undefined) ?? '')
  const [acpArgs, setAcpArgs] = createSignal(
    Array.isArray(custom()['acp_args'])
      ? (custom()['acp_args'] as string[]).join(' ')
      : ((custom()['acp_args'] as string | undefined) ?? ''),
  )

  // Shared custom fields (claude, claude-ws, adk)
  const [model, setModel] = createSignal((custom()['model'] as string | undefined) ?? '')

  // Claude / claude-ws fields
  const [permissionMode, setPermissionMode] = createSignal(
    (custom()['permission_mode'] as string | undefined) ?? '',
  )
  const [maxBudget, setMaxBudget] = createSignal(
    (custom()['max_budget_usd'] as number | undefined) != null
      ? String(custom()['max_budget_usd'])
      : '',
  )
  const [allowedTools, setAllowedTools] = createSignal(
    Array.isArray(custom()['allowed_tools'])
      ? (custom()['allowed_tools'] as string[]).join(', ')
      : '',
  )
  const [disallowedTools, setDisallowedTools] = createSignal(
    Array.isArray(custom()['disallowed_tools'])
      ? (custom()['disallowed_tools'] as string[]).join(', ')
      : '',
  )
  const [dangerouslySkipPerms, setDangerouslySkipPerms] = createSignal(
    (custom()['dangerously_skip_permissions'] as boolean | undefined) ?? false,
  )

  // claude-ws extras
  const [maxTurns, setMaxTurns] = createSignal(
    (custom()['max_turns'] as number | undefined) != null ? String(custom()['max_turns']) : '',
  )
  const [resumeSessionId, setResumeSessionId] = createSignal(
    (custom()['resume_session_id'] as string | undefined) ?? '',
  )

  // ADK-specific
  const [useVertexAI, setUseVertexAI] = createSignal(
    (custom()['use_vertex_ai'] as boolean | undefined) ?? false,
  )
  const [vertexProjectId, setVertexProjectId] = createSignal(
    (custom()['vertex_project_id'] as string | undefined) ?? '',
  )
  const [vertexLocation, setVertexLocation] = createSignal(
    (custom()['vertex_location'] as string | undefined) ?? '',
  )

  // Environment variables
  const [envEntries, setEnvEntries] = createSignal(
    props.provider?.env
      ? Object.entries(props.provider.env).map(([key, value]) => ({ key, value }))
      : [],
  )
  const [isActive, setIsActive] = createSignal(props.provider?.is_active ?? true)
  const [error, setError] = createSignal<string | null>(null)
  const [saving, setSaving] = createSignal(false)

  const addEnvEntry = () => setEnvEntries((prev) => [...prev, { key: '', value: '' }])

  const updateEnvEntry = (index: number, patch: { key?: string; value?: string }) =>
    setEnvEntries((prev) => prev.map((entry, i) => (i === index ? { ...entry, ...patch } : entry)))

  const removeEnvEntry = (index: number) =>
    setEnvEntries((prev) => prev.filter((_, i) => i !== index))

  const buildEnvMap = () => {
    const env: Record<string, string> = {}
    envEntries().forEach(({ key, value }) => {
      const k = key.trim()
      if (k) env[k] = value.trim()
    })
    return env
  }

  const parseTools = (raw: string): string[] =>
    raw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)

    try {
      const t = type()
      const config: ProviderConfigRequest = {
        name: name().trim(),
        type: t,
        is_active: isActive(),
      }

      if (!config.name) throw new Error('Name is required')

      const env = buildEnvMap()
      if (Object.keys(env).length > 0) config.env = env

      // API key
      const key = apiKey().trim()
      if (key) config.api_key = key

      // Build custom map
      const customMap: Record<string, unknown> = {}

      if (t === 'pty') {
        const cmdParts = command().trim().split(/\s+/)
        if (!cmdParts[0]) throw new Error('Command is required for PTY provider')
        config.command = cmdParts
      }

      if (t === 'acp') {
        const cmd = acpCommand().trim()
        if (!cmd) throw new Error('ACP command is required')
        customMap['acp_command'] = cmd
        const rawArgs = acpArgs().trim()
        if (rawArgs) customMap['acp_args'] = rawArgs.split(/\s+/)
      }

      if (MODEL_TYPES.includes(t as ProviderType) && model().trim()) {
        customMap['model'] = model().trim()
      }

      if (t === 'claude' || t === 'claude-ws') {
        if (permissionMode()) customMap['permission_mode'] = permissionMode()
        const budget = parseFloat(maxBudget())
        if (!isNaN(budget) && budget > 0) customMap['max_budget_usd'] = budget
        const allowed = parseTools(allowedTools())
        if (allowed.length > 0) customMap['allowed_tools'] = allowed
        const disallowed = parseTools(disallowedTools())
        if (disallowed.length > 0) customMap['disallowed_tools'] = disallowed
        if (dangerouslySkipPerms()) customMap['dangerously_skip_permissions'] = true
      }

      if (t === 'claude-ws') {
        const turns = parseInt(maxTurns(), 10)
        if (!isNaN(turns) && turns > 0) customMap['max_turns'] = turns
        if (resumeSessionId().trim()) customMap['resume_session_id'] = resumeSessionId().trim()
      }

      if (t === 'adk') {
        if (useVertexAI()) {
          customMap['use_vertex_ai'] = true
          customMap['vertex_project_id'] = vertexProjectId().trim()
          customMap['vertex_location'] = vertexLocation().trim()
        }
      }

      if (Object.keys(customMap).length > 0) config.custom = customMap

      await props.onSave(config)
    } catch (err) {
      setError(String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form class="provider-form" onSubmit={handleSubmit}>
      {/* ── Name ── */}
      <div class="form-group">
        <label for="provider-name">Name</label>
        <input
          id="provider-name"
          type="text"
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
          placeholder="e.g. My Claude provider"
          required
        />
      </div>

      {/* ── Type ── */}
      <div class="form-group">
        <label for="provider-type">Type</label>
        <select
          id="provider-type"
          value={type()}
          onChange={(e) => setType(e.currentTarget.value)}
          required
        >
          <For each={PROVIDER_TYPES}>
            {(pt) => <option value={pt.value}>{pt.label}</option>}
          </For>
        </select>
      </div>

      {/* ── PTY: command ── */}
      <Show when={type() === 'pty'}>
        <div class="form-group">
          <label for="provider-command">Command</label>
          <input
            id="provider-command"
            type="text"
            value={command()}
            onInput={(e) => setCommand(e.currentTarget.value)}
            placeholder="e.g. claude"
            required
          />
          <p class="form-hint">Space-separated command and arguments (e.g. <code>claude --verbose</code>)</p>
        </div>
      </Show>

      {/* ── ACP: command + args ── */}
      <Show when={type() === 'acp'}>
        <div class="form-group">
          <label for="acp-command">ACP Command</label>
          <input
            id="acp-command"
            type="text"
            value={acpCommand()}
            onInput={(e) => setAcpCommand(e.currentTarget.value)}
            placeholder="e.g. gemini"
            required
          />
          <p class="form-hint">The executable to launch (ACP-compatible agent binary).</p>
        </div>
        <div class="form-group">
          <label for="acp-args">ACP Arguments</label>
          <input
            id="acp-args"
            type="text"
            value={acpArgs()}
            onInput={(e) => setAcpArgs(e.currentTarget.value)}
            placeholder="e.g. --experimental-acp"
          />
          <p class="form-hint">Space-separated arguments passed to the ACP command.</p>
        </div>
      </Show>

      {/* ── API key (all non-PTY types) ── */}
      <Show when={type() !== 'pty'}>
        <div class="form-group">
          <label for="provider-api-key">{apiKeyLabel(type())}</label>
          <input
            id="provider-api-key"
            type="password"
            value={apiKey()}
            onInput={(e) => setApiKey(e.currentTarget.value)}
            placeholder={apiKeyPlaceholder(type())}
          />
          <p class="form-hint">
            Stored securely and injected as an environment variable when sessions start.
          </p>
        </div>
      </Show>

      {/* ── Model (claude, claude-ws, adk) ── */}
      <Show when={MODEL_TYPES.includes(type() as ProviderType)}>
        <div class="form-group">
          <label for="provider-model">Model</label>
          <input
            id="provider-model"
            type="text"
            value={model()}
            onInput={(e) => setModel(e.currentTarget.value)}
            placeholder={
              type() === 'adk'
                ? 'e.g. gemini-2.5-flash'
                : 'e.g. claude-opus-4-5'
            }
          />
          <p class="form-hint">Optional model override. Leave blank to use the provider default.</p>
        </div>
      </Show>

      {/* ── Claude / claude-ws shared fields ── */}
      <Show when={type() === 'claude' || type() === 'claude-ws'}>
        <div class="form-group">
          <label for="provider-permission-mode">Permission Mode</label>
          <select
            id="provider-permission-mode"
            value={permissionMode()}
            onChange={(e) => setPermissionMode(e.currentTarget.value)}
          >
            <option value="">Default</option>
            <option value="acceptEdits">Accept edits</option>
            <option value="autoEdit">Auto-edit</option>
            <option value="bypassPermissions">Bypass permissions</option>
          </select>
          <p class="form-hint">Controls how Claude handles tool permission prompts.</p>
        </div>

        <div class="form-group">
          <label for="provider-max-budget">Max Budget (USD)</label>
          <input
            id="provider-max-budget"
            type="number"
            min="0"
            step="0.01"
            value={maxBudget()}
            onInput={(e) => setMaxBudget(e.currentTarget.value)}
            placeholder="e.g. 5.00"
          />
          <p class="form-hint">Cap API spend per session. Leave blank for no limit.</p>
        </div>

        <div class="form-group">
          <label for="provider-allowed-tools">Allowed Tools</label>
          <input
            id="provider-allowed-tools"
            type="text"
            value={allowedTools()}
            onInput={(e) => setAllowedTools(e.currentTarget.value)}
            placeholder="e.g. Bash, Read, Write"
          />
          <p class="form-hint">Comma-separated tool names. Leave blank to allow all.</p>
        </div>

        <div class="form-group">
          <label for="provider-disallowed-tools">Disallowed Tools</label>
          <input
            id="provider-disallowed-tools"
            type="text"
            value={disallowedTools()}
            onInput={(e) => setDisallowedTools(e.currentTarget.value)}
            placeholder="e.g. Bash"
          />
          <p class="form-hint">Comma-separated tool names to block. Leave blank to block none.</p>
        </div>

        <div class="form-group form-group--danger">
          <label class="checkbox-label">
            <input
              type="checkbox"
              checked={dangerouslySkipPerms()}
              onChange={(e) => setDangerouslySkipPerms(e.currentTarget.checked)}
            />
            <span>Dangerously skip permissions</span>
          </label>
          <p class="form-hint form-hint--danger">
            Auto-approves all tool calls without prompting. Use only in trusted, sandboxed environments.
          </p>
        </div>
      </Show>

      {/* ── claude-ws extras ── */}
      <Show when={type() === 'claude-ws'}>
        <div class="form-group">
          <label for="provider-max-turns">Max Turns</label>
          <input
            id="provider-max-turns"
            type="number"
            min="1"
            step="1"
            value={maxTurns()}
            onInput={(e) => setMaxTurns(e.currentTarget.value)}
            placeholder="e.g. 20"
          />
          <p class="form-hint">Maximum agentic turns per session. Leave blank for no limit.</p>
        </div>

        <div class="form-group">
          <label for="provider-resume-session">Resume Session ID</label>
          <input
            id="provider-resume-session"
            type="text"
            value={resumeSessionId()}
            onInput={(e) => setResumeSessionId(e.currentTarget.value)}
            placeholder="e.g. abc123"
          />
          <p class="form-hint">Resume a previous Claude session by its session ID.</p>
        </div>
      </Show>

      {/* ── ADK extras ── */}
      <Show when={type() === 'adk'}>
        <div class="form-group">
          <label class="checkbox-label">
            <input
              type="checkbox"
              checked={useVertexAI()}
              onChange={(e) => setUseVertexAI(e.currentTarget.checked)}
            />
            <span>Use Vertex AI (OAuth)</span>
          </label>
          <p class="form-hint">Uses your Google Cloud credentials instead of an API key.</p>
        </div>
        <Show when={useVertexAI()}>
          <div class="form-group">
            <label for="provider-vertex-project">Vertex Project ID</label>
            <input
              id="provider-vertex-project"
              type="text"
              value={vertexProjectId()}
              onInput={(e) => setVertexProjectId(e.currentTarget.value)}
              placeholder="e.g. my-gcp-project"
            />
          </div>
          <div class="form-group">
            <label for="provider-vertex-location">Vertex Location</label>
            <input
              id="provider-vertex-location"
              type="text"
              value={vertexLocation()}
              onInput={(e) => setVertexLocation(e.currentTarget.value)}
              placeholder="e.g. us-central1"
            />
          </div>
        </Show>
      </Show>

      {/* ── Environment variables ── */}
      <div class="form-group">
        <label>Environment Variables</label>
        <div class="env-list">
          <Show when={envEntries().length === 0}>
            <p class="form-hint">No environment variables added.</p>
          </Show>
          <For each={envEntries()}>
            {(entry, index) => (
              <div class="env-row">
                <input
                  type="text"
                  value={entry.key}
                  onInput={(e) => updateEnvEntry(index(), { key: e.currentTarget.value })}
                  placeholder="KEY"
                />
                <input
                  type="text"
                  value={entry.value}
                  onInput={(e) => updateEnvEntry(index(), { value: e.currentTarget.value })}
                  placeholder="Value"
                />
                <button type="button" class="btn-secondary" onClick={() => removeEnvEntry(index())}>
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
            checked={isActive()}
            onChange={(e) => setIsActive(e.currentTarget.checked)}
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
          {saving() ? 'Saving...' : props.provider ? 'Update' : 'Create'}
        </button>
      </div>
    </form>
  )
}

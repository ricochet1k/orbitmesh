import { createFileRoute } from '@tanstack/solid-router'
import { createSignal, createResource, For, Show } from 'solid-js'
import { apiClient } from '../api/client'
import type { ProviderConfigRequest, ProviderConfigResponse } from '../types/api'

export const Route = createFileRoute('/settings')({
  component: RouteComponent,
})

function RouteComponent() {
  const [providers, { refetch }] = createResource(() => apiClient.listProviders())
  const [showAddForm, setShowAddForm] = createSignal(false)
  const [editingProvider, setEditingProvider] = createSignal<ProviderConfigResponse | null>(null)

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
            <button
              class="btn-primary"
              onClick={() => setShowAddForm(!showAddForm())}
            >
              {showAddForm() ? 'Cancel' : 'Add Provider'}
            </button>
          </div>

          <Show when={showAddForm()}>
            <ProviderForm
              onSave={async (config) => {
                await apiClient.createProvider(config)
                refetch()
                setShowAddForm(false)
              }}
              onCancel={() => setShowAddForm(false)}
            />
          </Show>

          <Show when={editingProvider()}>
            {(provider) => (
              <ProviderForm
                provider={provider()}
                onSave={async (config) => {
                  await apiClient.updateProvider(provider().id, config)
                  refetch()
                  setEditingProvider(null)
                }}
                onCancel={() => setEditingProvider(null)}
              />
            )}
          </Show>

          <div class="providers-list">
            <Show when={providers.loading}>
              <p class="muted">Loading providers...</p>
            </Show>
            <Show when={providers.error}>
              <p class="error-message">Failed to load providers: {String(providers.error)}</p>
            </Show>
            <Show when={providers()?.providers.length === 0}>
              <p class="muted">No providers configured yet. Add one to get started.</p>
            </Show>
            <For each={providers()?.providers}>
              {(provider) => (
                <div class="provider-card">
                  <div class="provider-info">
                    <h3>{provider.name}</h3>
                    <p class="muted">Type: {provider.type}</p>
                    <Show when={provider.command}>
                      <p class="muted">Command: {provider.command?.join(' ')}</p>
                    </Show>
                    <span class={`status-badge ${provider.is_active ? 'active' : 'inactive'}`}>
                      {provider.is_active ? 'Active' : 'Inactive'}
                    </span>
                  </div>
                  <div class="provider-actions">
                    <button
                      class="btn-secondary"
                      onClick={() => setEditingProvider(provider)}
                    >
                      Edit
                    </button>
                    <button
                      class="btn-danger"
                      onClick={async () => {
                        if (confirm(`Delete provider "${provider.name}"?`)) {
                          await apiClient.deleteProvider(provider.id)
                          refetch()
                        }
                      }}
                    >
                      Delete
                    </button>
                  </div>
                </div>
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
  const [name, setName] = createSignal(props.provider?.name ?? '')
  const [type, setType] = createSignal(props.provider?.type ?? 'pty')
  const [command, setCommand] = createSignal(props.provider?.command?.join(' ') ?? '')
  const [apiKey, setApiKey] = createSignal(props.provider?.api_key ?? '')
  const [model, setModel] = createSignal((props.provider?.custom?.model as string | undefined) ?? '')
  const [useVertexAI, setUseVertexAI] = createSignal(
    (props.provider?.custom?.use_vertex_ai as boolean | undefined) ?? false,
  )
  const [vertexProjectId, setVertexProjectId] = createSignal(
    (props.provider?.custom?.vertex_project_id as string | undefined) ?? '',
  )
  const [vertexLocation, setVertexLocation] = createSignal(
    (props.provider?.custom?.vertex_location as string | undefined) ?? '',
  )
  const [envEntries, setEnvEntries] = createSignal(
    props.provider?.env
      ? Object.entries(props.provider.env).map(([key, value]) => ({ key, value }))
      : [],
  )
  const [isActive, setIsActive] = createSignal(props.provider?.is_active ?? true)
  const [error, setError] = createSignal<string | null>(null)
  const [saving, setSaving] = createSignal(false)

  const addEnvEntry = () => {
    setEnvEntries((prev) => [...prev, { key: '', value: '' }])
  }

  const updateEnvEntry = (index: number, patch: { key?: string; value?: string }) => {
    setEnvEntries((prev) =>
      prev.map((entry, i) => (i === index ? { ...entry, ...patch } : entry)),
    )
  }

  const removeEnvEntry = (index: number) => {
    setEnvEntries((prev) => prev.filter((_, i) => i !== index))
  }

  const buildEnvMap = () => {
    const env: Record<string, string> = {}
    envEntries().forEach((entry) => {
      const key = entry.key.trim()
      if (!key) return
      env[key] = entry.value.trim()
    })
    return env
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)

    try {
      const config: ProviderConfigRequest = {
        name: name().trim(),
        type: type(),
        is_active: isActive(),
      }

      if (!config.name) {
        throw new Error('Name is required')
      }

      const env = buildEnvMap()
      if (Object.keys(env).length > 0) {
        config.env = env
      }

      if (model().trim()) {
        config.custom = { ...(config.custom ?? {}), model: model().trim() }
      }

      if (type() === 'adk') {
        if (useVertexAI()) {
          config.custom = {
            ...(config.custom ?? {}),
            use_vertex_ai: true,
            vertex_project_id: vertexProjectId().trim(),
            vertex_location: vertexLocation().trim(),
          }
        }
      }

      if (type() === 'pty') {
        const cmdParts = command().trim().split(/\s+/)
        if (cmdParts.length === 0 || cmdParts[0] === '') {
          throw new Error('Command is required for PTY provider')
        }
        config.command = cmdParts
      } else if (type() === 'adk' || type() === 'anthropic' || type() === 'openai') {
        if (apiKey().trim()) {
          config.api_key = apiKey().trim()
        }
      }

      await props.onSave(config)
    } catch (err) {
      setError(String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form class="provider-form" onSubmit={handleSubmit}>
      <div class="form-group">
        <label for="provider-name">Name</label>
        <input
          id="provider-name"
          type="text"
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
          placeholder="e.g. My PTY Provider"
          required
        />
      </div>

      <div class="form-group">
        <label for="provider-type">Type</label>
        <select
          id="provider-type"
          value={type()}
          onChange={(e) => setType(e.currentTarget.value)}
          required
        >
          <option value="pty">PTY (Claude CLI)</option>
          <option value="adk">ADK (Google)</option>
          <option value="anthropic">Anthropic</option>
          <option value="openai">OpenAI</option>
        </select>
      </div>

      <Show when={type() === 'pty'}>
        <div class="form-group">
          <label for="provider-command">Command</label>
          <input
            id="provider-command"
            type="text"
            value={command()}
            onInput={(e) => setCommand(e.currentTarget.value)}
            placeholder="e.g. bash or claude"
            required
          />
          <p class="form-hint">Space-separated command and arguments</p>
        </div>
      </Show>

      <Show when={type() === 'adk' || type() === 'anthropic' || type() === 'openai'}>
        <div class="form-group">
          <label for="provider-api-key">API Key</label>
          <input
            id="provider-api-key"
            type="password"
            value={apiKey()}
            onInput={(e) => setApiKey(e.currentTarget.value)}
            placeholder="Enter API key"
          />
        </div>
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
            <p class="form-hint">Uses your Google Cloud credentials instead of API key.</p>
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
        <div class="form-group">
          <label for="provider-model">Model</label>
          <input
            id="provider-model"
            type="text"
            value={model()}
            onInput={(e) => setModel(e.currentTarget.value)}
            placeholder="e.g. gemini-2.5-flash"
          />
          <p class="form-hint">Optional model override</p>
        </div>
      </Show>

      <div class="form-group">
        <label>Environment variables</label>
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

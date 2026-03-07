import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import type {
  MCPServerEntryRequest,
  MCPServerEntryResponse,
  MCPServerCapabilities,
} from '../../types/api'

export const Route = createFileRoute('/settings/mcp-servers')({
  component: MCPServersPage,
})

// ---- Form ----

interface MCPServerFormProps {
  server?: MCPServerEntryResponse
  onSave: (req: MCPServerEntryRequest) => Promise<void>
  onCancel: () => void
}

function MCPServerForm(props: MCPServerFormProps) {
  const [name, setName] = createSignal(props.server?.name ?? '')
  const [type, setType] = createSignal<'command' | 'sse'>(props.server?.type ?? 'command')
  const [command, setCommand] = createSignal(props.server?.command ?? '')
  const [args, setArgs] = createSignal((props.server?.args ?? []).join(' '))
  const [url, setUrl] = createSignal(props.server?.url ?? '')
  const [authType, setAuthType] = createSignal(props.server?.auth_type ?? 'none')
  const [token, setToken] = createSignal('')
  const [clientId, setClientId] = createSignal(props.server?.client_id ?? '')
  const [clientSecret, setClientSecret] = createSignal('')
  const [authUrl, setAuthUrl] = createSignal(props.server?.auth_url ?? '')
  const [tokenUrl, setTokenUrl] = createSignal(props.server?.token_url ?? '')
  const [scopes, setScopes] = createSignal((props.server?.scopes ?? []).join(' '))
  const [saving, setSaving] = createSignal(false)
  const [error, setError] = createSignal<string | null>(null)

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      const trimmedName = name().trim()
      if (!trimmedName) throw new Error('Name is required')

      const req: MCPServerEntryRequest = {
        name: trimmedName,
        type: type(),
      }

      if (type() === 'command') {
        req.command = command().trim()
        const argsStr = args().trim()
        if (argsStr) req.args = argsStr.split(/\s+/)
      } else {
        req.url = url().trim()
        req.auth_type = authType() as MCPServerEntryRequest['auth_type']
        if (authType() === 'bearer' && token().trim()) {
          req.auth = { token: token().trim() }
        } else if (authType() === 'api_key' && token().trim()) {
          req.auth = { api_key: token().trim() }
        } else if (authType() === 'oauth2') {
          req.auth = {
            client_id: clientId().trim(),
            client_secret: clientSecret().trim() || undefined,
            auth_url: authUrl().trim(),
            token_url: tokenUrl().trim(),
            scopes: scopes().trim() ? scopes().trim().split(/\s+/) : undefined,
          }
        }
      }

      await props.onSave(req)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form class="provider-form" onSubmit={handleSubmit}>
      <div class="form-group">
        <label for="mcp-name">Name</label>
        <input
          id="mcp-name"
          type="text"
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
          placeholder="e.g. filesystem-server"
          required
        />
      </div>

      <div class="form-group">
        <label for="mcp-type">Type</label>
        <select
          id="mcp-type"
          value={type()}
          onChange={(e) => setType(e.currentTarget.value as 'command' | 'sse')}
        >
          <option value="command">Command (stdio)</option>
          <option value="sse">URL (SSE/HTTP)</option>
        </select>
      </div>

      <Show when={type() === 'command'}>
        <div class="form-group">
          <label for="mcp-command">Command</label>
          <input
            id="mcp-command"
            type="text"
            value={command()}
            onInput={(e) => setCommand(e.currentTarget.value)}
            placeholder="e.g. npx @modelcontextprotocol/server-filesystem"
          />
        </div>
        <div class="form-group">
          <label for="mcp-args">Arguments (space-separated)</label>
          <input
            id="mcp-args"
            type="text"
            value={args()}
            onInput={(e) => setArgs(e.currentTarget.value)}
            placeholder="e.g. /tmp"
          />
        </div>
      </Show>

      <Show when={type() === 'sse'}>
        <div class="form-group">
          <label for="mcp-url">URL</label>
          <input
            id="mcp-url"
            type="text"
            value={url()}
            onInput={(e) => setUrl(e.currentTarget.value)}
            placeholder="https://example.com/mcp/sse"
          />
        </div>
        <div class="form-group">
          <label for="mcp-auth-type">Authentication</label>
          <select
            id="mcp-auth-type"
            value={authType()}
            onChange={(e) => setAuthType(e.currentTarget.value)}
          >
            <option value="none">None</option>
            <option value="bearer">Bearer Token</option>
            <option value="api_key">API Key</option>
            <option value="oauth2">OAuth 2.0</option>
          </select>
        </div>

        <Show when={authType() === 'bearer' || authType() === 'api_key'}>
          <div class="form-group">
            <label for="mcp-token">{authType() === 'bearer' ? 'Bearer Token' : 'API Key'}</label>
            <input
              id="mcp-token"
              type="password"
              value={token()}
              onInput={(e) => setToken(e.currentTarget.value)}
              placeholder={props.server?.has_token ? '(stored — leave blank to keep)' : 'Enter token'}
            />
          </div>
        </Show>

        <Show when={authType() === 'oauth2'}>
          <p class="form-hint">
            OAuth endpoints and client credentials will be discovered automatically
            via the server URL. Override below only if needed.
          </p>
          <details class="oauth-advanced">
            <summary>Advanced OAuth settings</summary>
            <div class="form-group">
              <label for="mcp-client-id">Client ID</label>
              <input
                id="mcp-client-id"
                type="text"
                value={clientId()}
                onInput={(e) => setClientId(e.currentTarget.value)}
                placeholder="(auto-discovered if blank)"
              />
            </div>
            <div class="form-group">
              <label for="mcp-client-secret">Client Secret</label>
              <input
                id="mcp-client-secret"
                type="password"
                value={clientSecret()}
                onInput={(e) => setClientSecret(e.currentTarget.value)}
                placeholder="(leave blank to keep existing)"
              />
            </div>
            <div class="form-group">
              <label for="mcp-auth-url">Authorization URL</label>
              <input
                id="mcp-auth-url"
                type="text"
                value={authUrl()}
                onInput={(e) => setAuthUrl(e.currentTarget.value)}
                placeholder="(auto-discovered if blank)"
              />
            </div>
            <div class="form-group">
              <label for="mcp-token-url">Token URL</label>
              <input
                id="mcp-token-url"
                type="text"
                value={tokenUrl()}
                onInput={(e) => setTokenUrl(e.currentTarget.value)}
                placeholder="(auto-discovered if blank)"
              />
            </div>
            <div class="form-group">
              <label for="mcp-scopes">Scopes (space-separated)</label>
              <input
                id="mcp-scopes"
                type="text"
                value={scopes()}
                onInput={(e) => setScopes(e.currentTarget.value)}
              />
            </div>
          </details>
        </Show>
      </Show>

      <Show when={error()}>
        <p class="error-message">{error()}</p>
      </Show>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onClick={props.onCancel}>
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" disabled={saving()}>
          {saving() ? 'Saving…' : props.server ? 'Update' : 'Create'}
        </button>
      </div>
    </form>
  )
}

// ---- Capabilities viewer ----

function CapabilitiesPanel(props: { serverId: string }) {
  const [caps, setCaps] = createSignal<MCPServerCapabilities | null>(null)
  const [loading, setLoading] = createSignal(false)
  const [error, setError] = createSignal<string | null>(null)

  const loadCapabilities = async () => {
    setLoading(true)
    setError(null)
    try {
      const result = await apiClient.getMCPServerCapabilities(props.serverId)
      setCaps(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div class="capabilities-panel" style="margin-top: 0.5rem;">
      <Show when={!caps()}>
        <button
          class="btn btn-secondary"
          onClick={loadCapabilities}
          disabled={loading()}
        >
          {loading() ? 'Connecting…' : 'View Tools'}
        </button>
      </Show>

      <Show when={error()}>
        <p class="error-message" style="margin-top: 0.25rem;">{error()}</p>
      </Show>

      <Show when={caps()}>
        <div style="margin-top: 0.5rem;">
          <h4 style="margin: 0 0 0.25rem;">Tools ({caps()!.tools.length})</h4>
          <Show
            when={caps()!.tools.length > 0}
            fallback={<p class="muted">No tools</p>}
          >
            <ul class="tool-list" style="list-style: none; padding: 0; margin: 0;">
              <For each={caps()!.tools}>
                {(tool) => (
                  <li style="padding: 0.25rem 0; border-bottom: 1px solid var(--border, #333);">
                    <strong>{tool.name}</strong>
                    <Show when={tool.description}>
                      <span class="muted" style="margin-left: 0.5rem;">{tool.description}</span>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>

          <Show when={caps()!.resources && caps()!.resources!.length > 0}>
            <h4 style="margin: 0.5rem 0 0.25rem;">Resources ({caps()!.resources!.length})</h4>
            <ul class="tool-list" style="list-style: none; padding: 0; margin: 0;">
              <For each={caps()!.resources}>
                {(res) => (
                  <li style="padding: 0.25rem 0;">
                    <strong>{res.name || res.uri}</strong>
                    <Show when={res.description}>
                      <span class="muted" style="margin-left: 0.5rem;">{res.description}</span>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>

          <Show when={caps()!.prompts && caps()!.prompts!.length > 0}>
            <h4 style="margin: 0.5rem 0 0.25rem;">Prompts ({caps()!.prompts!.length})</h4>
            <ul class="tool-list" style="list-style: none; padding: 0; margin: 0;">
              <For each={caps()!.prompts}>
                {(prompt) => (
                  <li style="padding: 0.25rem 0;">
                    <strong>{prompt.name}</strong>
                    <Show when={prompt.description}>
                      <span class="muted" style="margin-left: 0.5rem;">{prompt.description}</span>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>

          <button
            class="btn btn-secondary"
            style="margin-top: 0.5rem;"
            onClick={loadCapabilities}
            disabled={loading()}
          >
            Refresh
          </button>
        </div>
      </Show>
    </div>
  )
}

// ---- OAuth button ----

function OAuthButton(props: { server: MCPServerEntryResponse; onAuthorized: () => void }) {
  const [authorizing, setAuthorizing] = createSignal(false)
  const [error, setError] = createSignal<string | null>(null)

  const startOAuth = async () => {
    setAuthorizing(true)
    setError(null)
    try {
      const result = await apiClient.startMCPOAuth(props.server.id)
      window.open(result.auth_url, '_blank', 'width=600,height=700')
      // Poll for token completion
      const poll = setInterval(async () => {
        try {
          const updated = await apiClient.getMCPServer(props.server.id)
          if (updated.has_token) {
            clearInterval(poll)
            setAuthorizing(false)
            props.onAuthorized()
          }
        } catch {
          // keep polling
        }
      }, 2000)
      // Stop polling after 5 minutes
      setTimeout(() => {
        clearInterval(poll)
        setAuthorizing(false)
      }, 5 * 60 * 1000)
    } catch (err) {
      setAuthorizing(false)
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <Show
      when={props.server.has_token}
      fallback={
        <>
          <button class="btn btn-secondary" onClick={startOAuth} disabled={authorizing()}>
            {authorizing() ? 'Authorizing…' : 'Authorize'}
          </button>
          <Show when={error()}>
            <p class="error-message" style="margin-top: 0.25rem;">{error()}</p>
          </Show>
        </>
      }
    >
      <span class="meta-chip">authorized</span>
      <button
        class="btn btn-secondary"
        style="margin-left: 0.5rem;"
        onClick={async () => {
          await apiClient.revokeMCPOAuthToken(props.server.id)
          props.onAuthorized()
        }}
      >
        Revoke
      </button>
    </Show>
  )
}

// ---- Page ----

function MCPServersPage() {
  const [servers, { refetch }] = createResource(() => apiClient.listMCPServers())
  const [formMode, setFormMode] = createSignal<null | 'add' | string>(null)

  const openAdd = () => setFormMode('add')
  const openEdit = (id: string) => setFormMode(id)
  const closeForm = () => setFormMode(null)

  const editingServer = () => {
    const mode = formMode()
    if (!mode || mode === 'add') return undefined
    return servers()?.servers.find((s) => s.id === mode)
  }

  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Configuration</p>
          <h2>MCP Servers</h2>
        </div>
        <Show
          when={formMode() === 'add'}
          fallback={
            <button class="btn btn-primary" onClick={openAdd}>
              Add Server
            </button>
          }
        >
          <button class="btn btn-secondary" onClick={closeForm}>
            Cancel
          </button>
        </Show>
      </div>

      <Show when={formMode() === 'add'}>
        <MCPServerForm
          onSave={async (req) => {
            await apiClient.createMCPServer(req)
            refetch()
            closeForm()
          }}
          onCancel={closeForm}
        />
      </Show>

      <div class="providers-list">
        <Show when={servers.loading}>
          <p class="muted">Loading MCP servers…</p>
        </Show>
        <Show when={servers.error}>
          <p class="error-message">Failed to load MCP servers: {String(servers.error)}</p>
        </Show>
        <Show when={servers()?.servers.length === 0 && !servers.loading}>
          <div class="empty-state">
            <p class="muted">No MCP servers configured yet.</p>
            <p class="form-hint">
              Add an external MCP server to make its tools available to agents via the proxy.
            </p>
          </div>
        </Show>

        <For each={servers()?.servers}>
          {(server) => (
            <>
              <div class={`provider-card${formMode() === server.id ? ' provider-card--editing' : ''}`}>
                <div class="provider-info">
                  <div class="provider-name-row">
                    <h3>{server.name}</h3>
                  </div>
                  <p class="muted provider-type-label">
                    {server.type === 'command' ? `Command: ${server.command}` : `URL: ${server.url}`}
                  </p>
                  <div class="provider-meta-chips">
                    <span class="meta-chip">{server.type}</span>
                    <Show when={server.auth_type && server.auth_type !== 'none'}>
                      <span class="meta-chip">auth: {server.auth_type}</span>
                    </Show>
                    <Show when={server.has_token}>
                      <span class="meta-chip">token stored</span>
                    </Show>
                    <Show when={server.dynamic_registration}>
                      <span class="meta-chip">dynamic registration</span>
                    </Show>
                  </div>
                  <Show when={server.auth_type === 'oauth2'}>
                    <div style="margin-top: 0.5rem;">
                      <OAuthButton server={server} onAuthorized={() => refetch()} />
                    </div>
                  </Show>
                  <CapabilitiesPanel serverId={server.id} />
                </div>
                <div class="provider-actions">
                  <button
                    class="btn btn-secondary"
                    onClick={() => (formMode() === server.id ? closeForm() : openEdit(server.id))}
                  >
                    {formMode() === server.id ? 'Cancel' : 'Edit'}
                  </button>
                  <button
                    class="btn btn-danger"
                    onClick={async () => {
                      if (confirm(`Delete MCP server "${server.name}"?`)) {
                        await apiClient.deleteMCPServer(server.id)
                        refetch()
                        if (formMode() === server.id) closeForm()
                      }
                    }}
                  >
                    Delete
                  </button>
                </div>
              </div>

              <Show when={formMode() === server.id}>
                <div class="provider-edit-form">
                  <MCPServerForm
                    server={editingServer()}
                    onSave={async (req) => {
                      await apiClient.updateMCPServer(server.id, req)
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
  )
}

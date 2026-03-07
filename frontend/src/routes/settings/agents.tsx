import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import type { AgentConfigRequest, AgentConfigResponse, AgentMCPRef, MCPServerEntryResponse } from '../../types/api'
import { AgentMCPServerConfig } from './-agent_mcp_config'

export const Route = createFileRoute('/settings/agents')({
  component: AgentsPage,
})

function modelFromAgent(agent: AgentConfigResponse | null | undefined): string {
  if (!agent) return ''
  const value = agent.custom?.['model']
  return typeof value === 'string' ? value : ''
}

interface AgentFormProps {
  agent?: AgentConfigResponse
  onSave: (config: AgentConfigRequest) => Promise<void>
  onCancel: () => void
}

function AgentForm(props: AgentFormProps) {
  const [name, setName] = createSignal(props.agent?.name ?? '')
  const [systemPrompt, setSystemPrompt] = createSignal(props.agent?.system_prompt ?? '')
  const [defaultModel, setDefaultModel] = createSignal(modelFromAgent(props.agent))
  const [saving, setSaving] = createSignal(false)
  const [error, setError] = createSignal<string | null>(null)

  // MCP server refs
  const [mcpServers] = createResource(() => apiClient.listMCPServers())
  const [selectedRefs, setSelectedRefs] = createSignal<AgentMCPRef[]>(
    props.agent?.mcp_server_refs ?? [],
  )
  const [spawnSubAgentEnabled, setSpawnSubAgentEnabled] = createSignal(
    props.agent?.allowed_sub_agents !== undefined
  )
  const [allowedSubAgents, setAllowedSubAgents] = createSignal<string[]>(
    props.agent?.allowed_sub_agents ?? []
  )

  const isServerSelected = (id: string) => selectedRefs().some((r) => r.server_id === id)

  const toggleServer = (server: MCPServerEntryResponse) => {
    if (isServerSelected(server.id)) {
      setSelectedRefs((refs) => refs.filter((r) => r.server_id !== server.id))
    } else {
      setSelectedRefs((refs) => [...refs, { server_id: server.id }])
    }
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      const trimmedName = name().trim()
      if (!trimmedName) throw new Error('Name is required')

      const custom: Record<string, any> = { ...(props.agent?.custom ?? {}) }
      const model = defaultModel().trim()
      if (model) {
        custom['model'] = model
      } else {
        delete custom['model']
      }

      const refs = selectedRefs()
      const req: AgentConfigRequest = {
        name: trimmedName,
        system_prompt: systemPrompt().trim() || undefined,
        mcp_servers: props.agent?.mcp_servers,
        mcp_server_refs: refs.length > 0 ? refs : undefined,
        custom: Object.keys(custom).length > 0 ? custom : undefined,
        allowed_sub_agents: spawnSubAgentEnabled() ? allowedSubAgents() : undefined,
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
        <label for="agent-name">Name</label>
        <input
          id="agent-name"
          type="text"
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
          placeholder="e.g. Bugfix specialist"
          required
        />
      </div>

      <div class="form-group">
        <label for="agent-model">Default model (optional)</label>
        <input
          id="agent-model"
          type="text"
          value={defaultModel()}
          onInput={(e) => setDefaultModel(e.currentTarget.value)}
          placeholder="e.g. claude-sonnet-4-5"
        />
        <p class="form-hint">Used as the agent default. Sessions can override this per run.</p>
      </div>

      <div class="form-group">
        <label for="agent-system-prompt">System prompt (optional)</label>
        <textarea
          id="agent-system-prompt"
          rows={8}
          value={systemPrompt()}
          onInput={(e) => setSystemPrompt(e.currentTarget.value)}
          placeholder="Instruction defaults applied to sessions using this agent"
        />
      </div>

      <Show when={mcpServers()?.servers && mcpServers()!.servers.length > 0}>
        <div class="form-group">
          <label>MCP Servers (proxy tools into sessions)</label>
          <p class="form-hint">
            Select which MCP servers this agent can access. Their tools will be proxied through
            OrbitMesh's built-in MCP server.
          </p>
          <div style={{"margin-top":"0.5rem"}}>
            <For each={mcpServers()?.servers}>
              {(server) => (
                <div style={{"margin-bottom": "0.75rem"}}>
                  <label
                    style={{"display":"flex","align-items":"center","gap":"0.5rem","padding":"0.25rem 0","cursor":"pointer"}}
                  >
                    <input
                      type="checkbox"
                      checked={isServerSelected(server.id)}
                      onChange={() => toggleServer(server)}
                    />
                    <span>{server.name}</span>
                    <span class="muted" style={{"font-size":"0.85em"}}>
                      ({server.type === 'command' ? server.command : server.url})
                    </span>
                  </label>
                  <Show when={isServerSelected(server.id)}>
                    <AgentMCPServerConfig
                      server={server}
                      ref={selectedRefs().find(r => r.server_id === server.id)!}
                      onChange={(updatedRef) => {
                        setSelectedRefs(refs => refs.map(r => r.server_id === server.id ? updatedRef : r))
                      }}
                    />
                  </Show>
                </div>
              )}
            </For>
          </div>
        </div>
      </Show>

      <div class="form-group">
        <label>Built-in Tools</label>
        <div style={{"margin-top":"0.5rem"}}>
          <div style={{"margin-bottom": "0.75rem"}}>
            <label
              style={{"display":"flex","align-items":"center","gap":"0.5rem","padding":"0.25rem 0","cursor":"pointer"}}
            >
              <input
                type="checkbox"
                checked={spawnSubAgentEnabled()}
                onChange={(e) => {
                  setSpawnSubAgentEnabled(e.currentTarget.checked);
                  if (e.currentTarget.checked && allowedSubAgents().length === 0) {
                    setAllowedSubAgents([]);
                  }
                }}
              />
              <span>spawn_sub_agent</span>
              <span class="muted" style={{"font-size":"0.85em"}}>
                (Spawn a sub-agent to perform a task)
              </span>
            </label>
            <Show when={spawnSubAgentEnabled()}>
              <div style={{"margin-left": "1.5rem", "margin-top": "0.5rem", "padding-left": "0.5rem", "border-left": "2px solid var(--border-color)"}}>
                <label style={{"font-size": "0.9em", "font-weight": "bold", "margin-bottom": "0.5rem", "display": "block"}}>Allowed Sub-Agents:</label>
                <Show when={agentsList.loading}>
                  <p class="muted" style={{"font-size": "0.9em"}}>Loading agents...</p>
                </Show>
                <Show when={agentsList()?.agents}>
                  <div style={{"display": "flex", "flex-direction": "column", "gap": "0.25rem"}}>
                    <For each={agentsList()?.agents.filter(a => a.id !== props.agent?.id)}>
                      {(agent) => (
                        <label style={{"display": "flex", "align-items": "center", "gap": "0.5rem", "cursor": "pointer", "font-size": "0.9em"}}>
                          <input
                            type="checkbox"
                            checked={allowedSubAgents().includes(agent.id)}
                            onChange={(e) => {
                              if (e.currentTarget.checked) {
                                setAllowedSubAgents(prev => [...prev, agent.id]);
                              } else {
                                setAllowedSubAgents(prev => prev.filter(id => id !== agent.id));
                              }
                            }}
                          />
                          <span>{agent.name}</span>
                          <span class="muted" style={{"font-size": "0.85em"}}>({agent.id})</span>
                        </label>
                      )}
                    </For>
                    <Show when={agentsList()?.agents.filter(a => a.id !== props.agent?.id).length === 0}>
                      <p class="muted" style={{"font-size": "0.9em"}}>No other agents available.</p>
                    </Show>
                  </div>
                </Show>
              </div>
            </Show>
          </div>
        </div>
      </div>

      <Show when={error()}>
        <p class="error-message">{error()}</p>
      </Show>

      <div class="form-actions">
        <button type="button" class="btn btn-secondary" onClick={props.onCancel}>
          Cancel
        </button>
        <button type="submit" class="btn btn-primary" disabled={saving()}>
          {saving() ? 'Saving…' : props.agent ? 'Update' : 'Create'}
        </button>
      </div>
    </form>
  )
}

function AgentsPage() {
  const [agents, { refetch }] = createResource(() => apiClient.listAgents())
  const [formMode, setFormMode] = createSignal<null | 'add' | string>(null)

  const openAdd = () => setFormMode('add')
  const openEdit = (id: string) => setFormMode(id)
  const closeForm = () => setFormMode(null)

  const editingAgent = () => {
    const mode = formMode()
    if (!mode || mode === 'add') return null
    return agents()?.agents.find((agent) => agent.id === mode) ?? null
  }

  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Configuration</p>
          <h2>Agents</h2>
        </div>
        <Show
          when={formMode() === 'add'}
          fallback={
            <button class="btn btn-primary" onClick={openAdd}>
              Add Agent
            </button>
          }
        >
          <button class="btn btn-secondary" onClick={closeForm}>
            Cancel
          </button>
        </Show>
      </div>

      <Show when={formMode() === 'add'}>
        <AgentForm
          onSave={async (config) => {
            await apiClient.createAgent(config)
            refetch()
            closeForm()
          }}
          onCancel={closeForm}
        />
      </Show>

      <div class="providers-list">
        <Show when={agents.loading}>
          <p class="muted">Loading agents…</p>
        </Show>
        <Show when={agents.error}>
          <p class="error-message">Failed to load agents: {String(agents.error)}</p>
        </Show>
        <Show when={agents()?.agents.length === 0 && !agents.loading}>
          <div class="empty-state">
            <p class="muted">No agents configured yet.</p>
            <p class="form-hint">
              Create an agent to define reusable defaults like system prompts and model choice.
            </p>
          </div>
        </Show>

        <For each={agents()?.agents}>
          {(agent) => (
            <>
              <div class={`provider-card${formMode() === agent.id ? ' provider-card--editing' : ''}`}>
                <div class="provider-info">
                  <div class="provider-name-row">
                    <h3>{agent.name}</h3>
                  </div>
                  <p class="muted provider-type-label">Agent ID: {agent.id}</p>
                  <div class="provider-meta-chips">
                    <Show when={modelFromAgent(agent)}>
                      <span class="meta-chip">default model: {modelFromAgent(agent)}</span>
                    </Show>
                    <Show when={agent.system_prompt && agent.system_prompt.length > 0}>
                      <span class="meta-chip">system prompt configured</span>
                    </Show>
                    <Show when={agent.mcp_server_refs && agent.mcp_server_refs.length > 0}>
                      <span class="meta-chip">{agent.mcp_server_refs!.length} MCP server(s)</span>
                    </Show>
                  </div>
                </div>
                <div class="provider-actions">
                  <button
                    class="btn btn-secondary"
                    onClick={() => (formMode() === agent.id ? closeForm() : openEdit(agent.id))}
                  >
                    {formMode() === agent.id ? 'Cancel' : 'Edit'}
                  </button>
                  <button
                    class="btn btn-danger"
                    onClick={async () => {
                      if (confirm(`Delete agent "${agent.name}"?`)) {
                        await apiClient.deleteAgent(agent.id)
                        refetch()
                        if (formMode() === agent.id) closeForm()
                      }
                    }}
                  >
                    Delete
                  </button>
                </div>
              </div>

              <Show when={formMode() === agent.id}>
                <div class="provider-edit-form">
                  <AgentForm
                    agent={editingAgent()!}
                    onSave={async (config) => {
                      await apiClient.updateAgent(agent.id, config)
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

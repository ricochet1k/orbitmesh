import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import type { AgentConfigRequest, AgentConfigResponse } from '../../types/api'

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

      const req: AgentConfigRequest = {
        name: trimmedName,
        system_prompt: systemPrompt().trim() || undefined,
        mcp_servers: props.agent?.mcp_servers,
        custom: Object.keys(custom).length > 0 ? custom : undefined,
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

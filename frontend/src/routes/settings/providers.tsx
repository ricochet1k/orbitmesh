import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import { PROVIDER_TYPES } from './providers/-_shared'
import { ProviderForm } from './providers/-_form'

export const Route = createFileRoute('/settings/providers')({
  component: ProvidersPage,
})

function ProvidersPage() {
  const [providers, { refetch }] = createResource(() => apiClient.listProviders())
  // null = list view, 'add' = adding new, <id> = editing that provider
  const [formMode, setFormMode] = createSignal<null | 'add' | string>(null)

  const editingProvider = () => {
    const mode = formMode()
    if (!mode || mode === 'add') return null
    return providers()?.providers.find((p) => p.id === mode) ?? null
  }

  const openAdd   = () => setFormMode('add')
  const openEdit  = (id: string) => setFormMode(id)
  const closeForm = () => setFormMode(null)

  return (
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
          <p class="muted">Loading providers…</p>
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
              <div
                class={`provider-card${formMode() === provider.id ? ' provider-card--editing' : ''}`}
              >
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
  )
}

import { createFileRoute } from '@tanstack/solid-router'
import { createResource, createSignal, createMemo, For, Show } from 'solid-js'
import { apiClient } from '../../api/client'
import type { ProviderConfigResponse, ProviderUsageInsight } from '../../types/api'
import { PROVIDER_TYPES } from './providers/-_shared'
import { ProviderForm } from './providers/-_form'
import {
  extractProviderModelSummary,
  extractUsageLimitSummary,
  findProviderInsight,
} from '../../utils/providerInsights'
import { useUiStabilityProbe } from '../../state/uiStability'

export const Route = createFileRoute('/settings/providers')({
  component: ProvidersPage,
})

function ProvidersPage() {
  const providerPanelProbe = useUiStabilityProbe('settings/providers/panel', {
    animationNames: ['rise'],
  })
  const [providers, { refetch }] = createResource(() => apiClient.listProviders())
  const [usageInsights, { refetch: refetchInsights }] = createResource(() => apiClient.getProviderUsageInsights())
  // null = list view, 'add' = adding new, <id> = editing that provider
  const [formMode, setFormMode] = createSignal<null | 'add' | string>(null)

  const insightsByProviderId = createMemo(() => {
    const map = new Map<string, ProviderUsageInsight>()
    const providerList = providers()?.providers ?? []
    const insights = usageInsights()?.providers ?? []
    for (const provider of providerList) {
      const insight = findProviderInsight(provider, insights)
      if (insight) map.set(provider.id, insight)
    }
    return map
  })

  const editingProvider = () => {
    const mode = formMode()
    if (!mode || mode === 'add') return null
    return providers()?.providers.find((p) => p.id === mode) ?? null
  }

  const openAdd   = () => setFormMode('add')
  const openEdit  = (id: string) => setFormMode(id)
  const closeForm = () => setFormMode(null)

  return (
    <section
      class="placeholder-panel ds-panel"
      ref={providerPanelProbe}
      data-ui-stability-id="settings.providers.panel"
    >
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Configuration</p>
          <h2>Providers</h2>
        </div>
        <div class="provider-page-actions">
          <button
            class="btn btn-secondary"
            onClick={() => void refetchInsights()}
            disabled={usageInsights.loading}
          >
            {usageInsights.loading ? 'Refreshing…' : 'Refresh insights'}
          </button>
          <Show
            when={formMode() === 'add'}
            fallback={
              <button class="btn btn-primary" onClick={openAdd}>
                Add Provider
              </button>
            }
          >
            <button class="btn btn-secondary" onClick={closeForm}>
              Cancel
            </button>
          </Show>
        </div>
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
        <Show when={usageInsights.error}>
          <p class="error-message">Failed to load provider insights: {String(usageInsights.error)}</p>
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
                  <ProviderCapabilityPanel
                    provider={provider}
                    insight={insightsByProviderId().get(provider.id)}
                  />
                </div>
                <div class="provider-actions">
                  <button
                    class="btn btn-secondary"
                    onClick={() =>
                      formMode() === provider.id ? closeForm() : openEdit(provider.id)
                    }
                  >
                    {formMode() === provider.id ? 'Cancel' : 'Edit'}
                  </button>
                  <button
                    class="btn btn-danger"
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

function ProviderCapabilityPanel(props: {
  provider: ProviderConfigResponse
  insight?: ProviderUsageInsight
}) {
  const modelSummary = createMemo(() => extractProviderModelSummary(props.insight))
  const usageSummary = createMemo(() => extractUsageLimitSummary(props.insight))
  const listPreview = createMemo(() => {
    const models = modelSummary().availableModels
    if (models.length === 0) return ''
    return models.slice(0, 5).join(', ')
  })

  return (
    <div class="provider-capability-panel">
      <div class="provider-capability-block">
        <p class="provider-capability-title">Model availability</p>
        <Show
          when={modelSummary().availableModels.length > 0 || modelSummary().currentModel}
          fallback={
            <p class="provider-capability-note">
              unavailable ({modelSummary().discovery.status})
              <Show when={modelSummary().discovery.reason}>
                {' '} - {modelSummary().discovery.reason}
              </Show>
            </p>
          }
        >
          <p class="provider-capability-note">
            {modelSummary().availableModels.length} known
            <Show when={modelSummary().currentModel}>
              {' '} - current: {modelSummary().currentModel}
            </Show>
          </p>
          <Show when={listPreview()}>
            <p class="provider-capability-list">{listPreview()}</p>
          </Show>
          <Show when={modelSummary().availableModels.length > 5}>
            <p class="provider-capability-note">+{modelSummary().availableModels.length - 5} more</p>
          </Show>
        </Show>
      </div>

      <div class="provider-capability-block">
        <p class="provider-capability-title">Usage limits</p>
        <Show
          when={usageSummary().used !== undefined || usageSummary().limit !== undefined || usageSummary().remaining !== undefined}
          fallback={
            <p class="provider-capability-note">
              {usageSummary().status}
              <Show when={usageSummary().reason}>
                {' '} - {usageSummary().reason}
              </Show>
            </p>
          }
        >
          <p class="provider-capability-note">
            used: {formatMaybeNumber(usageSummary().used)} - limit: {formatMaybeNumber(usageSummary().limit)} - remaining: {formatMaybeNumber(usageSummary().remaining)}
          </p>
          <Show when={usageSummary().resetAt}>
            <p class="provider-capability-note">reset: {usageSummary().resetAt}</p>
          </Show>
        </Show>
      </div>
    </div>
  )
}

function formatMaybeNumber(value: number | undefined): string {
  if (value === undefined) return 'unknown'
  return Number.isInteger(value) ? String(value) : value.toFixed(2)
}

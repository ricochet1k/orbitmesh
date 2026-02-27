import { createFileRoute } from '@tanstack/solid-router'
import { createSignal, onMount } from 'solid-js'

import { getMCPGatewaySettings, updateMCPGatewaySettings } from '../../api/mcpGateway'

export const Route = createFileRoute('/settings/preferences')({
  component: PreferencesPage,
})

function PreferencesPage() {
  const [host, setHost] = createSignal('127.0.0.1')
  const [port, setPort] = createSignal(8790)
  const [saving, setSaving] = createSignal(false)
  const [status, setStatus] = createSignal('')

  onMount(async () => {
    try {
      const settings = await getMCPGatewaySettings()
      setHost(settings.host)
      setPort(settings.port)
    } catch (err) {
      setStatus((err as Error).message)
    }
  })

  async function saveSettings(e: Event) {
    e.preventDefault()
    setSaving(true)
    setStatus('')
    try {
      const settings = await updateMCPGatewaySettings({ host: host().trim(), port: Number(port()) })
      setHost(settings.host)
      setPort(settings.port)
      setStatus('Saved MCP gateway settings.')
    } catch (err) {
      setStatus((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Preferences</p>
          <h2>Default Behaviors</h2>
        </div>
      </div>
      <form class="provider-form" onSubmit={saveSettings}>
        <div class="provider-grid">
          <label>
            MCP WebSocket Host
            <input value={host()} onInput={(e) => setHost(e.currentTarget.value)} placeholder="127.0.0.1" />
          </label>
          <label>
            MCP WebSocket Port
            <input
              type="number"
              min="1"
              max="65535"
              value={port()}
              onInput={(e) => setPort(parseInt(e.currentTarget.value || '0', 10))}
            />
          </label>
        </div>
        <button type="submit" disabled={saving()} class="btn btn-primary">
          {saving() ? 'Saving...' : 'Save'}
        </button>
      </form>
      <p class="muted">{status()}</p>
    </section>
  )
}

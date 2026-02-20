import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/integrations')({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  return (
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
  )
}

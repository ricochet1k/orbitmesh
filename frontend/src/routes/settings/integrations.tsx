import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/integrations')({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Integrations</p>
          <h2>Connected Services</h2>
        </div>
        <span class="panel-pill neutral ds-pill ds-pill-neutral">Planned</span>
      </div>
      <p class="muted">Integration toggles and keys are coming soon.</p>
    </section>
  )
}

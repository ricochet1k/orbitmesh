import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/preferences')({
  component: PreferencesPage,
})

function PreferencesPage() {
  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Preferences</p>
          <h2>Default Behaviors</h2>
        </div>
        <span class="panel-pill neutral ds-pill ds-pill-neutral">Planned</span>
      </div>
      <p class="muted">Preference settings will live in this area.</p>
    </section>
  )
}

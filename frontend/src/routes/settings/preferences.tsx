import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/preferences')({
  component: PreferencesPage,
})

function PreferencesPage() {
  return (
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
  )
}

import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings')({
  component: RouteComponent,
})

function RouteComponent() {
  return (
    <div class="settings-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Workspace settings</p>
          <h1>Settings</h1>
          <p class="dashboard-subtitle">
            Placeholder controls for preferences, integrations, and operational defaults.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Profile</p>
            <strong>Coming soon</strong>
          </div>
          <div class="meta-card">
            <p>Integrations</p>
            <strong>Coming soon</strong>
          </div>
          <div class="meta-card">
            <p>Notifications</p>
            <strong>Coming soon</strong>
          </div>
        </div>
      </header>

      <main class="settings-layout">
        <section class="placeholder-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Account</p>
              <h2>User Profile</h2>
            </div>
            <span class="panel-pill neutral">Planned</span>
          </div>
          <p class="muted">User profile controls will appear here.</p>
        </section>

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
      </main>
    </div>
  )
}

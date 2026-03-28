import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/about')({
  component: About,
})

function About() {
  return (
    <div class="about-view ds-page" data-testid="about-view">
      <header class="view-header ds-page-header">
        <div>
          <p class="eyebrow ds-page-kicker">Product overview</p>
          <h1>About OrbitMesh</h1>
          <p class="dashboard-subtitle ds-page-subtitle">
            OrbitMesh coordinates agent sessions, task execution, terminal streams, and provider integrations in one operational workspace.
          </p>
        </div>
      </header>

      <main class="ds-layout">
        <section class="ds-panel">
          <div class="panel-header ds-panel-header">
            <div>
              <p class="panel-kicker ds-kicker">What you can do here</p>
              <h2>Core surfaces</h2>
            </div>
          </div>
          <ul class="dashboard-list">
            <li class="dashboard-list-item">
              <div>
                <p class="dashboard-list-title">Dashboard</p>
                <p class="ds-muted">Monitor session activity and codeflow signals.</p>
              </div>
            </li>
            <li class="dashboard-list-item">
              <div>
                <p class="dashboard-list-title">Tasks & Sessions</p>
                <p class="ds-muted">Plan work and launch agent sessions against task context.</p>
              </div>
            </li>
            <li class="dashboard-list-item">
              <div>
                <p class="dashboard-list-title">Terminals & Files</p>
                <p class="ds-muted">Inspect live terminal state and browse project files.</p>
              </div>
            </li>
            <li class="dashboard-list-item">
              <div>
                <p class="dashboard-list-title">Settings</p>
                <p class="ds-muted">Configure providers, agents, MCP servers, and preferences.</p>
              </div>
            </li>
          </ul>
        </section>
      </main>
    </div>
  )
}

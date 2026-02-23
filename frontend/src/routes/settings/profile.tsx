import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  return (
    <section class="placeholder-panel ds-panel">
      <div class="panel-header ds-panel-header">
        <div>
          <p class="panel-kicker ds-kicker">Account</p>
          <h2>User Profile</h2>
        </div>
        <span class="panel-pill neutral ds-pill ds-pill-neutral">Planned</span>
      </div>
      <p class="muted">User profile controls will appear here.</p>
    </section>
  )
}

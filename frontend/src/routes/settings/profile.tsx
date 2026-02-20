import { createFileRoute } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  return (
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
  )
}

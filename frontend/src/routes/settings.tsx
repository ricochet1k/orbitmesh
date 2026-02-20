import { createFileRoute, Outlet, Link, useLocation } from '@tanstack/solid-router'

export const Route = createFileRoute('/settings')({
  component: SettingsLayout,
})

const SETTINGS_NAV = [
  { to: '/settings/providers',    label: 'Providers'     },
  { to: '/settings/profile',      label: 'User Profile'  },
  { to: '/settings/integrations', label: 'Integrations'  },
  { to: '/settings/preferences',  label: 'Preferences'   },
] as const

function SettingsLayout() {
  const location = useLocation()

  return (
    <div class="settings-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Workspace settings</p>
          <h1>Settings</h1>
          <p class="dashboard-subtitle">
            Configure providers, integrations, and operational defaults.
          </p>
        </div>
      </header>

      <div class="settings-shell">
        <nav class="settings-subnav">
          {SETTINGS_NAV.map((item) => (
            <Link
              to={item.to}
              class={`settings-subnav-item${location().pathname === item.to ? ' active' : ''}`}
            >
              {item.label}
            </Link>
          ))}
        </nav>

        <main class="settings-content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

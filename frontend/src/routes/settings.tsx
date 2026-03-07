import { createFileRoute, Outlet, Link, useLocation } from '@tanstack/solid-router'
import { createSignal } from 'solid-js'

export const Route = createFileRoute('/settings')({
  component: SettingsLayout,
})

const SETTINGS_NAV = [
  { to: '/settings/providers',    label: 'Providers'     },
  { to: '/settings/agents',       label: 'Agents'        },
  { to: '/settings/mcp-servers',  label: 'MCP Servers'   },
  { to: '/settings/profile',      label: 'User Profile'  },
  { to: '/settings/integrations', label: 'Integrations'  },
  { to: '/settings/preferences',  label: 'Preferences'   },
] as const

function SettingsLayout() {
  const location = useLocation()
  const [mobileNavOpen, setMobileNavOpen] = createSignal(false)

  return (
    <div class="settings-view ds-page">
      <header class="view-header ds-page-header">
        <div>
          <p class="eyebrow ds-page-kicker">Workspace settings</p>
          <h1>Settings</h1>
          <p class="dashboard-subtitle ds-page-subtitle">
            Configure providers, agents, integrations, and operational defaults.
          </p>
        </div>
      </header>

      <div class="settings-shell">
        <button
          class="btn btn-secondary settings-subnav-toggle"
          type="button"
          aria-label="Toggle settings navigation"
          aria-expanded={mobileNavOpen()}
          aria-controls="settings-subnav"
          onClick={() => setMobileNavOpen((open) => !open)}
        >
          <span class="sidebar-toggle-icon" aria-hidden="true" />
          <span>{mobileNavOpen() ? 'Close menu' : 'Settings menu'}</span>
        </button>

        <nav id="settings-subnav" class={`settings-subnav${mobileNavOpen() ? ' open' : ''}`}>
          {SETTINGS_NAV.map((item) => (
            <Link
              to={item.to}
              class={`settings-subnav-item${location().pathname === item.to ? ' active' : ''}`}
              onClick={() => setMobileNavOpen(false)}
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

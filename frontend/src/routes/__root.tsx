import { createRootRoute, Outlet } from '@tanstack/solid-router'
import { TanStackRouterDevtools } from '@tanstack/solid-router-devtools'
import Sidebar from '../components/Sidebar'
import AgentDock from '../components/AgentDock'

const RootLayout = () => (
  <div class="app-shell">
    <Sidebar />
    <Outlet />
    <TanStackRouterDevtools />
    <AgentDock />
  </div>
)

export const Route = createRootRoute({ component: RootLayout })

import { createRootRoute, Outlet } from '@tanstack/solid-router'
import { TanStackRouterDevtools } from '@tanstack/solid-router-devtools'
import Sidebar from '../components/Sidebar'
import AgentDock from '../components/AgentDock'
import { UiStabilityRuntime } from '../state/uiStability'

const RootLayout = () => (
  <div class="app-shell ds-shell">
    <Sidebar />
    <Outlet />
    <UiStabilityRuntime />
    <TanStackRouterDevtools />
    <AgentDock />
  </div>
)

export const Route = createRootRoute({ component: RootLayout })
